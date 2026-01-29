package service

import (
	"context"
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForNeighborhood creates a shim graph for neighborhood testing.
func createShimGraphForNeighborhood(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createStarGraphForNeighborhood creates a star graph with hub 1.
// Graph structure: 1 connected to 2, 3, 4, 5
// Hub (1) has all other vertices as immediate neighbors.
func createStarGraphForNeighborhood() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 1, 1, 1}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-star-neighborhood",
		GraphName:   "star-neighborhood",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "star-neighborhood", false, build)
	return version
}

// createPathGraphForNeighborhood creates a path: 1 - 2 - 3 - 4 - 5
// Clear distance structure for hop testing.
func createPathGraphForNeighborhood() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-path-neighborhood",
		GraphName:   "path-neighborhood",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "path-neighborhood", false, build)
	return version
}

// createDirectedGraphForNeighborhood creates a directed graph for mode testing.
// Graph structure (directed):
//
//	1 -> 2 -> 3
//	     |
//	     v
//	     4 -> 5
func createDirectedGraphForNeighborhood() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 2, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-directed-neighborhood",
		GraphName:   "directed-neighborhood",
		Directed:    true,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "directed-neighborhood", true, build)
	return version
}

// createGridGraphForNeighborhood creates a 3x3 grid for multi-hop testing.
// Graph structure:
//
//	1 - 2 - 3
//	|   |   |
//	4 - 5 - 6
//	|   |   |
//	7 - 8 - 9
func createGridGraphForNeighborhood() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}
	// Horizontal edges
	src := []uint64{1, 2, 4, 5, 7, 8}
	dst := []uint64{2, 3, 5, 6, 8, 9}
	// Vertical edges
	src = append(src, 1, 2, 3, 4, 5, 6)
	dst = append(dst, 4, 5, 6, 7, 8, 9)

	build := &Build{
		ID:          "build-grid-neighborhood",
		GraphName:   "grid-neighborhood",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "grid-neighborhood", false, build)
	return version
}

func TestComputeNeighborhood_Basic(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}

	// Neighborhood of hub (vertex 1) with 1 hop
	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	// Should include seed (1) and all spokes (2, 3, 4, 5)
	if result.NumVertices != 5 {
		t.Errorf("expected 5 vertices, got %d", result.NumVertices)
	}

	// Verify external IDs are populated
	if len(result.VerticesExternal) != int(result.NumVertices) {
		t.Errorf("expected %d external IDs, got %d", result.NumVertices, len(result.VerticesExternal))
	}

	t.Logf("Neighborhood (seed=1, hops=1): %d vertices", result.NumVertices)
	t.Logf("Vertices: %v", result.VerticesExternal)
}

func TestComputeNeighborhood_MultipleSeeds(t *testing.T) {
	version := createPathGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}

	// Neighborhood of endpoints (1 and 5) with 1 hop
	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1, 5}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	// Seeds (1, 5) plus their 1-hop neighbors (2 for 1, 4 for 5) = 4 vertices
	if result.NumVertices != 4 {
		t.Errorf("expected 4 vertices, got %d", result.NumVertices)
	}

	t.Logf("Neighborhood (seeds=[1,5], hops=1): %d vertices", result.NumVertices)
	t.Logf("Vertices: %v", result.VerticesExternal)
}

func TestComputeNeighborhood_MultipleHops(t *testing.T) {
	version := createPathGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	// 1-hop from vertex 1 should get 1, 2
	config1 := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}
	result1, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config1, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (1 hop) failed: %v", err)
	}

	// 2-hops from vertex 1 should get 1, 2, 3
	config2 := &NeighborhoodConfig{
		Hops:              2,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}
	result2, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config2, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (2 hops) failed: %v", err)
	}

	// 3-hops from vertex 1 should get 1, 2, 3, 4
	config3 := &NeighborhoodConfig{
		Hops:              3,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}
	result3, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config3, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (3 hops) failed: %v", err)
	}

	t.Logf("Neighborhood (seed=1, hops=1): %d vertices - %v", result1.NumVertices, result1.VerticesExternal)
	t.Logf("Neighborhood (seed=1, hops=2): %d vertices - %v", result2.NumVertices, result2.VerticesExternal)
	t.Logf("Neighborhood (seed=1, hops=3): %d vertices - %v", result3.NumVertices, result3.VerticesExternal)

	// More hops should include more vertices
	if result2.NumVertices <= result1.NumVertices {
		t.Error("2 hops should include more vertices than 1 hop")
	}
	if result3.NumVertices <= result2.NumVertices {
		t.Error("3 hops should include more vertices than 2 hops")
	}
}

