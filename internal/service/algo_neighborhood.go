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
	// UseShim enables the igraph shim for neighborhood queries.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
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

// ComputeNeighborhood finds all vertices within a specified number of hops from seed vertices.
//
// When shimCfg.UseShim is true and shimCfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go BFS-based neighborhood.
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

	// Default shim config if nil
	if shimCfg == nil {
		shimCfg = &NeighborhoodShimConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
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
	}

	// Try igraph shim first if enabled
	if shimCfg.UseShim && shimCfg.ShimGraph != nil {
		result, err := computeNeighborhoodShim(version, shimCfg, seedIndices, config)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if shimCfg.FallbackOnError {
			_ = err // Would log in production
		} else {
			return nil, fmt.Errorf("igraph shim neighborhood failed: %w", err)
		}
	}

	// FALLBACK: Pure Go BFS-based neighborhood
	return computeNeighborhoodGo(ctx, version, view, seedIndices, config)
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
// FALLBACK: Pure Go BFS-based neighborhood implementation
// =============================================================================

// computeNeighborhoodGo computes neighborhood using pure Go BFS.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeNeighborhoodGo(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	seedIndices []uint32,
	config *NeighborhoodConfig,
) (*NeighborhoodResult, error) {
	// Build adjacency list based on mode
	adj := buildNeighborhoodAdjacencyListGo(version, view, config.Mode)

	// Track visited vertices and their distances
	distanceMap := make(map[uint32]uint32)
	var verticesByDistance map[uint32][]uint32
	if config.GroupByDistance {
		verticesByDistance = make(map[uint32][]uint32)
	}

	// Initialize with seeds at distance 0
	type queueItem struct {
		vertex   uint32
		distance uint32
	}
	queue := make([]queueItem, 0, len(seedIndices))

	for _, seed := range seedIndices {
		if _, seen := distanceMap[seed]; !seen {
			distanceMap[seed] = 0
			queue = append(queue, queueItem{seed, 0})
			if config.GroupByDistance {
				verticesByDistance[0] = append(verticesByDistance[0], seed)
			}
		}
	}

	// BFS to find neighborhood
	for len(queue) > 0 {
		// Check context cancellation periodically
		if len(distanceMap)%1000 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		current := queue[0]
		queue = queue[1:]

		// Check hop limit
		if current.distance >= config.Hops {
			continue
		}

		// Explore neighbors
		for _, neighbor := range adj[current.vertex] {
			if _, seen := distanceMap[neighbor]; !seen {
				newDist := current.distance + 1
				distanceMap[neighbor] = newDist
				queue = append(queue, queueItem{neighbor, newDist})
				if config.GroupByDistance {
					verticesByDistance[newDist] = append(verticesByDistance[newDist], neighbor)
				}
			}
		}
	}

	// Build result arrays
	vertices := make([]uint32, 0, len(distanceMap))
	distances := make([]uint32, 0, len(distanceMap))

	// Iterate in consistent order (by distance, then by vertex)
	for dist := uint32(0); dist <= config.Hops; dist++ {
		if config.GroupByDistance {
			for _, v := range verticesByDistance[dist] {
				vertices = append(vertices, v)
				distances = append(distances, dist)
			}
		} else {
			for v, d := range distanceMap {
				if d == dist {
					vertices = append(vertices, v)
					distances = append(distances, d)
				}
			}
		}
	}

	result := &NeighborhoodResult{
		Vertices:           vertices,
		Distances:          distances,
		DistanceMap:        distanceMap,
		NumVertices:        uint32(len(vertices)),
		VerticesByDistance: verticesByDistance,
		Meta:               make(map[string]string),
	}

	// Convert to external IDs if requested
	if config.ReturnExternalIDs {
		result.VerticesExternal = make([]uint64, len(vertices))
		for i, idx := range vertices {
			nodeID, _ := version.GetNodeID(idx)
			result.VerticesExternal[i] = nodeID
		}
	}

	result.Meta["source"] = "go-fallback"
	result.Meta["mode"] = config.Mode.String()
	result.Meta["hops"] = fmt.Sprintf("%d", config.Hops)

	return result, nil
}

// buildNeighborhoodAdjacencyListGo builds an adjacency list for neighborhood based on traversal mode.
// FALLBACK: Used by pure Go neighborhood implementation.
func buildNeighborhoodAdjacencyListGo(version *GraphVersion, view *View, mode NeighborhoodMode) map[uint32][]uint32 {
	adj := make(map[uint32][]uint32)

	for i, srcIdx := range version.EdgeSrc {
		dstIdx := version.EdgeDst[i]

		// Apply view mask if present
		if view != nil {
			if !view.ContainsEdge(i) {
				continue
			}
			if !view.ContainsVertexByIndex(srcIdx) || !view.ContainsVertexByIndex(dstIdx) {
				continue
			}
		}

		switch mode {
		case NeighborhoodModeOut:
			// Only outgoing edges: src -> dst
			adj[srcIdx] = append(adj[srcIdx], dstIdx)
		case NeighborhoodModeIn:
			// Only incoming edges: dst -> src (reversed)
			adj[dstIdx] = append(adj[dstIdx], srcIdx)
		default: // NeighborhoodModeAll
			// Both directions (undirected)
			adj[srcIdx] = append(adj[srcIdx], dstIdx)
			adj[dstIdx] = append(adj[dstIdx], srcIdx)
		}
	}

	return adj
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
