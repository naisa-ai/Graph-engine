package service

import (
	"context"
	"fmt"
	"math"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// BFSMode specifies the traversal direction for BFS.
type BFSMode int

const (
	// BFSModeAll traverses both directions (undirected).
	BFSModeAll BFSMode = iota
	// BFSModeOut traverses outgoing edges only.
	BFSModeOut
	// BFSModeIn traverses incoming edges only.
	BFSModeIn
)

func (m BFSMode) String() string {
	switch m {
	case BFSModeAll:
		return "all"
	case BFSModeOut:
		return "out"
	case BFSModeIn:
		return "in"
	default:
		return "unknown"
	}
}

func (m BFSMode) toShimMode() shim.Mode {
	switch m {
	case BFSModeOut:
		return shim.ModeOut
	case BFSModeIn:
		return shim.ModeIn
	default:
		return shim.ModeAll
	}
}

// BFSResult contains the result of a breadth-first search.
type BFSResult struct {
	// Visited contains vertices in BFS traversal order.
	Visited []uint32

	// VisitedExternal contains external node IDs in BFS traversal order.
	VisitedExternal []uint64

	// Depths maps each visited vertex to its distance from source.
	Depths []uint32

	// Parents maps each visited vertex to its parent (-1 or MaxUint32 for source).
	Parents []uint32

	// DepthMap provides O(1) lookup of depth by vertex index.
	DepthMap map[uint32]uint32

	// NumVisited is the number of vertices visited.
	NumVisited uint32

	// MaxDepthReached is the maximum depth reached during traversal.
	MaxDepthReached uint32

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// BFSConfig contains configuration for BFS.
type BFSConfig struct {
	// MaxDepth limits the search depth (0 = unlimited).
	MaxDepth uint32

	// Mode specifies traversal direction.
	Mode BFSMode

	// ReturnExternalIDs indicates whether to populate VisitedExternal.
	ReturnExternalIDs bool
}

// BFSShimConfig holds shim configuration for BFS computation.
type BFSShimConfig struct {
	// UseShim enables the igraph shim for BFS.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultBFSConfig returns a BFSConfig with sensible defaults.
func DefaultBFSConfig() *BFSConfig {
	return &BFSConfig{
		MaxDepth:          0, // unlimited
		Mode:              BFSModeAll,
		ReturnExternalIDs: false,
	}
}

// ComputeBFS performs a breadth-first search from a source vertex.
//
// When shimCfg.UseShim is true and shimCfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go BFS.
func ComputeBFS(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID uint64,
	config *BFSConfig,
	shimCfg *BFSShimConfig,
) (*BFSResult, error) {
	if config == nil {
		config = DefaultBFSConfig()
	}

	// Default shim config if nil
	if shimCfg == nil {
		shimCfg = &BFSShimConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
	}

	// Convert external ID to internal index
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return nil, fmt.Errorf("source node not found: %d", sourceID)
	}

	// Check if source is in view
	if view != nil && !view.ContainsVertexByIndex(sourceIdx) {
		return nil, fmt.Errorf("source node not in view: %d", sourceID)
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Try igraph shim first if enabled
	if shimCfg.UseShim && shimCfg.ShimGraph != nil {
		result, err := computeBFSShim(version, shimCfg, sourceIdx, config)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if shimCfg.FallbackOnError {
			_ = err // Would log in production
		} else {
			return nil, fmt.Errorf("igraph shim BFS failed: %w", err)
		}
	}

	// FALLBACK: Pure Go BFS
	return computeBFSGo(ctx, version, view, sourceIdx, config)
}

// computeBFSShim performs BFS using the igraph C shim.
func computeBFSShim(
	version *GraphVersion,
	shimCfg *BFSShimConfig,
	sourceIdx uint32,
	config *BFSConfig,
) (*BFSResult, error) {
	var shimResult *shim.BFSResult
	var err error

	shimMode := config.Mode.toShimMode()

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.BFSOnView(shimCfg.ShimView, sourceIdx, config.MaxDepth, shimMode)
	} else {
		shimResult, err = shimCfg.ShimGraph.BFS(sourceIdx, config.MaxDepth, shimMode)
	}

	if err != nil {
		return nil, fmt.Errorf("shim.BFS failed: %w", err)
	}

	// Build depth map for O(1) lookup
	depthMap := make(map[uint32]uint32, len(shimResult.Visited))
	var maxDepthReached uint32
	for i, v := range shimResult.Visited {
		depth := shimResult.Depths[i]
		depthMap[v] = depth
		if depth > maxDepthReached {
			maxDepthReached = depth
		}
	}

	result := &BFSResult{
		Visited:         shimResult.Visited,
		Depths:          shimResult.Depths,
		Parents:         shimResult.Parents,
		DepthMap:        depthMap,
		NumVisited:      uint32(len(shimResult.Visited)),
		MaxDepthReached: maxDepthReached,
		Meta:            make(map[string]string),
	}

	// Convert to external IDs if requested
	if config.ReturnExternalIDs {
		result.VisitedExternal = make([]uint64, len(shimResult.Visited))
		for i, idx := range shimResult.Visited {
			nodeID, _ := version.GetNodeID(idx)
			result.VisitedExternal[i] = nodeID
		}
	}

	result.Meta["source"] = "igraph"
	result.Meta["mode"] = config.Mode.String()

	return result, nil
}

// =============================================================================
// FALLBACK: Pure Go BFS implementation
// =============================================================================

// computeBFSGo performs BFS using pure Go.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeBFSGo(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceIdx uint32,
	config *BFSConfig,
) (*BFSResult, error) {
	// Build adjacency list based on mode
	adj := buildBFSAdjacencyListGo(version, view, config.Mode)

	// BFS state
	visited := make([]uint32, 0, version.VCount)
	depths := make([]uint32, 0, version.VCount)
	parents := make([]uint32, 0, version.VCount)
	depthMap := make(map[uint32]uint32)
	parentMap := make(map[uint32]uint32)

	// Queue: (vertex, depth)
	type queueItem struct {
		vertex uint32
		depth  uint32
	}
	queue := []queueItem{{sourceIdx, 0}}
	depthMap[sourceIdx] = 0
	parentMap[sourceIdx] = math.MaxUint32 // source has no parent

	var maxDepthReached uint32

	for len(queue) > 0 {
		// Check context cancellation periodically
		if len(visited)%1000 == 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		current := queue[0]
		queue = queue[1:]

		// Skip if already visited (can happen with duplicate queue entries)
		if _, alreadyVisited := depthMap[current.vertex]; alreadyVisited && len(visited) > 0 {
			// Check if this is the source (which is pre-added to depthMap)
			found := false
			for _, v := range visited {
				if v == current.vertex {
					found = true
					break
				}
			}
			if found {
				continue
			}
		}

		// Add to visited
		visited = append(visited, current.vertex)
		depths = append(depths, current.depth)
		parents = append(parents, parentMap[current.vertex])

		if current.depth > maxDepthReached {
			maxDepthReached = current.depth
		}

		// Check depth limit
		if config.MaxDepth > 0 && current.depth >= config.MaxDepth {
			continue
		}

		// Explore neighbors
		for _, neighbor := range adj[current.vertex] {
			if _, seen := depthMap[neighbor]; !seen {
				depthMap[neighbor] = current.depth + 1
				parentMap[neighbor] = current.vertex
				queue = append(queue, queueItem{neighbor, current.depth + 1})
			}
		}
	}

	result := &BFSResult{
		Visited:         visited,
		Depths:          depths,
		Parents:         parents,
		DepthMap:        depthMap,
		NumVisited:      uint32(len(visited)),
		MaxDepthReached: maxDepthReached,
		Meta:            make(map[string]string),
	}

	// Convert to external IDs if requested
	if config.ReturnExternalIDs {
		result.VisitedExternal = make([]uint64, len(visited))
		for i, idx := range visited {
			nodeID, _ := version.GetNodeID(idx)
			result.VisitedExternal[i] = nodeID
		}
	}

	result.Meta["source"] = "go-fallback"
	result.Meta["mode"] = config.Mode.String()

	return result, nil
}

// buildBFSAdjacencyListGo builds an adjacency list for BFS based on traversal mode.
// FALLBACK: Used by pure Go BFS implementation.
func buildBFSAdjacencyListGo(version *GraphVersion, view *View, mode BFSMode) map[uint32][]uint32 {
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
		case BFSModeOut:
			// Only outgoing edges: src -> dst
			adj[srcIdx] = append(adj[srcIdx], dstIdx)
		case BFSModeIn:
			// Only incoming edges: dst -> src (reversed)
			adj[dstIdx] = append(adj[dstIdx], srcIdx)
		default: // BFSModeAll
			// Both directions (undirected)
			adj[srcIdx] = append(adj[srcIdx], dstIdx)
			adj[dstIdx] = append(adj[dstIdx], srcIdx)
		}
	}

	return adj
}

