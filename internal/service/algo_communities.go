package service

import (
	"context"
	"fmt"

	"github.com/naisa-ai/graph-engine/internal/shim"
)

// CommunityAlgorithm specifies which community detection algorithm to use.
type CommunityAlgorithm int

const (
	// CommunityAlgorithmLeiden uses the Leiden algorithm (recommended).
	// Generally produces better quality communities than Louvain.
	CommunityAlgorithmLeiden CommunityAlgorithm = iota

	// CommunityAlgorithmLouvain uses the Louvain algorithm.
	// Faster than Leiden but may produce lower quality communities.
	CommunityAlgorithmLouvain
)

func (a CommunityAlgorithm) String() string {
	switch a {
	case CommunityAlgorithmLeiden:
		return "leiden"
	case CommunityAlgorithmLouvain:
		return "louvain"
	default:
		return "unknown"
	}
}

// CommunitiesResult contains the result of community detection.
type CommunitiesResult struct {
	// Membership maps vertex index to community ID.
	Membership []uint32

	// Modularity is the modularity score of the partition.
	// Higher values indicate better community structure.
	Modularity float64

	// NumCommunities is the number of communities found.
	NumCommunities uint32

	// CommunitySize maps community ID to number of members.
	CommunitySize map[uint32]uint32

	// Meta contains algorithm metadata.
	Meta map[string]string
}

// CommunitiesConfig contains configuration for community detection.
type CommunitiesConfig struct {
	// Algorithm specifies which algorithm to use (Leiden or Louvain).
	Algorithm CommunityAlgorithm

	// Resolution controls community granularity.
	// Higher values produce more, smaller communities.
	// Default: 1.0
	Resolution float64
}

// CommunitiesShimConfig holds shim configuration for community detection.
type CommunitiesShimConfig struct {
	// UseShim enables the igraph shim for community detection.
	UseShim bool

	// FallbackOnError falls back to pure Go if the shim encounters an error.
	// Note: There is no pure Go fallback for community detection,
	// so this will return an error if the shim fails.
	FallbackOnError bool

	// ShimGraph is the shim graph to use (if UseShim is true).
	ShimGraph *shim.Graph

	// ShimView is the shim view to use for view-based queries (optional).
	ShimView *shim.View
}

// DefaultCommunitiesConfig returns a CommunitiesConfig with sensible defaults.
func DefaultCommunitiesConfig() *CommunitiesConfig {
	return &CommunitiesConfig{
		Algorithm:  CommunityAlgorithmLeiden,
		Resolution: 1.0,
	}
}

// ComputeCommunities detects communities in the graph using the specified algorithm.
//
// Community detection is only available via the igraph shim - there is no pure Go fallback.
// When shimCfg.UseShim is false or shimCfg.ShimGraph is nil, returns an error.
func ComputeCommunities(
	ctx context.Context,
	version *GraphVersion,
	view *View,
	config *CommunitiesConfig,
	shimCfg *CommunitiesShimConfig,
) (*CommunitiesResult, error) {
	if config == nil {
		config = DefaultCommunitiesConfig()
	}
	if config.Resolution <= 0 {
		config.Resolution = 1.0
	}

	// Default shim config if nil
	if shimCfg == nil {
		shimCfg = &CommunitiesShimConfig{
			UseShim:         false,
			FallbackOnError: true,
		}
	}

	// Community detection requires the igraph shim
	if !shimCfg.UseShim || shimCfg.ShimGraph == nil {
		return nil, fmt.Errorf("community detection requires igraph shim (no pure Go fallback available)")
	}

	// Check context cancellation
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Compute communities using the shim
	result, err := computeCommunitiesShim(version, shimCfg, config)
	if err != nil {
		if shimCfg.FallbackOnError {
			// No fallback available for community detection
			return nil, fmt.Errorf("igraph shim communities failed (no fallback available): %w", err)
		}
		return nil, fmt.Errorf("igraph shim communities failed: %w", err)
	}

	return result, nil
}

// computeCommunitiesShim computes communities using the igraph C shim.
func computeCommunitiesShim(
	version *GraphVersion,
	shimCfg *CommunitiesShimConfig,
	config *CommunitiesConfig,
) (*CommunitiesResult, error) {
	var shimResult *shim.CommunitiesResult
	var err error

	// Choose algorithm and whether to use view
	if shimCfg.ShimView != nil {
		// Use view-based computation
		switch config.Algorithm {
		case CommunityAlgorithmLeiden:
			shimResult, err = shimCfg.ShimGraph.CommunitiesLeidenOnView(shimCfg.ShimView, config.Resolution)
		case CommunityAlgorithmLouvain:
			shimResult, err = shimCfg.ShimGraph.CommunitiesLouvainOnView(shimCfg.ShimView, config.Resolution)
		default:
			return nil, fmt.Errorf("unknown community algorithm: %d", config.Algorithm)
		}
	} else {
		// Use full graph computation
		switch config.Algorithm {
		case CommunityAlgorithmLeiden:
			shimResult, err = shimCfg.ShimGraph.CommunitiesLeiden(config.Resolution)
		case CommunityAlgorithmLouvain:
			shimResult, err = shimCfg.ShimGraph.CommunitiesLouvain(config.Resolution)
		default:
			return nil, fmt.Errorf("unknown community algorithm: %d", config.Algorithm)
		}
	}

	if err != nil {
		return nil, fmt.Errorf("shim.Communities failed: %w", err)
	}

	// Build community size map
	communitySize := make(map[uint32]uint32)
	for _, cid := range shimResult.Membership {
		communitySize[cid]++
	}

	result := &CommunitiesResult{
		Membership:     shimResult.Membership,
		Modularity:     shimResult.Modularity,
		NumCommunities: shimResult.NumCommunities,
		CommunitySize:  communitySize,
		Meta:           make(map[string]string),
	}

	result.Meta["source"] = "igraph"
	result.Meta["algorithm"] = config.Algorithm.String()
	result.Meta["resolution"] = fmt.Sprintf("%.4f", config.Resolution)

	return result, nil
}

