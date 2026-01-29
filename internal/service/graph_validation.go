// Package service provides core services for the graph-engine.
package service

import (
	"fmt"
	"math"
)

// GraphValidationResult holds the results of graph validation with metrics.
type GraphValidationResult struct {
	// Valid is true if no errors were found
	Valid bool

	// Errors are fatal issues that indicate data corruption
	Errors []*ValidationError

	// Warnings are non-fatal issues that might indicate problems
	Warnings []string

	// Metrics contains validation statistics
	Metrics map[string]string
}

// NewGraphValidationResult creates a new empty validation result.
func NewGraphValidationResult() *GraphValidationResult {
	return &GraphValidationResult{
		Valid:    true,
		Errors:   make([]*ValidationError, 0),
		Warnings: make([]string, 0),
		Metrics:  make(map[string]string),
	}
}

// AddError adds an error to the result and marks it as invalid.
func (r *GraphValidationResult) AddError(code, message string, details map[string]interface{}) {
	r.Valid = false
	r.Errors = append(r.Errors, &ValidationError{
		Code:    code,
		Message: message,
		Details: details,
	})
}

// AddWarning adds a warning to the result.
func (r *GraphValidationResult) AddWarning(message string) {
	r.Warnings = append(r.Warnings, message)
}

// SetMetric sets a validation metric.
func (r *GraphValidationResult) SetMetric(key, value string) {
	r.Metrics[key] = value
}

// ValidateGraph performs validation on a GraphVersion.
// If deep is true, performs comprehensive validation.
// If deep is false, performs only shallow (fast) validation.
func ValidateGraph(gv *GraphVersion, deep bool) *GraphValidationResult {
	result := NewGraphValidationResult()

	// Always run shallow validation
	validateShallow(gv, result)

	// Run deep validation if requested
	if deep {
		validateDeep(gv, result)
	}

	return result
}

// validateShallow performs fast, basic validation checks.
func validateShallow(gv *GraphVersion, result *GraphValidationResult) {
	// Record basic metrics
	result.SetMetric("vcount", fmt.Sprintf("%d", gv.VCount))
	result.SetMetric("ecount", fmt.Sprintf("%d", gv.ECount))
	result.SetMetric("directed", fmt.Sprintf("%t", gv.Directed))
	result.SetMetric("memory_bytes", fmt.Sprintf("%d", gv.EstimateMemory()))

	// Calculate density
	if gv.VCount > 1 {
		maxEdges := float64(gv.VCount * (gv.VCount - 1))
		if !gv.Directed {
			maxEdges /= 2
		}
		density := float64(gv.ECount) / maxEdges
		result.SetMetric("density", fmt.Sprintf("%.6f", density))
	} else {
		result.SetMetric("density", "0")
	}

	// Check 1: Vertex count consistency
	if uint64(len(gv.indexToNodeID)) != gv.VCount {
		result.AddError("VCOUNT_MISMATCH",
			fmt.Sprintf("vertex count mismatch: VCount=%d, indexToNodeID length=%d",
				gv.VCount, len(gv.indexToNodeID)),
			map[string]interface{}{
				"expected": gv.VCount,
				"actual":   len(gv.indexToNodeID),
			})
	}

	// Check 2: Node ID mapping consistency
	if len(gv.nodeIDToIndex) != len(gv.indexToNodeID) {
		result.AddError("MAPPING_SIZE_MISMATCH",
			fmt.Sprintf("node mapping size mismatch: nodeIDToIndex=%d, indexToNodeID=%d",
				len(gv.nodeIDToIndex), len(gv.indexToNodeID)),
			nil)
	}

	// Check 3: Edge array lengths consistency
	if len(gv.EdgeSrc) != len(gv.EdgeDst) {
		result.AddError("EDGE_ARRAY_MISMATCH",
			fmt.Sprintf("edge array length mismatch: EdgeSrc=%d, EdgeDst=%d",
				len(gv.EdgeSrc), len(gv.EdgeDst)),
			nil)
	}

	if uint64(len(gv.EdgeSrc)) != gv.ECount {
		result.AddError("ECOUNT_MISMATCH",
			fmt.Sprintf("edge count mismatch: ECount=%d, EdgeSrc length=%d",
				gv.ECount, len(gv.EdgeSrc)),
			nil)
	}

	// Check 4: Edge kind array (if present)
	if gv.EdgeKind != nil && len(gv.EdgeKind) != len(gv.EdgeSrc) {
		result.AddError("EDGE_KIND_MISMATCH",
			fmt.Sprintf("EdgeKind length mismatch: expected=%d, actual=%d",
				len(gv.EdgeSrc), len(gv.EdgeKind)),
			nil)
	}

	// Check 5: Edge weight array (if present)
	if gv.EdgeWeight != nil && len(gv.EdgeWeight) != len(gv.EdgeSrc) {
		result.AddError("EDGE_WEIGHT_MISMATCH",
			fmt.Sprintf("EdgeWeight length mismatch: expected=%d, actual=%d",
				len(gv.EdgeSrc), len(gv.EdgeWeight)),
			nil)
	}

	// Check 6: Validate edge indices are within bounds
	invalidSrcCount := 0
	invalidDstCount := 0
	for i := 0; i < len(gv.EdgeSrc); i++ {
		if uint64(gv.EdgeSrc[i]) >= gv.VCount {
			invalidSrcCount++
		}
		if uint64(gv.EdgeDst[i]) >= gv.VCount {
			invalidDstCount++
		}
	}

	if invalidSrcCount > 0 {
		result.AddError("INVALID_EDGE_SRC",
			fmt.Sprintf("%d edges have source index out of bounds (>= %d)",
				invalidSrcCount, gv.VCount),
			map[string]interface{}{
				"invalid_count": invalidSrcCount,
			})
	}

	if invalidDstCount > 0 {
		result.AddError("INVALID_EDGE_DST",
			fmt.Sprintf("%d edges have destination index out of bounds (>= %d)",
				invalidDstCount, gv.VCount),
			map[string]interface{}{
				"invalid_count": invalidDstCount,
			})
	}

	// Check 7: Detect orphan vertices (no edges)
	if gv.VCount > 0 && gv.ECount > 0 {
		connectedNodes := make(map[uint32]bool)
		for i := 0; i < len(gv.EdgeSrc); i++ {
			connectedNodes[gv.EdgeSrc[i]] = true
			connectedNodes[gv.EdgeDst[i]] = true
		}
		orphanCount := int(gv.VCount) - len(connectedNodes)
		if orphanCount > 0 {
			result.AddWarning(fmt.Sprintf("%d orphan vertices (no edges)", orphanCount))
			result.SetMetric("orphan_vertices", fmt.Sprintf("%d", orphanCount))
		} else {
			result.SetMetric("orphan_vertices", "0")
		}
	}

	// Check 8: Empty graph warning
	if gv.VCount == 0 {
		result.AddWarning("graph has no vertices")
	} else if gv.ECount == 0 {
		result.AddWarning("graph has no edges")
	}
}

