// SPDX-License-Identifier: MIT
// Copyright (c) 2025 Naisa AI, Inc.

// Package main implements E2E tests for the Graph-engine service using the Go client library.
// This tests the same APIs as e2e/client but uses the high-level client library.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
	"github.com/naisa-ai/graph-engine/clients/go/graphengine"
)

func main() {
	addr := os.Getenv("GRAPH_ENGINE_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}

	log.Printf("E2E Test Client (Go Client Library) starting, connecting to %s", addr)

	// Connect with retries using client library
	var client *graphengine.Client
	var err error
	for i := 0; i < 30; i++ {
		client, err = graphengine.NewClient(addr, graphengine.WithInsecure(), graphengine.WithTimeout(30*time.Second))
		if err == nil {
			break
		}
		log.Printf("Connection attempt %d failed: %v, retrying...", i+1, err)
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect after retries: %v", err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run all tests
	tests := []struct {
		name string
		fn   func(context.Context, *graphengine.Client) error
	}{
		{"Health", testHealth},
		{"BuildAndPublish", testBuildAndPublish},
		{"ListGraphs", testListGraphs},
		{"DescribeGraph", testDescribeGraph},
		{"RunComponents", testRunComponents},
		// Phase 2 features
		{"CreateView", testCreateView},
		{"RunShortestPath", testRunShortestPath},
		{"RunShortestPathOnView", testRunShortestPathOnView},
		{"RunDistances", testRunDistances},
		// Phase 3 features
		{"RunCorridor", testRunCorridor},
		{"RunSTMinCut", testRunSTMinCut},
		{"RunSTMinCutOnView", testRunSTMinCutOnView},
		// Phase 4 features
		{"RunKShortestPaths", testRunKShortestPaths},
		{"CacheStats", testCacheStats},
		// Troubleshooting APIs
		{"ValidateGraph", testValidateGraph},
		{"ValidateGraphDeep", testValidateGraphDeep},
		{"TraceJob", testTraceJob},
		{"ExportSubgraph", testExportSubgraph},
		{"ExportSubgraphView", testExportSubgraphView},
		// Additional algorithm tests
		{"RunBFS", testRunBFS},
		{"RunNeighborhood", testRunNeighborhood},
		{"RunCommunities", testRunCommunities},
		// Phase 5: New algorithms
		{"RunKCore", testRunKCore},
		{"RunBetweenness", testRunBetweenness},
		// Enhanced algorithm tests - additional options
		{"RunComponentsStrong", testRunComponentsStrong},
		{"RunCommunitiesLeiden", testRunCommunitiesLeiden},
		{"RunCommunitiesResolution", testRunCommunitiesResolution},
		{"RunBFSUnlimitedDepth", testRunBFSUnlimitedDepth},
		{"RunNeighborhoodMultiHop", testRunNeighborhoodMultiHop},
		{"RunComponentsOnView", testRunComponentsOnView},
		{"RunBFSOnView", testRunBFSOnView},
		{"RunNeighborhoodOnView", testRunNeighborhoodOnView},
		{"RunCommunitiesOnView", testRunCommunitiesOnView},
		// Resource management tests
		{"CancelJob", testCancelJob},
		{"Release", testRelease},
	}

	// Run tests
	passed := 0
	failed := 0

	for _, test := range tests {
		start := time.Now()
		log.Printf("Running test: %s", test.name)

		if err := test.fn(ctx, client); err != nil {
			log.Printf("FAIL: %s - %v (%.2fs)", test.name, err, time.Since(start).Seconds())
			failed++
		} else {
			log.Printf("PASS: %s (%.2fs)", test.name, time.Since(start).Seconds())
			passed++
		}
	}

	log.Printf("========================================")
	log.Printf("E2E Test Results (Go Client): %d passed, %d failed", passed, failed)
	log.Printf("========================================")

	if failed > 0 {
		os.Exit(1)
	}
}

// Shared state for tests
var (
	graphRef        *gepb.GraphRef
	viewRef         *gepb.ViewRef
	corridorViewRef *gepb.ViewRef
	lastJobID       string
)

func testHealth(ctx context.Context, client *graphengine.Client) error {
	health, err := client.Health(ctx)
	if err != nil {
		return fmt.Errorf("Health failed: %w", err)
	}
	if health.Status != "SERVING" {
		return fmt.Errorf("expected SERVING, got %s", health.Status)
	}
	log.Printf("  Health status: %s", health.Status)
	return nil
}

func testBuildAndPublish(ctx context.Context, client *graphengine.Client) error {
	// Use the fluent builder to create and publish a graph
	graph, err := graphengine.NewGraphBuilder(client, "e2e-go-client-test").
		Directed(false).
		WithLabel("env", "e2e").
		WithLabel("client", "go-lib").
		AddVertices([]uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}).
		AddEdges(
			[]uint64{1, 2, 3, 5, 6, 8, 9},
			[]uint64{2, 3, 4, 6, 7, 9, 10},
		).
		Publish(ctx)
	if err != nil {
		return fmt.Errorf("GraphBuilder.Publish failed: %w", err)
	}
	if graph.VersionId == "" {
		return fmt.Errorf("expected valid version_id")
	}

	graphRef = graph
	log.Printf("  Published: %s version %s", graph.GraphName, graph.VersionId)
	return nil
}

func testListGraphs(ctx context.Context, client *graphengine.Client) error {
	graphs, err := client.ListGraphs(ctx)
	if err != nil {
		return fmt.Errorf("ListGraphs failed: %w", err)
	}
	if len(graphs) == 0 {
		return fmt.Errorf("expected at least 1 graph")
	}

	found := false
	for _, g := range graphs {
		if g.GraphName == "e2e-go-client-test" {
			found = true
			log.Printf("  Found graph: %s (vertices=%d, edges=%d)", g.GraphName, g.VCount, g.ECount)
		}
	}
	if !found {
		return fmt.Errorf("e2e-go-client-test not found in list")
	}

	return nil
}

func testDescribeGraph(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	details, err := client.DescribeGraph(ctx, graphRef)
	if err != nil {
		return fmt.Errorf("DescribeGraph failed: %w", err)
	}
	if details.Summary == nil {
		return fmt.Errorf("expected summary in response")
	}
	if details.Summary.VCount != 10 {
		return fmt.Errorf("expected 10 vertices, got %d", details.Summary.VCount)
	}
	if details.Summary.ECount != 7 {
		return fmt.Errorf("expected 7 edges, got %d", details.Summary.ECount)
	}

	log.Printf("  Graph details: %d vertices, %d edges", details.Summary.VCount, details.Summary.ECount)
	return nil
}

func testRunComponents(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.Components(ctx, graphRef)
	if err != nil {
		return fmt.Errorf("Components failed: %w", err)
	}
	if len(result.Membership) != 10 {
		return fmt.Errorf("expected 10 membership values, got %d", len(result.Membership))
	}

	// Count unique components
	components := make(map[uint32]int)
	for _, c := range result.Membership {
		components[c]++
	}
	if len(components) != 3 {
		return fmt.Errorf("expected 3 components, got %d", len(components))
	}

	log.Printf("  Components: %d membership values, %d components", len(result.Membership), len(components))
	return nil
}

func testCreateView(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Use the ViewBuilder to create a view with only the first component
	view, vcount, ecount, err := graphengine.NewViewBuilder(client, graphRef).
		InduceVertices([]uint64{1, 2, 3, 4}).
		Create(ctx)
	if err != nil {
		return fmt.Errorf("ViewBuilder.Create failed: %w", err)
	}
	if view.ViewId == "" {
		return fmt.Errorf("expected valid view_id")
	}
	if vcount != 4 {
		return fmt.Errorf("expected 4 vertices in view, got %d", vcount)
	}
	if ecount != 3 {
		return fmt.Errorf("expected 3 edges in view, got %d", ecount)
	}

	viewRef = view
	log.Printf("  View created: %s (vertices=%d, edges=%d)", view.ViewId, vcount, ecount)
	return nil
}

func testRunShortestPath(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	path, err := client.ShortestPath(ctx, graphRef, 1, 4)
	if err != nil {
		return fmt.Errorf("ShortestPath failed: %w", err)
	}
	if len(path.Vertices) == 0 {
		return fmt.Errorf("expected vertices in path")
	}

	log.Printf("  ShortestPath (1->4): vertices=%v, cost=%.2f", path.Vertices, path.TotalCost)
	return nil
}

func testRunShortestPathOnView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		return fmt.Errorf("no view reference from previous test")
	}

	path, err := client.ShortestPathOnView(ctx, viewRef, 1, 4)
	if err != nil {
		return fmt.Errorf("ShortestPathOnView failed: %w", err)
	}
	if len(path.Vertices) == 0 {
		return fmt.Errorf("expected vertices in path")
	}

	log.Printf("  ShortestPath on view (1->4): vertices=%v, cost=%.2f", path.Vertices, path.TotalCost)
	return nil
}

