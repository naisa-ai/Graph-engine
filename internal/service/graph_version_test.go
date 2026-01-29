package service

import (
	"sync"
	"testing"
	"time"
)

// createTestBuild creates a Build with specified number of nodes and edges.
func createTestBuild(numNodes, numEdges int) *Build {
	build := &Build{
		ID:          "build-1",
		GraphName:   "test-graph",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: make([]uint64, numNodes),
		EdgesSrcU64: make([]uint64, numEdges),
		EdgesDstU64: make([]uint64, numEdges),
		Labels:      map[string]string{"env": "test"},
	}

	// Populate vertices
	for i := 0; i < numNodes; i++ {
		build.VerticesU64[i] = uint64(i + 1)
	}

	// Populate edges (simple chain)
	for i := 0; i < numEdges; i++ {
		build.EdgesSrcU64[i] = uint64((i % numNodes) + 1)
		build.EdgesDstU64[i] = uint64(((i + 1) % numNodes) + 1)
	}

	return build
}

func TestNewGraphVersion_Basic(t *testing.T) {
	build := createTestBuild(10, 5)

	gv, err := NewGraphVersion("v1", "test-graph", false, build)
	if err != nil {
		t.Fatalf("NewGraphVersion failed: %v", err)
	}

	// Check basic fields
	if gv.ID != "v1" {
		t.Errorf("expected ID v1, got %s", gv.ID)
	}
	if gv.GraphName != "test-graph" {
		t.Errorf("expected graph name test-graph, got %s", gv.GraphName)
	}
	if gv.Directed != false {
		t.Error("expected directed=false")
	}
	if gv.VCount != 10 {
		t.Errorf("expected VCount 10, got %d", gv.VCount)
	}
	if gv.ECount != 5 {
		t.Errorf("expected ECount 5, got %d", gv.ECount)
	}
}

func TestNewGraphVersion_DirectedGraph(t *testing.T) {
	build := createTestBuild(5, 4)

	gv, err := NewGraphVersion("v1", "directed-graph", true, build)
	if err != nil {
		t.Fatalf("NewGraphVersion failed: %v", err)
	}

	if !gv.Directed {
		t.Error("expected directed=true")
	}
}

func TestGraphVersion_NodeMapping(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		GraphName:   "test",
		VerticesU64: []uint64{100, 200, 300, 400, 500},
		EdgesSrcU64: []uint64{100, 200, 300, 400},
		EdgesDstU64: []uint64{200, 300, 400, 500},
	}

	gv, err := NewGraphVersion("v1", "test", false, build)
	if err != nil {
		t.Fatalf("NewGraphVersion failed: %v", err)
	}

	// Check node count
	if gv.VCount != 5 {
		t.Errorf("expected 5 nodes, got %d", gv.VCount)
	}

	// Check GetNodeIndex
	for _, nodeID := range []uint64{100, 200, 300, 400, 500} {
		idx, ok := gv.GetNodeIndex(nodeID)
		if !ok {
			t.Errorf("node %d not found", nodeID)
		}
		// Verify reverse mapping
		reverseID, ok := gv.GetNodeID(idx)
		if !ok {
			t.Errorf("index %d not found", idx)
		}
		if reverseID != nodeID {
			t.Errorf("mapping mismatch: %d -> %d -> %d", nodeID, idx, reverseID)
		}
	}

	// Check nonexistent node
	_, ok := gv.GetNodeIndex(999)
	if ok {
		t.Error("expected not found for nonexistent node")
	}

	// Check out of range index
	_, ok = gv.GetNodeID(999)
	if ok {
		t.Error("expected not found for out of range index")
	}
}

func TestGraphVersion_GetAllNodeIDs(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{10, 20, 30},
		EdgesSrcU64: []uint64{10},
		EdgesDstU64: []uint64{20},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	nodeIDs := gv.GetAllNodeIDs()
	if len(nodeIDs) != 3 {
		t.Errorf("expected 3 node IDs, got %d", len(nodeIDs))
	}

	// Check all IDs are present
	idSet := make(map[uint64]bool)
	for _, id := range nodeIDs {
		idSet[id] = true
	}
	for _, expected := range []uint64{10, 20, 30} {
		if !idSet[expected] {
			t.Errorf("missing node ID %d", expected)
		}
	}

	// Verify it's a copy (modifying shouldn't affect original)
	nodeIDs[0] = 999
	nodeIDs2 := gv.GetAllNodeIDs()
	for _, id := range nodeIDs2 {
		if id == 999 {
			t.Error("GetAllNodeIDs should return a copy")
		}
	}
}

