package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// MinCutResult contains the result of an s-t minimum cut computation.
type MinCutResult struct {
	// CutValue is the total capacity of edges in the minimum cut
	CutValue float64
	// SourceSideVertices contains the external node IDs on the source side of the cut
	SourceSideVertices []uint64
	// CutEdges contains the edge indices that form the cut
	CutEdges []uint64
	// Meta contains additional information about the computation
	Meta map[string]string
}

// MinCut configuration constants
const (
	// DefaultMaxCorridorEdges is the maximum number of edges allowed for mincut
	DefaultMaxCorridorEdges = 10000
	// DefaultMinCutTimeout is the default timeout for mincut computation
	DefaultMinCutTimeout = 30 * time.Second
)

// MinCutConfig holds configuration for mincut computation.
type MinCutConfig struct {
	// UseShim enables the igraph shim for mincut computation.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	ShimGraph *shim.Graph
}

// ComputeSTMinCut computes the minimum s-t cut.
// The cut separates source from target with minimum total edge capacity.
// If useWeights is false, all edges have capacity 1.
//
// When cfg.UseShim is true and cfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go Edmonds-Karp.
func ComputeSTMinCut(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	useWeights bool,
	maxEdges int,
	cfg *MinCutConfig,
) (*MinCutResult, error) {
	// Default config if nil
	if cfg == nil {
		cfg = &MinCutConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
	}

	// Validate inputs
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

	// Check view size constraint
	if maxEdges <= 0 {
		maxEdges = DefaultMaxCorridorEdges
	}

	edgeCount := countViewEdges(version, view)
	if edgeCount > maxEdges {
		return nil, fmt.Errorf("view has %d edges, exceeds maximum %d for mincut", edgeCount, maxEdges)
	}

	// Try igraph shim first if enabled
	if cfg.UseShim && cfg.ShimGraph != nil {
		result, err := computeSTMinCutShim(version, cfg.ShimGraph, sourceIdx, targetIdx, useWeights, edgeCount)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if cfg.FallbackOnError {
			_ = err // Would log in production
		} else {
			return nil, fmt.Errorf("igraph shim mincut failed: %w", err)
		}
	}

	// FALLBACK: Pure Go Edmonds-Karp implementation
	return computeSTMinCutGo(ctx, version, view, sourceIdx, targetIdx, sourceID, targetID, useWeights, edgeCount)
}

// computeSTMinCutShim computes minimum s-t cut using the igraph C shim.
func computeSTMinCutShim(
	version *GraphVersion,
	g *shim.Graph,
	sourceIdx, targetIdx uint32,
	useWeights bool,
	edgeCount int,
) (*MinCutResult, error) {
	// Prepare capacities if needed
	var capacity []float64
	if useWeights && len(version.EdgeWeight) > 0 {
		capacity = make([]float64, len(version.EdgeWeight))
		for i, w := range version.EdgeWeight {
			capacity[i] = float64(w)
			if capacity[i] <= 0 {
				capacity[i] = 1.0 // Ensure positive capacity
			}
		}
	}

	shimResult, err := g.STMinCut(sourceIdx, targetIdx, capacity)
	if err != nil {
		return nil, fmt.Errorf("shim.STMinCut failed: %w", err)
	}

	// Convert source side indices to external IDs
	sourceSideIDs := make([]uint64, len(shimResult.SourceSide))
	sourceSideMap := make(map[uint32]bool)
	for i, idx := range shimResult.SourceSide {
		nodeID, _ := version.GetNodeID(idx)
		sourceSideIDs[i] = nodeID
		sourceSideMap[idx] = true
	}

	// Convert cut edges
	cutEdges := make([]uint64, len(shimResult.CutEdges))
	for i, idx := range shimResult.CutEdges {
		cutEdges[i] = uint64(idx)
	}

	// Build metadata
	meta := make(map[string]string)
	meta["source"] = "igraph"
	meta["source_side_count"] = strconv.Itoa(len(shimResult.SourceSide))
	meta["cut_edges_count"] = strconv.Itoa(len(cutEdges))
	meta["view_edges"] = strconv.Itoa(edgeCount)
	meta["weighted"] = strconv.FormatBool(useWeights)
	meta["cut_value"] = strconv.FormatFloat(shimResult.CutValue, 'f', 4, 64)

	return &MinCutResult{
		CutValue:           shimResult.CutValue,
		SourceSideVertices: sourceSideIDs,
		CutEdges:           cutEdges,
		Meta:               meta,
	}, nil
}