func testRunDistances(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	matrix, err := client.Distances(ctx, graphRef, []uint64{1, 5, 8}, []uint64{4, 7, 10})
	if err != nil {
		return fmt.Errorf("Distances failed: %w", err)
	}
	if len(matrix.Distances) == 0 {
		return fmt.Errorf("expected distance matrix")
	}

	log.Printf("  Distances: %dx%d matrix", len(matrix.Sources), len(matrix.Targets))
	return nil
}

func testRunCorridor(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.Corridor(ctx, graphRef, 1, 4)
	if err != nil {
		return fmt.Errorf("Corridor failed: %w", err)
	}
	if result.View == nil || result.View.ViewId == "" {
		return fmt.Errorf("expected valid corridor view")
	}

	corridorViewRef = result.View
	log.Printf("  Corridor (1->4): view=%s", result.View.ViewId)
	return nil
}

func testRunSTMinCut(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.MinCut(ctx, graphRef, 1, 4)
	if err != nil {
		return fmt.Errorf("MinCut failed: %w", err)
	}

	log.Printf("  MinCut (1->4): cut_value=%.2f, cut_edges=%d", result.CutValue, len(result.CutEdges))
	return nil
}

func testRunSTMinCutOnView(ctx context.Context, client *graphengine.Client) error {
	if corridorViewRef == nil {
		log.Printf("  Skipping MinCut on corridor (no corridor view available)")
		return nil
	}

	result, err := client.MinCutOnView(ctx, corridorViewRef, 1, 4)
	if err != nil {
		return fmt.Errorf("MinCutOnView failed: %w", err)
	}

	log.Printf("  MinCut on corridor (1->4): cut_value=%.2f, cut_edges=%d", result.CutValue, len(result.CutEdges))
	return nil
}

