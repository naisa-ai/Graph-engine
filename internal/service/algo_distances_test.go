package service

import (
	"math"
	"testing"
	"time"
)

func TestComputeDistances_Basic(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	sources := []uint64{100}
	targets := []uint64{100, 101, 102}

	result, err := ComputeDistances(version, nil, sources, targets, false)
	if err != nil {
		t.Fatalf("ComputeDistances failed: %v", err)
	}

	if result.NumSources != 1 {
		t.Errorf("expected NumSources=1, got %d", result.NumSources)
	}
	if result.NumTargets != 3 {
		t.Errorf("expected NumTargets=3, got %d", result.NumTargets)
	}

	// Check distances from 100
	// 100->100 = 0, 100->101 = 1, 100->102 = 2
	expected := []float64{0, 1, 2}
	for i, exp := range expected {
		got := result.GetDistance(0, i)
		if got != exp {
			t.Errorf("distance to target %d: expected %f, got %f", i, exp, got)
		}
	}
}

func TestComputeDistances_MultiSource(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	sources := []uint64{100, 102}
	targets := []uint64{100, 101, 102}

	result, err := ComputeDistances(version, nil, sources, targets, false)
	if err != nil {
		t.Fatalf("ComputeDistances failed: %v", err)
	}

	if result.NumSources != 2 {
		t.Errorf("expected NumSources=2, got %d", result.NumSources)
	}

	// From 100: 0, 1, 2
	if result.GetDistance(0, 0) != 0 {
		t.Errorf("100->100: expected 0, got %f", result.GetDistance(0, 0))
	}
	if result.GetDistance(0, 1) != 1 {
		t.Errorf("100->101: expected 1, got %f", result.GetDistance(0, 1))
	}
	if result.GetDistance(0, 2) != 2 {
		t.Errorf("100->102: expected 2, got %f", result.GetDistance(0, 2))
	}

	// From 102: 2, 1, 0
	if result.GetDistance(1, 0) != 2 {
		t.Errorf("102->100: expected 2, got %f", result.GetDistance(1, 0))
	}
	if result.GetDistance(1, 1) != 1 {
		t.Errorf("102->101: expected 1, got %f", result.GetDistance(1, 1))
	}
	if result.GetDistance(1, 2) != 0 {
		t.Errorf("102->102: expected 0, got %f", result.GetDistance(1, 2))
	}
}

func TestComputeDistances_Weighted(t *testing.T) {
	version := createTestGraphForShortestPath(true)

	sources := []uint64{100}
	targets := []uint64{103, 104, 105}

	result, err := ComputeDistances(version, nil, sources, targets, true)
	if err != nil {
		t.Fatalf("ComputeDistances failed: %v", err)
	}

	// From 100:
	// To 103: direct = 2
	// To 104: 100->101->104 = 1+2 = 3, or 100->103->104 = 2+1 = 3
	// To 105: 100->101->102->105 = 1+1+2 = 4
	if result.GetDistance(0, 0) != 2 {
		t.Errorf("100->103: expected 2, got %f", result.GetDistance(0, 0))
	}
	if result.GetDistance(0, 1) != 3 {
		t.Errorf("100->104: expected 3, got %f", result.GetDistance(0, 1))
	}
	if result.GetDistance(0, 2) != 4 {
		t.Errorf("100->105: expected 4, got %f", result.GetDistance(0, 2))
	}
}

func TestComputeDistances_Disconnected(t *testing.T) {
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

	sources := []uint64{100}
	targets := []uint64{101, 200}

	result, err := ComputeDistances(version, nil, sources, targets, false)
	if err != nil {
		t.Fatalf("ComputeDistances failed: %v", err)
	}

	// 100->101 should be 1
	if result.GetDistance(0, 0) != 1 {
		t.Errorf("100->101: expected 1, got %f", result.GetDistance(0, 0))
	}

	// 100->200 should be infinity
	if !math.IsInf(result.GetDistance(0, 1), 1) {
		t.Errorf("100->200: expected infinity, got %f", result.GetDistance(0, 1))
	}
}

func TestComputeDistances_EmptySources(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeDistances(version, nil, []uint64{}, []uint64{100}, false)
	if err == nil {
		t.Error("expected error for empty sources")
	}
}

func TestComputeDistances_EmptyTargets(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeDistances(version, nil, []uint64{100}, []uint64{}, false)
	if err == nil {
		t.Error("expected error for empty targets")
	}
}

func TestComputeDistances_InvalidSources(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeDistances(version, nil, []uint64{999}, []uint64{100}, false)
	if err == nil {
		t.Error("expected error for all invalid sources")
	}
}

