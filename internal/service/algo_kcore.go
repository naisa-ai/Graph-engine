package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// KCoreResult contains the result of k-core decomposition.
type KCoreResult struct {
	// Coreness maps vertex index to its coreness value.
	// The coreness of a vertex is the largest k such that the vertex
	// belongs to a k-core.
	Coreness []uint32

	// MaxCore is the maximum coreness value found in the graph.
	MaxCore uint32

	// CoreSize maps k value to the number of vertices with coreness >= k.
	CoreSize map[uint32]uint32

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// KCoreConfig contains configuration for k-core computation.
type KCoreConfig struct {
	// K specifies a specific k value to extract (0 = compute full decomposition).
	// If K > 0, only vertices with coreness >= K will have their coreness computed.
	K uint32
}

// KCoreShimConfig holds shim configuration for k-core computation.
type KCoreShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultKCoreConfig returns a KCoreConfig with sensible defaults.
func DefaultKCoreConfig() *KCoreConfig {
	return &KCoreConfig{
		K: 0, // Full decomposition
	}
}

// ComputeKCore computes the k-core decomposition of the graph.
//
// The k-core of a graph is the maximal subgraph in which every vertex
// has degree at least k. The coreness of a vertex is the largest k
// for which it belongs to a k-core.
//
// K-core decomposition requires the igraph shim.
func ComputeKCore(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	config *KCoreConfig,
	shimCfg *KCoreShimConfig,
) (*KCoreResult, error) {
	// config is accepted for API consistency; k-core options may be used in future

	// K-core requires the igraph shim
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("k-core computation requires igraph shim")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		_ = 0 // non-blocking check
	}

	// Compute k-core using the shim
	result, err := computeKCoreShim(version, shimCfg)
	if err != nil {
		return nil, fmt.Errorf("igraph shim k-core failed: %w", err)
	}

	return result, nil
}

// computeKCoreShim computes k-core using the igraph C shim.
func computeKCoreShim(version *GraphVersion, shimCfg *KCoreShimConfig) (*KCoreResult, error) {
	var shimResult *shim.KCoreResult
	var err error

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.KCoreOnView(shimCfg.ShimView)
	} else {
		shimResult, err = shimCfg.ShimGraph.KCore()
	}

	if err != nil {
		return nil, fmt.Errorf("shim.KCore failed: %w", err)
	}

	// Build core size map
	coreSize := make(map[uint32]uint32)
	for _, coreness := range shimResult.Coreness {
		// Count vertices with coreness >= k for each k
		for k := uint32(0); k <= coreness; k++ {
			coreSize[k]++
		}
	}

	result := &KCoreResult{
		Coreness: shimResult.Coreness,
		MaxCore:  shimResult.MaxCore,
		CoreSize: coreSize,
		Meta:     make(map[string]string),
	}

	result.Meta["source"] = "igraph"
	result.Meta["max_core"] = fmt.Sprintf("%d", shimResult.MaxCore)

	return result, nil
}

// =============================================================================
// Helper functions for k-core analysis
// =============================================================================

// GetKCoreMembers returns all vertex indices belonging to the k-core.
// A vertex belongs to the k-core if its coreness is >= k.
func (r *KCoreResult) GetKCoreMembers(k uint32) []uint32 {
	members := make([]uint32, 0)
	for vertexIdx, coreness := range r.Coreness {
		if coreness >= k {
			members = append(members, uint32(vertexIdx))
		}
	}
	return members
}

// GetKCoreMembersExternal returns the external vertex IDs belonging to the k-core.
func (r *KCoreResult) GetKCoreMembersExternal(k uint32, version *GraphVersion) []uint64 {
	members := make([]uint64, 0)
	for vertexIdx, coreness := range r.Coreness {
		if coreness >= k {
			if nodeID, ok := version.GetNodeID(uint32(vertexIdx)); ok {
				members = append(members, nodeID)
			}
		}
	}
	return members
}

// GetVertexCoreness returns the coreness value for a specific vertex.
func (r *KCoreResult) GetVertexCoreness(vertexIdx uint32) (uint32, bool) {
	if int(vertexIdx) >= len(r.Coreness) {
		return 0, false
	}
	return r.Coreness[vertexIdx], true
}

// GetVertexCorenessByExternalID returns the coreness for a vertex given its external ID.
func (r *KCoreResult) GetVertexCorenessByExternalID(nodeID uint64, version *GraphVersion) (uint32, bool) {
	vertexIdx, ok := version.GetNodeIndex(nodeID)
	if !ok {
		return 0, false
	}
	return r.GetVertexCoreness(vertexIdx)
}

// GetCoreShells returns vertices grouped by their coreness value.
// Each shell contains vertices with exactly that coreness value.
func (r *KCoreResult) GetCoreShells() map[uint32][]uint32 {
	shells := make(map[uint32][]uint32)
	for vertexIdx, coreness := range r.Coreness {
		shells[coreness] = append(shells[coreness], uint32(vertexIdx))
	}
	return shells
}

// =============================================================================
// Validation functions
// =============================================================================

// ValidateKCoreRequest validates a k-core request.
func ValidateKCoreRequest(k uint32) error {
	// k = 0 means full decomposition, which is always valid
	return nil
}

// HashKCoreParams generates a hash for k-core cache key normalization.
func HashKCoreParams(k uint32, viewHash string) string {
	return fmt.Sprintf("kcore:%d:%s", k, viewHash)
}
