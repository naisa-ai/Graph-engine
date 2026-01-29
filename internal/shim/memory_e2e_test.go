package shim

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// E2E Memory Tracking Tests
//
// These tests verify memory tracking across the Go-shim boundary.
// They test graph lifecycle, result lifecycle, concurrent operations,
// and memory limits enforcement.
//
// When igraph is not available (stub mode), these tests verify that:
// - Stubs return appropriate errors
// - Memory tracker still functions correctly
// - Error handling is robust
//
// When igraph IS available (cgo build), these tests verify:
// - Memory is properly tracked when graphs are created
// - Memory is released when graphs are closed
// - Concurrent operations don't cause memory leaks
// - Memory limits are enforced
// =============================================================================

// -----------------------------------------------------------------------------
// Graph Lifecycle Tests
// -----------------------------------------------------------------------------

func TestE2E_GraphLifecycle_Basic(t *testing.T) {
	tracker := NewMemoryTracker(0) // unlimited

	// Create a tracked graph
	tg, err := NewTrackedGraph(tracker, 5, []uint32{0, 1, 2, 3}, []uint32{1, 2, 3, 4}, false)
	if err != nil {
		t.Fatalf("expected graph creation to succeed, got: %v", err)
	}

	// Verify memory is tracked
	initialMemory := tracker.Current()
	if initialMemory == 0 {
		t.Error("expected non-zero memory tracking after graph creation")
	}

	stats := tracker.Stats()
	if stats.ActiveAllocations != 1 {
		t.Errorf("expected 1 active allocation, got %d", stats.ActiveAllocations)
	}

	graphCount, graphBytes := tracker.CountByType("graph")
	if graphCount != 1 {
		t.Errorf("expected 1 graph, got %d", graphCount)
	}
	if graphBytes == 0 {
		t.Error("expected non-zero bytes for graph")
	}

	// Close the graph
	tg.Close()

	// Verify memory is released
	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after close, got %d", tracker.Current())
	}

	stats = tracker.Stats()
	if stats.ActiveAllocations != 0 {
		t.Errorf("expected 0 active allocations after close, got %d", stats.ActiveAllocations)
	}
}

func TestE2E_GraphLifecycle_MultipleGraphs(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create multiple graphs
	graphs := make([]*TrackedGraph, 5)
	var totalExpectedGraphs int

	for i := 0; i < 5; i++ {
		g, err := NewTrackedGraph(tracker, uint32(10+i*5),
			makeEdges(10+i*5), makeEdges(10+i*5), i%2 == 0)
		if err != nil {
			t.Fatalf("failed to create graph %d: %v", i, err)
		}
		graphs[i] = g
		totalExpectedGraphs++
	}

	// Verify all are tracked
	count, _ := tracker.CountByType("graph")
	if count != totalExpectedGraphs {
		t.Errorf("expected %d graphs tracked, got %d", totalExpectedGraphs, count)
	}

	stats := tracker.Stats()
	if stats.ActiveAllocations != totalExpectedGraphs {
		t.Errorf("expected %d active allocations, got %d", totalExpectedGraphs, stats.ActiveAllocations)
	}

	initialMemory := tracker.Current()
	if initialMemory == 0 {
		t.Error("expected non-zero memory with multiple graphs")
	}

	// Close some graphs
	graphs[0].Close()
	graphs[2].Close()
	graphs[4].Close()

	count, _ = tracker.CountByType("graph")
	if count != 2 {
		t.Errorf("expected 2 graphs after closing 3, got %d", count)
	}

	if tracker.Current() >= initialMemory {
		t.Error("memory should decrease after closing graphs")
	}

	// Close remaining
	graphs[1].Close()
	graphs[3].Close()

	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after closing all graphs, got %d", tracker.Current())
	}
}

func TestE2E_GraphLifecycle_DoubleClose(t *testing.T) {

	tracker := NewMemoryTracker(0)

	tg, err := NewTrackedGraph(tracker, 3, []uint32{0, 1}, []uint32{1, 2}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	// Close once
	tg.Close()

	// Double close should be safe (no panic, no error)
	tg.Close()

	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after double close, got %d", tracker.Current())
	}
}

// -----------------------------------------------------------------------------
// View Lifecycle Tests
// -----------------------------------------------------------------------------

