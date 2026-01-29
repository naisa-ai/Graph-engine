package service

import (
	"fmt"
	"sync"
	"time"
)

// viewEntry wraps a View with LRU metadata.
type viewEntry struct {
	view       *View
	lastAccess time.Time
}

// ViewStore stores View objects with caching and LRU eviction.
type ViewStore struct {
	// Views by ID
	views map[string]*viewEntry
	// Cache index: cacheKey (version_id + spec_hash) -> viewID
	cacheIndex map[string]string
	mu         sync.RWMutex

	// Limits
	maxItems    int
	maxMemory   uint64
	ttl         time.Duration
	totalMemory uint64
}

// NewViewStore creates a new ViewStore.
func NewViewStore(maxItems int, maxMemory uint64, ttl time.Duration) *ViewStore {
	vs := &ViewStore{
		views:      make(map[string]*viewEntry),
		cacheIndex: make(map[string]string),
		maxItems:   maxItems,
		maxMemory:  maxMemory,
		ttl:        ttl,
	}

	// Start background cleanup
	go vs.cleanupLoop()

	return vs
}

// Store adds a view to the store.
func (vs *ViewStore) Store(view *View) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Check if already exists
	if _, exists := vs.views[view.ID]; exists {
		return fmt.Errorf("view already exists: %s", view.ID)
	}

	// Evict if needed
	vs.evictIfNeeded(view.EstimateMemory())

	// Store view
	entry := &viewEntry{
		view:       view,
		lastAccess: time.Now(),
	}
	vs.views[view.ID] = entry
	vs.totalMemory += view.EstimateMemory()

	// Add to cache index
	cacheKey := vs.makeCacheKey(view.VersionID, view.SpecHash)
	vs.cacheIndex[cacheKey] = view.ID

	return nil
}

// Get retrieves and pins a view by ID.
func (vs *ViewStore) Get(viewID string) (*View, error) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	entry, exists := vs.views[viewID]
	if !exists {
		return nil, fmt.Errorf("view not found: %s", viewID)
	}

	entry.lastAccess = time.Now()
	entry.view.Pin()

	return entry.view, nil
}

// GetByKey retrieves a cached view by version ID and spec hash.
func (vs *ViewStore) GetByKey(versionID, specHash string) (*View, bool) {
	cacheKey := vs.makeCacheKey(versionID, specHash)

	vs.mu.Lock()
	defer vs.mu.Unlock()

	viewID, exists := vs.cacheIndex[cacheKey]
	if !exists {
		return nil, false
	}

	entry, exists := vs.views[viewID]
	if !exists {
		// Stale cache entry
		delete(vs.cacheIndex, cacheKey)
		return nil, false
	}

	entry.lastAccess = time.Now()
	entry.view.Pin()

	return entry.view, true
}

// makeCacheKey creates a cache key from version ID and spec hash.
func (vs *ViewStore) makeCacheKey(versionID, specHash string) string {
	return versionID + ":" + specHash
}

// evictIfNeeded removes old entries to make room for new ones.
// Must be called with lock held.
func (vs *ViewStore) evictIfNeeded(newBytes uint64) {
	// Check item limit
	for len(vs.views) >= vs.maxItems {
		if !vs.evictLRU() {
			break
		}
	}

	// Check memory limit
	for vs.maxMemory > 0 && vs.totalMemory+newBytes > vs.maxMemory {
		if !vs.evictLRU() {
			break // No more to evict
		}
	}
}

// evictLRU removes the least recently used unpinned entry.
// Must be called with lock held. Returns false if nothing to evict.
func (vs *ViewStore) evictLRU() bool {
	var oldestID string
	var oldestTime time.Time

	for id, entry := range vs.views {
		if entry.view.RefCount() > 0 {
			continue // Skip pinned
		}
		if oldestID == "" || entry.lastAccess.Before(oldestTime) {
			oldestID = id
			oldestTime = entry.lastAccess
		}
	}

	if oldestID == "" {
		return false
	}

	entry := vs.views[oldestID]
	vs.totalMemory -= entry.view.EstimateMemory()
	cacheKey := vs.makeCacheKey(entry.view.VersionID, entry.view.SpecHash)
	delete(vs.cacheIndex, cacheKey)
	delete(vs.views, oldestID)

	return true
}

// cleanupLoop periodically removes expired views.
func (vs *ViewStore) cleanupLoop() {
	ticker := time.NewTicker(vs.ttl / 2)
	defer ticker.Stop()

	for range ticker.C {
		vs.cleanup()
	}
}

// cleanup removes expired unpinned views.
func (vs *ViewStore) cleanup() {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	now := time.Now()
	for id, entry := range vs.views {
		if entry.view.RefCount() > 0 {
			continue
		}
		if now.Sub(entry.lastAccess) > vs.ttl {
			vs.totalMemory -= entry.view.EstimateMemory()
			cacheKey := vs.makeCacheKey(entry.view.VersionID, entry.view.SpecHash)
			delete(vs.cacheIndex, cacheKey)
			delete(vs.views, id)
		}
	}
}

// Count returns the number of views in the store.
func (vs *ViewStore) Count() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.views)
}

// TotalMemory returns total memory usage.
func (vs *ViewStore) TotalMemory() uint64 {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.totalMemory
}

// Stats returns store statistics.
func (vs *ViewStore) Stats() ViewStoreStats {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	return ViewStoreStats{
		TotalItems:  len(vs.views),
		TotalMemory: vs.totalMemory,
		MaxItems:    vs.maxItems,
		MaxMemory:   vs.maxMemory,
	}
}

// ViewStoreStats contains statistics about the view store.
type ViewStoreStats struct {
	TotalItems  int
	TotalMemory uint64
	MaxItems    int
	MaxMemory   uint64
}

// Delete removes a view by ID.
func (vs *ViewStore) Delete(viewID string) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	entry, exists := vs.views[viewID]
	if !exists {
		return fmt.Errorf("view not found: %s", viewID)
	}

	if entry.view.RefCount() > 0 {
		return fmt.Errorf("view still pinned: %s", viewID)
	}

	vs.totalMemory -= entry.view.EstimateMemory()
	cacheKey := vs.makeCacheKey(entry.view.VersionID, entry.view.SpecHash)
	delete(vs.cacheIndex, cacheKey)
	delete(vs.views, viewID)

	return nil
}

// ListViewIDs returns all view IDs in the store.
func (vs *ViewStore) ListViewIDs() []string {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	ids := make([]string, 0, len(vs.views))
	for id := range vs.views {
		ids = append(ids, id)
	}
	return ids
}
