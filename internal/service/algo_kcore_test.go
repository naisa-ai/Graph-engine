package service

import (
	"context"
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForKCore creates a shim graph for k-core testing.
func createShimGraphForKCore(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createTestGraphForKCore creates a graph with varying core structure.
// Graph structure - a k-core example:
//
//	Core-3: 1 - 2 - 3 - 4 (clique of 4)
//	         |   |   |
//	         5 - 6 - 7  (core-2 extension)
//	             |
//	             8      (core-1 extension)
//	             |
//	             9      (isolated leaf)
//
// The 4 vertices (1,2,3,4) form a dense core.
// Vertices 5,6,7 are core-2.
// Vertices 8,9 are core-1.
func createTestGraphForKCore() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9}

	// Clique edges (1-2-3-4)
	src := []uint64{1, 1, 1, 2, 2, 3}
	dst := []uint64{2, 3, 4, 3, 4, 4}

	// Core-2 extension
	src = append(src, 1, 2, 3, 5, 5, 6)
	dst = append(dst, 5, 6, 7, 6, 7, 7)

	// Core-1 extension
	src = append(src, 6, 8)
	dst = append(dst, 8, 9)

	build := &Build{
		ID:          "build-kcore-test",
		GraphName:   "kcore-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "kcore-test", false, build)
	return version
}

// createSimpleChainGraph creates a simple chain: 1 - 2 - 3 - 4 - 5
// All vertices should have coreness 1.
func createSimpleChainGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-chain",
		GraphName:   "chain",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "chain", false, build)
	return version
}

// createTriangleGraph creates a simple triangle: 1 - 2 - 3 - 1
// All vertices should have coreness 2.
func createTriangleGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3}
	src := []uint64{1, 2, 3}
	dst := []uint64{2, 3, 1}

	build := &Build{
		ID:          "build-triangle",
		GraphName:   "triangle",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "triangle", false, build)
	return version
}

func TestComputeKCore_Basic(t *testing.T) {
	version := createTestGraphForKCore()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	// Should have coreness values for all vertices
	if len(result.Coreness) != int(version.VCount) {
		t.Errorf("expected %d coreness values, got %d", version.VCount, len(result.Coreness))
	}

	// MaxCore should be at least 2 (we have a clique)
	if result.MaxCore < 2 {
		t.Errorf("expected MaxCore >= 2, got %d", result.MaxCore)
	}

	t.Logf("MaxCore: %d", result.MaxCore)
	t.Logf("Coreness values: %v", result.Coreness)
}

func TestComputeKCore_Chain(t *testing.T) {
	version := createSimpleChainGraph()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	// All vertices in a chain should have coreness 1
	if result.MaxCore != 1 {
		t.Errorf("expected MaxCore=1 for chain, got %d", result.MaxCore)
	}

	for i, c := range result.Coreness {
		if c != 1 {
			t.Errorf("vertex %d: expected coreness=1, got %d", i, c)
		}
	}
}

func TestComputeKCore_Triangle(t *testing.T) {
	version := createTriangleGraph()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	// All vertices in a triangle should have coreness 2
	if result.MaxCore != 2 {
		t.Errorf("expected MaxCore=2 for triangle, got %d", result.MaxCore)
	}

	for i, c := range result.Coreness {
		if c != 2 {
			t.Errorf("vertex %d: expected coreness=2, got %d", i, c)
		}
	}
}