// =============================================================================
// FALLBACK: Pure Go Edmonds-Karp implementation (not deleted when igraph available)
// =============================================================================

// computeSTMinCutGo computes minimum s-t cut using pure Go Edmonds-Karp.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeSTMinCutGo(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceIdx, targetIdx uint32,
	sourceID, targetID uint64,
	useWeights bool,
	edgeCount int,
) (*MinCutResult, error) {
	// Build flow network
	network := buildFlowNetworkGo(version, view, useWeights)

	// Run Edmonds-Karp algorithm
	maxFlow, residual, err := edmondsKarpGo(ctx, network, sourceIdx, targetIdx)
	if err != nil {
		return nil, fmt.Errorf("edmonds-karp failed: %w", err)
	}

	// Find cut edges from residual graph
	sourceSide := findSourceSideGo(residual, sourceIdx)
	cutEdges := findCutEdgesGo(version, view, sourceSide)

	// Convert source side indices to external IDs
	sourceSideIDs := make([]uint64, 0, len(sourceSide))
	for idx := range sourceSide {
		if nodeID, ok := version.GetNodeID(idx); ok {
			sourceSideIDs = append(sourceSideIDs, nodeID)
		}
	}

	// Build metadata with explainability information
	meta := computeMinCutMetaGo(version, view, sourceSide, cutEdges, maxFlow, edgeCount, useWeights)
	meta["source"] = "go-fallback"

	return &MinCutResult{
		CutValue:           maxFlow,
		SourceSideVertices: sourceSideIDs,
		CutEdges:           cutEdges,
		Meta:               meta,
	}, nil
}

// computeMinCutMetaGo computes explainability metadata for a mincut result.
// FALLBACK: Used by pure Go implementation.
func computeMinCutMetaGo(
	version *GraphVersion,
	view *View,
	sourceSide map[uint32]bool,
	cutEdges []uint64,
	cutValue float64,
	edgeCount int,
	useWeights bool,
) map[string]string {
	meta := make(map[string]string)

	// Basic counts
	sourceSideCount := len(sourceSide)
	meta["source_side_count"] = strconv.Itoa(sourceSideCount)
	meta["cut_edges_count"] = strconv.Itoa(len(cutEdges))
	meta["view_edges"] = strconv.Itoa(edgeCount)
	meta["weighted"] = strconv.FormatBool(useWeights)
	meta["cut_value"] = strconv.FormatFloat(cutValue, 'f', 4, 64)

	// Compute target side count
	totalVertices := int(version.VCount)
	if view != nil {
		totalVertices = int(view.VCount)
	}
	targetSideCount := totalVertices - sourceSideCount
	meta["target_side_count"] = strconv.Itoa(targetSideCount)

	// Partition balance (0 = perfectly balanced, 1 = maximally imbalanced)
	if totalVertices > 0 {
		minSide := sourceSideCount
		if targetSideCount < minSide {
			minSide = targetSideCount
		}
		balance := 1.0 - (2.0 * float64(minSide) / float64(totalVertices))
		meta["partition_imbalance"] = strconv.FormatFloat(balance, 'f', 4, 64)
	}

	// Average cut edge capacity
	if len(cutEdges) > 0 {
		avgCapacity := cutValue / float64(len(cutEdges))
		meta["avg_cut_edge_capacity"] = strconv.FormatFloat(avgCapacity, 'f', 4, 64)
	}

	// Cut sparsity (cut edges / possible cross edges)
	if sourceSideCount > 0 && targetSideCount > 0 {
		maxCrossEdges := sourceSideCount * targetSideCount
		sparsity := float64(len(cutEdges)) / float64(maxCrossEdges)
		meta["cut_sparsity"] = strconv.FormatFloat(sparsity, 'f', 6, 64)
	}

	// Normalized cut value (cut value / total edge capacity in view)
	if edgeCount > 0 {
		totalCapacity := float64(edgeCount) // For unweighted
		if useWeights && len(version.EdgeWeight) > 0 {
			totalCapacity = 0
			for i := range version.EdgeSrc {
				if view != nil && !view.ContainsEdge(i) {
					continue
				}
				if i < len(version.EdgeWeight) {
					totalCapacity += float64(version.EdgeWeight[i])
				} else {
					totalCapacity += 1.0
				}
			}
		}
		if totalCapacity > 0 {
			normalizedCut := cutValue / totalCapacity
			meta["normalized_cut"] = strconv.FormatFloat(normalizedCut, 'f', 4, 64)
		}
	}

	return meta
}

