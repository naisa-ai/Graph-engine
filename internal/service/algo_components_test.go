package service

import (
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForComponents creates a shim graph for components testing.
func createShimGraphForComponents(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createConnectedGraph creates a fully connected triangle graph.
// Graph structure: 1 - 2 - 3 - 1 (single component)
func createConnectedGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3}
	src := []uint64{1, 2, 3}
	dst := []uint64{2, 3, 1}

	build := &Build{
		ID:          "build-connected",
		GraphName:   "connected",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "connected", false, build)
	return version
}

// createDisconnectedGraph creates a graph with 3 separate components.
// Graph structure:
//
//	Component 1: 1 - 2 - 3
//	Component 2: 4 - 5
//	Component 3: 6 (isolated)
func createDisconnectedGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6}
	src := []uint64{1, 2, 4}
	dst := []uint64{2, 3, 5}

	build := &Build{
		ID:          "build-disconnected",
		GraphName:   "disconnected",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "disconnected", false, build)
	return version
}

// createDirectedGraphForComponents creates a directed graph where weak != strong components.
// Graph structure (directed):
//
//	1 -> 2 -> 3
//	^         |
//	+---------+
//
// This forms a strongly connected component.
// Plus: 4 -> 5 (not strongly connected back)
//
// Weak components: 2 (1-2-3 and 4-5)
// Strong components: 2 (1-2-3 cycle and 4-5 as separate nodes or pairs)
func createDirectedGraphForComponents() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 1, 5}

	build := &Build{
		ID:          "build-directed",
		GraphName:   "directed",
		Directed:    true,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "directed", true, build)
	return version
}

// createGraphWithIsolatedVertices creates a graph with isolated vertices.
// Graph structure:
//
//	1 - 2 (connected pair)
//	3 (isolated)
//	4 (isolated)
//	5 (isolated)
func createGraphWithIsolatedVertices() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1}
	dst := []uint64{2}

	build := &Build{
		ID:          "build-isolated",
		GraphName:   "isolated",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "isolated", false, build)
	return version
}

