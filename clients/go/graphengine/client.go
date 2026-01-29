package graphengine

import (
	"context"
	"io"
	"sync"
	"time"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Client provides a high-level interface to the Graph-engine service.
type Client struct {
	conn    *grpc.ClientConn
	engine  gepb.GraphEngineClient
	ops     gepb.GraphEngineOpsClient
	options *clientOptions

	mu     sync.RWMutex
	closed bool
}

// NewClient creates a new Graph-engine client.
func NewClient(addr string, opts ...Option) (*Client, error) {
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	conn, err := grpc.NewClient(addr, options.dialOptions...)
	if err != nil {
		return nil, wrapError(err, "dial")
	}

	return &Client{
		conn:    conn,
		engine:  gepb.NewGraphEngineClient(conn),
		ops:     gepb.NewGraphEngineOpsClient(conn),
		options: options,
	}, nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// context returns a context with the client's default timeout and metadata.
func (c *Client) context(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Add metadata if present
	if md := c.options.outgoingMetadata(); md != nil {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	// Add timeout if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline && c.options.timeout > 0 {
		return context.WithTimeout(ctx, c.options.timeout)
	}

	return ctx, func() {}
}

// =============================================================================
// Core Graph Operations
// =============================================================================

// BeginBuild starts a new graph build.
func (c *Client) BeginBuild(ctx context.Context, graphName string, directed bool, labels map[string]string) (string, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.BeginBuild(ctx, &gepb.BeginBuildRequest{
		GraphName: graphName,
		Directed:  directed,
		Labels:    labels,
	})
	if err != nil {
		return "", wrapError(err, "BeginBuild")
	}

	return resp.BuildId, nil
}

// PublishBuild publishes a completed build.
func (c *Client) PublishBuild(ctx context.Context, buildID string, computeComponents bool) (*gepb.GraphRef, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.PublishBuild(ctx, &gepb.PublishBuildRequest{
		BuildId: buildID,
		Artifacts: &gepb.BatchArtifacts{
			ComputeComponents: computeComponents,
		},
	})
	if err != nil {
		return nil, wrapError(err, "PublishBuild")
	}

	if resp.Status != nil && resp.Status.Code != 0 {
		return nil, &GraphEngineError{
			Message: resp.Status.Message,
		}
	}

	return resp.Graph, nil
}

// CreateView creates a view of a graph.
func (c *Client) CreateView(ctx context.Context, graph *gepb.GraphRef, spec *gepb.ViewSpec) (*gepb.ViewRef, uint64, uint64, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.CreateView(ctx, &gepb.CreateViewRequest{
		Graph: graph,
		Spec:  spec,
	})
	if err != nil {
		return nil, 0, 0, wrapError(err, "CreateView")
	}

	return resp.View, resp.Vcount, resp.Ecount, nil
}

// Run executes an algorithm and returns a job reference.
func (c *Client) Run(ctx context.Context, req *gepb.RunRequest) (*gepb.JobRef, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.Run(ctx, req)
	if err != nil {
		return nil, wrapError(err, "Run")
	}

	return resp.Job, nil
}

// GetJob gets the status of a job.
func (c *Client) GetJob(ctx context.Context, job *gepb.JobRef) (*gepb.GetJobResponse, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.GetJob(ctx, &gepb.GetJobRequest{Job: job})
	if err != nil {
		return nil, wrapError(err, "GetJob")
	}

	return resp, nil
}

// CancelJob cancels a running job.
func (c *Client) CancelJob(ctx context.Context, job *gepb.JobRef) (bool, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.CancelJob(ctx, &gepb.CancelJobRequest{Job: job})
	if err != nil {
		return false, wrapError(err, "CancelJob")
	}

	return resp.Canceled, nil
}

// Release releases a resource early.
func (c *Client) Release(ctx context.Context, req *gepb.ReleaseRequest) (bool, error) {
	ctx, cancel := c.context(ctx)
	defer cancel()

	resp, err := c.engine.Release(ctx, req)
	if err != nil {
		return false, wrapError(err, "Release")
	}

	return resp.Released, nil
}

// ReleaseView releases a view.
func (c *Client) ReleaseView(ctx context.Context, view *gepb.ViewRef) (bool, error) {
	return c.Release(ctx, &gepb.ReleaseRequest{
		Target: &gepb.ReleaseRequest_View{View: view},
	})
}

// ReleaseResult releases a result.
func (c *Client) ReleaseResult(ctx context.Context, result *gepb.ResultRef) (bool, error) {
	return c.Release(ctx, &gepb.ReleaseRequest{
		Target: &gepb.ReleaseRequest_Result{Result: result},
	})
}

// =============================================================================
// Job Utilities
// =============================================================================

// WaitForJob polls until a job completes and returns the result reference.
func (c *Client) WaitForJob(ctx context.Context, job *gepb.JobRef, pollInterval time.Duration) (*gepb.ResultRef, error) {
	if pollInterval <= 0 {
		pollInterval = 100 * time.Millisecond
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, wrapError(ctx.Err(), "WaitForJob")
		case <-ticker.C:
			resp, err := c.GetJob(ctx, job)
			if err != nil {
				return nil, err
			}

			switch resp.State {
			case gepb.GetJobResponse_SUCCEEDED:
				return resp.Result, nil
			case gepb.GetJobResponse_FAILED:
				msg := "job failed"
				if resp.Status != nil {
					msg = resp.Status.Message
				}
				return nil, &GraphEngineError{
					Message: msg,
					Cause:   ErrJobFailed,
				}
			case gepb.GetJobResponse_CANCELED:
				return nil, &GraphEngineError{
					Message: "job was canceled",
					Cause:   ErrCanceled,
				}
			}
			// PENDING or RUNNING - continue polling
		}
	}
}

// RunAndWait executes an algorithm and waits for completion.
func (c *Client) RunAndWait(ctx context.Context, req *gepb.RunRequest) (*gepb.ResultRef, error) {
	job, err := c.Run(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.WaitForJob(ctx, job, 100*time.Millisecond)
}

// =============================================================================
// Result Streaming
// =============================================================================

// ResultIterator iterates over result chunks.
type ResultIterator struct {
	stream gepb.GraphEngine_GetResultClient
	err    error
}

// GetResultStream returns an iterator for streaming result chunks.
func (c *Client) GetResultStream(ctx context.Context, result *gepb.ResultRef) (*ResultIterator, error) {
	ctx, cancel := c.context(ctx)
	_ = cancel // Note: caller should manage context

	stream, err := c.engine.GetResult(ctx, &gepb.GetResultRequest{Result: result})
	if err != nil {
		return nil, wrapError(err, "GetResult")
	}

	return &ResultIterator{stream: stream}, nil
}

// Next returns the next result chunk.
func (it *ResultIterator) Next() (*gepb.ResultChunk, error) {
	if it.err != nil {
		return nil, it.err
	}

	chunk, err := it.stream.Recv()
	if err == io.EOF {
		return nil, io.EOF
	}
	if err != nil {
		it.err = wrapError(err, "GetResult.Recv")
		return nil, it.err
	}

	return chunk, nil
}

// CollectAll collects all result chunks into a slice.
func (it *ResultIterator) CollectAll() ([]*gepb.ResultChunk, error) {
	var chunks []*gepb.ResultChunk
	for {
		chunk, err := it.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return chunks, err
		}
		chunks = append(chunks, chunk)
	}
	return chunks, nil
}

// =============================================================================
// Upload Streaming
// =============================================================================

// Uploader handles streaming uploads to a build.
type Uploader struct {
	client  *Client
	stream  gepb.GraphEngine_UploadClient
	buildID string
	err     error
}

// NewUploader creates a new uploader for a build.
func (c *Client) NewUploader(ctx context.Context, buildID string) (*Uploader, error) {
	stream, err := c.engine.Upload(ctx)
	if err != nil {
		return nil, wrapError(err, "Upload")
	}

	return &Uploader{
		client:  c,
		stream:  stream,
		buildID: buildID,
	}, nil
}

// SendVertices sends a chunk of vertices.
func (u *Uploader) SendVertices(nodeIDs []uint64) error {
	if u.err != nil {
		return u.err
	}

	err := u.stream.Send(&gepb.UploadRequest{
		BuildId: u.buildID,
		Payload: &gepb.UploadRequest_Vertices{
			Vertices: &gepb.VertexChunk{
				NodeIdU64: nodeIDs,
			},
		},
	})
	if err != nil {
		u.err = wrapError(err, "Upload.SendVertices")
	}
	return u.err
}

// SendEdges sends a chunk of edges.
func (u *Uploader) SendEdges(src, dst []uint64, weights []float32, kinds []uint32) error {
	if u.err != nil {
		return u.err
	}

	chunk := &gepb.EdgeChunk{
		SrcU64: src,
		DstU64: dst,
	}
	if len(weights) > 0 {
		chunk.Weight = weights
	}
	if len(kinds) > 0 {
		chunk.Kind = kinds
	}

	err := u.stream.Send(&gepb.UploadRequest{
		BuildId: u.buildID,
		Payload: &gepb.UploadRequest_Edges{
			Edges: chunk,
		},
	})
	if err != nil {
		u.err = wrapError(err, "Upload.SendEdges")
	}
	return u.err
}

// Close finalizes the upload and returns the response.
func (u *Uploader) Close() (*gepb.UploadResponse, error) {
	if u.err != nil {
		return nil, u.err
	}

	resp, err := u.stream.CloseAndRecv()
	if err != nil {
		return nil, wrapError(err, "Upload.Close")
	}

	return resp, nil
}

// Engine returns the underlying GraphEngine client for advanced usage.
func (c *Client) Engine() gepb.GraphEngineClient {
	return c.engine
}

// Ops returns the underlying GraphEngineOps client for advanced usage.
func (c *Client) Ops() gepb.GraphEngineOpsClient {
	return c.ops
}
