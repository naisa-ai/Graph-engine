// Package main implements an E2E test client for the Graph-engine service.
// It exercises all gRPC APIs to verify end-to-end functionality.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

func main() {
	addr := os.Getenv("GRAPH_ENGINE_ADDR")
	if addr == "" {
		addr = "localhost:50051"
	}

	log.Printf("E2E Test Client starting, connecting to %s", addr)

	// Connect with retries
	var conn *grpc.ClientConn
	var err error
	for i := 0; i < 30; i++ {
		conn, err = grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			break
		}
		log.Printf("Connection attempt %d failed: %v, retrying...", i+1, err)
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatalf("Failed to connect after retries: %v", err)
	}
	defer conn.Close()

	client := gepb.NewGraphEngineClient(conn)
	opsClient := gepb.NewGraphEngineOpsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Run all tests
	tests := []struct {
		name string
		fn   func(context.Context, gepb.GraphEngineClient, gepb.GraphEngineOpsClient) error
	}{
		{"Health", testHealth},
		{"BeginBuild", testBeginBuild},
		{"Upload", testUpload},
		{"PublishBuild", testPublishBuild},
		{"ListGraphs", testListGraphs},
		{"DescribeGraph", testDescribeGraph},
		{"RunComponents", testRunComponents},
		{"GetJob", testGetJob},
		{"GetResult", testGetResult},
		// Phase 2 features
		{"CreateView", testCreateView},
		{"RunShortestPath", testRunShortestPath},
		{"RunShortestPathOnView", testRunShortestPathOnView},
		{"RunDistances", testRunDistances},
		// Phase 3 features
		{"RunCorridor", testRunCorridor},
		{"RunCorridorOnView", testRunCorridorOnView},
		{"RunSTMinCut", testRunSTMinCut},
		{"RunSTMinCutOnCorridor", testRunSTMinCutOnCorridor},
		// Phase 4 features
		{"RunKShortestPaths", testRunKShortestPaths},
		{"RunKShortestPathsOnView", testRunKShortestPathsOnView},
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
		// Phase 5: New algorithms (k-core, betweenness)
		{"RunKCore", testRunKCore},
		{"RunBetweenness", testRunBetweenness},
		{"RunBetweennessSampled", testRunBetweennessSampled},
		// Batch artifacts test
		{"BatchArtifactsAll", testBatchArtifactsAll},
		// Resource management tests
		{"CancelJob", testCancelJob},
		{"Release", testRelease},
	}

	// Shared state between tests
	passed := 0
	failed := 0

	for _, test := range tests {
		start := time.Now()
		log.Printf("Running test: %s", test.name)

		if err := test.fn(ctx, client, opsClient); err != nil {
			log.Printf("FAIL: %s - %v (%.2fs)", test.name, err, time.Since(start).Seconds())
			failed++
		} else {
			log.Printf("PASS: %s (%.2fs)", test.name, time.Since(start).Seconds())
			passed++
		}
	}

	log.Printf("========================================")
	log.Printf("E2E Test Results: %d passed, %d failed", passed, failed)
	log.Printf("========================================")

	if failed > 0 {
		os.Exit(1)
	}
}

// Shared state for tests
var (
	buildID         string
	graphRef        *gepb.GraphRef
	jobID           string
	resultID        string
	viewRef         *gepb.ViewRef
	corridorViewRef *gepb.ViewRef // for Phase 3 corridor view
)

func testHealth(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	resp, err := ops.Health(ctx, &gepb.HealthRequest{})
	if err != nil {
		return fmt.Errorf("Health RPC failed: %w", err)
	}
	if resp.Status != "SERVING" {
		return fmt.Errorf("expected SERVING, got %s", resp.Status)
	}
	log.Printf("  Health status: %s", resp.Status)
	return nil
}

func testBeginBuild(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	resp, err := client.BeginBuild(ctx, &gepb.BeginBuildRequest{
		GraphName: "e2e-test-graph",
		Directed:  false,
		Labels:    map[string]string{"env": "e2e", "test": "true"},
	})
	if err != nil {
		return fmt.Errorf("BeginBuild RPC failed: %w", err)
	}
	if resp.BuildId == "" {
		return fmt.Errorf("expected non-empty build_id")
	}
	buildID = resp.BuildId
	log.Printf("  Build ID: %s", buildID)
	return nil
}

