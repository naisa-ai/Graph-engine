package service

import (
	"fmt"
	"strconv"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// CorridorMethod represents the method used to construct a corridor.
type CorridorMethod string

const (
	CorridorMethodShortestPathHull CorridorMethod = "SHORTEST_PATH_HULL"
	CorridorMethodKSPHull          CorridorMethod = "KSP_HULL"
	CorridorMethodCommunityAware   CorridorMethod = "COMMUNITY_AWARE"
)

// CorridorResult contains the result of a corridor computation.
type CorridorResult struct {
	// View is the corridor view (filtered subgraph)
	View *View
	// Method is the method used to construct the corridor
	Method CorridorMethod
	// PathSummary contains the underlying shortest path (for SHORTEST_PATH_HULL)
	PathSummary *ShortestPathResult
	// Meta contains explainability metadata
	Meta map[string]string
}

// ComputeCorridor computes a corridor (focused subgraph) between two nodes.
// The corridor includes nodes and edges around the path between source and target.
func ComputeCorridor(
	version *GraphVersion,
	view *View, // optional: apply corridor to existing view
	sourceID, targetID uint64,
	method gepb.CorridorSpec_Method,
	hops uint32,
	k uint32, // for KSP_HULL
) (*CorridorResult, error) {
	// Validate source and target
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

	switch method {
	case gepb.CorridorSpec_SHORTEST_PATH_HULL:
		return buildShortestPathHull(version, view, sourceID, targetID, sourceIdx, targetIdx, hops)
	case gepb.CorridorSpec_KSP_HULL:
		// KSP_HULL requires K-shortest paths algorithm (Phase 4)
		// For now, fall back to SHORTEST_PATH_HULL with a warning
		result, err := buildShortestPathHull(version, view, sourceID, targetID, sourceIdx, targetIdx, hops)
		if err != nil {
			return nil, err
		}
		result.Meta["warning"] = "KSP_HULL not yet implemented, using SHORTEST_PATH_HULL"
		return result, nil
	case gepb.CorridorSpec_COMMUNITY_AWARE:
		return buildCommunityAwareCorridor(version, view, sourceID, targetID, sourceIdx, targetIdx, hops)
	default:
		return nil, fmt.Errorf("unknown corridor method: %v", method)
	}
}

// buildShortestPathHull creates a corridor by finding the shortest path
// and then expanding by `hops` around all nodes in the path.
func buildShortestPathHull(
	version *GraphVersion,
	baseView *View,
	sourceID, targetID uint64,
	sourceIdx, targetIdx uint32,
	hops uint32,
) (*CorridorResult, error) {
	// Step 1: Find shortest path
	pathResult, err := ComputeShortestPath(version, baseView, sourceID, targetID, false, true, true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shortest path: %w", err)
	}

	if !pathResult.Found {
		return nil, fmt.Errorf("no path found between %d and %d", sourceID, targetID)
	}

	// Step 2: Get path vertices as indices
	pathVertexIndices := make([]uint32, len(pathResult.PathVertices))
	for i, nodeID := range pathResult.PathVertices {
		idx, ok := version.GetNodeIndex(nodeID)
		if !ok {
			return nil, fmt.Errorf("path vertex not found: %d", nodeID)
		}
		pathVertexIndices[i] = idx
	}

	// Step 3: Expand around path vertices using BFS
	corridorVertices := expandNeighborhood(version, baseView, pathVertexIndices, hops)

	// Step 4: Create corridor view with these vertices
	corridorView := createCorridorView(version, baseView, corridorVertices)

	// Step 5: Compute explainability metadata
	meta := computeCorridorMeta(version, corridorView, pathResult)
	meta["method"] = string(CorridorMethodShortestPathHull)
	meta["hops"] = strconv.Itoa(int(hops))
	meta["path_length"] = strconv.Itoa(len(pathResult.PathVertices) - 1)

	return &CorridorResult{
		View:        corridorView,
		Method:      CorridorMethodShortestPathHull,
		PathSummary: pathResult,
		Meta:        meta,
	}, nil
}

// buildCommunityAwareCorridor creates a corridor using community information.
// If communities exist, includes all nodes from communities containing source/target.
// Falls back to neighborhood expansion if communities are not available.
func buildCommunityAwareCorridor(
	version *GraphVersion,
	baseView *View,
	sourceID, targetID uint64,
	sourceIdx, targetIdx uint32,
	hops uint32,
) (*CorridorResult, error) {
	// For now, we don't have precomputed community data stored in GraphVersion.
	// Fall back to a hybrid approach: shortest path + endpoint neighborhoods
	pathResult, err := ComputeShortestPath(version, baseView, sourceID, targetID, false, true, true, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to compute shortest path: %w", err)
	}

	if !pathResult.Found {
		// If no path found, just expand around endpoints
		return buildEndpointNeighborhoodCorridor(version, baseView, sourceIdx, targetIdx, hops)
	}

	// Get path vertices as indices
	pathVertexIndices := make([]uint32, len(pathResult.PathVertices))
	for i, nodeID := range pathResult.PathVertices {
		idx, ok := version.GetNodeIndex(nodeID)
		if !ok {
			return nil, fmt.Errorf("path vertex not found: %d", nodeID)
		}
		pathVertexIndices[i] = idx
	}

	// For community-aware, we expand with larger hops around endpoints
	// and smaller hops around intermediate path nodes
	corridorVertices := make(map[uint32]bool)

	// Expand around endpoints with full hops
	endpointHops := hops
	if endpointHops == 0 {
		endpointHops = 2 // default expansion
	}
	sourceNeighbors := bfsExpand(version, baseView, sourceIdx, endpointHops)
	targetNeighbors := bfsExpand(version, baseView, targetIdx, endpointHops)
	for v := range sourceNeighbors {
		corridorVertices[v] = true
	}
	for v := range targetNeighbors {
		corridorVertices[v] = true
	}

	// Include all path vertices
	for _, idx := range pathVertexIndices {
		corridorVertices[idx] = true
	}

	// Create corridor view
	corridorView := createCorridorView(version, baseView, corridorVertices)

	// Compute metadata
	meta := computeCorridorMeta(version, corridorView, pathResult)
	meta["method"] = string(CorridorMethodCommunityAware)
	meta["hops"] = strconv.Itoa(int(hops))
	meta["path_length"] = strconv.Itoa(len(pathResult.PathVertices) - 1)
	meta["note"] = "community data not available, using endpoint expansion"

	return &CorridorResult{
		View:        corridorView,
		Method:      CorridorMethodCommunityAware,
		PathSummary: pathResult,
		Meta:        meta,
	}, nil
}

// buildEndpointNeighborhoodCorridor creates a corridor by expanding around
// source and target endpoints when no path exists.
func buildEndpointNeighborhoodCorridor(
	version *GraphVersion,
	baseView *View,
	sourceIdx, targetIdx uint32,
	hops uint32,
) (*CorridorResult, error) {
	corridorVertices := make(map[uint32]bool)

	// Expand around both endpoints
	if hops == 0 {
		hops = 2
	}

	sourceNeighbors := bfsExpand(version, baseView, sourceIdx, hops)
	targetNeighbors := bfsExpand(version, baseView, targetIdx, hops)

	for v := range sourceNeighbors {
		corridorVertices[v] = true
	}
	for v := range targetNeighbors {
		corridorVertices[v] = true
	}

	// Create corridor view
	corridorView := createCorridorView(version, baseView, corridorVertices)

	// Compute metadata
	meta := computeCorridorMeta(version, corridorView, nil)
	meta["method"] = string(CorridorMethodCommunityAware)
	meta["hops"] = strconv.Itoa(int(hops))
	meta["note"] = "no path found, expanded around endpoints"

	return &CorridorResult{
		View:        corridorView,
		Method:      CorridorMethodCommunityAware,
		PathSummary: nil,
		Meta:        meta,
	}, nil
}

// expandNeighborhood expands around a set of seed vertices using BFS.
func expandNeighborhood(
	version *GraphVersion,
	baseView *View,
	seeds []uint32,
	hops uint32,
) map[uint32]bool {
	result := make(map[uint32]bool)

	// Add all seeds first
	for _, seed := range seeds {
		result[seed] = true
	}

	if hops == 0 {
		return result
	}

	// BFS expansion from all seeds simultaneously
	current := make(map[uint32]bool)
	for _, seed := range seeds {
		current[seed] = true
	}

	for hop := uint32(0); hop < hops; hop++ {
		next := make(map[uint32]bool)

		for v := range current {
			// Find neighbors
			for i := range version.EdgeSrc {
				if baseView != nil && !baseView.ContainsEdge(i) {
					continue
				}

				src := version.EdgeSrc[i]
				dst := version.EdgeDst[i]

				if src == v && !result[dst] {
					result[dst] = true
					next[dst] = true
				}
				// For undirected graphs or reverse edges
				if !version.Directed && dst == v && !result[src] {
					result[src] = true
					next[src] = true
				}
			}
		}

		current = next
	}

	return result
}

// bfsExpand expands from a single vertex using BFS.
func bfsExpand(
	version *GraphVersion,
	baseView *View,
	start uint32,
	hops uint32,
) map[uint32]bool {
	result := make(map[uint32]bool)
	result[start] = true

	if hops == 0 {
		return result
	}

	// Build adjacency list for efficiency
	adj := make(map[uint32][]uint32)
	for i := range version.EdgeSrc {
		if baseView != nil && !baseView.ContainsEdge(i) {
			continue
		}
		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]
		adj[src] = append(adj[src], dst)
		if !version.Directed {
			adj[dst] = append(adj[dst], src)
		}
	}

	// BFS
	current := []uint32{start}
	for hop := uint32(0); hop < hops && len(current) > 0; hop++ {
		var next []uint32
		for _, v := range current {
			for _, neighbor := range adj[v] {
				if !result[neighbor] {
					result[neighbor] = true
					next = append(next, neighbor)
				}
			}
		}
		current = next
	}

	return result
}

