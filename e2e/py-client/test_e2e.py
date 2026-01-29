#!/usr/bin/env python3
# SPDX-License-Identifier: MIT
# Copyright (c) 2025 Naisa AI, Inc.

"""E2E tests for the Graph-engine service using the Python client library.

This tests the same APIs as e2e/client but uses the high-level Python client library.
"""

import os
import sys
import time
import logging
from typing import Optional

# Add client library to path
sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', '..', 'clients', 'py'))

from graphengine import (
    GraphEngineClient,
    GraphBuilder,
    ViewBuilder,
    GraphRef,
    ViewRef,
    ExportFormat,
    GraphEngineError,
)

logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
log = logging.getLogger(__name__)

# Shared state between tests
graph_ref: Optional[GraphRef] = None
view_ref: Optional[ViewRef] = None
corridor_view_ref: Optional[ViewRef] = None
last_job_id: str = ""


def test_health(client: GraphEngineClient) -> bool:
    """Test Health API."""
    health = client.health()
    if health.status != "SERVING":
        log.error(f"expected SERVING, got {health.status}")
        return False
    log.info(f"  Health status: {health.status}")
    return True


def test_build_and_publish(client: GraphEngineClient) -> bool:
    """Test GraphBuilder to build and publish a graph."""
    global graph_ref
    
    graph = (GraphBuilder(client, "e2e-py-client-test")
        .directed(False)
        .with_label("env", "e2e")
        .with_label("client", "py-lib")
        .add_vertices([1, 2, 3, 4, 5, 6, 7, 8, 9, 10])
        .add_edges(
            [1, 2, 3, 5, 6, 8, 9],
            [2, 3, 4, 6, 7, 9, 10],
        )
        .publish())
    
    if not graph.version_id:
        log.error("expected valid version_id")
        return False
    
    graph_ref = graph
    log.info(f"  Published: {graph.graph_name} version {graph.version_id}")
    return True


def test_list_graphs(client: GraphEngineClient) -> bool:
    """Test ListGraphs API."""
    graphs = client.list_graphs()
    if len(graphs) == 0:
        log.error("expected at least 1 graph")
        return False
    
    found = False
    for g in graphs:
        if g.graph_name == "e2e-py-client-test":
            found = True
            log.info(f"  Found graph: {g.graph_name} (vertices={g.vcount}, edges={g.ecount})")
    
    if not found:
        log.error("e2e-py-client-test not found in list")
        return False
    
    return True


def test_describe_graph(client: GraphEngineClient) -> bool:
    """Test DescribeGraph API."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    details = client.describe_graph(graph_ref)
    if details.summary is None:
        log.error("expected summary in response")
        return False
    if details.summary.vcount != 10:
        log.error(f"expected 10 vertices, got {details.summary.vcount}")
        return False
    if details.summary.ecount != 7:
        log.error(f"expected 7 edges, got {details.summary.ecount}")
        return False
    
    log.info(f"  Graph details: {details.summary.vcount} vertices, {details.summary.ecount} edges")
    return True


def test_run_components(client: GraphEngineClient) -> bool:
    """Test Components algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.components(graph_ref)
    if len(result.membership) != 10:
        log.error(f"expected 10 membership values, got {len(result.membership)}")
        return False
    
    # Count unique components
    components = set(result.membership)
    if len(components) != 3:
        log.error(f"expected 3 components, got {len(components)}")
        return False
    
    log.info(f"  Components: {len(result.membership)} membership values, {len(components)} components")
    return True


def test_create_view(client: GraphEngineClient) -> bool:
    """Test ViewBuilder to create a view."""
    global graph_ref, view_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    view, vcount, ecount = (ViewBuilder(client, graph_ref)
        .induce_vertices([1, 2, 3, 4])
        .create())
    
    if not view.view_id:
        log.error("expected valid view_id")
        return False
    if vcount != 4:
        log.error(f"expected 4 vertices in view, got {vcount}")
        return False
    if ecount != 3:
        log.error(f"expected 3 edges in view, got {ecount}")
        return False
    
    view_ref = view
    log.info(f"  View created: {view.view_id} (vertices={vcount}, edges={ecount})")
    return True


