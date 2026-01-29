//go:build cgo && igraph

// Package shim provides Go bindings to the igraph C shim layer.
// This package isolates cgo code and provides a clean Go API for graph algorithms.
//
// Design: Section 5.4 of graph-engine-design.md
package shim

/*
#cgo pkg-config: igraph
#cgo CFLAGS: -I${SRCDIR}/cgo
#cgo LDFLAGS: -L${SRCDIR}/cgo -lm

#include "cgo/ge_igraph_shim.h"
#include <stdlib.h>
#include <string.h>
*/
import "C"

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

var (
	ErrInvalidArg  = errors.New("invalid argument")
	ErrOutOfMemory = errors.New("out of memory")
	ErrNotFound    = errors.New("not found")
	ErrTimeout     = errors.New("timeout")
	ErrIgraph      = errors.New("igraph error")
	ErrShimClosed  = errors.New("shim object already closed")
)

func statusToError(status C.ge_status_t) error {
	switch status {
	case C.GE_OK:
		return nil
	case C.GE_ERR_INVALID_ARG:
		return ErrInvalidArg
	case C.GE_ERR_OUT_OF_MEMORY:
		return ErrOutOfMemory
	case C.GE_ERR_NOT_FOUND:
		return ErrNotFound
	case C.GE_ERR_TIMEOUT:
		return ErrTimeout
	case C.GE_ERR_IGRAPH:
		if msg := C.ge_last_error(); msg != nil {
			return fmt.Errorf("%w: %s", ErrIgraph, C.GoString(msg))
		}
		return ErrIgraph
	default:
		if msg := C.ge_last_error(); msg != nil {
			return fmt.Errorf("shim error %d: %s", status, C.GoString(msg))
		}
		return fmt.Errorf("shim error %d", status)
	}
}

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

func (m Mode) toC() C.ge_mode_t {
	switch m {
	case ModeOut:
		return C.GE_MODE_OUT
	case ModeIn:
		return C.GE_MODE_IN
	default:
		return C.GE_MODE_ALL
	}
}

// -----------------------------------------------------------------------------
// Graph
// -----------------------------------------------------------------------------

// Graph wraps an igraph graph via the C shim.
// Thread-safe for concurrent read operations.
type Graph struct {
	ptr    *C.ge_graph_t
	mu     sync.RWMutex
	closed bool

	// Cached properties
	vcount uint32
	ecount uint64
}

// NewGraph creates a new graph from an edge list.
// The graph takes ownership of the data; the input slices can be modified after this call.
// src and dst must have the same length (the number of edges).
// n is the number of vertices (vertices are numbered 0 to n-1).
func NewGraph(n uint32, src, dst []uint32, directed bool) (*Graph, error) {
	if len(src) != len(dst) {
		return nil, fmt.Errorf("%w: src/dst length mismatch", ErrInvalidArg)
	}

	m := len(src)

	// Allocate C arrays and copy data
	var cSrc, cDst *C.uint32_t
	if m > 0 {
		cSrc = (*C.uint32_t)(C.malloc(C.size_t(m) * C.size_t(unsafe.Sizeof(C.uint32_t(0)))))
		cDst = (*C.uint32_t)(C.malloc(C.size_t(m) * C.size_t(unsafe.Sizeof(C.uint32_t(0)))))
		if cSrc == nil || cDst == nil {
			C.free(unsafe.Pointer(cSrc))
			C.free(unsafe.Pointer(cDst))
			return nil, ErrOutOfMemory
		}
		defer C.free(unsafe.Pointer(cSrc))
		defer C.free(unsafe.Pointer(cDst))

		// Copy Go slices to C arrays
		srcSlice := unsafe.Slice(cSrc, m)
		dstSlice := unsafe.Slice(cDst, m)
		for i := 0; i < m; i++ {
			srcSlice[i] = C.uint32_t(src[i])
			dstSlice[i] = C.uint32_t(dst[i])
		}
	}

	directedInt := 0
	if directed {
		directedInt = 1
	}

	edges := C.ge_edge_list_t{
		n_vertices: C.uint32_t(n),
		src:        cSrc,
		dst:        cDst,
		m_edges:    C.size_t(m),
		directed:   C.int(directedInt),
	}

	var out *C.ge_graph_t
	status := C.ge_graph_create(&edges, &out)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	return &Graph{
		ptr:    out,
		vcount: n,
		ecount: uint64(m),
	}, nil
}

