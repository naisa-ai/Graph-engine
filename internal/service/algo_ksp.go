package service

import (
	"container/heap"
	"context"
	"fmt"
	"math"
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
	// UseShim enables the igraph shim for KSP computation.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	ShimGraph *shim.Graph
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

// ComputeKShortestPaths computes K shortest paths using Yen's algorithm.
//
// When shimCfg.UseShim is true and shimCfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go Yen's algorithm.
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

	// Default shim config if nil
	if shimCfg == nil {
		shimCfg = &KSPShimConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
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

	// Try igraph shim first if enabled
	if shimCfg.UseShim && shimCfg.ShimGraph != nil {
		result, err := computeKSPShim(version, shimCfg.ShimGraph, sourceIdx, targetIdx, config)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if shimCfg.FallbackOnError {
			_ = err // Would log in production
		} else {
			return nil, fmt.Errorf("igraph shim KSP failed: %w", err)
		}
	}

	// FALLBACK: Pure Go Yen's algorithm
	return computeKSPGo(ctx, version, view, sourceIdx, targetIdx, config)
}

// computeKSPShim computes K shortest paths using the igraph C shim.
func computeKSPShim(
	version *GraphVersion,
	g *shim.Graph,
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

	shimResult, err := g.KShortestPaths(sourceIdx, targetIdx, uint32(config.K), weights, uint32(config.MaxCandidates))
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

	result.Meta["status"] = "complete"
	result.Meta["source"] = "igraph"

	return result, nil
}

// =============================================================================
// FALLBACK: Pure Go Yen's algorithm (not deleted when igraph available)
// =============================================================================

// candidatePath represents a candidate path in Yen's algorithm.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
type candidatePath struct {
	path      []uint32 // internal node indices
	edges     []int    // edge indices
	cost      float64
	spurIndex int // index of the spur node in the root path
	index     int // heap index
}

// candidateHeap is a min-heap of candidate paths ordered by cost.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
type candidateHeap []*candidatePath

func (h candidateHeap) Len() int           { return len(h) }
func (h candidateHeap) Less(i, j int) bool { return h[i].cost < h[j].cost }
func (h candidateHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *candidateHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*candidatePath)
	item.index = n
	*h = append(*h, item)
}

func (h *candidateHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[0 : n-1]
	return item
}

