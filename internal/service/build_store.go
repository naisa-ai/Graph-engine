// Package service provides core business logic for graph-engine.
package service

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Build represents an in-progress graph build.
type Build struct {
	ID        string
	GraphName string
	Directed  bool
	CreatedAt time.Time

	// Vertex data
	VerticesU64 []uint64
	VerticesStr []string

	// Edge data
	EdgesSrcU64 []uint64
	EdgesDstU64 []uint64
	EdgesSrcStr []string
	EdgesDstStr []string
	EdgesKind   []uint32
	EdgesWeight []float32

	// Metadata
	Labels map[string]string

	mu sync.RWMutex
}

// VertexCount returns the number of vertices in the build.
func (b *Build) VertexCount() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return uint64(len(b.VerticesU64) + len(b.VerticesStr))
}

// EdgeCount returns the number of edges in the build.
func (b *Build) EdgeCount() uint64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Use src arrays as the canonical count
	return uint64(len(b.EdgesSrcU64) + len(b.EdgesSrcStr))
}

// BuildStore provides thread-safe storage for in-progress builds.
type BuildStore struct {
	builds map[string]*Build
	mu     sync.RWMutex

	// Limits
	maxBuilds int
}

// NewBuildStore creates a new BuildStore with the specified limits.
func NewBuildStore(maxBuilds int) *BuildStore {
	return &BuildStore{
		builds:    make(map[string]*Build),
		maxBuilds: maxBuilds,
	}
}

// CreateBuild creates a new build and returns its ID.
func (s *BuildStore) CreateBuild(graphName string, directed bool, labels map[string]string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.builds) >= s.maxBuilds {
		return "", fmt.Errorf("maximum number of pending builds (%d) reached", s.maxBuilds)
	}

	buildID := uuid.New().String()
	s.builds[buildID] = &Build{
		ID:        buildID,
		GraphName: graphName,
		Directed:  directed,
		CreatedAt: time.Now(),
		Labels:    labels,
	}

	return buildID, nil
}

// GetBuild returns a build by ID, or nil if not found.
func (s *BuildStore) GetBuild(buildID string) *Build {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.builds[buildID]
}

// DeleteBuild removes a build from the store.
func (s *BuildStore) DeleteBuild(buildID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.builds[buildID]; exists {
		delete(s.builds, buildID)
		return true
	}
	return false
}

// AddVerticesU64 adds uint64 vertex IDs to a build.
func (s *BuildStore) AddVerticesU64(buildID string, vertices []uint64) error {
	build := s.GetBuild(buildID)
	if build == nil {
		return fmt.Errorf("build not found: %s", buildID)
	}

	build.mu.Lock()
	defer build.mu.Unlock()
	build.VerticesU64 = append(build.VerticesU64, vertices...)
	return nil
}

// AddVerticesStr adds string vertex IDs to a build.
func (s *BuildStore) AddVerticesStr(buildID string, vertices []string) error {
	build := s.GetBuild(buildID)
	if build == nil {
		return fmt.Errorf("build not found: %s", buildID)
	}

	build.mu.Lock()
	defer build.mu.Unlock()
	build.VerticesStr = append(build.VerticesStr, vertices...)
	return nil
}

// AddEdgesU64 adds edges with uint64 node IDs to a build.
func (s *BuildStore) AddEdgesU64(buildID string, src, dst []uint64, kind []uint32, weight []float32) error {
	if len(src) != len(dst) {
		return fmt.Errorf("src and dst arrays must have same length")
	}

	build := s.GetBuild(buildID)
	if build == nil {
		return fmt.Errorf("build not found: %s", buildID)
	}

	build.mu.Lock()
	defer build.mu.Unlock()
	build.EdgesSrcU64 = append(build.EdgesSrcU64, src...)
	build.EdgesDstU64 = append(build.EdgesDstU64, dst...)

	if len(kind) > 0 {
		build.EdgesKind = append(build.EdgesKind, kind...)
	}
	if len(weight) > 0 {
		build.EdgesWeight = append(build.EdgesWeight, weight...)
	}

	return nil
}

// AddEdgesStr adds edges with string node IDs to a build.
func (s *BuildStore) AddEdgesStr(buildID string, src, dst []string, kind []uint32, weight []float32) error {
	if len(src) != len(dst) {
		return fmt.Errorf("src and dst arrays must have same length")
	}

	build := s.GetBuild(buildID)
	if build == nil {
		return fmt.Errorf("build not found: %s", buildID)
	}

	build.mu.Lock()
	defer build.mu.Unlock()
	build.EdgesSrcStr = append(build.EdgesSrcStr, src...)
	build.EdgesDstStr = append(build.EdgesDstStr, dst...)

	if len(kind) > 0 {
		build.EdgesKind = append(build.EdgesKind, kind...)
	}
	if len(weight) > 0 {
		build.EdgesWeight = append(build.EdgesWeight, weight...)
	}

	return nil
}