func TestComputeComponents_SingleComponent(t *testing.T) {
	version := createConnectedGraph()
	g := createShimGraphForComponents(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ComponentsConfig{ShimGraph: g}

	result, err := ComputeComponents(version, true, cfg)
	if err != nil {
		t.Fatalf("ComputeComponents failed: %v", err)
	}

	// Should have membership for all vertices
	if len(result.MembershipU32) != int(version.VCount) {
		t.Errorf("expected %d membership values, got %d", version.VCount, len(result.MembershipU32))
	}

	// All vertices should be in the same component
	components := make(map[uint32]bool)
	for _, c := range result.MembershipU32 {
		components[c] = true
	}
	if len(components) != 1 {
		t.Errorf("expected 1 component, got %d", len(components))
	}

	// Check metadata
	if result.Meta["num_components"] != "1" {
		t.Errorf("expected num_components=1, got %s", result.Meta["num_components"])
	}

	t.Logf("Single component test: membership=%v, num_components=%s", result.MembershipU32, result.Meta["num_components"])
}

func TestComputeComponents_MultipleComponents(t *testing.T) {
	version := createDisconnectedGraph()
	g := createShimGraphForComponents(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ComponentsConfig{ShimGraph: g}

	result, err := ComputeComponents(version, true, cfg)
	if err != nil {
		t.Fatalf("ComputeComponents failed: %v", err)
	}

	// Should have membership for all vertices
	if len(result.MembershipU32) != int(version.VCount) {
		t.Errorf("expected %d membership values, got %d", version.VCount, len(result.MembershipU32))
	}

	// Should have 3 components
	components := make(map[uint32]bool)
	for _, c := range result.MembershipU32 {
		components[c] = true
	}
	if len(components) != 3 {
		t.Errorf("expected 3 components, got %d", len(components))
	}

	// Check metadata
	if result.Meta["num_components"] != "3" {
		t.Errorf("expected num_components=3, got %s", result.Meta["num_components"])
	}

	t.Logf("Multiple components test: membership=%v, num_components=%s", result.MembershipU32, result.Meta["num_components"])
}

func TestComputeComponents_WeakVsStrong(t *testing.T) {
	version := createDirectedGraphForComponents()
	g := createShimGraphForComponents(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ComponentsConfig{ShimGraph: g}

	// Test weak components
	weakResult, err := ComputeComponents(version, true, cfg)
	if err != nil {
		t.Fatalf("ComputeComponents (weak) failed: %v", err)
	}

	weakComponents := make(map[uint32]bool)
	for _, c := range weakResult.MembershipU32 {
		weakComponents[c] = true
	}

	// Test strong components
	strongResult, err := ComputeComponents(version, false, cfg)
	if err != nil {
		t.Fatalf("ComputeComponents (strong) failed: %v", err)
	}

	strongComponents := make(map[uint32]bool)
	for _, c := range strongResult.MembershipU32 {
		strongComponents[c] = true
	}

	// Weak should have fewer or equal components than strong
	// (In this graph: weak=2, strong should be >= 2)
	t.Logf("Weak components: %d, Strong components: %d", len(weakComponents), len(strongComponents))
	t.Logf("Weak membership: %v", weakResult.MembershipU32)
	t.Logf("Strong membership: %v", strongResult.MembershipU32)

	// Check that weak mode is recorded in metadata
	if weakResult.Meta["mode"] != "weak" {
		t.Errorf("expected mode=weak, got %s", weakResult.Meta["mode"])
	}
	if strongResult.Meta["mode"] != "strong" {
		t.Errorf("expected mode=strong, got %s", strongResult.Meta["mode"])
	}
}

func TestComputeComponents_NoShim(t *testing.T) {
	version := createConnectedGraph()

	_, err := ComputeComponents(version, true, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeComponents_NilShimGraph(t *testing.T) {
	version := createConnectedGraph()
	cfg := &ComponentsConfig{ShimGraph: nil}

	_, err := ComputeComponents(version, true, cfg)
	if err == nil {
		t.Error("expected error when ShimGraph is nil")
	}
}

func TestComputeComponents_IsolatedVertices(t *testing.T) {
	version := createGraphWithIsolatedVertices()
	g := createShimGraphForComponents(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &ComponentsConfig{ShimGraph: g}

	result, err := ComputeComponents(version, true, cfg)
	if err != nil {
		t.Fatalf("ComputeComponents failed: %v", err)
	}

	// Should have 4 components (1 pair + 3 isolated)
	components := make(map[uint32]bool)
	for _, c := range result.MembershipU32 {
		components[c] = true
	}
	if len(components) != 4 {
		t.Errorf("expected 4 components, got %d", len(components))
	}

	t.Logf("Isolated vertices test: membership=%v, num_components=%d", result.MembershipU32, len(components))
}

func TestGetComponentStats(t *testing.T) {
	// Create a membership array: 3 components with sizes 3, 2, 1
	membership := []uint32{0, 0, 0, 1, 1, 2}

	stats := GetComponentStats(membership)

	if stats.NumComponents != 3 {
		t.Errorf("expected 3 components, got %d", stats.NumComponents)
	}
	if stats.LargestSize != 3 {
		t.Errorf("expected largest size 3, got %d", stats.LargestSize)
	}
	if stats.SmallestSize != 1 {
		t.Errorf("expected smallest size 1, got %d", stats.SmallestSize)
	}
	if stats.IsolatedVertices != 1 {
		t.Errorf("expected 1 isolated vertex, got %d", stats.IsolatedVertices)
	}
	expectedAvg := float64(6) / float64(3)
	if stats.AverageSize != expectedAvg {
		t.Errorf("expected average size %.2f, got %.2f", expectedAvg, stats.AverageSize)
	}

	t.Logf("ComponentStats: %+v", stats)
}

func TestGetComponentStats_Empty(t *testing.T) {
	membership := []uint32{}
	stats := GetComponentStats(membership)

	if stats.NumComponents != 0 {
		t.Errorf("expected 0 components for empty input, got %d", stats.NumComponents)
	}
}

func TestGetComponentStats_SingleComponent(t *testing.T) {
	membership := []uint32{0, 0, 0, 0, 0}
	stats := GetComponentStats(membership)

	if stats.NumComponents != 1 {
		t.Errorf("expected 1 component, got %d", stats.NumComponents)
	}
	if stats.LargestSize != 5 {
		t.Errorf("expected largest size 5, got %d", stats.LargestSize)
	}
	if stats.SmallestSize != 5 {
		t.Errorf("expected smallest size 5, got %d", stats.SmallestSize)
	}
	if stats.IsolatedVertices != 0 {
		t.Errorf("expected 0 isolated vertices, got %d", stats.IsolatedVertices)
	}
}

func TestHashParams_Components(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashParams("weak", "v1")
	hash2 := HashParams("weak", "v1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Different parameters should produce different hash
	hash3 := HashParams("strong", "v1")
	if hash1 == hash3 {
		t.Error("different mode should produce different hash")
	}

	hash4 := HashParams("weak", "v2")
	if hash1 == hash4 {
		t.Error("different version should produce different hash")
	}
}