// computeKSPGo computes K shortest paths using pure Go Yen's algorithm.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeKSPGo(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceIdx, targetIdx uint32,
	config *KSPConfig,
) (*KSPResult, error) {
	// Build adjacency list with optional diversity penalties
	adj := buildWeightedAdjacencyList(version, view)

	result := &KSPResult{
		Paths: make([]*ShortestPathResult, 0, config.K),
		Meta:  make(map[string]string),
	}

	// Step 1: Find the first shortest path
	path, edges, cost, found := findShortestPathInternalGo(adj, sourceIdx, targetIdx, config.Weighted, nil)
	if !found {
		result.Meta["status"] = "no_path"
		result.Meta["source"] = "go-fallback"
		return result, nil
	}

	// Add first path
	firstPath := &ShortestPathResult{
		TotalCost: cost,
		Found:     true,
		Source:    "go-fallback",
	}
	if config.ReturnVertices {
		firstPath.PathVertices = convertToExternalIDsGo(version, path)
	}
	if config.ReturnEdges {
		firstPath.PathEdges = edges
	}
	result.Paths = append(result.Paths, firstPath)
	result.PathsFound = 1

	// If K=1, we're done
	if config.K == 1 {
		result.Meta["status"] = "complete"
		result.Meta["source"] = "go-fallback"
		return result, nil
	}

	// Step 2: Yen's algorithm - find K-1 more paths
	candidates := &candidateHeap{}
	heap.Init(candidates)

	// Track all found paths to avoid duplicates
	foundPaths := make(map[string]bool)
	foundPaths[pathKeyGo(path)] = true

	// Track edge usage for diversity penalties
	edgeUsage := make(map[int]int)
	for _, e := range edges {
		edgeUsage[e]++
	}

	// Keep track of the last added path (A[k-1] in Yen's notation)
	kPaths := [][]uint32{path}
	kEdges := [][]int{edges}

	for k := 1; k < config.K; k++ {
		// Check context cancellation
		select {
		case <-ctx.Done():
			result.Meta["status"] = "timeout"
			result.Meta["source"] = "go-fallback"
			return result, nil
		default:
		}

		// Get the (k-1)th shortest path
		lastPath := kPaths[k-1]
		lastEdges := kEdges[k-1]

		// For each spur node in the (k-1)th path
		for i := 0; i < len(lastPath)-1; i++ {
			spurNode := lastPath[i]
			rootPath := lastPath[:i+1]
			rootEdges := lastEdges[:i]

			// Create a modified adjacency list that excludes:
			// 1. Edges that would lead to the same path prefix as existing k-paths
			// 2. Nodes in the root path (except spur node)
			excludedEdges := make(map[int]bool)
			excludedNodes := make(map[uint32]bool)

			// Exclude nodes in root path (except spur node and source)
			for j := 0; j < i; j++ {
				excludedNodes[rootPath[j]] = true
			}

			// Exclude edges that share the same root path with any k-path
			for pi, existingPath := range kPaths {
				existingEdges := kEdges[pi]
				if len(existingPath) > i && pathPrefixMatchesGo(existingPath[:i+1], rootPath) {
					// Exclude the edge from spur node in this path
					if i < len(existingEdges) {
						excludedEdges[existingEdges[i]] = true
					}
				}
			}

			// Apply diversity penalty if configured
			var penalties map[int]float64
			if config.DiversityPenalty > 1.0 {
				penalties = make(map[int]float64)
				for e, count := range edgeUsage {
					if count > 0 {
						penalties[e] = math.Pow(config.DiversityPenalty, float64(count))
					}
				}
			}

			// Find shortest path from spur node to target, avoiding excluded nodes/edges
			spurPath, spurEdges, _, spurFound := findShortestPathWithExclusionsGo(
				adj, spurNode, targetIdx, config.Weighted, excludedNodes, excludedEdges, penalties,
			)

			if spurFound {
				// Construct total path: rootPath + spurPath[1:]
				totalPath := make([]uint32, len(rootPath)+len(spurPath)-1)
				copy(totalPath, rootPath)
				copy(totalPath[len(rootPath):], spurPath[1:])

				totalEdges := make([]int, len(rootEdges)+len(spurEdges))
				copy(totalEdges, rootEdges)
				copy(totalEdges[len(rootEdges):], spurEdges)

				// Calculate total cost
				totalCost := computePathCostGo(adj, totalPath, totalEdges, config.Weighted)

				// Check if this path is new
				key := pathKeyGo(totalPath)
				if !foundPaths[key] {
					foundPaths[key] = true
					candidate := &candidatePath{
						path:      totalPath,
						edges:     totalEdges,
						cost:      totalCost,
						spurIndex: i,
					}

					// Add to candidates (with limit)
					if candidates.Len() < config.MaxCandidates {
						heap.Push(candidates, candidate)
					} else if candidates.Len() > 0 && (*candidates)[0].cost > totalCost {
						// Replace worst candidate if this is better
						heap.Pop(candidates)
						heap.Push(candidates, candidate)
					}
					result.CandidatesExplored++
				}
			}
		}

		// If no candidates, we're done
		if candidates.Len() == 0 {
			break
		}

		// Get the best candidate
		best := heap.Pop(candidates).(*candidatePath)
		kPaths = append(kPaths, best.path)
		kEdges = append(kEdges, best.edges)

		// Update edge usage for diversity
		for _, e := range best.edges {
			edgeUsage[e]++
		}

		// Add to results
		pathResult := &ShortestPathResult{
			TotalCost: best.cost,
			Found:     true,
			Source:    "go-fallback",
		}
		if config.ReturnVertices {
			pathResult.PathVertices = convertToExternalIDsGo(version, best.path)
		}
		if config.ReturnEdges {
			pathResult.PathEdges = best.edges
		}
		result.Paths = append(result.Paths, pathResult)
		result.PathsFound++
	}

	result.Meta["status"] = "complete"
	result.Meta["source"] = "go-fallback"
	result.Meta["candidates_explored"] = fmt.Sprintf("%d", result.CandidatesExplored)
	return result, nil
}

// findShortestPathInternalGo finds shortest path using internal indices.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func findShortestPathInternalGo(
	adj map[uint32][]weightedEdge,
	source, target uint32,
	weighted bool,
	penalties map[int]float64,
) ([]uint32, []int, float64, bool) {
	return findShortestPathWithExclusionsGo(adj, source, target, weighted, nil, nil, penalties)
}

// findShortestPathWithExclusionsGo finds shortest path avoiding certain nodes/edges.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func findShortestPathWithExclusionsGo(
	adj map[uint32][]weightedEdge,
	source, target uint32,
	weighted bool,
	excludedNodes map[uint32]bool,
	excludedEdges map[int]bool,
	penalties map[int]float64,
) ([]uint32, []int, float64, bool) {
	if source == target {
		return []uint32{source}, nil, 0, true
	}

	// Parent map: node -> (parent node, edge index)
	parent := make(map[uint32]struct {
		node      uint32
		edgeIndex int
	})

	if weighted {
		return dijkstraWithExclusionsGo(adj, source, target, excludedNodes, excludedEdges, parent, penalties)
	}
	return bfsWithExclusionsGo(adj, source, target, excludedNodes, excludedEdges, parent)
}

