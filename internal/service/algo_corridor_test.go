package service

import (
	"testing"
	"time"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForCorridor creates a shim graph for corridor testing.
func createShimGraphForCorridor(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createTestGraphForCorridor creates a graph for corridor testing.
// Graph structure:
//
//	1 --- 2 --- 3 --- 4
//	|     |     |     |
//	5 --- 6 --- 7 --- 8
//	|     |     |     |
//	9 --- 10 -- 11 -- 12
//
// This is a 3x4 grid graph with 12 vertices and undirected edges.
func createTestGraphForCorridor() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	// Horizontal edges
	src := []uint64{1, 2, 3, 5, 6, 7, 9, 10, 11}
	dst := []uint64{2, 3, 4, 6, 7, 8, 10, 11, 12}

	// Add vertical edges
	src = append(src, 1, 2, 3, 4, 5, 6, 7, 8)
	dst = append(dst, 5, 6, 7, 8, 9, 10, 11, 12)

	build := &Build{
		ID:          "build-corridor-test",
		GraphName:   "corridor-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "corridor-test", false, build)
	return version
}

// createTestGraphWithKinds creates a graph with edge kinds for testing.
func createTestGraphWithKinds() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}

	src := []uint64{1, 2, 3, 4, 1, 2}
	dst := []uint64{2, 3, 4, 5, 3, 4}
	kinds := []uint32{1, 1, 2, 2, 3, 3} // Different edge kinds

	build := &Build{
		ID:          "build-kinds-test",
		GraphName:   "kinds-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
		EdgesKind:   kinds,
	}

	version, _ := NewGraphVersion("v1", "kinds-test", false, build)
	return version
}

func TestComputeCorridor_ShortestPathHull_Basic(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Corridor from 1 to 4 with 0 hops expansion
	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 0, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	if result.View == nil {
		t.Fatal("expected corridor view")
	}

	// With 0 hops, should only include path vertices (1, 2, 3, 4)
	if result.View.VCount != 4 {
		t.Errorf("expected 4 vertices in corridor, got %d", result.View.VCount)
	}

	// Check path summary
	if result.PathSummary == nil || !result.PathSummary.Found {
		t.Error("expected path summary with found=true")
	}

	// Check method
	if result.Method != CorridorMethodShortestPathHull {
		t.Errorf("expected method SHORTEST_PATH_HULL, got %s", result.Method)
	}
}

func TestComputeCorridor_ShortestPathHull_WithExpansion(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Corridor from 1 to 4 with 1 hop expansion
	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	// With 1 hop expansion from path (1,2,3,4), should include more vertices
	// 1 hop from 1: 2, 5
	// 1 hop from 2: 1, 3, 6
	// 1 hop from 3: 2, 4, 7
	// 1 hop from 4: 3, 8
	// Total unique: 1,2,3,4,5,6,7,8
	if result.View.VCount < 4 {
		t.Errorf("expected at least 4 vertices in expanded corridor, got %d", result.View.VCount)
	}

	// Check metadata exists
	if result.Meta == nil {
		t.Error("expected metadata")
	}
	if _, ok := result.Meta["corridor_vcount"]; !ok {
		t.Error("expected corridor_vcount in metadata")
	}
}

func TestComputeCorridor_CommunityAware_NoCommunities(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Community-aware corridor (falls back to endpoint expansion when no communities)
	result, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 1, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	if result.View == nil {
		t.Fatal("expected corridor view")
	}

	// Check method
	if result.Method != CorridorMethodCommunityAware {
		t.Errorf("expected method COMMUNITY_AWARE, got %s", result.Method)
	}

	// Should have note about community data not available
	if note, ok := result.Meta["note"]; !ok || note == "" {
		t.Error("expected note in metadata about community data")
	}
}

func TestComputeCorridor_CommunityAware_WithCommunities(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Set up community membership
	// Grid layout:
	//   1 --- 2 --- 3 --- 4      (community 0)
	//   |     |     |     |
	//   5 --- 6 --- 7 --- 8      (community 0 for 5,6; community 1 for 7,8)
	//   |     |     |     |
	//   9 --- 10 -- 11 -- 12     (community 1)
	//
	// Community 0: vertices 1,2,3,4,5,6 (top-left region)
	// Community 1: vertices 7,8,9,10,11,12 (bottom-right region)
	membership := make([]uint32, version.VCount)
	for i := uint64(0); i < version.VCount; i++ {
		nodeID, _ := version.GetNodeID(uint32(i))
		if nodeID <= 6 {
			membership[i] = 0 // Community 0
		} else {
			membership[i] = 1 // Community 1
		}
	}
	version.SetCommunities(membership, 2)

	// Corridor from vertex 1 (community 0) to vertex 12 (community 1)
	result, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 0, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	if result.View == nil {
		t.Fatal("expected corridor view")
	}

	// Check method
	if result.Method != CorridorMethodCommunityAware {
		t.Errorf("expected method COMMUNITY_AWARE, got %s", result.Method)
	}

	// Should indicate communities were used
	if result.Meta["communities_used"] != "true" {
		t.Errorf("expected communities_used=true in metadata, got %s", result.Meta["communities_used"])
	}

	// Source and target in different communities
	if result.Meta["same_community"] != "false" {
		t.Errorf("expected same_community=false, got %s", result.Meta["same_community"])
	}

	// Corridor should include vertices from both communities
	// All 12 vertices should be included since we're spanning two communities
	if result.View.VCount < 6 {
		t.Errorf("expected at least 6 vertices in corridor spanning two communities, got %d", result.View.VCount)
	}

	t.Logf("Community-aware corridor: %d vertices, %d edges", result.View.VCount, result.View.ECount)
	t.Logf("Source community: %s, Target community: %s", result.Meta["source_community"], result.Meta["target_community"])
}

