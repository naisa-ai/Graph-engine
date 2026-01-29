package service

import (
	"sync"
	"time"
	"unsafe"

	gepb "github.com/naisa-ai/graph-engine/gen/graphengine/v1"
)

// GraphVersion represents a published, immutable graph version.
// Once published, a GraphVersion is read-only and can be accessed
// concurrently by multiple readers.
type GraphVersion struct {
	// Identity
	ID          string
	GraphName   string
	Directed    bool
	PublishedAt time.Time

	// Node ID mapping: external IDs to compact indices
	// External node IDs (uint64) are mapped to compact indices (uint32)
	// for memory efficiency and igraph compatibility.
	nodeIDToIndex map[uint64]uint32 // external -> compact index
	indexToNodeID []uint64          // compact index -> external

	// Edge data using compact node indices
	// All arrays have the same length (number of edges)
	EdgeSrc    []uint32  // Source node indices
	EdgeDst    []uint32  // Destination node indices
	EdgeKind   []uint32  // Edge type/kind (optional)
	EdgeWeight []float32 // Edge weights (optional)

	// Column data (indexed by column name)
	VertexColumns map[string]*ColumnData
	EdgeColumns   map[string]*ColumnData

	// Schema definition
	Schema *gepb.Schema
	Labels map[string]string

	// Statistics
	VCount uint64 // Number of vertices
	ECount uint64 // Number of edges

	// Precomputed artifacts (optional, set during PublishBuild with BatchArtifacts)
	// Community membership (vertex index -> community ID)
	CommunityMembership []uint32
	NumCommunities      uint32

	// K-Core decomposition (vertex index -> coreness value)
	KCoreness []uint32
	MaxCore   uint32

	// Betweenness centrality scores (vertex index -> score)
	BetweennessScores []float64

	// Reference counting for RCU-style access
	refCount int
	refMu    sync.Mutex

	// Memory estimate in bytes
	memoryBytes uint64
}

// NewGraphVersion creates a new GraphVersion from a Build.
func NewGraphVersion(id, graphName string, directed bool, build *Build) (*GraphVersion, error) {
	gv := &GraphVersion{
		ID:            id,
		GraphName:     graphName,
		Directed:      directed,
		PublishedAt:   time.Now(),
		Labels:        build.Labels,
		VertexColumns: make(map[string]*ColumnData),
		EdgeColumns:   make(map[string]*ColumnData),
		refCount:      0,
	}

	// Build node ID mapping
	if err := gv.buildNodeMapping(build); err != nil {
		return nil, err
	}

	// Build edge arrays with compact indices
	if err := gv.buildEdgeArrays(build); err != nil {
		return nil, err
	}

	// Copy column data from build
	gv.copyColumnData(build)

	// Calculate statistics
	gv.VCount = uint64(len(gv.indexToNodeID))
	gv.ECount = uint64(len(gv.EdgeSrc))

	// Estimate memory usage
	gv.memoryBytes = gv.estimateMemory()

	return gv, nil
}

// buildNodeMapping creates the bidirectional mapping between external
// node IDs and compact indices.
func (gv *GraphVersion) buildNodeMapping(build *Build) error {
	build.mu.RLock()
	defer build.mu.RUnlock()

	// Collect all unique node IDs
	nodeSet := make(map[uint64]struct{})

	// From explicit vertices
	for _, id := range build.VerticesU64 {
		nodeSet[id] = struct{}{}
	}

	// From edges (in case vertices weren't explicitly declared)
	for _, id := range build.EdgesSrcU64 {
		nodeSet[id] = struct{}{}
	}
	for _, id := range build.EdgesDstU64 {
		nodeSet[id] = struct{}{}
	}

	// Build mappings
	gv.nodeIDToIndex = make(map[uint64]uint32, len(nodeSet))
	gv.indexToNodeID = make([]uint64, 0, len(nodeSet))

	var idx uint32
	for nodeID := range nodeSet {
		gv.nodeIDToIndex[nodeID] = idx
		gv.indexToNodeID = append(gv.indexToNodeID, nodeID)
		idx++
	}

	return nil
}

// buildEdgeArrays converts edges from external IDs to compact indices.
func (gv *GraphVersion) buildEdgeArrays(build *Build) error {
	build.mu.RLock()
	defer build.mu.RUnlock()

	numEdges := len(build.EdgesSrcU64)

	gv.EdgeSrc = make([]uint32, numEdges)
	gv.EdgeDst = make([]uint32, numEdges)

	for i := 0; i < numEdges; i++ {
		srcIdx, ok := gv.nodeIDToIndex[build.EdgesSrcU64[i]]
		if !ok {
			// This shouldn't happen if buildNodeMapping ran first
			srcIdx = 0
		}
		dstIdx, ok := gv.nodeIDToIndex[build.EdgesDstU64[i]]
		if !ok {
			dstIdx = 0
		}
		gv.EdgeSrc[i] = srcIdx
		gv.EdgeDst[i] = dstIdx
	}

	// Copy edge attributes if present
	if len(build.EdgesKind) == numEdges {
		gv.EdgeKind = make([]uint32, numEdges)
		copy(gv.EdgeKind, build.EdgesKind)
	}

	if len(build.EdgesWeight) == numEdges {
		gv.EdgeWeight = make([]float32, numEdges)
		copy(gv.EdgeWeight, build.EdgesWeight)
	}

	return nil
}

