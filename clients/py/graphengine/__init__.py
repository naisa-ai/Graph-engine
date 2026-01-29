# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""Graph-engine Python client library."""

from graphengine.client import GraphEngineClient, AsyncGraphEngineClient
from graphengine.builder import GraphBuilder, ViewBuilder
from graphengine.types import (
    PathResult,
    ComponentsResult,
    DistanceMatrix,
    MinCutResult,
    CorridorResult,
    KCoreResult,
    BetweennessResult,
    HealthStatus,
    GraphSummary,
    GraphDetails,
    ValidationResult,
    CacheStats,
    TraceSpan,
    ExportFormat,
    GraphRef,
    ViewRef,
    JobRef,
    ResultRef,
)
from graphengine.exceptions import (
    GraphEngineError,
    NotFoundError,
    TimeoutError,
    CanceledError,
    InvalidArgumentError,
    ResourceExhaustedError,
    InternalError,
    JobFailedError,
)

__version__ = "0.1.0"

__all__ = [
    # Clients
    "GraphEngineClient",
    "AsyncGraphEngineClient",
    # Builders
    "GraphBuilder",
    "ViewBuilder",
    # Reference types
    "GraphRef",
    "ViewRef",
    "JobRef",
    "ResultRef",
    # Result types
    "PathResult",
    "ComponentsResult",
    "DistanceMatrix",
    "MinCutResult",
    "CorridorResult",
    "KCoreResult",
    "BetweennessResult",
    "HealthStatus",
    "GraphSummary",
    "GraphDetails",
    "ValidationResult",
    "CacheStats",
    "TraceSpan",
    "ExportFormat",
    # Exceptions
    "GraphEngineError",
    "NotFoundError",
    "TimeoutError",
    "CanceledError",
    "InvalidArgumentError",
    "ResourceExhaustedError",
    "InternalError",
    "JobFailedError",
]