func TestComputeDistances_WithView(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	// Create a view excluding 101 (top middle)
	view := NewView(version)
	view.ApplyExclusions([]uint64{101}, nil)
	view.updateCounts()

	sources := []uint64{100}
	targets := []uint64{102}

	result, err := ComputeDistances(version, view, sources, targets, false)
	if err != nil {
		t.Fatalf("ComputeDistances failed: %v", err)
	}

	// Without 101, path 100->102 must go through bottom: 100->103->104->105->102
	// That's 4 hops
	dist := result.GetDistance(0, 0)
	if dist != 4 {
		t.Errorf("expected distance 4 (via bottom row), got %f", dist)
	}
}

func TestDistancesResult_GetDistance_OutOfBounds(t *testing.T) {
	result := &DistancesResult{
		Distances:  []float64{0, 1, 2, 3},
		NumSources: 2,
		NumTargets: 2,
	}

	// Out of bounds should return infinity
	if !math.IsInf(result.GetDistance(-1, 0), 1) {
		t.Error("negative source index should return infinity")
	}
	if !math.IsInf(result.GetDistance(0, -1), 1) {
		t.Error("negative target index should return infinity")
	}
	if !math.IsInf(result.GetDistance(5, 0), 1) {
		t.Error("source index >= NumSources should return infinity")
	}
	if !math.IsInf(result.GetDistance(0, 5), 1) {
		t.Error("target index >= NumTargets should return infinity")
	}
}

func TestComputeSingleSourceDistances(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	distances, err := ComputeSingleSourceDistances(version, nil, 100, false)
	if err != nil {
		t.Fatalf("ComputeSingleSourceDistances failed: %v", err)
	}

	// Should have distances to all 6 nodes
	if len(distances) != 6 {
		t.Errorf("expected distances to 6 nodes, got %d", len(distances))
	}

	// Check specific distances
	expected := map[uint64]float64{
		100: 0,
		101: 1,
		102: 2,
		103: 1,
		104: 2,
		105: 3,
	}

	for nodeID, expectedDist := range expected {
		if dist, ok := distances[nodeID]; !ok {
			t.Errorf("missing distance to node %d", nodeID)
		} else if dist != expectedDist {
			t.Errorf("distance to %d: expected %f, got %f", nodeID, expectedDist, dist)
		}
	}
}

func TestComputeSingleSourceDistances_InvalidSource(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	_, err := ComputeSingleSourceDistances(version, nil, 999, false)
	if err == nil {
		t.Error("expected error for invalid source")
	}
}

func TestComputeSingleSourceDistances_WithView(t *testing.T) {
	version := createTestGraphForShortestPath(false)

	// Create view excluding bottom row
	view := NewView(version)
	view.ApplyExclusions([]uint64{103, 104, 105}, nil)
	view.updateCounts()

	distances, err := ComputeSingleSourceDistances(version, view, 100, false)
	if err != nil {
		t.Fatalf("ComputeSingleSourceDistances failed: %v", err)
	}

	// Should only have distances to top row nodes
	if len(distances) != 3 {
		t.Errorf("expected distances to 3 nodes, got %d", len(distances))
	}

	// 103, 104, 105 should not be in distances
	for _, excluded := range []uint64{103, 104, 105} {
		if _, ok := distances[excluded]; ok {
			t.Errorf("excluded node %d should not be in distances", excluded)
		}
	}
}

func TestComputeAllPairsDistances(t *testing.T) {
	// Create a small graph for all-pairs test
	build := &Build{
		ID:          "build-small",
		GraphName:   "small",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: []uint64{100, 101, 102},
		EdgesSrcU64: []uint64{100, 101},
		EdgesDstU64: []uint64{101, 102},
	}
	version, _ := NewGraphVersion("v1", "small", false, build)

	result, err := ComputeAllPairsDistances(version, nil, false)
	if err != nil {
		t.Fatalf("ComputeAllPairsDistances failed: %v", err)
	}

	if result.NumSources != 3 || result.NumTargets != 3 {
		t.Errorf("expected 3x3 matrix, got %dx%d", result.NumSources, result.NumTargets)
	}

	// Check symmetry for undirected graph
	for i := 0; i < result.NumSources; i++ {
		for j := 0; j < result.NumTargets; j++ {
			if result.GetDistance(i, j) != result.GetDistance(j, i) {
				t.Errorf("distance matrix should be symmetric: d[%d][%d]=%f != d[%d][%d]=%f",
					i, j, result.GetDistance(i, j), j, i, result.GetDistance(j, i))
			}
		}
	}
}
