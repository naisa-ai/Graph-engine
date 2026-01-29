package service

import (
	"context"
	"fmt"
	"strconv"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
	"github.com/naisa-ai/graph-engine/internal/shim"
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

// CorridorConfig holds configuration for corridor computation.
type CorridorConfig struct {
	// ShimGraph is the shim graph to use (required for SP and KSP computations).
	ShimGraph *shim.Graph
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
	cfg *ShortestPathConfig, // required for shortest path computation
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
		return buildShortestPathHull(version, view, sourceID, targetID, sourceIdx, targetIdx, hops, cfg)
	case gepb.CorridorSpec_KSP_HULL:
		// Use default k=3 if not specified
		if k == 0 {
			k = 3
		}
		return buildKSPHull(version, view, sourceID, targetID, sourceIdx, targetIdx, hops, k, cfg)
	case gepb.CorridorSpec_COMMUNITY_AWARE:
		return buildCommunityAwareCorridor(version, view, sourceID, targetID, sourceIdx, targetIdx, hops, cfg)
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
	cfg *ShortestPathConfig,
) (*CorridorResult, error) {
	// Step 1: Find shortest path
	pathResult, err := ComputeShortestPath(version, baseView, sourceID, targetID, false, true, true, cfg)
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

// buildKSPHull creates a corridor by finding K shortest paths
// and then expanding by `hops` around all nodes in all paths.
func buildKSPHull(
	version *GraphVersion,
	baseView *View,
	sourceID, targetID uint64,
	sourceIdx, targetIdx uint32,
	hops uint32,
	k uint32,
	cfg *ShortestPathConfig,
) (*CorridorResult, error) {
	// Step 1: Compute K shortest paths
	kspConfig := DefaultKSPConfig(int(k))
	kspConfig.ReturnVertices = true
	kspConfig.ReturnEdges = true

	// Build KSP shim config from the shortest path config's shim graph
	kspShimCfg := &KSPShimConfig{ShimGraph: cfg.ShimGraph}

	kspResult, err := ComputeKShortestPaths(
		context.Background(),
		version,
		baseView,
		sourceID,
		targetID,
		kspConfig,
		kspShimCfg,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to compute K shortest paths: %w", err)
	}

	if kspResult.PathsFound == 0 {
		// No paths found, fall back to endpoint neighborhood expansion
		return buildEndpointNeighborhoodCorridor(version, baseView, sourceIdx, targetIdx, hops)
	}

	// Step 2: Collect union of all path vertices (as indices)
	pathVertexSet := make(map[uint32]bool)
	var primaryPath *ShortestPathResult

	for i, path := range kspResult.Paths {
		if path == nil || !path.Found {
			continue
		}

		// Keep the first path as the primary path summary
		if i == 0 {
			primaryPath = path
		}

		// Add all vertices from this path to the set
		for _, nodeID := range path.PathVertices {
			idx, ok := version.GetNodeIndex(nodeID)
			if ok {
				pathVertexSet[idx] = true
			}
		}
	}

	// Step 3: Convert vertex set to slice for neighborhood expansion
	pathVertexIndices := make([]uint32, 0, len(pathVertexSet))
	for idx := range pathVertexSet {
		pathVertexIndices = append(pathVertexIndices, idx)
	}

	// Step 4: Expand around all path vertices using BFS
	corridorVertices := expandNeighborhood(version, baseView, pathVertexIndices, hops)

	// Step 5: Create corridor view with these vertices
	corridorView := createCorridorView(version, baseView, corridorVertices)

	// Step 6: Compute explainability metadata
	meta := computeCorridorMeta(version, corridorView, primaryPath)
	meta["method"] = string(CorridorMethodKSPHull)
	meta["hops"] = strconv.Itoa(int(hops))
	meta["k"] = strconv.Itoa(int(k))
	meta["paths_found"] = strconv.Itoa(kspResult.PathsFound)
	meta["total_path_vertices"] = strconv.Itoa(len(pathVertexSet))

	if primaryPath != nil {
		meta["path_length"] = strconv.Itoa(len(primaryPath.PathVertices) - 1)
	}

	return &CorridorResult{
		View:        corridorView,
		Method:      CorridorMethodKSPHull,
		PathSummary: primaryPath,
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
	cfg *ShortestPathConfig,
) (*CorridorResult, error) {
	// Check if community data is available
	if version.HasCommunities() {
		return buildCommunityAwareCorridorWithCommunities(version, baseView, sourceID, targetID, sourceIdx, targetIdx, hops, cfg)
	}

	// Fall back to a hybrid approach: shortest path + endpoint neighborhoods
	pathResult, err := ComputeShortestPath(version, baseView, sourceID, targetID, false, true, true, cfg)
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

// buildCommunityAwareCorridorWithCommunities creates a corridor using precomputed
// community membership data. Includes all vertices from communities containing
// source and target, plus path vertices.
func buildCommunityAwareCorridorWithCommunities(
	version *GraphVersion,
	baseView *View,
	sourceID, targetID uint64,
	sourceIdx, targetIdx uint32,
	hops uint32,
	cfg *ShortestPathConfig,
) (*CorridorResult, error) {
	// Get communities of source and target
	srcComm, srcOk := version.GetVertexCommunity(sourceIdx)
	dstComm, dstOk := version.GetVertexCommunity(targetIdx)

	if !srcOk || !dstOk {
		// Fallback to endpoint expansion if community lookup fails
		return buildEndpointNeighborhoodCorridor(version, baseView, sourceIdx, targetIdx, hops)
	}

	// Collect all vertices in source and target communities
	corridorVertices := make(map[uint32]bool)
	for i, comm := range version.CommunityMembership {
		if comm == srcComm || comm == dstComm {
			corridorVertices[uint32(i)] = true
		}
	}

	// Also compute shortest path to include any intermediate vertices
	pathResult, err := ComputeShortestPath(version, baseView, sourceID, targetID, false, true, true, cfg)
	if err != nil {
		// Non-fatal: we can still build corridor from communities
		pathResult = nil
	}

	// Include path vertices if path was found
	if pathResult != nil && pathResult.Found {
		for _, nodeID := range pathResult.PathVertices {
			if idx, ok := version.GetNodeIndex(nodeID); ok {
				corridorVertices[idx] = true
			}
		}
	}

	// Optionally expand around corridor vertices by hops (if specified)
	if hops > 0 {
		seeds := make([]uint32, 0, len(corridorVertices))
		for v := range corridorVertices {
			seeds = append(seeds, v)
		}
		expanded := expandNeighborhood(version, baseView, seeds, hops)
		for v := range expanded {
			corridorVertices[v] = true
		}
	}

	// Create corridor view
	corridorView := createCorridorView(version, baseView, corridorVertices)

	// Compute metadata
	meta := computeCorridorMeta(version, corridorView, pathResult)
	meta["method"] = string(CorridorMethodCommunityAware)
	meta["hops"] = strconv.Itoa(int(hops))
	meta["source_community"] = strconv.Itoa(int(srcComm))
	meta["target_community"] = strconv.Itoa(int(dstComm))
	meta["communities_used"] = "true"

	if srcComm == dstComm {
		meta["same_community"] = "true"
	} else {
		meta["same_community"] = "false"
	}

	if pathResult != nil && pathResult.Found {
		meta["path_length"] = strconv.Itoa(len(pathResult.PathVertices) - 1)
	}

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
	cfg *ShortestPathConfig,
) (uint64, error) {
	// Get path length as rough estimate
	pathResult, err := ComputeShortestPath(version, view, sourceID, targetID, false, false, true, cfg)
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