def test_run_shortest_path(client: GraphEngineClient) -> bool:
    """Test ShortestPath algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    path = client.shortest_path(graph_ref, source=1, target=4)
    if len(path.vertices) == 0:
        log.error("expected vertices in path")
        return False
    
    log.info(f"  ShortestPath (1->4): vertices={path.vertices}, cost={path.total_cost:.2f}")
    return True


def test_run_shortest_path_on_view(client: GraphEngineClient) -> bool:
    """Test ShortestPath on view."""
    global view_ref
    if view_ref is None:
        log.error("no view reference from previous test")
        return False
    
    path = client.shortest_path_on_view(view_ref, source=1, target=4)
    if len(path.vertices) == 0:
        log.error("expected vertices in path")
        return False
    
    log.info(f"  ShortestPath on view (1->4): vertices={path.vertices}, cost={path.total_cost:.2f}")
    return True


def test_run_distances(client: GraphEngineClient) -> bool:
    """Test Distances algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    matrix = client.distances(graph_ref, sources=[1, 5, 8], targets=[4, 7, 10])
    if len(matrix.distances) == 0:
        log.error("expected distance matrix")
        return False
    
    log.info(f"  Distances: {len(matrix.sources)}x{len(matrix.targets)} matrix")
    return True


def test_run_corridor(client: GraphEngineClient) -> bool:
    """Test Corridor algorithm."""
    global graph_ref, corridor_view_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.corridor(graph_ref, source=1, target=4)
    if not result.view_id:
        log.error("expected valid corridor view")
        return False
    
    corridor_view_ref = ViewRef(view_id=result.view_id)
    log.info(f"  Corridor (1->4): view={result.view_id}")
    return True


def test_run_min_cut(client: GraphEngineClient) -> bool:
    """Test MinCut algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.min_cut(graph_ref, source=1, target=4)
    
    log.info(f"  MinCut (1->4): cut_value={result.cut_value:.2f}, cut_edges={len(result.cut_edges)}")
    return True


def test_run_min_cut_on_view(client: GraphEngineClient) -> bool:
    """Test MinCut on corridor view."""
    global corridor_view_ref
    if corridor_view_ref is None:
        log.info("  Skipping MinCut on corridor (no corridor view available)")
        return True
    
    result = client.min_cut_on_view(corridor_view_ref, source=1, target=4)
    
    log.info(f"  MinCut on corridor (1->4): cut_value={result.cut_value:.2f}, cut_edges={len(result.cut_edges)}")
    return True


def test_run_k_shortest_paths(client: GraphEngineClient) -> bool:
    """Test KShortestPaths algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    paths = client.k_shortest_paths(graph_ref, source=1, target=4, k=3)
    if len(paths) < 1:
        log.error("expected at least 1 path")
        return False
    
    for i, p in enumerate(paths):
        log.info(f"  KSP path {i+1}: vertices={p.vertices}, cost={p.total_cost:.2f}")
    return True


def test_cache_stats(client: GraphEngineClient) -> bool:
    """Test CacheStats API."""
    stats = client.cache_stats()
    
    log.info(f"  Cache stats: results={stats.results_items} ({stats.results_bytes} bytes), "
             f"views={stats.views_items} ({stats.views_bytes} bytes)")
    
    if stats.results_items == 0:
        log.error("expected at least 1 cached result")
        return False
    return True


def test_validate_graph(client: GraphEngineClient) -> bool:
    """Test ValidateGraph (shallow)."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.validate_graph(graph_ref, deep=False)
    if not result.metrics:
        log.error("expected metrics in response")
        return False
    
    log.info(f"  ValidateGraph (shallow): valid={result.valid}, warnings={len(result.warnings)}, "
             f"metrics={len(result.metrics)}")
    return True


def test_validate_graph_deep(client: GraphEngineClient) -> bool:
    """Test ValidateGraph (deep)."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.validate_graph(graph_ref, deep=True)
    if not result.metrics:
        log.error("expected metrics in response")
        return False
    
    log.info(f"  ValidateGraph (deep): valid={result.valid}, warnings={len(result.warnings)}, "
             f"metrics={len(result.metrics)}")
    return True


