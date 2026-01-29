package service

import (
	"fmt"
	"sync"
	"time"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// GraphEntry represents a registered graph with its current version.
type GraphEntry struct {
	GraphName        string
	CurrentVersionID string
	PublishedAt      time.Time
}

// GraphRegistry tracks published graphs and their current versions.
// It provides atomic version swapping for the "one writer, many readers" pattern.
type GraphRegistry struct {
	// graphs maps graph_name -> GraphEntry
	graphs map[string]*GraphEntry
	mu     sync.RWMutex

	// versionStore holds the actual GraphVersion objects
	versionStore *VersionStore

	// Limits
	maxGraphs int
}

// NewGraphRegistry creates a new GraphRegistry.
func NewGraphRegistry(versionStore *VersionStore, maxGraphs int) *GraphRegistry {
	return &GraphRegistry{
		graphs:       make(map[string]*GraphEntry),
		versionStore: versionStore,
		maxGraphs:    maxGraphs,
	}
}

// Publish registers a new version as the current version for a graph.
// This operation is atomic - readers will see either the old or new version,
// never a partial state.
func (r *GraphRegistry) Publish(graphName string, version *GraphVersion) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check if this is a new graph
	entry, exists := r.graphs[graphName]
	if !exists {
		// Check max graphs limit
		if len(r.graphs) >= r.maxGraphs {
			return fmt.Errorf("maximum number of graphs (%d) reached", r.maxGraphs)
		}

		// Create new entry
		entry = &GraphEntry{
			GraphName: graphName,
		}
		r.graphs[graphName] = entry
	}

	// Store the version in VersionStore
	if err := r.versionStore.Store(version); err != nil {
		return fmt.Errorf("failed to store version: %w", err)
	}

	// Get old version ID for cleanup scheduling
	oldVersionID := entry.CurrentVersionID

	// Atomic swap to new version
	entry.CurrentVersionID = version.ID
	entry.PublishedAt = version.PublishedAt

	// Schedule old version for cleanup (if exists)
	if oldVersionID != "" {
		r.versionStore.MarkForCleanup(oldVersionID)
	}

	return nil
}

// GetCurrentVersion returns the current version for a graph.
// The returned version is pinned and must be unpinned after use.
func (r *GraphRegistry) GetCurrentVersion(graphName string) (*GraphVersion, error) {
	r.mu.RLock()
	entry, exists := r.graphs[graphName]
	if !exists {
		r.mu.RUnlock()
		return nil, fmt.Errorf("graph not found: %s", graphName)
	}
	versionID := entry.CurrentVersionID
	r.mu.RUnlock()

	// Get and pin the version from VersionStore
	version, err := r.versionStore.Get(versionID)
	if err != nil {
		return nil, fmt.Errorf("version not found: %s", versionID)
	}

	return version, nil
}

// GetVersion returns a specific version by ID.
// The returned version is pinned and must be unpinned after use.
func (r *GraphRegistry) GetVersion(graphName, versionID string) (*GraphVersion, error) {
	r.mu.RLock()
	entry, exists := r.graphs[graphName]
	if !exists {
		r.mu.RUnlock()
		return nil, fmt.Errorf("graph not found: %s", graphName)
	}
	r.mu.RUnlock()

	// If versionID is empty, use current version
	if versionID == "" {
		versionID = entry.CurrentVersionID
	}

	// Get and pin the version from VersionStore
	version, err := r.versionStore.Get(versionID)
	if err != nil {
		return nil, fmt.Errorf("version not found: %s", versionID)
	}

	return version, nil
}

// ListGraphs returns summaries of all published graphs.
func (r *GraphRegistry) ListGraphs() []*gepb.GraphSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()

	summaries := make([]*gepb.GraphSummary, 0, len(r.graphs))
	for _, entry := range r.graphs {
		version, err := r.versionStore.GetWithoutPin(entry.CurrentVersionID)
		if err != nil {
			continue
		}
		summaries = append(summaries, version.ToSummary())
	}

	return summaries
}

// GetGraphEntry returns the registry entry for a graph.
func (r *GraphRegistry) GetGraphEntry(graphName string) (*GraphEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, exists := r.graphs[graphName]
	if !exists {
		return nil, false
	}
	// Return a copy to avoid race conditions
	entryCopy := *entry
	return &entryCopy, true
}

// GraphExists checks if a graph is registered.
func (r *GraphRegistry) GraphExists(graphName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.graphs[graphName]
	return exists
}

// Count returns the number of registered graphs.
func (r *GraphRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.graphs)
}

// DeleteGraph removes a graph from the registry.
// All versions are marked for cleanup.
func (r *GraphRegistry) DeleteGraph(graphName string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.graphs[graphName]
	if !exists {
		return false
	}

	// Mark current version for cleanup
	if entry.CurrentVersionID != "" {
		r.versionStore.MarkForCleanup(entry.CurrentVersionID)
	}

	delete(r.graphs, graphName)
	return true
}
