// SPDX-License-Identifier: MIT
// Copyright (c) 2025 Naisa AI, Inc.

package graphengine

import (
	"context"
	"fmt"

	gepb "github.com/naisa-ai/graph-engine/clients/go/gen/graphengine/v1"
)

// GraphBuilder provides a fluent API for building and uploading graphs.
type GraphBuilder struct {
	client    *Client
	graphName string
	directed  bool
	labels    map[string]string

	vertices []uint64
	edges    []edge

	chunkSize int
	err       error
}

type edge struct {
	src, dst uint64
	weight   float32
	kind     uint32
}

// NewGraphBuilder creates a new graph builder.
func NewGraphBuilder(client *Client, graphName string) *GraphBuilder {
	return &GraphBuilder{
		client:    client,
		graphName: graphName,
		directed:  false,
		labels:    make(map[string]string),
		chunkSize: 10000,
	}
}

// Directed sets whether the graph is directed.
func (b *GraphBuilder) Directed(directed bool) *GraphBuilder {
	b.directed = directed
	return b
}

// WithLabels adds labels to the graph.
func (b *GraphBuilder) WithLabels(labels map[string]string) *GraphBuilder {
	for k, v := range labels {
		b.labels[k] = v
	}
	return b
}

// WithLabel adds a single label.
func (b *GraphBuilder) WithLabel(key, value string) *GraphBuilder {
	b.labels[key] = value
	return b
}

// ChunkSize sets the upload chunk size.
func (b *GraphBuilder) ChunkSize(size int) *GraphBuilder {
	if size > 0 {
		b.chunkSize = size
	}
	return b
}

// AddVertices adds vertices to the graph.
func (b *GraphBuilder) AddVertices(nodeIDs []uint64) *GraphBuilder {
	b.vertices = append(b.vertices, nodeIDs...)
	return b
}

// AddVertex adds a single vertex.
func (b *GraphBuilder) AddVertex(nodeID uint64) *GraphBuilder {
	b.vertices = append(b.vertices, nodeID)
	return b
}

// AddEdges adds edges to the graph (without weights).
func (b *GraphBuilder) AddEdges(src, dst []uint64) *GraphBuilder {
	if len(src) != len(dst) {
		b.err = fmt.Errorf("src and dst must have same length")
		return b
	}

	for i := range src {
		b.edges = append(b.edges, edge{src: src[i], dst: dst[i]})
	}
	return b
}

// AddEdge adds a single edge.
func (b *GraphBuilder) AddEdge(src, dst uint64) *GraphBuilder {
	b.edges = append(b.edges, edge{src: src, dst: dst})
	return b
}

// AddWeightedEdges adds edges with weights.
func (b *GraphBuilder) AddWeightedEdges(src, dst []uint64, weights []float32) *GraphBuilder {
	if len(src) != len(dst) || len(src) != len(weights) {
		b.err = fmt.Errorf("src, dst, and weights must have same length")
		return b
	}

	for i := range src {
		b.edges = append(b.edges, edge{src: src[i], dst: dst[i], weight: weights[i]})
	}
	return b
}

// AddWeightedEdge adds a single weighted edge.
func (b *GraphBuilder) AddWeightedEdge(src, dst uint64, weight float32) *GraphBuilder {
	b.edges = append(b.edges, edge{src: src, dst: dst, weight: weight})
	return b
}

// AddTypedEdges adds edges with kinds/types.
func (b *GraphBuilder) AddTypedEdges(src, dst []uint64, kinds []uint32) *GraphBuilder {
	if len(src) != len(dst) || len(src) != len(kinds) {
		b.err = fmt.Errorf("src, dst, and kinds must have same length")
		return b
	}

	for i := range src {
		b.edges = append(b.edges, edge{src: src[i], dst: dst[i], kind: kinds[i]})
	}
	return b
}

// AddFullEdges adds edges with both weights and kinds.
func (b *GraphBuilder) AddFullEdges(src, dst []uint64, weights []float32, kinds []uint32) *GraphBuilder {
	if len(src) != len(dst) {
		b.err = fmt.Errorf("src and dst must have same length")
		return b
	}
	if len(weights) > 0 && len(weights) != len(src) {
		b.err = fmt.Errorf("weights must match edge count or be empty")
		return b
	}
	if len(kinds) > 0 && len(kinds) != len(src) {
		b.err = fmt.Errorf("kinds must match edge count or be empty")
		return b
	}

	for i := range src {
		e := edge{src: src[i], dst: dst[i]}
		if len(weights) > 0 {
			e.weight = weights[i]
		}
		if len(kinds) > 0 {
			e.kind = kinds[i]
		}
		b.edges = append(b.edges, e)
	}
	return b
}

