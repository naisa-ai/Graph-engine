package service

import (
	"context"
	"fmt"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// KSPResult contains the result of a K-shortest paths computation.
type KSPResult struct {
	// Paths contains the K shortest paths found
	Paths []*ShortestPathResult
	// PathsFound is the number of paths actually found (may be < K if not enough paths exist)
	PathsFound int
	// Meta contains algorithm metadata
	Meta map[string]string
	// CandidatesExplored is the number of candidate paths explored
	CandidatesExplored int
}

// KSPConfig contains configuration for the KSP algorithm.
type KSPConfig struct {
	// K is the number of shortest paths to find
	K int
	// MaxCandidates limits the candidate heap size (0 = default: 10*K)
	MaxCandidates int
	// PerPathTimeout is the timeout for finding each individual path
	PerPathTimeout time.Duration
	// DiversityPenalty is a multiplier applied to edges already used in found paths (1.0 = no penalty)
	DiversityPenalty float64
	// Weighted indicates whether to use edge weights
	Weighted bool
	// ReturnVertices indicates whether to return path vertices
	ReturnVertices bool
	// ReturnEdges indicates whether to return path edges
	ReturnEdges bool
}

// KSPShimConfig holds shim configuration for KSP computation.
type KSPShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph
	// ShimView is the shim view to use for view-based queries (optional).
	// If not provided but a service View is passed, one will be created automatically.
	ShimView *shim.View
}

// DefaultKSPConfig returns a KSPConfig with sensible defaults.
func DefaultKSPConfig(k int) *KSPConfig {
	return &KSPConfig{
		K:                k,
		MaxCandidates:    10 * k,
		PerPathTimeout:   30 * time.Second,
		DiversityPenalty: 1.0, // no penalty by default
		Weighted:         false,
		ReturnVertices:   true,
		ReturnEdges:      false,
	}
}

// ComputeKShortestPaths computes K shortest paths using Yen's algorithm via igraph.
//
// This function requires a configured ShimGraph. The igraph C shim is always used
// for optimal performance.
func ComputeKShortestPaths(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	config *KSPConfig,
	shimCfg *KSPShimConfig,
) (*KSPResult, error) {
	if config == nil {
		config = DefaultKSPConfig(3)
	}
	if config.K <= 0 {
		return nil, fmt.Errorf("K must be positive")
	}
	if config.MaxCandidates <= 0 {
		config.MaxCandidates = 10 * config.K
	}

	// Validate shim config
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("KSPShimConfig with ShimGraph is required")
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

	// Check if nodes are in view
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return nil, fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return nil, fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Determine shim view to use
	shimView := shimCfg.ShimView
	var shimViewToClose *shim.View
	if view != nil && shimView == nil {
		// Create shim view from service view
		var err error
		shimView, err = createShimViewFromServiceView(shimCfg.ShimGraph, view)
		if err != nil {
			return nil, fmt.Errorf("failed to create shim view: %w", err)
		}
		shimViewToClose = shimView
		defer shimViewToClose.Close()
	}

	// Use igraph shim for KSP computation
	return computeKSPShim(version, shimCfg.ShimGraph, shimView, sourceIdx, targetIdx, config)
}

// computeKSPShim computes K shortest paths using the igraph C shim.
func computeKSPShim(
	version *GraphVersion,
	g *shim.Graph,
	view *shim.View,
	sourceIdx, targetIdx uint32,
	config *KSPConfig,
) (*KSPResult, error) {
	// Prepare weights if needed
	var weights []float64
	if config.Weighted && len(version.EdgeWeight) > 0 {
		weights = make([]float64, len(version.EdgeWeight))
		for i, w := range version.EdgeWeight {
			weights[i] = float64(w)
		}
	}

	var shimResult *shim.KSPResult
	var err error
	if view != nil {
		shimResult, err = g.KShortestPathsOnView(view, sourceIdx, targetIdx, uint32(config.K), weights, uint32(config.MaxCandidates))
	} else {
		shimResult, err = g.KShortestPaths(sourceIdx, targetIdx, uint32(config.K), weights, uint32(config.MaxCandidates))
	}
	if err != nil {
		return nil, fmt.Errorf("shim.KShortestPaths failed: %w", err)
	}

	result := &KSPResult{
		Paths:      make([]*ShortestPathResult, shimResult.NumPaths),
		PathsFound: int(shimResult.NumPaths),
		Meta:       make(map[string]string),
	}

	// Convert each path
	for i := uint32(0); i < shimResult.NumPaths; i++ {
		pathResult := &ShortestPathResult{
			Found:  true,
			Source: "igraph",
		}

		if i < uint32(len(shimResult.Costs)) {
			pathResult.TotalCost = shimResult.Costs[i]
		}

		if config.ReturnVertices && i < uint32(len(shimResult.Paths)) {
			// Convert internal indices to external IDs
			path := shimResult.Paths[i]
			pathResult.PathVertices = make([]uint64, len(path))
			for j, idx := range path {
				nodeID, _ := version.GetNodeID(idx)
				pathResult.PathVertices[j] = nodeID
			}
		}

		result.Paths[i] = pathResult
	}

	if result.PathsFound > 0 {
		result.Meta["status"] = "complete"
	} else {
		result.Meta["status"] = "no_path"
	}
	result.Meta["source"] = "igraph"

	return result, nil
}

// =============================================================================
// Validation and helper functions
// =============================================================================

// ValidateKSPRequest validates a K-shortest paths request.
func ValidateKSPRequest(k, maxCandidates uint32) error {
	if k == 0 {
		return fmt.Errorf("k must be positive")
	}
	if k > 100 {
		return fmt.Errorf("k exceeds maximum allowed (100)")
	}
	if maxCandidates > 10000 {
		return fmt.Errorf("max_candidates exceeds maximum allowed (10000)")
	}
	return nil
}

// HashKSPParams generates a hash for KSP cache key normalization.
func HashKSPParams(src, dst uint64, k uint32, weightColumn string, viewHash string) string {
	// Normalize: ensure src < dst for undirected consistency
	if src > dst {
		src, dst = dst, src
	}
	return fmt.Sprintf("ksp:%d:%d:%d:%s:%s", src, dst, k, weightColumn, viewHash)
}

// createShimViewFromServiceView converts a service-level View to a shim View.
// The shim View uses an edge mask bitset format.
func createShimViewFromServiceView(g *shim.Graph, view *View) (*shim.View, error) {
	if view == nil {
		return nil, nil
	}

	// Convert roaring bitmap edge mask to byte slice bitset
	edgeMask := viewEdgeMaskToBytes(view)
	return g.NewViewFromEdgeMask(edgeMask)
}

// viewEdgeMaskToBytes converts a View's roaring bitmap edge mask to a byte slice bitset.
// The bitset format has bit i set if edge i is included in the view.
func viewEdgeMaskToBytes(view *View) []byte {
	numEdges := view.totalEdges
	if numEdges == 0 {
		return nil
	}

	// Calculate number of bytes needed
	numBytes := (numEdges + 7) / 8
	mask := make([]byte, numBytes)

	// Get the roaring bitmap
	bitmap := view.EdgeMaskBitmap()

	// Iterate through the set bits and set corresponding bytes
	it := bitmap.Iterator()
	for it.HasNext() {
		idx := it.Next()
		if idx < numEdges {
			byteIdx := idx / 8
			bitIdx := idx % 8
			mask[byteIdx] |= (1 << bitIdx)
		}
	}

	return mask
}
