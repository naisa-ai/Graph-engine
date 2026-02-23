package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
	"unsafe"

	"github.com/RoaringBitmap/roaring"
	"github.com/google/uuid"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// View represents a filtered subgraph of a GraphVersion.
// Views use roaring bitmaps for edge/vertex masks for memory-efficient storage.
type View struct {
	// Identity
	ID        string
	VersionID string
	GraphName string
	CreatedAt time.Time

	// SpecHash for caching - hash of the ViewSpec used to create this view
	SpecHash string

	// Edge mask - bit set means edge is included in view
	// Uses roaring bitmap for memory-efficient storage on sparse views
	edgeMask *roaring.Bitmap

	// Vertex mask - derived from edges or explicit filter
	// Uses roaring bitmap for memory-efficient storage
	vertexMask *roaring.Bitmap

	// Total edge and vertex counts in the full graph (for bounds checking)
	totalEdges    uint32
	totalVertices uint32

	// Cached counts of included elements
	VCount uint64
	ECount uint64

	// Reference to parent version (must be pinned while view is in use)
	version *GraphVersion

	// Memory tracking
	memoryBytes uint64

	// Reference counting for concurrent access
	refCount int
	refMu    sync.Mutex
}

// EdgeMask provides backward-compatible access to edge inclusion status.
// Returns a wrapper that implements index-based access.
// DEPRECATED: Use EdgeMaskBitmap() for direct bitmap access in new code.
type EdgeMask struct {
	bitmap *roaring.Bitmap //nolint:unused // deprecated wrapper field
	size   uint32          //nolint:unused // deprecated wrapper field
}

// VertexMask provides backward-compatible access to vertex inclusion status.
// Returns a wrapper that implements index-based access.
// DEPRECATED: Use VertexMaskBitmap() for direct bitmap access in new code.
type VertexMask struct {
	bitmap *roaring.Bitmap //nolint:unused // deprecated wrapper field
	size   uint32          //nolint:unused // deprecated wrapper field
}

// NewView creates a new empty View from a GraphVersion.
// All edges and vertices are initially included.
func NewView(version *GraphVersion) *View {
	numEdges := uint32(len(version.EdgeSrc))
	numVertices := uint32(len(version.indexToNodeID))

	v := &View{
		ID:            uuid.New().String(),
		VersionID:     version.ID,
		GraphName:     version.GraphName,
		CreatedAt:     time.Now(),
		edgeMask:      roaring.New(),
		vertexMask:    roaring.New(),
		totalEdges:    numEdges,
		totalVertices: numVertices,
		version:       version,
	}

	// Initially include all edges and vertices
	if numEdges > 0 {
		v.edgeMask.AddRange(0, uint64(numEdges))
	}
	if numVertices > 0 {
		v.vertexMask.AddRange(0, uint64(numVertices))
	}

	v.VCount = uint64(numVertices)
	v.ECount = uint64(numEdges)
	v.memoryBytes = v.estimateMemory()

	return v
}

// NewViewFromSpec creates a View by applying a ViewSpec to a GraphVersion.
func NewViewFromSpec(version *GraphVersion, spec *gepb.ViewSpec) (*View, error) {
	v := NewView(version)
	v.SpecHash = HashViewSpec(spec)

	// Apply induced vertices first (most restrictive)
	if len(spec.GetInduceVerticesU64()) > 0 {
		if err := v.ApplyInducedVertices(spec.GetInduceVerticesU64()); err != nil {
			return nil, err
		}
	}

	// Apply neighborhood expansion
	if spec.GetNeighborhood() != nil {
		if err := v.ApplyNeighborhood(spec.GetNeighborhood()); err != nil {
			return nil, err
		}
	}

	// Apply vertex filter
	if spec.GetVfilter() != nil && len(spec.GetVfilter().GetPredicates()) > 0 {
		if err := v.ApplyVertexFilter(spec.GetVfilter()); err != nil {
			return nil, err
		}
	}

	// Apply edge filter
	if spec.GetEfilter() != nil && len(spec.GetEfilter().GetPredicates()) > 0 {
		if err := v.ApplyEdgeFilter(spec.GetEfilter()); err != nil {
			return nil, err
		}
	}

	// Apply exclusions last
	if len(spec.GetExcludeVerticesU64()) > 0 || len(spec.GetExcludeEdgesU64()) > 0 {
		v.ApplyExclusions(spec.GetExcludeVerticesU64(), spec.GetExcludeEdgesU64())
	}

	// Update counts
	v.updateCounts()
	v.memoryBytes = v.estimateMemory()

	return v, nil
}

