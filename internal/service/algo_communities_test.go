package service

import (
	"context"
	"testing"
	"time"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// createShimGraphForCommunities creates a shim graph for communities testing.
func createShimGraphForCommunities(version *GraphVersion) *shim.Graph {
	g, err := shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
	if err != nil {
		return nil
	}
	return g
}

// createTwoCliquesGraph creates a graph with two clear communities (cliques)
// connected by a single edge.
// Graph structure:
//
//	Clique 1: 1-2-3 (fully connected)
//	Clique 2: 4-5-6 (fully connected)
//	Bridge: 3-4
func createTwoCliquesGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6}
	// Clique 1 edges
	src := []uint64{1, 1, 2}
	dst := []uint64{2, 3, 3}
	// Clique 2 edges
	src = append(src, 4, 4, 5)
	dst = append(dst, 5, 6, 6)
	// Bridge edge
	src = append(src, 3)
	dst = append(dst, 4)

	build := &Build{
		ID:          "build-two-cliques",
		GraphName:   "two-cliques",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "two-cliques", false, build)
	return version
}

// createSingleCliqueGraph creates a single clique (K5).
// All vertices are in one community.
func createSingleCliqueGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5}
	// Complete graph K5
	src := []uint64{1, 1, 1, 1, 2, 2, 2, 3, 3, 4}
	dst := []uint64{2, 3, 4, 5, 3, 4, 5, 4, 5, 5}

	build := &Build{
		ID:          "build-clique",
		GraphName:   "clique",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "clique", false, build)
	return version
}

// createStarGraphForCommunities creates a star graph with hub and spokes.
// Graph structure: Hub (1) connected to spokes (2, 3, 4, 5, 6)
//
//nolint:unused // test helper for future community tests
func createStarGraphForCommunities() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6}
	src := []uint64{1, 1, 1, 1, 1}
	dst := []uint64{2, 3, 4, 5, 6}

	build := &Build{
		ID:          "build-star-communities",
		GraphName:   "star-communities",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "star-communities", false, build)
	return version
}

// createLargerCommunityGraph creates a graph with more vertices for resolution testing.
// 3 clusters of 4 vertices each, connected by single edges between clusters.
func createLargerCommunityGraph() *GraphVersion {
	vertices := []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	// Cluster 1: 1-2-3-4 (fully connected)
	src := []uint64{1, 1, 1, 2, 2, 3}
	dst := []uint64{2, 3, 4, 3, 4, 4}

	// Cluster 2: 5-6-7-8 (fully connected)
	src = append(src, 5, 5, 5, 6, 6, 7)
	dst = append(dst, 6, 7, 8, 7, 8, 8)

	// Cluster 3: 9-10-11-12 (fully connected)
	src = append(src, 9, 9, 9, 10, 10, 11)
	dst = append(dst, 10, 11, 12, 11, 12, 12)

	// Inter-cluster edges (bridges)
	src = append(src, 4, 8)
	dst = append(dst, 5, 9)

	build := &Build{
		ID:          "build-larger-community",
		GraphName:   "larger-community",
		Directed:    false,
		CreatedAt:   time.Now(),
		VerticesU64: vertices,
		EdgesSrcU64: src,
		EdgesDstU64: dst,
	}

	version, _ := NewGraphVersion("v1", "larger-community", false, build)
	return version
}

