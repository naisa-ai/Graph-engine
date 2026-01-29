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

	// Schema definition
	Schema *gepb.Schema
	Labels map[string]string

	// Statistics
	VCount uint64 // Number of vertices
	ECount uint64 // Number of edges

	// Reference counting for RCU-style access
	refCount int
	refMu    sync.Mutex

	// Memory estimate in bytes
	memoryBytes uint64
}

// NewGraphVersion creates a new GraphVersion from a Build.
func NewGraphVersion(id, graphName string, directed bool, build *Build) (*GraphVersion, error) {
	gv := &GraphVersion{
		ID:          id,
		GraphName:   graphName,
		Directed:    directed,
		PublishedAt: time.Now(),
		Labels:      build.Labels,
		refCount:    0,
	}

	// Build node ID mapping
	if err := gv.buildNodeMapping(build); err != nil {
		return nil, err
	}

	// Build edge arrays with compact indices
	if err := gv.buildEdgeArrays(build); err != nil {
		return nil, err
	}

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

	// Struct overhead
	total += uint64(unsafe.Sizeof(*gv))

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