// createCorridorView creates a View containing only the specified vertices
// and edges between them.
func createCorridorView(
	version *GraphVersion,
	baseView *View,
	vertices map[uint32]bool,
) *View {
	// Start with a full view and then restrict it
	corridorView := NewView(version)

	// Clear the masks - we'll add only the vertices and edges we want
	corridorView.VertexMaskBitmap().Clear()
	corridorView.EdgeMaskBitmap().Clear()

	// Set vertex mask
	for v := range vertices {
		if v < uint32(version.VCount) {
			corridorView.VertexMaskBitmap().Add(v)
		}
	}

	// Set edge mask: include edges where both endpoints are in corridor
	for i := range version.EdgeSrc {
		// If base view exists, respect its mask
		if baseView != nil && !baseView.ContainsEdge(i) {
			continue
		}

		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]

		if vertices[src] && vertices[dst] {
			corridorView.EdgeMaskBitmap().Add(uint32(i))
		}
	}

	// Update counts
	corridorView.VCount = corridorView.VertexMaskBitmap().GetCardinality()
	corridorView.ECount = corridorView.EdgeMaskBitmap().GetCardinality()

	return corridorView
}

// computeCorridorMeta computes explainability metadata for a corridor.
func computeCorridorMeta(
	version *GraphVersion,
	corridorView *View,
	pathResult *ShortestPathResult,
) map[string]string {
	meta := make(map[string]string)

	// Basic counts
	meta["corridor_vcount"] = strconv.FormatUint(corridorView.VCount, 10)
	meta["corridor_ecount"] = strconv.FormatUint(corridorView.ECount, 10)

	// Corridor density (edges / max possible edges)
	if corridorView.VCount > 1 {
		maxEdges := corridorView.VCount * (corridorView.VCount - 1)
		if !version.Directed {
			maxEdges /= 2
		}
		density := float64(corridorView.ECount) / float64(maxEdges)
		meta["corridor_density"] = strconv.FormatFloat(density, 'f', 4, 64)
	}

	// Average degree in corridor
	if corridorView.VCount > 0 {
		avgDegree := float64(corridorView.ECount*2) / float64(corridorView.VCount)
		if version.Directed {
			avgDegree = float64(corridorView.ECount) / float64(corridorView.VCount)
		}
		meta["corridor_avg_degree"] = strconv.FormatFloat(avgDegree, 'f', 2, 64)
	}

	// Edge kind distribution
	edgeKindCounts := computeEdgeKindCounts(version, corridorView)
	totalKinds := 0
	for kind, count := range edgeKindCounts {
		meta[fmt.Sprintf("edge_kind_%d", kind)] = strconv.Itoa(count)
		totalKinds++
	}
	meta["edge_kind_types"] = strconv.Itoa(totalKinds)

	// Path summary if available
	if pathResult != nil && pathResult.Found {
		meta["path_found"] = "true"
		meta["path_cost"] = strconv.FormatFloat(pathResult.TotalCost, 'f', 2, 64)
		meta["path_vertices"] = strconv.Itoa(len(pathResult.PathVertices))
		meta["path_edges"] = strconv.Itoa(len(pathResult.PathEdges))

		// Compute path edges vs non-path edges ratio
		if len(pathResult.PathEdges) > 0 && corridorView.ECount > 0 {
			pathEdgeRatio := float64(len(pathResult.PathEdges)) / float64(corridorView.ECount)
			meta["path_edge_ratio"] = strconv.FormatFloat(pathEdgeRatio, 'f', 4, 64)
		}
	} else {
		meta["path_found"] = "false"
	}

	// Compression ratio (corridor size vs full graph)
	if version.VCount > 0 && version.ECount > 0 {
		vertexCompression := float64(corridorView.VCount) / float64(version.VCount)
		edgeCompression := float64(corridorView.ECount) / float64(version.ECount)
		meta["vertex_compression"] = strconv.FormatFloat(vertexCompression, 'f', 4, 64)
		meta["edge_compression"] = strconv.FormatFloat(edgeCompression, 'f', 4, 64)
	}

	return meta
}