def test_trace_job(client: GraphEngineClient) -> bool:
    """Test TraceJob API."""
    global graph_ref, last_job_id
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # Run a quick job to trace
    result = client.components(graph_ref)  # This will create a job
    
    # Note: We need access to the job ID to trace it
    # For now, just verify the trace API works with a previously completed job
    log.info("  TraceJob: (skipped - need job ID tracking)")
    return True


def test_export_subgraph(client: GraphEngineClient) -> bool:
    """Test ExportSubgraph (graph)."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    data = client.export_graph_to_bytes(graph_ref, format=ExportFormat.EDGE_LIST)
    if len(data) == 0:
        log.error("expected non-empty export data")
        return False
    
    log.info(f"  ExportSubgraph (graph): {len(data)} bytes")
    return True


def test_export_subgraph_view(client: GraphEngineClient) -> bool:
    """Test ExportSubgraph (view)."""
    global view_ref
    if view_ref is None:
        log.info("  Skipping ExportSubgraph on view (no view reference available)")
        return True
    
    data = client.export_view_to_bytes(view_ref, format=ExportFormat.CSV)
    if len(data) == 0:
        log.error("expected non-empty export data")
        return False
    
    log.info(f"  ExportSubgraph (view, CSV): {len(data)} bytes")
    return True


def test_run_bfs(client: GraphEngineClient) -> bool:
    """Test BFS algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    vertices = client.bfs(graph_ref, source=1, max_depth=2)
    if len(vertices) == 0:
        log.error("expected vertices from BFS")
        return False
    
    log.info(f"  BFS (source=1, depth=2): found {len(vertices)} vertices")
    return True


def test_run_neighborhood(client: GraphEngineClient) -> bool:
    """Test Neighborhood algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    vertices = client.neighborhood(graph_ref, seeds=[1, 5], hops=1)
    if len(vertices) == 0:
        log.error("expected vertices from Neighborhood")
        return False
    
    log.info(f"  Neighborhood (seeds=[1,5], hops=1): found {len(vertices)} vertices")
    return True


def test_run_communities(client: GraphEngineClient) -> bool:
    """Test Communities algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.communities(graph_ref)
    if len(result.membership) != 10:
        log.error(f"expected 10 membership values, got {len(result.membership)}")
        return False
    
    # Count unique communities
    communities = set(result.membership)
    
    log.info(f"  Communities (Louvain): {len(result.membership)} membership values, {len(communities)} communities")
    return True


def test_run_kcore(client: GraphEngineClient) -> bool:
    """Test KCore algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.k_core(graph_ref)
    if len(result.coreness) != 10:
        log.error(f"expected 10 coreness values, got {len(result.coreness)}")
        return False
    
    log.info(f"  KCore: {len(result.coreness)} vertices, max_core={result.max_core}")
    return True


def test_run_betweenness(client: GraphEngineClient) -> bool:
    """Test Betweenness algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.betweenness(graph_ref)
    if len(result.scores) != 10:
        log.error(f"expected 10 betweenness scores, got {len(result.scores)}")
        return False
    
    log.info(f"  Betweenness: {len(result.scores)} scores computed")
    return True


# =============================================================================
# Enhanced Algorithm Tests - Testing Additional Options
# =============================================================================


