package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// PageRankResult contains the result of PageRank computation.
type PageRankResult struct {
	// Scores contains the PageRank score for each vertex.
	// Higher scores indicate more "important" nodes in the link structure.
	Scores []float64

	// MaxScore is the maximum PageRank score found.
	MaxScore float64

	// MinScore is the minimum PageRank score found.
	MinScore float64

	// Iterations is the number of iterations performed.
	Iterations uint32

	// Converged indicates whether the algorithm converged.
	Converged bool

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// PageRankConfig contains configuration for PageRank computation.
type PageRankConfig struct {
	// Damping is the damping factor (probability of following a link vs random jump).
	// Typical value: 0.85. Default: 0.85.
	Damping float64

	// MaxIterations is the maximum number of iterations.
	// Default: 100.
	MaxIterations uint32

	// Epsilon is the convergence tolerance (stop when change < epsilon).
	// Default: 1e-6.
	Epsilon float64

	// WeightColumn specifies the edge weight column to use (empty = unweighted).
	WeightColumn string
}

// PageRankShimConfig holds shim configuration for PageRank computation.
type PageRankShimConfig struct {
	// ShimGraph is the shim graph to use (required).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultPageRankConfig returns a PageRankConfig with sensible defaults.
func DefaultPageRankConfig() *PageRankConfig {
	return &PageRankConfig{
		Damping:       0.85,
		MaxIterations: 100,
		Epsilon:       1e-6,
		WeightColumn:  "",
	}
}

// ComputePageRank computes the PageRank of all vertices.
//
// PageRank is an algorithm used to rank web pages based on their link structure.
// In a network context, it measures the "importance" of nodes based on the
// structure of incoming links. Nodes with high PageRank receive many links
// from other high-PageRank nodes.
//
// PageRank computation requires the igraph shim.
func ComputePageRank(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	config *PageRankConfig,
	shimCfg *PageRankShimConfig,
) (*PageRankResult, error) {
	if config == nil {
		config = DefaultPageRankConfig()
	}

	// PageRank requires the igraph shim
	if shimCfg == nil || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("pagerank computation requires igraph shim")
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

	// Compute PageRank using the shim
	result, err := computePageRankShim(version, shimCfg, config, weights)
	if err != nil {
		return nil, fmt.Errorf("igraph shim pagerank failed: %w", err)
	}

	return result, nil
}

// computePageRankShim computes PageRank using the igraph C shim.
func computePageRankShim(
	version *GraphVersion,
	shimCfg *PageRankShimConfig,
	config *PageRankConfig,
	weights []float64,
) (*PageRankResult, error) {
	var shimResult *shim.PageRankResult
	var err error

	if shimCfg.ShimView != nil {
		shimResult, err = shimCfg.ShimGraph.PageRankOnView(
			shimCfg.ShimView,
			config.Damping,
			config.MaxIterations,
			config.Epsilon,
			weights,
		)
	} else {
		shimResult, err = shimCfg.ShimGraph.PageRank(
			config.Damping,
			config.MaxIterations,
			config.Epsilon,
			weights,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("shim.PageRank failed: %w", err)
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

	result := &PageRankResult{
		Scores:     shimResult.Scores,
		MaxScore:   maxScore,
		MinScore:   minScore,
		Iterations: shimResult.Iterations,
		Converged:  shimResult.Converged,
		Meta:       make(map[string]string),
	}

	result.Meta["source"] = "igraph"
	result.Meta["damping"] = fmt.Sprintf("%.4f", config.Damping)
	result.Meta["max_iterations"] = fmt.Sprintf("%d", config.MaxIterations)
	result.Meta["epsilon"] = fmt.Sprintf("%.2e", config.Epsilon)
	result.Meta["iterations"] = fmt.Sprintf("%d", shimResult.Iterations)
	result.Meta["converged"] = fmt.Sprintf("%t", shimResult.Converged)
	result.Meta["max_score"] = fmt.Sprintf("%.6f", maxScore)
	result.Meta["min_score"] = fmt.Sprintf("%.6f", minScore)

	return result, nil
}

// =============================================================================
// Helper functions for PageRank analysis
// =============================================================================

// GetVertexPageRank returns the PageRank score for a specific vertex.
func (r *PageRankResult) GetVertexPageRank(vertexIdx uint32) (float64, bool) {
	if int(vertexIdx) >= len(r.Scores) {
		return 0, false
	}
	return r.Scores[vertexIdx], true
}

// GetVertexPageRankByExternalID returns the PageRank for a vertex given its external ID.
func (r *PageRankResult) GetVertexPageRankByExternalID(nodeID uint64, version *GraphVersion) (float64, bool) {
	vertexIdx, ok := version.GetNodeIndex(nodeID)
	if !ok {
		return 0, false
	}
	return r.GetVertexPageRank(vertexIdx)
}

// GetTopKVertices returns the top K vertices by PageRank score.
func (r *PageRankResult) GetTopKVertices(k int) []struct {
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

// ValidatePageRankRequest validates a PageRank request.
func ValidatePageRankRequest(damping float64) error {
	if damping < 0 || damping >= 1 {
		return fmt.Errorf("damping must be in range [0, 1), got %.4f", damping)
	}
	return nil
}

// HashPageRankParams generates a hash for PageRank cache key normalization.
func HashPageRankParams(damping float64, maxIterations uint32, epsilon float64, weightColumn string, viewHash string) string {
	return fmt.Sprintf("pagerank:%.4f:%d:%.2e:%s:%s", damping, maxIterations, epsilon, weightColumn, viewHash)
}
