package grpc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/naisa-ai/graph-engine/internal/metrics"
	"github.com/naisa-ai/graph-engine/internal/service"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// GraphEngineHandler implements the GraphEngine gRPC service.
type GraphEngineHandler struct {
	gepb.UnimplementedGraphEngineServer
	logger        *slog.Logger
	buildStore    *service.BuildStore
	graphRegistry *service.GraphRegistry
	versionStore  *service.VersionStore
	resultStore   *service.ResultStore
	viewManager   *service.ViewManager
}

// NewGraphEngineHandler creates a new GraphEngineHandler.
func NewGraphEngineHandler(
	logger *slog.Logger,
	buildStore *service.BuildStore,
	graphRegistry *service.GraphRegistry,
	versionStore *service.VersionStore,
	resultStore *service.ResultStore,
	viewManager *service.ViewManager,
) *GraphEngineHandler {
	return &GraphEngineHandler{
		logger:        logger,
		buildStore:    buildStore,
		graphRegistry: graphRegistry,
		versionStore:  versionStore,
		resultStore:   resultStore,
		viewManager:   viewManager,
	}
}

// BeginBuild starts a new graph build and returns a build ID.
func (h *GraphEngineHandler) BeginBuild(ctx context.Context, req *gepb.BeginBuildRequest) (*gepb.BeginBuildResponse, error) {
	h.logger.Info("BeginBuild called",
		"graph_name", req.GetGraphName(),
		"directed", req.GetDirected(),
	)

	buildID, err := h.buildStore.CreateBuild(
		req.GetGraphName(),
		req.GetDirected(),
		req.GetLabels(),
	)
	if err != nil {
		h.logger.Error("failed to create build", "error", err)
		return nil, status.Errorf(codes.ResourceExhausted, "failed to create build: %v", err)
	}

	h.logger.Info("build created", "build_id", buildID)
	return &gepb.BeginBuildResponse{
		BuildId: buildID,
	}, nil
}

// Upload streams vertices and edges into a build.
func (h *GraphEngineHandler) Upload(stream gepb.GraphEngine_UploadServer) error {
	var buildID string
	var totalVertices uint64
	var totalEdges uint64

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			h.logger.Info("upload stream completed",
				"build_id", buildID,
				"total_vertices", totalVertices,
				"total_edges", totalEdges,
			)
			return stream.SendAndClose(&gepb.UploadResponse{
				BuildId:          buildID,
				ReceivedVertices: totalVertices,
				ReceivedEdges:    totalEdges,
			})
		}
		if err != nil {
			h.logger.Error("upload stream error", "error", err)
			return status.Errorf(codes.Internal, "stream error: %v", err)
		}

		// Track build ID from first message
		if buildID == "" {
			buildID = req.GetBuildId()
		}

		// Verify build exists
		build := h.buildStore.GetBuild(buildID)
		if build == nil {
			return status.Errorf(codes.NotFound, "build not found: %s", buildID)
		}

		// Process payload
		switch payload := req.GetPayload().(type) {
		case *gepb.UploadRequest_Vertices:
			chunk := payload.Vertices
			if len(chunk.GetNodeIdU64()) > 0 {
				if err := h.buildStore.AddVerticesU64(buildID, chunk.GetNodeIdU64()); err != nil {
					return status.Errorf(codes.Internal, "failed to add vertices: %v", err)
				}
				totalVertices += uint64(len(chunk.GetNodeIdU64()))
			}
			if len(chunk.GetNodeIdStr()) > 0 {
				if err := h.buildStore.AddVerticesStr(buildID, chunk.GetNodeIdStr()); err != nil {
					return status.Errorf(codes.Internal, "failed to add vertices: %v", err)
				}
				totalVertices += uint64(len(chunk.GetNodeIdStr()))
			}

		case *gepb.UploadRequest_Edges:
			chunk := payload.Edges
			if len(chunk.GetSrcU64()) > 0 {
				if err := h.buildStore.AddEdgesU64(
					buildID,
					chunk.GetSrcU64(),
					chunk.GetDstU64(),
					chunk.GetKind(),
					chunk.GetWeight(),
				); err != nil {
					return status.Errorf(codes.Internal, "failed to add edges: %v", err)
				}
				totalEdges += uint64(len(chunk.GetSrcU64()))
			}
			if len(chunk.GetSrcStr()) > 0 {
				if err := h.buildStore.AddEdgesStr(
					buildID,
					chunk.GetSrcStr(),
					chunk.GetDstStr(),
					chunk.GetKind(),
					chunk.GetWeight(),
				); err != nil {
					return status.Errorf(codes.Internal, "failed to add edges: %v", err)
				}
				totalEdges += uint64(len(chunk.GetSrcStr()))
			}

		case *gepb.UploadRequest_VertexColumns:
			// TODO: Phase 1 - handle column data
			h.logger.Debug("vertex columns received (not implemented yet)")

		case *gepb.UploadRequest_EdgeColumns:
			// TODO: Phase 1 - handle column data
			h.logger.Debug("edge columns received (not implemented yet)")

		case *gepb.UploadRequest_Finalize:
			h.logger.Info("finalize marker received", "build_id", buildID)
		}
	}
}

