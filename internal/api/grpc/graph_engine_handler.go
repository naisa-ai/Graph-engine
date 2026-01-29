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
	"github.com/naisa-ai/graph-engine/internal/shim"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// createShimGraphForVersion creates a shim graph from a GraphVersion.
func createShimGraphForVersion(version *service.GraphVersion) (*shim.Graph, error) {
	return shim.NewGraph(uint32(version.VCount), version.EdgeSrc, version.EdgeDst, version.Directed)
}

// GraphEngineHandler implements the GraphEngine gRPC service.
type GraphEngineHandler struct {
	gepb.UnimplementedGraphEngineServer
	logger        *slog.Logger
	buildStore    *service.BuildStore
	graphRegistry *service.GraphRegistry
	versionStore  *service.VersionStore
	resultStore   *service.ResultStore
	viewManager   *service.ViewManager
	scheduler     *service.Scheduler
}

// NewGraphEngineHandler creates a new GraphEngineHandler.
func NewGraphEngineHandler(
	logger *slog.Logger,
	buildStore *service.BuildStore,
	graphRegistry *service.GraphRegistry,
	versionStore *service.VersionStore,
	resultStore *service.ResultStore,
	viewManager *service.ViewManager,
	scheduler *service.Scheduler,
) *GraphEngineHandler {
	return &GraphEngineHandler{
		logger:        logger,
		buildStore:    buildStore,
		graphRegistry: graphRegistry,
		versionStore:  versionStore,
		resultStore:   resultStore,
		viewManager:   viewManager,
		scheduler:     scheduler,
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
			chunk := payload.VertexColumns
			data := columnChunkToData(chunk)
			if err := h.buildStore.AddVertexColumn(buildID, chunk.GetName(), data); err != nil {
				h.logger.Error("failed to add vertex column", "error", err, "name", chunk.GetName())
				return status.Errorf(codes.Internal, "failed to add vertex column: %v", err)
			}
			h.logger.Debug("vertex column added",
				"build_id", buildID,
				"column_name", chunk.GetName(),
				"length", data.Length(),
			)

		case *gepb.UploadRequest_EdgeColumns:
			chunk := payload.EdgeColumns
			data := columnChunkToData(chunk)
			if err := h.buildStore.AddEdgeColumn(buildID, chunk.GetName(), data); err != nil {
				h.logger.Error("failed to add edge column", "error", err, "name", chunk.GetName())
				return status.Errorf(codes.Internal, "failed to add edge column: %v", err)
			}
			h.logger.Debug("edge column added",
				"build_id", buildID,
				"column_name", chunk.GetName(),
				"length", data.Length(),
			)

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

	// Create a shared shim graph for batch artifacts if any are requested
	var shimGraph *shim.Graph
	if artifacts != nil && (artifacts.GetComputeComponents() || artifacts.GetComputeCommunities() || artifacts.GetComputeKcore() || artifacts.GetComputeBetweennessSampled()) {
		var err error
		shimGraph, err = createShimGraphForVersion(version)
		if err != nil {
			h.logger.Error("failed to create shim graph for artifacts", "error", err)
		} else {
			defer shimGraph.Close()
		}
	}

	if shimGraph != nil && artifacts != nil && artifacts.GetComputeComponents() {
		h.logger.Info("computing components artifact", "version_id", versionID)
		compCfg := &service.ComponentsConfig{ShimGraph: shimGraph}

		// Components will be computed when Run is called or as part of publish
		// For now, we'll compute it here synchronously
		result, err := service.ComputeComponents(version, true, compCfg) // weak components
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

	// Compute communities if requested
	if shimGraph != nil && artifacts != nil && artifacts.GetComputeCommunities() {
		h.logger.Info("computing communities artifact", "version_id", versionID)
		commShimCfg := &service.CommunitiesShimConfig{UseShim: true, ShimGraph: shimGraph}
		commConfig := service.DefaultCommunitiesConfig()

		commResult, err := service.ComputeCommunities(ctx, version, nil, commConfig, commShimCfg)
		if err != nil {
			h.logger.Error("failed to compute communities", "error", err)
		} else {
			// Attach communities to version for COMMUNITY_AWARE corridor
			version.SetCommunities(commResult.Membership, commResult.NumCommunities)

			// Store result for caching
			paramsHash := service.HashCommunitiesParams(commConfig.Algorithm.String(), commConfig.Resolution, "")
			algoResult := service.NewAlgoResult(version.ID, service.AlgoKindCommunities, paramsHash)
			algoResult.MembershipU32 = commResult.Membership
			algoResult.Meta["num_communities"] = fmt.Sprintf("%d", commResult.NumCommunities)
			algoResult.Meta["modularity"] = fmt.Sprintf("%.6f", commResult.Modularity)
			algoResult.Meta["algorithm"] = commConfig.Algorithm.String()
			algoResult.Meta["source"] = "batch_artifact"

			if err := h.resultStore.Store(algoResult); err != nil {
				h.logger.Warn("failed to store communities result", "error", err)
			} else {
				h.logger.Info("communities computed and stored",
					"result_id", algoResult.ID,
					"num_communities", commResult.NumCommunities,
					"modularity", commResult.Modularity,
				)
			}
		}
	}

	// Compute k-core if requested
	if shimGraph != nil && artifacts != nil && artifacts.GetComputeKcore() {
		h.logger.Info("computing k-core artifact", "version_id", versionID)
		kcoreConfig := service.DefaultKCoreConfig()
		kcoreShimCfg := &service.KCoreShimConfig{ShimGraph: shimGraph}

		kcoreResult, err := service.ComputeKCore(ctx, version, nil, kcoreConfig, kcoreShimCfg)
		if err != nil {
			h.logger.Error("failed to compute k-core", "error", err)
		} else {
			// Attach k-core to version
			version.SetKCore(kcoreResult.Coreness, kcoreResult.MaxCore)

			// Store result for caching
			paramsHash := service.HashParams("kcore", version.ID)
			algoResult := service.NewAlgoResult(version.ID, service.AlgoKindKCore, paramsHash)
			algoResult.CorenessU32 = kcoreResult.Coreness
			algoResult.Meta["max_core"] = fmt.Sprintf("%d", kcoreResult.MaxCore)
			algoResult.Meta["source"] = "batch_artifact"

			if err := h.resultStore.Store(algoResult); err != nil {
				h.logger.Warn("failed to store k-core result", "error", err)
			} else {
				h.logger.Info("k-core computed and stored",
					"result_id", algoResult.ID,
					"max_core", kcoreResult.MaxCore,
				)
			}
		}
	}

	// Compute sampled betweenness centrality if requested
	if shimGraph != nil && artifacts != nil && artifacts.GetComputeBetweennessSampled() {
		h.logger.Info("computing sampled betweenness artifact", "version_id", versionID)
		betwConfig := service.DefaultBetweennessConfig()
		betwConfig.SampleSize = 100 // Use sampling for batch artifact
		betwShimCfg := &service.BetweennessShimConfig{ShimGraph: shimGraph}

		betwResult, err := service.ComputeBetweenness(ctx, version, nil, betwConfig, betwShimCfg)
		if err != nil {
			h.logger.Error("failed to compute betweenness", "error", err)
		} else {
			// Attach betweenness to version
			version.SetBetweenness(betwResult.Scores)

			// Store result for caching
			paramsHash := service.HashParams("betweenness", betwConfig.SampleSize, betwConfig.Normalized, version.ID)
			algoResult := service.NewAlgoResult(version.ID, service.AlgoKindBetweenness, paramsHash)
			algoResult.BetweennessF64 = betwResult.Scores
			algoResult.Meta["sample_size"] = fmt.Sprintf("%d", betwConfig.SampleSize)
			algoResult.Meta["normalized"] = fmt.Sprintf("%t", betwConfig.Normalized)
			algoResult.Meta["source"] = "batch_artifact"

			if err := h.resultStore.Store(algoResult); err != nil {
				h.logger.Warn("failed to store betweenness result", "error", err)
			} else {
				h.logger.Info("betweenness computed and stored",
					"result_id", algoResult.ID,
					"sample_size", betwConfig.SampleSize,
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

// columnChunkToData converts a protobuf ColumnChunk to a service ColumnData.
func columnChunkToData(chunk *gepb.ColumnChunk) *service.ColumnData {
	data := &service.ColumnData{
		Name: chunk.GetName(),
	}

	// Map protobuf ColumnType to service ColumnType and extract data
	switch chunk.GetType() {
	case gepb.ColumnType_COL_BOOL:
		data.Type = service.ColumnTypeBool
		data.BoolVal = chunk.GetVBool()
	case gepb.ColumnType_COL_U32:
		data.Type = service.ColumnTypeU32
		data.U32Val = chunk.GetVU32()
	case gepb.ColumnType_COL_U64:
		data.Type = service.ColumnTypeU64
		data.U64Val = chunk.GetVU64()
	case gepb.ColumnType_COL_F32:
		data.Type = service.ColumnTypeF32
		data.F32Val = chunk.GetVF32()
	case gepb.ColumnType_COL_F64:
		data.Type = service.ColumnTypeF64
		data.F64Val = chunk.GetVF64()
	case gepb.ColumnType_COL_STRING:
		data.Type = service.ColumnTypeString
		data.StrVal = chunk.GetVString()
	}

	return data
}

// protoModeToString converts a NeighborhoodSpec_Mode to a string.
func protoModeToString(m gepb.NeighborhoodSpec_Mode) string {
	switch m {
	case gepb.NeighborhoodSpec_MODE_OUT:
		return "out"
	case gepb.NeighborhoodSpec_MODE_IN:
		return "in"
	default:
		return "all"
	}
}

// protoModeToBFSMode converts a NeighborhoodSpec_Mode to a service.BFSMode.
func protoModeToBFSMode(m gepb.NeighborhoodSpec_Mode) service.BFSMode {
	switch m {
	case gepb.NeighborhoodSpec_MODE_OUT:
		return service.BFSModeOut
	case gepb.NeighborhoodSpec_MODE_IN:
		return service.BFSModeIn
	default:
		return service.BFSModeAll
	}
}

// protoModeToNeighborhoodMode converts a NeighborhoodSpec_Mode to a service.NeighborhoodMode.
func protoModeToNeighborhoodMode(m gepb.NeighborhoodSpec_Mode) service.NeighborhoodMode {
	switch m {
	case gepb.NeighborhoodSpec_MODE_OUT:
		return service.NeighborhoodModeOut
	case gepb.NeighborhoodSpec_MODE_IN:
		return service.NeighborhoodModeIn
	default:
		return service.NeighborhoodModeAll
	}
}

// protoMethodToCommunityAlgorithm converts a CommunitiesSpec_Method to a service.CommunityAlgorithm.
func protoMethodToCommunityAlgorithm(m gepb.CommunitiesSpec_Method) service.CommunityAlgorithm {
	switch m {
	case gepb.CommunitiesSpec_LOUVAIN:
		return service.CommunityAlgorithmLouvain
	default:
		return service.CommunityAlgorithmLeiden
	}
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

		// Create shim graph for components computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		compCfg := &service.ComponentsConfig{ShimGraph: shimGraph}

		// Compute
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		result, err = service.ComputeComponents(version, weak, compCfg)
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

		// Create shim graph for shortest path computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		spCfg := &service.ShortestPathConfig{ShimGraph: shimGraph}

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
			spCfg,
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

		// Create shim graph for KSP computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		kspShimCfg := &service.KSPShimConfig{ShimGraph: shimGraph}

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
			kspShimCfg,
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
		bfsSpec := spec.Bfs
		h.logger.Info("running BFS algorithm",
			"version_id", version.ID,
			"source", bfsSpec.GetSourceU64(),
			"max_depth", bfsSpec.GetMaxDepth(),
			"mode", bfsSpec.GetMode(),
		)

		// Build cache key
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		paramsHash := service.HashBFSParams(
			bfsSpec.GetSourceU64(),
			bfsSpec.GetMaxDepth(),
			protoModeToString(bfsSpec.GetMode()),
			viewHash,
		)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindBFS, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for bfs", "result_id", cached.ID)
				metrics.IncCacheHits("bfs")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("bfs")

		// Create shim graph for BFS computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		bfsShimCfg := &service.BFSShimConfig{ShimGraph: shimGraph}

		// Build BFS config
		bfsConfig := &service.BFSConfig{
			MaxDepth:          bfsSpec.GetMaxDepth(),
			Mode:              protoModeToBFSMode(bfsSpec.GetMode()),
			ReturnExternalIDs: true,
		}

		// Compute BFS
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		bfsResult, err := service.ComputeBFS(
			ctx,
			version,
			view,
			bfsSpec.GetSourceU64(),
			bfsConfig,
			bfsShimCfg,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute bfs: %v", err)
		}
		metrics.RecordAlgoDuration("bfs", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindBFS, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.VerticesU64 = bfsResult.VisitedExternal
		result.Meta["source"] = bfsResult.Meta["source"]
		result.Meta["mode"] = bfsResult.Meta["mode"]
		result.Meta["num_visited"] = fmt.Sprintf("%d", bfsResult.NumVisited)
		result.Meta["max_depth_reached"] = fmt.Sprintf("%d", bfsResult.MaxDepthReached)

	case *gepb.AlgoSpec_Neighborhood:
		neighSpec := spec.Neighborhood
		h.logger.Info("running neighborhood algorithm",
			"version_id", version.ID,
			"num_seeds", len(neighSpec.GetSeedsU64()),
			"hops", neighSpec.GetHops(),
			"mode", neighSpec.GetMode(),
		)

		// Build cache key
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		paramsHash := service.HashNeighborhoodParams(
			neighSpec.GetSeedsU64(),
			neighSpec.GetHops(),
			protoModeToString(neighSpec.GetMode()),
			viewHash,
		)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindNeighborhood, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for neighborhood", "result_id", cached.ID)
				metrics.IncCacheHits("neighborhood")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("neighborhood")

		// Create shim graph for neighborhood computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		neighShimCfg := &service.NeighborhoodShimConfig{ShimGraph: shimGraph}

		// Build neighborhood config
		neighConfig := &service.NeighborhoodConfig{
			Hops:              neighSpec.GetHops(),
			Mode:              protoModeToNeighborhoodMode(neighSpec.GetMode()),
			ReturnExternalIDs: true,
		}

		// Compute neighborhood
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		neighResult, err := service.ComputeNeighborhood(
			ctx,
			version,
			view,
			neighSpec.GetSeedsU64(),
			neighConfig,
			neighShimCfg,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute neighborhood: %v", err)
		}
		metrics.RecordAlgoDuration("neighborhood", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindNeighborhood, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.VerticesU64 = neighResult.VerticesExternal
		result.Meta["source"] = neighResult.Meta["source"]
		result.Meta["mode"] = neighResult.Meta["mode"]
		result.Meta["hops"] = neighResult.Meta["hops"]
		result.Meta["num_vertices"] = fmt.Sprintf("%d", neighResult.NumVertices)

	case *gepb.AlgoSpec_Communities:
		commSpec := spec.Communities
		h.logger.Info("running communities algorithm",
			"version_id", version.ID,
			"method", commSpec.GetMethod(),
			"resolution", commSpec.GetResolution(),
		)

		// Build cache key
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		algorithm := protoMethodToCommunityAlgorithm(commSpec.GetMethod())
		resolution := commSpec.GetResolution()
		if resolution <= 0 {
			resolution = 1.0
		}
		paramsHash := service.HashCommunitiesParams(
			algorithm.String(),
			resolution,
			viewHash,
		)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindCommunities, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for communities", "result_id", cached.ID)
				metrics.IncCacheHits("communities")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("communities")

		// Create shim graph for communities computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		commShimCfg := &service.CommunitiesShimConfig{
			UseShim:   true,
			ShimGraph: shimGraph,
		}

		// Build communities config
		commConfig := &service.CommunitiesConfig{
			Algorithm:  algorithm,
			Resolution: resolution,
		}

		// Compute communities
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		commResult, err := service.ComputeCommunities(
			ctx,
			version,
			view,
			commConfig,
			commShimCfg,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute communities: %v", err)
		}
		metrics.RecordAlgoDuration("communities", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindCommunities, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.MembershipU32 = commResult.Membership
		result.Meta["source"] = commResult.Meta["source"]
		result.Meta["algorithm"] = commResult.Meta["algorithm"]
		result.Meta["resolution"] = commResult.Meta["resolution"]
		result.Meta["num_communities"] = fmt.Sprintf("%d", commResult.NumCommunities)
		result.Meta["modularity"] = fmt.Sprintf("%.6f", commResult.Modularity)

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

		// Create shim graph for mincut computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		mincutCfg := &service.MinCutConfig{ShimGraph: shimGraph}

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
			mincutCfg,
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

		// Create shim graph for shortest path computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		spCfg := &service.ShortestPathConfig{ShimGraph: shimGraph}

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
			spCfg,
		)
		computeSpan.End()
		if err != nil {
			shimGraph.Close() // Close before returning error
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

	case *gepb.AlgoSpec_Kcore:
		kcoreSpec := spec.Kcore
		h.logger.Info("running k-core algorithm",
			"version_id", version.ID,
			"k", kcoreSpec.GetK(),
		)

		// Build cache key
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		paramsHash := service.HashKCoreParams(kcoreSpec.GetK(), viewHash)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindKCore, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for k-core", "result_id", cached.ID)
				metrics.IncCacheHits("kcore")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("kcore")

		// Create shim graph for k-core computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		kcoreShimCfg := &service.KCoreShimConfig{ShimGraph: shimGraph}

		// Build k-core config
		kcoreConfig := &service.KCoreConfig{
			K: kcoreSpec.GetK(),
		}

		// Compute k-core
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		kcoreResult, err := service.ComputeKCore(
			ctx,
			version,
			view,
			kcoreConfig,
			kcoreShimCfg,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute k-core: %v", err)
		}
		metrics.RecordAlgoDuration("kcore", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindKCore, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.CorenessU32 = kcoreResult.Coreness
		result.MaxCore = kcoreResult.MaxCore
		result.Meta["max_core"] = fmt.Sprintf("%d", kcoreResult.MaxCore)
		result.Meta["k"] = fmt.Sprintf("%d", kcoreSpec.GetK())

	case *gepb.AlgoSpec_Betweenness:
		betwSpec := spec.Betweenness
		h.logger.Info("running betweenness algorithm",
			"version_id", version.ID,
			"sample_size", betwSpec.GetSampleSize(),
			"normalized", betwSpec.GetNormalized(),
			"weight_column", betwSpec.GetWeightColumn(),
		)

		// Build cache key
		viewHash := ""
		if view != nil {
			viewHash = view.SpecHash
		}
		paramsHash := service.HashBetweennessParams(
			betwSpec.GetSampleSize(),
			betwSpec.GetNormalized(),
			betwSpec.GetWeightColumn(),
			viewHash,
		)

		// Check cache
		cacheSpan := service.NewTraceSpan("cache_lookup")
		if req.GetAllowCache() {
			if cached, found := h.resultStore.GetByKey(version.ID, service.AlgoKindBetweenness, paramsHash); found {
				cacheSpan.End()
				cacheSpan.AddTag("hit", "true")
				h.logger.Info("cache hit for betweenness", "result_id", cached.ID)
				metrics.IncCacheHits("betweenness")
				return &gepb.RunResponse{
					Job: &gepb.JobRef{JobId: cached.ID},
				}, nil
			}
		}
		cacheSpan.End()
		cacheSpan.AddTag("hit", "false")
		metrics.IncCacheMisses("betweenness")

		// Create shim graph for betweenness computation
		shimGraph, err := createShimGraphForVersion(version)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to create shim graph: %v", err)
		}
		defer shimGraph.Close()
		betwShimCfg := &service.BetweennessShimConfig{ShimGraph: shimGraph}

		// Build betweenness config
		betwConfig := &service.BetweennessConfig{
			SampleSize:   betwSpec.GetSampleSize(),
			Normalized:   betwSpec.GetNormalized(),
			WeightColumn: betwSpec.GetWeightColumn(),
		}

		// Compute betweenness
		computeSpan := service.NewTraceSpan("compute")
		computeStart := time.Now()
		betwResult, err := service.ComputeBetweenness(
			ctx,
			version,
			view,
			betwConfig,
			betwShimCfg,
		)
		computeSpan.End()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to compute betweenness: %v", err)
		}
		metrics.RecordAlgoDuration("betweenness", time.Since(computeStart).Seconds())

		// Build result
		result = service.NewAlgoResult(version.ID, service.AlgoKindBetweenness, paramsHash)
		result.AddSpan(cacheSpan)
		result.AddSpan(computeSpan)
		result.BetweennessF64 = betwResult.Scores
		result.Meta["sample_size"] = fmt.Sprintf("%d", betwSpec.GetSampleSize())
		result.Meta["normalized"] = fmt.Sprintf("%t", betwSpec.GetNormalized())
		result.Meta["max_score"] = fmt.Sprintf("%.6f", betwResult.MaxScore)
		result.Meta["min_score"] = fmt.Sprintf("%.6f", betwResult.MinScore)

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
	jobID := req.GetJob().GetJobId()
	h.logger.Info("CancelJob called", "job_id", jobID)

	if jobID == "" {
		return nil, status.Errorf(codes.InvalidArgument, "job_id is required")
	}

	// Check if scheduler is available
	if h.scheduler == nil {
		h.logger.Warn("scheduler not configured, cannot cancel job")
		return &gepb.CancelJobResponse{Canceled: false}, nil
	}

	// Attempt to cancel the job
	canceled := h.scheduler.CancelJob(jobID)

	h.logger.Info("CancelJob result", "job_id", jobID, "canceled", canceled)
	return &gepb.CancelJobResponse{Canceled: canceled}, nil
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

	case service.AlgoKindBFS, service.AlgoKindNeighborhood:
		// Send vertices array for BFS/Neighborhood results
		if len(result.VerticesU64) > 0 {
			chunk := &gepb.ResultChunk{
				Payload: &gepb.ResultChunk_U64{
					U64: &gepb.U64Buffer{
						Name:   "vertices",
						Values: result.VerticesU64,
					},
				},
			}
			if err := stream.Send(chunk); err != nil {
				return status.Errorf(codes.Internal, "failed to send vertices: %v", err)
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

	case service.AlgoKindKCore:
		// Send k-core result
		chunk := &gepb.ResultChunk{
			Payload: &gepb.ResultChunk_Kcore{
				Kcore: &gepb.KCoreResult{
					Coreness: result.CorenessU32,
					MaxCore:  result.MaxCore,
				},
			},
		}
		if err := stream.Send(chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to send k-core: %v", err)
		}

	case service.AlgoKindBetweenness:
		// Send betweenness result
		chunk := &gepb.ResultChunk{
			Payload: &gepb.ResultChunk_Betweenness{
				Betweenness: &gepb.BetweennessResult{
					Scores: result.BetweennessF64,
				},
			},
		}
		if err := stream.Send(chunk); err != nil {
			return status.Errorf(codes.Internal, "failed to send betweenness: %v", err)
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

	switch target := req.GetTarget().(type) {
	case *gepb.ReleaseRequest_View:
		viewID := target.View.GetViewId()
		if viewID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "view_id is required")
		}

		h.logger.Info("releasing view", "view_id", viewID)

		// First, unpin the view (client is done with it)
		if err := h.viewManager.ReleaseView(viewID); err != nil {
			h.logger.Warn("failed to unpin view", "view_id", viewID, "error", err)
			return nil, status.Errorf(codes.NotFound, "view not found: %s", viewID)
		}

		// Now try to delete the view (will succeed if no one else is using it)
		err := h.viewManager.DeleteView(viewID)
		if err != nil {
			// If delete fails because view is still pinned by others, that's OK
			// The view was still unpinned for this client
			h.logger.Info("view unpinned but still in use by others", "view_id", viewID)
			return &gepb.ReleaseResponse{Released: false}, nil
		}

		h.logger.Info("view released and deleted", "view_id", viewID)
		return &gepb.ReleaseResponse{Released: true}, nil

	case *gepb.ReleaseRequest_Result:
		resultID := target.Result.GetResultId()
		if resultID == "" {
			return nil, status.Errorf(codes.InvalidArgument, "result_id is required")
		}

		h.logger.Info("releasing result", "result_id", resultID)

		// Get the result to unpin it
		result, err := h.resultStore.Get(resultID)
		if err != nil {
			h.logger.Warn("failed to get result for release", "result_id", resultID, "error", err)
			return nil, status.Errorf(codes.NotFound, "result not found: %s", resultID)
		}

		// Unpin twice: once for the Get() call above, once for the client's release
		result.Unpin()
		result.Unpin()

		// Now try to delete the result
		err = h.resultStore.Delete(resultID)
		if err != nil {
			// If delete fails because result is still pinned by others, that's OK
			h.logger.Info("result unpinned but still in use by others", "result_id", resultID)
			return &gepb.ReleaseResponse{Released: false}, nil
		}

		h.logger.Info("result released and deleted", "result_id", resultID)
		return &gepb.ReleaseResponse{Released: true}, nil

	default:
		return nil, status.Errorf(codes.InvalidArgument, "target must be specified (view or result)")
	}
}