// flowEdge represents an edge in the flow network.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
type flowEdge struct {
	to       uint32
	capacity float64
	flow     float64
	reverse  int // index of reverse edge in adjacency list
}

// flowNetwork represents a flow network for max-flow computation.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
type flowNetwork struct {
	adj [][]flowEdge
	n   int
}

// countViewEdges counts the number of edges in a view.
func countViewEdges(version *GraphVersion, view *View) int {
	if view == nil {
		return len(version.EdgeSrc)
	}
	return int(view.ECount)
}

// buildFlowNetworkGo constructs a flow network from the graph.
// For undirected graphs, creates edges in both directions.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
func buildFlowNetworkGo(version *GraphVersion, view *View, useWeights bool) *flowNetwork {
	n := int(version.VCount)
	network := &flowNetwork{
		adj: make([][]flowEdge, n),
		n:   n,
	}

	// Initialize adjacency lists
	for i := range network.adj {
		network.adj[i] = make([]flowEdge, 0)
	}

	hasWeights := useWeights && len(version.EdgeWeight) == len(version.EdgeSrc)

	for i := range version.EdgeSrc {
		// Skip if not in view
		if view != nil && !view.ContainsEdge(i) {
			continue
		}

		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]

		var capacity float64 = 1.0
		if hasWeights {
			capacity = float64(version.EdgeWeight[i])
			if capacity <= 0 {
				capacity = 1.0 // Ensure positive capacity
			}
		}

		// Add forward edge
		srcIdx := int(src)
		dstIdx := int(dst)

		forwardIdx := len(network.adj[srcIdx])
		reverseIdx := len(network.adj[dstIdx])

		// Forward edge
		network.adj[srcIdx] = append(network.adj[srcIdx], flowEdge{
			to:       dst,
			capacity: capacity,
			flow:     0,
			reverse:  reverseIdx,
		})

		// Reverse edge (for residual graph)
		// For undirected graphs, reverse has same capacity
		// For directed graphs, reverse has 0 capacity
		reverseCapacity := 0.0
		if !version.Directed {
			reverseCapacity = capacity
		}

		network.adj[dstIdx] = append(network.adj[dstIdx], flowEdge{
			to:       src,
			capacity: reverseCapacity,
			flow:     0,
			reverse:  forwardIdx,
		})
	}

	return network
}

// edmondsKarpGo implements the Edmonds-Karp algorithm (BFS-based Ford-Fulkerson).
// Returns the maximum flow value and the residual graph.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func edmondsKarpGo(ctx context.Context, network *flowNetwork, source, target uint32) (float64, *flowNetwork, error) {
	maxFlow := 0.0

	for {
		// Check context for cancellation/timeout
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		default:
		}

		// Find augmenting path using BFS
		path, pathFlow := bfsFindPathGo(network, source, target)
		if path == nil {
			// No more augmenting paths
			break
		}

		// Augment flow along the path
		maxFlow += pathFlow

		// Update residual capacities
		current := source
		for _, edgeIdx := range path {
			edge := &network.adj[current][edgeIdx]
			reverseEdge := &network.adj[edge.to][edge.reverse]

			edge.flow += pathFlow
			reverseEdge.flow -= pathFlow

			current = edge.to
		}
	}

	return maxFlow, network, nil
}

// bfsFindPathGo finds an augmenting path using BFS.
// Returns the path as edge indices and the bottleneck flow.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
func bfsFindPathGo(network *flowNetwork, source, target uint32) ([]int, float64) {
	n := network.n
	parent := make([]int, n)     // parent[v] = index of node that leads to v
	parentEdge := make([]int, n) // parentEdge[v] = edge index in parent's adj list
	visited := make([]bool, n)

	for i := range parent {
		parent[i] = -1
		parentEdge[i] = -1
	}

	queue := []uint32{source}
	visited[source] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for edgeIdx, edge := range network.adj[current] {
			// Check if edge has residual capacity
			residual := edge.capacity - edge.flow
			if residual <= 0 || visited[edge.to] {
				continue
			}

			visited[edge.to] = true
			parent[edge.to] = int(current)
			parentEdge[edge.to] = edgeIdx

			if edge.to == target {
				// Found path to target, reconstruct it
				return reconstructFlowPathGo(parent, parentEdge, source, target, network)
			}

			queue = append(queue, edge.to)
		}
	}

	return nil, 0
}

