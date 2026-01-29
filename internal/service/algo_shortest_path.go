package service

import (
	"container/heap"
	"fmt"
	"math"

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
	// Source indicates the algorithm source ("igraph" or "go-fallback")
	Source string
}

// ShortestPathConfig holds configuration for shortest path computation.
type ShortestPathConfig struct {
	// UseShim enables the igraph shim for shortest path computation.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	ShimGraph *shim.Graph
}

// ComputeShortestPath computes the shortest path between two nodes.
// If view is nil, uses the full graph.
// If weighted is true, uses edge weights. Otherwise uses hop count.
//
// When cfg.UseShim is true and cfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go implementation.
func ComputeShortestPath(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	weighted bool,
	returnEdges, returnVertices bool,
	cfg *ShortestPathConfig,
) (*ShortestPathResult, error) {
	// Default config if nil
	if cfg == nil {
		cfg = &ShortestPathConfig{
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

	// Check if nodes are in view (if view is specified)
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return nil, fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return nil, fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Try igraph shim first if enabled
	if cfg.UseShim && cfg.ShimGraph != nil {
		result, err := computeShortestPathShim(
			version, cfg.ShimGraph, sourceIdx, targetIdx,
			weighted, returnEdges, returnVertices,
		)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if cfg.FallbackOnError {
			_ = err // Would log in production
		} else {
			return nil, fmt.Errorf("igraph shim shortest path failed: %w", err)
		}
	}

	// FALLBACK: Pure Go implementation
	return computeShortestPathGo(version, view, sourceIdx, targetIdx, weighted, returnEdges, returnVertices)
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
// FALLBACK: Pure Go implementation (not deleted when igraph available)
// =============================================================================

// computeShortestPathGo computes shortest path using pure Go BFS/Dijkstra.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeShortestPathGo(
	version *GraphVersion,
	view *View,
	sourceIdx, targetIdx uint32,
	weighted bool,
	returnEdges, returnVertices bool,
) (*ShortestPathResult, error) {
	// Build adjacency list
	adj := buildWeightedAdjacencyList(version, view)

	var path []uint32
	var edgeIndices []int
	var cost float64
	var found bool

	if weighted && version.EdgeWeight != nil {
		// Use Dijkstra for weighted graphs
		path, edgeIndices, cost, found = dijkstraGo(adj, sourceIdx, targetIdx)
	} else {
		// Use BFS for unweighted graphs
		path, edgeIndices, cost, found = bfsGo(adj, sourceIdx, targetIdx)
	}

	result := &ShortestPathResult{
		TotalCost: cost,
		Found:     found,
		Source:    "go-fallback",
	}

	if found {
		if returnVertices {
			result.PathVertices = make([]uint64, len(path))
			for i, idx := range path {
				nodeID, _ := version.GetNodeID(idx)
				result.PathVertices[i] = nodeID
			}
		}
		if returnEdges {
			result.PathEdges = edgeIndices
		}
	}

	return result, nil
}

// weightedEdge represents an edge with weight for adjacency list.
// FALLBACK: Used by pure Go shortest path implementation.
type weightedEdge struct {
	dst       uint32
	weight    float32
	edgeIndex int
}

// buildWeightedAdjacencyList builds an adjacency list from the graph.
// FALLBACK: Used by pure Go shortest path implementation.
func buildWeightedAdjacencyList(version *GraphVersion, view *View) map[uint32][]weightedEdge {
	adj := make(map[uint32][]weightedEdge)

	hasWeights := len(version.EdgeWeight) == len(version.EdgeSrc)

	for i := range version.EdgeSrc {
		// Skip if edge is not in view
		if view != nil && !view.ContainsEdge(i) {
			continue
		}

		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]

		var weight float32 = 1.0
		if hasWeights {
			weight = version.EdgeWeight[i]
		}

		edge := weightedEdge{
			dst:       dst,
			weight:    weight,
			edgeIndex: i,
		}
		adj[src] = append(adj[src], edge)

		// For undirected graphs, add reverse edge
		if !version.Directed {
			reverseEdge := weightedEdge{
				dst:       src,
				weight:    weight,
				edgeIndex: i,
			}
			adj[dst] = append(adj[dst], reverseEdge)
		}
	}

	return adj
}

// bfsGo performs breadth-first search for unweighted shortest path.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func bfsGo(adj map[uint32][]weightedEdge, source, target uint32) ([]uint32, []int, float64, bool) {
	if source == target {
		return []uint32{source}, nil, 0, true
	}

	// Parent map: node -> (parent node, edge index)
	parent := make(map[uint32]struct {
		node      uint32
		edgeIndex int
	})
	visited := make(map[uint32]bool)
	queue := []uint32{source}
	visited[source] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range adj[current] {
			if visited[edge.dst] {
				continue
			}

			parent[edge.dst] = struct {
				node      uint32
				edgeIndex int
			}{current, edge.edgeIndex}
			visited[edge.dst] = true

			if edge.dst == target {
				// Reconstruct path
				path, edges := reconstructPathGo(parent, source, target)
				return path, edges, float64(len(path) - 1), true
			}

			queue = append(queue, edge.dst)
		}
	}

	return nil, nil, math.Inf(1), false
}