// Close releases the graph resources.
// Safe to call multiple times.
func (g *Graph) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.closed || g.ptr == nil {
		return
	}
	C.ge_graph_destroy(g.ptr)
	g.ptr = nil
	g.closed = true
}

// VCount returns the number of vertices.
func (g *Graph) VCount() uint32 {
	return g.vcount
}

// ECount returns the number of edges.
func (g *Graph) ECount() uint64 {
	return g.ecount
}

// MemoryBytes returns the estimated memory usage of the graph in bytes.
func (g *Graph) MemoryBytes() uint64 {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return 0
	}
	return uint64(C.ge_graph_memory_bytes(g.ptr))
}

// IsClosed returns true if the graph has been closed.
func (g *Graph) IsClosed() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.closed
}

// -----------------------------------------------------------------------------
// View
// -----------------------------------------------------------------------------

// View wraps a subgraph view via the C shim.
type View struct {
	ptr    *C.ge_view_t
	mu     sync.RWMutex
	closed bool
}

// NewViewFromEdgeMask creates a view by including only edges where the corresponding bit is set.
// edgeMask is a bitset where bit i indicates edge i is included.
func (g *Graph) NewViewFromEdgeMask(edgeMask []byte) (*View, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var maskPtr *C.uint8_t
	if len(edgeMask) > 0 {
		maskPtr = (*C.uint8_t)(unsafe.Pointer(&edgeMask[0]))
	}

	var out *C.ge_view_t
	status := C.ge_view_create_by_edge_mask(
		g.ptr,
		maskPtr,
		C.size_t(len(edgeMask)),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	return &View{ptr: out}, nil
}

// Close releases the view resources.
func (v *View) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed || v.ptr == nil {
		return
	}
	C.ge_view_destroy(v.ptr)
	v.ptr = nil
	v.closed = true
}

// MemoryBytes returns the estimated memory usage of the view in bytes.
func (v *View) MemoryBytes() uint64 {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return 0
	}
	return uint64(C.ge_view_memory_bytes(v.ptr))
}

// -----------------------------------------------------------------------------
// Result extraction helpers
// -----------------------------------------------------------------------------

// Result wraps a C result containing named buffers.
type Result struct {
	ptr *C.ge_result_t
}

// Close releases the result resources.
func (r *Result) Close() {
	if r.ptr != nil {
		C.ge_result_destroy(r.ptr)
		r.ptr = nil
	}
}

// MemoryBytes returns the estimated memory usage of the result.
func (r *Result) MemoryBytes() uint64 {
	if r.ptr == nil {
		return 0
	}
	return uint64(C.ge_result_memory_bytes(r.ptr))
}

// GetU32 retrieves a uint32 buffer by name.
func (r *Result) GetU32(name string) ([]uint32, error) {
	if r.ptr == nil {
		return nil, ErrShimClosed
	}

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var data *C.uint32_t
	var length C.size_t

	status := C.ge_result_get_u32(r.ptr, cName, &data, &length)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	if length == 0 {
		return []uint32{}, nil
	}

	// Copy data to Go slice
	result := make([]uint32, int(length))
	cSlice := unsafe.Slice(data, int(length))
	for i := range result {
		result[i] = uint32(cSlice[i])
	}

	return result, nil
}

