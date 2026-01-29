package service

import (
	"fmt"
	"sync"
	"time"
)

// versionEntry wraps a GraphVersion with cleanup metadata.
type versionEntry struct {
	version       *GraphVersion
	markedAt      time.Time // When marked for cleanup (zero if not marked)
	markedCleanup bool
}

// VersionStore stores GraphVersion objects and manages their lifecycle.
// It implements RCU-style pin/unpin for safe concurrent access.
type VersionStore struct {
	versions map[string]*versionEntry
	mu       sync.RWMutex

	// Cleanup settings
	cleanupTTL time.Duration // How long to keep marked versions

	// Memory tracking
	totalMemory uint64
	memoryLimit uint64
}

// NewVersionStore creates a new VersionStore.
func NewVersionStore(cleanupTTL time.Duration, memoryLimit uint64) *VersionStore {
	vs := &VersionStore{
		versions:    make(map[string]*versionEntry),
		cleanupTTL:  cleanupTTL,
		memoryLimit: memoryLimit,
	}

	// Start background cleanup goroutine
	go vs.cleanupLoop()

	return vs
}

// Store adds a GraphVersion to the store.
func (vs *VersionStore) Store(version *GraphVersion) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	// Check if version already exists
	if _, exists := vs.versions[version.ID]; exists {
		return fmt.Errorf("version already exists: %s", version.ID)
	}

	// Check memory limit
	newMemory := vs.totalMemory + version.EstimateMemory()
	if vs.memoryLimit > 0 && newMemory > vs.memoryLimit {
		return fmt.Errorf("memory limit exceeded: %d > %d", newMemory, vs.memoryLimit)
	}

	vs.versions[version.ID] = &versionEntry{
		version: version,
	}
	vs.totalMemory = newMemory

	return nil
}

// Get retrieves and pins a GraphVersion by ID.
// The caller must call Unpin on the version when done.
func (vs *VersionStore) Get(versionID string) (*GraphVersion, error) {
	vs.mu.RLock()
	entry, exists := vs.versions[versionID]
	if !exists {
		vs.mu.RUnlock()
		return nil, fmt.Errorf("version not found: %s", versionID)
	}
	// Pin before releasing lock
	entry.version.Pin()
	vs.mu.RUnlock()

	return entry.version, nil
}

// GetWithoutPin retrieves a GraphVersion without pinning.
// Use only for read-only operations that don't escape the call.
func (vs *VersionStore) GetWithoutPin(versionID string) (*GraphVersion, error) {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	entry, exists := vs.versions[versionID]
	if !exists {
		return nil, fmt.Errorf("version not found: %s", versionID)
	}

	return entry.version, nil
}

// MarkForCleanup marks a version for eventual cleanup.
// The version won't be removed until TTL expires and refcount is zero.
func (vs *VersionStore) MarkForCleanup(versionID string) {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	entry, exists := vs.versions[versionID]
	if !exists {
		return
	}

	if !entry.markedCleanup {
		entry.markedCleanup = true
		entry.markedAt = time.Now()
	}
}

// Cleanup removes versions that are marked for cleanup, past TTL, and have no refs.
func (vs *VersionStore) Cleanup() int {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	now := time.Now()
	removed := 0

	for id, entry := range vs.versions {
		if !entry.markedCleanup {
			continue
		}

		// Check if TTL has passed
		if now.Sub(entry.markedAt) < vs.cleanupTTL {
			continue
		}

		// Check if no references
		if entry.version.RefCount() > 0 {
			continue
		}

		// Safe to remove
		vs.totalMemory -= entry.version.EstimateMemory()
		delete(vs.versions, id)
		removed++
	}

	return removed
}

// cleanupLoop periodically runs cleanup.
func (vs *VersionStore) cleanupLoop() {
	ticker := time.NewTicker(vs.cleanupTTL / 2)
	defer ticker.Stop()

	for range ticker.C {
		vs.Cleanup()
	}
}

// Count returns the number of versions in the store.
func (vs *VersionStore) Count() int {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return len(vs.versions)
}

// TotalMemory returns the total estimated memory usage.
func (vs *VersionStore) TotalMemory() uint64 {
	vs.mu.RLock()
	defer vs.mu.RUnlock()
	return vs.totalMemory
}

// MemoryLimit returns the configured memory limit.
func (vs *VersionStore) MemoryLimit() uint64 {
	return vs.memoryLimit
}

// Stats returns store statistics.
func (vs *VersionStore) Stats() VersionStoreStats {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	stats := VersionStoreStats{
		TotalVersions:    len(vs.versions),
		TotalMemory:      vs.totalMemory,
		MemoryLimit:      vs.memoryLimit,
		MarkedForCleanup: 0,
	}

	for _, entry := range vs.versions {
		if entry.markedCleanup {
			stats.MarkedForCleanup++
		}
	}

	return stats
}

// VersionStoreStats contains statistics about the version store.
type VersionStoreStats struct {
	TotalVersions    int
	TotalMemory      uint64
	MemoryLimit      uint64
	MarkedForCleanup int
}

// ListVersionIDs returns all version IDs in the store.
func (vs *VersionStore) ListVersionIDs() []string {
	vs.mu.RLock()
	defer vs.mu.RUnlock()

	ids := make([]string, 0, len(vs.versions))
	for id := range vs.versions {
		ids = append(ids, id)
	}
	return ids
}

// Delete immediately removes a version (use with caution).
// Returns error if version is still referenced.
func (vs *VersionStore) Delete(versionID string) error {
	vs.mu.Lock()
	defer vs.mu.Unlock()

	entry, exists := vs.versions[versionID]
	if !exists {
		return fmt.Errorf("version not found: %s", versionID)
	}

	if entry.version.RefCount() > 0 {
		return fmt.Errorf("version still referenced: %s (refs=%d)", versionID, entry.version.RefCount())
	}

	vs.totalMemory -= entry.version.EstimateMemory()
	delete(vs.versions, versionID)
	return nil
}
