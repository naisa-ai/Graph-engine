//go:build !cgo || !igraph

// Package shim provides Go bindings to the igraph C shim layer.
// This is the stub version used when CGO is disabled or igraph is not available.
// All methods return ErrShimNotAvailable.
package shim

import (
	"errors"
	"sync"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	ErrInvalidArg       = errors.New("invalid argument")
	ErrOutOfMemory      = errors.New("out of memory")
	ErrNotFound         = errors.New("not found")
	ErrTimeout          = errors.New("timeout")
	ErrIgraph           = errors.New("igraph error")
	ErrShimClosed       = errors.New("shim object already closed")
	ErrShimNotAvailable = errors.New("igraph shim not available (built without CGO)")
)

// -----------------------------------------------------------------------------
// Mode enum
// -----------------------------------------------------------------------------

// Mode specifies direction for BFS and neighborhood traversal.
type Mode int

const (
	ModeAll Mode = iota // Both directions (undirected traversal)
	ModeOut             // Outgoing edges only
	ModeIn              // Incoming edges only
)

// -----------------------------------------------------------------------------
// Graph (stub)
// -----------------------------------------------------------------------------

// Graph wraps an igraph graph via the C shim.
// This is a stub implementation - all methods return ErrShimNotAvailable.
type Graph struct {
	mu     sync.RWMutex
	closed bool
	vcount uint32
	ecount uint64
}

// NewGraph creates a new Graph from edge lists.
// STUB: Returns ErrShimNotAvailable.
func NewGraph(n uint32, src, dst []uint32, directed bool) (*Graph, error) {
	return nil, ErrShimNotAvailable
}

// Close releases the graph resources.
func (g *Graph) Close() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
}

// VCount returns the number of vertices.
func (g *Graph) VCount() uint32 {
	if g == nil {
		return 0
	}
	return g.vcount
}

// ECount returns the number of edges.
func (g *Graph) ECount() uint64 {
	if g == nil {
		return 0
	}
	return g.ecount
}

// MemoryBytes returns estimated memory usage.
func (g *Graph) MemoryBytes() uint64 {
	return 0
}

// -----------------------------------------------------------------------------
// View (stub)
// -----------------------------------------------------------------------------

// View wraps an igraph subgraph view.
type View struct {
	mu     sync.RWMutex
	closed bool
}

// NewViewFromEdgeMask creates a view from an edge mask.
// STUB: Returns ErrShimNotAvailable.
func (g *Graph) NewViewFromEdgeMask(edgeMask []byte) (*View, error) {
	return nil, ErrShimNotAvailable
}

// Close releases the view resources.
func (v *View) Close() {
	if v == nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
}

// MemoryBytes returns estimated memory usage.
func (v *View) MemoryBytes() uint64 {
	return 0
}

// -----------------------------------------------------------------------------
// Result types (stubs)
// -----------------------------------------------------------------------------

// ComponentsResult contains the result of connected components computation.
type ComponentsResult struct {
	Membership    []uint32
	NumComponents uint32
}

// ShortestPathResult contains the result of shortest path computation.
type ShortestPathResult struct {
	PathVertices []uint32 // Vertex indices in the path
	PathEdges    []uint32 // Edge indices in the path
	TotalCost    float64  // Total path cost
	Found        bool     // Whether a path was found
}

// KShortestPathsResult contains the result of k-shortest paths computation.
type KShortestPathsResult struct {
	NumPaths uint32
	Paths    [][]uint32
	Costs    []float64
}

// MinCutResult contains the result of min-cut computation.
type MinCutResult struct {
	CutValue   float64
	CutEdges   []uint32
	SourceSide []uint32
	TargetSide []uint32
}

// BFSResult contains the result of BFS traversal.
type BFSResult struct {
	Visited []uint32
	Depths  []uint32
	Parents []uint32
}

// NeighborhoodResult contains the result of neighborhood query.
type NeighborhoodResult struct {
	Vertices  []uint32
	Distances []uint32
}

// CommunitiesResult contains the result of community detection.
type CommunitiesResult struct {
	Membership     []uint32
	Modularity     float64
	NumCommunities uint32
}

// Result wraps a C result object.
// This is a stub implementation.
type Result struct {
	closed bool
}

// Close releases the result resources.
func (r *Result) Close() {
	if r == nil {
		return
	}
	r.closed = true
}

// GetU32 gets a uint32 array from the result.
func (r *Result) GetU32(name string) ([]uint32, error) {
	return nil, ErrShimNotAvailable
}

