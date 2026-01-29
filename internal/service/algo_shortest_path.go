package service

import (
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// ShortestPathResult contains the result of a shortest path computation.
type ShortestPathResult struct {
	// PathVertices contains the external node IDs in the path (source to target)
	PathVertices []uint64
	// PathEdges contains the edge indices in the path
	PathEdges []int
	// TotalCost is the total path cost (hop count for unweighted, sum of weights for weighted)
	TotalCost float64
	// Found indicates whether a path was found
	Found bool
	// Source indicates the algorithm source ("igraph")
	Source string
}

// ShortestPathConfig holds configuration for shortest path computation.
type ShortestPathConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph
}

// ComputeShortestPath computes the shortest path between two nodes using igraph.
// If view is nil, uses the full graph.
// If weighted is true, uses edge weights. Otherwise uses hop count.
//
// This function requires a configured ShimGraph. The igraph C shim is always used
// for optimal performance.
func ComputeShortestPath(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	weighted bool,
	returnEdges, returnVertices bool,
	cfg *ShortestPathConfig,
) (*ShortestPathResult, error) {
	// Validate config
	if cfg == nil || cfg.ShimGraph == nil {
		return nil, fmt.Errorf("ShortestPathConfig with ShimGraph is required")
	}

	// Convert external IDs to internal indices
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return nil, fmt.Errorf("source node not found: %d", sourceID)
	}
	targetIdx, ok := version.GetNodeIndex(targetID)
	if !ok {
		return nil, fmt.Errorf("target node not found: %d", targetID)
	}

	// Check if nodes are in view (if view is specified)
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return nil, fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return nil, fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Use igraph shim for shortest path computation
	return computeShortestPathShim(
		version, cfg.ShimGraph, sourceIdx, targetIdx,
		weighted, returnEdges, returnVertices,
	)
}

// computeShortestPathShim computes shortest path using the igraph C shim.
func computeShortestPathShim(
	version *GraphVersion,
	g *shim.Graph,
	sourceIdx, targetIdx uint32,
	weighted bool,
	returnEdges, returnVertices bool,
) (*ShortestPathResult, error) {
	// Prepare weights if needed
	var weights []float64
	if weighted && len(version.EdgeWeight) > 0 {
		weights = make([]float64, len(version.EdgeWeight))
		for i, w := range version.EdgeWeight {
			weights[i] = float64(w)
		}
	}

	shimResult, err := g.ShortestPath(sourceIdx, targetIdx, weights, returnVertices, returnEdges)
	if err != nil {
		return nil, fmt.Errorf("shim.ShortestPath failed: %w", err)
	}

	result := &ShortestPathResult{
		TotalCost: shimResult.TotalCost,
		Found:     shimResult.Found,
		Source:    "igraph",
	}

	if shimResult.Found {
		if returnVertices && len(shimResult.PathVertices) > 0 {
			// Convert internal indices to external IDs
			result.PathVertices = make([]uint64, len(shimResult.PathVertices))
			for i, idx := range shimResult.PathVertices {
				nodeID, _ := version.GetNodeID(idx)
				result.PathVertices[i] = nodeID
			}
		}
		if returnEdges && len(shimResult.PathEdges) > 0 {
			result.PathEdges = make([]int, len(shimResult.PathEdges))
			for i, idx := range shimResult.PathEdges {
				result.PathEdges[i] = int(idx)
			}
		}
	}

	return result, nil
}

// =============================================================================
// Batch computation
// =============================================================================

// ComputeShortestPathBatch computes shortest paths for multiple source-target pairs.
func ComputeShortestPathBatch(
	version *GraphVersion,
	view *View,
	sources, targets []uint64,
	weighted bool,
	cfg *ShortestPathConfig,
) ([]*ShortestPathResult, error) {
	results := make([]*ShortestPathResult, len(sources))

	for i := range sources {
		var targetID uint64
		if i < len(targets) {
			targetID = targets[i]
		} else if len(targets) > 0 {
			targetID = targets[0]
		} else {
			return nil, fmt.Errorf("no target specified for source %d", i)
		}

		result, err := ComputeShortestPath(version, view, sources[i], targetID, weighted, true, true, cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to compute path %d: %w", i, err)
		}
		results[i] = result
	}

	return results, nil
}