func TestGraphVersion_EdgeArrays(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{1, 2, 3},
		EdgesSrcU64: []uint64{1, 2},
		EdgesDstU64: []uint64{2, 3},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	if len(gv.EdgeSrc) != 2 {
		t.Errorf("expected 2 edge sources, got %d", len(gv.EdgeSrc))
	}
	if len(gv.EdgeDst) != 2 {
		t.Errorf("expected 2 edge destinations, got %d", len(gv.EdgeDst))
	}

	// Verify edges use compact indices
	for i := 0; i < 2; i++ {
		if gv.EdgeSrc[i] >= uint32(gv.VCount) {
			t.Errorf("edge source index out of range: %d", gv.EdgeSrc[i])
		}
		if gv.EdgeDst[i] >= uint32(gv.VCount) {
			t.Errorf("edge dest index out of range: %d", gv.EdgeDst[i])
		}
	}
}

func TestGraphVersion_EdgeAttributes(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{1, 2, 3},
		EdgesSrcU64: []uint64{1, 2},
		EdgesDstU64: []uint64{2, 3},
		EdgesKind:   []uint32{10, 20},
		EdgesWeight: []float32{0.5, 1.5},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	// Check edge kind is copied
	if gv.EdgeKind == nil {
		t.Error("EdgeKind should be set")
	}
	if len(gv.EdgeKind) != 2 {
		t.Errorf("expected 2 edge kinds, got %d", len(gv.EdgeKind))
	}
	if gv.EdgeKind[0] != 10 || gv.EdgeKind[1] != 20 {
		t.Errorf("edge kind mismatch: %v", gv.EdgeKind)
	}

	// Check edge weight is copied
	if gv.EdgeWeight == nil {
		t.Error("EdgeWeight should be set")
	}
	if len(gv.EdgeWeight) != 2 {
		t.Errorf("expected 2 edge weights, got %d", len(gv.EdgeWeight))
	}
	if gv.EdgeWeight[0] != 0.5 || gv.EdgeWeight[1] != 1.5 {
		t.Errorf("edge weight mismatch: %v", gv.EdgeWeight)
	}
}

func TestGraphVersion_NoEdgeAttributes(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{1, 2},
		EdgesSrcU64: []uint64{1},
		EdgesDstU64: []uint64{2},
		// No EdgesKind or EdgesWeight
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	if gv.EdgeKind != nil {
		t.Error("EdgeKind should be nil when not provided")
	}
	if gv.EdgeWeight != nil {
		t.Error("EdgeWeight should be nil when not provided")
	}
}

func TestGraphVersion_EstimateMemory(t *testing.T) {
	build := createTestBuild(1000, 500)

	gv, _ := NewGraphVersion("v1", "test", false, build)

	mem := gv.EstimateMemory()
	if mem == 0 {
		t.Error("memory estimate should be > 0")
	}

	// Verify memory is cached
	mem2 := gv.EstimateMemory()
	if mem != mem2 {
		t.Error("memory estimate should be cached")
	}

	// Rough sanity check on memory size
	// nodeIDToIndex: ~60 bytes per entry * 1000 nodes = 60000
	// indexToNodeID: 8 bytes * 1000 nodes = 8000
	// EdgeSrc + EdgeDst: 4 bytes * 500 * 2 = 4000
	// Total should be at least 72000 bytes
	if mem < 70000 {
		t.Errorf("memory estimate seems too low: %d", mem)
	}
}

func TestGraphVersion_EstimateMemory_WithAttributes(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{1, 2, 3, 4, 5},
		EdgesSrcU64: []uint64{1, 2, 3, 4},
		EdgesDstU64: []uint64{2, 3, 4, 5},
		EdgesKind:   []uint32{1, 2, 3, 4},
		EdgesWeight: []float32{1.0, 2.0, 3.0, 4.0},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	// Should include EdgeKind and EdgeWeight in memory estimate
	mem := gv.EstimateMemory()

	// Create version without attributes for comparison
	buildNoAttr := &Build{
		ID:          "build-2",
		VerticesU64: []uint64{1, 2, 3, 4, 5},
		EdgesSrcU64: []uint64{1, 2, 3, 4},
		EdgesDstU64: []uint64{2, 3, 4, 5},
	}
	gvNoAttr, _ := NewGraphVersion("v2", "test", false, buildNoAttr)
	memNoAttr := gvNoAttr.EstimateMemory()

	// Version with attributes should use more memory
	if mem <= memNoAttr {
		t.Errorf("version with attributes should use more memory: %d vs %d", mem, memNoAttr)
	}
}