// =============================================================================
// Backward-compatible mask accessors (for existing algorithm code)
// =============================================================================

// EdgeMask provides backward-compatible []bool-like access to edge mask.
// DEPRECATED: Prefer EdgeMaskBitmap() for new code.
var _ interface {
	ContainsEdge(idx int) bool
} = (*View)(nil)

// VertexMask provides backward-compatible []bool-like access to vertex mask.
// DEPRECATED: Prefer VertexMaskBitmap() for new code.
var _ interface {
	ContainsVertex(nodeID uint64) bool
} = (*View)(nil)

// EdgeMaskBitmap returns the underlying roaring bitmap for edge mask.
// This is the preferred way to access edge mask in new code.
func (v *View) EdgeMaskBitmap() *roaring.Bitmap {
	return v.edgeMask
}

// VertexMaskBitmap returns the underlying roaring bitmap for vertex mask.
// This is the preferred way to access vertex mask in new code.
func (v *View) VertexMaskBitmap() *roaring.Bitmap {
	return v.vertexMask
}

// EdgeMaskContains checks if an edge index is included in the view.
func (v *View) EdgeMaskContains(idx uint32) bool {
	return v.edgeMask.Contains(idx)
}

// VertexMaskContains checks if a vertex index is included in the view.
func (v *View) VertexMaskContains(idx uint32) bool {
	return v.vertexMask.Contains(idx)
}

// GetEdgeMaskBytes returns the edge mask as a packed byte array for shim compatibility.
// Each bit represents one edge (bit 0 of byte 0 = edge 0, etc.).
func (v *View) GetEdgeMaskBytes() []byte {
	numBytes := (v.totalEdges + 7) / 8
	result := make([]byte, numBytes)

	it := v.edgeMask.Iterator()
	for it.HasNext() {
		idx := it.Next()
		if idx < v.totalEdges {
			result[idx/8] |= 1 << (idx % 8)
		}
	}

	return result
}

// GetVertexMaskBytes returns the vertex mask as a packed byte array for shim compatibility.
func (v *View) GetVertexMaskBytes() []byte {
	numBytes := (v.totalVertices + 7) / 8
	result := make([]byte, numBytes)

	it := v.vertexMask.Iterator()
	for it.HasNext() {
		idx := it.Next()
		if idx < v.totalVertices {
			result[idx/8] |= 1 << (idx % 8)
		}
	}

	return result
}

// =============================================================================
// View operations
// =============================================================================

// ApplyInducedVertices restricts the view to only the specified vertices
// and edges between them.
func (v *View) ApplyInducedVertices(vertices []uint64) error {
	// Create a set of included vertex indices
	includedVertices := roaring.New()
	for _, nodeID := range vertices {
		idx, ok := v.version.GetNodeIndex(nodeID)
		if !ok {
			// Skip vertices not in graph
			continue
		}
		includedVertices.Add(idx)
	}

	// Update vertex mask (intersection with current mask)
	v.vertexMask.And(includedVertices)

	// Update edge mask - only include edges where both endpoints are in induced set
	newEdgeMask := roaring.New()
	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		src := v.version.EdgeSrc[i]
		dst := v.version.EdgeDst[i]
		if v.vertexMask.Contains(src) && v.vertexMask.Contains(dst) {
			newEdgeMask.Add(i)
		}
	}
	v.edgeMask = newEdgeMask

	return nil
}

