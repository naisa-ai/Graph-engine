package service

import (
	"math"
	"testing"
)

func TestValidateGraph_EmptyGraph(t *testing.T) {
	// Create an empty graph version
	gv := &GraphVersion{
		ID:            "test-empty",
		GraphName:     "test",
		Directed:      false,
		VCount:        0,
		ECount:        0,
		nodeIDToIndex: make(map[uint64]uint32),
		indexToNodeID: make([]uint64, 0),
		EdgeSrc:       make([]uint32, 0),
		EdgeDst:       make([]uint32, 0),
	}

	result := ValidateGraph(gv, false)

	// Should be valid but with warnings
	if !result.Valid {
		t.Error("empty graph should be valid")
	}

	// Should have warning about no vertices
	foundWarning := false
	for _, w := range result.Warnings {
		if w == "graph has no vertices" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about no vertices")
	}

	// Check metrics
	if result.Metrics["vcount"] != "0" {
		t.Errorf("expected vcount=0, got %s", result.Metrics["vcount"])
	}
}

func TestValidateGraph_SimpleGraph(t *testing.T) {
	// Create a simple graph: 1 -> 2 -> 3
	gv := &GraphVersion{
		ID:        "test-simple",
		GraphName: "test",
		Directed:  true,
		VCount:    3,
		ECount:    2,
		nodeIDToIndex: map[uint64]uint32{
			100: 0,
			200: 1,
			300: 2,
		},
		indexToNodeID: []uint64{100, 200, 300},
		EdgeSrc:       []uint32{0, 1},
		EdgeDst:       []uint32{1, 2},
	}

	result := ValidateGraph(gv, false)

	if !result.Valid {
		t.Errorf("simple graph should be valid, errors: %v", result.Errors)
	}

	// Check metrics
	if result.Metrics["vcount"] != "3" {
		t.Errorf("expected vcount=3, got %s", result.Metrics["vcount"])
	}
	if result.Metrics["ecount"] != "2" {
		t.Errorf("expected ecount=2, got %s", result.Metrics["ecount"])
	}
}

func TestValidateGraph_VCountMismatch(t *testing.T) {
	// Create graph with mismatched VCount
	gv := &GraphVersion{
		ID:            "test-mismatch",
		GraphName:     "test",
		Directed:      false,
		VCount:        10, // Wrong!
		ECount:        1,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1},
		indexToNodeID: []uint64{1, 2}, // Only 2 vertices
		EdgeSrc:       []uint32{0},
		EdgeDst:       []uint32{1},
	}

	result := ValidateGraph(gv, false)

	if result.Valid {
		t.Error("graph with VCount mismatch should be invalid")
	}

	// Check for specific error
	foundError := false
	for _, e := range result.Errors {
		if e.Code == "VCOUNT_MISMATCH" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected VCOUNT_MISMATCH error")
	}
}

func TestValidateGraph_InvalidEdgeIndex(t *testing.T) {
	// Create graph with edge pointing to non-existent vertex
	gv := &GraphVersion{
		ID:            "test-invalid-edge",
		GraphName:     "test",
		Directed:      true,
		VCount:        2,
		ECount:        1,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1},
		indexToNodeID: []uint64{1, 2},
		EdgeSrc:       []uint32{0},
		EdgeDst:       []uint32{99}, // Invalid index!
	}

	result := ValidateGraph(gv, false)

	if result.Valid {
		t.Error("graph with invalid edge index should be invalid")
	}

	foundError := false
	for _, e := range result.Errors {
		if e.Code == "INVALID_EDGE_DST" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected INVALID_EDGE_DST error")
	}
}

func TestValidateGraph_Deep_SelfLoops(t *testing.T) {
	// Create graph with self-loops
	gv := &GraphVersion{
		ID:            "test-self-loop",
		GraphName:     "test",
		Directed:      true,
		VCount:        2,
		ECount:        2,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1},
		indexToNodeID: []uint64{1, 2},
		EdgeSrc:       []uint32{0, 0}, // Self-loop: 0 -> 0
		EdgeDst:       []uint32{1, 0},
	}

	result := ValidateGraph(gv, true) // Deep validation

	if !result.Valid {
		t.Errorf("graph with self-loops should be valid (warning only)")
	}

	// Check for self-loop metric
	if result.Metrics["self_loops"] != "1" {
		t.Errorf("expected self_loops=1, got %s", result.Metrics["self_loops"])
	}
}

