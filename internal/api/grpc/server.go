// Package grpc provides the gRPC server implementation for graph-engine.
package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/recovery"

	"github.com/naisa-ai/graph-engine/internal/config"
	"github.com/naisa-ai/graph-engine/internal/metrics"
	"github.com/naisa-ai/graph-engine/internal/service"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// Server wraps the gRPC server and its dependencies.
type Server struct {
	cfg        *config.Config
	grpcServer *grpc.Server
	logger     *slog.Logger

	// Core services
	buildStore    *service.BuildStore
	versionStore  *service.VersionStore
	graphRegistry *service.GraphRegistry
	resultStore   *service.ResultStore
	viewStore     *service.ViewStore
	viewManager   *service.ViewManager

	// Quota management
	quotaManager *QuotaManager

	// Service handlers
	graphEngine    *GraphEngineHandler
	graphEngineOps *GraphEngineOpsHandler
}

// NewServer creates a new gRPC server with the given configuration.
func NewServer(cfg *config.Config, logger *slog.Logger, buildStore *service.BuildStore) (*Server, error) {
	s := &Server{
		cfg:        cfg,
		logger:     logger,
		buildStore: buildStore,
	}

	// Create core services
	// VersionStore: 5 minute TTL for old versions, 1GB memory limit
	s.versionStore = service.NewVersionStore(5*time.Minute, 1<<30)

	// GraphRegistry: max 10 graphs (from config)
	s.graphRegistry = service.NewGraphRegistry(s.versionStore, cfg.Limits.MaxGraphs)

	// ResultStore: max 1000 results, 500MB memory, 10 minute TTL
	s.resultStore = service.NewResultStore(1000, 500<<20, 10*time.Minute)

	// ViewStore: max 100 views, 200MB memory, 5 minute TTL
	s.viewStore = service.NewViewStore(100, 200<<20, 5*time.Minute)

	// ViewManager: orchestrates view creation and caching
	s.viewManager = service.NewViewManager(s.viewStore, s.versionStore, logger)

	// QuotaManager: enforce per-tenant and global request limits
	quotaConfig := DefaultQuotaConfig()
	if cfg.Limits.MaxConcurrentPerTenant > 0 {
		quotaConfig.MaxConcurrentPerTenant = int32(cfg.Limits.MaxConcurrentPerTenant)
	}
	if cfg.Limits.MaxGlobalConcurrent > 0 {
		quotaConfig.MaxGlobalConcurrent = int32(cfg.Limits.MaxGlobalConcurrent)
	}
	s.quotaManager = NewQuotaManager(quotaConfig, logger)

	// Create interceptors
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			s.metricsInterceptor(),
			s.quotaInterceptor(s.quotaManager),
			s.loggingInterceptor(),
			s.authInterceptor(),
			recovery.UnaryServerInterceptor(),
		),
		grpc.ChainStreamInterceptor(
			s.streamMetricsInterceptor(),
			s.streamQuotaInterceptor(s.quotaManager),
			s.streamLoggingInterceptor(),
			s.streamAuthInterceptor(),
			recovery.StreamServerInterceptor(),
		),
	}

	// Add TLS if configured
	if cfg.TLS.Enabled {
		creds, err := credentials.NewServerTLSFromFile(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		opts = append(opts, grpc.Creds(creds))
	}

	s.grpcServer = grpc.NewServer(opts...)

	// Initialize handlers
	s.graphEngine = NewGraphEngineHandler(logger, buildStore, s.graphRegistry, s.versionStore, s.resultStore, s.viewManager)
	s.graphEngineOps = NewGraphEngineOpsHandler(logger, buildStore, s.graphRegistry, s.versionStore, s.resultStore, s.viewStore, cfg.Limits.MaxExportEdges)

	// Register services
	gepb.RegisterGraphEngineServer(s.grpcServer, s.graphEngine)
	gepb.RegisterGraphEngineOpsServer(s.grpcServer, s.graphEngineOps)

	// Enable reflection for debugging
	reflection.Register(s.grpcServer)

	return s, nil
}

