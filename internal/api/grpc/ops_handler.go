package grpc

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/naisa-ai/graph-engine/internal/service"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// Version information (set via ldflags at build time)
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// GraphEngineOpsHandler implements the GraphEngineOps gRPC service.
type GraphEngineOpsHandler struct {
	gepb.UnimplementedGraphEngineOpsServer
	logger         *slog.Logger
	buildStore     *service.BuildStore
	graphRegistry  *service.GraphRegistry
	versionStore   *service.VersionStore
	resultStore    *service.ResultStore
	viewStore      *service.ViewStore
	maxExportEdges uint64
	startTime      time.Time
}

// NewGraphEngineOpsHandler creates a new GraphEngineOpsHandler.
func NewGraphEngineOpsHandler(
	logger *slog.Logger,
	buildStore *service.BuildStore,
	graphRegistry *service.GraphRegistry,
	versionStore *service.VersionStore,
	resultStore *service.ResultStore,
	viewStore *service.ViewStore,
	maxExportEdges int,
) *GraphEngineOpsHandler {
	if maxExportEdges <= 0 {
		maxExportEdges = 10000
	}
	return &GraphEngineOpsHandler{
		logger:         logger,
		buildStore:     buildStore,
		graphRegistry:  graphRegistry,
		versionStore:   versionStore,
		resultStore:    resultStore,
		viewStore:      viewStore,
		maxExportEdges: uint64(maxExportEdges),
		startTime:      time.Now(),
	}
}

// Health returns the health status of the service.
func (h *GraphEngineOpsHandler) Health(ctx context.Context, req *gepb.HealthRequest) (*gepb.HealthResponse, error) {
	h.logger.Debug("Health check called")

	uptime := time.Since(h.startTime).Round(time.Second).String()

	return &gepb.HealthResponse{
		Status: "SERVING",
		Meta: map[string]string{
			"version":        Version,
			"build_time":     BuildTime,
			"uptime":         uptime,
			"go_version":     runtime.Version(),
			"pending_builds": intToString(h.buildStore.Count()),
		},
	}, nil
}

// ListGraphs returns a list of all graphs.
func (h *GraphEngineOpsHandler) ListGraphs(ctx context.Context, req *gepb.ListGraphsRequest) (*gepb.ListGraphsResponse, error) {
	h.logger.Debug("ListGraphs called")

	summaries := h.graphRegistry.ListGraphs()
	h.logger.Debug("ListGraphs returning", "count", len(summaries))

	return &gepb.ListGraphsResponse{
		Graphs: summaries,
	}, nil
}

// DescribeGraph returns detailed information about a graph.
func (h *GraphEngineOpsHandler) DescribeGraph(ctx context.Context, req *gepb.DescribeGraphRequest) (*gepb.DescribeGraphResponse, error) {
	graphName := req.GetGraph().GetGraphName()
	versionID := req.GetGraph().GetVersionId()

	h.logger.Debug("DescribeGraph called",
		"graph_name", graphName,
		"version_id", versionID,
	)

	// Get version from registry
	version, err := h.graphRegistry.GetVersion(graphName, versionID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "graph not found: %v", err)
	}
	defer version.Unpin()

	return &gepb.DescribeGraphResponse{
		Summary: version.ToSummary(),
		Schema:  version.Schema,
		Labels:  version.Labels,
	}, nil
}

// CacheStats returns cache statistics.
func (h *GraphEngineOpsHandler) CacheStats(ctx context.Context, req *gepb.CacheStatsRequest) (*gepb.CacheStatsResponse, error) {
	h.logger.Debug("CacheStats called")

	resultStats := h.resultStore.Stats()
	versionStats := h.versionStore.Stats()

	return &gepb.CacheStatsResponse{
		ResultsItems: uint64(resultStats.TotalItems),
		ResultsBytes: resultStats.TotalMemory,
		ViewsItems:   uint64(versionStats.TotalVersions), // Using versions as proxy for now
		ViewsBytes:   versionStats.TotalMemory,
	}, nil
}

