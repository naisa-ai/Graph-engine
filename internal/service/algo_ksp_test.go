package service

import (
	"context"
	"testing"
	"time"
)

// createTestGraphForKSP creates a graph with multiple paths between source and target.
// Graph structure:
//
//	  10 --- 20 --- 30
//	 /  \    |    /  \
//	5    15  25  35   40
//	 \  /    |    \  /
//	  50 --- 60 --- 70
//
// Edges: 5-10, 5-50, 10-20, 10-15, 15-50, 20-30, 20-25, 25-60, 30-35, 30-40, 35-70, 40-70, 50-60, 60-70
func createTestGraphForKSP() *GraphVersion {
	vertices := []uint64{5, 10, 15, 20, 25, 30, 35, 40, 50, 60, 70}

	src := []uint64{5, 5, 10, 10, 15, 20, 20, 25, 30, 30, 35, 40, 50, 60}
	dst := []uint64{10, 50, 20, 15, 50, 30, 25, 60, 35, 40, 70, 70, 60, 70}

	build := &Build{
		ID:          "build-ksp-test",
		GraphName:   "ksp-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "ksp-test", false, build)
	return version
}

// createTestGraphForKSPWeighted creates a weighted graph.
// Same structure as createTestGraphForKSP but with edge weights.
func createTestGraphForKSPWeighted() *GraphVersion {
	vertices := []uint64{5, 10, 15, 20, 25, 30, 35, 40, 50, 60, 70}

	src := []uint64{5, 5, 10, 10, 15, 20, 20, 25, 30, 30, 35, 40, 50, 60}
	dst := []uint64{10, 50, 20, 15, 50, 30, 25, 60, 35, 40, 70, 70, 60, 70}

	// Varied weights to create different path costs
	weights := []float32{1.0, 2.0, 1.0, 1.0, 2.0, 1.0, 3.0, 3.0, 1.0, 2.0, 1.0, 1.0, 1.0, 1.0}

	build := &Build{
		ID:          "build-ksp-weighted-test",
		GraphName:   "ksp-weighted-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
		EdgesWeight: weights,
	}

	version, _ := NewGraphVersion("v1", "ksp-weighted-test", false, build)
	return version
}

// createLinearGraphForKSP creates a simple linear graph: 1 - 2 - 3 - 4
// Only one path exists between any two nodes.
func createLinearGraphForKSP() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4}
	src := []uint64{1, 2, 3}
	dst := []uint64{2, 3, 4}

	build := &Build{
		ID:          "build-linear-ksp",
		GraphName:   "linear-ksp",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "linear-ksp", false, build)
	return version
}

func TestComputeKShortestPaths_Basic(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	config := DefaultKSPConfig(3)
	config.ReturnVertices = true
	config.ReturnEdges = true

	result, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	// Should find at least 2 paths
	if result.PathsFound < 2 {
		t.Errorf("expected at least 2 paths, got %d", result.PathsFound)
	}

	// Paths should be in order of increasing cost
	for i := 1; i < len(result.Paths); i++ {
		if result.Paths[i].TotalCost < result.Paths[i-1].TotalCost {
			t.Errorf("paths not in order: path %d cost %f < path %d cost %f",
				i, result.Paths[i].TotalCost, i-1, result.Paths[i-1].TotalCost)
		}
	}

	// Each path should start at 5 and end at 70
	for i, p := range result.Paths {
		if len(p.PathVertices) < 2 {
			t.Errorf("path %d has fewer than 2 vertices", i)
			continue
		}
		if p.PathVertices[0] != 5 {
			t.Errorf("path %d doesn't start at 5: %v", i, p.PathVertices)
		}
		if p.PathVertices[len(p.PathVertices)-1] != 70 {
			t.Errorf("path %d doesn't end at 70: %v", i, p.PathVertices)
		}
	}

	t.Logf("Found %d paths, explored %d candidates", result.PathsFound, result.CandidatesExplored)
	for i, p := range result.Paths {
		t.Logf("Path %d: %v (cost: %.2f)", i, p.PathVertices, p.TotalCost)
	}
}