// GetF64 retrieves a float64 buffer by name.
func (r *Result) GetF64(name string) ([]float64, error) {
	if r.ptr == nil {
		return nil, ErrShimClosed
	}

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var data *C.double
	var length C.size_t

	status := C.ge_result_get_f64(r.ptr, cName, &data, &length)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	if length == 0 {
		return []float64{}, nil
	}

	// Copy data to Go slice
	result := make([]float64, int(length))
	cSlice := unsafe.Slice(data, int(length))
	for i := range result {
		result[i] = float64(cSlice[i])
	}

	return result, nil
}

// GetBytes retrieves a bytes buffer by name.
func (r *Result) GetBytes(name string) ([]byte, error) {
	if r.ptr == nil {
		return nil, ErrShimClosed
	}

	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var data *C.uint8_t
	var length C.size_t

	status := C.ge_result_get_bytes(r.ptr, cName, &data, &length)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	if length == 0 {
		return []byte{}, nil
	}

	// Copy data to Go slice
	return C.GoBytes(unsafe.Pointer(data), C.int(length)), nil
}

// -----------------------------------------------------------------------------
// Algorithm: Connected Components
// -----------------------------------------------------------------------------

// ComponentsResult contains the result of a components computation.
type ComponentsResult struct {
	Membership    []uint32 // Component ID for each vertex
	NumComponents uint32
}

// Components computes connected components.
// If weak is true, computes weakly connected components (ignores edge direction).
// If weak is false, computes strongly connected components.
func (g *Graph) Components(weak bool) (*ComponentsResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	weakInt := 0
	if weak {
		weakInt = 1
	}

	var out *C.ge_result_t
	status := C.ge_run_components(g.ptr, C.int(weakInt), &out)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	numComponents, err := result.GetU32("num_components")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_components: %w", err)
	}

	return &ComponentsResult{
		Membership:    membership,
		NumComponents: numComponents[0],
	}, nil
}

// ComponentsOnView computes connected components on a view.
func (g *Graph) ComponentsOnView(v *View, weak bool) (*ComponentsResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	weakInt := 0
	if weak {
		weakInt = 1
	}

	var out *C.ge_result_t
	status := C.ge_run_components_view(g.ptr, v.ptr, C.int(weakInt), &out)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	numComponents, err := result.GetU32("num_components")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_components: %w", err)
	}

	return &ComponentsResult{
		Membership:    membership,
		NumComponents: numComponents[0],
	}, nil
}

// -----------------------------------------------------------------------------
// Algorithm: Shortest Path
// -----------------------------------------------------------------------------

// ShortestPathResult contains the result of a shortest path computation.
type ShortestPathResult struct {
	PathVertices []uint32 // Vertex indices in the path (empty if not requested or no path)
	PathEdges    []uint32 // Edge indices in the path (empty if not requested or no path)
	TotalCost    float64  // Total path cost
	Found        bool     // Whether a path was found
}

