package service

import (
	"context"
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForBFS creates a shim graph for BFS testing.
func createShimGraphForBFS(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createPathGraphForBFS creates a simple path: 1 - 2 - 3 - 4 - 5
// This gives predictable depths from any source.
func createPathGraphForBFS() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-path-bfs",
		GraphName:   "path-bfs",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "path-bfs", false, build)
	return version
}

// createTreeGraphForBFS creates a binary tree structure.
// Graph structure:
//
//	    1
//	   / \
//	  2   3
//	 / \   \
//	4   5   6
func createTreeGraphForBFS() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6}
	src := []uint64{1, 1, 2, 2, 3}
	dst := []uint64{2, 3, 4, 5, 6}

	build := &Build{
		ID:          "build-tree-bfs",
		GraphName:   "tree-bfs",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "tree-bfs", false, build)
	return version
}

// createDirectedGraphForBFS creates a directed graph for testing modes.
// Graph structure (directed):
//
//	1 -> 2 -> 3
//	     |
//	     v
//	     4 -> 5
func createDirectedGraphForBFS() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 2, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-directed-bfs",
		GraphName:   "directed-bfs",
		Directed:    true,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "directed-bfs", true, build)
	return version
}

// createStarGraphForBFS creates a star graph with hub 1.
// Graph structure: 1 connected to 2, 3, 4, 5
func createStarGraphForBFS() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 1, 1, 1}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-star-bfs",
		GraphName:   "star-bfs",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "star-bfs", false, build)
	return version
}

func TestComputeBFS_Basic(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()
	config.ReturnExternalIDs = true

	// BFS from vertex 1
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// Should visit all 5 vertices
	if result.NumVisited != 5 {
		t.Errorf("expected 5 visited vertices, got %d", result.NumVisited)
	}

	// Verify external IDs are populated
	if len(result.VisitedExternal) != int(result.NumVisited) {
		t.Errorf("expected %d external IDs, got %d", result.NumVisited, len(result.VisitedExternal))
	}

	// Max depth should be 4 (path length 1->2->3->4->5)
	if result.MaxDepthReached != 4 {
		t.Errorf("expected max depth 4, got %d", result.MaxDepthReached)
	}

	t.Logf("BFS from 1: visited=%d, max_depth=%d", result.NumVisited, result.MaxDepthReached)
	t.Logf("Visited: %v", result.VisitedExternal)
}

func TestComputeBFS_MaxDepth(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	// Limit to depth 2
	config := &BFSConfig{
		MaxDepth:          2,
		Mode:              BFSModeAll,
		ReturnExternalIDs: true,
	}

	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// From vertex 1 with depth 2, should visit: 1, 2, 3 (3 vertices)
	if result.NumVisited != 3 {
		t.Errorf("expected 3 visited vertices with max_depth=2, got %d", result.NumVisited)
	}

	// Max depth should be 2
	if result.MaxDepthReached > 2 {
		t.Errorf("expected max depth <= 2, got %d", result.MaxDepthReached)
	}

	t.Logf("BFS with max_depth=2: visited=%d, max_depth_reached=%d", result.NumVisited, result.MaxDepthReached)
	t.Logf("Visited: %v", result.VisitedExternal)
}

func TestComputeBFS_TreeFromRoot(t *testing.T) {
	version := createTreeGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := &BFSConfig{
		MaxDepth:          0, // unlimited
		Mode:              BFSModeAll,
		ReturnExternalIDs: true,
	}

	// BFS from root (vertex 1)
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// Should visit all 6 vertices
	if result.NumVisited != 6 {
		t.Errorf("expected 6 visited vertices, got %d", result.NumVisited)
	}

	// Depth 0: vertex 1
	// Depth 1: vertices 2, 3
	// Depth 2: vertices 4, 5, 6
	if result.MaxDepthReached != 2 {
		t.Errorf("expected max depth 2 for tree, got %d", result.MaxDepthReached)
	}

	t.Logf("BFS from tree root: visited=%d, max_depth=%d", result.NumVisited, result.MaxDepthReached)
}

