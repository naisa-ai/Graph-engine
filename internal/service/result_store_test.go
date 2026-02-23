package service

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// createTestAlgoResult creates a minimal AlgoResult for testing.
func createTestAlgoResult(versionID string, algoKind AlgoKind, membershipSize int) *AlgoResult {
	result := NewAlgoResult(versionID, algoKind, HashParams(algoKind, "test"))
	result.MembershipU32 = make([]uint32, membershipSize)
	for i := 0; i < membershipSize; i++ {
		result.MembershipU32[i] = uint32(i % 10)
	}
	// Force memory calculation
	_ = result.EstimateMemory()
	return result
}

func TestResultStore_Store_Basic(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour) // No memory limit
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)

	// Store should succeed
	err := rs.Store(result)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Verify count
	if rs.Count() != 1 {
		t.Errorf("expected count 1, got %d", rs.Count())
	}

	// Verify memory tracking
	if rs.TotalMemory() != result.EstimateMemory() {
		t.Errorf("expected memory %d, got %d", result.EstimateMemory(), rs.TotalMemory())
	}
}

func TestResultStore_Store_Duplicate(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	result1 := createTestAlgoResult("v1", AlgoKindComponents, 100)
	result2 := &AlgoResult{ID: result1.ID} // Same ID

	// First store should succeed
	if err := rs.Store(result1); err != nil {
		t.Fatalf("First store failed: %v", err)
	}

	// Second store with same ID should fail
	err := rs.Store(result2)
	if err == nil {
		t.Error("expected error for duplicate ID, got nil")
	}
}

func TestResultStore_Get(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)
	_ = rs.Store(result)

	// Get should work and pin the result
	retrieved, err := rs.Get(result.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != result.ID {
		t.Errorf("expected ID %s, got %s", result.ID, retrieved.ID)
	}

	// Should be pinned
	if retrieved.RefCount() != 1 {
		t.Errorf("expected refcount 1, got %d", retrieved.RefCount())
	}

	// Unpin
	retrieved.Unpin()
}

func TestResultStore_GetNotFound(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	_, err := rs.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent result")
	}
}

func TestResultStore_CacheKey(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	versionID := "v1"
	algoKind := AlgoKindComponents
	paramsHash := HashParams(algoKind, "test")

	result := NewAlgoResult(versionID, algoKind, paramsHash)
	result.MembershipU32 = make([]uint32, 100)
	_ = rs.Store(result)

	// Get by cache key should work
	retrieved, found := rs.GetByKey(versionID, algoKind, paramsHash)
	if !found {
		t.Fatal("GetByKey returned false")
	}
	if retrieved.ID != result.ID {
		t.Errorf("expected ID %s, got %s", result.ID, retrieved.ID)
	}

	// Unpin
	retrieved.Unpin()

	// Different params should not find
	_, found = rs.GetByKey(versionID, algoKind, "different-hash")
	if found {
		t.Error("expected not found for different params")
	}

	// Different version should not find
	_, found = rs.GetByKey("v2", algoKind, paramsHash)
	if found {
		t.Error("expected not found for different version")
	}
}

func TestResultStore_LRUEviction_MaxItems(t *testing.T) {
	// Create store with max 5 items
	rs := NewResultStore(5, 0, time.Hour)
	defer rs.Close()

	// Add 5 items
	for i := 0; i < 5; i++ {
		result := createTestAlgoResult(fmt.Sprintf("v%d", i), AlgoKindComponents, 10)
		if err := rs.Store(result); err != nil {
			t.Fatalf("Store %d failed: %v", i, err)
		}
		// Small delay to ensure different access times
		time.Sleep(time.Millisecond)
	}

	if rs.Count() != 5 {
		t.Errorf("expected count 5, got %d", rs.Count())
	}

	// Access item 0 to make it most recently used
	result0, _ := rs.GetByKey("v0", AlgoKindComponents, HashParams(AlgoKindComponents, "test"))
	if result0 != nil {
		result0.Unpin()
	}

	// Add 6th item - should evict LRU (one of v1-v4, not v0)
	result6 := createTestAlgoResult("v5", AlgoKindComponents, 10)
	if err := rs.Store(result6); err != nil {
		t.Fatalf("Store 6th failed: %v", err)
	}

	// Count should still be 5
	if rs.Count() != 5 {
		t.Errorf("expected count 5 after eviction, got %d", rs.Count())
	}

	// v0 should still exist (was accessed recently)
	_, found := rs.GetByKey("v0", AlgoKindComponents, HashParams(AlgoKindComponents, "test"))
	if !found {
		t.Error("v0 should still exist (was recently accessed)")
	}
}