func TestComputeNeighborhood_Modes(t *testing.T) {
	version := createDirectedGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	// Test from vertex 2

	// Mode: All
	configAll := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeAll,
		ReturnExternalIDs: true,
	}
	resultAll, err := ComputeNeighborhood(ctx, version, nil, []uint64{2}, configAll, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (all) failed: %v", err)
	}

	// Mode: Out
	configOut := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeOut,
		ReturnExternalIDs: true,
	}
	resultOut, err := ComputeNeighborhood(ctx, version, nil, []uint64{2}, configOut, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (out) failed: %v", err)
	}

	// Mode: In
	configIn := &NeighborhoodConfig{
		Hops:              1,
		Mode:              NeighborhoodModeIn,
		ReturnExternalIDs: true,
	}
	resultIn, err := ComputeNeighborhood(ctx, version, nil, []uint64{2}, configIn, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood (in) failed: %v", err)
	}

	t.Logf("Neighborhood from vertex 2 (hops=1):")
	t.Logf("  Mode ALL: %d vertices - %v", resultAll.NumVertices, resultAll.VerticesExternal)
	t.Logf("  Mode OUT: %d vertices - %v", resultOut.NumVertices, resultOut.VerticesExternal)
	t.Logf("  Mode IN:  %d vertices - %v", resultIn.NumVertices, resultIn.VerticesExternal)

	// Verify metadata
	if resultAll.Meta["mode"] != "all" {
		t.Errorf("expected mode=all, got %s", resultAll.Meta["mode"])
	}
	if resultOut.Meta["mode"] != "out" {
		t.Errorf("expected mode=out, got %s", resultOut.Meta["mode"])
	}
	if resultIn.Meta["mode"] != "in" {
		t.Errorf("expected mode=in, got %s", resultIn.Meta["mode"])
	}
}

func TestComputeNeighborhood_EmptySeeds(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{}, config, shimCfg)
	if err == nil {
		t.Error("expected error for empty seeds")
	}
}

func TestComputeNeighborhood_ZeroHops(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 0, // Invalid: hops must be positive
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err == nil {
		t.Error("expected error for zero hops")
	}
}

func TestComputeNeighborhood_SeedNotFound(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{999}, config, shimCfg)
	if err == nil {
		t.Error("expected error for non-existent seed")
	}
}

func TestComputeNeighborhood_NoShim(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeNeighborhood_NilShimGraph(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: nil}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err == nil {
		t.Error("expected error when ShimGraph is nil")
	}
}

func TestComputeNeighborhood_ContextCanceled(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	_, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestNeighborhoodResult_GetDistance(t *testing.T) {
	version := createPathGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 3,
		Mode: NeighborhoodModeAll,
	}

	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	// Seed should be at distance 0
	seedIdx, _ := version.GetNodeIndex(1)
	dist, ok := result.GetDistance(seedIdx)
	if !ok || dist != 0 {
		t.Errorf("expected distance 0 for seed, got %d, ok=%v", dist, ok)
	}

	// Vertex 3 should be at distance 2 from seed 1
	idx3, _ := version.GetNodeIndex(3)
	dist3, ok := result.GetDistance(idx3)
	if !ok || dist3 != 2 {
		t.Errorf("expected distance 2 for vertex 3, got %d, ok=%v", dist3, ok)
	}

	t.Logf("Distance from 1: vertex 1=%d, vertex 3=%d", dist, dist3)
}

func TestNeighborhoodResult_Contains(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	// All vertices should be in the neighborhood
	for _, nodeID := range []uint64{1, 2, 3, 4, 5} {
		idx, _ := version.GetNodeIndex(nodeID)
		if !result.Contains(idx) {
			t.Errorf("expected vertex %d to be in neighborhood", nodeID)
		}
	}

	// Non-existent vertex should not be in neighborhood
	if result.Contains(uint32(version.VCount + 10)) {
		t.Error("expected non-existent vertex to not be in neighborhood")
	}
}

func TestNeighborhoodResult_GetVerticesAtDistance(t *testing.T) {
	version := createGridGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops:            2,
		Mode:            NeighborhoodModeAll,
		GroupByDistance: true, // Enable grouping
	}

	// Neighborhood from center of grid (vertex 5)
	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{5}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	// Distance 0: just the seed (5)
	atDist0 := result.GetVerticesAtDistance(0)
	if len(atDist0) != 1 {
		t.Errorf("expected 1 vertex at distance 0, got %d", len(atDist0))
	}

	// Distance 1: immediate neighbors (2, 4, 6, 8)
	atDist1 := result.GetVerticesAtDistance(1)
	if len(atDist1) != 4 {
		t.Errorf("expected 4 vertices at distance 1, got %d", len(atDist1))
	}

	// Distance 2: corners (1, 3, 7, 9)
	atDist2 := result.GetVerticesAtDistance(2)
	if len(atDist2) != 4 {
		t.Errorf("expected 4 vertices at distance 2, got %d", len(atDist2))
	}

	t.Logf("Vertices at distance 0: %v", atDist0)
	t.Logf("Vertices at distance 1: %v", atDist1)
	t.Logf("Vertices at distance 2: %v", atDist2)
}

func TestNeighborhoodResult_GetImmediateNeighbors(t *testing.T) {
	version := createStarGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 1,
		Mode: NeighborhoodModeAll,
	}

	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	immediateNeighbors := result.GetImmediateNeighbors()
	// Hub (1) has 4 immediate neighbors (2, 3, 4, 5)
	if len(immediateNeighbors) != 4 {
		t.Errorf("expected 4 immediate neighbors, got %d", len(immediateNeighbors))
	}

	t.Logf("Immediate neighbors of hub: %v", immediateNeighbors)
}

