# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""Type definitions for the Graph-engine client."""

from dataclasses import dataclass, field
from datetime import datetime, timedelta
from enum import Enum
from typing import Dict, List, Optional

try:
    import numpy as np
    HAS_NUMPY = True
except ImportError:
    HAS_NUMPY = False


# =============================================================================
# Enums
# =============================================================================

class ExportFormat(Enum):
    """Export format options."""
    EDGE_LIST = 0
    CSV = 1


class ComponentMode(Enum):
    """Connected components mode."""
    WEAK = 0
    STRONG = 1


class CorridorMethod(Enum):
    """Corridor creation method."""
    SHORTEST_PATH_HULL = 0
    KSP_HULL = 1
    COMMUNITY_AWARE = 2


class NeighborhoodMode(Enum):
    """Neighborhood expansion mode."""
    ALL = 0
    OUT = 1
    IN = 2


# =============================================================================
# Result Types
# =============================================================================

@dataclass
class PathResult:
    """Result of a shortest path query."""
    vertices: List[int] = field(default_factory=list)
    edges: List[int] = field(default_factory=list)
    total_cost: float = 0.0


@dataclass
class ComponentsResult:
    """Result of connected components or community detection computation.
    
    Attributes:
        node_ids: External node IDs corresponding to each membership value.
                  node_ids[i] is the external node ID for vertex with internal index i.
        membership: Component/community ID for each vertex.
                    membership[i] is the component/community ID for node node_ids[i].
        num_components: Number of distinct components/communities.
    
    To get a dict mapping node_id -> community_id:
        >>> result = client.communities(graph)
        >>> community_by_node = dict(zip(result.node_ids, result.membership))
    """
    node_ids: List[int] = field(default_factory=list)
    membership: List[int] = field(default_factory=list)
    num_components: int = 0


@dataclass
class DistanceMatrix:
    """Result of distance matrix computation."""
    sources: List[int] = field(default_factory=list)
    targets: List[int] = field(default_factory=list)
    distances: List[List[float]] = field(default_factory=list)

    def to_numpy(self):
        """Convert distances to numpy array (requires numpy)."""
        if not HAS_NUMPY:
            raise ImportError("numpy is required for to_numpy()")
        return np.array(self.distances)


@dataclass
class MinCutResult:
    """Result of s-t minimum cut computation."""
    cut_value: float = 0.0
    source_side_vertices: List[int] = field(default_factory=list)
    cut_edges: List[int] = field(default_factory=list)


@dataclass
class CorridorResult:
    """Result of corridor view creation."""
    view_id: str = ""
    meta: Dict[str, str] = field(default_factory=dict)


@dataclass
class KCoreResult:
    """Result of k-core decomposition.
    
    Attributes:
        node_ids: External node IDs corresponding to each coreness value.
                  node_ids[i] is the external node ID for vertex with internal index i.
        coreness: Coreness value for each vertex.
                  coreness[i] is the coreness value for node node_ids[i].
        max_core: Maximum k found (highest coreness value).
    
    To get a dict mapping node_id -> coreness:
        >>> result = client.k_core(graph)
        >>> coreness_by_node = dict(zip(result.node_ids, result.coreness))
    """
    node_ids: List[int] = field(default_factory=list)
    coreness: List[int] = field(default_factory=list)
    max_core: int = 0


@dataclass
class BetweennessResult:
    """Result of betweenness centrality computation.
    
    Attributes:
        node_ids: External node IDs corresponding to each score value.
                  node_ids[i] is the external node ID for vertex with internal index i.
        scores: Betweenness centrality score per vertex (parallel array with node_ids).
                scores[i] is the betweenness centrality score for the vertex node_ids[i].
    
    To get a dict mapping node_id -> betweenness_score:
        >>> result = client.betweenness(graph)
        >>> betweenness_by_node = dict(zip(result.node_ids, result.scores))
    """
    node_ids: List[int] = field(default_factory=list)
    scores: List[float] = field(default_factory=list)


@dataclass
class ClosenessResult:
    """Result of closeness centrality computation.
    
    Attributes:
        node_ids: External node IDs corresponding to each score value.
                  node_ids[i] is the external node ID for vertex with internal index i.
        scores: Closeness centrality score per vertex (parallel array with node_ids).
                scores[i] is the closeness centrality score for the vertex node_ids[i].
                Higher values indicate nodes that can reach others more quickly.
    
    To get a dict mapping node_id -> closeness_score:
        >>> result = client.closeness(graph)
        >>> closeness_by_node = dict(zip(result.node_ids, result.scores))
    """
    node_ids: List[int] = field(default_factory=list)
    scores: List[float] = field(default_factory=list)


@dataclass
class PageRankResult:
    """Result of PageRank computation.
    
    Attributes:
        node_ids: External node IDs corresponding to each score value.
                  node_ids[i] is the external node ID for vertex with internal index i.
        scores: PageRank score per vertex (parallel array with node_ids).
                scores[i] is the PageRank score for the vertex node_ids[i].
                Higher values indicate more "important" nodes in the link structure.
        iterations: Number of iterations performed.
        converged: Whether the algorithm converged within max_iterations.
    
    To get a dict mapping node_id -> pagerank_score:
        >>> result = client.pagerank(graph)
        >>> pagerank_by_node = dict(zip(result.node_ids, result.scores))
    """
    node_ids: List[int] = field(default_factory=list)
    scores: List[float] = field(default_factory=list)
    iterations: int = 0
    converged: bool = False


# =============================================================================
# Ops Types
# =============================================================================

@dataclass
class HealthStatus:
    """Service health status."""
    status: str = ""
    version: str = ""
    uptime: str = ""
    meta: Dict[str, str] = field(default_factory=dict)


@dataclass
class GraphSummary:
    """Summary information about a graph."""
    graph_name: str = ""
    current_version_id: str = ""
    vcount: int = 0
    ecount: int = 0
    published_at: Optional[datetime] = None


@dataclass
class GraphDetails:
    """Detailed information about a graph."""
    summary: Optional[GraphSummary] = None
    labels: Dict[str, str] = field(default_factory=dict)


@dataclass
class ValidationResult:
    """Result of graph validation."""
    valid: bool = True
    warnings: List[str] = field(default_factory=list)
    metrics: Dict[str, str] = field(default_factory=dict)
    message: str = ""


@dataclass
class CacheStats:
    """Cache statistics."""
    results_items: int = 0
    results_bytes: int = 0
    views_items: int = 0
    views_bytes: int = 0


@dataclass
class TraceSpan:
    """Trace span for job debugging."""
    name: str = ""
    duration: Optional[timedelta] = None
    tags: Dict[str, str] = field(default_factory=dict)


# =============================================================================
# Reference Types
# =============================================================================

@dataclass
class GraphRef:
    """Reference to a graph version."""
    graph_name: str
    version_id: str = ""


@dataclass
class ViewRef:
    """Reference to a view."""
    view_id: str


@dataclass
class JobRef:
    """Reference to a job."""
    job_id: str


@dataclass
class ResultRef:
    """Reference to a result."""
    result_id: str