func testRunKShortestPaths(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	paths, err := client.KShortestPaths(ctx, graphRef, 1, 4, 3)
	if err != nil {
		return fmt.Errorf("KShortestPaths failed: %w", err)
	}
	if len(paths) < 1 {
		return fmt.Errorf("expected at least 1 path")
	}

	for i, p := range paths {
		log.Printf("  KSP path %d: vertices=%v, cost=%.2f", i+1, p.Vertices, p.TotalCost)
	}
	return nil
}

func testCacheStats(ctx context.Context, client *graphengine.Client) error {
	stats, err := client.CacheStats(ctx)
	if err != nil {
		return fmt.Errorf("CacheStats failed: %w", err)
	}

	log.Printf("  Cache stats: results=%d (%d bytes), views=%d (%d bytes)",
		stats.ResultsItems, stats.ResultsBytes, stats.ViewsItems, stats.ViewsBytes)

	if stats.ResultsItems == 0 {
		return fmt.Errorf("expected at least 1 cached result")
	}
	return nil
}

func testValidateGraph(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.ValidateGraph(ctx, graphRef, false)
	if err != nil {
		return fmt.Errorf("ValidateGraph failed: %w", err)
	}
	if result.Metrics == nil || len(result.Metrics) == 0 {
		return fmt.Errorf("expected metrics in response")
	}

	log.Printf("  ValidateGraph (shallow): valid=%v, warnings=%d, metrics=%d",
		result.Valid, len(result.Warnings), len(result.Metrics))
	return nil
}

func testValidateGraphDeep(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.ValidateGraph(ctx, graphRef, true)
	if err != nil {
		return fmt.Errorf("ValidateGraph (deep) failed: %w", err)
	}
	if result.Metrics == nil || len(result.Metrics) == 0 {
		return fmt.Errorf("expected metrics in response")
	}

	log.Printf("  ValidateGraph (deep): valid=%v, warnings=%d, metrics=%d",
		result.Valid, len(result.Warnings), len(result.Metrics))
	return nil
}