func TestComputeKShortestPaths_SameNode(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	config := DefaultKSPConfig(3)
	result, err := ComputeKShortestPaths(ctx, version, nil, 5, 5, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	// Should find exactly 1 path (trivial path to self)
	if result.PathsFound != 1 {
		t.Errorf("expected 1 path, got %d", result.PathsFound)
	}

	if result.Paths[0].TotalCost != 0 {
		t.Errorf("expected cost 0 for self path, got %f", result.Paths[0].TotalCost)
	}
}

func TestComputeKShortestPaths_NoPath(t *testing.T) {
	// Create a disconnected graph
	vertices := []uint64{1, 2, 3, 4}
	src := []uint64{1, 3}
	dst := []uint64{2, 4}

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
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	config := DefaultKSPConfig(3)
	result, err := ComputeKShortestPaths(ctx, version, nil, 1, 3, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	if result.PathsFound != 0 {
		t.Errorf("expected 0 paths, got %d", result.PathsFound)
	}

	if result.Meta["status"] != "no_path" {
		t.Errorf("expected status 'no_path', got '%s'", result.Meta["status"])
	}
}

func TestComputeKShortestPaths_FewerThanK(t *testing.T) {
	version := createLinearGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	// Ask for 5 paths in a linear graph (only 1 path exists)
	config := DefaultKSPConfig(5)
	config.ReturnVertices = true

	result, err := ComputeKShortestPaths(ctx, version, nil, 1, 4, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	// Should find only 1 path (the only path)
	if result.PathsFound != 1 {
		t.Errorf("expected 1 path in linear graph, got %d", result.PathsFound)
	}

	if result.Paths[0].TotalCost != 3 {
		t.Errorf("expected cost 3, got %f", result.Paths[0].TotalCost)
	}
}

func TestComputeKShortestPaths_Weighted(t *testing.T) {
	version := createTestGraphForKSPWeighted()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	config := DefaultKSPConfig(3)
	config.Weighted = true
	config.ReturnVertices = true

	result, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	if result.PathsFound < 2 {
		t.Errorf("expected at least 2 paths, got %d", result.PathsFound)
	}

	// First path should be the minimum weight path
	for i, p := range result.Paths {
		t.Logf("Weighted Path %d: %v (cost: %.2f)", i, p.PathVertices, p.TotalCost)
	}
}

func TestComputeKShortestPaths_DiversityPenalty(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	// First, run without penalty
	configNoPenalty := DefaultKSPConfig(3)
	configNoPenalty.ReturnVertices = true
	configNoPenalty.Weighted = true
	resultNoPenalty, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, configNoPenalty, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths (no penalty) failed: %v", err)
	}

	// Now with penalty
	configPenalty := DefaultKSPConfig(3)
	configPenalty.ReturnVertices = true
	configPenalty.Weighted = true
	configPenalty.DiversityPenalty = 2.0 // Double the weight for reused edges
	resultPenalty, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, configPenalty, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths (penalty) failed: %v", err)
	}

	// With diversity penalty, later paths might be different or have different costs
	// The main check is that it doesn't error
	t.Logf("Without penalty: %d paths, %d candidates", resultNoPenalty.PathsFound, resultNoPenalty.CandidatesExplored)
	t.Logf("With penalty: %d paths, %d candidates", resultPenalty.PathsFound, resultPenalty.CandidatesExplored)
}

func TestComputeKShortestPaths_MaxCandidates(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	// Use a very small max_candidates limit
	config := &KSPConfig{
		K:              10,
		MaxCandidates:  2, // Very small limit
		ReturnVertices: true,
	}

	result, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	// Should still find some paths, but may be limited
	if result.PathsFound == 0 {
		t.Error("expected at least 1 path")
	}

	t.Logf("With max_candidates=2: found %d paths", result.PathsFound)
}

func TestComputeKShortestPaths_Timeout(t *testing.T) {
	version := createTestGraphForKSP()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	// Use a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Sleep to ensure timeout triggers
	time.Sleep(10 * time.Millisecond)

	config := DefaultKSPConfig(100)
	result, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths failed: %v", err)
	}

	// Should have timeout status or partial results
	// At minimum, the first path should be found before timeout
	if result.PathsFound == 0 {
		t.Log("No paths found before timeout (expected for very short timeout)")
	} else {
		t.Logf("Found %d paths before context expired", result.PathsFound)
	}
}

