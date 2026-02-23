package service

import (
	"testing"
	"time"
)

func TestViewStore_Basic(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)

	// Store should succeed
	err := vs.Store(view)
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}

	if vs.Count() != 1 {
		t.Errorf("expected Count=1, got %d", vs.Count())
	}
}

func TestViewStore_Duplicate(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)

	// First store should succeed
	err := vs.Store(view)
	if err != nil {
		t.Fatalf("First store failed: %v", err)
	}

	// Second store of same view should fail
	err = vs.Store(view)
	if err == nil {
		t.Error("expected error for duplicate view")
	}
}

func TestViewStore_Get(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)
	_ = vs.Store(view)

	// Get should work and pin the view
	retrieved, err := vs.Get(view.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != view.ID {
		t.Error("retrieved view ID mismatch")
	}
	if retrieved.RefCount() != 1 {
		t.Errorf("expected refCount=1, got %d", retrieved.RefCount())
	}

	retrieved.Unpin()
}

func TestViewStore_GetNotFound(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	_, err := vs.Get("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent view")
	}
}

func TestViewStore_GetByKey(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)
	view.SpecHash = "testhash123"
	_ = vs.Store(view)

	// Get by key should work
	retrieved, found := vs.GetByKey(view.VersionID, view.SpecHash)
	if !found {
		t.Fatal("GetByKey should find the view")
	}
	if retrieved.ID != view.ID {
		t.Error("retrieved view ID mismatch")
	}
	retrieved.Unpin()

	// Non-existent key
	_, found = vs.GetByKey("other-version", "other-hash")
	if found {
		t.Error("GetByKey should not find non-existent view")
	}
}

func TestViewStore_LRUEviction_MaxItems(t *testing.T) {
	vs := NewViewStore(3, 0, time.Hour) // Max 3 items

	version := createTestGraphVersionForView()

	// Add 3 views
	for i := 0; i < 3; i++ {
		view := NewView(version)
		view.SpecHash = string(rune('a' + i))
		_ = vs.Store(view)
		time.Sleep(time.Millisecond) // Ensure different access times
	}

	if vs.Count() != 3 {
		t.Errorf("expected Count=3, got %d", vs.Count())
	}

	// Add 4th view should evict oldest
	view4 := NewView(version)
	view4.SpecHash = "d"
	_ = vs.Store(view4)

	if vs.Count() != 3 {
		t.Errorf("expected Count=3 after eviction, got %d", vs.Count())
	}
}

func TestViewStore_LRUEviction_MaxMemory(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)
	memPerView := view.EstimateMemory()

	// Set memory limit to allow ~2 views
	memLimit := memPerView*2 + 100
	vs := NewViewStore(100, memLimit, time.Hour)

	// Add 3 views
	for i := 0; i < 3; i++ {
		v := NewView(version)
		v.SpecHash = string(rune('a' + i))
		_ = vs.Store(v)
		time.Sleep(time.Millisecond)
	}

	// Should have evicted to stay under memory limit
	if vs.TotalMemory() > memLimit {
		t.Errorf("memory %d exceeds limit %d", vs.TotalMemory(), memLimit)
	}
}

func TestViewStore_PinnedNotEvicted(t *testing.T) {
	vs := NewViewStore(2, 0, time.Hour) // Max 2 items

	version := createTestGraphVersionForView()

	// Add first view and pin it
	view1 := NewView(version)
	view1.SpecHash = "pinned"
	_ = vs.Store(view1)
	retrieved1, _ := vs.Get(view1.ID) // This pins view1
	defer retrieved1.Unpin()

	// Add second view (unpinned)
	view2 := NewView(version)
	view2.SpecHash = "unpinned"
	_ = vs.Store(view2)

	// Add third view - should evict view2 (unpinned), not view1 (pinned)
	view3 := NewView(version)
	view3.SpecHash = "new"
	_ = vs.Store(view3)

	// view1 should still exist
	_, err := vs.Get(view1.ID)
	if err != nil {
		t.Error("pinned view should not be evicted")
	}
}

func TestViewStore_Stats(t *testing.T) {
	vs := NewViewStore(10, 1000, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)
	_ = vs.Store(view)

	stats := vs.Stats()

	if stats.TotalItems != 1 {
		t.Errorf("expected TotalItems=1, got %d", stats.TotalItems)
	}
	if stats.TotalMemory == 0 {
		t.Error("TotalMemory should be > 0")
	}
	if stats.MaxItems != 10 {
		t.Errorf("expected MaxItems=10, got %d", stats.MaxItems)
	}
	if stats.MaxMemory != 1000 {
		t.Errorf("expected MaxMemory=1000, got %d", stats.MaxMemory)
	}
}

func TestViewStore_Delete(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)
	_ = vs.Store(view)

	// Delete should succeed
	err := vs.Delete(view.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if vs.Count() != 0 {
		t.Errorf("expected Count=0 after delete, got %d", vs.Count())
	}

	// Get should fail after delete
	_, err = vs.Get(view.ID)
	if err == nil {
		t.Error("Get should fail after delete")
	}
}

func TestViewStore_DeletePinned(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()
	view := NewView(version)
	_ = vs.Store(view)

	// Pin the view
	retrieved, _ := vs.Get(view.ID)
	// Don't unpin

	// Delete should fail for pinned view
	err := vs.Delete(view.ID)
	if err == nil {
		t.Error("Delete should fail for pinned view")
	}

	retrieved.Unpin()

	// Now delete should succeed
	err = vs.Delete(view.ID)
	if err != nil {
		t.Fatalf("Delete should succeed after unpin: %v", err)
	}
}

func TestViewStore_ListViewIDs(t *testing.T) {
	vs := NewViewStore(10, 0, time.Hour)

	version := createTestGraphVersionForView()

	ids := make(map[string]bool)
	for i := 0; i < 3; i++ {
		view := NewView(version)
		view.SpecHash = string(rune('a' + i))
		_ = vs.Store(view)
		ids[view.ID] = true
	}

	listed := vs.ListViewIDs()
	if len(listed) != 3 {
		t.Errorf("expected 3 IDs, got %d", len(listed))
	}

	for _, id := range listed {
		if !ids[id] {
			t.Errorf("unexpected ID in list: %s", id)
		}
	}
}