func TestComputeCorridor_CommunityAware_SameCommunity(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// All vertices in same community
	membership := make([]uint32, version.VCount)
	for i := range membership {
		membership[i] = 0 // All in community 0
	}
	version.SetCommunities(membership, 1)

	// Corridor from vertex 1 to vertex 4 (same community)
	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_COMMUNITY_AWARE, 0, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	if result.View == nil {
		t.Fatal("expected corridor view")
	}

	// Should indicate same community
	if result.Meta["same_community"] != "true" {
		t.Errorf("expected same_community=true, got %s", result.Meta["same_community"])
	}

	// Should include all vertices (since they're all in the same community)
	if result.View.VCount != version.VCount {
		t.Errorf("expected all %d vertices in same-community corridor, got %d", version.VCount, result.View.VCount)
	}

	t.Logf("Same-community corridor: %d vertices", result.View.VCount)
}

func TestComputeCorridor_CommunityAware_WithHops(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Two communities
	membership := make([]uint32, version.VCount)
	for i := uint64(0); i < version.VCount; i++ {
		nodeID, _ := version.GetNodeID(uint32(i))
		if nodeID <= 6 {
			membership[i] = 0
		} else {
			membership[i] = 1
		}
	}
	version.SetCommunities(membership, 2)

	// Corridor with expansion hops
	resultNoHops, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 0, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor (no hops) failed: %v", err)
	}

	resultWithHops, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 1, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor (with hops) failed: %v", err)
	}

	// With hops, corridor should be at least as large
	if resultWithHops.View.VCount < resultNoHops.View.VCount {
		t.Errorf("corridor with hops should have >= vertices: %d < %d",
			resultWithHops.View.VCount, resultNoHops.View.VCount)
	}

	t.Logf("No hops: %d vertices, With hops: %d vertices",
		resultNoHops.View.VCount, resultWithHops.View.VCount)
}

func TestComputeCorridor_InvalidSource(t *testing.T) {
	version := createTestGraphForCorridor()

	_, err := ComputeCorridor(version, nil, 999, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0, nil)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestComputeCorridor_InvalidTarget(t *testing.T) {
	version := createTestGraphForCorridor()

	_, err := ComputeCorridor(version, nil, 1, 999, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0, nil)
	if err == nil {
		t.Error("expected error for invalid target")
	}
}

func TestComputeCorridor_WithView(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Create a view that only includes top row (1,2,3,4) and middle row (5,6,7,8)
	baseView := NewView(version)
	// Exclude bottom row vertices
	for i, nodeID := range version.indexToNodeID {
		if nodeID >= 9 && nodeID <= 12 {
			baseView.VertexMaskBitmap().Remove(uint32(i))
		}
	}
	// Update edge mask to exclude edges to/from bottom row
	for i := range version.EdgeSrc {
		src := version.EdgeSrc[i]
		dst := version.EdgeDst[i]
		srcNodeID, _ := version.GetNodeID(src)
		dstNodeID, _ := version.GetNodeID(dst)
		if (srcNodeID >= 9 && srcNodeID <= 12) || (dstNodeID >= 9 && dstNodeID <= 12) {
			baseView.EdgeMaskBitmap().Remove(uint32(i))
		}
	}
	baseView.updateCounts()

	// Corridor from 1 to 8 should only use edges in view
	result, err := ComputeCorridor(version, baseView, 1, 8, gepb.CorridorSpec_SHORTEST_PATH_HULL, 0, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor with view failed: %v", err)
	}

	// All corridor vertices should be in top two rows
	it := result.View.VertexMaskBitmap().Iterator()
	for it.HasNext() {
		i := it.Next()
		nodeID, _ := version.GetNodeID(i)
		if nodeID >= 9 && nodeID <= 12 {
			t.Errorf("corridor should not include vertex %d from excluded row", nodeID)
		}
	}
}

func TestComputeCorridor_Metadata(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeCorridor failed: %v", err)
	}

	// Check all expected metadata fields
	requiredFields := []string{
		"corridor_vcount",
		"corridor_ecount",
		"method",
		"hops",
		"path_found",
	}

	for _, field := range requiredFields {
		if _, ok := result.Meta[field]; !ok {
			t.Errorf("expected metadata field %s", field)
		}
	}

	// Check path_found is "true"
	if result.Meta["path_found"] != "true" {
		t.Errorf("expected path_found=true, got %s", result.Meta["path_found"])
	}
}