func TestComputeKCore_NoShim(t *testing.T) {
	version := createTestGraphForKCore()
	ctx := context.Background()

	_, err := ComputeKCore(ctx, version, nil, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeKCore_ContextCanceled(t *testing.T) {
	version := createTestGraphForKCore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	_, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestKCoreResult_GetKCoreMembers(t *testing.T) {
	version := createTestGraphForKCore()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	// Get members of 1-core (should be all vertices)
	members1 := result.GetKCoreMembers(1)
	if len(members1) != int(version.VCount) {
		t.Errorf("expected all %d vertices in 1-core, got %d", version.VCount, len(members1))
	}

	// Get members of higher cores (should be fewer)
	membersMax := result.GetKCoreMembers(result.MaxCore)
	if len(membersMax) > len(members1) {
		t.Errorf("higher k-core should have fewer members")
	}

	// k=MaxCore+1 should have no members
	membersNone := result.GetKCoreMembers(result.MaxCore + 1)
	if len(membersNone) != 0 {
		t.Errorf("expected 0 members for k=%d, got %d", result.MaxCore+1, len(membersNone))
	}
}

func TestKCoreResult_GetVertexCoreness(t *testing.T) {
	version := createTestGraphForKCore()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	// Get coreness for valid vertex
	coreness, ok := result.GetVertexCoreness(0)
	if !ok {
		t.Error("expected to find coreness for vertex 0")
	}
	if coreness < 1 {
		t.Errorf("expected coreness >= 1, got %d", coreness)
	}

	// Get coreness for invalid vertex
	_, ok = result.GetVertexCoreness(uint32(version.VCount + 10))
	if ok {
		t.Error("expected not found for invalid vertex index")
	}
}

func TestKCoreResult_GetCoreShells(t *testing.T) {
	version := createTestGraphForKCore()
	ctx := context.Background()
	g := createShimGraphForKCore(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KCoreShimConfig{ShimGraph: g}

	result, err := ComputeKCore(ctx, version, nil, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKCore failed: %v", err)
	}

	shells := result.GetCoreShells()

	// Should have at least one shell
	if len(shells) == 0 {
		t.Error("expected at least one shell")
	}

	// Total vertices across shells should equal total vertices
	total := 0
	for k, members := range shells {
		total += len(members)
		t.Logf("Shell k=%d: %d vertices", k, len(members))
	}

	if total != int(version.VCount) {
		t.Errorf("total vertices in shells (%d) != total vertices (%d)", total, version.VCount)
	}
}

func TestValidateKCoreRequest(t *testing.T) {
	// k=0 should be valid (full decomposition)
	err := ValidateKCoreRequest(0)
	if err != nil {
		t.Errorf("expected no error for k=0, got %v", err)
	}

	// k>0 should also be valid
	err = ValidateKCoreRequest(5)
	if err != nil {
		t.Errorf("expected no error for k=5, got %v", err)
	}
}

func TestHashKCoreParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashKCoreParams(0, "view1")
	hash2 := HashKCoreParams(0, "view1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Different parameters should produce different hash
	hash3 := HashKCoreParams(5, "view1")
	if hash1 == hash3 {
		t.Error("different k should produce different hash")
	}

	hash4 := HashKCoreParams(0, "view2")
	if hash1 == hash4 {
		t.Error("different view should produce different hash")
	}
}

func TestDefaultKCoreConfig(t *testing.T) {
	config := DefaultKCoreConfig()

	if config.K != 0 {
		t.Errorf("expected K=0 for full decomposition, got %d", config.K)
	}
}

func TestGraphVersion_KCoreMethods(t *testing.T) {
	version := createTestGraphForKCore()

	// Initially should not have k-core data
	if version.HasKCore() {
		t.Error("should not have k-core data initially")
	}

	// Set k-core data
	coreness := []uint32{2, 2, 2, 2, 1, 1, 1, 0, 0}
	version.SetKCore(coreness, 2)

	// Now should have k-core data
	if !version.HasKCore() {
		t.Error("should have k-core data after SetKCore")
	}

	// Check GetVertexCoreness
	c, ok := version.GetVertexCoreness(0)
	if !ok || c != 2 {
		t.Errorf("expected coreness=2 for vertex 0, got %d, ok=%v", c, ok)
	}

	// Check GetKCoreMembers
	members := version.GetKCoreMembers(2)
	if len(members) != 4 {
		t.Errorf("expected 4 members in 2-core, got %d", len(members))
	}
}
