# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""Graph and View builders for the Graph-engine client."""

from __future__ import annotations

from typing import Any, Dict, List, Optional, Tuple, TYPE_CHECKING

from graphengine.types import GraphRef, ViewRef

if TYPE_CHECKING:
    from graphengine.client import GraphEngineClient

try:
    import numpy as np
    HAS_NUMPY = True
except ImportError:
    HAS_NUMPY = False

# Proto imports
from graphengine.gen import graph_engine_pb2 as gepb


class GraphBuilder:
    """Fluent builder for creating and uploading graphs.
    
    Example:
        >>> graph = (GraphBuilder(client, "my-network")
        ...     .directed(True)
        ...     .add_vertices([1, 2, 3, 4, 5])
        ...     .add_edges([1, 2, 3, 4], [2, 3, 4, 5])
        ...     .publish())
    """

    def __init__(self, client: "GraphEngineClient", graph_name: str):
        """Initialize the builder.
        
        Args:
            client: GraphEngineClient instance
            graph_name: Name for the graph
        """
        self._client = client
        self._graph_name = graph_name
        self._directed = False
        self._labels: Dict[str, str] = {}
        self._vertices: List[int] = []
        self._edges: List[Tuple[int, int, float, int]] = []
        self._chunk_size = 10000

    def directed(self, is_directed: bool = True) -> "GraphBuilder":
        """Set whether the graph is directed.
        
        Args:
            is_directed: True for directed graph
            
        Returns:
            Self for chaining
        """
        self._directed = is_directed
        return self

    def with_labels(self, labels: Dict[str, str]) -> "GraphBuilder":
        """Add labels to the graph.
        
        Args:
            labels: Dictionary of labels
            
        Returns:
            Self for chaining
        """
        self._labels.update(labels)
        return self

    def with_label(self, key: str, value: str) -> "GraphBuilder":
        """Add a single label.
        
        Args:
            key: Label key
            value: Label value
            
        Returns:
            Self for chaining
        """
        self._labels[key] = value
        return self

    def chunk_size(self, size: int) -> "GraphBuilder":
        """Set the upload chunk size.
        
        Args:
            size: Number of items per chunk
            
        Returns:
            Self for chaining
        """
        if size > 0:
            self._chunk_size = size
        return self

    def add_vertices(self, node_ids: List[int]) -> "GraphBuilder":
        """Add vertices to the graph.
        
        Args:
            node_ids: List of vertex IDs
            
        Returns:
            Self for chaining
        """
        self._vertices.extend(node_ids)
        return self

    def add_vertex(self, node_id: int) -> "GraphBuilder":
        """Add a single vertex.
        
        Args:
            node_id: Vertex ID
            
        Returns:
            Self for chaining
        """
        self._vertices.append(node_id)
        return self

    def add_vertices_numpy(self, node_ids: Any) -> "GraphBuilder":
        """Add vertices from a numpy array.
        
        Args:
            node_ids: Numpy array of vertex IDs
            
        Returns:
            Self for chaining
        """
        if not HAS_NUMPY:
            raise ImportError("numpy is required for add_vertices_numpy()")
        self._vertices.extend(node_ids.tolist())
        return self

    def add_edges(
        self,
        sources: List[int],
        destinations: List[int],
    ) -> "GraphBuilder":
        """Add edges to the graph.
        
        Args:
            sources: Source vertex IDs
            destinations: Destination vertex IDs
            
        Returns:
            Self for chaining
            
        Raises:
            ValueError: If sources and destinations have different lengths
        """
        if len(sources) != len(destinations):
            raise ValueError("sources and destinations must have same length")
        
        for src, dst in zip(sources, destinations):
            self._edges.append((src, dst, 0.0, 0))
        return self

    def add_edge(self, source: int, destination: int) -> "GraphBuilder":
        """Add a single edge.
        
        Args:
            source: Source vertex ID
            destination: Destination vertex ID
            
        Returns:
            Self for chaining
        """
        self._edges.append((source, destination, 0.0, 0))
        return self

    def add_weighted_edges(
        self,
        sources: List[int],
        destinations: List[int],
        weights: List[float],
    ) -> "GraphBuilder":
        """Add weighted edges to the graph.
        
        Args:
            sources: Source vertex IDs
            destinations: Destination vertex IDs
            weights: Edge weights
            
        Returns:
            Self for chaining
            
        Raises:
            ValueError: If array lengths don't match
        """
        if len(sources) != len(destinations) or len(sources) != len(weights):
            raise ValueError("sources, destinations, and weights must have same length")
        
        for src, dst, weight in zip(sources, destinations, weights):
            self._edges.append((src, dst, weight, 0))
        return self

    def add_weighted_edge(
        self,
        source: int,
        destination: int,
        weight: float,
    ) -> "GraphBuilder":
        """Add a single weighted edge.
        
        Args:
            source: Source vertex ID
            destination: Destination vertex ID
            weight: Edge weight
            
        Returns:
            Self for chaining
        """
        self._edges.append((source, destination, weight, 0))
        return self

    def add_typed_edges(
        self,
        sources: List[int],
        destinations: List[int],
        kinds: List[int],
    ) -> "GraphBuilder":
        """Add typed edges to the graph.
        
        Args:
            sources: Source vertex IDs
            destinations: Destination vertex IDs
            kinds: Edge types/kinds
            
        Returns:
            Self for chaining
        """
        if len(sources) != len(destinations) or len(sources) != len(kinds):
            raise ValueError("sources, destinations, and kinds must have same length")
        
        for src, dst, kind in zip(sources, destinations, kinds):
            self._edges.append((src, dst, 0.0, kind))
        return self

    def add_edges_numpy(
        self,
        sources: Any,
        destinations: Any,
        weights: Optional[Any] = None,
        kinds: Optional[Any] = None,
    ) -> "GraphBuilder":
        """Add edges from numpy arrays.
        
        Args:
            sources: Numpy array of source vertex IDs
            destinations: Numpy array of destination vertex IDs
            weights: Optional numpy array of weights
            kinds: Optional numpy array of edge types
            
        Returns:
            Self for chaining
        """
        if not HAS_NUMPY:
            raise ImportError("numpy is required for add_edges_numpy()")
        
        sources_list = sources.tolist()
        destinations_list = destinations.tolist()
        weights_list = weights.tolist() if weights is not None else [0.0] * len(sources_list)
        kinds_list = kinds.tolist() if kinds is not None else [0] * len(sources_list)
        
        for src, dst, w, k in zip(sources_list, destinations_list, weights_list, kinds_list):
            self._edges.append((src, dst, w, k))
        return self

    def build(self) -> str:
        """Build the graph without publishing.
        
        Returns:
            Build ID
        """
        # Begin build
        build_id = self._client.begin_build(
            self._graph_name,
            directed=self._directed,
            labels=self._labels,
        )
        
        # Upload vertices in chunks
        for i in range(0, len(self._vertices), self._chunk_size):
            chunk = self._vertices[i:i + self._chunk_size]
            self._upload_vertices(build_id, chunk)
        
        # Upload edges in chunks
        for i in range(0, len(self._edges), self._chunk_size):
            chunk = self._edges[i:i + self._chunk_size]
            self._upload_edges(build_id, chunk)
        
        # Finalize
        self._finalize_upload(build_id)
        
        return build_id

    def _upload_vertices(self, build_id: str, vertices: List[int]) -> None:
        """Upload a chunk of vertices."""
        def generate():
            yield gepb.UploadRequest(
                build_id=build_id,
                vertices=gepb.VertexChunk(node_id_u64=vertices),
            )
        
        self._client._engine_stub.Upload(
            generate(),
            timeout=self._client._timeout,
            metadata=self._client._get_metadata(),
        )

    def _upload_edges(
        self,
        build_id: str,
        edges: List[Tuple[int, int, float, int]],
    ) -> None:
        """Upload a chunk of edges."""
        src = [e[0] for e in edges]
        dst = [e[1] for e in edges]
        weights = [e[2] for e in edges]
        kinds = [e[3] for e in edges]
        
        # Only include non-zero weights and kinds
        has_weights = any(w != 0.0 for w in weights)
        has_kinds = any(k != 0 for k in kinds)
        
        def generate():
            edge_chunk = gepb.EdgeChunk(src_u64=src, dst_u64=dst)
            if has_weights:
                edge_chunk.weight.extend(weights)
            if has_kinds:
                edge_chunk.kind.extend(kinds)
            yield gepb.UploadRequest(build_id=build_id, edges=edge_chunk)
        
        self._client._engine_stub.Upload(
            generate(),
            timeout=self._client._timeout,
            metadata=self._client._get_metadata(),
        )

    def _finalize_upload(self, build_id: str) -> None:
        """Finalize the upload."""
        def generate():
            yield gepb.UploadRequest(build_id=build_id, finalize=True)
        
        self._client._engine_stub.Upload(
            generate(),
            timeout=self._client._timeout,
            metadata=self._client._get_metadata(),
        )

    def publish(self, compute_components: bool = True) -> GraphRef:
        """Build and publish the graph.
        
        Args:
            compute_components: Whether to compute connected components
            
        Returns:
            Reference to the published graph
        """
        build_id = self.build()
        return self._client.publish_build(build_id, compute_components=compute_components)

    @property
    def vertex_count(self) -> int:
        """Number of vertices added."""
        return len(self._vertices)

    @property
    def edge_count(self) -> int:
        """Number of edges added."""
        return len(self._edges)