// ListBuilds returns all build IDs in the store.
func (s *BuildStore) ListBuilds() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.builds))
	for id := range s.builds {
		ids = append(ids, id)
	}
	return ids
}

// Count returns the number of builds in the store.
func (s *BuildStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.builds)
}

// ValidationLimits defines size limits for validation.
type ValidationLimits struct {
	MaxVertices uint64
	MaxEdges    uint64
}

// DefaultValidationLimits returns default validation limits.
func DefaultValidationLimits() ValidationLimits {
	return ValidationLimits{
		MaxVertices: 1_000_000,  // 1M vertices
		MaxEdges:    10_000_000, // 10M edges
	}
}

// ValidationError contains details about a validation failure.
type ValidationError struct {
	Code    string
	Message string
	Details map[string]interface{}
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// ValidationResult contains the result of build validation.
type ValidationResult struct {
	Valid    bool
	Errors   []*ValidationError
	Warnings []string
}

// AddError adds an error to the validation result.
func (r *ValidationResult) AddError(code, message string, details map[string]interface{}) {
	r.Valid = false
	r.Errors = append(r.Errors, &ValidationError{
		Code:    code,
		Message: message,
		Details: details,
	})
}

// AddWarning adds a warning to the validation result.
func (r *ValidationResult) AddWarning(message string) {
	r.Warnings = append(r.Warnings, message)
}

// Validate checks the build for errors.
func (b *Build) Validate(limits ValidationLimits) *ValidationResult {
	b.mu.RLock()
	defer b.mu.RUnlock()

	result := &ValidationResult{Valid: true}

	// Check for duplicate node IDs
	b.validateDuplicateNodes(result)

	// Check edge array consistency
	b.validateEdgeArrays(result)

	// Check edge endpoints exist
	b.validateEdgeEndpoints(result)

	// Check size limits
	b.validateSizeLimits(result, limits)

	// Check for empty graph
	if len(b.VerticesU64) == 0 && len(b.VerticesStr) == 0 {
		if len(b.EdgesSrcU64) == 0 && len(b.EdgesSrcStr) == 0 {
			result.AddWarning("Graph has no vertices and no edges")
		}
	}

	return result
}

// validateDuplicateNodes checks for duplicate node IDs.
func (b *Build) validateDuplicateNodes(result *ValidationResult) {
	// Check uint64 vertices
	seenU64 := make(map[uint64]int)
	for i, id := range b.VerticesU64 {
		if prevIdx, exists := seenU64[id]; exists {
			result.AddError(
				"DUPLICATE_NODE_ID",
				fmt.Sprintf("Duplicate node ID: %d (first at index %d, duplicate at index %d)", id, prevIdx, i),
				map[string]interface{}{
					"node_id":         id,
					"first_index":     prevIdx,
					"duplicate_index": i,
				},
			)
		}
		seenU64[id] = i
	}

	// Check string vertices
	seenStr := make(map[string]int)
	for i, id := range b.VerticesStr {
		if prevIdx, exists := seenStr[id]; exists {
			result.AddError(
				"DUPLICATE_NODE_ID",
				fmt.Sprintf("Duplicate node ID: %s (first at index %d, duplicate at index %d)", id, prevIdx, i),
				map[string]interface{}{
					"node_id":         id,
					"first_index":     prevIdx,
					"duplicate_index": i,
				},
			)
		}
		seenStr[id] = i
	}
}

// validateEdgeArrays checks that edge arrays have consistent lengths.
func (b *Build) validateEdgeArrays(result *ValidationResult) {
	// Check uint64 edges
	if len(b.EdgesSrcU64) != len(b.EdgesDstU64) {
		result.AddError(
			"EDGE_ARRAY_MISMATCH",
			fmt.Sprintf("Edge src/dst array length mismatch: src=%d, dst=%d",
				len(b.EdgesSrcU64), len(b.EdgesDstU64)),
			map[string]interface{}{
				"src_length": len(b.EdgesSrcU64),
				"dst_length": len(b.EdgesDstU64),
			},
		)
	}

	// Check string edges
	if len(b.EdgesSrcStr) != len(b.EdgesDstStr) {
		result.AddError(
			"EDGE_ARRAY_MISMATCH",
			fmt.Sprintf("Edge src/dst array length mismatch: src=%d, dst=%d",
				len(b.EdgesSrcStr), len(b.EdgesDstStr)),
			map[string]interface{}{
				"src_length": len(b.EdgesSrcStr),
				"dst_length": len(b.EdgesDstStr),
			},
		)
	}

	// Check kind array if present
	totalEdges := len(b.EdgesSrcU64) + len(b.EdgesSrcStr)
	if len(b.EdgesKind) > 0 && len(b.EdgesKind) != totalEdges {
		result.AddError(
			"EDGE_ATTRIBUTE_MISMATCH",
			fmt.Sprintf("Edge kind array length mismatch: kind=%d, edges=%d",
				len(b.EdgesKind), totalEdges),
			map[string]interface{}{
				"kind_length":  len(b.EdgesKind),
				"edges_length": totalEdges,
			},
		)
	}

	// Check weight array if present
	if len(b.EdgesWeight) > 0 && len(b.EdgesWeight) != totalEdges {
		result.AddError(
			"EDGE_ATTRIBUTE_MISMATCH",
			fmt.Sprintf("Edge weight array length mismatch: weight=%d, edges=%d",
				len(b.EdgesWeight), totalEdges),
			map[string]interface{}{
				"weight_length": len(b.EdgesWeight),
				"edges_length":  totalEdges,
			},
		)
	}
}

// validateEdgeEndpoints checks that all edge endpoints exist in the vertex set.
func (b *Build) validateEdgeEndpoints(result *ValidationResult) {
	// Build vertex sets
	vertexSetU64 := make(map[uint64]struct{}, len(b.VerticesU64))
	for _, id := range b.VerticesU64 {
		vertexSetU64[id] = struct{}{}
	}

	vertexSetStr := make(map[string]struct{}, len(b.VerticesStr))
	for _, id := range b.VerticesStr {
		vertexSetStr[id] = struct{}{}
	}

	// If no explicit vertices, skip this check (vertices inferred from edges)
	if len(b.VerticesU64) == 0 && len(b.VerticesStr) == 0 {
		return
	}

	// Check uint64 edges
	invalidCount := 0
	for i, src := range b.EdgesSrcU64 {
		if _, exists := vertexSetU64[src]; !exists {
			invalidCount++
			if invalidCount <= 5 { // Limit reported errors
				result.AddError(
					"INVALID_EDGE_ENDPOINT",
					fmt.Sprintf("Edge %d source node %d not in vertex set", i, src),
					map[string]interface{}{
						"edge_index": i,
						"node_id":    src,
						"endpoint":   "source",
					},
				)
			}
		}
	}
	for i, dst := range b.EdgesDstU64 {
		if _, exists := vertexSetU64[dst]; !exists {
			invalidCount++
			if invalidCount <= 5 {
				result.AddError(
					"INVALID_EDGE_ENDPOINT",
					fmt.Sprintf("Edge %d destination node %d not in vertex set", i, dst),
					map[string]interface{}{
						"edge_index": i,
						"node_id":    dst,
						"endpoint":   "destination",
					},
				)
			}
		}
	}

	// Check string edges
	for i, src := range b.EdgesSrcStr {
		if _, exists := vertexSetStr[src]; !exists {
			invalidCount++
			if invalidCount <= 5 {
				result.AddError(
					"INVALID_EDGE_ENDPOINT",
					fmt.Sprintf("Edge %d source node %s not in vertex set", i, src),
					map[string]interface{}{
						"edge_index": i,
						"node_id":    src,
						"endpoint":   "source",
					},
				)
			}
		}
	}
	for i, dst := range b.EdgesDstStr {
		if _, exists := vertexSetStr[dst]; !exists {
			invalidCount++
			if invalidCount <= 5 {
				result.AddError(
					"INVALID_EDGE_ENDPOINT",
					fmt.Sprintf("Edge %d destination node %s not in vertex set", i, dst),
					map[string]interface{}{
						"edge_index": i,
						"node_id":    dst,
						"endpoint":   "destination",
					},
				)
			}
		}
	}

	if invalidCount > 5 {
		result.AddWarning(fmt.Sprintf("... and %d more invalid edge endpoints", invalidCount-5))
	}
}

// validateSizeLimits checks that the build doesn't exceed size limits.
func (b *Build) validateSizeLimits(result *ValidationResult, limits ValidationLimits) {
	vertexCount := uint64(len(b.VerticesU64) + len(b.VerticesStr))
	edgeCount := uint64(len(b.EdgesSrcU64) + len(b.EdgesSrcStr))

	if limits.MaxVertices > 0 && vertexCount > limits.MaxVertices {
		result.AddError(
			"SIZE_LIMIT_EXCEEDED",
			fmt.Sprintf("Vertex count %d exceeds limit %d", vertexCount, limits.MaxVertices),
			map[string]interface{}{
				"vertex_count": vertexCount,
				"limit":        limits.MaxVertices,
			},
		)
	}

	if limits.MaxEdges > 0 && edgeCount > limits.MaxEdges {
		result.AddError(
			"SIZE_LIMIT_EXCEEDED",
			fmt.Sprintf("Edge count %d exceeds limit %d", edgeCount, limits.MaxEdges),
			map[string]interface{}{
				"edge_count": edgeCount,
				"limit":      limits.MaxEdges,
			},
		)
	}
}
