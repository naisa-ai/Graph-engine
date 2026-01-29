package graphengine

import (
	"context"
	"io"
	"time"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
)

// =============================================================================
// Result Types
// =============================================================================

// PathResult represents a shortest path result.
type PathResult struct {
	Vertices  []uint64
	Edges     []uint64
	TotalCost float64
}

// ComponentsResult represents connected components result.
type ComponentsResult struct {
	Membership    []uint32
	NumComponents int
}

// DistanceMatrix represents a distance matrix result.
type DistanceMatrix struct {
	Sources   []uint64
	Targets   []uint64
	Distances [][]float64
}

// MinCutResult represents a minimum cut result.
type MinCutResult struct {
	CutValue           float64
	SourceSideVertices []uint64
	CutEdges           []uint64
}

// CorridorResult represents a corridor creation result.
type CorridorResult struct {
	View *gepb.ViewRef
	Meta map[string]string
}

// =============================================================================
// Algorithm Helper Methods
// =============================================================================

// ShortestPath finds the shortest path between two vertices.
func (c *Client) ShortestPath(ctx context.Context, graph *gepb.GraphRef, source, target uint64) (*PathResult, error) {
	return c.ShortestPathWeighted(ctx, graph, source, target, "")
}

// ShortestPathWeighted finds the shortest path with a weight column.
func (c *Client) ShortestPathWeighted(ctx context.Context, graph *gepb.GraphRef, source, target uint64, weightColumn string) (*PathResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_ShortestPath{
				ShortestPath: &gepb.ShortestPathSpec{
					SourceU64:      source,
					TargetU64:      target,
					WeightColumn:   weightColumn,
					ReturnVertices: true,
					ReturnEdges:    true,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectPathResult(ctx, result)
}

// ShortestPathOnView finds the shortest path on a view.
func (c *Client) ShortestPathOnView(ctx context.Context, view *gepb.ViewRef, source, target uint64) (*PathResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: view},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_ShortestPath{
				ShortestPath: &gepb.ShortestPathSpec{
					SourceU64:      source,
					TargetU64:      target,
					ReturnVertices: true,
					ReturnEdges:    true,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectPathResult(ctx, result)
}

// KShortestPaths finds the k shortest paths between two vertices.
func (c *Client) KShortestPaths(ctx context.Context, graph *gepb.GraphRef, source, target uint64, k int) ([]*PathResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_KShortestPaths{
				KShortestPaths: &gepb.KShortestPathsSpec{
					SourceU64: source,
					TargetU64: target,
					K:         uint32(k),
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectKSPResult(ctx, result)
}

// KShortestPathsOnView finds the k shortest paths on a view.
func (c *Client) KShortestPathsOnView(ctx context.Context, view *gepb.ViewRef, source, target uint64, k int) ([]*PathResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: view},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_KShortestPaths{
				KShortestPaths: &gepb.KShortestPathsSpec{
					SourceU64: source,
					TargetU64: target,
					K:         uint32(k),
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectKSPResult(ctx, result)
}

// Components computes connected components.
func (c *Client) Components(ctx context.Context, graph *gepb.GraphRef) (*ComponentsResult, error) {
	return c.ComponentsWithMode(ctx, graph, gepb.ComponentsSpec_WEAK)
}

// ComponentsWithMode computes connected components with specified mode.
func (c *Client) ComponentsWithMode(ctx context.Context, graph *gepb.GraphRef, mode gepb.ComponentsSpec_Mode) (*ComponentsResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Components{
				Components: &gepb.ComponentsSpec{
					Mode: mode,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectComponentsResult(ctx, result)
}

// Communities detects communities using the Louvain algorithm.
func (c *Client) Communities(ctx context.Context, graph *gepb.GraphRef) (*ComponentsResult, error) {
	return c.CommunitiesWithParams(ctx, graph, gepb.CommunitiesSpec_LOUVAIN, 1.0)
}

// CommunitiesWithParams detects communities with specified method and resolution.
func (c *Client) CommunitiesWithParams(ctx context.Context, graph *gepb.GraphRef, method gepb.CommunitiesSpec_Method, resolution float64) (*ComponentsResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Communities{
				Communities: &gepb.CommunitiesSpec{
					Method:     method,
					Resolution: resolution,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectComponentsResult(ctx, result)
}

// Distances computes distances between source and target sets.
func (c *Client) Distances(ctx context.Context, graph *gepb.GraphRef, sources, targets []uint64) (*DistanceMatrix, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Distances{
				Distances: &gepb.DistancesSpec{
					SourcesU64: sources,
					TargetsU64: targets,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectDistancesResult(ctx, result, sources, targets)
}

// MinCut computes the s-t minimum cut.
func (c *Client) MinCut(ctx context.Context, graph *gepb.GraphRef, source, target uint64) (*MinCutResult, error) {
	return c.MinCutWeighted(ctx, graph, source, target, "")
}

// MinCutWeighted computes the s-t minimum cut with a capacity column.
func (c *Client) MinCutWeighted(ctx context.Context, graph *gepb.GraphRef, source, target uint64, capacityColumn string) (*MinCutResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_StMincut{
				StMincut: &gepb.STMinCutSpec{
					SourceU64:      source,
					TargetU64:      target,
					CapacityColumn: capacityColumn,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectMinCutResult(ctx, result)
}

// MinCutOnView computes the s-t minimum cut on a view.
func (c *Client) MinCutOnView(ctx context.Context, view *gepb.ViewRef, source, target uint64) (*MinCutResult, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: view},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_StMincut{
				StMincut: &gepb.STMinCutSpec{
					SourceU64: source,
					TargetU64: target,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectMinCutResult(ctx, result)
}

// CorridorOption configures corridor creation.
type CorridorOption func(*gepb.CorridorSpec)

// WithCorridorMethod sets the corridor method.
func WithCorridorMethod(method gepb.CorridorSpec_Method) CorridorOption {
	return func(spec *gepb.CorridorSpec) {
		spec.Method = method
	}
}

// WithCorridorHops sets the expansion hops.
func WithCorridorHops(hops uint32) CorridorOption {
	return func(spec *gepb.CorridorSpec) {
		spec.Hops = hops
	}
}

// WithCorridorK sets k for KSP_HULL method.
func WithCorridorK(k uint32) CorridorOption {
	return func(spec *gepb.CorridorSpec) {
		spec.K = k
	}
}

// Corridor creates a corridor view between source and target.
func (c *Client) Corridor(ctx context.Context, graph *gepb.GraphRef, source, target uint64, opts ...CorridorOption) (*CorridorResult, error) {
	spec := &gepb.CorridorSpec{
		SourceU64: source,
		TargetU64: target,
		Method:    gepb.CorridorSpec_SHORTEST_PATH_HULL,
		Hops:      1,
	}

	for _, opt := range opts {
		opt(spec)
	}

	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Corridor{
				Corridor: spec,
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectCorridorResult(ctx, result)
}

// CorridorOnView creates a corridor view on an existing view.
func (c *Client) CorridorOnView(ctx context.Context, view *gepb.ViewRef, source, target uint64, opts ...CorridorOption) (*CorridorResult, error) {
	spec := &gepb.CorridorSpec{
		SourceU64: source,
		TargetU64: target,
		Method:    gepb.CorridorSpec_SHORTEST_PATH_HULL,
		Hops:      1,
	}

	for _, opt := range opts {
		opt(spec)
	}

	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_View{View: view},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Corridor{
				Corridor: spec,
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectCorridorResult(ctx, result)
}

// BFS performs breadth-first search from a source vertex.
func (c *Client) BFS(ctx context.Context, graph *gepb.GraphRef, source uint64, maxDepth uint32) ([]uint64, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Bfs{
				Bfs: &gepb.BFSSpec{
					SourceU64: source,
					MaxDepth:  maxDepth,
					Mode:      gepb.NeighborhoodSpec_MODE_ALL,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectU64Result(ctx, result, "vertices")
}

// Neighborhood finds the neighborhood of seed vertices.
func (c *Client) Neighborhood(ctx context.Context, graph *gepb.GraphRef, seeds []uint64, hops uint32) ([]uint64, error) {
	req := &gepb.RunRequest{
		Target: &gepb.RunRequest_Graph{Graph: graph},
		Algo: &gepb.AlgoSpec{
			Kind: &gepb.AlgoSpec_Neighborhood{
				Neighborhood: &gepb.NeighborhoodQuerySpec{
					SeedsU64: seeds,
					Hops:     hops,
					Mode:     gepb.NeighborhoodSpec_MODE_ALL,
				},
			},
		},
	}

	result, err := c.RunAndWait(ctx, req)
	if err != nil {
		return nil, err
	}

	return c.collectU64Result(ctx, result, "vertices")
}

// =============================================================================
// Result Collection Helpers
// =============================================================================

func (c *Client) collectPathResult(ctx context.Context, result *gepb.ResultRef) (*PathResult, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	path := &PathResult{}
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if sp := chunk.GetShortestPath(); sp != nil {
			path.Vertices = sp.VerticesU64
			path.Edges = sp.EdgesU64
			path.TotalCost = sp.TotalCost
		}
	}

	return path, nil
}

func (c *Client) collectKSPResult(ctx context.Context, result *gepb.ResultRef) ([]*PathResult, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	var paths []*PathResult
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if sp := chunk.GetShortestPath(); sp != nil {
			paths = append(paths, &PathResult{
				Vertices:  sp.VerticesU64,
				Edges:     sp.EdgesU64,
				TotalCost: sp.TotalCost,
			})
		}
	}

	return paths, nil
}

func (c *Client) collectComponentsResult(ctx context.Context, result *gepb.ResultRef) (*ComponentsResult, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	comp := &ComponentsResult{}
	componentSet := make(map[uint32]bool)

	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if u32 := chunk.GetU32(); u32 != nil && u32.Name == "membership" {
			comp.Membership = u32.Values
			for _, c := range u32.Values {
				componentSet[c] = true
			}
		}
	}

	comp.NumComponents = len(componentSet)
	return comp, nil
}

func (c *Client) collectDistancesResult(ctx context.Context, result *gepb.ResultRef, sources, targets []uint64) (*DistanceMatrix, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	dm := &DistanceMatrix{
		Sources: sources,
		Targets: targets,
	}

	var flatDistances []float64
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if f64 := chunk.GetF64(); f64 != nil && f64.Name == "distances" {
			flatDistances = f64.Values
		}
	}

	// Convert flat array to matrix
	if len(flatDistances) > 0 {
		dm.Distances = make([][]float64, len(sources))
		for i := range sources {
			dm.Distances[i] = make([]float64, len(targets))
			for j := range targets {
				idx := i*len(targets) + j
				if idx < len(flatDistances) {
					dm.Distances[i][j] = flatDistances[idx]
				}
			}
		}
	}

	return dm, nil
}

func (c *Client) collectMinCutResult(ctx context.Context, result *gepb.ResultRef) (*MinCutResult, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	mc := &MinCutResult{}
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if cut := chunk.GetStMincut(); cut != nil {
			mc.CutValue = cut.CutValue
			mc.SourceSideVertices = cut.SourceSideVerticesU64
			mc.CutEdges = cut.CutEdgesU64
		}
	}

	return mc, nil
}

func (c *Client) collectCorridorResult(ctx context.Context, result *gepb.ResultRef) (*CorridorResult, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	cr := &CorridorResult{}
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if corridor := chunk.GetCorridor(); corridor != nil {
			cr.View = corridor.View
			cr.Meta = corridor.Meta
		}
	}

	return cr, nil
}

func (c *Client) collectU64Result(ctx context.Context, result *gepb.ResultRef, name string) ([]uint64, error) {
	iter, err := c.GetResultStream(ctx, result)
	if err != nil {
		return nil, err
	}

	var values []uint64
	for {
		chunk, err := iter.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		if u64 := chunk.GetU64(); u64 != nil && u64.Name == name {
			values = u64.Values
		}
	}

	return values, nil
}

// =============================================================================
// Async Helpers
// =============================================================================

// RunAsync executes an algorithm and returns immediately with a job reference.
// Use WaitForJob to poll for completion.
func (c *Client) RunAsync(ctx context.Context, req *gepb.RunRequest) (*gepb.JobRef, error) {
	return c.Run(ctx, req)
}

// WaitForJobWithCallback polls for job completion, calling the callback on each poll.
func (c *Client) WaitForJobWithCallback(
	ctx context.Context,
	job *gepb.JobRef,
	pollInterval time.Duration,
	callback func(state gepb.GetJobResponse_State),
) (*gepb.ResultRef, error) {
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

			if callback != nil {
				callback(resp.State)
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
		}
	}
}