func TestComputeBFS_Modes(t *testing.T) {
	version := createDirectedGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	// Test from vertex 2 (middle of the graph)

	// Mode: All (both directions)
	configAll := &BFSConfig{
		MaxDepth:          0,
		Mode:              BFSModeAll,
		ReturnExternalIDs: true,
	}
	resultAll, err := ComputeBFS(ctx, version, nil, 2, configAll, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS (all) failed: %v", err)
	}

	// Mode: Out only
	configOut := &BFSConfig{
		MaxDepth:          0,
		Mode:              BFSModeOut,
		ReturnExternalIDs: true,
	}
	resultOut, err := ComputeBFS(ctx, version, nil, 2, configOut, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS (out) failed: %v", err)
	}

	// Mode: In only
	configIn := &BFSConfig{
		MaxDepth:          0,
		Mode:              BFSModeIn,
		ReturnExternalIDs: true,
	}
	resultIn, err := ComputeBFS(ctx, version, nil, 2, configIn, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS (in) failed: %v", err)
	}

	t.Logf("BFS from vertex 2:")
	t.Logf("  Mode ALL: visited=%d, vertices=%v", resultAll.NumVisited, resultAll.VisitedExternal)
	t.Logf("  Mode OUT: visited=%d, vertices=%v", resultOut.NumVisited, resultOut.VisitedExternal)
	t.Logf("  Mode IN:  visited=%d, vertices=%v", resultIn.NumVisited, resultIn.VisitedExternal)

	// Out mode should visit: 2, 3, 4, 5 (following outgoing edges)
	// In mode should visit: 2, 1 (following incoming edges)
	// All mode should visit all reachable vertices

	if resultOut.NumVisited > resultAll.NumVisited {
		t.Error("OUT mode should not visit more vertices than ALL mode")
	}
	if resultIn.NumVisited > resultAll.NumVisited {
		t.Error("IN mode should not visit more vertices than ALL mode")
	}
}

func TestComputeBFS_SourceNotFound(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()

	// Try BFS from non-existent vertex
	_, err := ComputeBFS(ctx, version, nil, 999, config, shimCfg)
	if err == nil {
		t.Error("expected error for non-existent source vertex")
	}
}

