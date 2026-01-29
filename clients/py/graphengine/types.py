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
    """Result of connected components computation."""
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