class ViewBuilder:
    """Fluent builder for creating views.
    
    Example:
        >>> view, vcount, ecount = (ViewBuilder(client, graph)
        ...     .induce_vertices([1, 2, 3, 4, 5])
        ...     .create())
    """

    def __init__(self, client: "GraphEngineClient", graph: GraphRef):
        """Initialize the builder.
        
        Args:
            client: GraphEngineClient instance
            graph: Reference to the graph
        """
        self._client = client
        self._graph = graph
        self._induce_vertices: Optional[List[int]] = None
        self._exclude_vertices: Optional[List[int]] = None
        self._exclude_edges: Optional[List[int]] = None
        self._neighborhood_seeds: Optional[List[int]] = None
        self._neighborhood_hops: int = 1
        self._neighborhood_mode: str = "all"

    def induce_vertices(self, node_ids: List[int]) -> "ViewBuilder":
        """Set the induced vertex set.
        
        Args:
            node_ids: Vertex IDs to include
            
        Returns:
            Self for chaining
        """
        self._induce_vertices = node_ids
        return self

    def exclude_vertices(self, node_ids: List[int]) -> "ViewBuilder":
        """Set vertices to exclude.
        
        Args:
            node_ids: Vertex IDs to exclude
            
        Returns:
            Self for chaining
        """
        self._exclude_vertices = node_ids
        return self

    def exclude_edges(self, edge_ids: List[int]) -> "ViewBuilder":
        """Set edges to exclude.
        
        Args:
            edge_ids: Edge IDs to exclude
            
        Returns:
            Self for chaining
        """
        self._exclude_edges = edge_ids
        return self

    def neighborhood(
        self,
        seeds: List[int],
        hops: int,
        mode: str = "all",
    ) -> "ViewBuilder":
        """Add a neighborhood expansion.
        
        Args:
            seeds: Seed vertex IDs
            hops: Number of hops
            mode: "all", "out", or "in"
            
        Returns:
            Self for chaining
        """
        self._neighborhood_seeds = seeds
        self._neighborhood_hops = hops
        self._neighborhood_mode = mode
        return self

    def create(self) -> Tuple[ViewRef, int, int]:
        """Create the view.
        
        Returns:
            Tuple of (view reference, vertex count, edge count)
        """
        # Build ViewSpec
        spec = gepb.ViewSpec()
        
        if self._induce_vertices:
            spec.induce_vertices_u64.extend(self._induce_vertices)
        
        if self._exclude_vertices:
            spec.exclude_vertices_u64.extend(self._exclude_vertices)
        
        if self._exclude_edges:
            spec.exclude_edges_u64.extend(self._exclude_edges)
        
        if self._neighborhood_seeds:
            mode_map = {
                "all": gepb.NeighborhoodSpec.MODE_ALL,
                "out": gepb.NeighborhoodSpec.MODE_OUT,
                "in": gepb.NeighborhoodSpec.MODE_IN,
            }
            spec.neighborhood.CopyFrom(gepb.NeighborhoodSpec(
                seeds_u64=self._neighborhood_seeds,
                hops=self._neighborhood_hops,
                mode=mode_map.get(self._neighborhood_mode, gepb.NeighborhoodSpec.MODE_ALL),
            ))
        
        # Create view via client
        req = gepb.CreateViewRequest(
            graph=gepb.GraphRef(
                graph_name=self._graph.graph_name,
                version_id=self._graph.version_id,
            ),
            spec=spec,
        )
        
        resp = self._client._engine_stub.CreateView(
            req,
            timeout=self._client._timeout,
            metadata=self._client._get_metadata(),
        )
        
        return (
            ViewRef(view_id=resp.view.view_id),
            resp.vcount,
            resp.ecount,
        )
