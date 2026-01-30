package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// ClosenessResult contains the result of closeness centrality computation.
type ClosenessResult struct {
	// Scores contains the closeness centrality score for each vertex.
	// Higher scores indicate vertices that can reach others more quickly.
	Scores []float64

	// MaxScore is the maximum closeness score found.
	MaxScore float64

	// MinScore is the minimum closeness score found.
	MinScore float64

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// ClosenessConfig contains configuration for closeness centrality computation.
type ClosenessConfig struct {
	// Mode specifies the direction mode for directed graphs.
	// 0 = ALL (default, for undirected), 1 = OUT, 2 = IN
	Mode int32

	// Normalized indicates whether to normalize scores.
	// If true, scores are normalized to the 0-1 range.
	Normalized bool

	// WeightColumn specifies the edge weight column to use (empty = unweighted).
	WeightColumn string
}

// ClosenessShimConfig holds shim configuration for closeness computation.
type ClosenessShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultClosenessConfig returns a ClosenessConfig with sensible defaults.
func DefaultClosenessConfig() *ClosenessConfig {
	return &ClosenessConfig{
		Mode:         0,     // ALL
		Normalized:   false, // Raw scores
		WeightColumn: "",    // Unweighted
	}
}

// ComputeCloseness computes the closeness centrality of all vertices.
//
// Closeness centrality measures how close a vertex is to all other vertices.
// It is defined as the reciprocal of the sum of shortest path distances from
// a vertex to all other vertices. Vertices with high closeness can reach
// other vertices more quickly.
//
// For disconnected graphs, closeness is only computed for reachable vertices.
//
// Closeness computation requires the igraph shim.
func ComputeCloseness(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	config *ClosenessConfig,
	shimCfg *ClosenessShimConfig,
) (*ClosenessResult, error) {
	if config == nil {
		config = DefaultClosenessConfig()
	}

	// Closeness requires the igraph shim
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("closeness computation requires igraph shim")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Get edge weights if specified
	var weights []float64
	if config.WeightColumn != "" && version.EdgeColumns != nil {
		if col, ok := version.EdgeColumns[config.WeightColumn]; ok {
			weights = col.F64Val
		}
	}

	// Compute closeness using the shim
	result, err := computeClosenessShim(version, shimCfg, config, weights)
	if err != nil {
		return nil, fmt.Errorf("igraph shim closeness failed: %w", err)
	}

	return result, nil
}

// computeClosenessShim computes closeness using the igraph C shim.
func computeClosenessShim(
	version *GraphVersion,
	shimCfg *ClosenessShimConfig,
	config *ClosenessConfig,
	weights []float64,
) (*ClosenessResult, error) {
	var shimResult *shim.ClosenessResult
	var err error

	mode := shim.ClosenessMode(config.Mode)

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.ClosenessOnView(
			shimCfg.ShimView,
			mode,
			config.Normalized,
			weights,
		)
	} else {
		shimResult, err = shimCfg.ShimGraph.Closeness(
			mode,
			config.Normalized,
			weights,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("shim.Closeness failed: %w", err)
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

	result := &ClosenessResult{
		Scores:   shimResult.Scores,
		MaxScore: maxScore,
		MinScore: minScore,
		Meta:     make(map[string]string),
	}

	result.Meta["source"] = "igraph"
	result.Meta["mode"] = fmt.Sprintf("%d", config.Mode)
	result.Meta["normalized"] = fmt.Sprintf("%t", config.Normalized)
	result.Meta["max_score"] = fmt.Sprintf("%.6f", maxScore)
	result.Meta["min_score"] = fmt.Sprintf("%.6f", minScore)

	return result, nil
}

// =============================================================================
// Helper functions for closeness analysis
// =============================================================================

// GetVertexCloseness returns the closeness score for a specific vertex.
func (r *ClosenessResult) GetVertexCloseness(vertexIdx uint32) (float64, bool) {
	if int(vertexIdx) >= len(r.Scores) {
		return 0, false
	}
	return r.Scores[vertexIdx], true
}

// GetVertexClosenessByExternalID returns the closeness for a vertex given its external ID.
func (r *ClosenessResult) GetVertexClosenessByExternalID(nodeID uint64, version *GraphVersion) (float64, bool) {
	vertexIdx, ok := version.GetNodeIndex(nodeID)
	if !ok {
		return 0, false
	}
	return r.GetVertexCloseness(vertexIdx)
}

// GetTopKVertices returns the top K vertices by closeness score.
func (r *ClosenessResult) GetTopKVertices(k int) []struct {
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

// =============================================================================
// Validation functions
// =============================================================================

// ValidateClosenessRequest validates a closeness request.
func ValidateClosenessRequest(mode int32) error {
	if mode < 0 || mode > 2 {
		return fmt.Errorf("invalid mode: %d (must be 0=ALL, 1=OUT, or 2=IN)", mode)
	}
	return nil
}

// HashClosenessParams generates a hash for closeness cache key normalization.
func HashClosenessParams(mode int32, normalized bool, weightColumn string, viewHash string) string {
	return fmt.Sprintf("closeness:%d:%t:%s:%s", mode, normalized, weightColumn, viewHash)
}