// =============================================================================
// Helper methods on BFSResult
// =============================================================================

// GetDepth returns the depth of a vertex from the source.
// Returns (depth, true) if found, (0, false) if vertex was not visited.
func (r *BFSResult) GetDepth(vertexIdx uint32) (uint32, bool) {
	depth, ok := r.DepthMap[vertexIdx]
	return depth, ok
}

// GetVerticesAtDepth returns all vertices at a specific depth.
func (r *BFSResult) GetVerticesAtDepth(depth uint32) []uint32 {
	result := make([]uint32, 0)
	for i, d := range r.Depths {
		if d == depth {
			result = append(result, r.Visited[i])
		}
	}
	return result
}

// GetVerticesAtDepthExternal returns external IDs of all vertices at a specific depth.
func (r *BFSResult) GetVerticesAtDepthExternal(depth uint32, version *GraphVersion) []uint64 {
	result := make([]uint64, 0)
	for i, d := range r.Depths {
		if d == depth {
			if nodeID, ok := version.GetNodeID(r.Visited[i]); ok {
				result = append(result, nodeID)
			}
		}
	}
	return result
}

// GetPath reconstructs the path from source to a target vertex.
// Returns nil if the target was not visited.
func (r *BFSResult) GetPath(targetIdx uint32) []uint32 {
	// Find target in visited list
	targetPos := -1
	for i, v := range r.Visited {
		if v == targetIdx {
			targetPos = i
			break
		}
	}
	if targetPos == -1 {
		return nil
	}

	// Build parent lookup from visited/parents arrays
	parentLookup := make(map[uint32]uint32)
	for i, v := range r.Visited {
		parentLookup[v] = r.Parents[i]
	}

	// Reconstruct path from target to source
	path := []uint32{targetIdx}
	current := targetIdx
	for {
		parent, exists := parentLookup[current]
		if !exists || parent == math.MaxUint32 {
			break
		}
		path = append([]uint32{parent}, path...)
		current = parent
	}

	return path
}

// WasVisited checks if a vertex was visited during BFS.
func (r *BFSResult) WasVisited(vertexIdx uint32) bool {
	_, ok := r.DepthMap[vertexIdx]
	return ok
}

// =============================================================================
// Validation functions
// =============================================================================

// ValidateBFSRequest validates a BFS request.
func ValidateBFSRequest(maxDepth uint32, mode string) error {
	switch mode {
	case "all", "out", "in", "":
		// valid
	default:
		return fmt.Errorf("unknown mode: %s (expected 'all', 'out', or 'in')", mode)
	}
	return nil
}

// ParseBFSMode parses a string to BFSMode.
func ParseBFSMode(s string) (BFSMode, error) {
	switch s {
	case "all", "":
		return BFSModeAll, nil
	case "out":
		return BFSModeOut, nil
	case "in":
		return BFSModeIn, nil
	default:
		return 0, fmt.Errorf("unknown mode: %s", s)
	}
}

// HashBFSParams generates a hash for BFS cache key normalization.
func HashBFSParams(src uint64, maxDepth uint32, mode string, viewHash string) string {
	return fmt.Sprintf("bfs:%d:%d:%s:%s", src, maxDepth, mode, viewHash)
}