// ValidateGraph validates graph integrity.
func (h *GraphEngineOpsHandler) ValidateGraph(ctx context.Context, req *gepb.ValidateGraphRequest) (*gepb.ValidateGraphResponse, error) {
	graphName := req.GetGraph().GetGraphName()
	versionID := req.GetGraph().GetVersionId()
	deep := req.GetDeep()

	h.logger.Info("ValidateGraph called",
		"graph_name", graphName,
		"version_id", versionID,
		"deep", deep,
	)

	// Get the graph version
	version, err := h.graphRegistry.GetVersion(graphName, versionID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "graph not found: %v", err)
	}
	defer version.Unpin()

	// Perform validation
	result := service.ValidateGraph(version, deep)

	// Build response
	resp := &gepb.ValidateGraphResponse{
		Status: &gepb.Status{
			Code:    0,
			Message: "validation complete",
		},
		Warnings: result.Warnings,
		Metrics:  result.Metrics,
	}

	// Set error status if validation failed
	if !result.Valid {
		resp.Status.Code = 1
		if len(result.Errors) > 0 {
			resp.Status.Message = result.Errors[0].Message
		} else {
			resp.Status.Message = "validation failed"
		}
	}

	h.logger.Info("ValidateGraph completed",
		"graph_name", graphName,
		"valid", result.Valid,
		"errors", len(result.Errors),
		"warnings", len(result.Warnings),
	)

	return resp, nil
}

// TraceJob returns trace spans for a job.
func (h *GraphEngineOpsHandler) TraceJob(ctx context.Context, req *gepb.TraceJobRequest) (*gepb.TraceJobResponse, error) {
	jobID := req.GetJob().GetJobId()
	h.logger.Info("TraceJob called", "job_id", jobID)

	// Get result from store (job_id == result_id in Phase 1)
	result, err := h.resultStore.Get(jobID)
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "job not found: %s", jobID)
	}
	defer result.Unpin()

	// Convert service spans to proto spans
	spans := make([]*gepb.TraceSpan, 0, len(result.Spans))
	for _, span := range result.Spans {
		protoSpan := &gepb.TraceSpan{
			Name:     span.Name,
			Duration: durationpb.New(span.Duration),
			Tags:     span.Tags,
		}
		spans = append(spans, protoSpan)
	}

	// Add a synthetic "total" span if we have timing info
	if len(spans) == 0 && result.CreatedAt.After(time.Time{}) {
		// Add basic info even without detailed spans
		spans = append(spans, &gepb.TraceSpan{
			Name:     "total",
			Duration: durationpb.New(time.Since(result.CreatedAt)),
			Tags: map[string]string{
				"algo_kind":  string(result.AlgoKind),
				"version_id": result.VersionID,
			},
		})
	}

	h.logger.Debug("TraceJob returning", "job_id", jobID, "spans", len(spans))

	return &gepb.TraceJobResponse{
		Spans: spans,
	}, nil
}

// ExportSubgraph exports subgraph data.
func (h *GraphEngineOpsHandler) ExportSubgraph(req *gepb.ExportSubgraphRequest, stream gepb.GraphEngineOps_ExportSubgraphServer) error {
	h.logger.Info("ExportSubgraph called",
		"format", req.GetFormat().String(),
		"max_edges", req.GetMaxEdges(),
	)

	// Determine export format
	var exportFormat service.ExportFormat
	switch req.GetFormat() {
	case gepb.ExportSubgraphRequest_EDGE_LIST:
		exportFormat = service.ExportFormatEdgeList
	case gepb.ExportSubgraphRequest_CSV:
		exportFormat = service.ExportFormatCSV
	default:
		exportFormat = service.ExportFormatEdgeList
	}

	// Determine max edges limit (use request value if specified, else default)
	maxEdges := h.maxExportEdges
	if req.GetMaxEdges() > 0 {
		maxEdges = req.GetMaxEdges()
	}

	// Create exporter with config
	exportConfig := service.ExportConfig{
		Format:         exportFormat,
		MaxEdges:       maxEdges,
		IncludeWeights: true,
		IncludeKind:    true,
		ChunkSize:      1000,
	}
	exporter := service.NewGraphExporter(exportConfig)

	// Handle graph or view target
	switch target := req.GetTarget().(type) {
	case *gepb.ExportSubgraphRequest_Graph:
		return h.exportGraph(target.Graph, exporter, stream)
	case *gepb.ExportSubgraphRequest_View:
		return h.exportView(target.View, exporter, stream)
	default:
		return status.Errorf(codes.InvalidArgument, "target must be specified (graph or view)")
	}
}