func TestGraphVersion_PinUnpin(t *testing.T) {
	build := createTestBuild(10, 5)
	gv, _ := NewGraphVersion("v1", "test", false, build)

	// Initially refcount is 0
	if gv.RefCount() != 0 {
		t.Errorf("expected initial refcount 0, got %d", gv.RefCount())
	}

	// Pin increments
	gv.Pin()
	if gv.RefCount() != 1 {
		t.Errorf("expected refcount 1 after pin, got %d", gv.RefCount())
	}

	gv.Pin()
	if gv.RefCount() != 2 {
		t.Errorf("expected refcount 2 after second pin, got %d", gv.RefCount())
	}

	// Unpin decrements
	canClean := gv.Unpin()
	if canClean {
		t.Error("should not be cleanable with refcount > 0")
	}
	if gv.RefCount() != 1 {
		t.Errorf("expected refcount 1 after unpin, got %d", gv.RefCount())
	}

	canClean = gv.Unpin()
	if !canClean {
		t.Error("should be cleanable with refcount <= 0")
	}
}

func TestGraphVersion_ConcurrentPinUnpin(t *testing.T) {
	build := createTestBuild(10, 5)
	gv, _ := NewGraphVersion("v1", "test", false, build)

	var wg sync.WaitGroup
	numGoroutines := 100
	pinsPerGoroutine := 100

	// Concurrent pins
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < pinsPerGoroutine; j++ {
				gv.Pin()
			}
		}()
	}

	wg.Wait()

	expectedRefCount := numGoroutines * pinsPerGoroutine
	if gv.RefCount() != expectedRefCount {
		t.Errorf("expected refcount %d, got %d", expectedRefCount, gv.RefCount())
	}

	// Concurrent unpins
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < pinsPerGoroutine; j++ {
				gv.Unpin()
			}
		}()
	}

	wg.Wait()

	if gv.RefCount() != 0 {
		t.Errorf("expected refcount 0 after all unpins, got %d", gv.RefCount())
	}
}

func TestGraphVersion_ToSummary(t *testing.T) {
	build := createTestBuild(100, 50)
	gv, _ := NewGraphVersion("version-123", "my-graph", false, build)

	summary := gv.ToSummary()

	if summary.GraphName != "my-graph" {
		t.Errorf("expected graph name my-graph, got %s", summary.GraphName)
	}
	if summary.CurrentVersionId != "version-123" {
		t.Errorf("expected version ID version-123, got %s", summary.CurrentVersionId)
	}
	if summary.Vcount != 100 {
		t.Errorf("expected vcount 100, got %d", summary.Vcount)
	}
	if summary.Ecount != 50 {
		t.Errorf("expected ecount 50, got %d", summary.Ecount)
	}
}

func TestGraphVersion_Labels(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{1, 2},
		EdgesSrcU64: []uint64{1},
		EdgesDstU64: []uint64{2},
		Labels: map[string]string{
			"env":     "prod",
			"version": "1.0",
		},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	if gv.Labels["env"] != "prod" {
		t.Errorf("expected env=prod, got %s", gv.Labels["env"])
	}
	if gv.Labels["version"] != "1.0" {
		t.Errorf("expected version=1.0, got %s", gv.Labels["version"])
	}
}

func TestGraphVersion_EmptyGraph(t *testing.T) {
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{},
		EdgesSrcU64: []uint64{},
		EdgesDstU64: []uint64{},
	}

	gv, err := NewGraphVersion("v1", "empty", false, build)
	if err != nil {
		t.Fatalf("NewGraphVersion failed for empty graph: %v", err)
	}

	if gv.VCount != 0 {
		t.Errorf("expected VCount 0, got %d", gv.VCount)
	}
	if gv.ECount != 0 {
		t.Errorf("expected ECount 0, got %d", gv.ECount)
	}
}

func TestGraphVersion_ImplicitVertices(t *testing.T) {
	// Build with no explicit vertices, only edges
	build := &Build{
		ID:          "build-1",
		VerticesU64: []uint64{}, // No explicit vertices
		EdgesSrcU64: []uint64{10, 20, 30},
		EdgesDstU64: []uint64{20, 30, 40},
	}

	gv, _ := NewGraphVersion("v1", "test", false, build)

	// Should infer vertices from edges
	if gv.VCount != 4 { // 10, 20, 30, 40
		t.Errorf("expected 4 implicit vertices, got %d", gv.VCount)
	}

	// All vertices should be in mapping
	for _, id := range []uint64{10, 20, 30, 40} {
		_, ok := gv.GetNodeIndex(id)
		if !ok {
			t.Errorf("implicit vertex %d not in mapping", id)
		}
	}
}

func TestGraphVersion_PublishedAt(t *testing.T) {
	before := time.Now()
	build := createTestBuild(5, 3)
	gv, _ := NewGraphVersion("v1", "test", false, build)
	after := time.Now()

	if gv.PublishedAt.Before(before) || gv.PublishedAt.After(after) {
		t.Errorf("PublishedAt %v not in expected range [%v, %v]", gv.PublishedAt, before, after)
	}
}
