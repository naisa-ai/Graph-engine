package service

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// createTestGraphVersion creates a minimal GraphVersion for testing.
func createTestGraphVersion(id string, numNodes, numEdges int) *GraphVersion {
	gv := &GraphVersion{
		ID:            id,
		GraphName:     "test-graph",
		Directed:      false,
		PublishedAt:   time.Now(),
		nodeIDToIndex: make(map[uint64]uint32),
		indexToNodeID: make([]uint64, numNodes),
		EdgeSrc:       make([]uint32, numEdges),
		EdgeDst:       make([]uint32, numEdges),
		Labels:        make(map[string]string),
		VCount:        uint64(numNodes),
		ECount:        uint64(numEdges),
	}

	// Build node mapping
	for i := 0; i < numNodes; i++ {
		nodeID := uint64(i + 1)
		gv.nodeIDToIndex[nodeID] = uint32(i)
		gv.indexToNodeID[i] = nodeID
	}

	// Build edges (simple chain)
	for i := 0; i < numEdges; i++ {
		gv.EdgeSrc[i] = uint32(i % numNodes)
		gv.EdgeDst[i] = uint32((i + 1) % numNodes)
	}

	// Calculate memory
	gv.memoryBytes = gv.estimateMemory()

	return gv
}

func TestVersionStore_Store_Basic(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0) // No memory limit

	gv := createTestGraphVersion("v1", 10, 5)

	// Store should succeed
	err := vs.Store(gv)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Verify count
	if vs.Count() != 1 {
		t.Errorf("expected count 1, got %d", vs.Count())
	}

	// Verify total memory is tracked
	if vs.TotalMemory() != gv.EstimateMemory() {
		t.Errorf("expected memory %d, got %d", gv.EstimateMemory(), vs.TotalMemory())
	}

	// Retrieve should work
	retrieved, err := vs.Get("v1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.ID != "v1" {
		t.Errorf("expected ID v1, got %s", retrieved.ID)
	}

	// Don't forget to unpin
	retrieved.Unpin()
}

func TestVersionStore_Store_Duplicate(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	gv1 := createTestGraphVersion("v1", 10, 5)
	gv2 := createTestGraphVersion("v1", 20, 10) // Same ID

	// First store should succeed
	if err := vs.Store(gv1); err != nil {
		t.Fatalf("First store failed: %v", err)
	}

	// Second store with same ID should fail
	err := vs.Store(gv2)
	if err == nil {
		t.Error("expected error for duplicate ID, got nil")
	}

	// Count should still be 1
	if vs.Count() != 1 {
		t.Errorf("expected count 1, got %d", vs.Count())
	}
}

func TestVersionStore_MemoryLimit(t *testing.T) {
	// Create version with known memory size
	gv1 := createTestGraphVersion("v1", 100, 50)
	memoryPerVersion := gv1.EstimateMemory()

	// Set limit to allow only 2 versions
	memoryLimit := memoryPerVersion*2 + 100 // Small buffer
	vs := NewVersionStore(time.Hour, memoryLimit)

	// Store first version - should succeed
	if err := vs.Store(gv1); err != nil {
		t.Fatalf("Store v1 failed: %v", err)
	}

	// Store second version - should succeed
	gv2 := createTestGraphVersion("v2", 100, 50)
	if err := vs.Store(gv2); err != nil {
		t.Fatalf("Store v2 failed: %v", err)
	}

	// Store third version - should fail due to memory limit
	gv3 := createTestGraphVersion("v3", 100, 50)
	err := vs.Store(gv3)
	if err == nil {
		t.Error("expected memory limit error, got nil")
	}

	// Verify count
	if vs.Count() != 2 {
		t.Errorf("expected count 2, got %d", vs.Count())
	}

	// Verify memory tracking
	if vs.TotalMemory() > memoryLimit {
		t.Errorf("total memory %d exceeds limit %d", vs.TotalMemory(), memoryLimit)
	}
}

func TestVersionStore_MemoryLimit_Zero(t *testing.T) {
	// Zero memory limit means no limit
	vs := NewVersionStore(time.Hour, 0)

	// Should be able to store many versions
	for i := 0; i < 100; i++ {
		gv := createTestGraphVersion(fmt.Sprintf("v%d", i), 100, 50)
		if err := vs.Store(gv); err != nil {
			t.Fatalf("Store v%d failed: %v", i, err)
		}
	}

	if vs.Count() != 100 {
		t.Errorf("expected count 100, got %d", vs.Count())
	}
}