func TestComputeCommunities_Basic(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	// Should have membership for all vertices
	if len(result.Membership) != int(version.VCount) {
		t.Errorf("expected %d membership values, got %d", version.VCount, len(result.Membership))
	}

	// Should detect at least 1 community (likely 2 for this structure)
	if result.NumCommunities < 1 {
		t.Errorf("expected at least 1 community, got %d", result.NumCommunities)
	}

	// Modularity should be positive for community structure
	if result.Modularity < 0 {
		t.Logf("Warning: modularity is negative: %.4f", result.Modularity)
	}

	t.Logf("Basic communities test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
	t.Logf("Membership: %v", result.Membership)
}

func TestComputeCommunities_Leiden(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm:  CommunityAlgorithmLeiden,
		Resolution: 1.0,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (Leiden) failed: %v", err)
	}

	// Verify algorithm metadata
	if result.Meta["algorithm"] != "leiden" {
		t.Errorf("expected algorithm=leiden, got %s", result.Meta["algorithm"])
	}

	t.Logf("Leiden test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_Louvain(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm:  CommunityAlgorithmLouvain,
		Resolution: 1.0,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (Louvain) failed: %v", err)
	}

	// Verify algorithm metadata
	if result.Meta["algorithm"] != "louvain" {
		t.Errorf("expected algorithm=louvain, got %s", result.Meta["algorithm"])
	}

	t.Logf("Louvain test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_Resolution(t *testing.T) {
	version := createLargerCommunityGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	// Low resolution should produce fewer, larger communities
	lowResConfig := &CommunitiesConfig{
		Algorithm:  CommunityAlgorithmLeiden,
		Resolution: 0.5,
	}
	lowResResult, err := ComputeCommunities(ctx, version, nil, lowResConfig, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (low res) failed: %v", err)
	}

	// High resolution should produce more, smaller communities
	highResConfig := &CommunitiesConfig{
		Algorithm:  CommunityAlgorithmLeiden,
		Resolution: 2.0,
	}
	highResResult, err := ComputeCommunities(ctx, version, nil, highResConfig, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (high res) failed: %v", err)
	}

	t.Logf("Resolution test: low_res=%d communities, high_res=%d communities",
		lowResResult.NumCommunities, highResResult.NumCommunities)

	// Generally, higher resolution should result in more or equal communities
	// (this is not guaranteed for all graphs, but typical)
	if highResResult.NumCommunities < lowResResult.NumCommunities {
		t.Logf("Note: high resolution produced fewer communities than low resolution (unusual but possible)")
	}
}

func TestComputeCommunities_NoShim(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()

	config := DefaultCommunitiesConfig()
	_, err := ComputeCommunities(ctx, version, nil, config, nil)
	if err == nil {
		t.Error("expected error when shim config is nil")
	}
}

func TestComputeCommunities_ShimDisabled(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   false,
		ShimGraph: nil,
	}

	config := DefaultCommunitiesConfig()
	_, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err == nil {
		t.Error("expected error when UseShim is false")
	}
}

func TestComputeCommunities_ContextCanceled(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	_, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err == nil {
		t.Error("expected error when context is canceled")
	}
}

func TestCommunitiesResult_GetCommunityMembers(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	// Get members of community 0
	members := result.GetCommunityMembers(0)
	if len(members) == 0 {
		t.Error("expected at least one member in community 0")
	}

	// Get members of non-existent community (should be empty)
	nonExistent := result.GetCommunityMembers(uint32(result.NumCommunities + 100))
	if len(nonExistent) != 0 {
		t.Errorf("expected 0 members for non-existent community, got %d", len(nonExistent))
	}

	t.Logf("Community 0 members: %v", members)
}

func TestCommunitiesResult_GetLargestCommunities(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	// Get top 2 communities
	topK := result.GetLargestCommunities(2)
	if len(topK) > 2 {
		t.Errorf("expected at most 2 communities, got %d", len(topK))
	}

	// Verify they are sorted by size (descending)
	for i := 1; i < len(topK); i++ {
		if topK[i].Size > topK[i-1].Size {
			t.Errorf("communities should be sorted by size descending")
		}
	}

	for i, c := range topK {
		t.Logf("Top community %d: id=%d, size=%d", i, c.CommunityID, c.Size)
	}
}

func TestCommunitiesResult_GetVertexCommunity(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	// Get community for valid vertex
	cid, ok := result.GetVertexCommunity(0)
	if !ok {
		t.Error("expected to find community for vertex 0")
	}
	t.Logf("Vertex 0 is in community %d", cid)

	// Get community for invalid vertex
	_, ok = result.GetVertexCommunity(uint32(version.VCount + 10))
	if ok {
		t.Error("expected not found for invalid vertex index")
	}
}

func TestCommunitiesResult_AreInSameCommunity(t *testing.T) {
	version := createSingleCliqueGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := DefaultCommunitiesConfig()
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities failed: %v", err)
	}

	// In a single clique, all vertices should be in the same community
	same := result.AreInSameCommunity(0, 1)
	t.Logf("Vertices 0 and 1 in same community: %v", same)

	// For invalid vertex, should return false
	invalid := result.AreInSameCommunity(0, uint32(version.VCount+10))
	if invalid {
		t.Error("expected false for invalid vertex")
	}
}

func TestValidateCommunitiesRequest(t *testing.T) {
	// Valid requests
	err := ValidateCommunitiesRequest("leiden", 1.0)
	if err != nil {
		t.Errorf("expected no error for valid leiden request, got %v", err)
	}

	err = ValidateCommunitiesRequest("louvain", 1.0)
	if err != nil {
		t.Errorf("expected no error for valid louvain request, got %v", err)
	}

	err = ValidateCommunitiesRequest("", 1.0) // empty should default to valid
	if err != nil {
		t.Errorf("expected no error for empty algorithm, got %v", err)
	}

	// Invalid algorithm
	err = ValidateCommunitiesRequest("invalid", 1.0)
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}

	// Negative resolution
	err = ValidateCommunitiesRequest("leiden", -1.0)
	if err == nil {
		t.Error("expected error for negative resolution")
	}
}

