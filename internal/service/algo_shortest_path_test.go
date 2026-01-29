package service

import (
	"math"
	"testing"
	"time"
)

// createTestGraphForShortestPath creates a graph for shortest path testing.
// Graph structure (unweighted):
//
//	100 --1-- 101 --1-- 102
//	 |         |         |
//	 1         1         1
//	 |         |         |
//	103 --1-- 104 --1-- 105
//
// For weighted, edges have weights: horizontal=1, vertical=2
func createTestGraphForShortestPath(weighted bool) *GraphVersion {
	vertices := []uint64{100, 101, 102, 103, 104, 105}

	src := []uint64{100, 101, 103, 104, 100, 101, 102}
	dst := []uint64{101, 102, 104, 105, 103, 104, 105}

	var weights []float32
	if weighted {
		// Horizontal edges: weight 1, Vertical edges: weight 2
		weights = []float32{1.0, 1.0, 1.0, 1.0, 2.0, 2.0, 2.0}
	}

	build := &Build{
		ID:          "build-sp-test",
		GraphName:   "sp-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
		EdgesWeight: weights,
	}

	version, _ := NewGraphVersion("v1", "sp-test", false, build)
	return version
}

func TestComputeShortestPath_SameNode(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	result, err := ComputeShortestPath(version, nil, 100, 100, false, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if !result.Found {
		t.Error("path to self should be found")
	}
	if result.TotalCost != 0 {
		t.Errorf("expected cost 0, got %f", result.TotalCost)
	}
	if len(result.PathVertices) != 1 || result.PathVertices[0] != 100 {
		t.Errorf("expected path [100], got %v", result.PathVertices)
	}
}

func TestComputeShortestPath_Adjacent(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	result, err := ComputeShortestPath(version, nil, 100, 101, false, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if !result.Found {
		t.Error("path should be found")
	}
	if result.TotalCost != 1 {
		t.Errorf("expected cost 1, got %f", result.TotalCost)
	}
	if len(result.PathVertices) != 2 {
		t.Errorf("expected 2 vertices in path, got %d", len(result.PathVertices))
	}
}

func TestComputeShortestPath_BFS_Unweighted(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	// 100 to 105: shortest path is 100->101->102->105 or 100->103->104->105 (3 hops)
	result, err := ComputeShortestPath(version, nil, 100, 105, false, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if !result.Found {
		t.Error("path should be found")
	}
	if result.TotalCost != 3 {
		t.Errorf("expected cost 3, got %f", result.TotalCost)
	}
	if len(result.PathVertices) != 4 {
		t.Errorf("expected 4 vertices in path, got %d", len(result.PathVertices))
	}
}

func TestComputeShortestPath_Dijkstra_Weighted(t *testing.T) {
	version := createTestGraphForShortestPath(true)

	// 100 to 105 with weights:
	// Path 1: 100->101->102->105 = 1+1+2 = 4
	// Path 2: 100->103->104->105 = 2+1+2 = 5
	// Path 3: 100->101->104->105 = 1+2+2 = 5
	// Shortest is path 1 with cost 4
	result, err := ComputeShortestPath(version, nil, 100, 105, true, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if !result.Found {
		t.Error("path should be found")
	}
	if result.TotalCost != 4 {
		t.Errorf("expected cost 4, got %f", result.TotalCost)
	}
}

func TestComputeShortestPath_NoPath(t *testing.T) {
	// Create a disconnected graph
	build := &Build{
		ID:          "build-disconnected",
		GraphName:   "disconnected",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: []uint64{100, 101, 200, 201},
		EdgesSrcU64: []uint64{100, 200},
		EdgesDstU64: []uint64{101, 201},
	}
	version, _ := NewGraphVersion("v1", "disconnected", false, build)

	result, err := ComputeShortestPath(version, nil, 100, 200, false, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if result.Found {
		t.Error("path should not be found between disconnected components")
	}
	if !math.IsInf(result.TotalCost, 1) {
		t.Errorf("expected infinite cost for no path, got %f", result.TotalCost)
	}
}

func TestComputeShortestPath_InvalidSource(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeShortestPath(version, nil, 999, 100, false, true, true, nil)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestComputeShortestPath_InvalidTarget(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeShortestPath(version, nil, 100, 999, false, true, true, nil)
	if err == nil {
		t.Error("expected error for invalid target")
	}
}

func TestComputeShortestPath_WithView(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	// Create a view that excludes the middle row (103, 104, 105)
	view := NewView(version)
	view.ApplyExclusions([]uint64{103, 104, 105}, nil)
	view.updateCounts()

	// Now 100 to 102 should only go through top row
	result, err := ComputeShortestPath(version, view, 100, 102, false, true, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}

	if !result.Found {
		t.Error("path should be found through top row")
	}
	if result.TotalCost != 2 {
		t.Errorf("expected cost 2 (100->101->102), got %f", result.TotalCost)
	}
}

func TestComputeShortestPath_ReturnOptions(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	// Without returning vertices
	result, err := ComputeShortestPath(version, nil, 100, 102, false, true, false, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}
	if result.PathVertices != nil {
		t.Error("PathVertices should be nil when returnVertices=false")
	}
	if result.PathEdges == nil {
		t.Error("PathEdges should not be nil when returnEdges=true")
	}

	// Without returning edges
	result, err = ComputeShortestPath(version, nil, 100, 102, false, false, true, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPath failed: %v", err)
	}
	if result.PathVertices == nil {
		t.Error("PathVertices should not be nil when returnVertices=true")
	}
	if result.PathEdges != nil {
		t.Error("PathEdges should be nil when returnEdges=false")
	}
}

func TestComputeShortestPathBatch(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	sources := []uint64{100, 100, 101}
	targets := []uint64{102, 103, 105}

	results, err := ComputeShortestPathBatch(version, nil, sources, targets, false, nil)
	if err != nil {
		t.Fatalf("ComputeShortestPathBatch failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}

	for i, result := range results {
		if !result.Found {
			t.Errorf("result %d: path should be found", i)
		}
	}
}

// Test BFS correctness with a more complex graph
func TestBFS_Correctness(t *testing.T) {
	// Create a wheel graph: center connected to all outer nodes
	// Center: 100, Outer: 101, 102, 103, 104, 105
	vertices := []uint64{100, 101, 102, 103, 104, 105}
	src := []uint64{100, 100, 100, 100, 100, 101, 102, 103, 104, 105}
	dst := []uint64{101, 102, 103, 104, 105, 102, 103, 104, 105, 101}

	build := &Build{
		ID:          "build-wheel",
		GraphName:   "wheel",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "wheel", false, build)

	// From 101 to 103:
	// Direct: 101->102->103 = 2 hops
	// Through center: 101->100->103 = 2 hops
	// Both are equal, so either is valid
	result, _ := ComputeShortestPath(version, nil, 101, 103, false, true, true, nil)

	if !result.Found {
		t.Error("path should be found")
	}
	if result.TotalCost != 2 {
		t.Errorf("expected cost 2, got %f", result.TotalCost)
	}
}

// Test Dijkstra correctness with varying weights
func TestDijkstra_Correctness(t *testing.T) {
	// Create a triangle with different weights
	// 100 --(1)-- 101
	//  |           |
	// (5)        (1)
	//  |           |
	// 102 --(1)-- 103
	build := &Build{
		ID:          "build-triangle",
		GraphName:   "triangle",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: []uint64{100, 101, 102, 103},
		EdgesSrcU64: []uint64{100, 101, 100, 102},
		EdgesDstU64: []uint64{101, 103, 102, 103},
		EdgesWeight: []float32{1.0, 1.0, 5.0, 1.0},
	}

	version, _ := NewGraphVersion("v1", "triangle", false, build)

	// From 100 to 103:
	// Direct via 102: 5 + 1 = 6
	// Via 101: 1 + 1 = 2
	// Dijkstra should find the path via 101
	result, _ := ComputeShortestPath(version, nil, 100, 103, true, true, true, nil)

	if !result.Found {
		t.Error("path should be found")
	}
	if result.TotalCost != 2 {
		t.Errorf("expected cost 2, got %f", result.TotalCost)
	}
	// Check path goes through 101
	if len(result.PathVertices) != 3 {
		t.Errorf("expected 3 vertices in path, got %d", len(result.PathVertices))
	}
}