func TestValidateGraph_Deep_DuplicateEdges(t *testing.T) {
	// Create graph with duplicate edges
	gv := &GraphVersion{
		ID:            "test-duplicate",
		GraphName:     "test",
		Directed:      true,
		VCount:        2,
		ECount:        3,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1},
		indexToNodeID: []uint64{1, 2},
		EdgeSrc:       []uint32{0, 0, 0}, // Three edges 0 -> 1
		EdgeDst:       []uint32{1, 1, 1},
	}

	result := ValidateGraph(gv, true) // Deep validation

	if !result.Valid {
		t.Errorf("graph with duplicate edges should be valid (warning only)")
	}

	// Check for duplicate edges metric
	if result.Metrics["duplicate_edges"] != "2" {
		t.Errorf("expected duplicate_edges=2, got %s", result.Metrics["duplicate_edges"])
	}
}

func TestValidateGraph_Deep_NaNWeights(t *testing.T) {
	// Create graph with NaN weights
	gv := &GraphVersion{
		ID:            "test-nan",
		GraphName:     "test",
		Directed:      true,
		VCount:        2,
		ECount:        2,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1},
		indexToNodeID: []uint64{1, 2},
		EdgeSrc:       []uint32{0, 1},
		EdgeDst:       []uint32{1, 0},
		EdgeWeight:    []float32{1.0, float32(math.NaN())}, // NaN
	}

	result := ValidateGraph(gv, true) // Deep validation

	if result.Valid {
		t.Error("graph with NaN weights should be invalid")
	}

	foundError := false
	for _, e := range result.Errors {
		if e.Code == "WEIGHT_NAN" {
			foundError = true
			break
		}
	}
	if !foundError {
		t.Error("expected WEIGHT_NAN error")
	}
}

func TestValidateGraph_Deep_Components(t *testing.T) {
	// Create graph with multiple components
	// Component 1: 0 -> 1
	// Component 2: 2 -> 3
	gv := &GraphVersion{
		ID:            "test-components",
		GraphName:     "test",
		Directed:      true,
		VCount:        4,
		ECount:        2,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1, 3: 2, 4: 3},
		indexToNodeID: []uint64{1, 2, 3, 4},
		EdgeSrc:       []uint32{0, 2},
		EdgeDst:       []uint32{1, 3},
	}

	result := ValidateGraph(gv, true) // Deep validation

	// Should have warning about disconnected components
	if result.Metrics["connected_components"] != "2" {
		t.Errorf("expected connected_components=2, got %s", result.Metrics["connected_components"])
	}

	foundWarning := false
	for _, w := range result.Warnings {
		if w == "graph has 2 disconnected components" {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("expected warning about disconnected components")
	}
}

func TestValidateGraph_Deep_DegreeStats(t *testing.T) {
	// Create a star graph: center node 0 connected to 1, 2, 3
	gv := &GraphVersion{
		ID:            "test-degree",
		GraphName:     "test",
		Directed:      true,
		VCount:        4,
		ECount:        3,
		nodeIDToIndex: map[uint64]uint32{0: 0, 1: 1, 2: 2, 3: 3},
		indexToNodeID: []uint64{0, 1, 2, 3},
		EdgeSrc:       []uint32{0, 0, 0}, // 0 -> 1, 0 -> 2, 0 -> 3
		EdgeDst:       []uint32{1, 2, 3},
	}

	result := ValidateGraph(gv, true) // Deep validation

	// Check degree metrics
	if result.Metrics["max_out_degree"] != "3" {
		t.Errorf("expected max_out_degree=3, got %s", result.Metrics["max_out_degree"])
	}
	if result.Metrics["min_out_degree"] != "0" {
		t.Errorf("expected min_out_degree=0, got %s", result.Metrics["min_out_degree"])
	}
}

func TestValidateGraph_OrphanVertices(t *testing.T) {
	// Create graph with orphan vertices
	gv := &GraphVersion{
		ID:            "test-orphan",
		GraphName:     "test",
		Directed:      true,
		VCount:        5,
		ECount:        1,
		nodeIDToIndex: map[uint64]uint32{1: 0, 2: 1, 3: 2, 4: 3, 5: 4},
		indexToNodeID: []uint64{1, 2, 3, 4, 5},
		EdgeSrc:       []uint32{0}, // Only one edge: 0 -> 1
		EdgeDst:       []uint32{1},
	}

	result := ValidateGraph(gv, false)

	// Should have warning about orphan vertices
	if result.Metrics["orphan_vertices"] != "3" {
		t.Errorf("expected orphan_vertices=3, got %s", result.Metrics["orphan_vertices"])
	}
}