func TestComputeBFS_NoShim(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()

	config := DefaultBFSConfig()
	_, err := ComputeBFS(ctx, version, nil, 1, config, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeBFS_NilShimGraph(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	shimCfg := &BFSShimConfig{ShimGraph: nil}

	config := DefaultBFSConfig()
	_, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err == nil {
		t.Error("expected error when ShimGraph is nil")
	}
}

func TestComputeBFS_ContextCanceled(t *testing.T) {
	version := createPathGraphForBFS()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()
	_, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestBFSResult_GetDepth(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// Get depth for source (should be 0)
	sourceIdx, _ := version.GetNodeIndex(1)
	depth, ok := result.GetDepth(sourceIdx)
	if !ok {
		t.Error("expected to find depth for source vertex")
	}
	if depth != 0 {
		t.Errorf("expected depth 0 for source, got %d", depth)
	}

	// Get depth for vertex at distance 2
	idx3, _ := version.GetNodeIndex(3)
	depth3, ok := result.GetDepth(idx3)
	if !ok {
		t.Error("expected to find depth for vertex 3")
	}
	if depth3 != 2 {
		t.Errorf("expected depth 2 for vertex 3, got %d", depth3)
	}

	t.Logf("Depths: source=%d, vertex3=%d", depth, depth3)
}

func TestBFSResult_GetVerticesAtDepth(t *testing.T) {
	version := createTreeGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// Depth 0: should be just the source (1)
	atDepth0 := result.GetVerticesAtDepth(0)
	if len(atDepth0) != 1 {
		t.Errorf("expected 1 vertex at depth 0, got %d", len(atDepth0))
	}

	// Depth 1: should be 2, 3
	atDepth1 := result.GetVerticesAtDepth(1)
	if len(atDepth1) != 2 {
		t.Errorf("expected 2 vertices at depth 1, got %d", len(atDepth1))
	}

	// Depth 2: should be 4, 5, 6
	atDepth2 := result.GetVerticesAtDepth(2)
	if len(atDepth2) != 3 {
		t.Errorf("expected 3 vertices at depth 2, got %d", len(atDepth2))
	}

	// Depth 10: should be empty
	atDepth10 := result.GetVerticesAtDepth(10)
	if len(atDepth10) != 0 {
		t.Errorf("expected 0 vertices at depth 10, got %d", len(atDepth10))
	}

	t.Logf("Vertices at depth 0: %v", atDepth0)
	t.Logf("Vertices at depth 1: %v", atDepth1)
	t.Logf("Vertices at depth 2: %v", atDepth2)
}

func TestBFSResult_GetPath(t *testing.T) {
	version := createPathGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := DefaultBFSConfig()
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// Get path from source (1) to target (5)
	targetIdx, _ := version.GetNodeIndex(5)
	path := result.GetPath(targetIdx)
	if path == nil {
		t.Fatal("expected non-nil path")
	}

	// Path should have 5 vertices: 1 -> 2 -> 3 -> 4 -> 5
	if len(path) != 5 {
		t.Errorf("expected path length 5, got %d", len(path))
	}

	t.Logf("Path to vertex 5: %v", path)

	// Get path to source itself (should just be [source])
	sourceIdx, _ := version.GetNodeIndex(1)
	pathToSource := result.GetPath(sourceIdx)
	if len(pathToSource) != 1 {
		t.Errorf("expected path length 1 to source, got %d", len(pathToSource))
	}

	// Get path to unvisited vertex (should be nil)
	pathToNone := result.GetPath(uint32(version.VCount + 10))
	if pathToNone != nil {
		t.Error("expected nil path to unvisited vertex")
	}
}

func TestBFSResult_WasVisited(t *testing.T) {
	version := createStarGraphForBFS()
	ctx := context.Background()
	g := createShimGraphForBFS(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BFSShimConfig{ShimGraph: g}

	config := &BFSConfig{
		MaxDepth:          1, // Only visit immediate neighbors
		Mode:              BFSModeAll,
		ReturnExternalIDs: false,
	}

	// BFS from hub (vertex 1)
	result, err := ComputeBFS(ctx, version, nil, 1, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBFS failed: %v", err)
	}

	// All vertices should be visited (hub + all spokes at depth 1)
	for _, nodeID := range []uint64{1, 2, 3, 4, 5} {
		idx, _ := version.GetNodeIndex(nodeID)
		if !result.WasVisited(idx) {
			t.Errorf("expected vertex %d to be visited", nodeID)
		}
	}

	// Non-existent vertex should not be visited
	if result.WasVisited(uint32(version.VCount + 10)) {
		t.Error("expected non-existent vertex to not be visited")
	}
}

func TestValidateBFSRequest(t *testing.T) {
	// Valid requests
	err := ValidateBFSRequest(0, "all")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateBFSRequest(10, "out")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateBFSRequest(5, "in")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateBFSRequest(5, "") // empty mode should be valid
	if err != nil {
		t.Errorf("expected no error for empty mode, got %v", err)
	}

	// Invalid mode
	err = ValidateBFSRequest(5, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestParseBFSMode(t *testing.T) {
	// Valid modes
	mode, err := ParseBFSMode("all")
	if err != nil || mode != BFSModeAll {
		t.Errorf("expected BFSModeAll, got %v, err=%v", mode, err)
	}

	mode, err = ParseBFSMode("out")
	if err != nil || mode != BFSModeOut {
		t.Errorf("expected BFSModeOut, got %v, err=%v", mode, err)
	}

	mode, err = ParseBFSMode("in")
	if err != nil || mode != BFSModeIn {
		t.Errorf("expected BFSModeIn, got %v, err=%v", mode, err)
	}

	mode, err = ParseBFSMode("") // empty should default to All
	if err != nil || mode != BFSModeAll {
		t.Errorf("expected default BFSModeAll, got %v, err=%v", mode, err)
	}

	// Invalid mode
	_, err = ParseBFSMode("invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestHashBFSParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashBFSParams(1, 10, "all", "view1")
	hash2 := HashBFSParams(1, 10, "all", "view1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Different parameters should produce different hash
	hash3 := HashBFSParams(2, 10, "all", "view1") // different source
	if hash1 == hash3 {
		t.Error("different source should produce different hash")
	}

	hash4 := HashBFSParams(1, 5, "all", "view1") // different max_depth
	if hash1 == hash4 {
		t.Error("different max_depth should produce different hash")
	}

	hash5 := HashBFSParams(1, 10, "out", "view1") // different mode
	if hash1 == hash5 {
		t.Error("different mode should produce different hash")
	}

	hash6 := HashBFSParams(1, 10, "all", "view2") // different view
	if hash1 == hash6 {
		t.Error("different view should produce different hash")
	}
}

func TestDefaultBFSConfig(t *testing.T) {
	config := DefaultBFSConfig()

	if config.MaxDepth != 0 {
		t.Errorf("expected default MaxDepth=0 (unlimited), got %d", config.MaxDepth)
	}
	if config.Mode != BFSModeAll {
		t.Errorf("expected default Mode=All, got %v", config.Mode)
	}
	if config.ReturnExternalIDs != false {
		t.Errorf("expected default ReturnExternalIDs=false, got %v", config.ReturnExternalIDs)
	}
}

func TestBFSMode_String(t *testing.T) {
	if BFSModeAll.String() != "all" {
		t.Errorf("expected 'all', got %s", BFSModeAll.String())
	}
	if BFSModeOut.String() != "out" {
		t.Errorf("expected 'out', got %s", BFSModeOut.String())
	}
	if BFSModeIn.String() != "in" {
		t.Errorf("expected 'in', got %s", BFSModeIn.String())
	}
}