// GetF64 gets a float64 array from the result.
func (r *Result) GetF64(name string) ([]float64, error) {
	return nil, ErrShimNotAvailable
}

// GetBytes gets a byte array from the result.
func (r *Result) GetBytes(name string) ([]byte, error) {
	return nil, ErrShimNotAvailable
}

// MemoryBytes returns the memory usage of the result.
func (r *Result) MemoryBytes() uint64 {
	return 0
}

// -----------------------------------------------------------------------------
// Algorithm methods (all return ErrShimNotAvailable)
// -----------------------------------------------------------------------------

// Components computes connected components.
func (g *Graph) Components(weak bool) (*ComponentsResult, error) {
	return nil, ErrShimNotAvailable
}

// ComponentsOnView computes connected components on a view.
func (g *Graph) ComponentsOnView(v *View, weak bool) (*ComponentsResult, error) {
	return nil, ErrShimNotAvailable
}

// ShortestPath computes shortest path between two vertices.
func (g *Graph) ShortestPath(src, dst uint32, weights []float64, returnVertices, returnEdges bool) (*ShortestPathResult, error) {
	return nil, ErrShimNotAvailable
}

// ShortestPathOnView computes shortest path on a view.
func (g *Graph) ShortestPathOnView(v *View, src, dst uint32, weights []float64, returnVertices, returnEdges bool) (*ShortestPathResult, error) {
	return nil, ErrShimNotAvailable
}

// KShortestPaths computes k shortest paths.
func (g *Graph) KShortestPaths(src, dst, k uint32, weights []float64, maxCandidates uint32) (*KShortestPathsResult, error) {
	return nil, ErrShimNotAvailable
}

// KShortestPathsOnView computes k shortest paths on a view.
func (g *Graph) KShortestPathsOnView(v *View, src, dst, k uint32, weights []float64, maxCandidates uint32) (*KShortestPathsResult, error) {
	return nil, ErrShimNotAvailable
}

// STMinCut computes minimum s-t cut.
func (g *Graph) STMinCut(src, dst uint32, weights []float64) (*MinCutResult, error) {
	return nil, ErrShimNotAvailable
}

// STMinCutOnView computes minimum s-t cut on a view.
func (g *Graph) STMinCutOnView(v *View, src, dst uint32, weights []float64) (*MinCutResult, error) {
	return nil, ErrShimNotAvailable
}

// BFS performs breadth-first search.
func (g *Graph) BFS(src uint32, maxDepth uint32, mode Mode) (*BFSResult, error) {
	return nil, ErrShimNotAvailable
}

// BFSOnView performs BFS on a view.
func (g *Graph) BFSOnView(v *View, src uint32, maxDepth uint32, mode Mode) (*BFSResult, error) {
	return nil, ErrShimNotAvailable
}

// Neighborhood finds vertices within hops of seeds.
func (g *Graph) Neighborhood(seeds []uint32, hops uint32, mode Mode) (*NeighborhoodResult, error) {
	return nil, ErrShimNotAvailable
}

// NeighborhoodOnView finds neighborhood on a view.
func (g *Graph) NeighborhoodOnView(v *View, seeds []uint32, hops uint32, mode Mode) (*NeighborhoodResult, error) {
	return nil, ErrShimNotAvailable
}

// CommunitiesLeiden detects communities using Leiden algorithm.
func (g *Graph) CommunitiesLeiden(resolution float64) (*CommunitiesResult, error) {
	return nil, ErrShimNotAvailable
}

// CommunitiesLeidenOnView detects communities on a view using Leiden.
func (g *Graph) CommunitiesLeidenOnView(v *View, resolution float64) (*CommunitiesResult, error) {
	return nil, ErrShimNotAvailable
}

// CommunitiesLouvain detects communities using Louvain algorithm.
func (g *Graph) CommunitiesLouvain(resolution float64) (*CommunitiesResult, error) {
	return nil, ErrShimNotAvailable
}

// CommunitiesLouvainOnView detects communities on a view using Louvain.
func (g *Graph) CommunitiesLouvainOnView(v *View, resolution float64) (*CommunitiesResult, error) {
	return nil, ErrShimNotAvailable
}

// -----------------------------------------------------------------------------
// Version info (stubs)
// -----------------------------------------------------------------------------

// ShimVersion returns the version of the C shim library.
func ShimVersion() string {
	return "stub (no igraph)"
}

// IgraphVersion returns the version of the igraph library.
func IgraphVersion() string {
	return "not available"
}

// IsAvailable returns true if the igraph shim is available.
func IsAvailable() bool {
	return false
}