// Serve starts the gRPC server on the configured address.
func (s *Server) Serve() error {
	addr := s.cfg.GRPCAddr()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	return s.ServeListener(lis)
}

// ServeListener starts the gRPC server on the provided listener.
// This is useful for testing with a custom listener.
func (s *Server) ServeListener(lis net.Listener) error {
	s.logger.Info("starting gRPC server", "addr", lis.Addr().String())
	return s.grpcServer.Serve(lis)
}

// GracefulStop stops the server gracefully.
func (s *Server) GracefulStop() {
	s.logger.Info("stopping gRPC server gracefully")
	s.grpcServer.GracefulStop()
}

// Stop stops the server immediately.
func (s *Server) Stop() {
	s.logger.Info("stopping gRPC server")
	s.grpcServer.Stop()
}

// loggingInterceptor returns a unary interceptor that logs requests.
func (s *Server) loggingInterceptor() grpc.UnaryServerInterceptor {
	logFn := func(ctx context.Context, level logging.Level, msg string, fields ...any) {
		s.logger.Log(ctx, slog.Level(level), msg, fields...)
	}

	return logging.UnaryServerInterceptor(logging.LoggerFunc(logFn))
}

// streamLoggingInterceptor returns a stream interceptor that logs requests.
func (s *Server) streamLoggingInterceptor() grpc.StreamServerInterceptor {
	logFn := func(ctx context.Context, level logging.Level, msg string, fields ...any) {
		s.logger.Log(ctx, slog.Level(level), msg, fields...)
	}

	return logging.StreamServerInterceptor(logging.LoggerFunc(logFn))
}

// authInterceptor returns a unary interceptor for authentication (placeholder).
func (s *Server) authInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// TODO: Implement actual auth (mTLS validation, JWT token check, etc.)
		// For now, this is a pass-through placeholder.
		if s.cfg.Auth.Enabled {
			s.logger.Debug("auth check", "method", info.FullMethod)
			// Extract and validate token from metadata here
		}
		return handler(ctx, req)
	}
}

// streamAuthInterceptor returns a stream interceptor for authentication (placeholder).
func (s *Server) streamAuthInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		// TODO: Implement actual auth for streams
		if s.cfg.Auth.Enabled {
			s.logger.Debug("stream auth check", "method", info.FullMethod)
		}
		return handler(srv, ss)
	}
}

// metricsInterceptor returns a unary interceptor that records Prometheus metrics.
func (s *Server) metricsInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		// Extract method name (last part of full method)
		method := extractMethodName(info.FullMethod)
		metrics.IncRequests(method)

		// Call the handler
		resp, err := handler(ctx, req)

		// Record duration and status
		duration := time.Since(start).Seconds()
		statusCode := "OK"
		if err != nil {
			st, _ := status.FromError(err)
			statusCode = st.Code().String()
			metrics.IncErrors(method, statusCode)
		}
		metrics.RecordRPCDuration(method, statusCode, duration)

		return resp, err
	}
}

// streamMetricsInterceptor returns a stream interceptor that records Prometheus metrics.
func (s *Server) streamMetricsInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		start := time.Now()

		// Extract method name
		method := extractMethodName(info.FullMethod)
		metrics.IncRequests(method)

		// Call the handler
		err := handler(srv, ss)

		// Record duration and status
		duration := time.Since(start).Seconds()
		statusCode := "OK"
		if err != nil {
			st, _ := status.FromError(err)
			statusCode = st.Code().String()
			metrics.IncErrors(method, statusCode)
		}
		metrics.RecordRPCDuration(method, statusCode, duration)

		return err
	}
}

// extractMethodName extracts the method name from a full gRPC method path.
// e.g., "/graphengine.v1.GraphEngine/Run" -> "Run"
func extractMethodName(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return fullMethod
}
