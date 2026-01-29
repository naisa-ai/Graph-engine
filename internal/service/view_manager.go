package service

import (
	"fmt"
	"log/slog"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// ViewManager orchestrates view creation and caching.
type ViewManager struct {
	viewStore    *ViewStore
	versionStore *VersionStore
	logger       *slog.Logger
}

// NewViewManager creates a new ViewManager.
func NewViewManager(viewStore *ViewStore, versionStore *VersionStore, logger *slog.Logger) *ViewManager {
	return &ViewManager{
		viewStore:    viewStore,
		versionStore: versionStore,
		logger:       logger,
	}
}

// CreateView creates a new view from a GraphVersion and ViewSpec.
// It checks the cache first and returns a cached view if available.
func (vm *ViewManager) CreateView(version *GraphVersion, spec *gepb.ViewSpec) (*View, error) {
	// Compute spec hash for caching
	specHash := HashViewSpec(spec)

	// Check cache first
	if cachedView, found := vm.viewStore.GetByKey(version.ID, specHash); found {
		vm.logger.Debug("view cache hit",
			"version_id", version.ID,
			"spec_hash", specHash,
			"view_id", cachedView.ID,
		)
		return cachedView, nil
	}

	vm.logger.Debug("view cache miss, creating new view",
		"version_id", version.ID,
		"spec_hash", specHash,
	)

	// Create new view from spec
	view, err := NewViewFromSpec(version, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create view: %w", err)
	}

	// Store in cache
	if err := vm.viewStore.Store(view); err != nil {
		// Log warning but continue - view is still usable
		vm.logger.Warn("failed to cache view",
			"view_id", view.ID,
			"error", err,
		)
	}

	vm.logger.Info("created view",
		"view_id", view.ID,
		"version_id", version.ID,
		"vcount", view.VCount,
		"ecount", view.ECount,
	)

	// Pin and return
	view.Pin()
	return view, nil
}

// GetView retrieves a view by ID.
func (vm *ViewManager) GetView(viewID string) (*View, error) {
	return vm.viewStore.Get(viewID)
}

// GetViewForVersion retrieves or creates a view for a version with the given spec.
// If spec is nil, returns a full-graph view.
func (vm *ViewManager) GetViewForVersion(versionID string, spec *gepb.ViewSpec) (*View, error) {
	// Get the version
	version, err := vm.versionStore.Get(versionID)
	if err != nil {
		return nil, fmt.Errorf("version not found: %w", err)
	}
	defer version.Unpin()

	// If no spec, create a full-graph view
	if spec == nil {
		spec = &gepb.ViewSpec{}
	}

	return vm.CreateView(version, spec)
}

// ValidateViewSpec validates a ViewSpec without creating a view.
func (vm *ViewManager) ValidateViewSpec(spec *gepb.ViewSpec) error {
	if spec == nil {
		return nil // Empty spec is valid (full graph)
	}

	// Validate vertex filter predicates
	if spec.GetVfilter() != nil {
		for i, pred := range spec.GetVfilter().GetPredicates() {
			if pred.GetColumn() == "" {
				return fmt.Errorf("vertex filter predicate %d: column is required", i)
			}
		}
	}

	// Validate edge filter predicates
	if spec.GetEfilter() != nil {
		for i, pred := range spec.GetEfilter().GetPredicates() {
			if pred.GetColumn() == "" {
				return fmt.Errorf("edge filter predicate %d: column is required", i)
			}
		}
	}

	// Validate neighborhood spec
	if spec.GetNeighborhood() != nil {
		if len(spec.GetNeighborhood().GetSeedsU64()) == 0 {
			return fmt.Errorf("neighborhood spec: at least one seed is required")
		}
		if spec.GetNeighborhood().GetHops() == 0 {
			return fmt.Errorf("neighborhood spec: hops must be > 0")
		}
	}

	return nil
}

// Stats returns statistics about the view manager.
func (vm *ViewManager) Stats() ViewManagerStats {
	storeStats := vm.viewStore.Stats()
	return ViewManagerStats{
		CachedViews: storeStats.TotalItems,
		TotalMemory: storeStats.TotalMemory,
		MaxViews:    storeStats.MaxItems,
		MaxMemory:   storeStats.MaxMemory,
	}
}

// ViewManagerStats contains statistics about the view manager.
type ViewManagerStats struct {
	CachedViews int
	TotalMemory uint64
	MaxViews    int
	MaxMemory   uint64
}

// ReleaseView releases a view, decrementing its reference count.
func (vm *ViewManager) ReleaseView(viewID string) error {
	view, err := vm.viewStore.Get(viewID)
	if err != nil {
		return err
	}
	// Get() pins it, so we need to unpin twice (once for Get, once for release)
	view.Unpin()
	view.Unpin()
	return nil
}

// DeleteView removes a view from the store.
func (vm *ViewManager) DeleteView(viewID string) error {
	return vm.viewStore.Delete(viewID)
}

// StoreView stores a view in the cache.
// Use this for views created externally (e.g., corridor views).
func (vm *ViewManager) StoreView(view *View) error {
	return vm.viewStore.Store(view)
}
