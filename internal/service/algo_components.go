package service

import (
	"fmt"
	"strconv"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// ComponentsConfig holds configuration for components computation.
type ComponentsConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph
}

// ComputeComponents computes connected components using igraph.
// If weak is true, computes weakly connected components (ignoring edge direction).
// If weak is false, computes strongly connected components (respecting direction).
//
// This function requires a configured ShimGraph. The igraph C shim is always used
// for optimal performance.
func ComputeComponents(version *GraphVersion, weak bool, cfg *ComponentsConfig) (*AlgoResult, error) {
	// Validate shim config
	if cfg == nil || cfg.ShimGraph == nil {
		return nil, fmt.Errorf("ComponentsConfig with ShimGraph is required")
	}

	// Use igraph shim for components computation
	return computeComponentsShim(version, cfg.ShimGraph, weak)
}

// computeComponentsShim computes components using the igraph C shim.
func computeComponentsShim(version *GraphVersion, g *shim.Graph, weak bool) (*AlgoResult, error) {
	shimResult, err := g.Components(weak)
	if err != nil {
		return nil, fmt.Errorf("shim.Components failed: %w", err)
	}

	// Convert shim result to AlgoResult
	mode := "weak"
	if !weak {
		mode = "strong"
	}
	paramsHash := HashParams(mode, version.ID)
	result := NewAlgoResult(version.ID, AlgoKindComponents, paramsHash)
	result.MembershipU32 = shimResult.Membership
	result.Meta["mode"] = mode
	result.Meta["num_components"] = strconv.FormatUint(uint64(shimResult.NumComponents), 10)
	result.Meta["source"] = "igraph"

	return result, nil
}

// =============================================================================
// Statistics helpers
// =============================================================================

// ComponentStats returns statistics about connected components.
type ComponentStats struct {
	NumComponents    int
	LargestSize      int
	SmallestSize     int
	AverageSize      float64
	IsolatedVertices int
}

// GetComponentStats computes statistics from a membership array.
func GetComponentStats(membership []uint32) ComponentStats {
	if len(membership) == 0 {
		return ComponentStats{}
	}

	// Count sizes
	sizes := make(map[uint32]int)
	for _, compID := range membership {
		sizes[compID]++
	}

	stats := ComponentStats{
		NumComponents: len(sizes),
	}

	var total int
	for _, size := range sizes {
		total += size
		if stats.LargestSize == 0 || size > stats.LargestSize {
			stats.LargestSize = size
		}
		if stats.SmallestSize == 0 || size < stats.SmallestSize {
			stats.SmallestSize = size
		}
		if size == 1 {
			stats.IsolatedVertices++
		}
	}

	if stats.NumComponents > 0 {
		stats.AverageSize = float64(total) / float64(stats.NumComponents)
	}

	return stats
}
