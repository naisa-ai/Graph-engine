package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// MinCutResult contains the result of an s-t minimum cut computation.
type MinCutResult struct {
	// CutValue is the total capacity of edges in the minimum cut
	CutValue float64
	// SourceSideVertices contains the external node IDs on the source side of the cut
	SourceSideVertices []uint64
	// CutEdges contains the edge indices that form the cut
	CutEdges []uint64
	// Meta contains additional information about the computation
	Meta map[string]string
}

// MinCut configuration constants
const (
	// DefaultMaxCorridorEdges is the maximum number of edges allowed for mincut
	DefaultMaxCorridorEdges = 10000
	// DefaultMinCutTimeout is the default timeout for mincut computation
	DefaultMinCutTimeout = 30 * time.Second
)

// MinCutConfig holds configuration for mincut computation.
type MinCutConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph
	// ShimView is the shim view to use for view-based queries (optional).
	// If not provided but a service View is passed, one will be created automatically.
	ShimView *shim.View
}

// ComputeSTMinCut computes the minimum s-t cut using igraph.
// The cut separates source from target with minimum total edge capacity.
// If useWeights is false, all edges have capacity 1.
//
// This function requires a configured ShimGraph. The igraph C shim is always used
// for optimal performance.
func ComputeSTMinCut(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	useWeights bool,
	maxEdges int,
	cfg *MinCutConfig,
) (*MinCutResult, error) {
	// Validate shim config
	if cfg == nil || cfg.ShimGraph == nil {
		return nil, fmt.Errorf("MinCutConfig with ShimGraph is required")
	}

	// Validate inputs
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return nil, fmt.Errorf("source node not found: %d", sourceID)
	}
	targetIdx, ok := version.GetNodeIndex(targetID)
	if !ok {
		return nil, fmt.Errorf("target node not found: %d", targetID)
	}

	// Check if nodes are in view
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return nil, fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return nil, fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Check view size constraint
	if maxEdges <= 0 {
		maxEdges = DefaultMaxCorridorEdges
	}

	edgeCount := countViewEdges(version, view)
	if edgeCount > maxEdges {
		return nil, fmt.Errorf("view has %d edges, exceeds maximum %d for mincut", edgeCount, maxEdges)
	}

	// Determine shim view to use
	shimView := cfg.ShimView
	var shimViewToClose *shim.View
	if view != nil && shimView == nil {
		// Create shim view from service view
		var err error
		shimView, err = createShimViewFromServiceView(cfg.ShimGraph, view)
		if err != nil {
			return nil, fmt.Errorf("failed to create shim view: %w", err)
		}
		shimViewToClose = shimView
		defer shimViewToClose.Close()
	}

	// Use igraph shim for mincut computation
	return computeSTMinCutShim(version, cfg.ShimGraph, shimView, sourceIdx, targetIdx, useWeights, edgeCount)
}

// computeSTMinCutShim computes minimum s-t cut using the igraph C shim.
func computeSTMinCutShim(
	version *GraphVersion,
	g *shim.Graph,
	view *shim.View,
	sourceIdx, targetIdx uint32,
	useWeights bool,
	edgeCount int,
) (*MinCutResult, error) {
	// Prepare capacities if needed
	var capacity []float64
	if useWeights && len(version.EdgeWeight) > 0 {
		capacity = make([]float64, len(version.EdgeWeight))
		for i, w := range version.EdgeWeight {
			capacity[i] = float64(w)
			if capacity[i] <= 0 {
				capacity[i] = 1.0 // Ensure positive capacity
			}
		}
	}

	var shimResult *shim.MinCutResult
	var err error
	if view != nil {
		shimResult, err = g.STMinCutOnView(view, sourceIdx, targetIdx, capacity)
	} else {
		shimResult, err = g.STMinCut(sourceIdx, targetIdx, capacity)
	}
	if err != nil {
		return nil, fmt.Errorf("shim.STMinCut failed: %w", err)
	}

	// Convert source side indices to external IDs
	sourceSideIDs := make([]uint64, len(shimResult.SourceSide))
	sourceSideMap := make(map[uint32]bool)
	for i, idx := range shimResult.SourceSide {
		nodeID, _ := version.GetNodeID(idx)
		sourceSideIDs[i] = nodeID
		sourceSideMap[idx] = true
	}

	// Convert cut edges
	cutEdges := make([]uint64, len(shimResult.CutEdges))
	for i, idx := range shimResult.CutEdges {
		cutEdges[i] = uint64(idx)
	}

	// Build metadata
	meta := make(map[string]string)
	meta["source"] = "igraph"
	meta["source_side_count"] = strconv.Itoa(len(shimResult.SourceSide))
	meta["cut_edges_count"] = strconv.Itoa(len(cutEdges))
	meta["view_edges"] = strconv.Itoa(edgeCount)
	meta["weighted"] = strconv.FormatBool(useWeights)
	meta["cut_value"] = strconv.FormatFloat(shimResult.CutValue, 'f', 4, 64)

	return &MinCutResult{
		CutValue:           shimResult.CutValue,
		SourceSideVertices: sourceSideIDs,
		CutEdges:           cutEdges,
		Meta:               meta,
	}, nil
}

// countViewEdges counts the number of edges in a view (or full graph if view is nil).
func countViewEdges(version *GraphVersion, view *View) int {
	if view == nil {
		return int(version.ECount)
	}
	return int(view.ECount)
}

// =============================================================================
// Validation and convenience functions
// =============================================================================

// ValidateMinCutRequest validates parameters for a mincut request.
func ValidateMinCutRequest(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	maxEdges int,
) error {
	// Check source exists
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return fmt.Errorf("source node not found: %d", sourceID)
	}

	// Check target exists
	targetIdx, ok := version.GetNodeIndex(targetID)
	if !ok {
		return fmt.Errorf("target node not found: %d", targetID)
	}

	// Check source != target
	if sourceIdx == targetIdx {
		return fmt.Errorf("source and target must be different")
	}

	// Check nodes are in view
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Check edge count
	if maxEdges <= 0 {
		maxEdges = DefaultMaxCorridorEdges
	}
	edgeCount := countViewEdges(version, view)
	if edgeCount > maxEdges {
		return fmt.Errorf("view has %d edges, exceeds maximum %d", edgeCount, maxEdges)
	}

	return nil
}

// ComputeSTMinCutWithTimeout wraps ComputeSTMinCut with a timeout.
func ComputeSTMinCutWithTimeout(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	useWeights bool,
	maxEdges int,
	timeout time.Duration,
	cfg *MinCutConfig,
) (*MinCutResult, error) {
	if timeout <= 0 {
		timeout = DefaultMinCutTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return ComputeSTMinCut(ctx, version, view, sourceID, targetID, useWeights, maxEdges, cfg)
}
