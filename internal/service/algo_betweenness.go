package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// BetweennessResult contains the result of betweenness centrality computation.
type BetweennessResult struct {
	// Scores contains the betweenness centrality score for each vertex.
	// Higher scores indicate vertices that lie on more shortest paths.
	Scores []float64

	// MaxScore is the maximum betweenness score found.
	MaxScore float64

	// MinScore is the minimum betweenness score found (usually 0).
	MinScore float64

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// BetweennessConfig contains configuration for betweenness centrality computation.
type BetweennessConfig struct {
	// SampleSize specifies the number of source vertices to sample (0 = all vertices).
	// Sampling significantly speeds up computation for large graphs while
	// providing approximate results.
	SampleSize uint32

	// Normalized indicates whether to normalize scores.
	// If true, scores are normalized by 2/((n-1)(n-2)) for undirected graphs
	// or 1/((n-1)(n-2)) for directed graphs.
	Normalized bool

	// WeightColumn specifies the edge weight column to use (empty = unweighted).
	WeightColumn string
}

// BetweennessShimConfig holds shim configuration for betweenness computation.
type BetweennessShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultBetweennessConfig returns a BetweennessConfig with sensible defaults.
func DefaultBetweennessConfig() *BetweennessConfig {
	return &BetweennessConfig{
		SampleSize:   0,     // Full computation
		Normalized:   false, // Raw scores
		WeightColumn: "",    // Unweighted
	}
}

// ComputeBetweenness computes the betweenness centrality of all vertices.
//
// Betweenness centrality measures the extent to which a vertex lies on
// paths between other vertices. Vertices with high betweenness may have
// considerable influence within a network by virtue of their control
// over information passing between others.
//
// Betweenness computation requires the igraph shim.
func ComputeBetweenness(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	config *BetweennessConfig,
	shimCfg *BetweennessShimConfig,
) (*BetweennessResult, error) {
	if config == nil {
		config = DefaultBetweennessConfig()
	}

	// Betweenness requires the igraph shim
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("betweenness computation requires igraph shim")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default: //nolint:revive // non-blocking context check
	}

	// Get edge weights if specified
	var weights []float64
	if config.WeightColumn != "" && version.EdgeColumns != nil {
		if col, ok := version.EdgeColumns[config.WeightColumn]; ok {
			weights = col.F64Val
		}
	}

	// Compute betweenness using the shim
	result, err := computeBetweennessShim(version, shimCfg, config, weights)
	if err != nil {
		return nil, fmt.Errorf("igraph shim betweenness failed: %w", err)
	}

	return result, nil
}

// computeBetweennessShim computes betweenness using the igraph C shim.
func computeBetweennessShim(
	version *GraphVersion,
	shimCfg *BetweennessShimConfig,
	config *BetweennessConfig,
	weights []float64,
) (*BetweennessResult, error) {
	var shimResult *shim.BetweennessResult
	var err error

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.BetweennessOnView(
			shimCfg.ShimView,
			config.SampleSize,
			config.Normalized,
			weights,
		)
	} else {
		shimResult, err = shimCfg.ShimGraph.Betweenness(
			config.SampleSize,
			config.Normalized,
			weights,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("shim.Betweenness failed: %w", err)
	}

	// Find min/max scores
	var minScore, maxScore float64
	if len(shimResult.Scores) > 0 {
		minScore = shimResult.Scores[0]
		maxScore = shimResult.Scores[0]
		for _, score := range shimResult.Scores[1:] {
			if score < minScore {
				minScore = score
			}
			if score > maxScore {
				maxScore = score
			}
		}
	}

	result := &BetweennessResult{
		Scores:   shimResult.Scores,
		MaxScore: maxScore,
		MinScore: minScore,
		Meta:     make(map[string]string),
	}

	result.Meta["source"] = "igraph"
	result.Meta["sample_size"] = fmt.Sprintf("%d", config.SampleSize)
	result.Meta["normalized"] = fmt.Sprintf("%t", config.Normalized)
	result.Meta["max_score"] = fmt.Sprintf("%.6f", maxScore)
	result.Meta["min_score"] = fmt.Sprintf("%.6f", minScore)

	return result, nil
}

// =============================================================================
// Helper functions for betweenness analysis
// =============================================================================

// GetVertexBetweenness returns the betweenness score for a specific vertex.
func (r *BetweennessResult) GetVertexBetweenness(vertexIdx uint32) (float64, bool) {
	if int(vertexIdx) >= len(r.Scores) {
		return 0, false
	}
	return r.Scores[vertexIdx], true
}

// GetVertexBetweennessByExternalID returns the betweenness for a vertex given its external ID.
func (r *BetweennessResult) GetVertexBetweennessByExternalID(nodeID uint64, version *GraphVersion) (float64, bool) {
	vertexIdx, ok := version.GetNodeIndex(nodeID)
	if !ok {
		return 0, false
	}
	return r.GetVertexBetweenness(vertexIdx)
}

// GetTopKVertices returns the top K vertices by betweenness score.
func (r *BetweennessResult) GetTopKVertices(k int) []struct {
	VertexIdx uint32
	Score     float64
} {
	// Build list of (vertex, score) pairs
	type vertexScore struct {
		VertexIdx uint32
		Score     float64
	}
	pairs := make([]vertexScore, len(r.Scores))
	for i, score := range r.Scores {
		pairs[i] = vertexScore{uint32(i), score}
	}

	// Sort by score descending
	for i := 0; i < len(pairs)-1; i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].Score > pairs[i].Score {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	// Return top K
	if k > len(pairs) {
		k = len(pairs)
	}
	result := make([]struct {
		VertexIdx uint32
		Score     float64
	}, k)
	for i := 0; i < k; i++ {
		result[i].VertexIdx = pairs[i].VertexIdx
		result[i].Score = pairs[i].Score
	}
	return result
}

// GetTopKVerticesExternal returns the top K vertices by betweenness with external IDs.
func (r *BetweennessResult) GetTopKVerticesExternal(k int, version *GraphVersion) []struct {
	NodeID uint64
	Score  float64
} {
	topK := r.GetTopKVertices(k)
	result := make([]struct {
		NodeID uint64
		Score  float64
	}, len(topK))
	for i, v := range topK {
		nodeID, _ := version.GetNodeID(v.VertexIdx)
		result[i].NodeID = nodeID
		result[i].Score = v.Score
	}
	return result
}

// GetVerticesAboveThreshold returns all vertices with betweenness score above a threshold.
func (r *BetweennessResult) GetVerticesAboveThreshold(threshold float64) []uint32 {
	vertices := make([]uint32, 0)
	for i, score := range r.Scores {
		if score > threshold {
			vertices = append(vertices, uint32(i))
		}
	}
	return vertices
}

// =============================================================================
// Validation functions
// =============================================================================

// ValidateBetweennessRequest validates a betweenness request.
func ValidateBetweennessRequest(sampleSize uint32) error {
	// sample_size = 0 means full computation, which is always valid
	return nil
}

// HashBetweennessParams generates a hash for betweenness cache key normalization.
func HashBetweennessParams(sampleSize uint32, normalized bool, weightColumn string, viewHash string) string {
	return fmt.Sprintf("betweenness:%d:%t:%s:%s", sampleSize, normalized, weightColumn, viewHash)
}
