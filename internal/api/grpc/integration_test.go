package grpc_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpcserver "github.com/naisa-ai/graph-engine/internal/api/grpc"
	"github.com/naisa-ai/graph-engine/internal/config"
	"github.com/naisa-ai/graph-engine/internal/service"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// TestIntegrationFullFlow tests the complete workflow:
// BeginBuild -> Upload -> PublishBuild -> Run -> GetJob -> GetResult -> ListGraphs -> DescribeGraph
func TestIntegrationFullFlow(t *testing.T) {
	// Setup
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	cfg := &config.Config{
		Server: config.ServerConfig{
			GRPCPort: 0, // Use any available port
		},
		Limits: config.LimitsConfig{
			MaxGraphs:        10,
			MaxBuildsPending: 5,
		},
	}

	buildStore := service.NewBuildStore(cfg.Limits.MaxBuildsPending)

	server, err := grpcserver.NewServer(cfg, logger, buildStore)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// Start server on random port
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	addr := lis.Addr().String()

	go func() {
		if err := server.ServeListener(lis); err != nil {
			// Server stopped
		}
	}()
	defer server.Stop()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Create client
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()

	client := gepb.NewGraphEngineClient(conn)
	opsClient := gepb.NewGraphEngineOpsClient(conn)

	// Test 1: Health check
	t.Run("Health", func(t *testing.T) {
		resp, err := opsClient.Health(ctx, &gepb.HealthRequest{})
		if err != nil {
			t.Fatalf("Health failed: %v", err)
		}
		if resp.Status != "SERVING" {
			t.Errorf("expected SERVING, got %s", resp.Status)
		}
	})

	// Test 2: BeginBuild
	var buildID string
	t.Run("BeginBuild", func(t *testing.T) {
		resp, err := client.BeginBuild(ctx, &gepb.BeginBuildRequest{
			GraphName: "test-graph",
			Directed:  false,
			Labels:    map[string]string{"env": "test"},
		})
		if err != nil {
			t.Fatalf("BeginBuild failed: %v", err)
		}
		if resp.BuildId == "" {
			t.Error("expected non-empty build_id")
		}
		buildID = resp.BuildId
		t.Logf("Build ID: %s", buildID)
	})

	// Test 3: Upload
	t.Run("Upload", func(t *testing.T) {
		stream, err := client.Upload(ctx)
		if err != nil {
			t.Fatalf("Upload failed to start: %v", err)
		}

		// Send vertices
		err = stream.Send(&gepb.UploadRequest{
			BuildId: buildID,
			Payload: &gepb.UploadRequest_Vertices{
				Vertices: &gepb.VertexChunk{
					NodeIdU64: []uint64{1, 2, 3, 4, 5, 6},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to send vertices: %v", err)
		}

		// Send edges (creates two components: {1,2,3} and {4,5,6})
		err = stream.Send(&gepb.UploadRequest{
			BuildId: buildID,
			Payload: &gepb.UploadRequest_Edges{
				Edges: &gepb.EdgeChunk{
					SrcU64: []uint64{1, 2, 4, 5},
					DstU64: []uint64{2, 3, 5, 6},
				},
			},
		})
		if err != nil {
			t.Fatalf("failed to send edges: %v", err)
		}

		resp, err := stream.CloseAndRecv()
		if err != nil {
			t.Fatalf("Upload close failed: %v", err)
		}

		if resp.ReceivedVertices != 6 {
			t.Errorf("expected 6 vertices, got %d", resp.ReceivedVertices)
		}
		if resp.ReceivedEdges != 4 {
			t.Errorf("expected 4 edges, got %d", resp.ReceivedEdges)
		}
		t.Logf("Uploaded %d vertices, %d edges", resp.ReceivedVertices, resp.ReceivedEdges)
	})

	// Test 4: PublishBuild
	var graphRef *gepb.GraphRef
	t.Run("PublishBuild", func(t *testing.T) {
		resp, err := client.PublishBuild(ctx, &gepb.PublishBuildRequest{
			BuildId: buildID,
			Artifacts: &gepb.BatchArtifacts{
				ComputeComponents: true,
			},
		})
		if err != nil {
			t.Fatalf("PublishBuild failed: %v", err)
		}
		if resp.Status.Code != 0 {
			t.Errorf("expected success, got code %d: %s", resp.Status.Code, resp.Status.Message)
		}
		if resp.Graph.GraphName != "test-graph" {
			t.Errorf("expected test-graph, got %s", resp.Graph.GraphName)
		}
		graphRef = resp.Graph
		t.Logf("Published: %s version %s", graphRef.GraphName, graphRef.VersionId)
	})

	// Test 5: ListGraphs
	t.Run("ListGraphs", func(t *testing.T) {
		resp, err := opsClient.ListGraphs(ctx, &gepb.ListGraphsRequest{})
		if err != nil {
			t.Fatalf("ListGraphs failed: %v", err)
		}
		if len(resp.Graphs) != 1 {
			t.Errorf("expected 1 graph, got %d", len(resp.Graphs))
		}
		if resp.Graphs[0].GraphName != "test-graph" {
			t.Errorf("expected test-graph, got %s", resp.Graphs[0].GraphName)
		}
		t.Logf("Listed %d graphs", len(resp.Graphs))
	})

	// Test 6: DescribeGraph
	t.Run("DescribeGraph", func(t *testing.T) {
		resp, err := opsClient.DescribeGraph(ctx, &gepb.DescribeGraphRequest{
			Graph: graphRef,
		})
		if err != nil {
			t.Fatalf("DescribeGraph failed: %v", err)
		}
		if resp.Summary.Vcount != 6 {
			t.Errorf("expected 6 vertices, got %d", resp.Summary.Vcount)
		}
		if resp.Summary.Ecount != 4 {
			t.Errorf("expected 4 edges, got %d", resp.Summary.Ecount)
		}
		t.Logf("Described: %d vertices, %d edges", resp.Summary.Vcount, resp.Summary.Ecount)
	})

	// Test 7: Run components
	var jobID string
	t.Run("RunComponents", func(t *testing.T) {
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
			t.Fatalf("Run failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}
		jobID = resp.Job.JobId
		t.Logf("Job ID: %s", jobID)
	})

	// Test 8: GetJob
	var resultID string
	t.Run("GetJob", func(t *testing.T) {
		resp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: &gepb.JobRef{JobId: jobID},
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if resp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", resp.State)
		}
		if resp.Result.ResultId == "" {
			t.Error("expected non-empty result_id")
		}
		resultID = resp.Result.ResultId
		t.Logf("Result ID: %s", resultID)
	})

	// Test 9: GetResult
	t.Run("GetResult", func(t *testing.T) {
		stream, err := client.GetResult(ctx, &gepb.GetResultRequest{
			Result: &gepb.ResultRef{ResultId: resultID},
		})
		if err != nil {
			t.Fatalf("GetResult failed to start: %v", err)
		}

		var membership []uint32
		var gotHeader, gotDone bool

		for {
			chunk, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("GetResult recv failed: %v", err)
			}

			switch payload := chunk.Payload.(type) {
			case *gepb.ResultChunk_Header:
				gotHeader = true
				t.Logf("Header: type=%s", payload.Header.Type)
			case *gepb.ResultChunk_U32:
				if payload.U32.Name == "membership" {
					membership = payload.U32.Values
					t.Logf("Membership: %v", membership)
				}
			case *gepb.ResultChunk_Done:
				gotDone = true
			}
		}

		if !gotHeader {
			t.Error("expected header chunk")
		}
		if !gotDone {
			t.Error("expected done chunk")
		}
		if len(membership) != 6 {
			t.Errorf("expected 6 membership values, got %d", len(membership))
		}

		// Verify we have exactly 2 components
		components := make(map[uint32]int)
		for _, c := range membership {
			components[c]++
		}
		if len(components) != 2 {
			t.Errorf("expected 2 components, got %d", len(components))
		}
		t.Logf("Found %d components", len(components))
	})

	// Test 10: CreateView
	var viewRef *gepb.ViewRef
	t.Run("CreateView", func(t *testing.T) {
		resp, err := client.CreateView(ctx, &gepb.CreateViewRequest{
			Graph: graphRef,
			Spec: &gepb.ViewSpec{
				InduceVerticesU64: []uint64{1, 2, 3}, // First component only
			},
		})
		if err != nil {
			t.Fatalf("CreateView failed: %v", err)
		}
		if resp.View.ViewId == "" {
			t.Error("expected non-empty view_id")
		}
		if resp.Vcount != 3 {
			t.Errorf("expected 3 vertices in view, got %d", resp.Vcount)
		}
		// Edges: 1->2, 2->3 = 2 edges
		if resp.Ecount != 2 {
			t.Errorf("expected 2 edges in view, got %d", resp.Ecount)
		}
		viewRef = resp.View
		t.Logf("View ID: %s (v=%d, e=%d)", viewRef.ViewId, resp.Vcount, resp.Ecount)
	})

	// Test 11: Run ShortestPath on full graph
	t.Run("RunShortestPath", func(t *testing.T) {
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_ShortestPath{
					ShortestPath: &gepb.ShortestPathSpec{
						SourceU64:      1,
						TargetU64:      3,
						ReturnVertices: true,
						ReturnEdges:    true,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run ShortestPath failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}
		t.Logf("ShortestPath Job ID: %s", resp.Job.JobId)

		// Get result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		t.Logf("ShortestPath result: %s", jobResp.Result.ResultId)
	})

	// Test 12: Run ShortestPath with no path (across components)
	t.Run("RunShortestPath_NoPath", func(t *testing.T) {
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_ShortestPath{
					ShortestPath: &gepb.ShortestPathSpec{
						SourceU64:      1, // Component 1
						TargetU64:      4, // Component 2
						ReturnVertices: true,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run ShortestPath failed: %v", err)
		}

		// Get result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		// Result should indicate no path found (infinite cost)
		t.Logf("ShortestPath (no path) result: %s", jobResp.Result.ResultId)
	})

	// Test 13: Run ShortestPath on View
	t.Run("RunShortestPath_OnView", func(t *testing.T) {
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_View{View: viewRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_ShortestPath{
					ShortestPath: &gepb.ShortestPathSpec{
						SourceU64:      1,
						TargetU64:      3,
						ReturnVertices: true,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run ShortestPath on view failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}
		t.Logf("ShortestPath on View Job ID: %s", resp.Job.JobId)
	})

	// Test 14: Run Distances
	t.Run("RunDistances", func(t *testing.T) {
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_Distances{
					Distances: &gepb.DistancesSpec{
						SourcesU64: []uint64{1, 2},
						TargetsU64: []uint64{1, 2, 3},
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run Distances failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}

		// Get result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		t.Logf("Distances result: %s", jobResp.Result.ResultId)
	})

	// Test 15: Run Corridor
	t.Run("RunCorridor", func(t *testing.T) {
		// Graph has two components: 1-2-3 and 4-5-6
		// Use endpoints within same component
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_Corridor{
					Corridor: &gepb.CorridorSpec{
						SourceU64: 1,
						TargetU64: 3,
						Method:    gepb.CorridorSpec_SHORTEST_PATH_HULL,
						Hops:      1,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run Corridor failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}

		// Get result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		t.Logf("Corridor result: %s", jobResp.Result.ResultId)
	})

	// Test 16: Run STMinCut
	t.Run("RunSTMinCut", func(t *testing.T) {
		// Use endpoints within same component (1-2-3)
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_StMincut{
					StMincut: &gepb.STMinCutSpec{
						SourceU64: 1,
						TargetU64: 3,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run STMinCut failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}

		// Get result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		t.Logf("STMinCut result: %s", jobResp.Result.ResultId)
	})

	// Test 17: Run STMinCut on View (corridor-like workflow)
	t.Run("RunSTMinCut_OnView", func(t *testing.T) {
		// First create a corridor view
		corridorResp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_Corridor{
					Corridor: &gepb.CorridorSpec{
						SourceU64: 1,
						TargetU64: 3,
						Method:    gepb.CorridorSpec_SHORTEST_PATH_HULL,
						Hops:      1,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Create corridor failed: %v", err)
		}

		// Get corridor result to find view ID
		corridorJobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: corridorResp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob for corridor failed: %v", err)
		}
		if corridorJobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Fatalf("corridor job not succeeded: %v", corridorJobResp.State)
		}

		// Get the result to extract view ID from metadata
		resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
			Result: corridorJobResp.Result,
		})
		if err != nil {
			t.Fatalf("GetResult for corridor failed: %v", err)
		}

		// Read the corridor result to find the view ID
		var corridorViewID string
		for {
			chunk, err := resultStream.Recv()
			if err != nil {
				break
			}
			if corridorResult := chunk.GetCorridor(); corridorResult != nil {
				if corridorResult.View != nil {
					corridorViewID = corridorResult.View.ViewId
				}
				break
			}
		}

		if corridorViewID == "" {
			t.Log("Corridor view ID not found in result, skipping mincut on view")
			return
		}

		// Now run mincut on the corridor view
		mincutResp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_View{
				View: &gepb.ViewRef{ViewId: corridorViewID},
			},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_StMincut{
					StMincut: &gepb.STMinCutSpec{
						SourceU64: 1,
						TargetU64: 3,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run STMinCut on view failed: %v", err)
		}

		mincutJobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: mincutResp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob for mincut failed: %v", err)
		}
		if mincutJobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("mincut job not succeeded: %v", mincutJobResp.State)
		}
		t.Logf("STMinCut on corridor view: %s", mincutJobResp.Result.ResultId)
	})

	// Test 18: Run KShortestPaths
	t.Run("RunKShortestPaths", func(t *testing.T) {
		// Graph has edges: 1-2, 2-3, 4-5, 5-6
		// Find k=3 shortest paths from 1 to 3
		resp, err := client.Run(ctx, &gepb.RunRequest{
			Target: &gepb.RunRequest_Graph{Graph: graphRef},
			Algo: &gepb.AlgoSpec{
				Kind: &gepb.AlgoSpec_KShortestPaths{
					KShortestPaths: &gepb.KShortestPathsSpec{
						SourceU64: 1,
						TargetU64: 3,
						K:         3,
					},
				},
			},
		})
		if err != nil {
			t.Fatalf("Run KShortestPaths failed: %v", err)
		}
		if resp.Job.JobId == "" {
			t.Error("expected non-empty job_id")
		}

		// Get job result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}

		// Get the result and verify we got paths
		resultStream, err := client.GetResult(ctx, &gepb.GetResultRequest{
			Result: jobResp.Result,
		})
		if err != nil {
			t.Fatalf("GetResult failed: %v", err)
		}

		pathCount := 0
		for {
			chunk, err := resultStream.Recv()
			if err != nil {
				break
			}
			if spResult := chunk.GetShortestPath(); spResult != nil {
				pathCount++
				t.Logf("KSP Path %d: vertices=%v, cost=%.2f", pathCount, spResult.VerticesU64, spResult.TotalCost)
			}
		}

		// Should find at least 1 path (1->2->3)
		if pathCount < 1 {
			t.Errorf("expected at least 1 path, got %d", pathCount)
		}
		t.Logf("KShortestPaths found %d paths", pathCount)
	})

	// Test 19: Run KShortestPaths on view
	t.Run("RunKShortestPaths_OnView", func(t *testing.T) {
		if viewRef == nil {
			t.Skip("no view reference available")
		}

		// Find k=2 shortest paths from 1 to 3 on the view
		// (using connected nodes within the view's first component)
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
			t.Fatalf("Run KShortestPaths on view failed: %v", err)
		}

		// Get job result
		jobResp, err := client.GetJob(ctx, &gepb.GetJobRequest{
			Job: resp.Job,
		})
		if err != nil {
			t.Fatalf("GetJob failed: %v", err)
		}
		if jobResp.State != gepb.GetJobResponse_SUCCEEDED {
			t.Errorf("expected SUCCEEDED, got %v", jobResp.State)
		}
		t.Logf("KShortestPaths on view: %s", jobResp.Result.ResultId)
	})

	// Test 20: CacheStats (updated)
	t.Run("CacheStats", func(t *testing.T) {
		resp, err := opsClient.CacheStats(ctx, &gepb.CacheStatsRequest{})
		if err != nil {
			t.Fatalf("CacheStats failed: %v", err)
		}
		// Should have multiple results cached now (components + shortest paths + distances + corridor + mincut + ksp)
		if resp.ResultsItems < 7 {
			t.Errorf("expected at least 7 cached results, got %d", resp.ResultsItems)
		}
		t.Logf("Cache: %d results, %d bytes", resp.ResultsItems, resp.ResultsBytes)
	})

	t.Log("Integration test completed successfully!")
}