// PublishBuild publishes a build as the current version.
func (h *GraphEngineHandler) PublishBuild(ctx context.Context, req *gepb.PublishBuildRequest) (*gepb.PublishBuildResponse, error) {
	h.logger.Info("PublishBuild called", "build_id", req.GetBuildId())

	// Get build
	build := h.buildStore.GetBuild(req.GetBuildId())
	if build == nil {
		return nil, status.Errorf(codes.NotFound, "build not found: %s", req.GetBuildId())
	}

	// Validate build
	validationResult := build.Validate(service.DefaultValidationLimits())
	if !validationResult.Valid {
		h.logger.Error("build validation failed",
			"build_id", req.GetBuildId(),
			"errors", len(validationResult.Errors),
		)
		// Return first error
		if len(validationResult.Errors) > 0 {
			return &gepb.PublishBuildResponse{
				Status: &gepb.Status{
					Code:    int32(codes.InvalidArgument),
					Message: validationResult.Errors[0].Message,
				},
			}, nil
		}
		return nil, status.Errorf(codes.InvalidArgument, "build validation failed")
	}

	// Log warnings if any
	for _, warning := range validationResult.Warnings {
		h.logger.Warn("build validation warning", "warning", warning)
	}

	// Create GraphVersion from build
	versionID := uuid.New().String()
	version, err := service.NewGraphVersion(versionID, build.GraphName, build.Directed, build)
	if err != nil {
		h.logger.Error("failed to create graph version", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create graph version: %v", err)
	}

	h.logger.Info("graph version created",
		"version_id", versionID,
		"graph_name", build.GraphName,
		"vcount", version.VCount,
		"ecount", version.ECount,
		"memory_bytes", version.EstimateMemory(),
	)

	// Run batch artifacts if requested
	artifacts := req.GetArtifacts()
	if artifacts != nil && artifacts.GetComputeComponents() {
		h.logger.Info("computing components artifact", "version_id", versionID)
		// Components will be computed when Run is called or as part of publish
		// For now, we'll compute it here synchronously
		result, err := service.ComputeComponents(version, true, nil) // weak components
		if err != nil {
			h.logger.Error("failed to compute components", "error", err)
			// Continue anyway - components are optional
		} else {
			// Store result
			if err := h.resultStore.Store(result); err != nil {
				h.logger.Warn("failed to store components result", "error", err)
			} else {
				h.logger.Info("components computed and stored",
					"result_id", result.ID,
					"num_components", countUnique(result.MembershipU32),
				)
			}
		}
	}

	// Publish to registry (atomic swap)
	if err := h.graphRegistry.Publish(build.GraphName, version); err != nil {
		h.logger.Error("failed to publish graph", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to publish graph: %v", err)
	}

	// Cleanup build from BuildStore
	h.buildStore.DeleteBuild(req.GetBuildId())

	h.logger.Info("graph published successfully",
		"graph_name", build.GraphName,
		"version_id", versionID,
	)

	return &gepb.PublishBuildResponse{
		Graph: &gepb.GraphRef{
			GraphName: build.GraphName,
			VersionId: versionID,
		},
		Status: &gepb.Status{
			Code:    0,
			Message: "published successfully",
		},
	}, nil
}

// countUnique counts unique values in a slice.
func countUnique(arr []uint32) int {
	seen := make(map[uint32]struct{})
	for _, v := range arr {
		seen[v] = struct{}{}
	}
	return len(seen)
}

// CreateView creates a filtered view of a graph.
func (h *GraphEngineHandler) CreateView(ctx context.Context, req *gepb.CreateViewRequest) (*gepb.CreateViewResponse, error) {
	h.logger.Info("CreateView called",
		"graph_name", req.GetGraph().GetGraphName(),
		"version_id", req.GetGraph().GetVersionId(),
	)

	// Validate request
	graphRef := req.GetGraph()
	if graphRef == nil {
		return nil, status.Errorf(codes.InvalidArgument, "graph reference is required")
	}

	// Validate view spec
	spec := req.GetSpec()
	if err := h.viewManager.ValidateViewSpec(spec); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid view spec: %v", err)
	}

	// Get the graph version
	version, err := h.graphRegistry.GetVersion(graphRef.GetGraphName(), graphRef.GetVersionId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "graph not found: %v", err)
	}
	defer version.Unpin()

	// Create the view
	view, err := h.viewManager.CreateView(version, spec)
	if err != nil {
		h.logger.Error("failed to create view", "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create view: %v", err)
	}
	// Note: view is pinned by CreateView, will be unpinned when client releases

	h.logger.Info("view created",
		"view_id", view.ID,
		"vcount", view.VCount,
		"ecount", view.ECount,
	)

	return &gepb.CreateViewResponse{
		View: &gepb.ViewRef{
			ViewId: view.ID,
		},
		Vcount: view.VCount,
		Ecount: view.ECount,
	}, nil
}