// ShortestPath computes the shortest path between two vertices.
// If weights is nil, computes unweighted shortest path (hop count).
// Otherwise, weights should have length equal to the number of edges.
func (g *Graph) ShortestPath(src, dst uint32, weights []float64, returnVertices, returnEdges bool) (*ShortestPathResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var weightsPtr *C.double
	if len(weights) > 0 {
		weightsPtr = (*C.double)(unsafe.Pointer(&weights[0]))
	}

	returnVerticesInt := 0
	if returnVertices {
		returnVerticesInt = 1
	}
	returnEdgesInt := 0
	if returnEdges {
		returnEdgesInt = 1
	}

	var out *C.ge_result_t
	status := C.ge_run_shortest_path(
		g.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		weightsPtr,
		C.int(returnVerticesInt),
		C.int(returnEdgesInt),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	found, err := result.GetU32("found")
	if err != nil {
		return nil, fmt.Errorf("failed to get found: %w", err)
	}

	totalCost, err := result.GetF64("total_cost")
	if err != nil {
		return nil, fmt.Errorf("failed to get total_cost: %w", err)
	}

	spResult := &ShortestPathResult{
		TotalCost: totalCost[0],
		Found:     found[0] == 1,
	}

	if spResult.Found {
		if returnVertices {
			spResult.PathVertices, _ = result.GetU32("path_vertices")
		}
		if returnEdges {
			spResult.PathEdges, _ = result.GetU32("path_edges")
		}
	}

	return spResult, nil
}

// ShortestPathOnView computes shortest path on a view.
func (g *Graph) ShortestPathOnView(v *View, src, dst uint32, weights []float64, returnVertices, returnEdges bool) (*ShortestPathResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var weightsPtr *C.double
	if len(weights) > 0 {
		weightsPtr = (*C.double)(unsafe.Pointer(&weights[0]))
	}

	returnVerticesInt := 0
	if returnVertices {
		returnVerticesInt = 1
	}
	returnEdgesInt := 0
	if returnEdges {
		returnEdgesInt = 1
	}

	var out *C.ge_result_t
	status := C.ge_run_shortest_path_view(
		g.ptr,
		v.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		weightsPtr,
		C.int(returnVerticesInt),
		C.int(returnEdgesInt),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	found, err := result.GetU32("found")
	if err != nil {
		return nil, fmt.Errorf("failed to get found: %w", err)
	}

	totalCost, err := result.GetF64("total_cost")
	if err != nil {
		return nil, fmt.Errorf("failed to get total_cost: %w", err)
	}

	spResult := &ShortestPathResult{
		TotalCost: totalCost[0],
		Found:     found[0] == 1,
	}

	if spResult.Found {
		if returnVertices {
			spResult.PathVertices, _ = result.GetU32("path_vertices")
		}
		if returnEdges {
			spResult.PathEdges, _ = result.GetU32("path_edges")
		}
	}

	return spResult, nil
}

// -----------------------------------------------------------------------------
// Algorithm: K-Shortest Paths
// -----------------------------------------------------------------------------

// KSPResult contains the result of a k-shortest paths computation.
type KSPResult struct {
	Paths    [][]uint32 // List of paths, each path is a list of vertex indices
	Costs    []float64  // Cost of each path
	NumPaths uint32     // Number of paths found (may be < k)
}

// KShortestPaths computes the k shortest paths between two vertices using Yen's algorithm.
// If weights is nil, computes unweighted shortest paths.
// maxCandidates limits the search space (0 = no limit).
func (g *Graph) KShortestPaths(src, dst uint32, k uint32, weights []float64, maxCandidates uint32) (*KSPResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var weightsPtr *C.double
	if len(weights) > 0 {
		weightsPtr = (*C.double)(unsafe.Pointer(&weights[0]))
	}

	var out *C.ge_result_t
	status := C.ge_run_ksp(
		g.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		weightsPtr,
		C.uint32_t(k),
		C.uint32_t(maxCandidates),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	numPaths, err := result.GetU32("num_paths")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_paths: %w", err)
	}

	offsets, err := result.GetU32("path_offsets")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_offsets: %w", err)
	}

	vertices, err := result.GetU32("path_vertices")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_vertices: %w", err)
	}

	costs, err := result.GetF64("path_costs")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_costs: %w", err)
	}

	// Decode ragged array
	paths := make([][]uint32, numPaths[0])
	for i := uint32(0); i < numPaths[0]; i++ {
		start := offsets[i]
		end := offsets[i+1]
		paths[i] = make([]uint32, end-start)
		copy(paths[i], vertices[start:end])
	}

	return &KSPResult{
		Paths:    paths,
		Costs:    costs,
		NumPaths: numPaths[0],
	}, nil
}