// ApplyNeighborhood expands the view to include the neighborhood around seed vertices.
func (v *View) ApplyNeighborhood(spec *gepb.NeighborhoodSpec) error {
	if len(spec.GetSeedsU64()) == 0 || spec.GetHops() == 0 {
		return nil
	}

	// Build adjacency list from the full graph
	adj := v.buildAdjacencyList(spec.GetMode() == gepb.NeighborhoodSpec_MODE_OUT)

	// BFS from seeds using roaring bitmap for visited set
	visited := roaring.New()
	queue := make([]uint32, 0)

	// Initialize with seeds
	for _, seedID := range spec.GetSeedsU64() {
		idx, ok := v.version.GetNodeIndex(seedID)
		if !ok {
			continue
		}
		if !visited.Contains(idx) {
			visited.Add(idx)
			queue = append(queue, idx)
		}
	}

	// BFS for specified hops
	for hop := uint32(0); hop < spec.GetHops() && len(queue) > 0; hop++ {
		nextQueue := make([]uint32, 0)
		for _, nodeIdx := range queue {
			for _, neighbor := range adj[nodeIdx] {
				if !visited.Contains(neighbor) {
					visited.Add(neighbor)
					nextQueue = append(nextQueue, neighbor)
				}
			}
		}
		queue = nextQueue
	}

	// Update vertex mask
	v.vertexMask = visited

	// Update edge mask - include edges where both endpoints are visited
	newEdgeMask := roaring.New()
	for i := uint32(0); i < v.totalEdges; i++ {
		src := v.version.EdgeSrc[i]
		dst := v.version.EdgeDst[i]
		if visited.Contains(src) && visited.Contains(dst) {
			newEdgeMask.Add(i)
		}
	}
	v.edgeMask = newEdgeMask

	return nil
}

// ApplyVertexFilter applies vertex predicates to filter the view.
func (v *View) ApplyVertexFilter(filter *gepb.VertexFilter) error {
	// For now, we support filtering by edge endpoints since we don't have
	// vertex attributes stored separately. This can be extended when
	// vertex attributes are added.

	// Each predicate is ANDed together
	for _, pred := range filter.GetPredicates() {
		switch pred.GetColumn() {
		case "kind":
			// Filter vertices by incident edge kind
			if err := v.filterVerticesByEdgeKind(pred); err != nil {
				return err
			}
		default:
			// Unknown column - skip for now
			// In production, might want to return an error
		}
	}

	return nil
}

// ApplyEdgeFilter applies edge predicates to filter the view.
func (v *View) ApplyEdgeFilter(filter *gepb.EdgeFilter) error {
	for _, pred := range filter.GetPredicates() {
		switch pred.GetColumn() {
		case "kind":
			if err := v.filterEdgesByKind(pred); err != nil {
				return err
			}
		case "weight":
			if err := v.filterEdgesByWeight(pred); err != nil {
				return err
			}
		default:
			// Unknown column - skip for now
		}
	}

	// After filtering edges, update vertex mask to only include vertices
	// that have at least one incident edge
	v.updateVertexMaskFromEdges()

	return nil
}

// ApplyExclusions removes specific vertices and edges from the view.
func (v *View) ApplyExclusions(excludeVertices, excludeEdges []uint64) {
	// Exclude vertices
	for _, nodeID := range excludeVertices {
		idx, ok := v.version.GetNodeIndex(nodeID)
		if ok {
			v.vertexMask.Remove(idx)
		}
	}

	// Exclude edges by their external representation
	// Note: excludeEdges should be edge indices in the original graph
	for _, edgeIdx := range excludeEdges {
		if uint32(edgeIdx) < v.totalEdges {
			v.edgeMask.Remove(uint32(edgeIdx))
		}
	}

	// Update edge mask to exclude edges with excluded vertices
	newEdgeMask := roaring.New()
	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		src := v.version.EdgeSrc[i]
		dst := v.version.EdgeDst[i]
		if v.vertexMask.Contains(src) && v.vertexMask.Contains(dst) {
			newEdgeMask.Add(i)
		}
	}
	v.edgeMask = newEdgeMask
}

// GetEdges returns the filtered edge arrays (using compact indices).
func (v *View) GetEdges() (src, dst []uint32) {
	src = make([]uint32, 0, v.ECount)
	dst = make([]uint32, 0, v.ECount)

	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		src = append(src, v.version.EdgeSrc[i])
		dst = append(dst, v.version.EdgeDst[i])
	}

	return src, dst
}