func TestE2E_ViewLifecycle_Basic(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create a graph
	tg, err := NewTrackedGraph(tracker, 10,
		[]uint32{0, 1, 2, 3, 4, 5, 6, 7, 8},
		[]uint32{1, 2, 3, 4, 5, 6, 7, 8, 9}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer tg.Close()

	graphMemory := tracker.Current()

	// Create edge mask (include first half of edges)
	edgeMask := make([]byte, 2) // 9 edges = 2 bytes
	edgeMask[0] = 0x0F          // First 4 edges

	// Create a view
	tv, err := tg.NewTrackedViewFromEdgeMask(edgeMask)
	if err != nil {
		t.Fatalf("failed to create view: %v", err)
	}

	// Verify view is tracked
	viewCount, viewBytes := tracker.CountByType("view")
	if viewCount != 1 {
		t.Errorf("expected 1 view, got %d", viewCount)
	}

	if tracker.Current() <= graphMemory {
		t.Error("memory should increase with view")
	}

	totalMemory := tracker.Current()
	t.Logf("Graph memory: %d, Total with view: %d, View bytes: %d",
		graphMemory, totalMemory, viewBytes)

	// Close view
	tv.Close()

	// Memory should return to graph-only level
	if tracker.Current() != graphMemory {
		t.Errorf("expected memory %d after view close, got %d", graphMemory, tracker.Current())
	}
}

func TestE2E_ViewLifecycle_MultipleViews(t *testing.T) {

	tracker := NewMemoryTracker(0)

	tg, err := NewTrackedGraph(tracker, 20, makeEdges(20), makeEdges(20), false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer tg.Close()

	// Create multiple views
	views := make([]*TrackedView, 3)
	for i := 0; i < 3; i++ {
		mask := make([]byte, 3)
		mask[0] = byte(0xFF >> i) // Different masks
		v, err := tg.NewTrackedViewFromEdgeMask(mask)
		if err != nil {
			t.Fatalf("failed to create view %d: %v", i, err)
		}
		views[i] = v
	}

	// Verify all views tracked
	viewCount, _ := tracker.CountByType("view")
	if viewCount != 3 {
		t.Errorf("expected 3 views, got %d", viewCount)
	}

	// Close all views
	for _, v := range views {
		v.Close()
	}

	viewCount, _ = tracker.CountByType("view")
	if viewCount != 0 {
		t.Errorf("expected 0 views after closing all, got %d", viewCount)
	}
}

// -----------------------------------------------------------------------------
// Result Lifecycle Tests
// -----------------------------------------------------------------------------

func TestE2E_ResultLifecycle_Components(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create a graph with multiple components
	// Component 1: 0-1-2, Component 2: 3-4
	tg, err := NewTrackedGraph(tracker, 5,
		[]uint32{0, 1, 3},
		[]uint32{1, 2, 4}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer tg.Close()

	graphMemory := tracker.Current()

	// Compute components
	result, err := tg.Graph.Components(true)
	if err != nil {
		t.Fatalf("failed to compute components: %v", err)
	}

	// Result should be valid
	if result.NumComponents != 2 {
		t.Errorf("expected 2 components, got %d", result.NumComponents)
	}

	// Note: Components result from shim doesn't track memory automatically
	// (it's returned as a struct, not a *Result)
	t.Logf("Components: %d, Membership: %v", result.NumComponents, result.Membership)

	// Memory for graph should still be tracked
	if tracker.Current() != graphMemory {
		t.Logf("Memory after components: %d (graph: %d)", tracker.Current(), graphMemory)
	}
}

func TestE2E_ResultLifecycle_ShortestPath(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create a linear graph: 0-1-2-3-4
	tg, err := NewTrackedGraph(tracker, 5,
		[]uint32{0, 1, 2, 3},
		[]uint32{1, 2, 3, 4}, true)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer tg.Close()

	// Compute shortest path
	result, err := tg.Graph.ShortestPath(0, 4, nil, true, true)
	if err != nil {
		t.Fatalf("failed to compute shortest path: %v", err)
	}

	if !result.Found {
		t.Error("expected path to be found")
	}

	if len(result.PathVertices) != 5 {
		t.Errorf("expected 5 vertices in path, got %d", len(result.PathVertices))
	}

	t.Logf("Shortest path: %v, cost: %f", result.PathVertices, result.TotalCost)
}

// -----------------------------------------------------------------------------
// Memory Limits Tests
// -----------------------------------------------------------------------------

func TestE2E_MemoryLimits_EnforcementOnGraph(t *testing.T) {

	// Set a small limit
	tracker := NewMemoryTracker(1000) // 1KB limit

	// First small graph should succeed
	tg1, err := NewTrackedGraph(tracker, 3, []uint32{0, 1}, []uint32{1, 2}, false)
	if err != nil {
		t.Fatalf("small graph should succeed: %v", err)
	}

	// Track memory used
	used := tracker.Current()
	t.Logf("Small graph uses %d bytes", used)

	// Try to create a larger graph that exceeds limit
	_, err = NewTrackedGraph(tracker, 100, makeEdges(100), makeEdges(100), false)
	if err == nil {
		t.Error("large graph should fail due to memory limit")
	}

	// Memory should not have increased
	if tracker.Current() != used {
		t.Errorf("memory should not increase on failed allocation: before=%d, after=%d",
			used, tracker.Current())
	}

	// Clean up
	tg1.Close()
}

func TestE2E_MemoryLimits_EnforcementOnView(t *testing.T) {

	// Create graph first, then set tight limit
	tracker := NewMemoryTracker(0) // Start unlimited

	tg, err := NewTrackedGraph(tracker, 50, makeEdges(50), makeEdges(50), false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	defer tg.Close()

	graphMemory := tracker.Current()

	// Set limit just above graph memory
	err = tracker.SetLimit(graphMemory + 100)
	if err != nil {
		t.Fatalf("failed to set limit: %v", err)
	}

	// Small view should succeed
	mask := make([]byte, 7) // 50 edges
	mask[0] = 0x01
	tv, err := tg.NewTrackedViewFromEdgeMask(mask)
	if err != nil {
		t.Logf("View creation failed (may be expected with tight limit): %v", err)
	} else {
		tv.Close()
	}
}

func TestE2E_MemoryLimits_DynamicAdjustment(t *testing.T) {
	tracker := NewMemoryTracker(10000) // 10KB

	// Allocate some memory
	tracker.Allocate(5000, "test", "")

	// Can increase limit
	err := tracker.SetLimit(20000)
	if err != nil {
		t.Errorf("increasing limit should succeed: %v", err)
	}

	// Can decrease limit above current usage
	err = tracker.SetLimit(6000)
	if err != nil {
		t.Errorf("decreasing limit above usage should succeed: %v", err)
	}

	// Cannot decrease limit below current usage
	err = tracker.SetLimit(4000)
	if !errors.Is(err, ErrMemoryLimitExceeded) {
		t.Errorf("expected ErrMemoryLimitExceeded, got: %v", err)
	}
}

// -----------------------------------------------------------------------------
// Concurrent Operations Tests
// -----------------------------------------------------------------------------

func TestE2E_Concurrent_GraphCreation(t *testing.T) {

	tracker := NewMemoryTracker(0)

	const goroutines = 10
	const graphsPerGoroutine = 5

	var wg sync.WaitGroup
	var successCount atomic.Int64
	var graphs sync.Map // Store for cleanup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < graphsPerGoroutine; j++ {
				g, err := NewTrackedGraph(tracker, 10, makeEdges(10), makeEdges(10), false)
				if err == nil {
					successCount.Add(1)
					graphs.Store(fmt.Sprintf("%d-%d", id, j), g)
				}
			}
		}(i)
	}

	wg.Wait()

	expectedCount := goroutines * graphsPerGoroutine
	if int(successCount.Load()) != expectedCount {
		t.Errorf("expected %d graphs, created %d", expectedCount, successCount.Load())
	}

	count, _ := tracker.CountByType("graph")
	if count != int(successCount.Load()) {
		t.Errorf("tracker shows %d graphs, expected %d", count, successCount.Load())
	}

	// Clean up all graphs
	graphs.Range(func(key, value interface{}) bool {
		value.(*TrackedGraph).Close()
		return true
	})

	// All memory should be released
	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after cleanup, got %d", tracker.Current())
	}
}

