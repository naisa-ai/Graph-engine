package graphengine

import (
	"bytes"
	"context"
	"io"
	"time"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
)

// =============================================================================
// Ops Result Types
// =============================================================================

// HealthStatus represents the health check response.
type HealthStatus struct {
	Status  string
	Version string
	Uptime  string
	Meta    map[string]string
}

// GraphSummary represents summary information about a graph.
type GraphSummary struct {
	GraphName        string
	CurrentVersionID string
	VCount           uint64
	ECount           uint64
	PublishedAt      time.Time
}

// GraphDetails represents detailed information about a graph.
type GraphDetails struct {
	Summary *GraphSummary
	Schema  *gepb.Schema
	Labels  map[string]string
}

// ValidationResult represents graph validation results.
type ValidationResult struct {
	Valid    bool
	Warnings []string
	Metrics  map[string]string
	Message  string
}

// CacheStats represents cache statistics.
type CacheStats struct {
	ResultsItems uint64
	ResultsBytes uint64
	ViewsItems   uint64
	ViewsBytes   uint64
}

// TraceSpan represents a trace span for job debugging.
type TraceSpan struct {
	Name     string
	Duration time.Duration
	Tags     map[string]string
}

// =============================================================================
// Ops Client Methods
// =============================================================================

// Health returns the health status of the service.
func (c *Client) Health(ctx context.Context) (*HealthStatus, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.Health(ctx, &gepb.HealthRequest{})
	if err != nil {
		return nil, wrapError(err, "Health")
	}

	status := &HealthStatus{
		Status: resp.Status,
		Meta:   resp.Meta,
	}

	if resp.Meta != nil {
		status.Version = resp.Meta["version"]
		status.Uptime = resp.Meta["uptime"]
	}

	return status, nil
}

// ListGraphs returns a list of all graphs.
func (c *Client) ListGraphs(ctx context.Context) ([]*GraphSummary, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.ListGraphs(ctx, &gepb.ListGraphsRequest{})
	if err != nil {
		return nil, wrapError(err, "ListGraphs")
	}

	summaries := make([]*GraphSummary, len(resp.Graphs))
	for i, g := range resp.Graphs {
		summaries[i] = &GraphSummary{
			GraphName:        g.GraphName,
			CurrentVersionID: g.CurrentVersionId,
			VCount:           g.Vcount,
			ECount:           g.Ecount,
		}
		if g.PublishedAt != nil {
			summaries[i].PublishedAt = g.PublishedAt.AsTime()
		}
	}

	return summaries, nil
}

// DescribeGraph returns detailed information about a graph.
func (c *Client) DescribeGraph(ctx context.Context, graph *gepb.GraphRef) (*GraphDetails, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.DescribeGraph(ctx, &gepb.DescribeGraphRequest{Graph: graph})
	if err != nil {
		return nil, wrapError(err, "DescribeGraph")
	}

	details := &GraphDetails{
		Schema: resp.Schema,
		Labels: resp.Labels,
	}

	if resp.Summary != nil {
		details.Summary = &GraphSummary{
			GraphName:        resp.Summary.GraphName,
			CurrentVersionID: resp.Summary.CurrentVersionId,
			VCount:           resp.Summary.Vcount,
			ECount:           resp.Summary.Ecount,
		}
		if resp.Summary.PublishedAt != nil {
			details.Summary.PublishedAt = resp.Summary.PublishedAt.AsTime()
		}
	}

	return details, nil
}

// CacheStats returns cache statistics.
func (c *Client) CacheStats(ctx context.Context) (*CacheStats, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.CacheStats(ctx, &gepb.CacheStatsRequest{})
	if err != nil {
		return nil, wrapError(err, "CacheStats")
	}

	return &CacheStats{
		ResultsItems: resp.ResultsItems,
		ResultsBytes: resp.ResultsBytes,
		ViewsItems:   resp.ViewsItems,
		ViewsBytes:   resp.ViewsBytes,
	}, nil
}

// ValidateGraph validates graph integrity.
func (c *Client) ValidateGraph(ctx context.Context, graph *gepb.GraphRef, deep bool) (*ValidationResult, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.ValidateGraph(ctx, &gepb.ValidateGraphRequest{
		Graph: graph,
		Deep:  deep,
	})
	if err != nil {
		return nil, wrapError(err, "ValidateGraph")
	}

	result := &ValidationResult{
		Valid:    resp.Status == nil || resp.Status.Code == 0,
		Warnings: resp.Warnings,
		Metrics:  resp.Metrics,
	}

	if resp.Status != nil {
		result.Message = resp.Status.Message
	}

	return result, nil
}