// GetEdgesWithWeights returns filtered edges with their weights.
func (v *View) GetEdgesWithWeights() (src, dst []uint32, weights []float32) {
	src = make([]uint32, 0, v.ECount)
	dst = make([]uint32, 0, v.ECount)
	weights = make([]float32, 0, v.ECount)

	hasWeights := len(v.version.EdgeWeight) == len(v.version.EdgeSrc)

	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		src = append(src, v.version.EdgeSrc[i])
		dst = append(dst, v.version.EdgeDst[i])
		if hasWeights {
			weights = append(weights, v.version.EdgeWeight[i])
		} else {
			weights = append(weights, 1.0)
		}
	}

	return src, dst, weights
}

// GetEdgeIndices returns the indices of included edges.
func (v *View) GetEdgeIndices() []uint32 {
	return v.edgeMask.ToArray()
}

// GetVertexIndices returns the indices of included vertices.
func (v *View) GetVertexIndices() []uint32 {
	return v.vertexMask.ToArray()
}

// GetVertices returns the external node IDs of included vertices.
func (v *View) GetVertices() []uint64 {
	result := make([]uint64, 0, v.VCount)
	it := v.vertexMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		nodeID, _ := v.version.GetNodeID(i)
		result = append(result, nodeID)
	}
	return result
}

// GetVersion returns the underlying GraphVersion.
func (v *View) GetVersion() *GraphVersion {
	return v.version
}

// ContainsVertex checks if a vertex (by external ID) is in the view.
func (v *View) ContainsVertex(nodeID uint64) bool {
	idx, ok := v.version.GetNodeIndex(nodeID)
	if !ok {
		return false
	}
	return v.vertexMask.Contains(idx)
}

// ContainsVertexByIndex checks if a vertex (by internal index) is in the view.
func (v *View) ContainsVertexByIndex(idx uint32) bool {
	return v.vertexMask.Contains(idx)
}

// ContainsEdge checks if an edge (by index) is in the view.
func (v *View) ContainsEdge(edgeIdx int) bool {
	if edgeIdx < 0 || uint32(edgeIdx) >= v.totalEdges {
		return false
	}
	return v.edgeMask.Contains(uint32(edgeIdx))
}

// Pin increments the reference count.
func (v *View) Pin() {
	v.refMu.Lock()
	v.refCount++
	v.refMu.Unlock()
}

// Unpin decrements the reference count.
// Returns true if the view can be cleaned up.
func (v *View) Unpin() bool {
	v.refMu.Lock()
	defer v.refMu.Unlock()
	v.refCount--
	return v.refCount <= 0
}

// RefCount returns the current reference count.
func (v *View) RefCount() int {
	v.refMu.Lock()
	defer v.refMu.Unlock()
	return v.refCount
}

// EstimateMemory returns the estimated memory usage in bytes.
func (v *View) EstimateMemory() uint64 {
	return v.memoryBytes
}

// estimateMemory calculates memory usage.
func (v *View) estimateMemory() uint64 {
	var total uint64

	// Roaring bitmap sizes (approximation using serialized size)
	total += v.edgeMask.GetSizeInBytes()
	total += v.vertexMask.GetSizeInBytes()

	// Struct overhead
	total += uint64(unsafe.Sizeof(*v))

	return total
}

// updateCounts recalculates VCount and ECount from masks.
func (v *View) updateCounts() {
	v.VCount = v.vertexMask.GetCardinality()
	v.ECount = v.edgeMask.GetCardinality()
}

// buildAdjacencyList builds an adjacency list from the full graph.
func (v *View) buildAdjacencyList(outOnly bool) map[uint32][]uint32 {
	adj := make(map[uint32][]uint32)

	for i := range v.version.EdgeSrc {
		src := v.version.EdgeSrc[i]
		dst := v.version.EdgeDst[i]

		adj[src] = append(adj[src], dst)
		if !outOnly && !v.version.Directed {
			adj[dst] = append(adj[dst], src)
		}
	}

	return adj
}

// filterVerticesByEdgeKind filters vertices based on incident edge kinds.
func (v *View) filterVerticesByEdgeKind(pred *gepb.Predicate) error {
	if v.version.EdgeKind == nil {
		return nil // No edge kinds to filter
	}

	// Get the target kind(s)
	var targetKinds []uint32
	switch val := pred.GetValue().(type) {
	case *gepb.Predicate_U32:
		targetKinds = []uint32{val.U32}
	case *gepb.Predicate_U32S:
		targetKinds = val.U32S.GetValues()
	default:
		return nil
	}

	kindSet := make(map[uint32]bool)
	for _, k := range targetKinds {
		kindSet[k] = true
	}

	// Find vertices with at least one edge of the target kind
	verticesWithKind := roaring.New()
	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		if kindSet[v.version.EdgeKind[i]] {
			verticesWithKind.Add(v.version.EdgeSrc[i])
			verticesWithKind.Add(v.version.EdgeDst[i])
		}
	}

	// Update vertex mask (intersection)
	v.vertexMask.And(verticesWithKind)

	return nil
}