// exportGraph exports a full graph.
func (h *GraphEngineOpsHandler) exportGraph(graphRef *gepb.GraphRef, exporter *service.GraphExporter, stream gepb.GraphEngineOps_ExportSubgraphServer) error {
	graphName := graphRef.GetGraphName()
	versionID := graphRef.GetVersionId()

	h.logger.Info("ExportSubgraph: exporting graph",
		"graph_name", graphName,
		"version_id", versionID,
	)

	// Get the graph version
	version, err := h.graphRegistry.GetVersion(graphName, versionID)
	if err != nil {
		return status.Errorf(codes.NotFound, "graph not found: %v", err)
	}
	defer version.Unpin()

	// Check size limit
	canExport, ecount, msg := exporter.CanExport(version.ECount)
	if !canExport {
		return status.Errorf(codes.FailedPrecondition, "%s", msg)
	}

	h.logger.Info("ExportSubgraph: starting export",
		"graph_name", graphName,
		"edge_count", ecount,
	)

	// Export and stream
	ch, result, err := exporter.ExportGraph(version)
	if err != nil {
		return status.Errorf(codes.Internal, "export failed: %v", err)
	}

	// Stream chunks to client
	for chunk := range ch {
		if err := stream.Send(&gepb.ExportChunk{Data: chunk}); err != nil {
			h.logger.Warn("ExportSubgraph: stream send failed", "error", err)
			return err
		}
	}

	h.logger.Info("ExportSubgraph: completed",
		"graph_name", graphName,
		"total_edges", result.TotalEdges,
		"total_bytes", result.TotalBytes,
	)

	return nil
}

// exportView exports a view.
func (h *GraphEngineOpsHandler) exportView(viewRef *gepb.ViewRef, exporter *service.GraphExporter, stream gepb.GraphEngineOps_ExportSubgraphServer) error {
	viewID := viewRef.GetViewId()

	h.logger.Info("ExportSubgraph: exporting view", "view_id", viewID)

	// Get the view
	if h.viewStore == nil {
		return status.Errorf(codes.Internal, "view store not available")
	}

	view, err := h.viewStore.Get(viewID)
	if err != nil {
		return status.Errorf(codes.NotFound, "view not found: %v", err)
	}
	defer view.Unpin()

	// Check size limit
	canExport, ecount, msg := exporter.CanExport(view.ECount)
	if !canExport {
		return status.Errorf(codes.FailedPrecondition, "%s", msg)
	}

	h.logger.Info("ExportSubgraph: starting view export",
		"view_id", viewID,
		"edge_count", ecount,
	)

	// Export and stream
	ch, result, err := exporter.ExportView(view)
	if err != nil {
		return status.Errorf(codes.Internal, "export failed: %v", err)
	}

	// Stream chunks to client
	for chunk := range ch {
		if err := stream.Send(&gepb.ExportChunk{Data: chunk}); err != nil {
			h.logger.Warn("ExportSubgraph: stream send failed", "error", err)
			return err
		}
	}

	h.logger.Info("ExportSubgraph: view export completed",
		"view_id", viewID,
		"total_edges", result.TotalEdges,
		"total_bytes", result.TotalBytes,
	)

	return nil
}

// Helper function to convert int to string
func intToString(n int) string {
	return fmt.Sprintf("%d", n)
}