// copyColumnData copies column data from build to graph version.
func (gv *GraphVersion) copyColumnData(build *Build) {
	build.mu.RLock()
	defer build.mu.RUnlock()

	// Copy vertex columns
	for name, col := range build.VertexColumns {
		gv.VertexColumns[name] = copyColumnData(col)
	}

	// Copy edge columns
	for name, col := range build.EdgeColumns {
		gv.EdgeColumns[name] = copyColumnData(col)
	}
}

// copyColumnData creates a deep copy of column data.
func copyColumnData(src *ColumnData) *ColumnData {
	if src == nil {
		return nil
	}

	dst := &ColumnData{
		Name: src.Name,
		Type: src.Type,
	}

	switch src.Type {
	case ColumnTypeBool:
		if len(src.BoolVal) > 0 {
			dst.BoolVal = make([]bool, len(src.BoolVal))
			copy(dst.BoolVal, src.BoolVal)
		}
	case ColumnTypeU32:
		if len(src.U32Val) > 0 {
			dst.U32Val = make([]uint32, len(src.U32Val))
			copy(dst.U32Val, src.U32Val)
		}
	case ColumnTypeU64:
		if len(src.U64Val) > 0 {
			dst.U64Val = make([]uint64, len(src.U64Val))
			copy(dst.U64Val, src.U64Val)
		}
	case ColumnTypeF32:
		if len(src.F32Val) > 0 {
			dst.F32Val = make([]float32, len(src.F32Val))
			copy(dst.F32Val, src.F32Val)
		}
	case ColumnTypeF64:
		if len(src.F64Val) > 0 {
			dst.F64Val = make([]float64, len(src.F64Val))
			copy(dst.F64Val, src.F64Val)
		}
	case ColumnTypeString:
		if len(src.StrVal) > 0 {
			dst.StrVal = make([]string, len(src.StrVal))
			copy(dst.StrVal, src.StrVal)
		}
	}

	return dst
}

// GetNodeIndex returns the compact index for an external node ID.
// Returns (index, true) if found, (0, false) if not found.
func (gv *GraphVersion) GetNodeIndex(nodeID uint64) (uint32, bool) {
	idx, ok := gv.nodeIDToIndex[nodeID]
	return idx, ok
}

// GetNodeID returns the external node ID for a compact index.
// Returns (nodeID, true) if valid, (0, false) if index out of range.
func (gv *GraphVersion) GetNodeID(index uint32) (uint64, bool) {
	if int(index) >= len(gv.indexToNodeID) {
		return 0, false
	}
	return gv.indexToNodeID[index], true
}

// GetAllNodeIDs returns a copy of all external node IDs.
func (gv *GraphVersion) GetAllNodeIDs() []uint64 {
	result := make([]uint64, len(gv.indexToNodeID))
	copy(result, gv.indexToNodeID)
	return result
}

// estimateMemory calculates an approximate memory footprint in bytes.
func (gv *GraphVersion) estimateMemory() uint64 {
	var total uint64

	// Node mapping
	// map overhead: ~48 bytes per entry + key (8) + value (4)
	total += uint64(len(gv.nodeIDToIndex)) * 60
	// indexToNodeID slice: 8 bytes per uint64
	total += uint64(len(gv.indexToNodeID)) * 8

	// Edge arrays: 4 bytes per uint32/float32
	total += uint64(len(gv.EdgeSrc)) * 4
	total += uint64(len(gv.EdgeDst)) * 4
	if gv.EdgeKind != nil {
		total += uint64(len(gv.EdgeKind)) * 4
	}
	if gv.EdgeWeight != nil {
		total += uint64(len(gv.EdgeWeight)) * 4
	}

	// Column data
	for _, col := range gv.VertexColumns {
		total += estimateColumnMemory(col)
	}
	for _, col := range gv.EdgeColumns {
		total += estimateColumnMemory(col)
	}

	// Struct overhead
	total += uint64(unsafe.Sizeof(*gv))

	return total
}

// estimateColumnMemory estimates memory usage of a column.
func estimateColumnMemory(col *ColumnData) uint64 {
	if col == nil {
		return 0
	}

	var total uint64
	// Base struct overhead
	total += uint64(unsafe.Sizeof(*col))

	switch col.Type {
	case ColumnTypeBool:
		total += uint64(len(col.BoolVal)) // 1 byte per bool
	case ColumnTypeU32:
		total += uint64(len(col.U32Val)) * 4
	case ColumnTypeU64:
		total += uint64(len(col.U64Val)) * 8
	case ColumnTypeF32:
		total += uint64(len(col.F32Val)) * 4
	case ColumnTypeF64:
		total += uint64(len(col.F64Val)) * 8
	case ColumnTypeString:
		// Estimate string memory: 16 bytes header + avg string length
		for _, s := range col.StrVal {
			total += 16 + uint64(len(s))
		}
	}

	return total
}