// filterEdgesByKind filters edges based on their kind.
func (v *View) filterEdgesByKind(pred *gepb.Predicate) error {
	if v.version.EdgeKind == nil {
		return nil
	}

	var targetKinds []uint32
	switch val := pred.GetValue().(type) {
	case *gepb.Predicate_U32:
		targetKinds = []uint32{val.U32}
	case *gepb.Predicate_U32S:
		targetKinds = val.U32S.GetValues()
	default:
		return nil
	}

	kindSet := make(map[uint32]bool)
	for _, k := range targetKinds {
		kindSet[k] = true
	}

	// Apply filter based on operation
	switch pred.GetOp() {
	case gepb.Predicate_OP_EQ, gepb.Predicate_OP_IN:
		newEdgeMask := roaring.New()
		it := v.edgeMask.Iterator()
		for it.HasNext() {
			i := it.Next()
			if kindSet[v.version.EdgeKind[i]] {
				newEdgeMask.Add(i)
			}
		}
		v.edgeMask = newEdgeMask
	}

	return nil
}

// filterEdgesByWeight filters edges based on their weight.
func (v *View) filterEdgesByWeight(pred *gepb.Predicate) error {
	if v.version.EdgeWeight == nil {
		return nil
	}

	switch pred.GetOp() {
	case gepb.Predicate_OP_RANGE:
		rangeVal := pred.GetRangeF64()
		if rangeVal == nil {
			return nil
		}
		minVal := rangeVal.GetLo()
		maxVal := rangeVal.GetHi()

		newEdgeMask := roaring.New()
		it := v.edgeMask.Iterator()
		for it.HasNext() {
			i := it.Next()
			w := float64(v.version.EdgeWeight[i])
			if w >= minVal && w <= maxVal {
				newEdgeMask.Add(i)
			}
		}
		v.edgeMask = newEdgeMask
	}

	return nil
}

// updateVertexMaskFromEdges updates vertex mask to only include vertices
// with at least one incident edge in the view.
func (v *View) updateVertexMaskFromEdges() {
	// Clear vertex mask
	v.vertexMask = roaring.New()

	// Include vertices that have at least one included edge
	it := v.edgeMask.Iterator()
	for it.HasNext() {
		i := it.Next()
		v.vertexMask.Add(v.version.EdgeSrc[i])
		v.vertexMask.Add(v.version.EdgeDst[i])
	}
}

// =============================================================================
// Set operations on views
// =============================================================================

// Intersect creates a new view that is the intersection of this view and another.
// Both views must be based on the same GraphVersion.
func (v *View) Intersect(other *View) (*View, error) {
	if v.VersionID != other.VersionID {
		return nil, fmt.Errorf("cannot intersect views from different graph versions")
	}

	result := &View{
		ID:            uuid.New().String(),
		VersionID:     v.VersionID,
		GraphName:     v.GraphName,
		CreatedAt:     time.Now(),
		edgeMask:      roaring.And(v.edgeMask, other.edgeMask),
		vertexMask:    roaring.And(v.vertexMask, other.vertexMask),
		totalEdges:    v.totalEdges,
		totalVertices: v.totalVertices,
		version:       v.version,
	}

	result.updateCounts()
	result.memoryBytes = result.estimateMemory()

	return result, nil
}

// Union creates a new view that is the union of this view and another.
// Both views must be based on the same GraphVersion.
func (v *View) Union(other *View) (*View, error) {
	if v.VersionID != other.VersionID {
		return nil, fmt.Errorf("cannot union views from different graph versions")
	}

	result := &View{
		ID:            uuid.New().String(),
		VersionID:     v.VersionID,
		GraphName:     v.GraphName,
		CreatedAt:     time.Now(),
		edgeMask:      roaring.Or(v.edgeMask, other.edgeMask),
		vertexMask:    roaring.Or(v.vertexMask, other.vertexMask),
		totalEdges:    v.totalEdges,
		totalVertices: v.totalVertices,
		version:       v.version,
	}

	result.updateCounts()
	result.memoryBytes = result.estimateMemory()

	return result, nil
}

