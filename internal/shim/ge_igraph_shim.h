// ge_igraph_shim.h - C API header for igraph integration
// This shim provides a minimal, stable C API that wraps igraph,
// isolating GPL-licensed code from the Go service layer.
//
// Design: Section 5.2-5.3 of graph-engine-design.md

#pragma once

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

// -----------------------------------------------------------------------------
// Opaque types
// -----------------------------------------------------------------------------

typedef struct ge_graph ge_graph_t;
typedef struct ge_view ge_view_t;
typedef struct ge_result ge_result_t;

// -----------------------------------------------------------------------------
// Status codes
// -----------------------------------------------------------------------------

typedef enum {
    GE_OK = 0,
    GE_ERR_GENERAL = 1,
    GE_ERR_INVALID_ARG = 2,
    GE_ERR_OUT_OF_MEMORY = 3,
    GE_ERR_NOT_FOUND = 4,
    GE_ERR_TIMEOUT = 5,
    GE_ERR_IGRAPH = 6,  // igraph library error
} ge_status_t;

// -----------------------------------------------------------------------------
// Algorithm kinds
// -----------------------------------------------------------------------------

typedef enum {
    GE_ALGO_COMPONENTS_WEAK = 1,
    GE_ALGO_COMPONENTS_STRONG = 2,
    GE_ALGO_SHORTEST_PATH = 3,
    GE_ALGO_ST_MINCUT = 4,
    GE_ALGO_BFS = 5,
    GE_ALGO_KSP = 6,
    GE_ALGO_COMMUNITIES_LEIDEN = 7,
    GE_ALGO_COMMUNITIES_LOUVAIN = 8,
    GE_ALGO_NEIGHBORHOOD = 9,
} ge_algo_kind_t;

// -----------------------------------------------------------------------------
// Direction mode (for BFS, neighborhood)
// -----------------------------------------------------------------------------

typedef enum {
    GE_MODE_ALL = 0,  // Both directions (undirected traversal)
    GE_MODE_OUT = 1,  // Outgoing edges only
    GE_MODE_IN = 2,   // Incoming edges only
} ge_mode_t;

// -----------------------------------------------------------------------------
// Graph construction input
// -----------------------------------------------------------------------------

typedef struct {
    uint32_t n_vertices;      // Number of vertices (compact indices 0..n-1)
    const uint32_t* src;      // Source vertices (length m_edges)
    const uint32_t* dst;      // Destination vertices (length m_edges)
    size_t m_edges;           // Number of edges
    int directed;             // 1 = directed, 0 = undirected
} ge_edge_list_t;

// -----------------------------------------------------------------------------
// Graph lifecycle
// -----------------------------------------------------------------------------

// Create a graph from an edge list.
// On success, *out points to a newly allocated graph.
// Caller must call ge_graph_destroy() to free.
ge_status_t ge_graph_create(const ge_edge_list_t* edges, ge_graph_t** out);

// Destroy a graph and free all associated memory.
void ge_graph_destroy(ge_graph_t* g);

// Get the memory footprint of the graph in bytes (for Go-side tracking).
size_t ge_graph_memory_bytes(const ge_graph_t* g);

// Get vertex count.
uint32_t ge_graph_vcount(const ge_graph_t* g);

// Get edge count.
size_t ge_graph_ecount(const ge_graph_t* g);

// -----------------------------------------------------------------------------
// View lifecycle (optional - views can also be managed in Go)
// -----------------------------------------------------------------------------

// Create a view by specifying which edges to include.
// edge_mask_bits is a bitset where bit i indicates edge i is included.
// edge_mask_nbytes should be (m_edges + 7) / 8.
ge_status_t ge_view_create_by_edge_mask(
    const ge_graph_t* g,
    const uint8_t* edge_mask_bits,
    size_t edge_mask_nbytes,
    ge_view_t** out
);

// Destroy a view.
void ge_view_destroy(ge_view_t* v);

// Get the memory footprint of a view in bytes.
size_t ge_view_memory_bytes(const ge_view_t* v);