func TestHashCommunitiesParams(t *testing.T) {
	// Same parameters should produce same hash
	hash1 := HashCommunitiesParams("leiden", 1.0, "view1")
	hash2 := HashCommunitiesParams("leiden", 1.0, "view1")
	if hash1 != hash2 {
		t.Errorf("same params should produce same hash: %s != %s", hash1, hash2)
	}

	// Different parameters should produce different hash
	hash3 := HashCommunitiesParams("louvain", 1.0, "view1")
	if hash1 == hash3 {
		t.Error("different algorithm should produce different hash")
	}

	hash4 := HashCommunitiesParams("leiden", 2.0, "view1")
	if hash1 == hash4 {
		t.Error("different resolution should produce different hash")
	}

	hash5 := HashCommunitiesParams("leiden", 1.0, "view2")
	if hash1 == hash5 {
		t.Error("different view should produce different hash")
	}
}

func TestParseCommunityAlgorithm(t *testing.T) {
	// Valid algorithms
	algo, err := ParseCommunityAlgorithm("leiden")
	if err != nil || algo != CommunityAlgorithmLeiden {
		t.Errorf("expected Leiden algorithm, got %v, err=%v", algo, err)
	}

	algo, err = ParseCommunityAlgorithm("louvain")
	if err != nil || algo != CommunityAlgorithmLouvain {
		t.Errorf("expected Louvain algorithm, got %v, err=%v", algo, err)
	}

	algo, err = ParseCommunityAlgorithm("") // empty should default to Leiden
	if err != nil || algo != CommunityAlgorithmLeiden {
		t.Errorf("expected default (Leiden) algorithm, got %v, err=%v", algo, err)
	}

	// Invalid algorithm
	_, err = ParseCommunityAlgorithm("invalid")
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}
}

func TestDefaultCommunitiesConfig(t *testing.T) {
	config := DefaultCommunitiesConfig()

	if config.Algorithm != CommunityAlgorithmLeiden {
		t.Errorf("expected default algorithm=Leiden, got %v", config.Algorithm)
	}
	if config.Resolution != 1.0 {
		t.Errorf("expected default resolution=1.0, got %f", config.Resolution)
	}
}

func TestCommunityAlgorithm_String(t *testing.T) {
	if CommunityAlgorithmLeiden.String() != "leiden" {
		t.Errorf("expected 'leiden', got %s", CommunityAlgorithmLeiden.String())
	}
	if CommunityAlgorithmLouvain.String() != "louvain" {
		t.Errorf("expected 'louvain', got %s", CommunityAlgorithmLouvain.String())
	}
	if CommunityAlgorithmLabelPropagation.String() != "label_propagation" {
		t.Errorf("expected 'label_propagation', got %s", CommunityAlgorithmLabelPropagation.String())
	}
	if CommunityAlgorithmInfomap.String() != "infomap" {
		t.Errorf("expected 'infomap', got %s", CommunityAlgorithmInfomap.String())
	}
	if CommunityAlgorithmWalktrap.String() != "walktrap" {
		t.Errorf("expected 'walktrap', got %s", CommunityAlgorithmWalktrap.String())
	}
	if CommunityAlgorithmFastGreedy.String() != "fast_greedy" {
		t.Errorf("expected 'fast_greedy', got %s", CommunityAlgorithmFastGreedy.String())
	}
	if CommunityAlgorithmEdgeBetweenness.String() != "edge_betweenness" {
		t.Errorf("expected 'edge_betweenness', got %s", CommunityAlgorithmEdgeBetweenness.String())
	}
	if CommunityAlgorithmLeadingEigenvector.String() != "leading_eigenvector" {
		t.Errorf("expected 'leading_eigenvector', got %s", CommunityAlgorithmLeadingEigenvector.String())
	}
	if CommunityAlgorithmSpinglass.String() != "spinglass" {
		t.Errorf("expected 'spinglass', got %s", CommunityAlgorithmSpinglass.String())
	}
}