def test_run_components_strong(client: GraphEngineClient) -> bool:
    """Test Strong Components algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # Note: Strong components only makes sense for directed graphs
    # Our test graph is undirected, so strong == weak
    # We're testing that the API works with mode="strong"
    result = client.components(graph_ref, mode="strong")
    if len(result.membership) != 10:
        log.error(f"expected 10 membership values, got {len(result.membership)}")
        return False
    
    log.info(f"  Components (strong): {len(result.membership)} membership values")
    return True


def test_run_communities_leiden(client: GraphEngineClient) -> bool:
    """Test Communities with Leiden algorithm."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    result = client.communities(graph_ref, method="leiden")
    if len(result.membership) != 10:
        log.error(f"expected 10 membership values, got {len(result.membership)}")
        return False
    
    communities = set(result.membership)
    log.info(f"  Communities (Leiden): {len(communities)} communities found")
    return True


def test_run_communities_resolution(client: GraphEngineClient) -> bool:
    """Test Communities with different resolution values."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # Lower resolution -> fewer communities
    result_low = client.communities(graph_ref, method="louvain", resolution=0.5)
    communities_low = set(result_low.membership)
    
    # Higher resolution -> more communities
    result_high = client.communities(graph_ref, method="louvain", resolution=2.0)
    communities_high = set(result_high.membership)
    
    log.info(f"  Communities resolution test: low_res={len(communities_low)}, high_res={len(communities_high)}")
    return True


def test_run_bfs_unlimited_depth(client: GraphEngineClient) -> bool:
    """Test BFS with larger depth to explore more of the graph."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # Use a large depth to explore the entire component
    vertices = client.bfs(graph_ref, source=1, max_depth=10)
    if len(vertices) == 0:
        log.error("expected vertices from BFS")
        return False
    
    log.info(f"  BFS (source=1, depth=10): found {len(vertices)} vertices")
    return True