// computeEdgeKindCounts counts edges by kind in a view.
func computeEdgeKindCounts(version *GraphVersion, view *View) map[uint32]int {
	counts := make(map[uint32]int)

	hasKinds := len(version.EdgeKind) == len(version.EdgeSrc)

	for i := range version.EdgeSrc {
		if !view.ContainsEdge(i) {
			continue
		}

		var kind uint32 = 0 // default kind
		if hasKinds {
			kind = version.EdgeKind[i]
		}
		counts[kind]++
	}

	return counts
}

// ValidateCorridorSpec validates a CorridorSpec.
func ValidateCorridorSpec(spec *gepb.CorridorSpec) error {
	if spec == nil {
		return fmt.Errorf("corridor spec is nil")
	}
	if spec.GetSourceU64() == 0 && spec.GetTargetU64() == 0 {
		return fmt.Errorf("source and target must be specified")
	}
	return nil
}

// EstimateCorridorSize estimates the number of edges in a corridor
// without actually building it. Useful for size checks before running mincut.
func EstimateCorridorSize(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	hops uint32,
) (uint64, error) {
	// Get path length as rough estimate
	pathResult, err := ComputeShortestPath(version, view, sourceID, targetID, false, false, true, nil)
	if err != nil {
		return 0, err
	}

	if !pathResult.Found {
		// Estimate based on endpoint neighborhoods only
		sourceIdx, _ := version.GetNodeIndex(sourceID)
		targetIdx, _ := version.GetNodeIndex(targetID)

		sourceNeighbors := bfsExpand(version, view, sourceIdx, hops)
		targetNeighbors := bfsExpand(version, view, targetIdx, hops)

		// Rough estimate: vertices * average_degree
		totalVertices := len(sourceNeighbors) + len(targetNeighbors)
		avgDegree := float64(version.ECount) / float64(version.VCount)
		return uint64(float64(totalVertices) * avgDegree / 2), nil
	}

	// Estimate based on path + expansion
	pathLen := len(pathResult.PathVertices)
	// Each path vertex can expand to approximately (avg_degree)^hops vertices
	avgDegree := float64(version.ECount) / float64(version.VCount)
	if avgDegree < 1 {
		avgDegree = 1
	}

	expansionFactor := 1.0
	for i := uint32(0); i < hops; i++ {
		expansionFactor *= avgDegree
	}

	estimatedVertices := float64(pathLen) * expansionFactor
	estimatedEdges := estimatedVertices * avgDegree / 2

	return uint64(estimatedEdges), nil
}