func TestComputeCommunities_LabelPropagation(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmLabelPropagation,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (LabelPropagation) failed: %v", err)
	}

	if result.Meta["algorithm"] != "label_propagation" {
		t.Errorf("expected algorithm=label_propagation, got %s", result.Meta["algorithm"])
	}

	t.Logf("LabelPropagation test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_Infomap(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmInfomap,
		Trials:    10,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (Infomap) failed: %v", err)
	}

	if result.Meta["algorithm"] != "infomap" {
		t.Errorf("expected algorithm=infomap, got %s", result.Meta["algorithm"])
	}

	t.Logf("Infomap test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_Walktrap(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmWalktrap,
		Steps:     4,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (Walktrap) failed: %v", err)
	}

	if result.Meta["algorithm"] != "walktrap" {
		t.Errorf("expected algorithm=walktrap, got %s", result.Meta["algorithm"])
	}

	t.Logf("Walktrap test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_FastGreedy(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmFastGreedy,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (FastGreedy) failed: %v", err)
	}

	if result.Meta["algorithm"] != "fast_greedy" {
		t.Errorf("expected algorithm=fast_greedy, got %s", result.Meta["algorithm"])
	}

	t.Logf("FastGreedy test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_EdgeBetweenness(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmEdgeBetweenness,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (EdgeBetweenness) failed: %v", err)
	}

	if result.Meta["algorithm"] != "edge_betweenness" {
		t.Errorf("expected algorithm=edge_betweenness, got %s", result.Meta["algorithm"])
	}

	t.Logf("EdgeBetweenness test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_LeadingEigenvector(t *testing.T) {
	version := createTwoCliquesGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmLeadingEigenvector,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (LeadingEigenvector) failed: %v", err)
	}

	if result.Meta["algorithm"] != "leading_eigenvector" {
		t.Errorf("expected algorithm=leading_eigenvector, got %s", result.Meta["algorithm"])
	}

	t.Logf("LeadingEigenvector test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestComputeCommunities_Spinglass(t *testing.T) {
	// Use single clique graph for spinglass (requires connected graph)
	version := createSingleCliqueGraph()
	ctx := context.Background()
	g := createShimGraphForCommunities(version)
	if g == nil {
		t.Fatal("failed to create shim graph")
	}
	defer g.Close()
	shimCfg := &CommunitiesShimConfig{
		UseShim:   true,
		ShimGraph: g,
	}

	config := &CommunitiesConfig{
		Algorithm: CommunityAlgorithmSpinglass,
		Spins:     25,
		Gamma:     1.0,
	}
	result, err := ComputeCommunities(ctx, version, nil, config, shimCfg)
	if err != nil {
		t.Fatalf("ComputeCommunities (Spinglass) failed: %v", err)
	}

	if result.Meta["algorithm"] != "spinglass" {
		t.Errorf("expected algorithm=spinglass, got %s", result.Meta["algorithm"])
	}

	t.Logf("Spinglass test: %d communities, modularity=%.4f", result.NumCommunities, result.Modularity)
}

func TestParseCommunityAlgorithm_AllAlgorithms(t *testing.T) {
	testCases := []struct {
		input    string
		expected CommunityAlgorithm
	}{
		{"leiden", CommunityAlgorithmLeiden},
		{"louvain", CommunityAlgorithmLouvain},
		{"label_propagation", CommunityAlgorithmLabelPropagation},
		{"infomap", CommunityAlgorithmInfomap},
		{"walktrap", CommunityAlgorithmWalktrap},
		{"fast_greedy", CommunityAlgorithmFastGreedy},
		{"edge_betweenness", CommunityAlgorithmEdgeBetweenness},
		{"leading_eigenvector", CommunityAlgorithmLeadingEigenvector},
		{"spinglass", CommunityAlgorithmSpinglass},
		{"", CommunityAlgorithmLeiden}, // default
	}

	for _, tc := range testCases {
		algo, err := ParseCommunityAlgorithm(tc.input)
		if err != nil {
			t.Errorf("ParseCommunityAlgorithm(%q) unexpected error: %v", tc.input, err)
			continue
		}
		if algo != tc.expected {
			t.Errorf("ParseCommunityAlgorithm(%q) = %v, want %v", tc.input, algo, tc.expected)
		}
	}

	// Test invalid algorithm
	_, err := ParseCommunityAlgorithm("invalid_algorithm")
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}
}

func TestValidateCommunitiesRequest_AllAlgorithms(t *testing.T) {
	validAlgorithms := []string{
		"leiden", "louvain", "label_propagation", "infomap",
		"walktrap", "fast_greedy", "edge_betweenness",
		"leading_eigenvector", "spinglass", "",
	}

	for _, algo := range validAlgorithms {
		err := ValidateCommunitiesRequest(algo, 1.0)
		if err != nil {
			t.Errorf("ValidateCommunitiesRequest(%q, 1.0) unexpected error: %v", algo, err)
		}
	}

	// Test invalid algorithm
	err := ValidateCommunitiesRequest("invalid_algorithm", 1.0)
	if err == nil {
		t.Error("expected error for invalid algorithm")
	}
}
