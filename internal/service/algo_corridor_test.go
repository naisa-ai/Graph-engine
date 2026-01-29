package service

import (
	"testing"
	"time"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

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

	// Corridor from 1 to 4 with 0 hops expansion
	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 0, 0)
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

	// Corridor from 1 to 4 with 1 hop expansion
	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0)
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

func TestComputeCorridor_CommunityAware(t *testing.T) {
	version := createTestGraphForCorridor()

	// Community-aware corridor (falls back to endpoint expansion)
	result, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 1, 0)
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

func TestComputeCorridor_InvalidSource(t *testing.T) {
	version := createTestGraphForCorridor()

	_, err := ComputeCorridor(version, nil, 999, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestComputeCorridor_InvalidTarget(t *testing.T) {
	version := createTestGraphForCorridor()

	_, err := ComputeCorridor(version, nil, 1, 999, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0)
	if err == nil {
		t.Error("expected error for invalid target")
	}
}

func TestComputeCorridor_WithView(t *testing.T) {
	version := createTestGraphForCorridor()

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
	result, err := ComputeCorridor(version, baseView, 1, 8, gepb.CorridorSpec_SHORTEST_PATH_HULL, 0, 0)
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

	result, err := ComputeCorridor(version, nil, 1, 4, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0)
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

	// Estimate corridor size from 1 to 4 with 1 hop
	estimate, err := EstimateCorridorSize(version, nil, 1, 4, 1)
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

	// Try to find corridor between disconnected components
	_, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_SHORTEST_PATH_HULL, 1, 0)
	if err == nil {
		t.Error("expected error for disconnected components with SHORTEST_PATH_HULL")
	}

	// COMMUNITY_AWARE should still work (returns endpoint neighborhoods)
	result, err := ComputeCorridor(version, nil, 1, 12, gepb.CorridorSpec_COMMUNITY_AWARE, 1, 0)
	if err != nil {
		t.Fatalf("COMMUNITY_AWARE should work for disconnected: %v", err)
	}

	// Should have some vertices from both sides
	if result.View.VCount == 0 {
		t.Error("expected some vertices in corridor")
	}
}