// =============================================================================
// Helper functions for community analysis
// =============================================================================

// GetCommunityMembers returns the vertex indices belonging to a specific community.
func (r *CommunitiesResult) GetCommunityMembers(communityID uint32) []uint32 {
	members := make([]uint32, 0)
	for vertexIdx, cid := range r.Membership {
		if cid == communityID {
			members = append(members, uint32(vertexIdx))
		}
	}
	return members
}

// GetCommunityMembersExternal returns the external vertex IDs belonging to a specific community.
func (r *CommunitiesResult) GetCommunityMembersExternal(communityID uint32, version *GraphVersion) []uint64 {
	members := make([]uint64, 0)
	for vertexIdx, cid := range r.Membership {
		if cid == communityID {
			if nodeID, ok := version.GetNodeID(uint32(vertexIdx)); ok {
				members = append(members, nodeID)
			}
		}
	}
	return members
}

// GetLargestCommunities returns the top N communities by size.
func (r *CommunitiesResult) GetLargestCommunities(n int) []struct {
	CommunityID uint32
	Size        uint32
} {
	// Collect all communities
	type communityInfo struct {
		CommunityID uint32
		Size        uint32
	}
	communities := make([]communityInfo, 0, len(r.CommunitySize))
	for cid, size := range r.CommunitySize {
		communities = append(communities, communityInfo{cid, size})
	}

	// Sort by size (descending)
	for i := 0; i < len(communities)-1; i++ {
		for j := i + 1; j < len(communities); j++ {
			if communities[j].Size > communities[i].Size {
				communities[i], communities[j] = communities[j], communities[i]
			}
		}
	}

	// Return top N
	if n > len(communities) {
		n = len(communities)
	}
	result := make([]struct {
		CommunityID uint32
		Size        uint32
	}, n)
	for i := 0; i < n; i++ {
		result[i].CommunityID = communities[i].CommunityID
		result[i].Size = communities[i].Size
	}
	return result
}

// GetVertexCommunity returns the community ID for a specific vertex.
func (r *CommunitiesResult) GetVertexCommunity(vertexIdx uint32) (uint32, bool) {
	if int(vertexIdx) >= len(r.Membership) {
		return 0, false
	}
	return r.Membership[vertexIdx], true
}

// GetVertexCommunityByExternalID returns the community ID for a vertex given its external ID.
func (r *CommunitiesResult) GetVertexCommunityByExternalID(nodeID uint64, version *GraphVersion) (uint32, bool) {
	vertexIdx, ok := version.GetNodeIndex(nodeID)
	if !ok {
		return 0, false
	}
	return r.GetVertexCommunity(vertexIdx)
}

// AreInSameCommunity checks if two vertices are in the same community.
func (r *CommunitiesResult) AreInSameCommunity(vertexIdx1, vertexIdx2 uint32) bool {
	cid1, ok1 := r.GetVertexCommunity(vertexIdx1)
	cid2, ok2 := r.GetVertexCommunity(vertexIdx2)
	if !ok1 || !ok2 {
		return false
	}
	return cid1 == cid2
}

// =============================================================================
// Validation functions
// =============================================================================

// ValidateCommunitiesRequest validates a communities request.
func ValidateCommunitiesRequest(algorithm string, resolution float64) error {
	switch algorithm {
	case "leiden", "louvain", "":
		// valid
	default:
		return fmt.Errorf("unknown algorithm: %s (expected 'leiden' or 'louvain')", algorithm)
	}

	if resolution < 0 {
		return fmt.Errorf("resolution must be non-negative, got: %f", resolution)
	}

	return nil
}

// ParseCommunityAlgorithm parses a string to CommunityAlgorithm.
func ParseCommunityAlgorithm(s string) (CommunityAlgorithm, error) {
	switch s {
	case "leiden", "":
		return CommunityAlgorithmLeiden, nil
	case "louvain":
		return CommunityAlgorithmLouvain, nil
	default:
		return 0, fmt.Errorf("unknown algorithm: %s", s)
	}
}

// HashCommunitiesParams generates a hash for communities cache key normalization.
func HashCommunitiesParams(algorithm string, resolution float64, viewHash string) string {
	return fmt.Sprintf("communities:%s:%.4f:%s", algorithm, resolution, viewHash)
}