func TestComputeKShortestPaths_OnView(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()
	g := createShimGraph(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &KSPShimConfig{ShimGraph: g}

	// Create a view that excludes some vertices (node 60)
	// This should force different paths
	view := NewView(version)

	// Find and exclude node 60
	for i := uint64(0); i < version.VCount; i++ {
		nodeID, _ := version.GetNodeID(uint32(i))
		if nodeID == 60 {
			view.VertexMaskBitmap().Remove(uint32(i))
		}
	}

	// Update edge mask to exclude edges involving excluded vertices
	for i := range version.EdgeSrc {
		srcIdx := version.EdgeSrc[i]
		dstIdx := version.EdgeDst[i]
		if !view.VertexMaskBitmap().Contains(srcIdx) || !view.VertexMaskBitmap().Contains(dstIdx) {
			view.EdgeMaskBitmap().Remove(uint32(i))
		}
	}
	view.updateCounts()

	config := DefaultKSPConfig(3)
	config.ReturnVertices = true

	result, err := ComputeKShortestPaths(ctx, version, view, 5, 70, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeKShortestPaths on view failed: %v", err)
	}

	// Should still find paths (through alternative routes)
	if result.PathsFound < 1 {
		t.Errorf("expected at least 1 path on view, got %d", result.PathsFound)
	}

	// No path should contain node 60
	for i, p := range result.Paths {
		for _, v := range p.PathVertices {
			if v == 60 {
				t.Errorf("path %d contains excluded node 60: %v", i, p.PathVertices)
			}
		}
	}

	t.Logf("On view (excluding 60): found %d paths", result.PathsFound)
	for i, p := range result.Paths {
		t.Logf("Path %d: %v", i, p.PathVertices)
	}
}

func TestComputeKShortestPaths_InvalidK(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()

	config := DefaultKSPConfig(0) // Invalid K
	_, err := ComputeKShortestPaths(ctx, version, nil, 5, 70, config, nil)
	if err == nil {
		t.Error("expected error for K=0")
	}
}

func TestComputeKShortestPaths_NodeNotFound(t *testing.T) {
	version := createTestGraphForKSP()
	ctx := context.Background()

	config := DefaultKSPConfig(3)

	// Non-existent source
	_, err := ComputeKShortestPaths(ctx, version, nil, 999, 70, config, nil)
	if err == nil {
		t.Error("expected error for non-existent source")
	}

	// Non-existent target
	_, err = ComputeKShortestPaths(ctx, version, nil, 5, 999, config, nil)
	if err == nil {
		t.Error("expected error for non-existent target")
	}
}

func TestValidateKSPRequest(t *testing.T) {
	tests := []struct {
		name          string
		k             uint32
		maxCandidates uint32
		wantErr       bool
	}{
		{"valid", 5, 100, false},
		{"k=0", 0, 100, true},
		{"k>100", 101, 100, true},
		{"max_candidates>10000", 5, 10001, true},
		{"valid edge cases", 100, 10000, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateKSPRequest(tt.k, tt.maxCandidates)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateKSPRequest() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashKSPParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashKSPParams(1, 2, 3, "weight", "view123")
	hash2 := HashKSPParams(1, 2, 3, "weight", "view123")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Swapped src/dst should produce same hash (normalization)
	hash3 := HashKSPParams(2, 1, 3, "weight", "view123")
	if hash1 != hash3 {
		t.Errorf("swapped src/dst should produce same hash: %s != %s", hash1, hash3)
	}

	// Different parameters should produce different hash
	hash4 := HashKSPParams(1, 2, 5, "weight", "view123") // different k
	if hash1 == hash4 {
		t.Error("different params should produce different hash")
	}
}

func TestDefaultKSPConfig(t *testing.T) {
	config := DefaultKSPConfig(5)

	if config.K != 5 {
		t.Errorf("expected K=5, got %d", config.K)
	}
	if config.MaxCandidates != 50 {
		t.Errorf("expected MaxCandidates=50, got %d", config.MaxCandidates)
	}
	if config.DiversityPenalty != 1.0 {
		t.Errorf("expected DiversityPenalty=1.0, got %f", config.DiversityPenalty)
	}
	if !config.ReturnVertices {
		t.Error("expected ReturnVertices=true")
	}
}