func TestVersionStore_MarkForCleanup(t *testing.T) {
	// Use longer TTL to avoid background cleanup interference
	vs := NewVersionStore(100*time.Millisecond, 0)

	gv := createTestGraphVersion("v1", 10, 5)
	memoryBefore := gv.EstimateMemory()
	if err := vs.Store(gv); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Verify version exists
	if vs.Count() != 1 {
		t.Errorf("expected count 1, got %d", vs.Count())
	}

	// Mark for cleanup
	vs.MarkForCleanup("v1")

	// Verify it's marked
	stats := vs.Stats()
	if stats.MarkedForCleanup != 1 {
		t.Errorf("expected 1 marked for cleanup, got %d", stats.MarkedForCleanup)
	}

	// Version should still exist (TTL not passed)
	if vs.Count() != 1 {
		t.Errorf("expected count 1 before TTL, got %d", vs.Count())
	}

	// Verify memory is tracked
	if vs.TotalMemory() != memoryBefore {
		t.Errorf("expected memory %d, got %d", memoryBefore, vs.TotalMemory())
	}

	// Wait for TTL to pass
	time.Sleep(150 * time.Millisecond)

	// Manual cleanup (might return 0 if background goroutine already cleaned)
	vs.Cleanup()

	// Version should be gone (either by manual or background cleanup)
	if vs.Count() != 0 {
		t.Errorf("expected count 0 after cleanup, got %d", vs.Count())
	}

	// Memory should be freed
	if vs.TotalMemory() != 0 {
		t.Errorf("expected 0 memory after cleanup, got %d", vs.TotalMemory())
	}
}

func TestVersionStore_RefCounting(t *testing.T) {
	// Use longer TTL to control timing precisely
	vs := NewVersionStore(100*time.Millisecond, 0)

	gv := createTestGraphVersion("v1", 10, 5)
	if err := vs.Store(gv); err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	// Get (and pin) the version
	retrieved, err := vs.Get("v1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// Mark for cleanup
	vs.MarkForCleanup("v1")

	// Wait for TTL to pass
	time.Sleep(150 * time.Millisecond)

	// Cleanup should NOT remove it (still pinned)
	removed := vs.Cleanup()
	if removed != 0 {
		t.Errorf("expected 0 removed (pinned), got %d", removed)
	}

	// Version should still exist
	if vs.Count() != 1 {
		t.Errorf("expected count 1 (pinned), got %d", vs.Count())
	}

	// Unpin
	retrieved.Unpin()

	// Now cleanup should work
	vs.Cleanup()

	// Version should be gone (either by manual or background cleanup)
	if vs.Count() != 0 {
		t.Errorf("expected count 0 after cleanup, got %d", vs.Count())
	}
}

func TestVersionStore_ConcurrentAccess(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	// Pre-populate with some versions
	for i := 0; i < 10; i++ {
		gv := createTestGraphVersion(fmt.Sprintf("v%d", i), 10, 5)
		if err := vs.Store(gv); err != nil {
			t.Fatalf("Store failed: %v", err)
		}
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
				gv, err := vs.Get(versionID)
				if err != nil {
					errors <- fmt.Errorf("Get %s failed: %v", versionID, err)
					return
				}
				// Simulate some work
				_ = gv.VCount
				gv.Unpin()
			}
		}(i)
	}

	// Concurrent writers (adding new versions)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				versionID := fmt.Sprintf("new-%d-%d", id, j)
				gv := createTestGraphVersion(versionID, 10, 5)
				if err := vs.Store(gv); err != nil {
					// Duplicate errors are acceptable in concurrent scenario
					if err.Error() != fmt.Sprintf("version already exists: %s", versionID) {
						errors <- fmt.Errorf("Store %s failed: %v", versionID, err)
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
				_ = vs.Stats()
				_ = vs.TotalMemory()
				_ = vs.Count()
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Error(err)
	}
}

