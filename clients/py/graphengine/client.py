# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""Graph-engine client implementations."""

from __future__ import annotations

import time
from datetime import timedelta
from typing import (
    Any,
    Callable,
    Dict,
    Iterator,
    List,
    Optional,
    Tuple,
    Union,
)

import grpc

from graphengine.exceptions import (
    GraphEngineError,
    JobFailedError,
    CanceledError,
    TimeoutError as GETimeoutError,
    wrap_grpc_error,
)
from graphengine.types import (
    BetweennessResult,
    CacheStats,
    ComponentsResult,
    CorridorResult,
    DistanceMatrix,
    ExportFormat,
    GraphDetails,
    GraphRef,
    GraphSummary,
    HealthStatus,
    JobRef,
    KCoreResult,
    MinCutResult,
    PathResult,
    ResultRef,
    TraceSpan,
    ValidationResult,
    ViewRef,
)

# Proto imports from generated code
from graphengine.gen import graph_engine_pb2 as gepb
from graphengine.gen import graph_engine_pb2_grpc as gepb_grpc
from graphengine.gen import graph_engine_ops_pb2 as ops_pb
from graphengine.gen import graph_engine_ops_pb2_grpc as ops_grpc


class GraphEngineClient:
    """Synchronous client for the Graph-engine service.
    
    Example:
        >>> with GraphEngineClient("localhost:50051") as client:
        ...     graphs = client.list_graphs()
        ...     for g in graphs:
        ...         print(f"{g.graph_name}: {g.vcount} vertices")
    """

    def __init__(
        self,
        address: str,
        *,
        timeout: float = 30.0,
        secure: bool = False,
        credentials: Optional[grpc.ChannelCredentials] = None,
        metadata: Optional[Dict[str, str]] = None,
        max_retries: int = 3,
        retry_backoff: float = 0.1,
    ):
        """Initialize the client.
        
        Args:
            address: Server address (host:port)
            timeout: Default timeout in seconds
            secure: Use TLS if True (ignored if credentials provided)
            credentials: Custom gRPC credentials
            metadata: Default metadata for all RPCs
            max_retries: Maximum retry attempts for retryable errors
            retry_backoff: Initial retry backoff in seconds
        """
        self._address = address
        self._timeout = timeout
        self._metadata = metadata or {}
        self._max_retries = max_retries
        self._retry_backoff = retry_backoff
        self._channel: Optional[grpc.Channel] = None
        self._engine_stub: Optional[gepb_grpc.GraphEngineStub] = None
        self._ops_stub: Optional[ops_grpc.GraphEngineOpsStub] = None

        # Create channel
        if credentials:
            self._channel = grpc.secure_channel(address, credentials)
        elif secure:
            self._channel = grpc.secure_channel(address, grpc.ssl_channel_credentials())
        else:
            self._channel = grpc.insecure_channel(address)

        # Create stubs
        self._engine_stub = gepb_grpc.GraphEngineStub(self._channel)
        self._ops_stub = ops_grpc.GraphEngineOpsStub(self._channel)

    def __enter__(self) -> "GraphEngineClient":
        return self

    def __exit__(self, exc_type: Any, exc_val: Any, exc_tb: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the client connection."""
        if self._channel:
            self._channel.close()
            self._channel = None

    def _get_metadata(self) -> List[Tuple[str, str]]:
        """Get metadata for RPC calls."""
        return [(k, v) for k, v in self._metadata.items()]

    # =========================================================================
    # Core Graph Operations
    # =========================================================================

    def begin_build(
        self,
        graph_name: str,
        directed: bool = False,
        labels: Optional[Dict[str, str]] = None,
    ) -> str:
        """Start a new graph build.
        
        Args:
            graph_name: Name for the graph
            directed: Whether the graph is directed
            labels: Optional metadata labels
            
        Returns:
            Build ID
        """
        try:
            req = gepb.BeginBuildRequest(
                graph_name=graph_name,
                directed=directed,
                labels=labels or {},
            )
            resp = self._engine_stub.BeginBuild(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return resp.build_id
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def upload(
        self,
        build_id: str,
        vertices: Optional[List[int]] = None,
        edges: Optional[Tuple[List[int], List[int]]] = None,
        weights: Optional[List[float]] = None,
        kinds: Optional[List[int]] = None,
    ) -> Tuple[int, int]:
        """Upload vertices and edges to a build.
        
        Args:
            build_id: Build ID from begin_build
            vertices: List of vertex IDs
            edges: Tuple of (sources, destinations)
            weights: Optional edge weights
            kinds: Optional edge types
            
        Returns:
            Tuple of (received_vertices, received_edges)
        """
        def generate_requests():
            if vertices:
                yield gepb.UploadRequest(
                    build_id=build_id,
                    vertices=gepb.VertexChunk(node_id_u64=vertices),
                )
            if edges:
                src, dst = edges
                edge_chunk = gepb.EdgeChunk(
                    src_u64=src,
                    dst_u64=dst,
                )
                if weights:
                    edge_chunk.weight.extend(weights)
                if kinds:
                    edge_chunk.kind.extend(kinds)
                yield gepb.UploadRequest(
                    build_id=build_id,
                    edges=edge_chunk,
                )
            # Finalize
            yield gepb.UploadRequest(build_id=build_id, finalize=True)
        
        try:
            resp = self._engine_stub.Upload(
                generate_requests(),
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return (resp.received_vertices, resp.received_edges)
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def publish_build(
        self,
        build_id: str,
        compute_components: bool = True,
    ) -> GraphRef:
        """Publish a completed build.
        
        Args:
            build_id: ID from begin_build
            compute_components: Whether to compute connected components
            
        Returns:
            Reference to the published graph
        """
        try:
            req = gepb.PublishBuildRequest(
                build_id=build_id,
                artifacts=gepb.BatchArtifacts(
                    compute_components=compute_components,
                ),
            )
            resp = self._engine_stub.PublishBuild(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return GraphRef(
                graph_name=resp.graph.graph_name,
                version_id=resp.graph.version_id,
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def create_view(
        self,
        graph: GraphRef,
        *,
        induce_vertices: Optional[List[int]] = None,
        exclude_vertices: Optional[List[int]] = None,
        neighborhood_seeds: Optional[List[int]] = None,
        neighborhood_hops: int = 1,
    ) -> Tuple[ViewRef, int, int]:
        """Create a view of a graph.
        
        Args:
            graph: Reference to the graph
            induce_vertices: Vertices to include (induced subgraph)
            exclude_vertices: Vertices to exclude
            neighborhood_seeds: Seeds for neighborhood expansion
            neighborhood_hops: Number of hops for expansion
            
        Returns:
            Tuple of (view reference, vertex count, edge count)
        """
        try:
            spec = gepb.ViewSpec()
            if induce_vertices:
                spec.induce_vertices_u64.extend(induce_vertices)
            if exclude_vertices:
                spec.exclude_vertices_u64.extend(exclude_vertices)
            if neighborhood_seeds:
                spec.neighborhood.CopyFrom(gepb.NeighborhoodSpec(
                    seeds_u64=neighborhood_seeds,
                    hops=neighborhood_hops,
                    mode=gepb.NeighborhoodSpec.MODE_ALL,
                ))
            
            req = gepb.CreateViewRequest(
                graph=gepb.GraphRef(
                    graph_name=graph.graph_name,
                    version_id=graph.version_id,
                ),
                spec=spec,
            )
            resp = self._engine_stub.CreateView(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return (
                ViewRef(view_id=resp.view.view_id),
                resp.vcount,
                resp.ecount,
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def release_view(self, view: ViewRef) -> bool:
        """Release a view.
        
        Args:
            view: View reference
            
        Returns:
            True if released
        """
        try:
            req = gepb.ReleaseRequest(
                view=gepb.ViewRef(view_id=view.view_id),
            )
            resp = self._engine_stub.Release(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return resp.released
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def release_result(self, result: ResultRef) -> bool:
        """Release a result.
        
        Args:
            result: Result reference
            
        Returns:
            True if released
        """
        try:
            req = gepb.ReleaseRequest(
                result=gepb.ResultRef(result_id=result.result_id),
            )
            resp = self._engine_stub.Release(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return resp.released
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    # =========================================================================
    # Low-level Job/Result APIs
    # =========================================================================

    def _run(
        self,
        graph: Optional[GraphRef],
        view: Optional[ViewRef],
        algo: gepb.AlgoSpec,
    ) -> JobRef:
        """Run an algorithm (internal)."""
        try:
            req = gepb.RunRequest(algo=algo)
            if graph:
                req.graph.CopyFrom(gepb.GraphRef(
                    graph_name=graph.graph_name,
                    version_id=graph.version_id,
                ))
            if view:
                req.view.CopyFrom(gepb.ViewRef(view_id=view.view_id))
            
            resp = self._engine_stub.Run(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return JobRef(job_id=resp.job.job_id)
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def get_job(self, job: JobRef) -> Tuple[str, Optional[ResultRef]]:
        """Get job status.
        
        Args:
            job: Job reference
            
        Returns:
            Tuple of (state string, result reference if completed)
        """
        try:
            req = gepb.GetJobRequest(job=gepb.JobRef(job_id=job.job_id))
            resp = self._engine_stub.GetJob(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            state_map = {
                gepb.GetJobResponse.PENDING: "PENDING",
                gepb.GetJobResponse.RUNNING: "RUNNING",
                gepb.GetJobResponse.SUCCEEDED: "SUCCEEDED",
                gepb.GetJobResponse.FAILED: "FAILED",
                gepb.GetJobResponse.CANCELED: "CANCELED",
            }
            state = state_map.get(resp.state, "UNKNOWN")
            result = None
            if resp.HasField("result") and resp.result.result_id:
                result = ResultRef(result_id=resp.result.result_id)
            return (state, result)
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def _get_result_chunks(self, result: ResultRef) -> Iterator[gepb.ResultChunk]:
        """Get result chunks (internal)."""
        try:
            req = gepb.GetResultRequest(result=gepb.ResultRef(result_id=result.result_id))
            for chunk in self._engine_stub.GetResult(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            ):
                yield chunk
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    # =========================================================================
    # Algorithm Methods
    # =========================================================================

    def shortest_path(
        self,
        graph: GraphRef,
        source: int,
        target: int,
        weight_column: str = "",
    ) -> PathResult:
        """Find the shortest path between two vertices.
        
        Args:
            graph: Reference to the graph
            source: Source vertex ID
            target: Target vertex ID
            weight_column: Optional weight column name
            
        Returns:
            Path result with vertices, edges, and cost
        """
        algo = gepb.AlgoSpec(
            shortest_path=gepb.ShortestPathSpec(
                source_u64=source,
                target_u64=target,
                weight_column=weight_column,
                return_vertices=True,
                return_edges=True,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_path_result(result)

    def shortest_path_on_view(
        self,
        view: ViewRef,
        source: int,
        target: int,
        weight_column: str = "",
    ) -> PathResult:
        """Find the shortest path on a view."""
        algo = gepb.AlgoSpec(
            shortest_path=gepb.ShortestPathSpec(
                source_u64=source,
                target_u64=target,
                weight_column=weight_column,
                return_vertices=True,
                return_edges=True,
            )
        )
        job = self._run(None, view, algo)
        result = self.wait_for_job(job)
        return self._collect_path_result(result)

    def _collect_path_result(self, result: ResultRef) -> PathResult:
        """Collect path result from chunks."""
        path = PathResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("shortest_path"):
                sp = chunk.shortest_path
                path.vertices = list(sp.vertices_u64)
                path.edges = list(sp.edges_u64)
                path.total_cost = sp.total_cost
        return path

    def k_shortest_paths(
        self,
        graph: GraphRef,
        source: int,
        target: int,
        k: int,
        weight_column: str = "",
    ) -> List[PathResult]:
        """Find the k shortest paths between two vertices.
        
        Args:
            graph: Reference to the graph
            source: Source vertex ID
            target: Target vertex ID
            k: Number of paths to find
            weight_column: Optional weight column name
            
        Returns:
            List of path results
        """
        algo = gepb.AlgoSpec(
            k_shortest_paths=gepb.KShortestPathsSpec(
                source_u64=source,
                target_u64=target,
                k=k,
                weight_column=weight_column,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_ksp_result(result)

    def _collect_ksp_result(self, result: ResultRef) -> List[PathResult]:
        """Collect k-shortest paths result from chunks."""
        paths = []
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("shortest_path"):
                sp = chunk.shortest_path
                paths.append(PathResult(
                    vertices=list(sp.vertices_u64),
                    edges=list(sp.edges_u64),
                    total_cost=sp.total_cost,
                ))
        return paths

    def components(
        self,
        graph: GraphRef,
        mode: str = "weak",
    ) -> ComponentsResult:
        """Compute connected components.
        
        Args:
            graph: Reference to the graph
            mode: "weak" or "strong"
            
        Returns:
            Components result with membership array
        """
        mode_enum = gepb.ComponentsSpec.WEAK if mode == "weak" else gepb.ComponentsSpec.STRONG
        algo = gepb.AlgoSpec(
            components=gepb.ComponentsSpec(mode=mode_enum)
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_components_result(result)

    def _collect_components_result(self, result: ResultRef) -> ComponentsResult:
        """Collect components result from chunks."""
        comp_result = ComponentsResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("components"):
                cr = chunk.components
                comp_result.node_ids = list(cr.node_ids_u64)
                comp_result.membership = list(cr.membership)
                comp_result.num_components = cr.num_components
            # Fallback for legacy u32 buffer format (pre node_ids)
            elif chunk.HasField("header"):
                comp_result.num_components = int(chunk.header.meta.get("num_components", "0"))
            elif chunk.HasField("u32"):
                comp_result.membership.extend(chunk.u32.values)
        return comp_result

    def distances(
        self,
        graph: GraphRef,
        sources: List[int],
        targets: List[int],
        weight_column: str = "",
    ) -> DistanceMatrix:
        """Compute distance matrix between source and target sets.
        
        Args:
            graph: Reference to the graph
            sources: Source vertex IDs
            targets: Target vertex IDs
            weight_column: Optional weight column name
            
        Returns:
            Distance matrix
        """
        algo = gepb.AlgoSpec(
            distances=gepb.DistancesSpec(
                sources_u64=sources,
                targets_u64=targets,
                weight_column=weight_column,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_distances_result(result, sources, targets)

    def _collect_distances_result(
        self,
        result: ResultRef,
        sources: List[int],
        targets: List[int],
    ) -> DistanceMatrix:
        """Collect distances result from chunks."""
        dist_result = DistanceMatrix(sources=sources, targets=targets)
        flat_distances: List[float] = []
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("f64"):
                flat_distances.extend(chunk.f64.values)
        
        # Reshape flat distances to matrix
        if flat_distances:
            n_targets = len(targets)
            dist_result.distances = [
                flat_distances[i * n_targets:(i + 1) * n_targets]
                for i in range(len(sources))
            ]
        return dist_result

    def min_cut(
        self,
        graph: GraphRef,
        source: int,
        target: int,
        capacity_column: str = "",
    ) -> MinCutResult:
        """Compute s-t minimum cut.
        
        Args:
            graph: Reference to the graph
            source: Source vertex ID
            target: Target vertex ID
            capacity_column: Optional capacity column name
            
        Returns:
            MinCut result with cut value and edges
        """
        algo = gepb.AlgoSpec(
            st_mincut=gepb.STMinCutSpec(
                source_u64=source,
                target_u64=target,
                capacity_column=capacity_column,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_mincut_result(result)

    def min_cut_on_view(
        self,
        view: ViewRef,
        source: int,
        target: int,
        capacity_column: str = "",
    ) -> MinCutResult:
        """Compute s-t minimum cut on a view."""
        algo = gepb.AlgoSpec(
            st_mincut=gepb.STMinCutSpec(
                source_u64=source,
                target_u64=target,
                capacity_column=capacity_column,
            )
        )
        job = self._run(None, view, algo)
        result = self.wait_for_job(job)
        return self._collect_mincut_result(result)

    def _collect_mincut_result(self, result: ResultRef) -> MinCutResult:
        """Collect min-cut result from chunks."""
        mc_result = MinCutResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("st_mincut"):
                mc = chunk.st_mincut
                mc_result.cut_value = mc.cut_value
                mc_result.source_side_vertices = list(mc.source_side_vertices_u64)
                mc_result.cut_edges = list(mc.cut_edges_u64)
        return mc_result

    def corridor(
        self,
        graph: GraphRef,
        source: int,
        target: int,
        method: str = "shortest_path_hull",
        hops: int = 1,
        k: int = 3,
    ) -> CorridorResult:
        """Create a corridor view between source and target.
        
        Args:
            graph: Reference to the graph
            source: Source vertex ID
            target: Target vertex ID
            method: "shortest_path_hull", "ksp_hull", or "community_aware"
            hops: Expansion hops
            k: Number of paths for ksp_hull
            
        Returns:
            Corridor result with view reference
        """
        method_map = {
            "shortest_path_hull": gepb.CorridorSpec.SHORTEST_PATH_HULL,
            "ksp_hull": gepb.CorridorSpec.KSP_HULL,
            "community_aware": gepb.CorridorSpec.COMMUNITY_AWARE,
        }
        algo = gepb.AlgoSpec(
            corridor=gepb.CorridorSpec(
                source_u64=source,
                target_u64=target,
                method=method_map.get(method, gepb.CorridorSpec.SHORTEST_PATH_HULL),
                hops=hops,
                k=k,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_corridor_result(result)

    def _collect_corridor_result(self, result: ResultRef) -> CorridorResult:
        """Collect corridor result from chunks."""
        corr_result = CorridorResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("corridor"):
                corr = chunk.corridor
                corr_result.view_id = corr.view.view_id
                corr_result.meta = dict(corr.meta)
        return corr_result

    def bfs(
        self,
        graph: GraphRef,
        source: int,
        max_depth: int,
    ) -> List[int]:
        """Perform breadth-first search.
        
        Args:
            graph: Reference to the graph
            source: Source vertex ID
            max_depth: Maximum search depth
            
        Returns:
            List of reachable vertex IDs
        """
        algo = gepb.AlgoSpec(
            bfs=gepb.BFSSpec(
                source_u64=source,
                max_depth=max_depth,
                mode=gepb.NeighborhoodSpec.MODE_ALL,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_vertex_list(result)

    def neighborhood(
        self,
        graph: GraphRef,
        seeds: List[int],
        hops: int,
    ) -> List[int]:
        """Find the neighborhood of seed vertices.
        
        Args:
            graph: Reference to the graph
            seeds: Seed vertex IDs
            hops: Number of hops
            
        Returns:
            List of vertex IDs in the neighborhood
        """
        algo = gepb.AlgoSpec(
            neighborhood=gepb.NeighborhoodQuerySpec(
                seeds_u64=seeds,
                hops=hops,
                mode=gepb.NeighborhoodSpec.MODE_ALL,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_vertex_list(result)

    def _collect_vertex_list(self, result: ResultRef) -> List[int]:
        """Collect vertex list from chunks."""
        vertices: List[int] = []
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("u64"):
                vertices.extend(chunk.u64.values)
        return vertices

    def communities(
        self,
        graph: GraphRef,
        method: str = "louvain",
        resolution: float = 1.0,
        steps: int = 0,
        spins: int = 0,
        gamma: float = 0.0,
        trials: int = 0,
    ) -> ComponentsResult:
        """Detect communities.
        
        Args:
            graph: Reference to the graph
            method: Community detection algorithm. Options:
                - "leiden" (recommended): High quality, fast
                - "louvain": Fast modularity optimization
                - "label_propagation": Very fast O(m), non-deterministic
                - "infomap": Information-theoretic method
                - "walktrap": Random walk based O(mn)
                - "fast_greedy": Greedy modularity optimization O(n·log²n)
                - "edge_betweenness": Accurate but slow O(n³)
                - "leading_eigenvector": Newman's spectral method O(n²+m)
                - "spinglass": Statistical physics (connected graphs only)
            resolution: Resolution parameter for Leiden/Louvain (higher = more communities)
            steps: Number of random walk steps for Walktrap (default: 4)
            spins: Number of spins for Spinglass (default: 25)
            gamma: Gamma parameter for Spinglass (default: 1.0)
            trials: Number of trials for Infomap (default: 10)
            
        Returns:
            Components result with community membership
        """
        method_map = {
            "leiden": gepb.CommunitiesSpec.LEIDEN,
            "louvain": gepb.CommunitiesSpec.LOUVAIN,
            "label_propagation": gepb.CommunitiesSpec.LABEL_PROPAGATION,
            "infomap": gepb.CommunitiesSpec.INFOMAP,
            "walktrap": gepb.CommunitiesSpec.WALKTRAP,
            "fast_greedy": gepb.CommunitiesSpec.FAST_GREEDY,
            "edge_betweenness": gepb.CommunitiesSpec.EDGE_BETWEENNESS,
            "leading_eigenvector": gepb.CommunitiesSpec.LEADING_EIGENVECTOR,
            "spinglass": gepb.CommunitiesSpec.SPINGLASS,
        }
        method_enum = method_map.get(method, gepb.CommunitiesSpec.LEIDEN)
        
        algo = gepb.AlgoSpec(
            communities=gepb.CommunitiesSpec(
                method=method_enum,
                resolution=resolution,
                steps=steps,
                spins=spins,
                gamma=gamma,
                trials=trials,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_components_result(result)

    def k_core(
        self,
        graph: GraphRef,
        k: int = 0,
    ) -> KCoreResult:
        """Compute k-core decomposition.
        
        Args:
            graph: Reference to the graph
            k: Specific k value (0 = full decomposition)
            
        Returns:
            KCoreResult with coreness values for all vertices
        """
        algo = gepb.AlgoSpec(
            kcore=gepb.KCoreSpec(k=k)
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_kcore_result(result)

    def _collect_kcore_result(self, result: ResultRef) -> KCoreResult:
        """Collect k-core result from chunks."""
        kcore_result = KCoreResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("kcore"):
                kc = chunk.kcore
                kcore_result.node_ids = list(kc.node_ids_u64)
                kcore_result.coreness = list(kc.coreness)
                kcore_result.max_core = kc.max_core
        return kcore_result

    def betweenness(
        self,
        graph: GraphRef,
        sample_size: int = 0,
        normalized: bool = False,
        weight_column: str = "",
    ) -> BetweennessResult:
        """Compute betweenness centrality.
        
        Args:
            graph: Reference to the graph
            sample_size: Number of source vertices to sample (0 = all)
            normalized: Whether to normalize scores
            weight_column: Optional edge weight column
            
        Returns:
            BetweennessResult with scores for all vertices
        """
        algo = gepb.AlgoSpec(
            betweenness=gepb.BetweennessSpec(
                sample_size=sample_size,
                normalized=normalized,
                weight_column=weight_column,
            )
        )
        job = self._run(graph, None, algo)
        result = self.wait_for_job(job)
        return self._collect_betweenness_result(result)

    def _collect_betweenness_result(self, result: ResultRef) -> BetweennessResult:
        """Collect betweenness result from chunks."""
        betw_result = BetweennessResult()
        for chunk in self._get_result_chunks(result):
            if chunk.HasField("betweenness"):
                b = chunk.betweenness
                betw_result.scores = list(b.scores)
        return betw_result

    # =========================================================================
    # Job Management
    # =========================================================================

    def wait_for_job(
        self,
        job: JobRef,
        poll_interval: float = 0.1,
        timeout: Optional[float] = None,
        callback: Optional[Callable[[str], None]] = None,
    ) -> ResultRef:
        """Wait for a job to complete.
        
        Args:
            job: Job reference
            poll_interval: Polling interval in seconds
            timeout: Optional timeout in seconds
            callback: Optional callback called with state on each poll
            
        Returns:
            Result reference
            
        Raises:
            JobFailedError: If the job fails
            CanceledError: If the job is canceled
            TimeoutError: If timeout is reached
        """
        start_time = time.time()
        while True:
            state, result = self.get_job(job)
            
            if callback:
                callback(state)
            
            if state == "SUCCEEDED":
                if result is None:
                    raise GraphEngineError("Job succeeded but no result returned")
                return result
            elif state == "FAILED":
                raise JobFailedError(f"Job {job.job_id} failed")
            elif state == "CANCELED":
                raise CanceledError(f"Job {job.job_id} was canceled")
            
            if timeout is not None:
                elapsed = time.time() - start_time
                if elapsed >= timeout:
                    raise GETimeoutError(f"Job {job.job_id} timed out after {timeout}s")
            
            time.sleep(poll_interval)

    def cancel_job(self, job: JobRef) -> bool:
        """Cancel a running job.
        
        Args:
            job: Job reference
            
        Returns:
            True if canceled
        """
        try:
            req = gepb.CancelJobRequest(job=gepb.JobRef(job_id=job.job_id))
            resp = self._engine_stub.CancelJob(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return resp.canceled
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    # =========================================================================
    # Ops / Troubleshooting
    # =========================================================================

    def health(self) -> HealthStatus:
        """Check service health.
        
        Returns:
            Health status
        """
        try:
            req = ops_pb.HealthRequest()
            resp = self._ops_stub.Health(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return HealthStatus(
                status=resp.status,
                version=resp.meta.get("version", ""),
                uptime=resp.meta.get("uptime", ""),
                meta=dict(resp.meta),
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def list_graphs(self) -> List[GraphSummary]:
        """List all graphs.
        
        Returns:
            List of graph summaries
        """
        try:
            req = ops_pb.ListGraphsRequest()
            resp = self._ops_stub.ListGraphs(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return [
                GraphSummary(
                    graph_name=g.graph_name,
                    current_version_id=g.current_version_id,
                    vcount=g.vcount,
                    ecount=g.ecount,
                    published_at=g.published_at.ToDatetime() if g.HasField("published_at") else None,
                )
                for g in resp.graphs
            ]
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def describe_graph(self, graph: GraphRef) -> GraphDetails:
        """Get detailed graph information.
        
        Args:
            graph: Graph reference
            
        Returns:
            Graph details
        """
        try:
            req = ops_pb.DescribeGraphRequest(
                graph=gepb.GraphRef(
                    graph_name=graph.graph_name,
                    version_id=graph.version_id,
                )
            )
            resp = self._ops_stub.DescribeGraph(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            summary = None
            if resp.HasField("summary"):
                s = resp.summary
                summary = GraphSummary(
                    graph_name=s.graph_name,
                    current_version_id=s.current_version_id,
                    vcount=s.vcount,
                    ecount=s.ecount,
                    published_at=s.published_at.ToDatetime() if s.HasField("published_at") else None,
                )
            return GraphDetails(
                summary=summary,
                labels=dict(resp.labels),
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def validate_graph(self, graph: GraphRef, deep: bool = False) -> ValidationResult:
        """Validate graph integrity.
        
        Args:
            graph: Graph reference
            deep: Whether to perform deep validation
            
        Returns:
            Validation result
        """
        try:
            req = ops_pb.ValidateGraphRequest(
                graph=gepb.GraphRef(
                    graph_name=graph.graph_name,
                    version_id=graph.version_id,
                ),
                deep=deep,
            )
            resp = self._ops_stub.ValidateGraph(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            valid = resp.status.code == 0 if resp.HasField("status") else True
            message = resp.status.message if resp.HasField("status") else ""
            return ValidationResult(
                valid=valid,
                warnings=list(resp.warnings),
                metrics=dict(resp.metrics),
                message=message,
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def cache_stats(self) -> CacheStats:
        """Get cache statistics.
        
        Returns:
            Cache statistics
        """
        try:
            req = ops_pb.CacheStatsRequest()
            resp = self._ops_stub.CacheStats(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return CacheStats(
                results_items=resp.results_items,
                results_bytes=resp.results_bytes,
                views_items=resp.views_items,
                views_bytes=resp.views_bytes,
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def trace_job(self, job: JobRef) -> List[TraceSpan]:
        """Get trace spans for a job.
        
        Args:
            job: Job reference
            
        Returns:
            List of trace spans
        """
        try:
            req = ops_pb.TraceJobRequest(
                job=gepb.JobRef(job_id=job.job_id)
            )
            resp = self._ops_stub.TraceJob(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            spans = []
            for s in resp.spans:
                duration = None
                if s.HasField("duration"):
                    duration = timedelta(
                        seconds=s.duration.seconds,
                        microseconds=s.duration.nanos // 1000,
                    )
                spans.append(TraceSpan(
                    name=s.name,
                    duration=duration,
                    tags=dict(s.tags),
                ))
            return spans
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def export_graph(
        self,
        graph: GraphRef,
        format: ExportFormat = ExportFormat.EDGE_LIST,
    ) -> Iterator[bytes]:
        """Export graph data.
        
        Args:
            graph: Graph reference
            format: Export format
            
        Yields:
            Chunks of exported data
        """
        try:
            format_map = {
                ExportFormat.EDGE_LIST: ops_pb.ExportSubgraphRequest.EDGE_LIST,
                ExportFormat.CSV: ops_pb.ExportSubgraphRequest.CSV,
            }
            req = ops_pb.ExportSubgraphRequest(
                graph=gepb.GraphRef(
                    graph_name=graph.graph_name,
                    version_id=graph.version_id,
                ),
                format=format_map.get(format, ops_pb.ExportSubgraphRequest.EDGE_LIST),
            )
            for chunk in self._ops_stub.ExportSubgraph(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            ):
                yield chunk.data
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def export_view(
        self,
        view: ViewRef,
        format: ExportFormat = ExportFormat.EDGE_LIST,
    ) -> Iterator[bytes]:
        """Export view data.
        
        Args:
            view: View reference
            format: Export format
            
        Yields:
            Chunks of exported data
        """
        try:
            format_map = {
                ExportFormat.EDGE_LIST: ops_pb.ExportSubgraphRequest.EDGE_LIST,
                ExportFormat.CSV: ops_pb.ExportSubgraphRequest.CSV,
            }
            req = ops_pb.ExportSubgraphRequest(
                view=gepb.ViewRef(view_id=view.view_id),
                format=format_map.get(format, ops_pb.ExportSubgraphRequest.EDGE_LIST),
            )
            for chunk in self._ops_stub.ExportSubgraph(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            ):
                yield chunk.data
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    def export_graph_to_bytes(
        self,
        graph: GraphRef,
        format: ExportFormat = ExportFormat.EDGE_LIST,
    ) -> bytes:
        """Export graph data to bytes.
        
        Args:
            graph: Graph reference
            format: Export format
            
        Returns:
            Exported data as bytes
        """
        chunks = list(self.export_graph(graph, format))
        return b"".join(chunks)

    def export_view_to_bytes(
        self,
        view: ViewRef,
        format: ExportFormat = ExportFormat.EDGE_LIST,
    ) -> bytes:
        """Export view data to bytes.
        
        Args:
            view: View reference
            format: Export format
            
        Returns:
            Exported data as bytes
        """
        chunks = list(self.export_view(view, format))
        return b"".join(chunks)


class AsyncGraphEngineClient:
    """Asynchronous client for the Graph-engine service.
    
    Example:
        >>> async with AsyncGraphEngineClient("localhost:50051") as client:
        ...     graphs = await client.list_graphs()
        ...     for g in graphs:
        ...         print(f"{g.graph_name}: {g.vcount} vertices")
    """

    def __init__(
        self,
        address: str,
        *,
        timeout: float = 30.0,
        secure: bool = False,
        credentials: Optional[grpc.ChannelCredentials] = None,
        metadata: Optional[Dict[str, str]] = None,
        max_retries: int = 3,
        retry_backoff: float = 0.1,
    ):
        """Initialize the async client."""
        self._address = address
        self._timeout = timeout
        self._metadata = metadata or {}
        self._max_retries = max_retries
        self._retry_backoff = retry_backoff
        self._channel: Any = None
        self._engine_stub: Any = None
        self._ops_stub: Any = None

        # Create async channel
        if credentials:
            self._channel = grpc.aio.secure_channel(address, credentials)
        elif secure:
            self._channel = grpc.aio.secure_channel(address, grpc.ssl_channel_credentials())
        else:
            self._channel = grpc.aio.insecure_channel(address)

        # Create stubs
        self._engine_stub = gepb_grpc.GraphEngineStub(self._channel)
        self._ops_stub = ops_grpc.GraphEngineOpsStub(self._channel)

    async def __aenter__(self) -> "AsyncGraphEngineClient":
        return self

    async def __aexit__(self, exc_type: Any, exc_val: Any, exc_tb: Any) -> None:
        await self.close()

    async def close(self) -> None:
        """Close the client connection."""
        if self._channel:
            await self._channel.close()
            self._channel = None

    def _get_metadata(self) -> List[Tuple[str, str]]:
        """Get metadata for RPC calls."""
        return [(k, v) for k, v in self._metadata.items()]

    async def health(self) -> HealthStatus:
        """Check service health (async)."""
        try:
            req = ops_pb.HealthRequest()
            resp = await self._ops_stub.Health(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return HealthStatus(
                status=resp.status,
                version=resp.meta.get("version", ""),
                uptime=resp.meta.get("uptime", ""),
                meta=dict(resp.meta),
            )
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    async def list_graphs(self) -> List[GraphSummary]:
        """List all graphs (async)."""
        try:
            req = ops_pb.ListGraphsRequest()
            resp = await self._ops_stub.ListGraphs(
                req,
                timeout=self._timeout,
                metadata=self._get_metadata(),
            )
            return [
                GraphSummary(
                    graph_name=g.graph_name,
                    current_version_id=g.current_version_id,
                    vcount=g.vcount,
                    ecount=g.ecount,
                    published_at=g.published_at.ToDatetime() if g.HasField("published_at") else None,
                )
                for g in resp.graphs
            ]
        except grpc.RpcError as e:
            raise wrap_grpc_error(e)

    async def shortest_path(
        self,
        graph: GraphRef,
        source: int,
        target: int,
        weight_column: str = "",
    ) -> PathResult:
        """Find shortest path (async)."""
        # For now, delegate to sync implementation via run_in_executor
        # A full async implementation would require async streaming support
        raise NotImplementedError("Full async implementation pending")

    async def wait_for_job(
        self,
        job: JobRef,
        poll_interval: float = 0.1,
        timeout: Optional[float] = None,
    ) -> ResultRef:
        """Wait for job completion (async)."""
        import asyncio
        start_time = time.time()
        while True:
            try:
                req = gepb.GetJobRequest(job=gepb.JobRef(job_id=job.job_id))
                resp = await self._engine_stub.GetJob(
                    req,
                    timeout=self._timeout,
                    metadata=self._get_metadata(),
                )
                state = resp.state
                
                if state == gepb.GetJobResponse.SUCCEEDED:
                    if resp.HasField("result") and resp.result.result_id:
                        return ResultRef(result_id=resp.result.result_id)
                    raise GraphEngineError("Job succeeded but no result returned")
                elif state == gepb.GetJobResponse.FAILED:
                    raise JobFailedError(f"Job {job.job_id} failed")
                elif state == gepb.GetJobResponse.CANCELED:
                    raise CanceledError(f"Job {job.job_id} was canceled")
                
                if timeout is not None:
                    elapsed = time.time() - start_time
                    if elapsed >= timeout:
                        raise GETimeoutError(f"Job {job.job_id} timed out after {timeout}s")
                
                await asyncio.sleep(poll_interval)
            except grpc.RpcError as e:
                raise wrap_grpc_error(e)