// -----------------------------------------------------------------------------
// Algorithm: Connected Components
// -----------------------------------------------------------------------------

// Compute connected components (weak or strong).
// weak=1: weakly connected components (ignores edge direction)
// weak=0: strongly connected components (respects edge direction)
// Result contains "membership" as uint32 array (length = vcount).
ge_status_t ge_run_components(
    const ge_graph_t* g,
    int weak,
    ge_result_t** out
);

// Run components on a view.
ge_status_t ge_run_components_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    int weak,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: Shortest Path
// -----------------------------------------------------------------------------

// Compute shortest path between src and dst.
// weights_or_null: if NULL, unweighted (hop count). Otherwise, edge weights.
// return_vertices: if 1, include path vertices in result ("path_vertices").
// return_edges: if 1, include path edge indices in result ("path_edges").
// Result contains:
//   - "path_vertices": uint32[] (if requested)
//   - "path_edges": uint32[] (if requested)
//   - "total_cost": f64[1]
//   - "found": uint32[1] (1 if path exists, 0 otherwise)
ge_status_t ge_run_shortest_path(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    int return_vertices,
    int return_edges,
    ge_result_t** out
);

// Shortest path on a view.
ge_status_t ge_run_shortest_path_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    int return_vertices,
    int return_edges,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: K-Shortest Paths (Yen's algorithm)
// -----------------------------------------------------------------------------

// Compute k shortest paths between src and dst.
// weights_or_null: if NULL, unweighted. Otherwise, edge weights.
// k: number of paths to find
// max_candidates: maximum candidates to consider (0 = no limit)
// Result contains:
//   - "path_offsets": uint32[] (length k+1, ragged array encoding)
//   - "path_vertices": uint32[] (concatenated path vertices)
//   - "path_costs": f64[] (length k, cost of each path)
//   - "num_paths": uint32[1] (actual number of paths found, may be < k)
ge_status_t ge_run_ksp(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    uint32_t k,
    uint32_t max_candidates,
    ge_result_t** out
);

// K-shortest paths on a view.
ge_status_t ge_run_ksp_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    uint32_t k,
    uint32_t max_candidates,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: s-t Minimum Cut
// -----------------------------------------------------------------------------

// Compute minimum s-t cut.
// capacity_or_null: if NULL, all edges have capacity 1. Otherwise, edge capacities.
// Result contains:
//   - "cut_value": f64[1]
//   - "source_side": uint32[] (vertices on source side of cut)
//   - "cut_edges": uint32[] (edge indices in the cut)
ge_status_t ge_run_st_mincut(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* capacity_or_null,
    ge_result_t** out
);

// Mincut on a view.
ge_status_t ge_run_st_mincut_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* capacity_or_null,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: BFS
// -----------------------------------------------------------------------------

// Perform BFS from a source vertex.
// max_depth: maximum depth to traverse (0 = unlimited)
// mode: GE_MODE_ALL, GE_MODE_OUT, or GE_MODE_IN
// Result contains:
//   - "visited": uint32[] (vertices visited in BFS order)
//   - "depths": uint32[] (depth of each visited vertex)
//   - "parents": uint32[] (parent of each visited vertex, UINT32_MAX for source)
ge_status_t ge_run_bfs(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t max_depth,
    ge_mode_t mode,
    ge_result_t** out
);

// BFS on a view.
ge_status_t ge_run_bfs_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t max_depth,
    ge_mode_t mode,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: Neighborhood Query
// -----------------------------------------------------------------------------

// Find vertices within hops of seed vertices.
// seeds: array of seed vertex indices
// n_seeds: number of seeds
// hops: maximum hop distance
// mode: GE_MODE_ALL, GE_MODE_OUT, or GE_MODE_IN
// Result contains:
//   - "vertices": uint32[] (vertices in neighborhood)
//   - "distances": uint32[] (distance from nearest seed)
ge_status_t ge_run_neighborhood(
    const ge_graph_t* g,
    const uint32_t* seeds,
    size_t n_seeds,
    uint32_t hops,
    ge_mode_t mode,
    ge_result_t** out
);

// Neighborhood on a view.
ge_status_t ge_run_neighborhood_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    const uint32_t* seeds,
    size_t n_seeds,
    uint32_t hops,
    ge_mode_t mode,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: Community Detection
// -----------------------------------------------------------------------------

// Detect communities using Leiden algorithm.
// resolution: resolution parameter (higher = more communities)
// Result contains:
//   - "membership": uint32[] (community ID for each vertex)
//   - "modularity": f64[1] (modularity score)
//   - "num_communities": uint32[1]
ge_status_t ge_run_communities_leiden(
    const ge_graph_t* g,
    double resolution,
    ge_result_t** out
);

// Leiden on a view.
ge_status_t ge_run_communities_leiden_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    double resolution,
    ge_result_t** out
);

