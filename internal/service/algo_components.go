package service

import (
	"fmt"
	"strconv"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// ComponentsConfig holds configuration for components computation.
type ComponentsConfig struct {
	// UseShim enables the igraph shim for components computation.
	// If false or if shim fails and FallbackOnError is true, uses pure Go.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	// If nil and UseShim is true, the function will return an error.
	ShimGraph *shim.Graph
}

// ComputeComponents computes connected components.
// If weak is true, computes weakly connected components (ignoring edge direction).
// If weak is false, computes strongly connected components (respecting direction).
//
// When cfg.UseShim is true and cfg.ShimGraph is provided, uses igraph via the C shim.
// Otherwise, or on shim error with FallbackOnError=true, uses pure Go implementation.
func ComputeComponents(version *GraphVersion, weak bool, cfg *ComponentsConfig) (*AlgoResult, error) {
	// Default config if nil
	if cfg == nil {
		cfg = &ComponentsConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
	}

	// Try igraph shim first if enabled
	if cfg.UseShim && cfg.ShimGraph != nil {
		result, err := computeComponentsShim(version, cfg.ShimGraph, weak)
		if err == nil {
			return result, nil
		}

		// Shim failed - fall back to Go implementation if configured
		if cfg.FallbackOnError {
			// Log the shim error and continue with fallback
			// In production, this would go to a proper logger
			_ = err // Suppress unused error warning; would log in production
		} else {
			return nil, fmt.Errorf("igraph shim components failed: %w", err)
		}
	}

	// FALLBACK: Pure Go implementation
	if weak {
		return computeWeakComponentsGo(version)
	}
	return computeStrongComponentsGo(version)
}

// computeComponentsShim computes components using the igraph C shim.
func computeComponentsShim(version *GraphVersion, g *shim.Graph, weak bool) (*AlgoResult, error) {
	shimResult, err := g.Components(weak)
	if err != nil {
		return nil, fmt.Errorf("shim.Components failed: %w", err)
	}

	// Convert shim result to AlgoResult
	mode := "weak"
	if !weak {
		mode = "strong"
	}
	paramsHash := HashParams(mode, version.ID)
	result := NewAlgoResult(version.ID, AlgoKindComponents, paramsHash)
	result.MembershipU32 = shimResult.Membership
	result.Meta["mode"] = mode
	result.Meta["num_components"] = strconv.FormatUint(uint64(shimResult.NumComponents), 10)
	result.Meta["source"] = "igraph"

	return result, nil
}

// =============================================================================
// FALLBACK: Pure Go implementation (not deleted when igraph available)
// =============================================================================

// computeWeakComponentsGo uses Union-Find to find weakly connected components.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeWeakComponentsGo(version *GraphVersion) (*AlgoResult, error) {
	n := int(version.VCount)
	if n == 0 {
		result := NewAlgoResult(version.ID, AlgoKindComponents, "weak")
		result.MembershipU32 = []uint32{}
		result.Meta["source"] = "go-fallback"
		return result, nil
	}

	// Initialize Union-Find
	uf := newUnionFind(n)

	// Union all edges (treat as undirected for weak components)
	for i := range version.EdgeSrc {
		src := int(version.EdgeSrc[i])
		dst := int(version.EdgeDst[i])
		uf.union(src, dst)
	}

	// Build membership array
	// Map each root to a component ID
	rootToComponent := make(map[int]uint32)
	var nextComponent uint32

	membership := make([]uint32, n)
	for i := 0; i < n; i++ {
		root := uf.find(i)
		if compID, exists := rootToComponent[root]; exists {
			membership[i] = compID
		} else {
			rootToComponent[root] = nextComponent
			membership[i] = nextComponent
			nextComponent++
		}
	}

	// Create result
	paramsHash := HashParams("weak", version.ID)
	result := NewAlgoResult(version.ID, AlgoKindComponents, paramsHash)
	result.MembershipU32 = membership
	result.Meta["mode"] = "weak"
	result.Meta["num_components"] = strconv.FormatUint(uint64(nextComponent), 10)
	result.Meta["source"] = "go-fallback"

	return result, nil
}