// reconstructFlowPathGo reconstructs the path and finds bottleneck flow.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
func reconstructFlowPathGo(parent, parentEdge []int, source, target uint32, network *flowNetwork) ([]int, float64) {
	path := []int{}
	pathFlow := math.Inf(1)

	current := target
	for current != source {
		p := uint32(parent[current])
		edgeIdx := parentEdge[current]

		path = append([]int{edgeIdx}, path...)

		edge := network.adj[p][edgeIdx]
		residual := edge.capacity - edge.flow
		if residual < pathFlow {
			pathFlow = residual
		}

		current = p
	}

	return path, pathFlow
}

// findSourceSideGo finds all vertices reachable from source in residual graph.
// These are the vertices on the source side of the minimum cut.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
func findSourceSideGo(network *flowNetwork, source uint32) map[uint32]bool {
	sourceSide := make(map[uint32]bool)
	visited := make([]bool, network.n)

	queue := []uint32{source}
	visited[source] = true
	sourceSide[source] = true

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		for _, edge := range network.adj[current] {
			// Can only traverse edges with positive residual capacity
			residual := edge.capacity - edge.flow
			if residual <= 0 || visited[edge.to] {
				continue
			}

			visited[edge.to] = true
			sourceSide[edge.to] = true
			queue = append(queue, edge.to)
		}
	}

	return sourceSide
}

// findCutEdgesGo finds the edges that form the minimum cut.
// These are edges from source side to non-source side.
// FALLBACK: Used by pure Go Edmonds-Karp implementation.
func findCutEdgesGo(version *GraphVersion, view *View, sourceSide map[uint32]bool) []uint64 {
	cutEdges := []uint64{}

	for i := range version.EdgeSrc {
		if view != nil && !view.ContainsEdge(i) {
			continue
		}

		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]

		// Edge is in cut if src is on source side and dst is not
		if sourceSide[src] && !sourceSide[dst] {
			cutEdges = append(cutEdges, uint64(i))
		}

		// For undirected graphs, also check reverse direction
		if !version.Directed && sourceSide[dst] && !sourceSide[src] {
			cutEdges = append(cutEdges, uint64(i))
		}
	}

	return cutEdges
}

// =============================================================================
// Validation and convenience functions
// =============================================================================

// ValidateMinCutRequest validates parameters for a mincut request.
func ValidateMinCutRequest(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	maxEdges int,
) error {
	// Check source exists
	sourceIdx, ok := version.GetNodeIndex(sourceID)
	if !ok {
		return fmt.Errorf("source node not found: %d", sourceID)
	}

	// Check target exists
	targetIdx, ok := version.GetNodeIndex(targetID)
	if !ok {
		return fmt.Errorf("target node not found: %d", targetID)
	}

	// Check source != target
	if sourceIdx == targetIdx {
		return fmt.Errorf("source and target must be different")
	}

	// Check nodes are in view
	if view != nil {
		if !view.ContainsVertexByIndex(sourceIdx) {
			return fmt.Errorf("source node not in view: %d", sourceID)
		}
		if !view.ContainsVertexByIndex(targetIdx) {
			return fmt.Errorf("target node not in view: %d", targetID)
		}
	}

	// Check edge count
	if maxEdges <= 0 {
		maxEdges = DefaultMaxCorridorEdges
	}
	edgeCount := countViewEdges(version, view)
	if edgeCount > maxEdges {
		return fmt.Errorf("view has %d edges, exceeds maximum %d", edgeCount, maxEdges)
	}

	return nil
}

// ComputeSTMinCutWithTimeout wraps ComputeSTMinCut with a timeout.
func ComputeSTMinCutWithTimeout(
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	useWeights bool,
	maxEdges int,
	timeout time.Duration,
	cfg *MinCutConfig,
) (*MinCutResult, error) {
	if timeout <= 0 {
		timeout = DefaultMinCutTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return ComputeSTMinCut(ctx, version, view, sourceID, targetID, useWeights, maxEdges, cfg)
}

// =============================================================================
// Legacy function signature for backward compatibility
// =============================================================================

// ComputeSTMinCutLegacy is the old function signature without config.
// Deprecated: Use ComputeSTMinCut with MinCutConfig instead.
func ComputeSTMinCutLegacy(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	sourceID, targetID uint64,
	useWeights bool,
	maxEdges int,
) (*MinCutResult, error) {
	return ComputeSTMinCut(ctx, version, view, sourceID, targetID, useWeights, maxEdges, nil)
}