// KShortestPathsOnView computes k-shortest paths on a view.
func (g *Graph) KShortestPathsOnView(v *View, src, dst uint32, k uint32, weights []float64, maxCandidates uint32) (*KSPResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var weightsPtr *C.double
	if len(weights) > 0 {
		weightsPtr = (*C.double)(unsafe.Pointer(&weights[0]))
	}

	var out *C.ge_result_t
	status := C.ge_run_ksp_view(
		g.ptr,
		v.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		weightsPtr,
		C.uint32_t(k),
		C.uint32_t(maxCandidates),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	numPaths, err := result.GetU32("num_paths")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_paths: %w", err)
	}

	offsets, err := result.GetU32("path_offsets")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_offsets: %w", err)
	}

	vertices, err := result.GetU32("path_vertices")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_vertices: %w", err)
	}

	costs, err := result.GetF64("path_costs")
	if err != nil {
		return nil, fmt.Errorf("failed to get path_costs: %w", err)
	}

	// Decode ragged array
	paths := make([][]uint32, numPaths[0])
	for i := uint32(0); i < numPaths[0]; i++ {
		start := offsets[i]
		end := offsets[i+1]
		paths[i] = make([]uint32, end-start)
		copy(paths[i], vertices[start:end])
	}

	return &KSPResult{
		Paths:    paths,
		Costs:    costs,
		NumPaths: numPaths[0],
	}, nil
}

// -----------------------------------------------------------------------------
// Algorithm: s-t Minimum Cut
// -----------------------------------------------------------------------------

// MinCutResult contains the result of an s-t minimum cut computation.
type MinCutResult struct {
	CutValue   float64  // Total capacity of edges in the cut
	SourceSide []uint32 // Vertices on the source side of the cut
	CutEdges   []uint32 // Edge indices in the cut
}