// validateDeep performs comprehensive validation checks.
func validateDeep(gv *GraphVersion, result *GraphValidationResult) {
	// Deep Check 1: Bidirectional mapping consistency
	for nodeID, idx := range gv.nodeIDToIndex {
		if int(idx) >= len(gv.indexToNodeID) {
			result.AddError("MAPPING_INDEX_OUT_OF_BOUNDS",
				fmt.Sprintf("nodeIDToIndex maps %d to index %d, but indexToNodeID has only %d entries",
					nodeID, idx, len(gv.indexToNodeID)),
				nil)
			continue
		}

		reverseID := gv.indexToNodeID[idx]
		if reverseID != nodeID {
			result.AddError("MAPPING_INCONSISTENT",
				fmt.Sprintf("bidirectional mapping inconsistent: nodeID %d -> index %d -> nodeID %d",
					nodeID, idx, reverseID),
				nil)
		}
	}

	// Deep Check 2: Check for NaN/Inf in weights
	if gv.EdgeWeight != nil {
		nanCount := 0
		infCount := 0
		negativeCount := 0

		for _, w := range gv.EdgeWeight {
			if math.IsNaN(float64(w)) {
				nanCount++
			} else if math.IsInf(float64(w), 0) {
				infCount++
			} else if w < 0 {
				negativeCount++
			}
		}

		if nanCount > 0 {
			result.AddError("WEIGHT_NAN",
				fmt.Sprintf("%d edges have NaN weights", nanCount),
				map[string]interface{}{"count": nanCount})
		}

		if infCount > 0 {
			result.AddError("WEIGHT_INF",
				fmt.Sprintf("%d edges have Inf weights", infCount),
				map[string]interface{}{"count": infCount})
		}

		if negativeCount > 0 {
			result.AddWarning(fmt.Sprintf("%d edges have negative weights", negativeCount))
			result.SetMetric("negative_weight_edges", fmt.Sprintf("%d", negativeCount))
		}
	}

	// Deep Check 3: Detect duplicate edges
	edgeSet := make(map[uint64]int) // packed edge -> count
	duplicateCount := 0
	for i := 0; i < len(gv.EdgeSrc); i++ {
		// Pack src and dst into single uint64 for fast lookup
		key := (uint64(gv.EdgeSrc[i]) << 32) | uint64(gv.EdgeDst[i])
		edgeSet[key]++
		if edgeSet[key] > 1 {
			duplicateCount++
		}
	}

	if duplicateCount > 0 {
		result.AddWarning(fmt.Sprintf("%d duplicate edges detected", duplicateCount))
		result.SetMetric("duplicate_edges", fmt.Sprintf("%d", duplicateCount))
	} else {
		result.SetMetric("duplicate_edges", "0")
	}

	// Deep Check 4: Detect self-loops
	selfLoopCount := 0
	for i := 0; i < len(gv.EdgeSrc); i++ {
		if gv.EdgeSrc[i] == gv.EdgeDst[i] {
			selfLoopCount++
		}
	}

	if selfLoopCount > 0 {
		result.AddWarning(fmt.Sprintf("%d self-loops detected", selfLoopCount))
	}
	result.SetMetric("self_loops", fmt.Sprintf("%d", selfLoopCount))

	// Deep Check 5: Component connectivity analysis (using Union-Find)
	if gv.VCount > 0 && gv.ECount > 0 {
		numComponents := countComponentsForValidation(gv)
		result.SetMetric("connected_components", fmt.Sprintf("%d", numComponents))

		if numComponents > 1 {
			result.AddWarning(fmt.Sprintf("graph has %d disconnected components", numComponents))
		}
	}

	// Deep Check 6: Degree distribution stats
	if gv.VCount > 0 {
		inDegree := make([]int, gv.VCount)
		outDegree := make([]int, gv.VCount)

		for i := 0; i < len(gv.EdgeSrc); i++ {
			outDegree[gv.EdgeSrc[i]]++
			inDegree[gv.EdgeDst[i]]++
		}

		var maxIn, maxOut, minIn, minOut int
		var sumIn, sumOut int
		minIn = int(gv.ECount) + 1
		minOut = int(gv.ECount) + 1

		for i := 0; i < int(gv.VCount); i++ {
			if inDegree[i] > maxIn {
				maxIn = inDegree[i]
			}
			if inDegree[i] < minIn {
				minIn = inDegree[i]
			}
			if outDegree[i] > maxOut {
				maxOut = outDegree[i]
			}
			if outDegree[i] < minOut {
				minOut = outDegree[i]
			}
			sumIn += inDegree[i]
			sumOut += outDegree[i]
		}

		avgIn := float64(sumIn) / float64(gv.VCount)
		avgOut := float64(sumOut) / float64(gv.VCount)

		result.SetMetric("max_in_degree", fmt.Sprintf("%d", maxIn))
		result.SetMetric("max_out_degree", fmt.Sprintf("%d", maxOut))
		result.SetMetric("min_in_degree", fmt.Sprintf("%d", minIn))
		result.SetMetric("min_out_degree", fmt.Sprintf("%d", minOut))
		result.SetMetric("avg_in_degree", fmt.Sprintf("%.2f", avgIn))
		result.SetMetric("avg_out_degree", fmt.Sprintf("%.2f", avgOut))
	}
}