// Run executes an algorithm on a graph or view.
func (h *GraphEngineHandler) Run(ctx context.Context, req *gepb.RunRequest) (*gepb.RunResponse, error) {
	h.logger.Info("Run called")

	// Get target graph version and optional view
	var version *service.GraphVersion
	var view *service.View
	var err error

	switch target := req.GetTarget().(type) {
	case *gepb.RunRequest_Graph:
		graphRef := target.Graph
		version, err = h.graphRegistry.GetVersion(graphRef.GetGraphName(), graphRef.GetVersionId())
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "graph not found: %v", err)
		}
		defer version.Unpin()

	case *gepb.RunRequest_View:
		viewRef := target.View
		view, err = h.viewManager.GetView(viewRef.GetViewId())
		if err != nil {
			return nil, status.Errorf(codes.NotFound, "view not found: %v", err)
		}
		defer view.Unpin()
		// Get the underlying version
		version = view.GetVersion()

	default:
		return nil, status.Errorf(codes.InvalidArgument, "target must be specified")
	}

	// Dispatch to algorithm
	algo := req.GetAlgo()
	if algo == nil {
		return nil, status.Errorf(codes.InvalidArgument, "algo must be specified")
	}

	var result *service.AlgoResult

	switch spec := algo.GetKind().(type) {
	case *gepb.AlgoSpec_Components:
		weak := spec.Components.GetMode() == gepb.ComponentsSpec_WEAK
		h.logger.Info("running components algorithm",
			"version_id", version.ID,
			"weak", weak,
		)

		// Check cache first
		paramsHash := service.HashParams("components", weak)
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindComponents, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for components", "result_id", cached.ID)
				metrics.IncCacheHits("components")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("components")

		// Compute
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		result, err = service.ComputeComponents(version, weak, nil)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute components: %v", err)
		}
		metrics.RecordAlgoDuration("components", time.Since(computeStart).Seconds())
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)

	case *gepb.AlgoSpec_ShortestPath:
		spSpec := spec.ShortestPath
		h.logger.Info("running shortest path algorithm",
			"version_id", version.ID,
			"source", spSpec.GetSourceU64(),
			"target", spSpec.GetTargetU64(),
			"weighted", spSpec.GetWeightColumn() != "",
		)

		weighted := spSpec.GetWeightColumn() != ""
		paramsHash := service.HashParams("shortest_path", spSpec.GetSourceU64(), spSpec.GetTargetU64(), weighted)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindShortestPath, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for shortest_path", "result_id", cached.ID)
				metrics.IncCacheHits("shortest_path")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("shortest_path")

		// Compute
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		spResult, err := service.ComputeShortestPath(
			version,
			view, // nil if not using a view
			spSpec.GetSourceU64(),
			spSpec.GetTargetU64(),
			weighted,
			spSpec.GetReturnEdges(),
			spSpec.GetReturnVertices(),
			nil, // use default config
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute shortest path: %v", err)
		}
		metrics.RecordAlgoDuration("shortest_path", time.Since(computeStart).Seconds())

		result = service.NewAlgoResult(version.ID, service.AlgoKindShortestPath, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.PathVertices = spResult.PathVertices
		if spResult.PathEdges != nil {
			result.PathEdges = make([]uint64, len(spResult.PathEdges))
			for i, e := range spResult.PathEdges {
				result.PathEdges[i] = uint64(e)
			}
		}
		result.PathCost = spResult.TotalCost
		result.Meta["found"] = "false"
		if spResult.Found {
			result.Meta["found"] = "true"
		}

	case *gepb.AlgoSpec_KShortestPaths:
		kspSpec := spec.KShortestPaths
		h.logger.Info("running k_shortest_paths algorithm",
			"version_id", version.ID,
			"source", kspSpec.GetSourceU64(),
			"target", kspSpec.GetTargetU64(),
			"k", kspSpec.GetK(),
			"weighted", kspSpec.GetWeightColumn() != "",
		)

		// Validate request
		if err := service.ValidateKSPRequest(kspSpec.GetK(), kspSpec.GetMaxCandidates()); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid ksp request: %v", err)
		}

		// Build cache key with normalization
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		paramsHash := service.HashKSPParams(
			kspSpec.GetSourceU64(),
			kspSpec.GetTargetU64(),
			kspSpec.GetK(),
			kspSpec.GetWeightColumn(),
			viewHash,
		)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindKSP, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for k_shortest_paths", "result_id", cached.ID)
				metrics.IncCacheHits("k_shortest_paths")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("k_shortest_paths")

		// Build config
		kspConfig := service.DefaultKSPConfig(int(kspSpec.GetK()))
		kspConfig.Weighted = kspSpec.GetWeightColumn() != ""
		kspConfig.ReturnVertices = true
		kspConfig.ReturnEdges = true
		if kspSpec.GetMaxCandidates() > 0 {
			kspConfig.MaxCandidates = int(kspSpec.GetMaxCandidates())
		}
		if kspSpec.GetPerPathTimeout() != nil {
			kspConfig.PerPathTimeout = kspSpec.GetPerPathTimeout().AsDuration()
		}

		// Compute
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		kspResult, err := service.ComputeKShortestPaths(
			ctx,
			version,
			view,
			kspSpec.GetSourceU64(),
			kspSpec.GetTargetU64(),
			kspConfig,
			nil, // use default shim config
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute k_shortest_paths: %v", err)
		}
		metrics.RecordAlgoDuration("k_shortest_paths", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindKSP, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.KSPPaths = make([]*service.KSPPath, len(kspResult.Paths))
		for i, p := range kspResult.Paths {
			kspPath := &service.KSPPath{
				Vertices: p.PathVertices,
				Cost:     p.TotalCost,
			}
			if p.PathEdges != nil {
				kspPath.Edges = make([]uint64, len(p.PathEdges))
				for j, e := range p.PathEdges {
					kspPath.Edges[j] = uint64(e)
				}
			}
			result.KSPPaths[i] = kspPath
		}
		result.Meta["k"] = fmt.Sprintf("%d", kspSpec.GetK())
		result.Meta["paths_found"] = fmt.Sprintf("%d", kspResult.PathsFound)
		result.Meta["candidates_explored"] = fmt.Sprintf("%d", kspResult.CandidatesExplored)
		if kspResult.Meta != nil {
			for k, v := range kspResult.Meta {
				result.Meta[k] = v
			}
		}

	case *gepb.AlgoSpec_Distances:
		distSpec := spec.Distances
		h.logger.Info("running distances algorithm",
			"version_id", version.ID,
			"num_sources", len(distSpec.GetSourcesU64()),
			"num_targets", len(distSpec.GetTargetsU64()),
			"weighted", distSpec.GetWeightColumn() != "",
		)

		weighted := distSpec.GetWeightColumn() != ""
		paramsHash := service.HashParams("distances", distSpec.GetSourcesU64(), distSpec.GetTargetsU64(), weighted)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindDistances, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for distances", "result_id", cached.ID)
				metrics.IncCacheHits("distances")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("distances")

		// Compute
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		distResult, err := service.ComputeDistances(
			version,
			view,
			distSpec.GetSourcesU64(),
			distSpec.GetTargetsU64(),
			weighted,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute distances: %v", err)
		}
		metrics.RecordAlgoDuration("distances", time.Since(computeStart).Seconds())

		result = service.NewAlgoResult(version.ID, service.AlgoKindDistances, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.DistancesF64 = distResult.Distances
		result.Meta["num_sources"] = fmt.Sprintf("%d", distResult.NumSources)
		result.Meta["num_targets"] = fmt.Sprintf("%d", distResult.NumTargets)

	case *gepb.AlgoSpec_Bfs:
		// BFS is similar to shortest path but always unweighted
		bfsSpec := spec.Bfs
		h.logger.Info("running BFS algorithm",
			"version_id", version.ID,
			"source", bfsSpec.GetSourceU64(),
			"max_depth", bfsSpec.GetMaxDepth(),
		)

		// For BFS, we use it as neighborhood exploration
		// which is already implemented in the View system
		// Return the reachable vertices as the result
		return nil, status.Errorf(codes.Unimplemented, "bfs result format pending - use CreateView with neighborhood spec instead")

	case *gepb.AlgoSpec_StMincut:
		mincutSpec := spec.StMincut
		h.logger.Info("running st_mincut algorithm",
			"version_id", version.ID,
			"source", mincutSpec.GetSourceU64(),
			"target", mincutSpec.GetTargetU64(),
			"capacity_column", mincutSpec.GetCapacityColumn(),
		)

		// Validate request
		useWeights := mincutSpec.GetCapacityColumn() != ""
		if err := service.ValidateMinCutRequest(version, view, mincutSpec.GetSourceU64(), mincutSpec.GetTargetU64(), service.DefaultMaxCorridorEdges); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid mincut request: %v", err)
		}

		// Get timeout from request or use default
		timeout := service.DefaultMinCutTimeout
		if req.GetTimeout() != nil {
			timeout = req.GetTimeout().AsDuration()
		}

		// Create context with timeout
		mincutCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		// Compute mincut
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		mincutResult, err := service.ComputeSTMinCut(
			mincutCtx,
			version,
			view, // nil if not using a view
			mincutSpec.GetSourceU64(),
			mincutSpec.GetTargetU64(),
			useWeights,
			service.DefaultMaxCorridorEdges,
			nil, // use default config
		)
		computeSpan.End()
		if err != nil {
			if mincutCtx.Err() == context.DeadlineExceeded {
				return nil, status.Errorf(codes.DeadlineExceeded, "mincut computation timed out after %v", timeout)
			}
			return nil, status.Errorf(codes.Internal, "failed to compute mincut: %v", err)
		}
		metrics.RecordAlgoDuration("st_mincut", time.Since(computeStart).Seconds())

		// Build result
		paramsHash := service.HashParams("st_mincut", mincutSpec.GetSourceU64(), mincutSpec.GetTargetU64(), useWeights)
		result = service.NewAlgoResult(version.ID, service.AlgoKindSTMinCut, paramsHash)
		result.AddSpan(computeSpan)
		result.CutValue = mincutResult.CutValue
		result.CutEdges = mincutResult.CutEdges
		result.Meta = mincutResult.Meta

		h.logger.Info("mincut completed",
			"cut_value", mincutResult.CutValue,
			"cut_edges", len(mincutResult.CutEdges),
			"source_side_vertices", len(mincutResult.SourceSideVertices),
		)

	case *gepb.AlgoSpec_Corridor:
		corrSpec := spec.Corridor
		h.logger.Info("running corridor algorithm",
			"version_id", version.ID,
			"source", corrSpec.GetSourceU64(),
			"target", corrSpec.GetTargetU64(),
			"method", corrSpec.GetMethod(),
			"hops", corrSpec.GetHops(),
		)

		// Validate spec
		if err := service.ValidateCorridorSpec(corrSpec); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid corridor spec: %v", err)
		}

		// Compute corridor
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		corrResult, err := service.ComputeCorridor(
			version,
			view, // nil if not using a view
			corrSpec.GetSourceU64(),
			corrSpec.GetTargetU64(),
			corrSpec.GetMethod(),
			corrSpec.GetHops(),
			corrSpec.GetK(),
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute corridor: %v", err)
		}
		metrics.RecordAlgoDuration("corridor", time.Since(computeStart).Seconds())

		// Store the corridor view in ViewStore
		corrResult.View.Pin() // Pin so it's not evicted immediately
		if err := h.viewManager.StoreView(corrResult.View); err != nil {
			h.logger.Warn("failed to store corridor view", "error", err)
		}

		// Build result
		paramsHash := service.HashParams("corridor", corrSpec.GetSourceU64(), corrSpec.GetTargetU64(), corrSpec.GetMethod(), corrSpec.GetHops())
		result = service.NewAlgoResult(version.ID, service.AlgoKindCorridor, paramsHash)
		result.AddSpan(computeSpan)
		result.Meta = corrResult.Meta
		result.Meta["view_id"] = corrResult.View.ID

		// Store path info if available
		if corrResult.PathSummary != nil && corrResult.PathSummary.Found {
			result.PathVertices = corrResult.PathSummary.PathVertices
			result.PathCost = corrResult.PathSummary.TotalCost
		}

	default:
		return nil, status.Errorf(codes.InvalidArgument, "unknown algorithm type")
	}

	// Store result
	storeSpan := service.NewTraceSpan("store_result")
	if err := h.resultStore.Store(result); err != nil {
		h.logger.Warn("failed to store result", "error", err)
		// Continue anyway - result is still valid
	}
	storeSpan.End()
	result.AddSpan(storeSpan)

	h.logger.Info("algorithm completed",
		"result_id", result.ID,
		"algo_kind", result.AlgoKind,
		"spans", len(result.Spans),
	)

	// For Phase 1, we run synchronously and return the result ID as the job ID
	return &gepb.RunResponse{
		Job: &gepb.JobRef{JobId: result.ID},
	}, nil
}