// STMinCut computes the minimum s-t cut.
// If capacity is nil, all edges have capacity 1.
func (g *Graph) STMinCut(src, dst uint32, capacity []float64) (*MinCutResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var capacityPtr *C.double
	if len(capacity) > 0 {
		capacityPtr = (*C.double)(unsafe.Pointer(&capacity[0]))
	}

	var out *C.ge_result_t
	status := C.ge_run_st_mincut(
		g.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		capacityPtr,
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	cutValue, err := result.GetF64("cut_value")
	if err != nil {
		return nil, fmt.Errorf("failed to get cut_value: %w", err)
	}

	sourceSide, err := result.GetU32("source_side")
	if err != nil {
		return nil, fmt.Errorf("failed to get source_side: %w", err)
	}

	cutEdges, err := result.GetU32("cut_edges")
	if err != nil {
		return nil, fmt.Errorf("failed to get cut_edges: %w", err)
	}

	return &MinCutResult{
		CutValue:   cutValue[0],
		SourceSide: sourceSide,
		CutEdges:   cutEdges,
	}, nil
}

// STMinCutOnView computes minimum s-t cut on a view.
func (g *Graph) STMinCutOnView(v *View, src, dst uint32, capacity []float64) (*MinCutResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var capacityPtr *C.double
	if len(capacity) > 0 {
		capacityPtr = (*C.double)(unsafe.Pointer(&capacity[0]))
	}

	var out *C.ge_result_t
	status := C.ge_run_st_mincut_view(
		g.ptr,
		v.ptr,
		C.uint32_t(src),
		C.uint32_t(dst),
		capacityPtr,
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	cutValue, err := result.GetF64("cut_value")
	if err != nil {
		return nil, fmt.Errorf("failed to get cut_value: %w", err)
	}

	sourceSide, err := result.GetU32("source_side")
	if err != nil {
		return nil, fmt.Errorf("failed to get source_side: %w", err)
	}

	cutEdges, err := result.GetU32("cut_edges")
	if err != nil {
		return nil, fmt.Errorf("failed to get cut_edges: %w", err)
	}

	return &MinCutResult{
		CutValue:   cutValue[0],
		SourceSide: sourceSide,
		CutEdges:   cutEdges,
	}, nil
}

// -----------------------------------------------------------------------------
// Algorithm: BFS
// -----------------------------------------------------------------------------

// BFSResult contains the result of a BFS traversal.
type BFSResult struct {
	Visited []uint32 // Vertices in BFS order
	Depths  []uint32 // Depth of each visited vertex
	Parents []uint32 // Parent of each visited vertex (UINT32_MAX for source)
}

// BFS performs a breadth-first search from a source vertex.
// maxDepth limits the search depth (0 = unlimited).
func (g *Graph) BFS(src uint32, maxDepth uint32, mode Mode) (*BFSResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_bfs(
		g.ptr,
		C.uint32_t(src),
		C.uint32_t(maxDepth),
		mode.toC(),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	visited, err := result.GetU32("visited")
	if err != nil {
		return nil, fmt.Errorf("failed to get visited: %w", err)
	}

	depths, err := result.GetU32("depths")
	if err != nil {
		return nil, fmt.Errorf("failed to get depths: %w", err)
	}

	parents, err := result.GetU32("parents")
	if err != nil {
		return nil, fmt.Errorf("failed to get parents: %w", err)
	}

	return &BFSResult{
		Visited: visited,
		Depths:  depths,
		Parents: parents,
	}, nil
}

// BFSOnView performs BFS on a view.
func (g *Graph) BFSOnView(v *View, src uint32, maxDepth uint32, mode Mode) (*BFSResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_bfs_view(
		g.ptr,
		v.ptr,
		C.uint32_t(src),
		C.uint32_t(maxDepth),
		mode.toC(),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	visited, err := result.GetU32("visited")
	if err != nil {
		return nil, fmt.Errorf("failed to get visited: %w", err)
	}

	depths, err := result.GetU32("depths")
	if err != nil {
		return nil, fmt.Errorf("failed to get depths: %w", err)
	}

	parents, err := result.GetU32("parents")
	if err != nil {
		return nil, fmt.Errorf("failed to get parents: %w", err)
	}

	return &BFSResult{
		Visited: visited,
		Depths:  depths,
		Parents: parents,
	}, nil
}

// -----------------------------------------------------------------------------
// Algorithm: Neighborhood Query
// -----------------------------------------------------------------------------

// NeighborhoodResult contains the result of a neighborhood query.
type NeighborhoodResult struct {
	Vertices  []uint32 // Vertices in the neighborhood
	Distances []uint32 // Distance from nearest seed
}

// Neighborhood finds all vertices within hops of the seed vertices.
func (g *Graph) Neighborhood(seeds []uint32, hops uint32, mode Mode) (*NeighborhoodResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	if len(seeds) == 0 {
		return nil, fmt.Errorf("%w: seeds cannot be empty", ErrInvalidArg)
	}

	var out *C.ge_result_t
	status := C.ge_run_neighborhood(
		g.ptr,
		(*C.uint32_t)(unsafe.Pointer(&seeds[0])),
		C.size_t(len(seeds)),
		C.uint32_t(hops),
		mode.toC(),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	vertices, err := result.GetU32("vertices")
	if err != nil {
		return nil, fmt.Errorf("failed to get vertices: %w", err)
	}

	distances, err := result.GetU32("distances")
	if err != nil {
		return nil, fmt.Errorf("failed to get distances: %w", err)
	}

	return &NeighborhoodResult{
		Vertices:  vertices,
		Distances: distances,
	}, nil
}

// NeighborhoodOnView finds neighborhood on a view.
func (g *Graph) NeighborhoodOnView(v *View, seeds []uint32, hops uint32, mode Mode) (*NeighborhoodResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	if len(seeds) == 0 {
		return nil, fmt.Errorf("%w: seeds cannot be empty", ErrInvalidArg)
	}

	var out *C.ge_result_t
	status := C.ge_run_neighborhood_view(
		g.ptr,
		v.ptr,
		(*C.uint32_t)(unsafe.Pointer(&seeds[0])),
		C.size_t(len(seeds)),
		C.uint32_t(hops),
		mode.toC(),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	vertices, err := result.GetU32("vertices")
	if err != nil {
		return nil, fmt.Errorf("failed to get vertices: %w", err)
	}

	distances, err := result.GetU32("distances")
	if err != nil {
		return nil, fmt.Errorf("failed to get distances: %w", err)
	}

	return &NeighborhoodResult{
		Vertices:  vertices,
		Distances: distances,
	}, nil
}

// -----------------------------------------------------------------------------
// Algorithm: Community Detection
// -----------------------------------------------------------------------------

// CommunitiesResult contains the result of community detection.
type CommunitiesResult struct {
	Membership     []uint32 // Community ID for each vertex
	Modularity     float64  // Modularity score
	NumCommunities uint32
}

// CommunitiesLeiden detects communities using the Leiden algorithm.
// resolution controls community granularity (higher = more communities).
func (g *Graph) CommunitiesLeiden(resolution float64) (*CommunitiesResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_communities_leiden(
		g.ptr,
		C.double(resolution),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	modularity, err := result.GetF64("modularity")
	if err != nil {
		return nil, fmt.Errorf("failed to get modularity: %w", err)
	}

	numCommunities, err := result.GetU32("num_communities")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_communities: %w", err)
	}

	return &CommunitiesResult{
		Membership:     membership,
		Modularity:     modularity[0],
		NumCommunities: numCommunities[0],
	}, nil
}

// CommunitiesLeidenOnView detects communities on a view.
func (g *Graph) CommunitiesLeidenOnView(v *View, resolution float64) (*CommunitiesResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_communities_leiden_view(
		g.ptr,
		v.ptr,
		C.double(resolution),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	modularity, err := result.GetF64("modularity")
	if err != nil {
		return nil, fmt.Errorf("failed to get modularity: %w", err)
	}

	numCommunities, err := result.GetU32("num_communities")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_communities: %w", err)
	}

	return &CommunitiesResult{
		Membership:     membership,
		Modularity:     modularity[0],
		NumCommunities: numCommunities[0],
	}, nil
}

// CommunitiesLouvain detects communities using the Louvain algorithm.
func (g *Graph) CommunitiesLouvain(resolution float64) (*CommunitiesResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_communities_louvain(
		g.ptr,
		C.double(resolution),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	modularity, err := result.GetF64("modularity")
	if err != nil {
		return nil, fmt.Errorf("failed to get modularity: %w", err)
	}

	numCommunities, err := result.GetU32("num_communities")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_communities: %w", err)
	}

	return &CommunitiesResult{
		Membership:     membership,
		Modularity:     modularity[0],
		NumCommunities: numCommunities[0],
	}, nil
}

// CommunitiesLouvainOnView detects communities on a view.
func (g *Graph) CommunitiesLouvainOnView(v *View, resolution float64) (*CommunitiesResult, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.closed || g.ptr == nil {
		return nil, ErrShimClosed
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed || v.ptr == nil {
		return nil, ErrShimClosed
	}

	var out *C.ge_result_t
	status := C.ge_run_communities_louvain_view(
		g.ptr,
		v.ptr,
		C.double(resolution),
		&out,
	)
	if status != C.GE_OK {
		return nil, statusToError(status)
	}

	result := &Result{ptr: out}
	defer result.Close()

	membership, err := result.GetU32("membership")
	if err != nil {
		return nil, fmt.Errorf("failed to get membership: %w", err)
	}

	modularity, err := result.GetF64("modularity")
	if err != nil {
		return nil, fmt.Errorf("failed to get modularity: %w", err)
	}

	numCommunities, err := result.GetU32("num_communities")
	if err != nil {
		return nil, fmt.Errorf("failed to get num_communities: %w", err)
	}

	return &CommunitiesResult{
		Membership:     membership,
		Modularity:     modularity[0],
		NumCommunities: numCommunities[0],
	}, nil
}

// -----------------------------------------------------------------------------
// Version info
// -----------------------------------------------------------------------------

// ShimVersion returns the version of the C shim library.
func ShimVersion() string {
	return C.GoString(C.ge_shim_version())
}

// IgraphVersion returns the version of the igraph library.
func IgraphVersion() string {
	return C.GoString(C.ge_igraph_version())
}
