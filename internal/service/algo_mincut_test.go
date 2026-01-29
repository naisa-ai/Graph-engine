package service

import (
	"context"
	"testing"
	"time"
)

// createTestGraphForMinCut creates a simple graph for mincut testing.
// Graph structure:
//
//	   1
//	  /|\
//	 / | \
//	2  3  4
//	 \ | /
//	  \|/
//	   5
//
// All edges have capacity 1 (unweighted).
// Min cut from 1 to 5 should be 3 (cutting edges 2-5, 3-5, 4-5 or 1-2, 1-3, 1-4).
func createTestGraphForMinCut() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}

	src := []uint64{1, 1, 1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5, 5, 5}

	build := &Build{
		ID:          "build-mincut-test",
		GraphName:   "mincut-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "mincut-test", false, build)
	return version
}

// createTestGraphForMinCutWeighted creates a weighted graph for mincut testing.
// Graph structure:
//
//	1 --(10)-- 2 --(5)-- 3
//	|          |         |
//	(5)       (3)       (8)
//	|          |         |
//	4 --(2)--- 5 --(7)-- 6
//
// Min cut from 1 to 6 depends on edge weights.
func createTestGraphForMinCutWeighted() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6}

	src := []uint64{1, 2, 1, 2, 4, 5, 3}
	dst := []uint64{2, 3, 4, 5, 5, 6, 6}
	weights := []float32{10.0, 5.0, 5.0, 3.0, 2.0, 7.0, 8.0}

	build := &Build{
		ID:          "build-mincut-weighted",
		GraphName:   "mincut-weighted",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
		EdgesWeight: weights,
	}

	version, _ := NewGraphVersion("v1", "mincut-weighted", false, build)
	return version
}

// createLinearGraphForMinCut creates a simple linear graph.
// 1 -- 2 -- 3 -- 4 -- 5
// Min cut from 1 to 5 should be 1.
func createLinearGraphForMinCut() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}

	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-linear",
		GraphName:   "linear-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "linear-test", false, build)
	return version
}

func TestComputeSTMinCut_Basic(t *testing.T) {
	version := createTestGraphForMinCut()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	result, err := ComputeSTMinCut(ctx, version, nil, 1, 5, false, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCut failed: %v", err)
	}

	// Min cut should be 3 (3 edges with capacity 1 each)
	if result.CutValue != 3 {
		t.Errorf("expected cut value 3, got %f", result.CutValue)
	}

	// Should have cut edges
	if len(result.CutEdges) == 0 {
		t.Error("expected cut edges")
	}

	// Source side should include vertex 1
	found := false
	for _, v := range result.SourceSideVertices {
		if v == 1 {
			found = true
			break
		}
	}
	if !found {
		t.Error("source side should include vertex 1")
	}

	// Target (5) should not be in source side
	for _, v := range result.SourceSideVertices {
		if v == 5 {
			t.Error("target should not be in source side")
		}
	}
}

func TestComputeSTMinCut_Linear(t *testing.T) {
	version := createLinearGraphForMinCut()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	result, err := ComputeSTMinCut(ctx, version, nil, 1, 5, false, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCut failed: %v", err)
	}

	// Min cut in linear graph should be 1
	if result.CutValue != 1 {
		t.Errorf("expected cut value 1, got %f", result.CutValue)
	}

	// Should have exactly 1 cut edge
	if len(result.CutEdges) != 1 {
		t.Errorf("expected 1 cut edge, got %d", len(result.CutEdges))
	}
}

func TestComputeSTMinCut_Weighted(t *testing.T) {
	version := createTestGraphForMinCutWeighted()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	result, err := ComputeSTMinCut(ctx, version, nil, 1, 6, true, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCut failed: %v", err)
	}

	// Should find a cut
	if result.CutValue <= 0 {
		t.Error("expected positive cut value for weighted graph")
	}

	// Cut edges should be present
	if len(result.CutEdges) == 0 {
		t.Error("expected cut edges")
	}

	// Metadata should indicate weighted
	if result.Meta["weighted"] != "true" {
		t.Error("expected weighted=true in metadata")
	}
}