// GetJob polls the status of a job.
func (h *GraphEngineHandler) GetJob(ctx context.Context, req *gepb.GetJobRequest) (*gepb.GetJobResponse, error) {
	jobID := req.GetJob().GetJobId()
	h.logger.Debug("GetJob called", "job_id", jobID)

	// For Phase 1, jobs run synchronously so job_id = result_id
	// Just check if the result exists
	result, err := h.resultStore.Get(jobID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "job not found: %s", jobID)
	}
	defer result.Unpin()

	// Since Phase 1 is synchronous, all jobs are already completed
	return &gepb.GetJobResponse{
		State: gepb.GetJobResponse_SUCCEEDED,
		Result: &gepb.ResultRef{
			ResultId: result.ID,
		},
	}, nil
}

// CancelJob cancels a running job.
func (h *GraphEngineHandler) CancelJob(ctx context.Context, req *gepb.CancelJobRequest) (*gepb.CancelJobResponse, error) {
	h.logger.Info("CancelJob called", "job_id", req.GetJob().GetJobId())

	// TODO: Phase 1 - Implement job cancellation
	return nil, status.Errorf(codes.Unimplemented, "CancelJob not implemented yet")
}

// GetResult streams result data back to the client.
func (h *GraphEngineHandler) GetResult(req *gepb.GetResultRequest, stream gepb.GraphEngine_GetResultServer) error {
	resultID := req.GetResult().GetResultId()
	h.logger.Debug("GetResult called", "result_id", resultID)

	// Get result from store
	result, err := h.resultStore.Get(resultID)
	if err != nil {
		return status.Errorf(codes.NotFound, "result not found: %s", resultID)
	}
	defer result.Unpin()

	// Send header
	header := &gepb.ResultChunk{
		Payload: &gepb.ResultChunk_Header{
			Header: &gepb.ResultHeader{
				ResultId: result.ID,
				Type:     string(result.AlgoKind),
				Meta:     result.Meta,
			},
		},
	}
	if err := stream.Send(header); err != nil {
		return status.Errorf(codes.Internal, "failed to send header: %v", err)
	}

	// Send data based on algorithm type
	switch result.AlgoKind {
	case service.AlgoKindComponents, service.AlgoKindCommunities:
		// Send membership array
		if len(result.MembershipU32) > 0 {
			chunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_U32{
					U32: &gepb.U32Buffer{
						Name:   "membership",
						Values: result.MembershipU32,
					},
				},
			}
			if err := stream.Send(chunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send membership: %v", err)
			}
		}

	case service.AlgoKindShortestPath:
		// Send shortest path result
		if len(result.PathVertices) > 0 || result.PathCost > 0 {
			chunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_ShortestPath{
					ShortestPath: &gepb.ShortestPathResult{
						VerticesU64: result.PathVertices,
						EdgesU64:    result.PathEdges,
						TotalCost:   result.PathCost,
					},
				},
			}
			if err := stream.Send(chunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send shortest path: %v", err)
			}
		}

	case service.AlgoKindDistances:
		// Send distances array
		if len(result.DistancesF64) > 0 {
			chunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_F64{
					F64: &gepb.F64Buffer{
						Name:   "distances",
						Values: result.DistancesF64,
					},
				},
			}
			if err := stream.Send(chunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send distances: %v", err)
			}
		}

	case service.AlgoKindSTMinCut:
		// Send mincut result
		chunk := &gepb.ResultChunk{
			Payload: &gepb.ResultChunk_StMincut{
				StMincut: &gepb.STMinCutResult{
					CutValue:    result.CutValue,
					CutEdgesU64: result.CutEdges,
				},
			},
		}
		if err := stream.Send(chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to send mincut: %v", err)
		}

	case service.AlgoKindCorridor:
		// Send corridor result with view reference
		viewID := result.Meta["view_id"]
		chunk := &gepb.ResultChunk{
			Payload: &gepb.ResultChunk_Corridor{
				Corridor: &gepb.CorridorResult{
					View: &gepb.ViewRef{
						ViewId: viewID,
					},
					Meta: result.Meta,
				},
			},
		}
		if err := stream.Send(chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to send corridor: %v", err)
		}

		// If there's a path summary, also send it
		if len(result.PathVertices) > 0 {
			pathChunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_ShortestPath{
					ShortestPath: &gepb.ShortestPathResult{
						VerticesU64: result.PathVertices,
						TotalCost:   result.PathCost,
					},
				},
			}
			if err := stream.Send(pathChunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send corridor path: %v", err)
			}
		}

	case service.AlgoKindKSP:
		// Send each K-shortest path as a ShortestPathResult chunk
		for i, kspPath := range result.KSPPaths {
			pathChunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_ShortestPath{
					ShortestPath: &gepb.ShortestPathResult{
						VerticesU64: kspPath.Vertices,
						EdgesU64:    kspPath.Edges,
						TotalCost:   kspPath.Cost,
					},
				},
			}
			if err := stream.Send(pathChunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send ksp path %d: %v", i, err)
			}
		}
	}

	// Send done marker
	done := &gepb.ResultChunk{
		Payload: &gepb.ResultChunk_Done{Done: true},
	}
	if err := stream.Send(done); err != nil {
		return status.Errorf(codes.Internal, "failed to send done: %v", err)
	}

	h.logger.Debug("GetResult completed", "result_id", resultID)
	return nil
}

// Release releases server-side resources early.
func (h *GraphEngineHandler) Release(ctx context.Context, req *gepb.ReleaseRequest) (*gepb.ReleaseResponse, error) {
	h.logger.Info("Release called")

	// TODO: Phase 1 - Implement resource release
	return nil, status.Errorf(codes.Unimplemented, "Release not implemented yet")
}
