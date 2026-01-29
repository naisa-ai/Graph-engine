package grpc

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func TestQuotaManager_Basic(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := QuotaConfig{
		MaxConcurrentPerTenant: 5,
		MaxGlobalConcurrent:    10,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
	}

	qm := NewQuotaManager(config, logger)

	// Acquire a slot
	release, err := qm.Acquire("tenant1")
	if err != nil {
		t.Fatalf("Acquire failed: %v", err)
	}

	// Check stats
	stats := qm.Stats()
	if stats.GlobalConcurrent != 1 {
		t.Errorf("expected GlobalConcurrent 1, got %d", stats.GlobalConcurrent)
	}
	if stats.TenantConcurrent["tenant1"] != 1 {
		t.Errorf("expected tenant1 concurrent 1, got %d", stats.TenantConcurrent["tenant1"])
	}

	// Release
	release()

	stats = qm.Stats()
	if stats.GlobalConcurrent != 0 {
		t.Errorf("expected GlobalConcurrent 0 after release, got %d", stats.GlobalConcurrent)
	}
}

func TestQuotaManager_PerTenantLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := QuotaConfig{
		MaxConcurrentPerTenant: 2,
		MaxGlobalConcurrent:    100,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
	}

	qm := NewQuotaManager(config, logger)

	// Acquire slots up to limit
	var releases []func()
	for i := 0; i < 2; i++ {
		release, err := qm.Acquire("tenant1")
		if err != nil {
			t.Fatalf("Acquire %d failed: %v", i, err)
		}
		releases = append(releases, release)
	}

	// Next acquire should fail
	_, err := qm.Acquire("tenant1")
	if err == nil {
		t.Error("expected error when per-tenant limit exceeded")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Errorf("expected ResourceExhausted error, got %v", err)
	}

	// Different tenant should still work
	release, err := qm.Acquire("tenant2")
	if err != nil {
		t.Errorf("different tenant should succeed: %v", err)
	}
	release()

	// Release one slot
	releases[0]()

	// Now tenant1 can acquire again
	release, err = qm.Acquire("tenant1")
	if err != nil {
		t.Errorf("should succeed after release: %v", err)
	}
	release()

	// Cleanup
	for _, r := range releases[1:] {
		r()
	}
}

func TestQuotaManager_GlobalLimit(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := QuotaConfig{
		MaxConcurrentPerTenant: 100,
		MaxGlobalConcurrent:    3,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
	}

	qm := NewQuotaManager(config, logger)

	// Acquire slots from different tenants up to global limit
	var releases []func()
	for i := 0; i < 3; i++ {
		tenant := "tenant" + string(rune('1'+i))
		release, err := qm.Acquire(tenant)
		if err != nil {
			t.Fatalf("Acquire for %s failed: %v", tenant, err)
		}
		releases = append(releases, release)
	}

	// Next acquire should fail regardless of tenant
	_, err := qm.Acquire("tenant-new")
	if err == nil {
		t.Error("expected error when global limit exceeded")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Errorf("expected ResourceExhausted error, got %v", err)
	}

	// Cleanup
	for _, r := range releases {
		r()
	}
}

func TestQuotaManager_Concurrent(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := QuotaConfig{
		MaxConcurrentPerTenant: 10,
		MaxGlobalConcurrent:    20,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
	}

	qm := NewQuotaManager(config, logger)

	// Concurrent acquire/release
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tenant := "tenant" + string(rune('1'+(idx%3)))
			release, err := qm.Acquire(tenant)
			if err == nil {
				// Hold for a tiny bit
				release()
			}
		}(i)
	}

	wg.Wait()

	// All should be released
	stats := qm.Stats()
	if stats.GlobalConcurrent != 0 {
		t.Errorf("expected GlobalConcurrent 0, got %d", stats.GlobalConcurrent)
	}
}

func TestQuotaManager_Stats(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := QuotaConfig{
		MaxConcurrentPerTenant: 5,
		MaxGlobalConcurrent:    10,
		TenantHeader:           "x-tenant-id",
		DefaultTenantID:        "default",
	}

	qm := NewQuotaManager(config, logger)

	stats := qm.Stats()
	if stats.MaxGlobalConcurrent != 10 {
		t.Errorf("expected MaxGlobalConcurrent 10, got %d", stats.MaxGlobalConcurrent)
	}
	if stats.MaxPerTenant != 5 {
		t.Errorf("expected MaxPerTenant 5, got %d", stats.MaxPerTenant)
	}
}

func TestExtractTenantID(t *testing.T) {
	tests := []struct {
		name     string
		md       metadata.MD
		header   string
		defID    string
		expected string
	}{
		{
			name:     "no metadata",
			md:       nil,
			header:   "x-tenant-id",
			defID:    "default",
			expected: "default",
		},
		{
			name:     "missing header",
			md:       metadata.Pairs("other-header", "value"),
			header:   "x-tenant-id",
			defID:    "default",
			expected: "default",
		},
		{
			name:     "empty header value",
			md:       metadata.Pairs("x-tenant-id", ""),
			header:   "x-tenant-id",
			defID:    "default",
			expected: "default",
		},
		{
			name:     "valid tenant",
			md:       metadata.Pairs("x-tenant-id", "my-tenant"),
			header:   "x-tenant-id",
			defID:    "default",
			expected: "my-tenant",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.md != nil {
				ctx = metadata.NewIncomingContext(ctx, tt.md)
			}

			result := extractTenantID(ctx, tt.header, tt.defID)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}