func TestComputeSTMinCut_InvalidSource(t *testing.T) {
	version := createTestGraphForMinCut()
	ctx := context.Background()

	_, err := ComputeSTMinCut(ctx, version, nil, 999, 5, false, 0, nil)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestComputeSTMinCut_InvalidTarget(t *testing.T) {
	version := createTestGraphForMinCut()
	ctx := context.Background()

	_, err := ComputeSTMinCut(ctx, version, nil, 1, 999, false, 0, nil)
	if err == nil {
		t.Error("expected error for invalid target")
	}
}

func TestComputeSTMinCut_SameSourceTarget(t *testing.T) {
	version := createTestGraphForMinCut()

	// Same source and target - validation should fail
	err := ValidateMinCutRequest(version, nil, 1, 1, 0)
	if err == nil {
		t.Error("expected error for same source and target")
	}
}

func TestComputeSTMinCut_EdgeLimit(t *testing.T) {
	version := createTestGraphForMinCut()
	ctx := context.Background()

	// Set very small edge limit
	_, err := ComputeSTMinCut(ctx, version, nil, 1, 5, false, 2, nil)
	if err == nil {
		t.Error("expected error when edge limit exceeded")
	}
}

func TestComputeSTMinCut_WithTimeout(t *testing.T) {
	version := createTestGraphForMinCut()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	// Test with timeout wrapper
	result, err := ComputeSTMinCutWithTimeout(version, nil, 1, 5, false, 0, 5*time.Second, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCutWithTimeout failed: %v", err)
	}

	if result.CutValue != 3 {
		t.Errorf("expected cut value 3, got %f", result.CutValue)
	}
}

func TestComputeSTMinCut_ContextCancellation(t *testing.T) {
	version := createTestGraphForMinCut()

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := ComputeSTMinCut(ctx, version, nil, 1, 5, false, 0, nil)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestComputeSTMinCut_WithView(t *testing.T) {
	version := createTestGraphForMinCut()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	// Create a view that only includes vertices 1, 2, 3, 5 (excluding 4)
	view := NewView(version)
	idx4, _ := version.GetNodeIndex(4)
	view.VertexMaskBitmap().Remove(idx4)

	// Update edge mask to exclude edges involving vertex 4
	for i := range version.EdgeSrc {
		if version.EdgeSrc[i] == idx4 || version.EdgeDst[i] == idx4 {
			view.EdgeMaskBitmap().Remove(uint32(i))
		}
	}
	view.updateCounts()

	result, err := ComputeSTMinCut(context.Background(), version, view, 1, 5, false, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCut with view failed: %v", err)
	}

	// With vertex 4 excluded, min cut should be 2 (only 2-5, 3-5)
	if result.CutValue != 2 {
		t.Errorf("expected cut value 2 with view, got %f", result.CutValue)
	}
}

func TestComputeSTMinCut_Metadata(t *testing.T) {
	version := createTestGraphForMinCut()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	cfg := &MinCutConfig{ShimGraph: g}

	result, err := ComputeSTMinCut(ctx, version, nil, 1, 5, false, 0, cfg)
	if err != nil {
		t.Fatalf("ComputeSTMinCut failed: %v", err)
	}

	// Check required metadata fields
	requiredFields := []string{
		"source_side_count",
		"cut_edges_count",
		"view_edges",
		"weighted",
		"cut_value",
	}

	for _, field := range requiredFields {
		if _, ok := result.Meta[field]; !ok {
			t.Errorf("expected metadata field %s", field)
		}
	}
}

func TestValidateMinCutRequest(t *testing.T) {
	version := createTestGraphForMinCut()

	// Test valid request
	err := ValidateMinCutRequest(version, nil, 1, 5, 0)
	if err != nil {
		t.Errorf("expected no error for valid request, got %v", err)
	}

	// Test invalid source
	err = ValidateMinCutRequest(version, nil, 999, 5, 0)
	if err == nil {
		t.Error("expected error for invalid source")
	}

	// Test invalid target
	err = ValidateMinCutRequest(version, nil, 1, 999, 0)
	if err == nil {
		t.Error("expected error for invalid target")
	}

	// Test same source and target
	err = ValidateMinCutRequest(version, nil, 1, 1, 0)
	if err == nil {
		t.Error("expected error for same source and target")
	}

	// Test edge limit exceeded
	err = ValidateMinCutRequest(version, nil, 1, 5, 2) // Only 2 edges allowed
	if err == nil {
		t.Error("expected error when edge limit exceeded")
	}
}

func TestCountViewEdges(t *testing.T) {
	version := createTestGraphForMinCut()

	// Full graph
	count := countViewEdges(version, nil)
	if count != 6 {
		t.Errorf("expected 6 edges in full graph, got %d", count)
	}

	// Create view with some edges excluded
	view := NewView(version)
	view.EdgeMaskBitmap().Remove(0)
	view.EdgeMaskBitmap().Remove(1)
	view.updateCounts()

	count = countViewEdges(version, view)
	if count != 4 {
		t.Errorf("expected 4 edges in partial view, got %d", count)
	}
}