// EstimateMemory returns the estimated memory usage in bytes.
func (gv *GraphVersion) EstimateMemory() uint64 {
	return gv.memoryBytes
}

// Pin increments the reference count, preventing cleanup.
// Must be paired with Unpin.
func (gv *GraphVersion) Pin() {
	gv.refMu.Lock()
	gv.refCount++
	gv.refMu.Unlock()
}

// Unpin decrements the reference count.
// Returns true if the version can be cleaned up (refCount == 0).
func (gv *GraphVersion) Unpin() bool {
	gv.refMu.Lock()
	defer gv.refMu.Unlock()
	gv.refCount--
	return gv.refCount <= 0
}

// RefCount returns the current reference count.
func (gv *GraphVersion) RefCount() int {
	gv.refMu.Lock()
	defer gv.refMu.Unlock()
	return gv.refCount
}

// ToSummary returns a GraphSummary proto for this version.
func (gv *GraphVersion) ToSummary() *gepb.GraphSummary {
	return &gepb.GraphSummary{
		GraphName:        gv.GraphName,
		CurrentVersionId: gv.ID,
		Vcount:           gv.VCount,
		Ecount:           gv.ECount,
	}
}

// =============================================================================
// Community Methods
// =============================================================================

// SetCommunities stores precomputed community membership data.
func (gv *GraphVersion) SetCommunities(membership []uint32, numCommunities uint32) {
	gv.CommunityMembership = membership
	gv.NumCommunities = numCommunities
}

// GetVertexCommunity returns the community ID for a vertex by its index.
// Returns (communityID, true) if found, (0, false) if not available.
func (gv *GraphVersion) GetVertexCommunity(vertexIdx uint32) (uint32, bool) {
	if gv.CommunityMembership == nil || int(vertexIdx) >= len(gv.CommunityMembership) {
		return 0, false
	}
	return gv.CommunityMembership[vertexIdx], true
}

// HasCommunities returns true if community data is available.
func (gv *GraphVersion) HasCommunities() bool {
	return gv.CommunityMembership != nil && len(gv.CommunityMembership) > 0
}

// GetCommunityMembers returns all vertex indices belonging to a specific community.
func (gv *GraphVersion) GetCommunityMembers(communityID uint32) []uint32 {
	if !gv.HasCommunities() {
		return nil
	}
	members := make([]uint32, 0)
	for i, cid := range gv.CommunityMembership {
		if cid == communityID {
			members = append(members, uint32(i))
		}
	}
	return members
}

// =============================================================================
// K-Core Methods
// =============================================================================

// SetKCore stores precomputed k-core decomposition data.
func (gv *GraphVersion) SetKCore(coreness []uint32, maxCore uint32) {
	gv.KCoreness = coreness
	gv.MaxCore = maxCore
}

// GetVertexCoreness returns the coreness value for a vertex by its index.
// Returns (coreness, true) if found, (0, false) if not available.
func (gv *GraphVersion) GetVertexCoreness(vertexIdx uint32) (uint32, bool) {
	if gv.KCoreness == nil || int(vertexIdx) >= len(gv.KCoreness) {
		return 0, false
	}
	return gv.KCoreness[vertexIdx], true
}

// HasKCore returns true if k-core data is available.
func (gv *GraphVersion) HasKCore() bool {
	return gv.KCoreness != nil && len(gv.KCoreness) > 0
}

// GetKCoreMembers returns all vertex indices with coreness >= k.
func (gv *GraphVersion) GetKCoreMembers(k uint32) []uint32 {
	if !gv.HasKCore() {
		return nil
	}
	members := make([]uint32, 0)
	for i, coreness := range gv.KCoreness {
		if coreness >= k {
			members = append(members, uint32(i))
		}
	}
	return members
}

// =============================================================================
// Betweenness Methods
// =============================================================================

// SetBetweenness stores precomputed betweenness centrality scores.
func (gv *GraphVersion) SetBetweenness(scores []float64) {
	gv.BetweennessScores = scores
}

// GetVertexBetweenness returns the betweenness centrality score for a vertex.
// Returns (score, true) if found, (0.0, false) if not available.
func (gv *GraphVersion) GetVertexBetweenness(vertexIdx uint32) (float64, bool) {
	if gv.BetweennessScores == nil || int(vertexIdx) >= len(gv.BetweennessScores) {
		return 0.0, false
	}
	return gv.BetweennessScores[vertexIdx], true
}

// HasBetweenness returns true if betweenness data is available.
func (gv *GraphVersion) HasBetweenness() bool {
	return gv.BetweennessScores != nil && len(gv.BetweennessScores) > 0
}