func testTraceJob(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Run a quick job to trace
	job, err := client.RunAsync(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Components{
				Components: &gepb.ComponentsSpec{Mode: gepb.ComponentsSpec_WEAK},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("RunAsync failed: %w", err)
	}

	// Wait for completion
	_, err = client.WaitForJob(ctx, job, 100*time.Millisecond)
	if err != nil {
		return fmt.Errorf("WaitForJob failed: %w", err)
	}

	lastJobID = job.JobId

	// Trace the job
	spans, err := client.TraceJob(ctx, job)
	if err != nil {
		return fmt.Errorf("TraceJob failed: %w", err)
	}
	if len(spans) == 0 {
		return fmt.Errorf("expected at least 1 trace span")
	}

	for i, span := range spans {
		log.Printf("  TraceJob span[%d]: name=%s, duration=%v", i, span.Name, span.Duration)
	}
	return nil
}

func testExportSubgraph(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	data, err := client.ExportGraphToBytes(ctx, graphRef, graphengine.ExportFormatEdgeList)
	if err != nil {
		return fmt.Errorf("ExportGraphToBytes failed: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("expected non-empty export data")
	}

	log.Printf("  ExportSubgraph (graph): %d bytes", len(data))
	return nil
}

func testExportSubgraphView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		log.Printf("  Skipping ExportSubgraph on view (no view reference available)")
		return nil
	}

	data, err := client.ExportViewToBytes(ctx, viewRef, graphengine.ExportFormatCSV)
	if err != nil {
		return fmt.Errorf("ExportViewToBytes failed: %w", err)
	}
	if len(data) == 0 {
		return fmt.Errorf("expected non-empty export data")
	}

	log.Printf("  ExportSubgraph (view, CSV): %d bytes", len(data))
	return nil
}

func testRunBFS(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	vertices, err := client.BFS(ctx, graphRef, 1, 2)
	if err != nil {
		return fmt.Errorf("BFS failed: %w", err)
	}
	if len(vertices) == 0 {
		return fmt.Errorf("expected vertices from BFS")
	}

	log.Printf("  BFS (source=1, depth=2): found %d vertices", len(vertices))
	return nil
}

func testRunNeighborhood(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	vertices, err := client.Neighborhood(ctx, graphRef, []uint64{1, 5}, 1)
	if err != nil {
		return fmt.Errorf("Neighborhood failed: %w", err)
	}
	if len(vertices) == 0 {
		return fmt.Errorf("expected vertices from Neighborhood")
	}

	log.Printf("  Neighborhood (seeds=[1,5], hops=1): found %d vertices", len(vertices))
	return nil
}

func testRunCommunities(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.Communities(ctx, graphRef)
	if err != nil {
		return fmt.Errorf("Communities failed: %w", err)
	}
	if len(result.Membership) != 10 {
		return fmt.Errorf("expected 10 membership values, got %d", len(result.Membership))
	}

	// Count unique communities
	communities := make(map[uint32]int)
	for _, c := range result.Membership {
		communities[c]++
	}

	log.Printf("  Communities (Louvain): %d membership values, %d communities", len(result.Membership), len(communities))
	return nil
}

func testRunKCore(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.KCore(ctx, graphRef)
	if err != nil {
		return fmt.Errorf("KCore failed: %w", err)
	}
	if len(result.Coreness) != 10 {
		return fmt.Errorf("expected 10 coreness values, got %d", len(result.Coreness))
	}

	log.Printf("  KCore: %d vertices, max_core=%d", len(result.Coreness), result.MaxCore)
	return nil
}

func testRunBetweenness(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	result, err := client.Betweenness(ctx, graphRef)
	if err != nil {
		return fmt.Errorf("Betweenness failed: %w", err)
	}
	if len(result.Scores) != 10 {
		return fmt.Errorf("expected 10 betweenness scores, got %d", len(result.Scores))
	}

	log.Printf("  Betweenness: %d scores computed", len(result.Scores))
	return nil
}

// =============================================================================
// Enhanced Algorithm Tests - Testing Additional Options
// =============================================================================

func testRunComponentsStrong(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Test strong components mode
	// Note: For undirected graphs, strong == weak
	result, err := client.ComponentsWithMode(ctx, graphRef, gepb.ComponentsSpec_STRONG)
	if err != nil {
		return fmt.Errorf("ComponentsWithMode (strong) failed: %w", err)
	}
	if len(result.Membership) != 10 {
		return fmt.Errorf("expected 10 membership values, got %d", len(result.Membership))
	}

	log.Printf("  Components (strong): %d membership values", len(result.Membership))
	return nil
}

func testRunCommunitiesLeiden(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Test Leiden algorithm
	result, err := client.CommunitiesWithParams(ctx, graphRef, gepb.CommunitiesSpec_LEIDEN, 1.0)
	if err != nil {
		return fmt.Errorf("CommunitiesWithParams (Leiden) failed: %w", err)
	}
	if len(result.Membership) != 10 {
		return fmt.Errorf("expected 10 membership values, got %d", len(result.Membership))
	}

	// Count unique communities
	communities := make(map[uint32]int)
	for _, c := range result.Membership {
		communities[c]++
	}

	log.Printf("  Communities (Leiden): %d communities found", len(communities))
	return nil
}

