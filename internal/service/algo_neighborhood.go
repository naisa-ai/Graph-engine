package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// NeighborhoodMode specifies the traversal direction for neighborhood queries.
type NeighborhoodMode int

const (
	// NeighborhoodModeAll traverses both directions (undirected).
	NeighborhoodModeAll NeighborhoodMode = iota
	// NeighborhoodModeOut traverses outgoing edges only.
	NeighborhoodModeOut
	// NeighborhoodModeIn traverses incoming edges only.
	NeighborhoodModeIn
)

func (m NeighborhoodMode) String() string {
	switch m {
	case NeighborhoodModeAll:
		return "all"
	case NeighborhoodModeOut:
		return "out"
	case NeighborhoodModeIn:
		return "in"
	default:
		return "unknown"
	}
}

func (m NeighborhoodMode) toShimMode() shim.Mode {
	switch m {
	case NeighborhoodModeOut:
		return shim.ModeOut
	case NeighborhoodModeIn:
		return shim.ModeIn
	default:
		return shim.ModeAll
	}
}

// NeighborhoodResult contains the result of a neighborhood query.
type NeighborhoodResult struct {
	// Vertices contains all vertices in the neighborhood (including seeds).
	Vertices []uint32

	// VerticesExternal contains external node IDs of vertices in the neighborhood.
	VerticesExternal []uint64

	// Distances maps each vertex to its distance from the nearest seed.
	Distances []uint32

	// DistanceMap provides O(1) lookup of distance by vertex index.
	DistanceMap map[uint32]uint32

	// NumVertices is the total number of vertices in the neighborhood.
	NumVertices uint32

	// VerticesByDistance groups vertices by their distance from seeds.
	VerticesByDistance map[uint32][]uint32

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// NeighborhoodConfig contains configuration for neighborhood queries.
type NeighborhoodConfig struct {
	// Hops is the maximum number of hops from seeds (1 = immediate neighbors).
	Hops uint32

	// Mode specifies traversal direction.
	Mode NeighborhoodMode

	// ReturnExternalIDs indicates whether to populate VerticesExternal.
	ReturnExternalIDs bool

	// GroupByDistance indicates whether to populate VerticesByDistance.
	GroupByDistance bool
}

// NeighborhoodShimConfig holds shim configuration for neighborhood computation.
type NeighborhoodShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultNeighborhoodConfig returns a NeighborhoodConfig with sensible defaults.
func DefaultNeighborhoodConfig(hops uint32) *NeighborhoodConfig {
	return &NeighborhoodConfig{
		Hops:              hops,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: false,
		GroupByDistance:   false,
	}
}

// ComputeNeighborhood finds all vertices within a specified number of hops from seed vertices
// using igraph.
//
// This function requires a configured ShimGraph. The igraph C shim is always used
// for optimal performance.
func ComputeNeighborhood(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	seedIDs []uint64,
	config *NeighborhoodConfig,
	shimCfg *NeighborhoodShimConfig,
) (*NeighborhoodResult, error) {
	if config == nil {
		config = DefaultNeighborhoodConfig(1)
	}
	if config.Hops == 0 {
		return nil, fmt.Errorf("hops must be positive")
	}
	if len(seedIDs) == 0 {
		return nil, fmt.Errorf("seeds cannot be empty")
	}

	// Validate shim config
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("NeighborhoodShimConfig with ShimGraph is required")
	}

	// Convert external IDs to internal indices
	seedIndices := make([]uint32, 0, len(seedIDs))
	for _, seedID := range seedIDs {
		idx, ok := version.GetNodeIndex(seedID)
		if !ok {
			return nil, fmt.Errorf("seed node not found: %d", seedID)
		}
		// Check if seed is in view
		if view != nil && !view.ContainsVertexByIndex(idx) {
			return nil, fmt.Errorf("seed node not in view: %d", seedID)
		}
		seedIndices = append(seedIndices, idx)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		_ = 0 // non-blocking check
	}

	// Use igraph shim for neighborhood computation
	return computeNeighborhoodShim(version, shimCfg, seedIndices, config)
}