// TraceJob returns trace spans for a job.
func (c *Client) TraceJob(ctx context.Context, job *gepb.JobRef) ([]*TraceSpan, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.ops.TraceJob(ctx, &gepb.TraceJobRequest{Job: job})
	if err != nil {
		return nil, wrapError(err, "TraceJob")
	}

	spans := make([]*TraceSpan, len(resp.Spans))
	for i, s := range resp.Spans {
		spans[i] = &TraceSpan{
			Name: s.Name,
			Tags: s.Tags,
		}
		if s.Duration != nil {
			spans[i].Duration = s.Duration.AsDuration()
		}
	}

	return spans, nil
}

// ExportFormat represents the export format.
type ExportFormat int

const (
	ExportFormatEdgeList ExportFormat = iota
	ExportFormatCSV
)

// ExportGraph exports a graph in the specified format.
// Returns an io.Reader for streaming the data.
func (c *Client) ExportGraph(ctx context.Context, graph *gepb.GraphRef, format ExportFormat) (io.Reader, error) {
	var pbFormat gepb.ExportSubgraphRequest_Format
	switch format {
	case ExportFormatCSV:
		pbFormat = gepb.ExportSubgraphRequest_CSV
	default:
		pbFormat = gepb.ExportSubgraphRequest_EDGE_LIST
	}

	stream, err := c.ops.ExportSubgraph(ctx, &gepb.ExportSubgraphRequest{
		Target: &gepb.ExportSubgraphRequest_Graph{Graph: graph},
		Format: pbFormat,
	})
	if err != nil {
		return nil, wrapError(err, "ExportSubgraph")
	}

	return &exportReader{stream: stream}, nil
}

// ExportView exports a view in the specified format.
func (c *Client) ExportView(ctx context.Context, view *gepb.ViewRef, format ExportFormat) (io.Reader, error) {
	var pbFormat gepb.ExportSubgraphRequest_Format
	switch format {
	case ExportFormatCSV:
		pbFormat = gepb.ExportSubgraphRequest_CSV
	default:
		pbFormat = gepb.ExportSubgraphRequest_EDGE_LIST
	}

	stream, err := c.ops.ExportSubgraph(ctx, &gepb.ExportSubgraphRequest{
		Target: &gepb.ExportSubgraphRequest_View{View: view},
		Format: pbFormat,
	})
	if err != nil {
		return nil, wrapError(err, "ExportSubgraph")
	}

	return &exportReader{stream: stream}, nil
}

// ExportGraphToBytes exports a graph and collects all data into a byte slice.
func (c *Client) ExportGraphToBytes(ctx context.Context, graph *gepb.GraphRef, format ExportFormat) ([]byte, error) {
	reader, err := c.ExportGraph(ctx, graph, format)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	if err != nil {
		return nil, wrapError(err, "ExportSubgraph.Read")
	}

	return buf.Bytes(), nil
}

// ExportViewToBytes exports a view and collects all data into a byte slice.
func (c *Client) ExportViewToBytes(ctx context.Context, view *gepb.ViewRef, format ExportFormat) ([]byte, error) {
	reader, err := c.ExportView(ctx, view, format)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	_, err = io.Copy(&buf, reader)
	if err != nil {
		return nil, wrapError(err, "ExportSubgraph.Read")
	}

	return buf.Bytes(), nil
}

// exportReader wraps the export stream as an io.Reader.
type exportReader struct {
	stream gepb.GraphEngineOps_ExportSubgraphClient
	buf    []byte
	err    error
}

func (r *exportReader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}

	// If we have buffered data, return it first
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}

	// Read next chunk from stream
	chunk, err := r.stream.Recv()
	if err == io.EOF {
		return 0, io.EOF
	}
	if err != nil {
		r.err = wrapError(err, "ExportSubgraph.Recv")
		return 0, r.err
	}

	// Copy data to output
	n := copy(p, chunk.Data)
	if n < len(chunk.Data) {
		r.buf = chunk.Data[n:]
	}

	return n, nil
}