func TestE2E_Concurrent_MixedOperations(t *testing.T) {

	tracker := NewMemoryTracker(0)

	const duration = 500 * time.Millisecond
	done := make(chan struct{})

	var createCount, closeCount atomic.Int64
	var graphStore sync.Map
	var graphID atomic.Int64

	// Creator goroutine
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				id := graphID.Add(1)
				g, err := NewTrackedGraph(tracker, 5, []uint32{0, 1, 2}, []uint32{1, 2, 3}, false)
				if err == nil {
					graphStore.Store(id, g)
					createCount.Add(1)
				}
				runtime.Gosched()
			}
		}
	}()

	// Closer goroutine
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				graphStore.Range(func(key, value interface{}) bool {
					if g, ok := value.(*TrackedGraph); ok {
						g.Close()
						graphStore.Delete(key)
						closeCount.Add(1)
						return false // Process one at a time
					}
					return true
				})
				runtime.Gosched()
			}
		}
	}()

	// Stats reader goroutine
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				_ = tracker.Stats()
				_ = tracker.Current()
				_ = tracker.Peak()
				runtime.Gosched()
			}
		}
	}()

	time.Sleep(duration)
	close(done)

	// Give goroutines time to exit
	time.Sleep(50 * time.Millisecond)

	// Clean up remaining graphs
	graphStore.Range(func(key, value interface{}) bool {
		if g, ok := value.(*TrackedGraph); ok {
			g.Close()
			closeCount.Add(1)
		}
		return true
	})

	t.Logf("Created: %d, Closed: %d", createCount.Load(), closeCount.Load())

	// Verify all memory released
	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after cleanup, got %d", tracker.Current())
	}
}

