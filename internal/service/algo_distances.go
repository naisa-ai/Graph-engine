package service

import (
	"container/heap"
	"fmt"
	"math"
)

// DistancesResult contains the result of a distances computation.
type DistancesResult struct {
	// Distances is a flattened matrix of distances (row-major order)
	// Index = sourceIndex * len(targets) + targetIndex
	Distances []float64
	// Sources are the source node IDs (external)
	Sources []uint64
	// Targets are the target node IDs (external)
	Targets []uint64
	// NumSources is the number of source nodes
	NumSources int
	// NumTargets is the number of target nodes
	NumTargets int
}

// GetDistance returns the distance from source index to target index.
func (r *DistancesResult) GetDistance(sourceIdx, targetIdx int) float64 {
	if sourceIdx < 0 || sourceIdx >= r.NumSources || targetIdx < 0 || targetIdx >= r.NumTargets {
		return math.Inf(1)
	}
	return r.Distances[sourceIdx*r.NumTargets+targetIdx]
}

// ComputeDistances computes distances from multiple sources to multiple targets.
// If view is nil, uses the full graph.
// If weighted is false, uses hop count.
func ComputeDistances(
	version *GraphVersion,
	view *View,
	sourceIDs, targetIDs []uint64,
	weighted bool,
) (*DistancesResult, error) {
	if len(sourceIDs) == 0 {
		return nil, fmt.Errorf("at least one source is required")
	}
	if len(targetIDs) == 0 {
		return nil, fmt.Errorf("at least one target is required")
	}

	// Convert source IDs to indices
	sourceIndices := make([]uint32, 0, len(sourceIDs))
	validSourceIDs := make([]uint64, 0, len(sourceIDs))
	for _, id := range sourceIDs {
		idx, ok := version.GetNodeIndex(id)
		if !ok {
			continue // Skip missing nodes
		}
		if view != nil && !view.ContainsVertexByIndex(idx) {
			continue // Skip nodes not in view
		}
		sourceIndices = append(sourceIndices, idx)
		validSourceIDs = append(validSourceIDs, id)
	}

	if len(sourceIndices) == 0 {
		return nil, fmt.Errorf("no valid sources found in graph/view")
	}

	// Convert target IDs to indices
	targetIndices := make([]uint32, 0, len(targetIDs))
	validTargetIDs := make([]uint64, 0, len(targetIDs))
	targetSet := make(map[uint32]int) // index -> position in targets
	for _, id := range targetIDs {
		idx, ok := version.GetNodeIndex(id)
		if !ok {
			continue
		}
		if view != nil && !view.ContainsVertexByIndex(idx) {
			continue
		}
		targetSet[idx] = len(targetIndices)
		targetIndices = append(targetIndices, idx)
		validTargetIDs = append(validTargetIDs, id)
	}

	if len(targetIndices) == 0 {
		return nil, fmt.Errorf("no valid targets found in graph/view")
	}

	// Build adjacency list
	adj := buildWeightedAdjacencyList(version, view)

	// Initialize result matrix with infinity
	numSources := len(sourceIndices)
	numTargets := len(targetIndices)
	distances := make([]float64, numSources*numTargets)
	for i := range distances {
		distances[i] = math.Inf(1)
	}

	// Compute distances from each source
	for srcI, srcIdx := range sourceIndices {
		var dist map[uint32]float64
		if weighted && version.EdgeWeight != nil {
			dist = dijkstraDistances(adj, srcIdx, targetSet)
		} else {
			dist = bfsDistances(adj, srcIdx, targetSet)
		}

		// Fill in distances for this source
		for tgtIdx, tgtPos := range targetSet {
			if d, ok := dist[tgtIdx]; ok {
				distances[srcI*numTargets+tgtPos] = d
			}
		}
	}

	return &DistancesResult{
		Distances:  distances,
		Sources:    validSourceIDs,
		Targets:    validTargetIDs,
		NumSources: numSources,
		NumTargets: numTargets,
	}, nil
}

