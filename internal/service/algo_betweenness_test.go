package service

import (
	"context"
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForBetweenness creates a shim graph for betweenness testing.
func createShimGraphForBetweenness(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createTestGraphForBetweenness creates a graph with known betweenness structure.
// Graph structure - a "star" with hub:
//
//	    1
//	    |
//	2 - 3 - 4
//	    |
//	    5
//
// Vertex 3 is the hub and should have highest betweenness.
func createTestGraphForBetweenness() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}

	src := []uint64{1, 2, 3, 3}
	dst := []uint64{3, 3, 4, 5}

	build := &Build{
		ID:          "build-betweenness-test",
		GraphName:   "betweenness-test",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "betweenness-test", false, build)
	return version
}

// createPathGraph creates a simple path: 1 - 2 - 3 - 4 - 5
// Middle vertices should have higher betweenness.
func createPathGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 2, 3, 4}
	dst := []uint64{2, 3, 4, 5}

	build := &Build{
		ID:          "build-path",
		GraphName:   "path",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "path", false, build)
	return version
}

// createWeightedGraphForBetweenness creates a weighted graph for betweenness testing.
//
//nolint:unused // test helper for future betweenness tests
func createWeightedGraphForBetweenness() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	src := []uint64{1, 1, 2, 3, 3}
	dst := []uint64{2, 3, 4, 4, 5}
	weights := []float32{1.0, 2.0, 1.0, 1.0, 1.0}

	build := &Build{
		ID:          "build-weighted-betweenness",
		GraphName:   "weighted-betweenness",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
		EdgesWeight: weights,
	}

	version, _ := NewGraphVersion("v1", "weighted-betweenness", false, build)
	version.EdgeColumns = make(map[string]*ColumnData)
	version.EdgeColumns["weight"] = &ColumnData{
		Name:   "weight",
		Type:   ColumnTypeF64,
		F64Val: []float64{1.0, 2.0, 1.0, 1.0, 1.0},
	}
	return version
}

func TestComputeBetweenness_Basic(t *testing.T) {
	version := createTestGraphForBetweenness()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness failed: %v", err)
	}

	// Should have scores for all vertices
	if len(result.Scores) != int(version.VCount) {
		t.Errorf("expected %d scores, got %d", version.VCount, len(result.Scores))
	}

	// The hub vertex (index for vertex 3) should have highest betweenness
	hubIdx, _ := version.GetNodeIndex(3)
	hubScore := result.Scores[hubIdx]

	for i, score := range result.Scores {
		if uint32(i) != hubIdx && score > hubScore {
			t.Errorf("expected hub (vertex 3) to have highest score, but vertex %d has score %.4f > %.4f", i, score, hubScore)
		}
	}

	t.Logf("Betweenness scores: %v", result.Scores)
	t.Logf("Max: %.4f, Min: %.4f", result.MaxScore, result.MinScore)
}

func TestComputeBetweenness_Path(t *testing.T) {
	version := createPathGraph()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness failed: %v", err)
	}

	// In a path graph 1-2-3-4-5:
	// Vertex 3 (middle) should have highest betweenness
	// Vertices 2 and 4 should have next highest
	// Vertices 1 and 5 (endpoints) should have 0 betweenness

	idx1, _ := version.GetNodeIndex(1)
	idx3, _ := version.GetNodeIndex(3)
	idx5, _ := version.GetNodeIndex(5)

	// Endpoints should have 0 betweenness
	if result.Scores[idx1] != 0 {
		t.Errorf("expected 0 betweenness for endpoint 1, got %.4f", result.Scores[idx1])
	}
	if result.Scores[idx5] != 0 {
		t.Errorf("expected 0 betweenness for endpoint 5, got %.4f", result.Scores[idx5])
	}

	// Middle vertex should have highest
	if result.Scores[idx3] != result.MaxScore {
		t.Errorf("expected vertex 3 to have max betweenness, got %.4f (max=%.4f)", result.Scores[idx3], result.MaxScore)
	}

	t.Logf("Path betweenness scores: %v", result.Scores)
}

func TestComputeBetweenness_Sampled(t *testing.T) {
	version := createTestGraphForBetweenness()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := &BetweennessConfig{
		SampleSize:   2, // Sample only 2 vertices
		Normalized:   false,
		WeightColumn: "",
	}
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness with sampling failed: %v", err)
	}

	// Should still have scores for all vertices
	if len(result.Scores) != int(version.VCount) {
		t.Errorf("expected %d scores, got %d", version.VCount, len(result.Scores))
	}

	t.Logf("Sampled betweenness (sample_size=2): %v", result.Scores)
}

func TestComputeBetweenness_Normalized(t *testing.T) {
	version := createPathGraph()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	// First compute without normalization
	configRaw := &BetweennessConfig{
		SampleSize:   0,
		Normalized:   false,
		WeightColumn: "",
	}
	resultRaw, err := ComputeBetweenness(ctx, version, nil, configRaw, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness (raw) failed: %v", err)
	}

	// Then compute with normalization
	configNorm := &BetweennessConfig{
		SampleSize:   0,
		Normalized:   true,
		WeightColumn: "",
	}
	resultNorm, err := ComputeBetweenness(ctx, version, nil, configNorm, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness (normalized) failed: %v", err)
	}

	// Normalized scores should be <= 1.0
	for i, score := range resultNorm.Scores {
		if score > 1.0 {
			t.Errorf("normalized score for vertex %d is > 1.0: %.4f", i, score)
		}
	}

	t.Logf("Raw max: %.4f, Normalized max: %.4f", resultRaw.MaxScore, resultNorm.MaxScore)
}