// Difference creates a new view with elements from this view that are not in other.
// Both views must be based on the same GraphVersion.
func (v *View) Difference(other *View) (*View, error) {
	if v.VersionID != other.VersionID {
		return nil, fmt.Errorf("cannot difference views from different graph versions")
	}

	result := &View{
		ID:            uuid.New().String(),
		VersionID:     v.VersionID,
		GraphName:     v.GraphName,
		CreatedAt:     time.Now(),
		edgeMask:      roaring.AndNot(v.edgeMask, other.edgeMask),
		vertexMask:    roaring.AndNot(v.vertexMask, other.vertexMask),
		totalEdges:    v.totalEdges,
		totalVertices: v.totalVertices,
		version:       v.version,
	}

	result.updateCounts()
	result.memoryBytes = result.estimateMemory()

	return result, nil
}

// Clone creates a deep copy of the view.
func (v *View) Clone() *View {
	return &View{
		ID:            uuid.New().String(),
		VersionID:     v.VersionID,
		GraphName:     v.GraphName,
		CreatedAt:     time.Now(),
		SpecHash:      v.SpecHash,
		edgeMask:      v.edgeMask.Clone(),
		vertexMask:    v.vertexMask.Clone(),
		totalEdges:    v.totalEdges,
		totalVertices: v.totalVertices,
		VCount:        v.VCount,
		ECount:        v.ECount,
		version:       v.version,
		memoryBytes:   v.memoryBytes,
	}
}

// =============================================================================
// Hash and serialization
// =============================================================================

// mustFprintf writes to w and logs any error (hash writes rarely fail).
func mustFprintf(w io.Writer, format string, args ...interface{}) {
	if _, err := fmt.Fprintf(w, format, args...); err != nil {
		slog.Default().Error("hash write failed", "error", err)
	}
}

// HashViewSpec creates a hash of a ViewSpec for caching.
func HashViewSpec(spec *gepb.ViewSpec) string {
	h := sha256.New()

	// Hash key components of the spec
	if spec.GetVfilter() != nil {
		for _, p := range spec.GetVfilter().GetPredicates() {
			mustFprintf(h, "vf:%s:%d", p.GetColumn(), p.GetOp())
		}
	}
	if spec.GetEfilter() != nil {
		for _, p := range spec.GetEfilter().GetPredicates() {
			mustFprintf(h, "ef:%s:%d", p.GetColumn(), p.GetOp())
		}
	}
	for _, v := range spec.GetInduceVerticesU64() {
		mustFprintf(h, "iv:%d", v)
	}
	if spec.GetNeighborhood() != nil {
		for _, s := range spec.GetNeighborhood().GetSeedsU64() {
			mustFprintf(h, "ns:%d", s)
		}
		mustFprintf(h, "nh:%d:%d", spec.GetNeighborhood().GetHops(), spec.GetNeighborhood().GetMode())
	}
	for _, v := range spec.GetExcludeVerticesU64() {
		mustFprintf(h, "ev:%d", v)
	}
	for _, e := range spec.GetExcludeEdgesU64() {
		mustFprintf(h, "ee:%d", e)
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ViewStats returns statistics about the view.
type ViewStats struct {
	VertexCount      uint64
	EdgeCount        uint64
	VertexMaskBytes  uint64
	EdgeMaskBytes    uint64
	TotalMemoryBytes uint64
	Density          float64 // ECount / (VCount * (VCount - 1)) for directed
}

// Stats returns statistics about the view.
func (v *View) Stats() ViewStats {
	var density float64
	if v.VCount > 1 {
		density = float64(v.ECount) / float64(v.VCount*(v.VCount-1))
	}

	return ViewStats{
		VertexCount:      v.VCount,
		EdgeCount:        v.ECount,
		VertexMaskBytes:  v.vertexMask.GetSizeInBytes(),
		EdgeMaskBytes:    v.edgeMask.GetSizeInBytes(),
		TotalMemoryBytes: v.memoryBytes,
		Density:          density,
	}
}