// computeNeighborhoodShim computes neighborhood using the igraph C shim.
func computeNeighborhoodShim(
	version *GraphVersion,
	shimCfg *NeighborhoodShimConfig,
	seedIndices []uint32,
	config *NeighborhoodConfig,
) (*NeighborhoodResult, error) {
	var shimResult *shim.NeighborhoodResult
	var err error

	shimMode := config.Mode.toShimMode()

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.NeighborhoodOnView(shimCfg.ShimView, seedIndices, config.Hops, shimMode)
	} else {
		shimResult, err = shimCfg.ShimGraph.Neighborhood(seedIndices, config.Hops, shimMode)
	}

	if err != nil {
		return nil, fmt.Errorf("shim.Neighborhood failed: %w", err)
	}

	// Build distance map and vertices by distance
	distanceMap := make(map[uint32]uint32, len(shimResult.Vertices))
	var verticesByDistance map[uint32][]uint32
	if config.GroupByDistance {
		verticesByDistance = make(map[uint32][]uint32)
	}

	for i, v := range shimResult.Vertices {
		dist := shimResult.Distances[i]
		distanceMap[v] = dist
		if config.GroupByDistance {
			verticesByDistance[dist] = append(verticesByDistance[dist], v)
		}
	}

	result := &NeighborhoodResult{
		Vertices:           shimResult.Vertices,
		Distances:          shimResult.Distances,
		DistanceMap:        distanceMap,
		NumVertices:        uint32(len(shimResult.Vertices)),
		VerticesByDistance: verticesByDistance,
		Meta:               make(map[string]string),
	}

	// Convert to external IDs if requested
	if config.ReturnExternalIDs {
		result.VerticesExternal = make([]uint64, len(shimResult.Vertices))
		for i, idx := range shimResult.Vertices {
			nodeID, _ := version.GetNodeID(idx)
			result.VerticesExternal[i] = nodeID
		}
	}

	result.Meta["source"] = "igraph"
	result.Meta["mode"] = config.Mode.String()
	result.Meta["hops"] = fmt.Sprintf("%d", config.Hops)

	return result, nil
}

// =============================================================================
// Helper methods on NeighborhoodResult
// =============================================================================

// GetDistance returns the distance of a vertex from the nearest seed.
// Returns (distance, true) if found, (0, false) if vertex not in neighborhood.
func (r *NeighborhoodResult) GetDistance(vertexIdx uint32) (uint32, bool) {
	dist, ok := r.DistanceMap[vertexIdx]
	return dist, ok
}

// Contains checks if a vertex is in the neighborhood.
func (r *NeighborhoodResult) Contains(vertexIdx uint32) bool {
	_, ok := r.DistanceMap[vertexIdx]
	return ok
}

// GetVerticesAtDistance returns all vertices at a specific distance from seeds.
func (r *NeighborhoodResult) GetVerticesAtDistance(distance uint32) []uint32 {
	if r.VerticesByDistance != nil {
		return r.VerticesByDistance[distance]
	}

	// Fall back to scanning if VerticesByDistance not populated
	result := make([]uint32, 0)
	for i, d := range r.Distances {
		if d == distance {
			result = append(result, r.Vertices[i])
		}
	}
	return result
}

// GetVerticesAtDistanceExternal returns external IDs of vertices at a specific distance.
func (r *NeighborhoodResult) GetVerticesAtDistanceExternal(distance uint32, version *GraphVersion) []uint64 {
	vertices := r.GetVerticesAtDistance(distance)
	result := make([]uint64, len(vertices))
	for i, idx := range vertices {
		nodeID, _ := version.GetNodeID(idx)
		result[i] = nodeID
	}
	return result
}

// GetImmediateNeighbors returns vertices at distance 1 (immediate neighbors of seeds).
func (r *NeighborhoodResult) GetImmediateNeighbors() []uint32 {
	return r.GetVerticesAtDistance(1)
}

// GetSeeds returns the seed vertices (at distance 0).
func (r *NeighborhoodResult) GetSeeds() []uint32 {
	return r.GetVerticesAtDistance(0)
}

// =============================================================================
// Validation functions
// =============================================================================

// ValidateNeighborhoodRequest validates a neighborhood request.
func ValidateNeighborhoodRequest(hops uint32, mode string) error {
	if hops == 0 {
		return fmt.Errorf("hops must be positive")
	}
	if hops > 100 {
		return fmt.Errorf("hops exceeds maximum allowed (100)")
	}

	switch mode {
	case "all", "out", "in", "":
		// valid
	default:
		return fmt.Errorf("unknown mode: %s (expected 'all', 'out', or 'in')", mode)
	}

	return nil
}

// ParseNeighborhoodMode parses a string to NeighborhoodMode.
func ParseNeighborhoodMode(s string) (NeighborhoodMode, error) {
	switch s {
	case "all", "":
		return NeighborhoodModeAll, nil
	case "out":
		return NeighborhoodModeOut, nil
	case "in":
		return NeighborhoodModeIn, nil
	default:
		return 0, fmt.Errorf("unknown mode: %s", s)
	}
}

// HashNeighborhoodParams generates a hash for neighborhood cache key normalization.
func HashNeighborhoodParams(seeds []uint64, hops uint32, mode string, viewHash string) string {
	// Sort seeds for consistent hashing
	sortedSeeds := make([]uint64, len(seeds))
	copy(sortedSeeds, seeds)
	// Simple bubble sort for typically small seed sets
	for i := 0; i < len(sortedSeeds)-1; i++ {
		for j := i + 1; j < len(sortedSeeds); j++ {
			if sortedSeeds[j] < sortedSeeds[i] {
				sortedSeeds[i], sortedSeeds[j] = sortedSeeds[j], sortedSeeds[i]
			}
		}
	}

	seedStr := ""
	for i, s := range sortedSeeds {
		if i > 0 {
			seedStr += ","
		}
		seedStr += fmt.Sprintf("%d", s)
	}

	return fmt.Sprintf("neighborhood:%s:%d:%s:%s", seedStr, hops, mode, viewHash)
}