func TestComputeEdgeKindCounts(t *testing.T) {
	version := createTestGraphWithKinds()
	view := NewView(version) // Full view

	counts := computeEdgeKindCounts(version, view)

	// Should have 3 different kinds
	if len(counts) != 3 {
		t.Errorf("expected 3 different edge kinds, got %d", len(counts))
	}

	// Each kind should have 2 edges
	for kind, count := range counts {
		if count != 2 {
			t.Errorf("expected 2 edges for kind %d, got %d", kind, count)
		}
	}
}

func TestExpandNeighborhood(t *testing.T) {
	version := createTestGraphForCorridor()

	// Expand from vertex 6 (center) with 1 hop
	idx, _ := version.GetNodeIndex(6)
	result := bfsExpand(version, nil, idx, 1)

	// Should include 6 and its neighbors (2, 5, 7, 10)
	if len(result) != 5 {
		t.Errorf("expected 5 vertices in 1-hop expansion from center, got %d", len(result))
	}

	// Verify center is included
	if !result[idx] {
		t.Error("center vertex should be included")
	}
}

func TestExpandNeighborhood_ZeroHops(t *testing.T) {
	version := createTestGraphForCorridor()

	idx, _ := version.GetNodeIndex(6)
	result := bfsExpand(version, nil, idx, 0)

	// Should only include the seed vertex
	if len(result) != 1 {
		t.Errorf("expected 1 vertex with 0 hops, got %d", len(result))
	}
	if !result[idx] {
		t.Error("seed vertex should be included")
	}
}

func TestValidateCorridorSpec(t *testing.T) {
	// Test nil spec
	err := ValidateCorridorSpec(nil)
	if err == nil {
		t.Error("expected error for nil spec")
	}

	// Test valid spec
	spec := &gepb.CorridorSpec{
		SourceU64: 1,
		TargetU64: 2,
	}
	err = ValidateCorridorSpec(spec)
	if err != nil {
		t.Errorf("expected no error for valid spec, got %v", err)
	}
}

func TestEstimateCorridorSize(t *testing.T) {
	version := createTestGraphForCorridor()
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Estimate corridor size from 1 to 4 with 1 hop
	estimate, err := EstimateCorridorSize(version, nil, 1, 4, 1, cfg)
	if err != nil {
		t.Fatalf("EstimateCorridorSize failed: %v", err)
	}

	// Should give a reasonable estimate (not 0, not huge)
	if estimate == 0 {
		t.Error("expected non-zero estimate")
	}
	if estimate > uint64(version.ECount)*2 {
		t.Errorf("estimate seems too large: %d vs graph edges %d", estimate, version.ECount)
	}
}

func TestCreateCorridorView(t *testing.T) {
	version := createTestGraphForCorridor()

	// Create a corridor view with just vertices 1, 2, 3
	vertices := map[uint32]bool{}
	idx1, _ := version.GetNodeIndex(1)
	idx2, _ := version.GetNodeIndex(2)
	idx3, _ := version.GetNodeIndex(3)
	vertices[idx1] = true
	vertices[idx2] = true
	vertices[idx3] = true

	corridorView := createCorridorView(version, nil, vertices)

	// Should have 3 vertices
	if corridorView.VCount != 3 {
		t.Errorf("expected 3 vertices, got %d", corridorView.VCount)
	}

	// Should have 2 edges (1-2, 2-3)
	if corridorView.ECount != 2 {
		t.Errorf("expected 2 edges, got %d", corridorView.ECount)
	}

	// Verify view ID is set
	if corridorView.ID == "" {
		t.Error("expected view ID to be set")
	}
}

func TestCorridorWithDisconnectedGraph(t *testing.T) {
	// Create a graph with two disconnected components
	vertices := []uint64{1, 2, 3, 10, 11, 12}
	src := []uint64{1, 2, 10, 11}
	dst := []uint64{2, 3, 11, 12}

	build := &Build{
		ID:          "build-disconnected",
		GraphName:   "disconnected-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "disconnected-test", false, build)
	g := createShimGraphForCorridor(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ShortestPathConfig{ShimGraph: g}

	// Try to find corridor between disconnected components
	_, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0, cfg)
	if err == nil {
		t.Error("expected error for disconnected components with SHORTEST_PATH_HULL")
	}

	// COMMUNITY_AWARE should still work (returns endpoint neighborhoods)
	result, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 1, 0, cfg)
	if err != nil {
		t.Fatalf("COMMUNITY_AWARE should work for disconnected: %v", err)
	}

	// Should have some vertices from both sides
	if result.View.VCount == 0 {
		t.Error("expected some vertices in corridor")
	}
}