// bfsDistances computes distances from a single source using BFS.
// Returns distances to all nodes in targetSet.
func bfsDistances(adj map[uint32][]weightedEdge, source uint32, targetSet map[uint32]int) map[uint32]float64 {
	dist := make(map[uint32]float64)
	dist[source] = 0

	visited := make(map[uint32]bool)
	visited[source] = true
	queue := []uint32{source}
	level := float64(0)

	// Track remaining targets
	remaining := len(targetSet)
	if _, isTarget := targetSet[source]; isTarget {
		remaining--
	}

	for len(queue) > 0 && remaining > 0 {
		// Process all nodes at current level
		levelSize := len(queue)
		level++

		for i := 0; i < levelSize; i++ {
			current := queue[0]
			queue = queue[1:]

			for _, edge := range adj[current] {
				if visited[edge.dst] {
					continue
				}

				visited[edge.dst] = true
				dist[edge.dst] = level
				queue = append(queue, edge.dst)

				if _, isTarget := targetSet[edge.dst]; isTarget {
					remaining--
				}
			}
		}
	}

	return dist
}

// dijkstraDistances computes distances from a single source using Dijkstra.
// Returns distances to all nodes in targetSet.
func dijkstraDistances(adj map[uint32][]weightedEdge, source uint32, targetSet map[uint32]int) map[uint32]float64 {
	dist := make(map[uint32]float64)
	dist[source] = 0

	// Priority queue
	pq := make(dijkstraPQ, 0)
	heap.Init(&pq)
	heap.Push(&pq, &dijkstraNode{node: source, distance: 0})

	// Visited set
	visited := make(map[uint32]bool)

	// Track remaining targets
	remaining := len(targetSet)
	if _, isTarget := targetSet[source]; isTarget {
		remaining--
	}

	for pq.Len() > 0 && remaining > 0 {
		current := heap.Pop(&pq).(*dijkstraNode)

		if visited[current.node] {
			continue
		}
		visited[current.node] = true

		// Check if this is a target
		if _, isTarget := targetSet[current.node]; isTarget && current.node != source {
			remaining--
		}

		for _, edge := range adj[current.node] {
			if visited[edge.dst] {
				continue
			}

			newDist := current.distance + float64(edge.weight)
			if oldDist, exists := dist[edge.dst]; !exists || newDist < oldDist {
				dist[edge.dst] = newDist
				heap.Push(&pq, &dijkstraNode{node: edge.dst, distance: newDist})
			}
		}
	}

	return dist
}

// ComputeAllPairsDistances computes distances between all pairs of vertices.
// Warning: This can be expensive for large graphs - O(V * (V + E) log V) for weighted.
func ComputeAllPairsDistances(
	version *GraphVersion,
	view *View,
	weighted bool,
) (*DistancesResult, error) {
	// Get all vertices
	var vertices []uint64
	if view != nil {
		vertices = view.GetVertices()
	} else {
		vertices = version.GetAllNodeIDs()
	}

	return ComputeDistances(version, view, vertices, vertices, weighted)
}

// ComputeSingleSourceDistances computes distances from a single source to all reachable nodes.
func ComputeSingleSourceDistances(
	version *GraphVersion,
	view *View,
	sourceID uint64,
	weighted bool,
) (map[uint64]float64, error) {
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return nil, fmt.Errorf("source node not found: %d", sourceID)
	}

	if view != nil && !view.ContainsVertexByIndex(sourceIdx) {
		return nil, fmt.Errorf("source node not in view: %d", sourceID)
	}

	// Build adjacency list
	adj := buildWeightedAdjacencyList(version, view)

	// Compute distances to all nodes
	allNodes := make(map[uint32]int)
	if view != nil {
		it := view.VertexMaskBitmap().Iterator()
		for it.HasNext() {
			idx := it.Next()
			allNodes[idx] = int(idx)
		}
	} else {
		for i := 0; i < len(version.indexToNodeID); i++ {
			allNodes[uint32(i)] = i
		}
	}

	var dist map[uint32]float64
	if weighted && version.EdgeWeight != nil {
		dist = dijkstraDistances(adj, sourceIdx, allNodes)
	} else {
		dist = bfsDistances(adj, sourceIdx, allNodes)
	}

	// Convert to external IDs
	result := make(map[uint64]float64)
	for idx, d := range dist {
		if nodeID, ok := version.GetNodeID(idx); ok {
			result[nodeID] = d
		}
	}

	return result, nil
}