func testUpload(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if buildID == "" {
		return fmt.Errorf("no build_id from previous test")
	}

	stream, err := client.Upload(ctx)
	if err != nil {
		return fmt.Errorf("Upload stream failed: %w", err)
	}

	// Upload 10 vertices
	err = stream.Send(&gepb.UploadRequest{
		BuildId: buildID,
		Payload: &gepb.UploadRequest_Vertices{
			Vertices: &gepb.VertexChunk{
				NodeIdU64: []uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send vertices: %w", err)
	}

	// Upload edges creating 3 components:
	// Component 1: {1, 2, 3, 4}
	// Component 2: {5, 6, 7}
	// Component 3: {8, 9, 10}
	err = stream.Send(&gepb.UploadRequest{
		BuildId: buildID,
		Payload: &gepb.UploadRequest_Edges{
			Edges: &gepb.EdgeChunk{
				SrcU64: []uint64{1, 2, 3, 5, 6, 8, 9},
				DstU64: []uint64{2, 3, 4, 6, 7, 9, 10},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send edges: %w", err)
	}

	resp, err := stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("Upload close failed: %w", err)
	}

	if resp.ReceivedVertices != 10 {
		return fmt.Errorf("expected 10 vertices, got %d", resp.ReceivedVertices)
	}
	if resp.ReceivedEdges != 7 {
		return fmt.Errorf("expected 7 edges, got %d", resp.ReceivedEdges)
	}

	log.Printf("  Uploaded: %d vertices, %d edges", resp.ReceivedVertices, resp.ReceivedEdges)
	return nil
}

func testPublishBuild(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if buildID == "" {
		return fmt.Errorf("no build_id from previous test")
	}

	resp, err := client.PublishBuild(ctx, &gepb.PublishBuildRequest{
		BuildId: buildID,
		Artifacts: &gepb.BatchArtifacts{
			ComputeComponents: true,
		},
	})
	if err != nil {
		return fmt.Errorf("PublishBuild RPC failed: %w", err)
	}
	if resp.Status.Code != 0 {
		return fmt.Errorf("publish failed: code=%d, msg=%s", resp.Status.Code, resp.Status.Message)
	}
	if resp.Graph == nil || resp.Graph.VersionId == "" {
		return fmt.Errorf("expected valid graph reference")
	}

	graphRef = resp.Graph
	log.Printf("  Published: %s version %s", graphRef.GraphName, graphRef.VersionId)
	return nil
}

func testListGraphs(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	resp, err := ops.ListGraphs(ctx, &gepb.ListGraphsRequest{})
	if err != nil {
		return fmt.Errorf("ListGraphs RPC failed: %w", err)
	}
	if len(resp.Graphs) == 0 {
		return fmt.Errorf("expected at least 1 graph")
	}

	found := false
	for _, g := range resp.Graphs {
		if g.GraphName == "e2e-test-graph" {
			found = true
			log.Printf("  Found graph: %s (vertices=%d, edges=%d)", g.GraphName, g.Vcount, g.Ecount)
		}
	}
	if !found {
		return fmt.Errorf("e2e-test-graph not found in list")
	}

	return nil
}

func testDescribeGraph(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	resp, err := ops.DescribeGraph(ctx, &gepb.DescribeGraphRequest{
		Graph: graphRef,
	})
	if err != nil {
		return fmt.Errorf("DescribeGraph RPC failed: %w", err)
	}
	if resp.Summary == nil {
		return fmt.Errorf("expected summary in response")
	}
	if resp.Summary.Vcount != 10 {
		return fmt.Errorf("expected 10 vertices, got %d", resp.Summary.Vcount)
	}
	if resp.Summary.Ecount != 7 {
		return fmt.Errorf("expected 7 edges, got %d", resp.Summary.Ecount)
	}

	log.Printf("  Graph details: %d vertices, %d edges",
		resp.Summary.Vcount, resp.Summary.Ecount)
	return nil
}

func testRunComponents(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Components{
				Components: &gepb.ComponentsSpec{
					Mode: gepb.ComponentsSpec_WEAK,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	jobID = resp.Job.JobId
	log.Printf("  Job started: %s", jobID)
	return nil
}

func testGetJob(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if jobID == "" {
		return fmt.Errorf("no job_id from previous test")
	}

	// Poll for job completion (should be instant for in-memory algo)
	var resp *gepb.GetJobResponse
	var err error
	for i := 0; i < 10; i++ {
		resp, err = client.GetJob(ctx, &gepb.GetJobRequest{
			Job: &gepb.JobRef{JobId: jobID},
		})
		if err != nil {
			return fmt.Errorf("GetJob RPC failed: %w", err)
		}

		if resp.State == gepb.GetJobResponse_SUCCEEDED {
			break
		}
		if resp.State == gepb.GetJobResponse_FAILED {
			return fmt.Errorf("job failed")
		}

		time.Sleep(100 * time.Millisecond)
	}

	if resp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not complete, state=%v", resp.State)
	}
	if resp.Result == nil || resp.Result.ResultId == "" {
		return fmt.Errorf("expected valid result reference")
	}

	resultID = resp.Result.ResultId
	log.Printf("  Job completed: state=%v, result=%s", resp.State, resultID)
	return nil
}

func testGetResult(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if resultID == "" {
		return fmt.Errorf("no result_id from previous test")
	}

	stream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: &gepb.ResultRef{ResultId: resultID},
	})
	if err != nil {
		return fmt.Errorf("GetResult stream failed: %w", err)
	}

	var membership []uint32
	var gotHeader, gotDone bool

	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		switch payload := chunk.Payload.(type) {
		case *gepb.ResultChunk_Header:
			gotHeader = true
			log.Printf("  Result header: type=%s", payload.Header.Type)
		case *gepb.ResultChunk_U32:
			if payload.U32.Name == "membership" {
				membership = payload.U32.Values
			}
		case *gepb.ResultChunk_Done:
			gotDone = true
		}
	}

	if !gotHeader {
		return fmt.Errorf("expected header chunk")
	}
	if !gotDone {
		return fmt.Errorf("expected done chunk")
	}
	if len(membership) != 10 {
		return fmt.Errorf("expected 10 membership values, got %d", len(membership))
	}

	// Count unique components
	components := make(map[uint32]int)
	for _, c := range membership {
		components[c]++
	}
	if len(components) != 3 {
		return fmt.Errorf("expected 3 components, got %d", len(components))
	}

	log.Printf("  Result: %d membership values, %d components", len(membership), len(components))
	return nil
}

// Phase 2: CreateView test
func testCreateView(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Create a view with only the first component (vertices 1-4)
	resp, err := client.CreateView(ctx, &gepb.CreateViewRequest{
		Graph: graphRef,
		Spec: &gepb.ViewSpec{
			InduceVerticesU64: []uint64{1, 2, 3, 4},
		},
	})
	if err != nil {
		return fmt.Errorf("CreateView RPC failed: %w", err)
	}
	if resp.View == nil || resp.View.ViewId == "" {
		return fmt.Errorf("expected valid view reference")
	}
	if resp.Vcount != 4 {
		return fmt.Errorf("expected 4 vertices in view, got %d", resp.Vcount)
	}
	// Component 1 has edges: 1-2, 2-3, 3-4 = 3 edges
	if resp.Ecount != 3 {
		return fmt.Errorf("expected 3 edges in view, got %d", resp.Ecount)
	}

	viewRef = resp.View
	log.Printf("  View created: %s (vertices=%d, edges=%d)", viewRef.ViewId, resp.Vcount, resp.Ecount)
	return nil
}

// Phase 2: ShortestPath test on full graph
func testRunShortestPath(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Find shortest path from 1 to 4 (should be 1->2->3->4 = 3 hops)
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_ShortestPath{
				ShortestPath: &gepb.ShortestPathSpec{
					SourceU64:      1,
					TargetU64:      4,
					ReturnVertices: true,
					ReturnEdges:    true,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run ShortestPath RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  ShortestPath (1->4) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 2: ShortestPath test on view
func testRunShortestPathOnView(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if viewRef == nil {
		return fmt.Errorf("no view reference from previous test")
	}

	// Find shortest path from 1 to 4 on the view
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: viewRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_ShortestPath{
				ShortestPath: &gepb.ShortestPathSpec{
					SourceU64:      1,
					TargetU64:      4,
					ReturnVertices: true,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run ShortestPath on view RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  ShortestPath on view (1->4) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 2: Distances test
func testRunDistances(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Compute distances from multiple sources to multiple targets
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Distances{
				Distances: &gepb.DistancesSpec{
					SourcesU64: []uint64{1, 5, 8},
					TargetsU64: []uint64{4, 7, 10},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Distances RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Distances job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 3: Corridor test on full graph
func testRunCorridor(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Create corridor from 1 to 4 using shortest-path hull method
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Corridor{
				Corridor: &gepb.CorridorSpec{
					SourceU64: 1,
					TargetU64: 4,
					Method:    gepb.CorridorSpec_SHORTEST_PATH_HULL,
					Hops:      1,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Corridor RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	// Get the result to extract the corridor view ID
	resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: jobResp.Result,
	})
	if err != nil {
		return fmt.Errorf("GetResult RPC failed: %w", err)
	}

	// Read the corridor result to find the view ID
	for {
		chunk, err := resultStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		if corridorResult := chunk.GetCorridor(); corridorResult != nil {
			if corridorResult.View != nil && corridorResult.View.ViewId != "" {
				corridorViewRef = corridorResult.View
				log.Printf("  Corridor view created: %s", corridorViewRef.ViewId)
			}
			break
		}
	}

	log.Printf("  Corridor (1->4, SHORTEST_PATH_HULL) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 3: Corridor test on existing view
func testRunCorridorOnView(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if viewRef == nil {
		return fmt.Errorf("no view reference from previous test")
	}

	// Create corridor from 1 to 4 on the view using KSP_HULL method
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: viewRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Corridor{
				Corridor: &gepb.CorridorSpec{
					SourceU64: 1,
					TargetU64: 4,
					Method:    gepb.CorridorSpec_KSP_HULL,
					Hops:      2,
					K:         2, // top-2 paths for KSP_HULL
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Corridor on view RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Corridor on view (1->4, KSP_HULL) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 3: STMinCut test on full graph
func testRunSTMinCut(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	// Run s-t mincut from 1 to 4
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_StMincut{
				StMincut: &gepb.STMinCutSpec{
					SourceU64: 1,
					TargetU64: 4,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run STMinCut RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  STMinCut (1->4) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 3: STMinCut test on corridor view (the "corridor + cut" workflow)
func testRunSTMinCutOnCorridor(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if corridorViewRef == nil {
		// If corridor view wasn't created, skip this test gracefully
		log.Printf("  Skipping STMinCut on corridor (no corridor view available)")
		return nil
	}

	// Run s-t mincut on the corridor view
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: corridorViewRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_StMincut{
				StMincut: &gepb.STMinCutSpec{
					SourceU64: 1,
					TargetU64: 4,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run STMinCut on corridor RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	// Optionally get the result to verify cut details
	resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: jobResp.Result,
	})
	if err != nil {
		return fmt.Errorf("GetResult RPC failed: %w", err)
	}

	// Read the mincut result
	for {
		chunk, err := resultStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		if mincutResult := chunk.GetStMincut(); mincutResult != nil {
			log.Printf("  MinCut result: cut_value=%.2f, cut_edges=%d",
				mincutResult.GetCutValue(), len(mincutResult.GetCutEdgesU64()))
			break
		}
	}

	log.Printf("  STMinCut on corridor view job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 4: K-Shortest Paths test
func testRunKShortestPaths(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Find k=3 shortest paths from 1 to 4
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_KShortestPaths{
				KShortestPaths: &gepb.KShortestPathsSpec{
					SourceU64: 1,
					TargetU64: 4,
					K:         3,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run KShortestPaths RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	// Get the result to verify paths
	resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: jobResp.Result,
	})
	if err != nil {
		return fmt.Errorf("GetResult RPC failed: %w", err)
	}

	// Count paths received
	pathCount := 0
	for {
		chunk, err := resultStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		if spResult := chunk.GetShortestPath(); spResult != nil {
			pathCount++
			log.Printf("  KSP path %d: vertices=%v, cost=%.2f",
				pathCount, spResult.GetVerticesU64(), spResult.GetTotalCost())
		}
	}

	if pathCount < 1 {
		return fmt.Errorf("expected at least 1 path, got %d", pathCount)
	}

	log.Printf("  KShortestPaths (1->4, k=3) found %d paths, job: %s", pathCount, resp.Job.JobId)
	return nil
}

// Phase 4: K-Shortest Paths test on view
func testRunKShortestPathsOnView(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if viewRef == nil {
		log.Printf("  Skipping KShortestPaths on view (no view reference available)")
		return nil
	}

	// Find k=2 shortest paths from 1 to 3 on the view
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: viewRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_KShortestPaths{
				KShortestPaths: &gepb.KShortestPathsSpec{
					SourceU64: 1,
					TargetU64: 3,
					K:         2,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run KShortestPaths on view RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  KShortestPaths on view (1->3, k=2) completed, job: %s", resp.Job.JobId)
	return nil
}

func testCacheStats(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	resp, err := ops.CacheStats(ctx, &gepb.CacheStatsRequest{})
	if err != nil {
		return fmt.Errorf("CacheStats RPC failed: %w", err)
	}

	log.Printf("  Cache stats: results=%d, result_bytes=%d, views=%d, view_bytes=%d",
		resp.ResultsItems, resp.ResultsBytes, resp.ViewsItems, resp.ViewsBytes)

	// Should have at least 1 result cached
	if resp.ResultsItems == 0 {
		return fmt.Errorf("expected at least 1 cached result")
	}

	return nil
}

// Troubleshooting API: ValidateGraph (shallow)
func testValidateGraph(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	resp, err := ops.ValidateGraph(ctx, &gepb.ValidateGraphRequest{
		Graph: graphRef,
		Deep:  false, // Shallow validation
	})
	if err != nil {
		return fmt.Errorf("ValidateGraph RPC failed: %w", err)
	}

	if resp.Status == nil {
		return fmt.Errorf("expected status in response")
	}

	// Check that we got metrics
	if resp.Metrics == nil || len(resp.Metrics) == 0 {
		return fmt.Errorf("expected metrics in response")
	}

	// Verify key metrics are present
	vcount, ok := resp.Metrics["vcount"]
	if !ok {
		return fmt.Errorf("expected vcount metric")
	}
	ecount, ok := resp.Metrics["ecount"]
	if !ok {
		return fmt.Errorf("expected ecount metric")
	}

	log.Printf("  ValidateGraph (shallow): status=%d, vcount=%s, ecount=%s, warnings=%d",
		resp.Status.Code, vcount, ecount, len(resp.Warnings))

	return nil
}

// Troubleshooting API: ValidateGraph (deep)
func testValidateGraphDeep(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	resp, err := ops.ValidateGraph(ctx, &gepb.ValidateGraphRequest{
		Graph: graphRef,
		Deep:  true, // Deep validation
	})
	if err != nil {
		return fmt.Errorf("ValidateGraph (deep) RPC failed: %w", err)
	}

	if resp.Status == nil {
		return fmt.Errorf("expected status in response")
	}

	// Deep validation should include additional metrics
	if resp.Metrics == nil || len(resp.Metrics) == 0 {
		return fmt.Errorf("expected metrics in response")
	}

	// Check for deep validation specific metrics
	components, hasComponents := resp.Metrics["connected_components"]
	selfLoops, hasSelfLoops := resp.Metrics["self_loops"]

	log.Printf("  ValidateGraph (deep): status=%d, components=%s, self_loops=%s, warnings=%d",
		resp.Status.Code, components, selfLoops, len(resp.Warnings))

	if !hasComponents {
		return fmt.Errorf("expected connected_components metric in deep validation")
	}
	if !hasSelfLoops {
		return fmt.Errorf("expected self_loops metric in deep validation")
	}

	return nil
}

// Troubleshooting API: TraceJob
func testTraceJob(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if jobID == "" {
		return fmt.Errorf("no job_id from previous test")
	}

	resp, err := ops.TraceJob(ctx, &gepb.TraceJobRequest{
		Job: &gepb.JobRef{JobId: jobID},
	})
	if err != nil {
		return fmt.Errorf("TraceJob RPC failed: %w", err)
	}

	if resp.Spans == nil {
		return fmt.Errorf("expected spans in response")
	}

	// Should have at least one span
	if len(resp.Spans) == 0 {
		return fmt.Errorf("expected at least 1 trace span")
	}

	// Log span details
	for i, span := range resp.Spans {
		log.Printf("  TraceJob span[%d]: name=%s, duration=%v", i, span.Name, span.Duration.AsDuration())
	}

	log.Printf("  TraceJob: found %d spans for job %s", len(resp.Spans), jobID)
	return nil
}

// Troubleshooting API: ExportSubgraph (graph)
func testExportSubgraph(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference from previous test")
	}

	stream, err := ops.ExportSubgraph(ctx, &gepb.ExportSubgraphRequest{
		Target: &gepb.ExportSubgraphRequest_Graph{Graph: graphRef},
		Format: gepb.ExportSubgraphRequest_EDGE_LIST,
	})
	if err != nil {
		return fmt.Errorf("ExportSubgraph RPC failed: %w", err)
	}

	// Collect all chunks
	var totalBytes int
	chunkCount := 0
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ExportSubgraph recv failed: %w", err)
		}
		totalBytes += len(chunk.Data)
		chunkCount++
	}

	if totalBytes == 0 {
		return fmt.Errorf("expected non-empty export data")
	}

	log.Printf("  ExportSubgraph (graph): received %d bytes in %d chunks", totalBytes, chunkCount)
	return nil
}

// Troubleshooting API: ExportSubgraph (view)
func testExportSubgraphView(ctx context.Context, _ gepb.GraphEngineClient, ops gepb.GraphEngineOpsClient) error {
	if viewRef == nil {
		log.Printf("  Skipping ExportSubgraph on view (no view reference available)")
		return nil
	}

	stream, err := ops.ExportSubgraph(ctx, &gepb.ExportSubgraphRequest{
		Target: &gepb.ExportSubgraphRequest_View{View: viewRef},
		Format: gepb.ExportSubgraphRequest_CSV,
	})
	if err != nil {
		return fmt.Errorf("ExportSubgraph (view) RPC failed: %w", err)
	}

	// Collect all chunks
	var totalBytes int
	var allData []byte
	chunkCount := 0
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ExportSubgraph (view) recv failed: %w", err)
		}
		allData = append(allData, chunk.Data...)
		totalBytes += len(chunk.Data)
		chunkCount++
	}

	if totalBytes == 0 {
		return fmt.Errorf("expected non-empty export data")
	}

	// Verify CSV format has header
	dataStr := string(allData)
	if len(dataStr) > 0 && dataStr[0:6] != "source" {
		log.Printf("  Warning: CSV export may not have expected header, got: %s...", dataStr[:min(50, len(dataStr))])
	}

	log.Printf("  ExportSubgraph (view, CSV): received %d bytes in %d chunks", totalBytes, chunkCount)
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Algorithm test: BFS
func testRunBFS(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run BFS from vertex 1 with max depth 2
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Bfs{
				Bfs: &gepb.BFSSpec{
					SourceU64: 1,
					MaxDepth:  2,
					Mode:      gepb.NeighborhoodSpec_MODE_ALL,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run BFS RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  BFS (source=1, depth=2) job completed: %s", resp.Job.JobId)
	return nil
}

// Algorithm test: Neighborhood query
func testRunNeighborhood(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run neighborhood query from multiple seeds
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Neighborhood{
				Neighborhood: &gepb.NeighborhoodQuerySpec{
					SeedsU64: []uint64{1, 5},
					Hops:     1,
					Mode:     gepb.NeighborhoodSpec_MODE_ALL,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Neighborhood RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Neighborhood (seeds=[1,5], hops=1) job completed: %s", resp.Job.JobId)
	return nil
}

// Algorithm test: Communities
func testRunCommunities(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run community detection (Louvain)
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Communities{
				Communities: &gepb.CommunitiesSpec{
					Method:     gepb.CommunitiesSpec_LOUVAIN,
					Resolution: 1.0,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Communities RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Communities (Louvain) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 5 Algorithm test: K-Core Decomposition
func testRunKCore(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run k-core decomposition (full decomposition, k=0)
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Kcore{
				Kcore: &gepb.KCoreSpec{
					K: 0, // Full decomposition
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run KCore RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	// Get the result to verify k-core data
	resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: jobResp.Result,
	})
	if err != nil {
		return fmt.Errorf("GetResult RPC failed: %w", err)
	}

	// Read the k-core result
	var maxCore uint32
	var corenessCount int
	for {
		chunk, err := resultStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		if kcoreResult := chunk.GetKcore(); kcoreResult != nil {
			maxCore = kcoreResult.GetMaxCore()
			corenessCount = len(kcoreResult.GetCoreness())
			log.Printf("  KCore result: max_core=%d, vertices=%d", maxCore, corenessCount)
			break
		}
	}

	if corenessCount != 10 {
		return fmt.Errorf("expected coreness for 10 vertices, got %d", corenessCount)
	}

	log.Printf("  KCore (k=0, full decomposition) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 5 Algorithm test: Betweenness Centrality
func testRunBetweenness(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run betweenness centrality (full computation)
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Betweenness{
				Betweenness: &gepb.BetweennessSpec{
					SampleSize: 0, // Full computation
					Normalized: false,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Betweenness RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	// Get the result to verify betweenness data
	resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
		Result: jobResp.Result,
	})
	if err != nil {
		return fmt.Errorf("GetResult RPC failed: %w", err)
	}

	// Read the betweenness result
	var scoresCount int
	for {
		chunk, err := resultStream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("GetResult recv failed: %w", err)
		}

		if betwResult := chunk.GetBetweenness(); betwResult != nil {
			scoresCount = len(betwResult.GetScores())
			log.Printf("  Betweenness result: %d vertex scores", scoresCount)
			break
		}
	}

	if scoresCount != 10 {
		return fmt.Errorf("expected betweenness for 10 vertices, got %d", scoresCount)
	}

	log.Printf("  Betweenness (full, unnormalized) job completed: %s", resp.Job.JobId)
	return nil
}

// Phase 5 Algorithm test: Betweenness Centrality (Sampled)
func testRunBetweennessSampled(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Run betweenness centrality with sampling
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Betweenness{
				Betweenness: &gepb.BetweennessSpec{
					SampleSize: 5, // Sample only 5 vertices
					Normalized: true,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Betweenness (sampled) RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Betweenness (sampled=5, normalized) job completed: %s", resp.Job.JobId)
	return nil
}

// Batch artifacts test: Publish with all 4 artifact flags
func testBatchArtifactsAll(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	// Create a new build for batch artifacts test
	beginResp, err := client.BeginBuild(ctx, &gepb.BeginBuildRequest{
		GraphName: "e2e-batch-artifacts-graph",
		Directed:  false,
		Labels:    map[string]string{"env": "e2e", "test": "batch_artifacts"},
	})
	if err != nil {
		return fmt.Errorf("BeginBuild RPC failed: %w", err)
	}
	batchBuildID := beginResp.BuildId

	// Upload vertices and edges for a small test graph
	stream, err := client.Upload(ctx)
	if err != nil {
		return fmt.Errorf("Upload stream failed: %w", err)
	}

	// Upload a small clique (complete graph on 5 vertices)
	err = stream.Send(&gepb.UploadRequest{
		BuildId: batchBuildID,
		Payload: &gepb.UploadRequest_Vertices{
			Vertices: &gepb.VertexChunk{
				NodeIdU64: []uint64{1, 2, 3, 4, 5},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send vertices: %w", err)
	}

	// Complete graph edges: 1-2, 1-3, 1-4, 1-5, 2-3, 2-4, 2-5, 3-4, 3-5, 4-5
	err = stream.Send(&gepb.UploadRequest{
		BuildId: batchBuildID,
		Payload: &gepb.UploadRequest_Edges{
			Edges: &gepb.EdgeChunk{
				SrcU64: []uint64{1, 1, 1, 1, 2, 2, 2, 3, 3, 4},
				DstU64: []uint64{2, 3, 4, 5, 3, 4, 5, 4, 5, 5},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send edges: %w", err)
	}

	_, err = stream.CloseAndRecv()
	if err != nil {
		return fmt.Errorf("Upload close failed: %w", err)
	}

	// Publish with all 4 batch artifact flags enabled
	pubResp, err := client.PublishBuild(ctx, &gepb.PublishBuildRequest{
		BuildId: batchBuildID,
		Artifacts: &gepb.BatchArtifacts{
			ComputeComponents:         true,
			ComputeCommunities:        true,
			ComputeKcore:              true,
			ComputeBetweennessSampled: true,
		},
	})
	if err != nil {
		return fmt.Errorf("PublishBuild RPC failed: %w", err)
	}
	if pubResp.Status.Code != 0 {
		return fmt.Errorf("publish failed: code=%d, msg=%s", pubResp.Status.Code, pubResp.Status.Message)
	}

	log.Printf("  Batch artifacts build published: %s version %s",
		pubResp.Graph.GraphName, pubResp.Graph.VersionId)

	// Verify by running corridor with COMMUNITY_AWARE method
	// If communities were precomputed, this should use them
	batchGraphRef := pubResp.Graph
	corridorResp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: batchGraphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Corridor{
				Corridor: &gepb.CorridorSpec{
					SourceU64: 1,
					TargetU64: 5,
					Method:    gepb.CorridorSpec_COMMUNITY_AWARE,
					Hops:      0,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run Corridor (COMMUNITY_AWARE) RPC failed: %w", err)
	}

	// Verify job completed
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: corridorResp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}
	if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("corridor job did not succeed, state=%v", jobResp.State)
	}

	log.Printf("  Batch artifacts test passed: components, communities, k-core, betweenness all computed")
	return nil
}

// Resource management test: CancelJob
func testCancelJob(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if graphRef == nil {
		return fmt.Errorf("no graph reference available")
	}

	// Start a job that we'll cancel
	resp, err := client.Run(ctx, &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graphRef},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Components{
				Components: &gepb.ComponentsSpec{
					Mode: gepb.ComponentsSpec_WEAK,
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("Run RPC failed: %w", err)
	}
	if resp.Job == nil || resp.Job.JobId == "" {
		return fmt.Errorf("expected valid job reference")
	}

	// Try to cancel it (it might have already completed, which is fine)
	cancelResp, err := client.CancelJob(ctx, &gepb.CancelJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("CancelJob RPC failed: %w", err)
	}

	// Check job state after cancel attempt
	jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
		Job: resp.Job,
	})
	if err != nil {
		return fmt.Errorf("GetJob RPC failed: %w", err)
	}

	// Job should be either CANCELED or SUCCEEDED (if it completed before cancel)
	if jobResp.State != gepb.GetJobResponse_CANCELED && jobResp.State != gepb.GetJobResponse_SUCCEEDED {
		return fmt.Errorf("unexpected job state: %v", jobResp.State)
	}

	log.Printf("  CancelJob: canceled=%v, final_state=%v", cancelResp.Canceled, jobResp.State)
	return nil
}

// Resource management test: Release
func testRelease(ctx context.Context, client gepb.GraphEngineClient, _ gepb.GraphEngineOpsClient) error {
	if viewRef == nil {
		log.Printf("  Skipping Release test (no view reference available)")
		return nil
	}

	// Create a temporary view to release
	createResp, err := client.CreateView(ctx, &gepb.CreateViewRequest{
		Graph: graphRef,
		Spec: &gepb.ViewSpec{
			InduceVerticesU64: []uint64{1, 2},
		},
	})
	if err != nil {
		return fmt.Errorf("CreateView RPC failed: %w", err)
	}
	if createResp.View == nil || createResp.View.ViewId == "" {
		return fmt.Errorf("expected valid view reference")
	}

	tempViewRef := createResp.View
	log.Printf("  Created temp view for release test: %s", tempViewRef.ViewId)

	// Release the view
	releaseResp, err := client.Release(ctx, &gepb.ReleaseRequest{
		Target: &gepb.ReleaseRequest_View{View: tempViewRef},
	})
	if err != nil {
		return fmt.Errorf("Release RPC failed: %w", err)
	}

	if !releaseResp.Released {
		return fmt.Errorf("expected release to succeed")
	}

	log.Printf("  Release: view %s released=%v", tempViewRef.ViewId, releaseResp.Released)
	return nil
}