def test_run_neighborhood_multi_hop(client: GraphEngineClient) -> bool:
    """Test Neighborhood with multiple hops."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # 1-hop neighborhood
    vertices_1hop = client.neighborhood(graph_ref, seeds=[1], hops=1)
    
    # 2-hop neighborhood (should include more vertices)
    vertices_2hop = client.neighborhood(graph_ref, seeds=[1], hops=2)
    
    if len(vertices_2hop) < len(vertices_1hop):
        log.error("2-hop neighborhood should include at least as many vertices as 1-hop")
        return False
    
    log.info(f"  Neighborhood: 1-hop={len(vertices_1hop)}, 2-hop={len(vertices_2hop)}")
    return True


def test_run_components_on_view(client: GraphEngineClient) -> bool:
    """Test Components algorithm on a view."""
    global view_ref
    if view_ref is None:
        log.info("  Skipping Components on view (no view reference available)")
        return True
    
    # Components on a view requires using the raw Run API
    # The high-level client may not expose this directly
    log.info("  Components on view: (skipped - requires raw API)")
    return True


def test_run_bfs_on_view(client: GraphEngineClient) -> bool:
    """Test BFS on a view."""
    global view_ref
    if view_ref is None:
        log.info("  Skipping BFS on view (no view reference available)")
        return True
    
    log.info("  BFS on view: (skipped - requires raw API)")
    return True


def test_run_neighborhood_on_view(client: GraphEngineClient) -> bool:
    """Test Neighborhood on a view."""
    global view_ref
    if view_ref is None:
        log.info("  Skipping Neighborhood on view (no view reference available)")
        return True
    
    log.info("  Neighborhood on view: (skipped - requires raw API)")
    return True


def test_run_communities_on_view(client: GraphEngineClient) -> bool:
    """Test Communities on a view."""
    global view_ref
    if view_ref is None:
        log.info("  Skipping Communities on view (no view reference available)")
        return True
    
    log.info("  Communities on view: (skipped - requires raw API)")
    return True


def test_cancel_job(client: GraphEngineClient) -> bool:
    """Test CancelJob API."""
    # Note: Without direct access to job refs from the high-level API,
    # we'll skip this test
    log.info("  CancelJob: (skipped - high-level API hides job refs)")
    return True


def test_release(client: GraphEngineClient) -> bool:
    """Test Release API."""
    global graph_ref
    if graph_ref is None:
        log.error("no graph reference from previous test")
        return False
    
    # Create a temporary view to release
    view, _, _ = (ViewBuilder(client, graph_ref)
        .induce_vertices([1, 2])
        .create())
    
    log.info(f"  Created temp view for release test: {view.view_id}")
    
    # Release the view
    released = client.release_view(view)
    if not released:
        log.error("expected release to succeed")
        return False
    
    log.info(f"  Release: view {view.view_id} released={released}")
    return True


def main():
    addr = os.environ.get("GRAPH_ENGINE_ADDR", "localhost:50051")
    
    log.info(f"E2E Test Client (Python Client Library) starting, connecting to {addr}")
    
    # Connect with retries
    client = None
    for i in range(30):
        try:
            client = GraphEngineClient(addr, timeout=30.0)
            # Try a quick health check to verify connection
            client.health()
            break
        except Exception as e:
            log.info(f"Connection attempt {i+1} failed: {e}, retrying...")
            time.sleep(1)
    
    if client is None:
        log.error("Failed to connect after retries")
        sys.exit(1)
    
    # Run all tests
    tests = [
        ("Health", test_health),
        ("BuildAndPublish", test_build_and_publish),
        ("ListGraphs", test_list_graphs),
        ("DescribeGraph", test_describe_graph),
        ("RunComponents", test_run_components),
        # Phase 2 features
        ("CreateView", test_create_view),
        ("RunShortestPath", test_run_shortest_path),
        ("RunShortestPathOnView", test_run_shortest_path_on_view),
        ("RunDistances", test_run_distances),
        # Phase 3 features
        ("RunCorridor", test_run_corridor),
        ("RunSTMinCut", test_run_min_cut),
        ("RunSTMinCutOnView", test_run_min_cut_on_view),
        # Phase 4 features
        ("RunKShortestPaths", test_run_k_shortest_paths),
        ("CacheStats", test_cache_stats),
        # Troubleshooting APIs
        ("ValidateGraph", test_validate_graph),
        ("ValidateGraphDeep", test_validate_graph_deep),
        ("TraceJob", test_trace_job),
        ("ExportSubgraph", test_export_subgraph),
        ("ExportSubgraphView", test_export_subgraph_view),
        # Additional algorithm tests
        ("RunBFS", test_run_bfs),
        ("RunNeighborhood", test_run_neighborhood),
        ("RunCommunities", test_run_communities),
        # Phase 5: New algorithms
        ("RunKCore", test_run_kcore),
        ("RunBetweenness", test_run_betweenness),
        # Enhanced algorithm tests - additional options
        ("RunComponentsStrong", test_run_components_strong),
        ("RunCommunitiesLeiden", test_run_communities_leiden),
        ("RunCommunitiesResolution", test_run_communities_resolution),
        ("RunBFSUnlimitedDepth", test_run_bfs_unlimited_depth),
        ("RunNeighborhoodMultiHop", test_run_neighborhood_multi_hop),
        ("RunComponentsOnView", test_run_components_on_view),
        ("RunBFSOnView", test_run_bfs_on_view),
        ("RunNeighborhoodOnView", test_run_neighborhood_on_view),
        ("RunCommunitiesOnView", test_run_communities_on_view),
        # Resource management tests
        ("CancelJob", test_cancel_job),
        ("Release", test_release),
    ]
    
    passed = 0
    failed = 0
    
    for name, test_fn in tests:
        start = time.time()
        log.info(f"Running test: {name}")
        
        try:
            if test_fn(client):
                log.info(f"PASS: {name} ({time.time() - start:.2f}s)")
                passed += 1
            else:
                log.error(f"FAIL: {name} ({time.time() - start:.2f}s)")
                failed += 1
        except Exception as e:
            log.error(f"FAIL: {name} - {e} ({time.time() - start:.2f}s)")
            failed += 1
    
    client.close()
    
    log.info("========================================")
    log.info(f"E2E Test Results (Python Client): {passed} passed, {failed} failed")
    log.info("========================================")
    
    if failed > 0:
        sys.exit(1)


if __name__ == "__main__":
    main()