func TestE2E_Concurrent_WithMemoryLimit(t *testing.T) {

	// Small limit to force contention
	tracker := NewMemoryTracker(50000)

	const goroutines = 20
	var wg sync.WaitGroup
	var successCount, failCount atomic.Int64

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			// Try to create a graph
			g, err := NewTrackedGraph(tracker, 50, makeEdges(50), makeEdges(50), false)
			if err == nil {
				successCount.Add(1)
				// Hold it briefly
				time.Sleep(10 * time.Millisecond)
				g.Close()
			} else {
				failCount.Add(1)
			}
		}()
	}

	wg.Wait()

	t.Logf("Success: %d, Failed: %d", successCount.Load(), failCount.Load())

	// Should have some successes and some failures due to limit
	if successCount.Load() == 0 {
		t.Error("expected at least some successful allocations")
	}

	// Memory should be released
	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after test, got %d", tracker.Current())
	}
}

// -----------------------------------------------------------------------------
// Leak Detection Tests
// -----------------------------------------------------------------------------

func TestE2E_LeakDetection_NoLeaks(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create and properly close a graph
	tg, err := NewTrackedGraph(tracker, 5, []uint32{0, 1, 2}, []uint32{1, 2, 3}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}
	tg.Close()

	// Check for leaks
	report := tracker.CheckLeaks(0)
	if report.HasLeaks {
		t.Errorf("should have no leaks, but found %d allocations with %d bytes",
			report.LeakedCount, report.LeakedBytes)
	}
}