func TestNeighborhoodResult_GetSeeds(t *testing.T) {
	version := createPathGraphForNeighborhood()
	ctx := context.Background()
	g := createShimGraphForNeighborhood(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &NeighborhoodShimConfig{ShimGraph: g}

	config := &NeighborhoodConfig{
		Hops: 2,
		Mode: NeighborhoodModeAll,
	}

	result, err := ComputeNeighborhood(ctx, version, nil, []uint64{1, 5}, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeNeighborhood failed: %v", err)
	}

	seeds := result.GetSeeds()
	// Should have 2 seeds
	if len(seeds) != 2 {
		t.Errorf("expected 2 seeds, got %d", len(seeds))
	}

	t.Logf("Seeds: %v", seeds)
}

func TestValidateNeighborhoodRequest(t *testing.T) {
	// Valid requests
	err := ValidateNeighborhoodRequest(1, "all")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateNeighborhoodRequest(10, "out")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateNeighborhoodRequest(5, "in")
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	err = ValidateNeighborhoodRequest(5, "") // empty mode should be valid
	if err != nil {
		t.Errorf("expected no error for empty mode, got %v", err)
	}

	// Invalid: zero hops
	err = ValidateNeighborhoodRequest(0, "all")
	if err == nil {
		t.Error("expected error for zero hops")
	}

	// Invalid: too many hops
	err = ValidateNeighborhoodRequest(101, "all")
	if err == nil {
		t.Error("expected error for hops > 100")
	}

	// Invalid mode
	err = ValidateNeighborhoodRequest(5, "invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestParseNeighborhoodMode(t *testing.T) {
	// Valid modes
	mode, err := ParseNeighborhoodMode("all")
	if err != nil || mode != NeighborhoodModeAll {
		t.Errorf("expected NeighborhoodModeAll, got %v, err=%v", mode, err)
	}

	mode, err = ParseNeighborhoodMode("out")
	if err != nil || mode != NeighborhoodModeOut {
		t.Errorf("expected NeighborhoodModeOut, got %v, err=%v", mode, err)
	}

	mode, err = ParseNeighborhoodMode("in")
	if err != nil || mode != NeighborhoodModeIn {
		t.Errorf("expected NeighborhoodModeIn, got %v, err=%v", mode, err)
	}

	mode, err = ParseNeighborhoodMode("") // empty should default to All
	if err != nil || mode != NeighborhoodModeAll {
		t.Errorf("expected default NeighborhoodModeAll, got %v, err=%v", mode, err)
	}

	// Invalid mode
	_, err = ParseNeighborhoodMode("invalid")
	if err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestHashNeighborhoodParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashNeighborhoodParams([]uint64{1, 2}, 2, "all", "view1")
	hash2 := HashNeighborhoodParams([]uint64{1, 2}, 2, "all", "view1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Seeds in different order should produce same hash (sorted internally)
	hash3 := HashNeighborhoodParams([]uint64{2, 1}, 2, "all", "view1")
	if hash1 != hash3 {
		t.Errorf("seeds in different order should produce same hash: %s != %s", hash1, hash3)
	}

	// Different parameters should produce different hash
	hash4 := HashNeighborhoodParams([]uint64{1, 3}, 2, "all", "view1") // different seeds
	if hash1 == hash4 {
		t.Error("different seeds should produce different hash")
	}

	hash5 := HashNeighborhoodParams([]uint64{1, 2}, 3, "all", "view1") // different hops
	if hash1 == hash5 {
		t.Error("different hops should produce different hash")
	}

	hash6 := HashNeighborhoodParams([]uint64{1, 2}, 2, "out", "view1") // different mode
	if hash1 == hash6 {
		t.Error("different mode should produce different hash")
	}

	hash7 := HashNeighborhoodParams([]uint64{1, 2}, 2, "all", "view2") // different view
	if hash1 == hash7 {
		t.Error("different view should produce different hash")
	}
}

func TestDefaultNeighborhoodConfig(t *testing.T) {
	config := DefaultNeighborhoodConfig(2)

	if config.Hops != 2 {
		t.Errorf("expected Hops=2, got %d", config.Hops)
	}
	if config.Mode != NeighborhoodModeAll {
		t.Errorf("expected default Mode=All, got %v", config.Mode)
	}
	if config.ReturnExternalIDs != false {
		t.Errorf("expected default ReturnExternalIDs=false, got %v", config.ReturnExternalIDs)
	}
	if config.GroupByDistance != false {
		t.Errorf("expected default GroupByDistance=false, got %v", config.GroupByDistance)
	}
}

func TestNeighborhoodMode_String(t *testing.T) {
	if NeighborhoodModeAll.String() != "all" {
		t.Errorf("expected 'all', got %s", NeighborhoodModeAll.String())
	}
	if NeighborhoodModeOut.String() != "out" {
		t.Errorf("expected 'out', got %s", NeighborhoodModeOut.String())
	}
	if NeighborhoodModeIn.String() != "in" {
		t.Errorf("expected 'in', got %s", NeighborhoodModeIn.String())
	}
}
