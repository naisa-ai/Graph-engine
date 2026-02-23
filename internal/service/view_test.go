package service

import (
	"testing"
	"time"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// createTestGraphVersionForView creates a test graph for view testing.
// Graph structure:
//
//	0 -- 1 -- 2
//	|    |    |
//	3 -- 4 -- 5
//	|    |    |
//	6 -- 7 -- 8
//
// All edges are undirected. Node IDs are 100, 101, ..., 108.
func createTestGraphVersionForView() *GraphVersion {
	// Vertices: 100-108
	vertices := []uint64{100, 101, 102, 103, 104, 105, 106, 107, 108}

	// Edges (using node IDs 100-108)
	edges := [][2]uint64{
		{100, 101}, {101, 102}, // Top row
		{100, 103}, {101, 104}, {102, 105}, // Vertical
		{103, 104}, {104, 105}, // Middle row
		{103, 106}, {104, 107}, {105, 108}, // Vertical
		{106, 107}, {107, 108}, // Bottom row
	}

	src := make([]uint64, len(edges))
	dst := make([]uint64, len(edges))
	for i, edge := range edges {
		src[i] = edge[0]
		dst[i] = edge[1]
	}

	build := &Build{
		ID:          "build-view-test",
		GraphName:   "test-graph",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "test-graph", false, build)
	return version
}

func TestView_NewView(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// All edges and vertices should be included
	if view.VCount != 9 {
		t.Errorf("expected VCount=9, got %d", view.VCount)
	}
	if view.ECount != 12 {
		t.Errorf("expected ECount=12, got %d", view.ECount)
	}
	if view.VersionID != version.ID {
		t.Errorf("expected VersionID=%s, got %s", version.ID, view.VersionID)
	}
}

func TestView_ApplyInducedVertices(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// Induce only center vertices: 100, 101, 103, 104 (forms a square)
	err := view.ApplyInducedVertices([]uint64{100, 101, 103, 104})
	if err != nil {
		t.Fatalf("ApplyInducedVertices failed: %v", err)
	}

	view.updateCounts()

	if view.VCount != 4 {
		t.Errorf("expected VCount=4, got %d", view.VCount)
	}
	// Edges within induced set: 100-101, 100-103, 101-104, 103-104
	if view.ECount != 4 {
		t.Errorf("expected ECount=4, got %d", view.ECount)
	}
}

func TestView_ApplyNeighborhood(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// Start from node 104 (center), 1 hop
	spec := &gepb.NeighborhoodSpec{
		SeedsU64: []uint64{104},
		Hops:     1,
		Mode:     gepb.NeighborhoodSpec_MODE_ALL,
	}

	err := view.ApplyNeighborhood(spec)
	if err != nil {
		t.Fatalf("ApplyNeighborhood failed: %v", err)
	}

	view.updateCounts()

	// From 104: neighbors are 101, 103, 105, 107 (4 direct neighbors)
	// Plus 104 itself = 5 vertices
	if view.VCount != 5 {
		t.Errorf("expected VCount=5, got %d", view.VCount)
	}
}

func TestView_ApplyNeighborhood_TwoHops(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// Start from node 104 (center), 2 hops
	spec := &gepb.NeighborhoodSpec{
		SeedsU64: []uint64{104},
		Hops:     2,
		Mode:     gepb.NeighborhoodSpec_MODE_ALL,
	}

	err := view.ApplyNeighborhood(spec)
	if err != nil {
		t.Fatalf("ApplyNeighborhood failed: %v", err)
	}

	view.updateCounts()

	// From 104: 2 hops should reach all 9 nodes
	if view.VCount != 9 {
		t.Errorf("expected VCount=9, got %d", view.VCount)
	}
}

func TestView_ApplyExclusions(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// Exclude vertex 104 (center)
	view.ApplyExclusions([]uint64{104}, nil)
	view.updateCounts()

	if view.VCount != 8 {
		t.Errorf("expected VCount=8, got %d", view.VCount)
	}
	// Edges removed: 101-104, 103-104, 104-105, 104-107 = 4 edges
	// Original: 12, remaining: 8
	if view.ECount != 8 {
		t.Errorf("expected ECount=8, got %d", view.ECount)
	}
}

func TestView_ContainsVertex(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// All vertices should be contained initially
	for i := uint64(100); i <= 108; i++ {
		if !view.ContainsVertex(i) {
			t.Errorf("expected vertex %d to be in view", i)
		}
	}

	// Non-existent vertex
	if view.ContainsVertex(999) {
		t.Error("vertex 999 should not be in view")
	}

	// After exclusion
	view.ApplyExclusions([]uint64{104}, nil)
	if view.ContainsVertex(104) {
		t.Error("vertex 104 should not be in view after exclusion")
	}
}

func TestView_GetEdges(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	// Induce a smaller set
	view.ApplyInducedVertices([]uint64{100, 101, 103, 104}) //nolint:errcheck // test setup
	view.updateCounts()

	src, dst := view.GetEdges()

	if len(src) != 4 || len(dst) != 4 {
		t.Errorf("expected 4 edges, got src=%d, dst=%d", len(src), len(dst))
	}
}

func TestView_GetVertices(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	vertices := view.GetVertices()
	if len(vertices) != 9 {
		t.Errorf("expected 9 vertices, got %d", len(vertices))
	}

	// Check that all expected vertices are present
	expected := map[uint64]bool{
		100: true, 101: true, 102: true,
		103: true, 104: true, 105: true,
		106: true, 107: true, 108: true,
	}
	for _, v := range vertices {
		if !expected[v] {
			t.Errorf("unexpected vertex %d", v)
		}
		delete(expected, v)
	}
	if len(expected) > 0 {
		t.Errorf("missing vertices: %v", expected)
	}
}

func TestView_PinUnpin(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	if view.RefCount() != 0 {
		t.Errorf("expected initial refCount=0, got %d", view.RefCount())
	}

	view.Pin()
	if view.RefCount() != 1 {
		t.Errorf("expected refCount=1 after Pin, got %d", view.RefCount())
	}

	view.Pin()
	if view.RefCount() != 2 {
		t.Errorf("expected refCount=2 after second Pin, got %d", view.RefCount())
	}

	canCleanup := view.Unpin()
	if canCleanup {
		t.Error("should not be able to cleanup with refCount > 0")
	}
	if view.RefCount() != 1 {
		t.Errorf("expected refCount=1 after Unpin, got %d", view.RefCount())
	}

	canCleanup = view.Unpin()
	if !canCleanup {
		t.Error("should be able to cleanup with refCount <= 0")
	}
}

func TestView_EstimateMemory(t *testing.T) {
	version := createTestGraphVersionForView()
	view := NewView(version)

	mem := view.EstimateMemory()
	if mem == 0 {
		t.Error("memory estimate should be > 0")
	}

	// Memory should include masks
	// 12 edges + 9 vertices = ~21 bytes minimum for masks
	if mem < 20 {
		t.Errorf("memory estimate too low: %d", mem)
	}
}

func TestNewViewFromSpec(t *testing.T) {
	version := createTestGraphVersionForView()

	spec := &gepb.ViewSpec{
		InduceVerticesU64: []uint64{100, 101, 103, 104},
	}

	view, err := NewViewFromSpec(version, spec)
	if err != nil {
		t.Fatalf("NewViewFromSpec failed: %v", err)
	}

	if view.VCount != 4 {
		t.Errorf("expected VCount=4, got %d", view.VCount)
	}
	if view.ECount != 4 {
		t.Errorf("expected ECount=4, got %d", view.ECount)
	}
	if view.SpecHash == "" {
		t.Error("SpecHash should be set")
	}
}

func TestHashViewSpec(t *testing.T) {
	spec1 := &gepb.ViewSpec{
		InduceVerticesU64: []uint64{1, 2, 3},
	}
	spec2 := &gepb.ViewSpec{
		InduceVerticesU64: []uint64{1, 2, 3},
	}
	spec3 := &gepb.ViewSpec{
		InduceVerticesU64: []uint64{1, 2, 4},
	}

	hash1 := HashViewSpec(spec1)
	hash2 := HashViewSpec(spec2)
	hash3 := HashViewSpec(spec3)

	if hash1 != hash2 {
		t.Error("identical specs should have same hash")
	}
	if hash1 == hash3 {
		t.Error("different specs should have different hash")
	}
	if len(hash1) != 16 {
		t.Errorf("expected hash length 16, got %d", len(hash1))
	}
}