// countComponentsForValidation counts the number of connected components using Union-Find.
func countComponentsForValidation(gv *GraphVersion) int {
	n := int(gv.VCount)
	parent := make([]int, n)
	rank := make([]int, n)

	for i := 0; i < n; i++ {
		parent[i] = i
	}

	var find func(x int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}

	union := func(x, y int) {
		px, py := find(x), find(y)
		if px == py {
			return
		}
		if rank[px] < rank[py] {
			parent[px] = py
		} else if rank[px] > rank[py] {
			parent[py] = px
		} else {
			parent[py] = px
			rank[px]++
		}
	}

	// Union all edges (treating as undirected for connectivity)
	for i := 0; i < len(gv.EdgeSrc); i++ {
		union(int(gv.EdgeSrc[i]), int(gv.EdgeDst[i]))
	}

	// Count unique roots
	roots := make(map[int]bool)
	for i := 0; i < n; i++ {
		roots[find(i)] = true
	}

	return len(roots)
}

// ValidateViewGraph performs validation on a View.
func ValidateViewGraph(view *View, deep bool) *GraphValidationResult {
	result := NewGraphValidationResult()

	// Basic view metrics
	result.SetMetric("view_id", view.ID)
	result.SetMetric("version_id", view.VersionID)
	result.SetMetric("vcount", fmt.Sprintf("%d", view.VCount))
	result.SetMetric("ecount", fmt.Sprintf("%d", view.ECount))

	// Check vertex mask consistency
	if view.VertexMaskBitmap() != nil && view.version != nil {
		// Count vertices in mask using roaring bitmap cardinality
		maskCount := view.VertexMaskBitmap().GetCardinality()
		if maskCount != view.VCount {
			result.AddWarning(fmt.Sprintf("VCount %d doesn't match mask count %d",
				view.VCount, maskCount))
		}
	}

	// Check edge mask consistency
	if view.EdgeMaskBitmap() != nil && view.version != nil {
		// Count edges in mask using roaring bitmap cardinality
		maskCount := view.EdgeMaskBitmap().GetCardinality()
		if maskCount != view.ECount {
			result.AddWarning(fmt.Sprintf("ECount %d doesn't match mask count %d",
				view.ECount, maskCount))
		}
	}

	return result
}