// bfsWithExclusionsGo performs BFS avoiding excluded nodes/edges.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func bfsWithExclusionsGo(
	adj map[uint32][]weightedEdge,
	source, target uint32,
	excludedNodes map[uint32]bool,
	excludedEdges map[int]bool,
	parent map[uint32]struct {
		node      uint32
		edgeIndex int
	},
) ([]uint32, []int, float64, bool) {
	visited := make(map[uint32]bool)
	queue := []uint32{source}
	visited[source] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range adj[current] {
			// Skip excluded nodes and edges
			if excludedNodes != nil && excludedNodes[edge.dst] {
				continue
			}
			if excludedEdges != nil && excludedEdges[edge.edgeIndex] {
				continue
			}
			if visited[edge.dst] {
				continue
			}

			parent[edge.dst] = struct {
				node      uint32
				edgeIndex int
			}{current, edge.edgeIndex}
			visited[edge.dst] = true

			if edge.dst == target {
				path, edges := reconstructPathFromParentGo(parent, source, target)
				return path, edges, float64(len(path) - 1), true
			}

			queue = append(queue, edge.dst)
		}
	}

	return nil, nil, math.Inf(1), false
}

// dijkstraWithExclusionsGo performs Dijkstra avoiding excluded nodes/edges.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func dijkstraWithExclusionsGo(
	adj map[uint32][]weightedEdge,
	source, target uint32,
	excludedNodes map[uint32]bool,
	excludedEdges map[int]bool,
	parent map[uint32]struct {
		node      uint32
		edgeIndex int
	},
	penalties map[int]float64,
) ([]uint32, []int, float64, bool) {
	dist := make(map[uint32]float64)
	dist[source] = 0

	pq := make(dijkstraPQ, 0)
	heap.Init(&pq)
	heap.Push(&pq, &dijkstraNode{node: source, distance: 0})

	visited := make(map[uint32]bool)

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*dijkstraNode)

		if visited[current.node] {
			continue
		}
		visited[current.node] = true

		if current.node == target {
			path, edges := reconstructPathFromParentGo(parent, source, target)
			return path, edges, current.distance, true
		}

		for _, edge := range adj[current.node] {
			// Skip excluded nodes and edges
			if excludedNodes != nil && excludedNodes[edge.dst] {
				continue
			}
			if excludedEdges != nil && excludedEdges[edge.edgeIndex] {
				continue
			}
			if visited[edge.dst] {
				continue
			}

			// Apply penalty if configured
			weight := float64(edge.weight)
			if penalties != nil {
				if penalty, ok := penalties[edge.edgeIndex]; ok {
					weight *= penalty
				}
			}

			newDist := current.distance + weight
			if oldDist, exists := dist[edge.dst]; !exists || newDist < oldDist {
				dist[edge.dst] = newDist
				parent[edge.dst] = struct {
					node      uint32
					edgeIndex int
				}{current.node, edge.edgeIndex}
				heap.Push(&pq, &dijkstraNode{node: edge.dst, distance: newDist})
			}
		}
	}

	return nil, nil, math.Inf(1), false
}

// reconstructPathFromParentGo reconstructs path from parent map.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func reconstructPathFromParentGo(parent map[uint32]struct {
	node      uint32
	edgeIndex int
}, source, target uint32) ([]uint32, []int) {
	path := []uint32{target}
	edges := []int{}
	current := target

	for current != source {
		p := parent[current]
		path = append([]uint32{p.node}, path...)
		edges = append([]int{p.edgeIndex}, edges...)
		current = p.node
	}

	return path, edges
}

// pathKeyGo generates a unique key for a path.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func pathKeyGo(path []uint32) string {
	key := ""
	for i, node := range path {
		if i > 0 {
			key += "-"
		}
		key += fmt.Sprintf("%d", node)
	}
	return key
}

// pathPrefixMatchesGo checks if two paths have the same prefix.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func pathPrefixMatchesGo(path1, path2 []uint32) bool {
	if len(path1) != len(path2) {
		return false
	}
	for i := range path1 {
		if path1[i] != path2[i] {
			return false
		}
	}
	return true
}

// computePathCostGo computes the total cost of a path.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func computePathCostGo(adj map[uint32][]weightedEdge, path []uint32, edges []int, weighted bool) float64 {
	if !weighted {
		return float64(len(path) - 1) // hop count
	}

	// Build edge lookup for fast weight access
	edgeWeights := make(map[int]float32)
	for _, neighbors := range adj {
		for _, e := range neighbors {
			edgeWeights[e.edgeIndex] = e.weight
		}
	}

	var cost float64
	for _, e := range edges {
		cost += float64(edgeWeights[e])
	}
	return cost
}

// convertToExternalIDsGo converts internal node indices to external IDs.
// FALLBACK: Used by pure Go Yen's algorithm implementation.
func convertToExternalIDsGo(version *GraphVersion, path []uint32) []uint64 {
	result := make([]uint64, len(path))
	for i, idx := range path {
		nodeID, _ := version.GetNodeID(idx)
		result[i] = nodeID
	}
	return result
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

// =============================================================================
// Legacy function signature for backward compatibility
// =============================================================================

// ComputeKShortestPathsLegacy is the old function signature without shim config.
// Deprecated: Use ComputeKShortestPaths with KSPShimConfig instead.
func ComputeKShortestPathsLegacy(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	config *KSPConfig,
) (*KSPResult, error) {
	return ComputeKShortestPaths(ctx, version, view, sourceID, targetID, config, nil)
}