func TestVersionStore_Stats(t *testing.T) {
	vs := NewVersionStore(time.Hour, 1000000) // 1MB limit

	// Initially empty
	stats := vs.Stats()
	if stats.TotalVersions != 0 {
		t.Errorf("expected 0 versions, got %d", stats.TotalVersions)
	}
	if stats.TotalMemory != 0 {
		t.Errorf("expected 0 memory, got %d", stats.TotalMemory)
	}
	if stats.MemoryLimit != 1000000 {
		t.Errorf("expected limit 1000000, got %d", stats.MemoryLimit)
	}

	// Add versions
	gv1 := createTestGraphVersion("v1", 10, 5)
	gv2 := createTestGraphVersion("v2", 20, 10)
	vs.Store(gv1) //nolint:errcheck // test setup
	vs.Store(gv2) //nolint:errcheck // test setup

	stats = vs.Stats()
	if stats.TotalVersions != 2 {
		t.Errorf("expected 2 versions, got %d", stats.TotalVersions)
	}
	expectedMem := gv1.EstimateMemory() + gv2.EstimateMemory()
	if stats.TotalMemory != expectedMem {
		t.Errorf("expected memory %d, got %d", expectedMem, stats.TotalMemory)
	}
	if stats.MarkedForCleanup != 0 {
		t.Errorf("expected 0 marked, got %d", stats.MarkedForCleanup)
	}

	// Mark one for cleanup
	vs.MarkForCleanup("v1")
	stats = vs.Stats()
	if stats.MarkedForCleanup != 1 {
		t.Errorf("expected 1 marked, got %d", stats.MarkedForCleanup)
	}
}

func TestVersionStore_GetNotFound(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	_, err := vs.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent version")
	}
}

func TestVersionStore_GetWithoutPin(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	gv := createTestGraphVersion("v1", 10, 5)
	vs.Store(gv) //nolint:errcheck // test setup

	// GetWithoutPin should work
	retrieved, err := vs.GetWithoutPin("v1")
	if err != nil {
		t.Fatalf("GetWithoutPin failed: %v", err)
	}
	if retrieved.ID != "v1" {
		t.Errorf("expected ID v1, got %s", retrieved.ID)
	}

	// RefCount should be 0 (not pinned)
	if retrieved.RefCount() != 0 {
		t.Errorf("expected refcount 0, got %d", retrieved.RefCount())
	}
}

func TestVersionStore_Delete(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	gv := createTestGraphVersion("v1", 10, 5)
	vs.Store(gv) //nolint:errcheck // test setup

	memBefore := vs.TotalMemory()

	// Delete should succeed
	err := vs.Delete("v1")
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Version should be gone
	if vs.Count() != 0 {
		t.Errorf("expected count 0, got %d", vs.Count())
	}

	// Memory should be freed
	if vs.TotalMemory() != memBefore-gv.EstimateMemory() {
		t.Errorf("memory not freed properly")
	}
}

func TestVersionStore_Delete_Pinned(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	gv := createTestGraphVersion("v1", 10, 5)
	vs.Store(gv) //nolint:errcheck // test setup

	// Pin the version
	retrieved, _ := vs.Get("v1")

	// Delete should fail (pinned)
	err := vs.Delete("v1")
	if err == nil {
		t.Error("expected error deleting pinned version")
	}

	// Version should still exist
	if vs.Count() != 1 {
		t.Errorf("expected count 1, got %d", vs.Count())
	}

	// Unpin and delete
	retrieved.Unpin()
	err = vs.Delete("v1")
	if err != nil {
		t.Fatalf("Delete after unpin failed: %v", err)
	}
}

func TestVersionStore_ListVersionIDs(t *testing.T) {
	vs := NewVersionStore(time.Hour, 0)

	// Add versions
	for i := 0; i < 5; i++ {
		gv := createTestGraphVersion(fmt.Sprintf("v%d", i), 10, 5)
		vs.Store(gv) //nolint:errcheck // test setup
	}

	ids := vs.ListVersionIDs()
	if len(ids) != 5 {
		t.Errorf("expected 5 IDs, got %d", len(ids))
	}

	// Check all IDs are present
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}
	for i := 0; i < 5; i++ {
		if !idSet[fmt.Sprintf("v%d", i)] {
			t.Errorf("missing version v%d", i)
		}
	}
}

func TestVersionStore_MemoryLimit_Enforcement(t *testing.T) {
	// Create versions with predictable memory sizes
	gv := createTestGraphVersion("test", 1000, 500)
	memPerVersion := gv.EstimateMemory()

	// Set limit to exactly 3 versions worth of memory
	limit := memPerVersion * 3
	vs := NewVersionStore(time.Hour, limit)

	// Store 3 versions - should succeed
	for i := 0; i < 3; i++ {
		gv := createTestGraphVersion(fmt.Sprintf("v%d", i), 1000, 500)
		if err := vs.Store(gv); err != nil {
			t.Errorf("Store v%d failed unexpectedly: %v", i, err)
		}
	}

	// 4th version should fail
	gv4 := createTestGraphVersion("v3", 1000, 500)
	err := vs.Store(gv4)
	if err == nil {
		t.Error("expected memory limit error for 4th version")
	}

	// Verify memory is at or below limit
	if vs.TotalMemory() > limit {
		t.Errorf("total memory %d exceeds limit %d", vs.TotalMemory(), limit)
	}
}