func TestComputeBetweenness_NoShim(t *testing.T) {
	version := createTestGraphForBetweenness()
	ctx := context.Background()

	config := DefaultBetweennessConfig()
	_, err := ComputeBetweenness(ctx, version, nil, config, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeBetweenness_ContextCanceled(t *testing.T) {
	version := createTestGraphForBetweenness()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	_, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestBetweennessResult_GetVertexBetweenness(t *testing.T) {
	version := createTestGraphForBetweenness()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness failed: %v", err)
	}

	// Get betweenness for valid vertex
	score, ok := result.GetVertexBetweenness(0)
	if !ok {
		t.Error("expected to find betweenness for vertex 0")
	}
	if score < 0 {
		t.Errorf("betweenness should be non-negative, got %.4f", score)
	}

	// Get betweenness for invalid vertex
	_, ok = result.GetVertexBetweenness(uint32(version.VCount + 10))
	if ok {
		t.Error("expected not found for invalid vertex index")
	}
}

func TestBetweennessResult_GetTopKVertices(t *testing.T) {
	version := createPathGraph()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness failed: %v", err)
	}

	// Get top 3 vertices
	topK := result.GetTopKVertices(3)
	if len(topK) != 3 {
		t.Errorf("expected 3 top vertices, got %d", len(topK))
	}

	// Scores should be in descending order
	for i := 1; i < len(topK); i++ {
		if topK[i].Score > topK[i-1].Score {
			t.Errorf("top-k should be in descending order: %.4f > %.4f", topK[i].Score, topK[i-1].Score)
		}
	}

	t.Logf("Top 3 vertices: %v", topK)
}

func TestBetweennessResult_GetVerticesAboveThreshold(t *testing.T) {
	version := createPathGraph()
	ctx := context.Background()
	g := createShimGraphForBetweenness(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &BetweennessShimConfig{ShimGraph: g}

	config := DefaultBetweennessConfig()
	result, err := ComputeBetweenness(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeBetweenness failed: %v", err)
	}

	// Get vertices above threshold 0
	verticesAboveZero := result.GetVerticesAboveThreshold(0)
	// Not all vertices will have betweenness > 0 (endpoints have 0)
	t.Logf("Vertices with betweenness > 0: %v", verticesAboveZero)

	// Get vertices above max (should be none)
	verticesAboveMax := result.GetVerticesAboveThreshold(result.MaxScore + 1)
	if len(verticesAboveMax) != 0 {
		t.Errorf("expected 0 vertices above max score, got %d", len(verticesAboveMax))
	}
}

func TestValidateBetweennessRequest(t *testing.T) {
	// sample_size=0 should be valid (full computation)
	err := ValidateBetweennessRequest(0)
	if err != nil {
		t.Errorf("expected no error for sample_size=0, got %v", err)
	}

	// sample_size>0 should also be valid
	err = ValidateBetweennessRequest(100)
	if err != nil {
		t.Errorf("expected no error for sample_size=100, got %v", err)
	}
}

func TestHashBetweennessParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashBetweennessParams(100, true, "weight", "view1")
	hash2 := HashBetweennessParams(100, true, "weight", "view1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Different parameters should produce different hash
	hash3 := HashBetweennessParams(50, true, "weight", "view1") // different sample_size
	if hash1 == hash3 {
		t.Error("different sample_size should produce different hash")
	}

	hash4 := HashBetweennessParams(100, false, "weight", "view1") // different normalized
	if hash1 == hash4 {
		t.Error("different normalized should produce different hash")
	}

	hash5 := HashBetweennessParams(100, true, "", "view1") // different weight_column
	if hash1 == hash5 {
		t.Error("different weight_column should produce different hash")
	}
}

func TestDefaultBetweennessConfig(t *testing.T) {
	config := DefaultBetweennessConfig()

	if config.SampleSize != 0 {
		t.Errorf("expected SampleSize=0, got %d", config.SampleSize)
	}
	if config.Normalized != false {
		t.Errorf("expected Normalized=false, got %v", config.Normalized)
	}
	if config.WeightColumn != "" {
		t.Errorf("expected WeightColumn='', got %s", config.WeightColumn)
	}
}

func TestGraphVersion_BetweennessMethods(t *testing.T) {
	version := createTestGraphForBetweenness()

	// Initially should not have betweenness data
	if version.HasBetweenness() {
		t.Error("should not have betweenness data initially")
	}

	// Get actual indices for nodes (map iteration order is non-deterministic)
	hubIdx, _ := version.GetNodeIndex(3)
	endIdx, _ := version.GetNodeIndex(1)

	// Build scores array with hub (node 3) having score 6.0
	scores := make([]float64, version.VCount)
	scores[hubIdx] = 6.0
	// All other scores are 0.0 by default

	version.SetBetweenness(scores)

	// Now should have betweenness data
	if !version.HasBetweenness() {
		t.Error("should have betweenness data after SetBetweenness")
	}

	// Check GetVertexBetweenness for hub
	score, ok := version.GetVertexBetweenness(hubIdx)
	if !ok || score != 6.0 {
		t.Errorf("expected score=6.0 for hub vertex, got %.4f, ok=%v", score, ok)
	}

	// Check endpoint
	score, ok = version.GetVertexBetweenness(endIdx)
	if !ok || score != 0.0 {
		t.Errorf("expected score=0.0 for endpoint, got %.4f, ok=%v", score, ok)
	}
}