// Build uploads the graph but does not publish it.
// Returns the build ID.
func (b *GraphBuilder) Build(ctx context.Context) (string, error) {
	if b.err != nil {
		return "", b.err
	}

	// Begin build
	buildID, err := b.client.BeginBuild(ctx, b.graphName, b.directed, b.labels)
	if err != nil {
		return "", err
	}

	// Upload data
	uploader, err := b.client.NewUploader(ctx, buildID)
	if err != nil {
		return "", err
	}

	// Upload vertices in chunks
	for i := 0; i < len(b.vertices); i += b.chunkSize {
		end := i + b.chunkSize
		if end > len(b.vertices) {
			end = len(b.vertices)
		}
		if err := uploader.SendVertices(b.vertices[i:end]); err != nil {
			return "", err
		}
	}

	// Upload edges in chunks
	for i := 0; i < len(b.edges); i += b.chunkSize {
		end := i + b.chunkSize
		if end > len(b.edges) {
			end = len(b.edges)
		}

		chunk := b.edges[i:end]
		src := make([]uint64, len(chunk))
		dst := make([]uint64, len(chunk))
		weights := make([]float32, len(chunk))
		kinds := make([]uint32, len(chunk))
		hasWeights := false
		hasKinds := false

		for j, e := range chunk {
			src[j] = e.src
			dst[j] = e.dst
			weights[j] = e.weight
			kinds[j] = e.kind
			if e.weight != 0 {
				hasWeights = true
			}
			if e.kind != 0 {
				hasKinds = true
			}
		}

		var w []float32
		var k []uint32
		if hasWeights {
			w = weights
		}
		if hasKinds {
			k = kinds
		}

		if err := uploader.SendEdges(src, dst, w, k); err != nil {
			return "", err
		}
	}

	// Close upload
	_, err = uploader.Close()
	if err != nil {
		return "", err
	}

	return buildID, nil
}

// Publish builds and publishes the graph.
// Returns the graph reference.
func (b *GraphBuilder) Publish(ctx context.Context) (*gepb.GraphRef, error) {
	return b.PublishWithArtifacts(ctx, true)
}

// PublishWithArtifacts builds and publishes with optional artifact computation.
func (b *GraphBuilder) PublishWithArtifacts(ctx context.Context, computeComponents bool) (*gepb.GraphRef, error) {
	buildID, err := b.Build(ctx)
	if err != nil {
		return nil, err
	}

	return b.client.PublishBuild(ctx, buildID, computeComponents)
}

// VertexCount returns the number of vertices added.
func (b *GraphBuilder) VertexCount() int {
	return len(b.vertices)
}

// EdgeCount returns the number of edges added.
func (b *GraphBuilder) EdgeCount() int {
	return len(b.edges)
}

// ViewBuilder provides a fluent API for creating views.
type ViewBuilder struct {
	client *Client
	graph  *gepb.GraphRef
	spec   *gepb.ViewSpec
}

// NewViewBuilder creates a new view builder.
func NewViewBuilder(client *Client, graph *gepb.GraphRef) *ViewBuilder {
	return &ViewBuilder{
		client: client,
		graph:  graph,
		spec:   &gepb.ViewSpec{},
	}
}

// InduceVertices sets the induced vertex set.
func (vb *ViewBuilder) InduceVertices(nodeIDs []uint64) *ViewBuilder {
	vb.spec.InduceVerticesU64 = nodeIDs
	return vb
}

// ExcludeVertices sets vertices to exclude.
func (vb *ViewBuilder) ExcludeVertices(nodeIDs []uint64) *ViewBuilder {
	vb.spec.ExcludeVerticesU64 = nodeIDs
	return vb
}

// ExcludeEdges sets edges to exclude.
func (vb *ViewBuilder) ExcludeEdges(edgeIDs []uint64) *ViewBuilder {
	vb.spec.ExcludeEdgesU64 = edgeIDs
	return vb
}

// Neighborhood adds a neighborhood expansion.
func (vb *ViewBuilder) Neighborhood(seeds []uint64, hops uint32) *ViewBuilder {
	vb.spec.Neighborhood = &gepb.NeighborhoodSpec{
		SeedsU64: seeds,
		Hops:     hops,
		Mode:     gepb.NeighborhoodSpec_MODE_ALL,
	}
	return vb
}

// NeighborhoodOut adds an outward neighborhood expansion.
func (vb *ViewBuilder) NeighborhoodOut(seeds []uint64, hops uint32) *ViewBuilder {
	vb.spec.Neighborhood = &gepb.NeighborhoodSpec{
		SeedsU64: seeds,
		Hops:     hops,
		Mode:     gepb.NeighborhoodSpec_MODE_OUT,
	}
	return vb
}

// NeighborhoodIn adds an inward neighborhood expansion.
func (vb *ViewBuilder) NeighborhoodIn(seeds []uint64, hops uint32) *ViewBuilder {
	vb.spec.Neighborhood = &gepb.NeighborhoodSpec{
		SeedsU64: seeds,
		Hops:     hops,
		Mode:     gepb.NeighborhoodSpec_MODE_IN,
	}
	return vb
}

// Create creates the view.
func (vb *ViewBuilder) Create(ctx context.Context) (*gepb.ViewRef, uint64, uint64, error) {
	return vb.client.CreateView(ctx, vb.graph, vb.spec)
}