// computeStrongComponentsGo computes strongly connected components using Kosaraju's algorithm.
// FALLBACK: Pure Go implementation used when igraph shim is disabled or fails.
func computeStrongComponentsGo(version *GraphVersion) (*AlgoResult, error) {
	n := int(version.VCount)
	if n == 0 {
		result := NewAlgoResult(version.ID, AlgoKindComponents, "strong")
		result.MembershipU32 = []uint32{}
		result.Meta["source"] = "go-fallback"
		return result, nil
	}

	// Build adjacency lists
	adj := make([][]int, n)    // Forward edges
	adjRev := make([][]int, n) // Reverse edges
	for i := 0; i < n; i++ {
		adj[i] = make([]int, 0)
		adjRev[i] = make([]int, 0)
	}

	for i := range version.EdgeSrc {
		src := int(version.EdgeSrc[i])
		dst := int(version.EdgeDst[i])
		adj[src] = append(adj[src], dst)
		adjRev[dst] = append(adjRev[dst], src)
	}

	// Kosaraju's algorithm
	// Step 1: DFS on original graph to get finish order
	visited := make([]bool, n)
	finishOrder := make([]int, 0, n)

	var dfs1 func(v int)
	dfs1 = func(v int) {
		visited[v] = true
		for _, u := range adj[v] {
			if !visited[u] {
				dfs1(u)
			}
		}
		finishOrder = append(finishOrder, v)
	}

	for i := 0; i < n; i++ {
		if !visited[i] {
			dfs1(i)
		}
	}

	// Step 2: DFS on reversed graph in reverse finish order
	membership := make([]uint32, n)
	for i := range membership {
		membership[i] = ^uint32(0) // Mark as unassigned
	}
	var componentID uint32

	var dfs2 func(v int)
	dfs2 = func(v int) {
		membership[v] = componentID
		for _, u := range adjRev[v] {
			if membership[u] == ^uint32(0) {
				dfs2(u)
			}
		}
	}

	// Process in reverse finish order
	for i := n - 1; i >= 0; i-- {
		v := finishOrder[i]
		if membership[v] == ^uint32(0) {
			dfs2(v)
			componentID++
		}
	}

	// Create result
	paramsHash := HashParams("strong", version.ID)
	result := NewAlgoResult(version.ID, AlgoKindComponents, paramsHash)
	result.MembershipU32 = membership
	result.Meta["mode"] = "strong"
	result.Meta["num_components"] = strconv.FormatUint(uint64(componentID), 10)
	result.Meta["source"] = "go-fallback"

	return result, nil
}

// =============================================================================
// Union-Find data structure (used by Go fallback)
// =============================================================================

// unionFind implements the Union-Find (Disjoint Set Union) data structure
// with path compression and union by rank.
// FALLBACK: Used by pure Go weak components implementation.
type unionFind struct {
	parent []int
	rank   []int
}

func newUnionFind(n int) *unionFind {
	uf := &unionFind{
		parent: make([]int, n),
		rank:   make([]int, n),
	}
	for i := 0; i < n; i++ {
		uf.parent[i] = i
		uf.rank[i] = 0
	}
	return uf
}

// find returns the root of the set containing x, with path compression.
func (uf *unionFind) find(x int) int {
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x]) // Path compression
	}
	return uf.parent[x]
}

// union merges the sets containing x and y using union by rank.
func (uf *unionFind) union(x, y int) {
	rootX := uf.find(x)
	rootY := uf.find(y)

	if rootX == rootY {
		return
	}

	// Union by rank
	if uf.rank[rootX] < uf.rank[rootY] {
		uf.parent[rootX] = rootY
	} else if uf.rank[rootX] > uf.rank[rootY] {
		uf.parent[rootY] = rootX
	} else {
		uf.parent[rootY] = rootX
		uf.rank[rootX]++
	}
}

// =============================================================================
// Statistics helpers
// =============================================================================

// ComponentStats returns statistics about connected components.
type ComponentStats struct {
	NumComponents    int
	LargestSize      int
	SmallestSize     int
	AverageSize      float64
	IsolatedVertices int
}

// GetComponentStats computes statistics from a membership array.
func GetComponentStats(membership []uint32) ComponentStats {
	if len(membership) == 0 {
		return ComponentStats{}
	}

	// Count sizes
	sizes := make(map[uint32]int)
	for _, compID := range membership {
		sizes[compID]++
	}

	stats := ComponentStats{
		NumComponents: len(sizes),
	}

	var total int
	for _, size := range sizes {
		total += size
		if stats.LargestSize == 0 || size > stats.LargestSize {
			stats.LargestSize = size
		}
		if stats.SmallestSize == 0 || size < stats.SmallestSize {
			stats.SmallestSize = size
		}
		if size == 1 {
			stats.IsolatedVertices++
		}
	}

	if stats.NumComponents > 0 {
		stats.AverageSize = float64(total) / float64(stats.NumComponents)
	}

	return stats
}