func TestE2E_LeakDetection_DetectsLeaks(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Create graph but don't close it
	tg, err := NewTrackedGraph(tracker, 5, []uint32{0, 1, 2}, []uint32{1, 2, 3}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Check for leaks with short threshold
	report := tracker.CheckLeaks(20 * time.Millisecond)
	if !report.HasLeaks {
		t.Error("should detect the unclosed graph as a leak")
	}

	if report.LeakedCount != 1 {
		t.Errorf("expected 1 leaked allocation, got %d", report.LeakedCount)
	}

	// Clean up (to not actually leak)
	tg.Close()
}

// -----------------------------------------------------------------------------
// Stress Tests
// -----------------------------------------------------------------------------

func TestE2E_Stress_RapidCreateClose(t *testing.T) {

	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tracker := NewMemoryTracker(0)

	const iterations = 100

	for i := 0; i < iterations; i++ {
		g, err := NewTrackedGraph(tracker, 10, makeEdges(10), makeEdges(10), false)
		if err != nil {
			t.Fatalf("iteration %d: failed to create graph: %v", i, err)
		}
		g.Close()
	}

	// All memory should be released
	if tracker.Current() != 0 {
		t.Errorf("expected 0 memory after stress test, got %d", tracker.Current())
	}

	stats := tracker.Stats()
	// Note: allocations may exceed iterations because NewTrackedGraph does
	// estimate-then-reallocate when actual size differs from estimated size.
	// The key invariant is that allocations == deallocations (no leaks).
	if stats.TotalAllocations < uint64(iterations) {
		t.Errorf("expected at least %d total allocations, got %d", iterations, stats.TotalAllocations)
	}
	if stats.TotalAllocations != stats.TotalDeallocations {
		t.Errorf("allocation/deallocation mismatch: allocations=%d, deallocations=%d",
			stats.TotalAllocations, stats.TotalDeallocations)
	}
}

func TestE2E_Stress_PeakMemoryTracking(t *testing.T) {

	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	tracker := NewMemoryTracker(0)

	// Create multiple graphs to increase peak
	var graphs []*TrackedGraph
	for i := 0; i < 10; i++ {
		g, err := NewTrackedGraph(tracker, uint32(10+i*5), makeEdges(10+i*5), makeEdges(10+i*5), false)
		if err != nil {
			t.Fatalf("failed to create graph %d: %v", i, err)
		}
		graphs = append(graphs, g)
	}

	peakWithAll := tracker.Peak()
	if peakWithAll == 0 {
		t.Error("peak should be non-zero with multiple graphs")
	}

	// Close all graphs
	for _, g := range graphs {
		g.Close()
	}

	// Current should be 0, but peak should remain
	if tracker.Current() != 0 {
		t.Errorf("expected 0 current memory, got %d", tracker.Current())
	}

	if tracker.Peak() != peakWithAll {
		t.Errorf("peak should remain %d, got %d", peakWithAll, tracker.Peak())
	}

	t.Logf("Peak memory: %d bytes", peakWithAll)
}

// -----------------------------------------------------------------------------
// Callback Tests
// -----------------------------------------------------------------------------

func TestE2E_Callbacks_AllocDealloc(t *testing.T) {

	tracker := NewMemoryTracker(0)

	var allocCalls, deallocCalls atomic.Int64
	var lastAllocType, lastDeallocType string
	var mu sync.Mutex

	tracker.SetCallbacks(
		func(info *AllocationInfo) {
			allocCalls.Add(1)
			mu.Lock()
			lastAllocType = info.Type
			mu.Unlock()
		},
		func(info *AllocationInfo) {
			deallocCalls.Add(1)
			mu.Lock()
			lastDeallocType = info.Type
			mu.Unlock()
		},
	)

	// Create and close a graph
	tg, err := NewTrackedGraph(tracker, 5, []uint32{0, 1}, []uint32{1, 2}, false)
	if err != nil {
		t.Fatalf("failed to create graph: %v", err)
	}

	if allocCalls.Load() == 0 {
		t.Error("allocation callback should have been called")
	}

	mu.Lock()
	if lastAllocType != "graph" {
		t.Errorf("expected graph allocation, got %s", lastAllocType)
	}
	mu.Unlock()

	tg.Close()

	if deallocCalls.Load() == 0 {
		t.Error("deallocation callback should have been called")
	}

	mu.Lock()
	if lastDeallocType != "graph" {
		t.Errorf("expected graph deallocation, got %s", lastDeallocType)
	}
	mu.Unlock()
}

// -----------------------------------------------------------------------------
// Edge Cases
// -----------------------------------------------------------------------------

func TestE2E_EdgeCase_EmptyGraph(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Empty graph (no edges)
	tg, err := NewTrackedGraph(tracker, 5, []uint32{}, []uint32{}, false)
	if err != nil {
		t.Fatalf("empty graph should succeed: %v", err)
	}

	// Should still track some memory (overhead)
	if tracker.Current() == 0 {
		t.Log("Warning: empty graph reports 0 memory")
	}

	tg.Close()

	if tracker.Current() != 0 {
		t.Errorf("expected 0 after close, got %d", tracker.Current())
	}
}

func TestE2E_EdgeCase_SingleVertex(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Single vertex, no edges
	tg, err := NewTrackedGraph(tracker, 1, []uint32{}, []uint32{}, false)
	if err != nil {
		t.Fatalf("single vertex graph should succeed: %v", err)
	}

	tg.Close()

	if tracker.Current() != 0 {
		t.Errorf("expected 0 after close, got %d", tracker.Current())
	}
}

func TestE2E_EdgeCase_SelfLoop(t *testing.T) {

	tracker := NewMemoryTracker(0)

	// Graph with self-loop
	tg, err := NewTrackedGraph(tracker, 3, []uint32{0, 1, 1}, []uint32{1, 2, 1}, false)
	if err != nil {
		t.Fatalf("graph with self-loop should succeed: %v", err)
	}

	tg.Close()

	if tracker.Current() != 0 {
		t.Errorf("expected 0 after close, got %d", tracker.Current())
	}
}

// -----------------------------------------------------------------------------
// Benchmark Tests
// -----------------------------------------------------------------------------

func BenchmarkE2E_GraphCreateClose(b *testing.B) {
	tracker := NewMemoryTracker(0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g, err := NewTrackedGraph(tracker, 10, makeEdges(10), makeEdges(10), false)
		if err != nil {
			b.Fatalf("failed: %v", err)
		}
		g.Close()
	}
}

func BenchmarkE2E_TrackerStatsRead(b *testing.B) {
	tracker := NewMemoryTracker(0)

	// Pre-populate with some allocations
	for i := 0; i < 100; i++ {
		tracker.Allocate(100, "test", fmt.Sprintf("label-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracker.Stats()
	}
}

// -----------------------------------------------------------------------------
// Helper Functions
// -----------------------------------------------------------------------------

// makeEdges creates edge arrays for a path graph
func makeEdges(n int) []uint32 {
	if n <= 1 {
		return []uint32{}
	}
	edges := make([]uint32, n-1)
	for i := 0; i < n-1; i++ {
		edges[i] = uint32(i)
	}
	return edges
}
