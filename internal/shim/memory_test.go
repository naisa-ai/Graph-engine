package shim

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// -----------------------------------------------------------------------------
// TestMemoryTracker_Allocate
// -----------------------------------------------------------------------------

func TestMemoryTracker_Allocate(t *testing.T) {
	t.Run("basic allocation", func(t *testing.T) {
		tracker := NewMemoryTracker(0) // unlimited

		id, err := tracker.Allocate(1000, "test", "label1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if id == 0 {
			t.Error("expected non-zero allocation ID")
		}

		if tracker.Current() != 1000 {
			t.Errorf("expected current 1000, got %d", tracker.Current())
		}
	})

	t.Run("multiple allocations", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id1, err := tracker.Allocate(100, "graph", "g1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		id2, err := tracker.Allocate(200, "view", "v1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		id3, err := tracker.Allocate(300, "result", "r1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if id1 == id2 || id2 == id3 || id1 == id3 {
			t.Error("allocation IDs should be unique")
		}

		if tracker.Current() != 600 {
			t.Errorf("expected current 600, got %d", tracker.Current())
		}

		stats := tracker.Stats()
		if stats.TotalAllocations != 3 {
			t.Errorf("expected 3 total allocations, got %d", stats.TotalAllocations)
		}
		if stats.ActiveAllocations != 3 {
			t.Errorf("expected 3 active allocations, got %d", stats.ActiveAllocations)
		}
	})

	t.Run("allocation updates peak", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(1000, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		if tracker.Peak() != 1000 {
			t.Errorf("expected peak 1000, got %d", tracker.Peak())
		}

		_, err = tracker.Allocate(500, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		if tracker.Peak() != 1500 {
			t.Errorf("expected peak 1500, got %d", tracker.Peak())
		}

		// Deallocate and allocate less - peak should stay
		err = tracker.DeallocateBySize(500, "test")
		if err != nil {
			t.Errorf("DeallocateBySize failed: %v", err)
		}
		_, err = tracker.Allocate(100, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if tracker.Peak() != 1500 {
			t.Errorf("peak should remain 1500, got %d", tracker.Peak())
		}
	})

	t.Run("allocation with callback", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		var callbackCalled bool
		var callbackInfo *AllocationInfo

		tracker.SetCallbacks(func(info *AllocationInfo) {
			callbackCalled = true
			callbackInfo = info
		}, nil)

		_, err := tracker.Allocate(500, "graph", "test-label")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if !callbackCalled {
			t.Error("allocation callback should have been called")
		}
		if callbackInfo == nil || callbackInfo.Size != 500 || callbackInfo.Type != "graph" {
			t.Error("callback received incorrect info")
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Deallocate
// -----------------------------------------------------------------------------

func TestMemoryTracker_Deallocate(t *testing.T) {
	t.Run("basic deallocation", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id, err := tracker.Allocate(1000, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		err = tracker.Deallocate(id)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tracker.Current() != 0 {
			t.Errorf("expected current 0 after deallocation, got %d", tracker.Current())
		}
	})

	t.Run("deallocation updates stats", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id1, err := tracker.Allocate(100, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		id2, err := tracker.Allocate(200, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		err = tracker.Deallocate(id1)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}

		stats := tracker.Stats()
		if stats.CurrentBytes != 200 {
			t.Errorf("expected current 200, got %d", stats.CurrentBytes)
		}
		if stats.TotalDeallocations != 1 {
			t.Errorf("expected 1 deallocation, got %d", stats.TotalDeallocations)
		}
		if 		stats.ActiveAllocations != 1 {
			t.Errorf("expected 1 active allocation, got %d", stats.ActiveAllocations)
		}

		err = tracker.Deallocate(id2)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}
		stats = tracker.Stats()
		if stats.CurrentBytes != 0 {
			t.Errorf("expected current 0, got %d", stats.CurrentBytes)
		}
	})

	t.Run("deallocation with callback", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		var callbackCalled bool
		tracker.SetCallbacks(nil, func(info *AllocationInfo) {
			callbackCalled = true
		})

		id, err := tracker.Allocate(500, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		err = tracker.Deallocate(id)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}

		if !callbackCalled {
			t.Error("deallocation callback should have been called")
		}
	})

	t.Run("deallocate by size", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "graph", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(200, "view", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(100, "result", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		err = tracker.DeallocateBySize(200, "view")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tracker.Current() != 200 {
			t.Errorf("expected current 200, got %d", tracker.Current())
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Limits
// -----------------------------------------------------------------------------

func TestMemoryTracker_Limits(t *testing.T) {
	t.Run("allocation within limit", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		_, err := tracker.Allocate(500, "test", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		_, err = tracker.Allocate(400, "test", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tracker.Current() != 900 {
			t.Errorf("expected current 900, got %d", tracker.Current())
		}
	})

	t.Run("allocation exceeds limit", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		_, err := tracker.Allocate(600, "test", "")
		if err != nil {
			t.Fatalf("first allocation should succeed: %v", err)
		}

		_, err = tracker.Allocate(500, "test", "")
		if !errors.Is(err, ErrMemoryLimitExceeded) {
			t.Errorf("expected ErrMemoryLimitExceeded, got %v", err)
		}

		// Current should still be 600
		if tracker.Current() != 600 {
			t.Errorf("expected current 600, got %d", tracker.Current())
		}
	})

	t.Run("allocation exactly at limit", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		_, err := tracker.Allocate(1000, "test", "")
		if err != nil {
			t.Fatalf("allocation at limit should succeed: %v", err)
		}

		_, err = tracker.Allocate(1, "test", "")
		if !errors.Is(err, ErrMemoryLimitExceeded) {
			t.Errorf("expected ErrMemoryLimitExceeded for even 1 byte over limit")
		}
	})

	t.Run("can allocate check", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		if !tracker.CanAllocate(500) {
			t.Error("should be able to allocate 500")
		}

		_, err := tracker.Allocate(600, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if tracker.CanAllocate(500) {
			t.Error("should not be able to allocate 500 more")
		}

		if !tracker.CanAllocate(400) {
			t.Error("should be able to allocate 400")
		}
	})

	t.Run("available memory", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		if tracker.Available() != 1000 {
			t.Errorf("expected 1000 available, got %d", tracker.Available())
		}

		_, err := tracker.Allocate(300, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		if tracker.Available() != 700 {
			t.Errorf("expected 700 available, got %d", tracker.Available())
		}
	})

	t.Run("unlimited tracker", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		// Should be able to allocate any amount
		_, err := tracker.Allocate(1<<30, "test", "") // 1 GB
		if err != nil {
			t.Errorf("unlimited tracker should not reject allocation: %v", err)
		}

		if !tracker.CanAllocate(1 << 40) { // 1 TB
			t.Error("unlimited tracker should allow any allocation")
		}
	})

	t.Run("set limit", func(t *testing.T) {
		tracker := NewMemoryTracker(2000)

		_, err := tracker.Allocate(500, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		// Can reduce limit above current usage
		err = tracker.SetLimit(1000)
		if err != nil {
			t.Errorf("should be able to set limit above current usage: %v", err)
		}

		// Cannot reduce limit below current usage
		err = tracker.SetLimit(400)
		if !errors.Is(err, ErrMemoryLimitExceeded) {
			t.Errorf("expected error when setting limit below current usage, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_LeakDetect
// -----------------------------------------------------------------------------

func TestMemoryTracker_LeakDetect(t *testing.T) {
	t.Run("no leaks", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id, err := tracker.Allocate(100, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		err = tracker.Deallocate(id)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}

		report := tracker.CheckLeaks(0)
		if report.HasLeaks {
			t.Error("should have no leaks")
		}
	})

	t.Run("detect old allocations", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "test", "old1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(200, "test", "old2")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		// Wait a bit
		time.Sleep(50 * time.Millisecond)

		_, err = tracker.Allocate(300, "test", "new")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		report := tracker.CheckLeaks(40 * time.Millisecond)
		if !report.HasLeaks {
			t.Error("should detect old allocations as potential leaks")
		}
		if report.LeakedCount != 2 {
			t.Errorf("expected 2 potential leaks, got %d", report.LeakedCount)
		}
		if report.LeakedBytes != 300 {
			t.Errorf("expected 300 leaked bytes, got %d", report.LeakedBytes)
		}
	})

	t.Run("leak report details", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "graph", "leaked-graph")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		time.Sleep(20 * time.Millisecond)

		report := tracker.CheckLeaks(10 * time.Millisecond)

		if len(report.OldAllocations) != 1 {
			t.Fatalf("expected 1 old allocation, got %d", len(report.OldAllocations))
		}

		info := report.OldAllocations[0]
		if info.Type != "graph" || info.Label != "leaked-graph" {
			t.Error("leak report should contain allocation details")
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Concurrent
// -----------------------------------------------------------------------------

func TestMemoryTracker_Concurrent(t *testing.T) {
	t.Run("concurrent allocations", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		const goroutines = 100
		const allocsPerGoroutine = 100

		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < allocsPerGoroutine; j++ {
					_, err := tracker.Allocate(10, "test", "")
					if err != nil {
						t.Errorf("Allocate failed: %v", err)
					}
				}
			}()
		}

		wg.Wait()

		expected := uint64(goroutines * allocsPerGoroutine * 10)
		if tracker.Current() != expected {
			t.Errorf("expected %d, got %d", expected, tracker.Current())
		}

		stats := tracker.Stats()
		if stats.TotalAllocations != goroutines*allocsPerGoroutine {
			t.Errorf("expected %d total allocations, got %d",
				goroutines*allocsPerGoroutine, stats.TotalAllocations)
		}
	})

	t.Run("concurrent alloc and dealloc", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		const goroutines = 50
		const operations = 100

		var wg sync.WaitGroup
		var allocCount atomic.Int64
		var deallocCount atomic.Int64

		// Allocators
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < operations; j++ {
					_, err := tracker.Allocate(10, "test", "")
					if err == nil {
						allocCount.Add(1)
					}
				}
			}()
		}

		// Deallocators (by size)
		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < operations; j++ {
					err := tracker.DeallocateBySize(10, "test")
					if err == nil {
						deallocCount.Add(1)
					}
				}
			}()
		}

		wg.Wait()

		// Verify consistency
		expectedCurrent := (allocCount.Load() - deallocCount.Load()) * 10
		if int64(tracker.Current()) != expectedCurrent {
			t.Errorf("expected current %d, got %d", expectedCurrent, tracker.Current())
		}
	})

	t.Run("concurrent with limit", func(t *testing.T) {
		tracker := NewMemoryTracker(1000)

		const goroutines = 20

		var wg sync.WaitGroup
		var successCount atomic.Int64

		wg.Add(goroutines)
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				_, err := tracker.Allocate(100, "test", "")
				if err == nil {
					successCount.Add(1)
				}
			}()
		}

		wg.Wait()

		// At most 10 allocations should succeed (1000 / 100)
		if successCount.Load() > 10 {
			t.Errorf("expected at most 10 successful allocations, got %d", successCount.Load())
		}

		if tracker.Current() > 1000 {
			t.Errorf("current %d exceeds limit 1000", tracker.Current())
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_ZeroAlloc
// -----------------------------------------------------------------------------

func TestMemoryTracker_ZeroAlloc(t *testing.T) {
	t.Run("zero size allocation", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id, err := tracker.Allocate(0, "test", "zero-alloc")
		if err != nil {
			t.Fatalf("zero-size allocation should succeed: %v", err)
		}

		if tracker.Current() != 0 {
			t.Errorf("current should be 0, got %d", tracker.Current())
		}

		err = tracker.Deallocate(id)
		if err != nil {
			t.Errorf("deallocation of zero-size should succeed: %v", err)
		}
	})

	t.Run("zero size counts as allocation", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(0, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(0, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		stats := tracker.Stats()
		if stats.TotalAllocations != 2 {
			t.Errorf("expected 2 allocations, got %d", stats.TotalAllocations)
		}
		if stats.ActiveAllocations != 2 {
			t.Errorf("expected 2 active allocations, got %d", stats.ActiveAllocations)
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_DoubleDealloc
// -----------------------------------------------------------------------------

func TestMemoryTracker_DoubleDealloc(t *testing.T) {
	t.Run("double deallocation by ID", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id, err := tracker.Allocate(100, "test", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		err = tracker.Deallocate(id)
		if err != nil {
			t.Fatalf("first deallocation should succeed: %v", err)
		}

		err = tracker.Deallocate(id)
		if !errors.Is(err, ErrAllocationNotFound) {
			t.Errorf("second deallocation should return ErrAllocationNotFound, got %v", err)
		}
	})

	t.Run("deallocation of unknown ID", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		err := tracker.Deallocate(999)
		if !errors.Is(err, ErrAllocationNotFound) {
			t.Errorf("expected ErrAllocationNotFound, got %v", err)
		}
	})

	t.Run("deallocate by size not found", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "graph", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		err = tracker.DeallocateBySize(100, "view") // wrong type
		if !errors.Is(err, ErrAllocationNotFound) {
			t.Errorf("expected ErrAllocationNotFound, got %v", err)
		}

		err = tracker.DeallocateBySize(200, "graph") // wrong size
		if !errors.Is(err, ErrAllocationNotFound) {
			t.Errorf("expected ErrAllocationNotFound, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Overflow
// -----------------------------------------------------------------------------

func TestMemoryTracker_Overflow(t *testing.T) {
	t.Run("large allocations", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		// Allocate near max uint64 values (just accounting, not real memory)
		largeSize := uint64(1) << 62 // 4 exabytes

		id, err := tracker.Allocate(largeSize, "test", "")
		if err != nil {
			t.Fatalf("large allocation should succeed: %v", err)
		}

		if tracker.Current() != largeSize {
			t.Errorf("expected current %d, got %d", largeSize, tracker.Current())
		}

		err = tracker.Deallocate(id)
		if err != nil {
			t.Fatalf("deallocation should succeed: %v", err)
		}

		if tracker.Current() != 0 {
			t.Errorf("expected current 0 after dealloc, got %d", tracker.Current())
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Statistics
// -----------------------------------------------------------------------------

func TestMemoryTracker_Statistics(t *testing.T) {
	t.Run("stats accumulation", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		id1, err := tracker.Allocate(100, "graph", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		id2, err := tracker.Allocate(200, "view", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		err = tracker.Deallocate(id1)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}
		_, err = tracker.Allocate(50, "result", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		stats := tracker.Stats()

		if stats.TotalAllocations != 3 {
			t.Errorf("expected 3 total allocations, got %d", stats.TotalAllocations)
		}
		if stats.TotalDeallocations != 1 {
			t.Errorf("expected 1 deallocation, got %d", stats.TotalDeallocations)
		}
		if stats.TotalBytesAlloc != 350 {
			t.Errorf("expected 350 total bytes allocated, got %d", stats.TotalBytesAlloc)
		}
		if stats.TotalBytesDealloc != 100 {
			t.Errorf("expected 100 total bytes deallocated, got %d", stats.TotalBytesDealloc)
		}
		if stats.CurrentBytes != 250 {
			t.Errorf("expected current 250, got %d", stats.CurrentBytes)
		}
		if stats.ActiveAllocations != 2 {
			t.Errorf("expected 2 active, got %d", stats.ActiveAllocations)
		}

		err = tracker.Deallocate(id2)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}
	})

	t.Run("active allocations list", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "graph", "g1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(200, "view", "v1")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(300, "graph", "g2")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		allocs := tracker.ActiveAllocations()
		if len(allocs) != 3 {
			t.Errorf("expected 3 allocations, got %d", len(allocs))
		}

		// Check that it's a copy
		allocs[0].Size = 999
		realAllocs := tracker.ActiveAllocations()
		for _, a := range realAllocs {
			if a.Size == 999 {
				t.Error("ActiveAllocations should return copies")
			}
		}
	})

	t.Run("allocations by type", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		_, err := tracker.Allocate(100, "graph", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(200, "graph", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}
		_, err = tracker.Allocate(300, "view", "")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		count, bytes := tracker.CountByType("graph")
		if count != 2 || bytes != 300 {
			t.Errorf("expected 2 graphs with 300 bytes, got %d with %d", count, bytes)
		}

		count, bytes = tracker.CountByType("view")
		if count != 1 || bytes != 300 {
			t.Errorf("expected 1 view with 300 bytes, got %d with %d", count, bytes)
		}

		count, bytes = tracker.CountByType("result")
		if count != 0 || bytes != 0 {
			t.Errorf("expected 0 results, got %d with %d bytes", count, bytes)
		}
	})
}

// -----------------------------------------------------------------------------
// TestMemoryTracker_Reset
// -----------------------------------------------------------------------------

func TestMemoryTracker_Reset(t *testing.T) {
	tracker := NewMemoryTracker(1000)

	_, err := tracker.Allocate(100, "test", "")
	if err != nil {
		t.Errorf("Allocate failed: %v", err)
	}
	_, err = tracker.Allocate(200, "test", "")
	if err != nil {
		t.Errorf("Allocate failed: %v", err)
	}

	tracker.Reset()

	stats := tracker.Stats()
	if stats.CurrentBytes != 0 {
		t.Errorf("expected current 0 after reset, got %d", stats.CurrentBytes)
	}
	if stats.PeakBytes != 0 {
		t.Errorf("expected peak 0 after reset, got %d", stats.PeakBytes)
	}
	if stats.TotalAllocations != 0 {
		t.Errorf("expected 0 total allocations after reset, got %d", stats.TotalAllocations)
	}
	if stats.ActiveAllocations != 0 {
		t.Errorf("expected 0 active allocations after reset, got %d", stats.ActiveAllocations)
	}

	// Should be able to allocate again with new IDs
	id, err := tracker.Allocate(50, "test", "")
	if err != nil {
		t.Errorf("Allocate failed: %v", err)
	}
	if id != 1 {
		t.Errorf("expected ID 1 after reset, got %d", id)
	}
}

// -----------------------------------------------------------------------------
// TestTrackedGraph
// -----------------------------------------------------------------------------

func TestTrackedGraph(t *testing.T) {
	t.Run("tracked graph lifecycle", func(t *testing.T) {
		tracker := NewMemoryTracker(0)

		// This test requires actual shim to work, so we just test the tracker integration
		// In a real scenario with cgo:
		// tg, err := NewTrackedGraph(tracker, 3, []uint32{0, 1}, []uint32{1, 2}, false)

		// For now, simulate with manual allocation
		id, err := tracker.Allocate(1000, "graph", "test-graph")
		if err != nil {
			t.Errorf("Allocate failed: %v", err)
		}

		if tracker.Current() != 1000 {
			t.Errorf("expected 1000 bytes tracked, got %d", tracker.Current())
		}

		count, _ := tracker.CountByType("graph")
		if count != 1 {
			t.Errorf("expected 1 graph, got %d", count)
		}

		err = tracker.Deallocate(id)
		if err != nil {
			t.Errorf("Deallocate failed: %v", err)
		}

		if tracker.Current() != 0 {
			t.Errorf("expected 0 bytes after close, got %d", tracker.Current())
		}
	})

	t.Run("tracked graph respects limit", func(t *testing.T) {
		tracker := NewMemoryTracker(500)

		_, err := tracker.Allocate(1000, "graph", "")
		if !errors.Is(err, ErrMemoryLimitExceeded) {
			t.Errorf("expected ErrMemoryLimitExceeded, got %v", err)
		}
	})
}

// -----------------------------------------------------------------------------
// TestEstimateGraphMemory
// -----------------------------------------------------------------------------

func TestEstimateGraphMemory(t *testing.T) {
	// Test the estimation function
	estimate := estimateGraphMemory(1000, 5000)

	// Should be roughly: 1000*24 + 5000*8 + 256 = 24000 + 40000 + 256 = 64256
	expected := uint64(1000*24 + 5000*8 + 256)
	if estimate != expected {
		t.Errorf("expected estimate %d, got %d", expected, estimate)
	}

	// Empty graph
	estimate = estimateGraphMemory(0, 0)
	if estimate != 256 { // base overhead
		t.Errorf("expected 256 for empty graph, got %d", estimate)
	}

	// Large graph
	estimate = estimateGraphMemory(200000, 1000000)
	// 200000*24 + 1000000*8 + 256 = 4800000 + 8000000 + 256 = 12800256
	expected = uint64(200000*24 + 1000000*8 + 256)
	if estimate != expected {
		t.Errorf("expected estimate %d for large graph, got %d", expected, estimate)
	}
}

// -----------------------------------------------------------------------------
// TestDefaultTracker
// -----------------------------------------------------------------------------

func TestDefaultTracker(t *testing.T) {
	// Note: This test may affect other tests if run in parallel
	// because it modifies global state

	tracker := DefaultTracker()
	if tracker == nil {
		t.Fatal("DefaultTracker should not return nil")
	}

	// Should return the same instance
	tracker2 := DefaultTracker()
	if tracker != tracker2 {
		t.Error("DefaultTracker should return the same instance")
	}
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkMemoryTracker_Allocate(b *testing.B) {
	tracker := NewMemoryTracker(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := tracker.Allocate(100, "test", "")
		if err != nil {
			b.Errorf("Allocate failed: %v", err)
		}
	}
}

func BenchmarkMemoryTracker_AllocateDealloc(b *testing.B) {
	tracker := NewMemoryTracker(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id, err := tracker.Allocate(100, "test", "")
		if err != nil {
			b.Errorf("Allocate failed: %v", err)
		}
		err = tracker.Deallocate(id)
		if err != nil {
			b.Errorf("Deallocate failed: %v", err)
		}
	}
}

func BenchmarkMemoryTracker_Concurrent(b *testing.B) {
	tracker := NewMemoryTracker(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id, err := tracker.Allocate(100, "test", "")
			if err != nil {
				b.Errorf("Allocate failed: %v", err)
			}
			err = tracker.Deallocate(id)
			if err != nil {
				b.Errorf("Deallocate failed: %v", err)
			}
		}
	})
}

func BenchmarkMemoryTracker_Current(b *testing.B) {
	tracker := NewMemoryTracker(0)
	_, err := tracker.Allocate(1000, "test", "")
	if err != nil {
		b.Errorf("Allocate failed: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.Current()
	}
}