// Detect communities using Louvain algorithm.
ge_status_t ge_run_communities_louvain(
    const ge_graph_t* g,
    double resolution,
    ge_result_t** out
);

// Louvain on a view.
ge_status_t ge_run_communities_louvain_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    double resolution,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: K-Core Decomposition
// -----------------------------------------------------------------------------

// Compute k-core decomposition.
// Returns coreness value for each vertex (the largest k for which
// the vertex belongs to a k-core).
// Result contains:
//   - "coreness": uint32[] (coreness value per vertex)
//   - "max_core": uint32[1] (maximum k found)
ge_status_t ge_run_kcore(
    const ge_graph_t* g,
    ge_result_t** out
);

// K-core on a view.
ge_status_t ge_run_kcore_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Algorithm: Betweenness Centrality
// -----------------------------------------------------------------------------

// Compute betweenness centrality for all vertices.
// sample_size: number of source vertices to sample (0 = all)
// normalized: whether to normalize scores
// weights_or_null: edge weights (NULL for unweighted)
// Result contains:
//   - "scores": f64[] (betweenness score per vertex)
ge_status_t ge_run_betweenness(
    const ge_graph_t* g,
    uint32_t sample_size,
    int normalized,
    const double* weights_or_null,
    ge_result_t** out
);

// Betweenness on a view.
ge_status_t ge_run_betweenness_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t sample_size,
    int normalized,
    const double* weights_or_null,
    ge_result_t** out
);

// -----------------------------------------------------------------------------
// Result extraction
// -----------------------------------------------------------------------------

// Destroy a result and free all associated memory.
void ge_result_destroy(ge_result_t* r);

// Get the memory footprint of a result in bytes.
size_t ge_result_memory_bytes(const ge_result_t* r);

// Get a uint32 buffer from the result by name.
// On success, *data points to the buffer (owned by result), *len is the length.
// Returns GE_ERR_NOT_FOUND if the named buffer doesn't exist.
ge_status_t ge_result_get_u32(
    const ge_result_t* r,
    const char* name,
    const uint32_t** data,
    size_t* len
);

// Get a float64 buffer from the result by name.
ge_status_t ge_result_get_f64(
    const ge_result_t* r,
    const char* name,
    const double** data,
    size_t* len
);

// Get a bytes buffer from the result by name.
ge_status_t ge_result_get_bytes(
    const ge_result_t* r,
    const char* name,
    const uint8_t** data,
    size_t* len
);

// Get the last error message (thread-local).
// Returns NULL if no error occurred.
const char* ge_last_error(void);

// -----------------------------------------------------------------------------
// Version info
// -----------------------------------------------------------------------------

// Get the shim library version string.
const char* ge_shim_version(void);

// Get the igraph library version string.
const char* ge_igraph_version(void);

// -----------------------------------------------------------------------------
// Thread safety
// -----------------------------------------------------------------------------

// Check if igraph was built with thread-safety (TLS) enabled.
// Returns 1 if thread-safe, 0 otherwise.
int ge_is_thread_safe(void);

#ifdef __cplusplus
}
#endif