// dijkstraNode represents a node in Dijkstra's priority queue.
// FALLBACK: Used by pure Go Dijkstra implementation.
type dijkstraNode struct {
	node     uint32
	distance float64
	index    int // index in heap
}

// dijkstraPQ is a priority queue for Dijkstra's algorithm.
// FALLBACK: Used by pure Go Dijkstra implementation.
type dijkstraPQ []*dijkstraNode

func (pq dijkstraPQ) Len() int { return len(pq) }

func (pq dijkstraPQ) Less(i, j int) bool {
	return pq[i].distance < pq[j].distance
}

func (pq dijkstraPQ) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
	pq[i].index = i
	pq[j].index = j
}

func (pq *dijkstraPQ) Push(x interface{}) {
	n := len(*pq)
	node := x.(*dijkstraNode)
	node.index = n
	*pq = append(*pq, node)
}

func (pq *dijkstraPQ) Pop() interface{} {
	old := *pq
	n := len(old)
	node := old[n-1]
	old[n-1] = nil
	node.index = -1
	*pq = old[0 : n-1]
	return node
}

// dijkstraGo performs Dijkstra's algorithm for weighted shortest path.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func dijkstraGo(adj map[uint32][]weightedEdge, source, target uint32) ([]uint32, []int, float64, bool) {
	if source == target {
		return []uint32{source}, nil, 0, true
	}

	// Distance map
	dist := make(map[uint32]float64)
	dist[source] = 0

	// Parent map: node -> (parent node, edge index)
	parent := make(map[uint32]struct {
		node      uint32
		edgeIndex int
	})

	// Priority queue
	pq := make(dijkstraPQ, 0)
	heap.Init(&pq)
	heap.Push(&pq, &dijkstraNode{node: source, distance: 0})

	// Visited set
	visited := make(map[uint32]bool)

	for pq.Len() > 0 {
		current := heap.Pop(&pq).(*dijkstraNode)

		if visited[current.node] {
			continue
		}
		visited[current.node] = true

		if current.node == target {
			// Reconstruct path
			path, edges := reconstructPathGo(parent, source, target)
			return path, edges, current.distance, true
		}

		for _, edge := range adj[current.node] {
			if visited[edge.dst] {
				continue
			}

			newDist := current.distance + float64(edge.weight)
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

// reconstructPathGo reconstructs the path from parent map.
// FALLBACK: Used by pure Go BFS/Dijkstra implementations.
func reconstructPathGo(parent map[uint32]struct {
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

// =============================================================================
// Legacy function signature for backward compatibility
// =============================================================================

// ComputeShortestPathLegacy is the old function signature without config.
// Deprecated: Use ComputeShortestPath with ShortestPathConfig instead.
func ComputeShortestPathLegacy(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	weighted bool,
	returnEdges, returnVertices bool,
) (*ShortestPathResult, error) {
	return ComputeShortestPath(version, view, sourceID, targetID, weighted, returnEdges, returnVertices, nil)
}
