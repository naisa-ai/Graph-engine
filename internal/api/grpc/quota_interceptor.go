package grpc

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/naisa-ai/graph-engine/internal/metrics"
)

// QuotaConfig holds configuration for the quota interceptor.
type QuotaConfig struct {
	// MaxConcurrentPerTenant is the maximum concurrent requests per tenant.
	// 0 means unlimited.
	MaxConcurrentPerTenant int32

	// MaxGlobalConcurrent is the maximum total concurrent requests.
	// 0 means unlimited.
	MaxGlobalConcurrent int32

	// TenantHeader is the metadata key for tenant identification.
	// Default is "x-tenant-id".
	TenantHeader string

	// DefaultTenantID is used when no tenant is specified.
	DefaultTenantID string

	// ExemptMethods are methods that bypass quota checks (e.g., Health).
	ExemptMethods map[string]bool
}

// DefaultQuotaConfig returns default quota configuration.
func DefaultQuotaConfig() QuotaConfig {
	return QuotaConfig{
		MaxConcurrentPerTenant: 10,
		MaxGlobalConcurrent:    100,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
		ExemptMethods: map[string]bool{
			"Health":     true,
			"ListGraphs": true,
			"CacheStats": true,
		},
	}
}

// QuotaManager tracks and enforces request quotas.
type QuotaManager struct {
	config QuotaConfig
	logger *slog.Logger

	// Per-tenant concurrent request counts
	tenantCounts map[string]*int32
	tenantMu     sync.RWMutex

	// Global concurrent request count
	globalCount int32

	// Metrics
	quotaRejections int64
}

// NewQuotaManager creates a new quota manager.
func NewQuotaManager(config QuotaConfig, logger *slog.Logger) *QuotaManager {
	return &QuotaManager{
		config:       config,
		logger:       logger,
		tenantCounts: make(map[string]*int32),
	}
}

// Acquire attempts to acquire a quota slot for the given tenant.
// Returns a release function if successful, or an error if quota exceeded.
func (q *QuotaManager) Acquire(tenantID string) (func(), error) {
	// Check global limit
	if q.config.MaxGlobalConcurrent > 0 {
		newGlobal := atomic.AddInt32(&q.globalCount, 1)
		if newGlobal > q.config.MaxGlobalConcurrent {
			atomic.AddInt32(&q.globalCount, -1)
			atomic.AddInt64(&q.quotaRejections, 1)
			metrics.IncErrors("quota", "global_exceeded")
			return nil, status.Errorf(codes.ResourceExhausted,
				"global request limit exceeded (%d/%d)", newGlobal-1, q.config.MaxGlobalConcurrent)
		}
	}

	// Check per-tenant limit
	if q.config.MaxConcurrentPerTenant > 0 {
		count := q.getTenantCount(tenantID)
		newCount := atomic.AddInt32(count, 1)
		if newCount > q.config.MaxConcurrentPerTenant {
			atomic.AddInt32(count, -1)
			if q.config.MaxGlobalConcurrent > 0 {
				atomic.AddInt32(&q.globalCount, -1)
			}
			atomic.AddInt64(&q.quotaRejections, 1)
			metrics.IncErrors("quota", "tenant_exceeded")
			return nil, status.Errorf(codes.ResourceExhausted,
				"tenant request limit exceeded for %q (%d/%d)", tenantID, newCount-1, q.config.MaxConcurrentPerTenant)
		}
	}

	// Return release function
	return func() {
		if q.config.MaxConcurrentPerTenant > 0 {
			count := q.getTenantCount(tenantID)
			atomic.AddInt32(count, -1)
		}
		if q.config.MaxGlobalConcurrent > 0 {
			atomic.AddInt32(&q.globalCount, -1)
		}
	}, nil
}

// getTenantCount returns the counter for a tenant, creating if needed.
func (q *QuotaManager) getTenantCount(tenantID string) *int32 {
	q.tenantMu.RLock()
	count, ok := q.tenantCounts[tenantID]
	q.tenantMu.RUnlock()

	if ok {
		return count
	}

	q.tenantMu.Lock()
	defer q.tenantMu.Unlock()

	// Double-check after acquiring write lock
	if count, ok := q.tenantCounts[tenantID]; ok {
		return count
	}

	var newCount int32
	q.tenantCounts[tenantID] = &newCount
	return &newCount
}

// Stats returns quota statistics.
func (q *QuotaManager) Stats() QuotaStats {
	q.tenantMu.RLock()
	tenantStats := make(map[string]int32, len(q.tenantCounts))
	for tenant, count := range q.tenantCounts {
		tenantStats[tenant] = atomic.LoadInt32(count)
	}
	q.tenantMu.RUnlock()

	return QuotaStats{
		GlobalConcurrent:    atomic.LoadInt32(&q.globalCount),
		TenantConcurrent:    tenantStats,
		TotalRejections:     atomic.LoadInt64(&q.quotaRejections),
		MaxGlobalConcurrent: q.config.MaxGlobalConcurrent,
		MaxPerTenant:        q.config.MaxConcurrentPerTenant,
	}
}

// QuotaStats holds quota statistics.
type QuotaStats struct {
	GlobalConcurrent    int32
	TenantConcurrent    map[string]int32
	TotalRejections     int64
	MaxGlobalConcurrent int32
	MaxPerTenant        int32
}

// extractTenantID extracts the tenant ID from gRPC metadata.
func extractTenantID(ctx context.Context, header, defaultID string) string {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return defaultID
	}

	values := md.Get(header)
	if len(values) == 0 || values[0] == "" {
		return defaultID
	}

	return values[0]
}

// quotaInterceptor returns a unary interceptor that enforces quotas.
func (s *Server) quotaInterceptor(qm *QuotaManager) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// Check if method is exempt
		method := extractMethodName(info.FullMethod)
		if qm.config.ExemptMethods[method] {
			return handler(ctx, req)
		}

		// Extract tenant ID
		tenantID := extractTenantID(ctx, qm.config.TenantHeader, qm.config.DefaultTenantID)

		// Acquire quota
		release, err := qm.Acquire(tenantID)
		if err != nil {
			s.logger.Warn("quota exceeded",
				"method", method,
				"tenant_id", tenantID,
				"error", err,
			)
			return nil, err
		}
		defer release()

		return handler(ctx, req)
	}
}

// streamQuotaInterceptor returns a stream interceptor that enforces quotas.
func (s *Server) streamQuotaInterceptor(qm *QuotaManager) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// Check if method is exempt
		method := extractMethodName(info.FullMethod)
		if qm.config.ExemptMethods[method] {
			return handler(srv, ss)
		}

		// Extract tenant ID
		tenantID := extractTenantID(ss.Context(), qm.config.TenantHeader, qm.config.DefaultTenantID)

		// Acquire quota
		release, err := qm.Acquire(tenantID)
		if err != nil {
			s.logger.Warn("stream quota exceeded",
				"method", method,
				"tenant_id", tenantID,
				"error", err,
			)
			return err
		}
		defer release()

		return handler(srv, ss)
	}
}