func TestResultStore_MemoryEviction(t *testing.T) {
	// Create results with known memory size
	result := createTestAlgoResult("test", AlgoKindComponents, 1000)
	memPerResult := result.EstimateMemory()

	// Set memory limit to allow ~3 results
	memoryLimit := memPerResult*3 + 100
	rs := NewResultStore(100, memoryLimit, time.Hour)
	defer rs.Close()

	// Add 3 results - should succeed
	for i := 0; i < 3; i++ {
		r := createTestAlgoResult(fmt.Sprintf("v%d", i), AlgoKindComponents, 1000)
		if err := rs.Store(r); err != nil {
			t.Fatalf("Store %d failed: %v", i, err)
		}
		time.Sleep(time.Millisecond) // Ensure different access times
	}

	if rs.Count() != 3 {
		t.Errorf("expected count 3, got %d", rs.Count())
	}

	// Add 4th result - should trigger eviction
	r4 := createTestAlgoResult("v3", AlgoKindComponents, 1000)
	if err := rs.Store(r4); err != nil {
		t.Fatalf("Store 4th failed: %v", err)
	}

	// Count should still be 3 (one evicted)
	if rs.Count() > 3 {
		t.Errorf("expected count <= 3 after eviction, got %d", rs.Count())
	}

	// Total memory should be within limit
	if rs.TotalMemory() > memoryLimit {
		t.Errorf("total memory %d exceeds limit %d", rs.TotalMemory(), memoryLimit)
	}
}

func TestResultStore_TTLCleanup(t *testing.T) {
	// Very short TTL for testing
	rs := NewResultStore(100, 0, 10*time.Millisecond)
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)
	_ = rs.Store(result)

	// Result should exist initially
	if rs.Count() != 1 {
		t.Errorf("expected count 1, got %d", rs.Count())
	}

	// Wait for TTL + cleanup interval
	time.Sleep(50 * time.Millisecond)

	// Manually trigger cleanup (normally runs in background)
	rs.cleanup()

	// Result should be gone
	if rs.Count() != 0 {
		t.Errorf("expected count 0 after TTL, got %d", rs.Count())
	}
}

func TestResultStore_PinnedNotEvicted(t *testing.T) {
	// Store with max 3 items to allow room for a 3rd
	rs := NewResultStore(3, 0, time.Hour)
	defer rs.Close()

	// Add 2 results
	r1 := createTestAlgoResult("v1", AlgoKindComponents, 100)
	r2 := createTestAlgoResult("v2", AlgoKindComponents, 100)
	_ = rs.Store(r1)
	time.Sleep(time.Millisecond)
	_ = rs.Store(r2)

	// Pin r1 (the older one)
	pinned1, _ := rs.Get(r1.ID)

	// Add 3rd result - this fills up
	r3 := createTestAlgoResult("v3", AlgoKindComponents, 100)
	_ = rs.Store(r3)
	time.Sleep(time.Millisecond)

	// Now add 4th - should evict r2 (unpinned, older than r3)
	r4 := createTestAlgoResult("v4", AlgoKindComponents, 100)
	_ = rs.Store(r4)

	// r1 should still exist (pinned)
	retrieved1, err := rs.Get(r1.ID)
	if err != nil {
		t.Error("pinned r1 was evicted")
	} else {
		retrieved1.Unpin()
	}

	// Cleanup
	pinned1.Unpin()
}

func TestResultStore_PinnedNotCleanedByTTL(t *testing.T) {
	rs := NewResultStore(100, 0, 10*time.Millisecond)
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)
	_ = rs.Store(result)

	// Pin the result
	pinned, _ := rs.Get(result.ID)

	// Wait for TTL
	time.Sleep(50 * time.Millisecond)

	// Trigger cleanup
	rs.cleanup()

	// Result should still exist (pinned)
	if rs.Count() != 1 {
		t.Error("pinned result was cleaned up")
	}

	// Unpin
	pinned.Unpin()

	// Now cleanup should work
	rs.cleanup()
	if rs.Count() != 0 {
		t.Errorf("expected count 0 after unpin and cleanup, got %d", rs.Count())
	}
}

func TestResultStore_ConcurrentAccess(t *testing.T) {
	rs := NewResultStore(1000, 0, time.Hour)
	defer rs.Close()

	// Pre-populate
	for i := 0; i < 10; i++ {
		result := createTestAlgoResult(fmt.Sprintf("v%d", i), AlgoKindComponents, 100)
		_ = rs.Store(result)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent readers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				versionID := fmt.Sprintf("v%d", id%10)
				result, found := rs.GetByKey(versionID, AlgoKindComponents, HashParams(AlgoKindComponents, "test"))
				if found {
					_ = result.MembershipU32
					result.Unpin()
				}
			}
		}(i)
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				result := createTestAlgoResult(fmt.Sprintf("new-%d-%d", id, j), AlgoKindCommunities, 50)
				if err := rs.Store(result); err != nil {
					// Duplicate errors are acceptable
					if err.Error() != fmt.Sprintf("result already exists: %s", result.ID) {
						errors <- err
					}
				}
			}
		}(i)
	}

	// Concurrent stats readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = rs.Stats()
				_ = rs.TotalMemory()
				_ = rs.Count()
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
}