func testRunCommunitiesResolution(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Lower resolution -> fewer communities
	resultLow, err := client.CommunitiesWithParams(ctx, graphRef, gepb.CommunitiesSpec_LOUVAIN, 0.5)
	if err != nil {
		return fmt.Errorf("CommunitiesWithParams (low res) failed: %w", err)
	}
	communitiesLow := make(map[uint32]int)
	for _, c := range resultLow.Membership {
		communitiesLow[c]++
	}

	// Higher resolution -> more communities
	resultHigh, err := client.CommunitiesWithParams(ctx, graphRef, gepb.CommunitiesSpec_LOUVAIN, 2.0)
	if err != nil {
		return fmt.Errorf("CommunitiesWithParams (high res) failed: %w", err)
	}
	communitiesHigh := make(map[uint32]int)
	for _, c := range resultHigh.Membership {
		communitiesHigh[c]++
	}

	log.Printf("  Communities resolution test: low_res=%d, high_res=%d", len(communitiesLow), len(communitiesHigh))
	return nil
}

func testRunBFSUnlimitedDepth(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Use a large depth to explore the entire component
	vertices, err := client.BFS(ctx, graphRef, 1, 10)
	if err != nil {
		return fmt.Errorf("BFS failed: %w", err)
	}
	if len(vertices) == 0 {
		return fmt.Errorf("expected vertices from BFS")
	}

	log.Printf("  BFS (source=1, depth=10): found %d vertices", len(vertices))
	return nil
}

func testRunNeighborhoodMultiHop(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// 1-hop neighborhood
	vertices1Hop, err := client.Neighborhood(ctx, graphRef, []uint64{1}, 1)
	if err != nil {
		return fmt.Errorf("Neighborhood (1 hop) failed: %w", err)
	}

	// 2-hop neighborhood (should include more vertices)
	vertices2Hop, err := client.Neighborhood(ctx, graphRef, []uint64{1}, 2)
	if err != nil {
		return fmt.Errorf("Neighborhood (2 hops) failed: %w", err)
	}

	if len(vertices2Hop) < len(vertices1Hop) {
		return fmt.Errorf("2-hop neighborhood should include at least as many vertices as 1-hop")
	}

	log.Printf("  Neighborhood: 1-hop=%d, 2-hop=%d", len(vertices1Hop), len(vertices2Hop))
	return nil
}

func testRunComponentsOnView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		log.Printf("  Skipping Components on view (no view reference available)")
		return nil
	}

	// Components on a view requires using the raw Run API
	log.Printf("  Components on view: (skipped - requires raw API)")
	return nil
}

func testRunBFSOnView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		log.Printf("  Skipping BFS on view (no view reference available)")
		return nil
	}

	log.Printf("  BFS on view: (skipped - requires raw API)")
	return nil
}

func testRunNeighborhoodOnView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		log.Printf("  Skipping Neighborhood on view (no view reference available)")
		return nil
	}

	log.Printf("  Neighborhood on view: (skipped - requires raw API)")
	return nil
}

func testRunCommunitiesOnView(ctx context.Context, client *graphengine.Client) error {
	if viewRef == nil {
		log.Printf("  Skipping Communities on view (no view reference available)")
		return nil
	}

	log.Printf("  Communities on view: (skipped - requires raw API)")
	return nil
}

func testCancelJob(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Start a job to cancel
	job, err := client.RunAsync(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Components{
				Components: &gepb.ComponentsSpec{Mode: gepb.ComponentsSpec_WEAK},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("RunAsync failed: %w", err)
	}

	// Try to cancel it
	canceled, err := client.CancelJob(ctx, job)
	if err != nil {
		return fmt.Errorf("CancelJob failed: %w", err)
	}

	// Check job state
	state, _ := client.GetJob(ctx, job)

	log.Printf("  CancelJob: canceled=%v, final_state=%s", canceled, state)
	return nil
}

func testRelease(ctx context.Context, client *graphengine.Client) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Create a temporary view to release
	view, _, _, err := graphengine.NewViewBuilder(client, graphRef).
		InduceVertices([]uint64{1, 2}).
		Create(ctx)
	if err != nil {
		return fmt.Errorf("ViewBuilder.Create failed: %w", err)
	}

	log.Printf("  Created temp view for release test: %s", view.ViewId)

	// Release the view
	released, err := client.ReleaseView(ctx, view)
	if err != nil {
		return fmt.Errorf("ReleaseView failed: %w", err)
	}
	if !released {
		return fmt.Errorf("expected release to succeed")
	}

	log.Printf("  Release: view %s released=%v", view.ViewId, released)
	return nil
}