func TestResultStore_Stats(t *testing.T) {
	memLimit := uint64(1000000)
	rs := NewResultStore(50, memLimit, time.Hour)
	defer rs.Close()

	// Initially empty
	stats := rs.Stats()
	if stats.TotalItems != 0 {
		t.Errorf("expected 0 items, got %d", stats.TotalItems)
	}
	if stats.TotalMemory != 0 {
		t.Errorf("expected 0 memory, got %d", stats.TotalMemory)
	}
	if stats.MaxItems != 50 {
		t.Errorf("expected max items 50, got %d", stats.MaxItems)
	}
	if stats.MaxMemory != memLimit {
		t.Errorf("expected max memory %d, got %d", memLimit, stats.MaxMemory)
	}

	// Add results
	r1 := createTestAlgoResult("v1", AlgoKindComponents, 100)
	r2 := createTestAlgoResult("v2", AlgoKindShortestPath, 200)
	_ = rs.Store(r1)
	_ = rs.Store(r2)

	stats = rs.Stats()
	if stats.TotalItems != 2 {
		t.Errorf("expected 2 items, got %d", stats.TotalItems)
	}
	expectedMem := r1.EstimateMemory() + r2.EstimateMemory()
	if stats.TotalMemory != expectedMem {
		t.Errorf("expected memory %d, got %d", expectedMem, stats.TotalMemory)
	}
}

func TestResultStore_Delete(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)
	_ = rs.Store(result)

	memBefore := rs.TotalMemory()

	// Delete should succeed
	err := rs.Delete(result.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Result should be gone
	if rs.Count() != 0 {
		t.Errorf("expected count 0, got %d", rs.Count())
	}

	// Memory should be freed
	if rs.TotalMemory() != memBefore-result.EstimateMemory() {
		t.Error("memory not freed properly")
	}

	// Cache key should also be removed
	_, found := rs.GetByKey("v1", AlgoKindComponents, result.ParamsHash)
	if found {
		t.Error("cache key should be removed after delete")
	}
}

func TestResultStore_Delete_Pinned(t *testing.T) {
	rs := NewResultStore(100, 0, time.Hour)
	defer rs.Close()

	result := createTestAlgoResult("v1", AlgoKindComponents, 100)
	_ = rs.Store(result)

	// Pin the result
	pinned, _ := rs.Get(result.ID)

	// Delete should fail
	err := rs.Delete(result.ID)
	if err == nil {
		t.Error("expected error deleting pinned result")
	}

	// Unpin
	pinned.Unpin()

	// Now delete should work
	err = rs.Delete(result.ID)
	if err != nil {
		t.Fatalf("Delete after unpin failed: %v", err)
	}
}

func TestHashParams(t *testing.T) {
	// Same params should give same hash
	h1 := HashParams("algo", "param1", 123)
	h2 := HashParams("algo", "param1", 123)
	if h1 != h2 {
		t.Errorf("same params gave different hashes: %s vs %s", h1, h2)
	}

	// Different params should give different hash
	h3 := HashParams("algo", "param2", 123)
	if h1 == h3 {
		t.Error("different params gave same hash")
	}

	// Hash should be consistent length (16 chars from our implementation)
	if len(h1) != 16 {
		t.Errorf("expected hash length 16, got %d", len(h1))
	}
}

func TestAlgoResult_RefCounting(t *testing.T) {
	result := createTestAlgoResult("v1", AlgoKindComponents, 100)

	// Initially 0
	if result.RefCount() != 0 {
		t.Errorf("expected initial refcount 0, got %d", result.RefCount())
	}

	// Pin increments
	result.Pin()
	if result.RefCount() != 1 {
		t.Errorf("expected refcount 1 after pin, got %d", result.RefCount())
	}

	result.Pin()
	if result.RefCount() != 2 {
		t.Errorf("expected refcount 2 after second pin, got %d", result.RefCount())
	}

	// Unpin decrements
	canClean := result.Unpin()
	if canClean {
		t.Error("should not be cleanable with refcount > 0")
	}
	if result.RefCount() != 1 {
		t.Errorf("expected refcount 1 after unpin, got %d", result.RefCount())
	}

	canClean = result.Unpin()
	if !canClean {
		t.Error("should be cleanable with refcount 0")
	}
}

func TestAlgoResult_EstimateMemory(t *testing.T) {
	// Result with only membership data
	r1 := &AlgoResult{
		MembershipU32: make([]uint32, 1000),
	}
	mem1 := r1.EstimateMemory()
	if mem1 < 4000 { // At least 4 bytes per uint32
		t.Errorf("memory too low for membership: %d", mem1)
	}

	// Result with path data
	r2 := &AlgoResult{
		PathVertices: make([]uint64, 500),
		PathEdges:    make([]uint64, 500),
	}
	mem2 := r2.EstimateMemory()
	if mem2 < 8000 { // At least 8 bytes per uint64 * 2 arrays
		t.Errorf("memory too low for path data: %d", mem2)
	}

	// Memory should be cached
	mem1Again := r1.EstimateMemory()
	if mem1 != mem1Again {
		t.Error("memory estimation not cached")
	}
}

func TestAlgoResult_CacheKey(t *testing.T) {
	result := NewAlgoResult("v1", AlgoKindComponents, "abc123")

	key := result.CacheKey()
	expected := "v1:components:abc123"
	if key != expected {
		t.Errorf("expected cache key %s, got %s", expected, key)
	}
}
