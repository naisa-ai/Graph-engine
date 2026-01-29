// ge_igraph_shim.c - C implementation wrapping igraph library
// This shim provides a minimal, stable C API that wraps igraph,
// isolating GPL-licensed code from the Go service layer.
//
// Design: Section 5.3 of graph-engine-design.md

#include "ge_igraph_shim.h"
#include <igraph/igraph.h>
#include <stdlib.h>
#include <string.h>
#include <stdio.h>
#include <pthread.h>
#include <math.h>
#include <time.h>

// -----------------------------------------------------------------------------
// Version info
// -----------------------------------------------------------------------------

#define GE_SHIM_VERSION "1.0.0"

// -----------------------------------------------------------------------------
// Thread-local error message
// -----------------------------------------------------------------------------

static __thread char ge_error_buf[512] = {0};

static void set_error(const char* fmt, ...) {
    va_list args;
    va_start(args, fmt);
    vsnprintf(ge_error_buf, sizeof(ge_error_buf), fmt, args);
    va_end(args);
}

const char* ge_last_error(void) {
    if (ge_error_buf[0] == '\0') {
        return NULL;
    }
    return ge_error_buf;
}

// -----------------------------------------------------------------------------
// Thread-local random number generator
// -----------------------------------------------------------------------------
// The default igraph RNG is not thread-safe. Each thread must have its own
// RNG instance to avoid data races during concurrent graph operations.

static __thread igraph_rng_t* ge_thread_rng = NULL;
static __thread int ge_rng_initialized = 0;

// Ensure the current thread has its own RNG initialized.
// This function is idempotent and safe to call multiple times.
static void ge_ensure_thread_rng(void) {
    if (ge_rng_initialized) {
        return;
    }
    
    ge_thread_rng = (igraph_rng_t*)malloc(sizeof(igraph_rng_t));
    if (ge_thread_rng == NULL) {
        // Fall back to default RNG if allocation fails
        return;
    }
    
    // Initialize with Mersenne Twister algorithm
    if (igraph_rng_init(ge_thread_rng, &igraph_rngtype_mt19937) != IGRAPH_SUCCESS) {
        free(ge_thread_rng);
        ge_thread_rng = NULL;
        return;
    }
    
    // Seed with a combination of thread ID and time for uniqueness
    unsigned long seed = (unsigned long)pthread_self() ^ (unsigned long)time(NULL);
    igraph_rng_seed(ge_thread_rng, seed);
    
    // Set this thread's RNG as the default for igraph operations
    igraph_rng_set_default(ge_thread_rng);
    
    ge_rng_initialized = 1;
}

// Check if igraph was built with thread-safety (TLS) enabled
int ge_is_thread_safe(void) {
#ifdef IGRAPH_THREAD_SAFE
    return IGRAPH_THREAD_SAFE;
#else
    return 0;
#endif
}

// -----------------------------------------------------------------------------
// Internal structures
// -----------------------------------------------------------------------------

// Named buffer for storing result data
typedef struct ge_buffer {
    char name[64];
    void* data;
    size_t len;
    size_t elem_size;  // 4 for uint32, 8 for double, 1 for bytes
    struct ge_buffer* next;
} ge_buffer_t;

// Graph structure wrapping igraph
struct ge_graph {
    igraph_t g;
    uint32_t n_vertices;
    size_t m_edges;
    int directed;
    size_t memory_estimate;
};

// View structure (subgraph defined by edge mask)
struct ge_view {
    igraph_t g;           // Induced subgraph
    uint32_t* vertex_map; // Maps view vertex index -> original vertex index
    uint32_t* edge_map;   // Maps view edge index -> original edge index
    uint32_t n_vertices;
    size_t m_edges;
    size_t memory_estimate;
};

// Result structure with named buffers
struct ge_result {
    ge_buffer_t* buffers;
    size_t memory_estimate;
};

// -----------------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------------

static ge_buffer_t* find_buffer(const ge_result_t* r, const char* name) {
    ge_buffer_t* buf = r->buffers;
    while (buf != NULL) {
        if (strcmp(buf->name, name) == 0) {
            return buf;
        }
        buf = buf->next;
    }
    return NULL;
}

static ge_status_t add_buffer_u32(ge_result_t* r, const char* name, const uint32_t* data, size_t len) {
    ge_buffer_t* buf = (ge_buffer_t*)calloc(1, sizeof(ge_buffer_t));
    if (!buf) {
        set_error("out of memory allocating buffer");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    strncpy(buf->name, name, sizeof(buf->name) - 1);
    buf->elem_size = sizeof(uint32_t);
    buf->len = len;
    
    if (len > 0) {
        buf->data = malloc(len * sizeof(uint32_t));
        if (!buf->data) {
            free(buf);
            set_error("out of memory allocating buffer data");
            return GE_ERR_OUT_OF_MEMORY;
        }
        memcpy(buf->data, data, len * sizeof(uint32_t));
        r->memory_estimate += len * sizeof(uint32_t);
    }
    
    buf->next = r->buffers;
    r->buffers = buf;
    r->memory_estimate += sizeof(ge_buffer_t);
    
    return GE_OK;
}

static ge_status_t add_buffer_f64(ge_result_t* r, const char* name, const double* data, size_t len) {
    ge_buffer_t* buf = (ge_buffer_t*)calloc(1, sizeof(ge_buffer_t));
    if (!buf) {
        set_error("out of memory allocating buffer");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    strncpy(buf->name, name, sizeof(buf->name) - 1);
    buf->elem_size = sizeof(double);
    buf->len = len;
    
    if (len > 0) {
        buf->data = malloc(len * sizeof(double));
        if (!buf->data) {
            free(buf);
            set_error("out of memory allocating buffer data");
            return GE_ERR_OUT_OF_MEMORY;
        }
        memcpy(buf->data, data, len * sizeof(double));
        r->memory_estimate += len * sizeof(double);
    }
    
    buf->next = r->buffers;
    r->buffers = buf;
    r->memory_estimate += sizeof(ge_buffer_t);
    
    return GE_OK;
}

static ge_result_t* create_result(void) {
    ge_result_t* r = (ge_result_t*)calloc(1, sizeof(ge_result_t));
    if (r) {
        r->memory_estimate = sizeof(ge_result_t);
    }
    return r;
}

static igraph_neimode_t mode_to_igraph(ge_mode_t mode) {
    switch (mode) {
        case GE_MODE_OUT: return IGRAPH_OUT;
        case GE_MODE_IN:  return IGRAPH_IN;
        default:          return IGRAPH_ALL;
    }
}

// -----------------------------------------------------------------------------
// Graph lifecycle
// -----------------------------------------------------------------------------

ge_status_t ge_graph_create(const ge_edge_list_t* edges, ge_graph_t** out) {
    // Ensure thread-local RNG is initialized for thread safety
    ge_ensure_thread_rng();
    
    if (!edges || !out) {
        set_error("invalid argument: edges or out is NULL");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t* gg = (ge_graph_t*)calloc(1, sizeof(ge_graph_t));
    if (!gg) {
        set_error("out of memory allocating graph");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Build edge vector for igraph
    igraph_vector_int_t edge_vec;
    igraph_error_t err = igraph_vector_int_init(&edge_vec, (igraph_integer_t)(edges->m_edges * 2));
    if (err != IGRAPH_SUCCESS) {
        free(gg);
        set_error("igraph error initializing edge vector: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    for (size_t i = 0; i < edges->m_edges; i++) {
        VECTOR(edge_vec)[2*i]   = (igraph_integer_t)edges->src[i];
        VECTOR(edge_vec)[2*i+1] = (igraph_integer_t)edges->dst[i];
    }
    
    // Create igraph
    err = igraph_create(&gg->g, &edge_vec, (igraph_integer_t)edges->n_vertices, 
                        edges->directed ? IGRAPH_DIRECTED : IGRAPH_UNDIRECTED);
    igraph_vector_int_destroy(&edge_vec);
    
    if (err != IGRAPH_SUCCESS) {
        free(gg);
        set_error("igraph error creating graph: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    gg->n_vertices = edges->n_vertices;
    gg->m_edges = edges->m_edges;
    gg->directed = edges->directed;
    
    // Estimate memory: igraph uses roughly 24 bytes per vertex + 8 bytes per edge
    // Plus our wrapper structure
    gg->memory_estimate = sizeof(ge_graph_t) + 
                          edges->n_vertices * 24 + 
                          edges->m_edges * 8;
    
    *out = gg;
    return GE_OK;
}

void ge_graph_destroy(ge_graph_t* g) {
    if (!g) return;
    igraph_destroy(&g->g);
    free(g);
}

size_t ge_graph_memory_bytes(const ge_graph_t* g) {
    return g ? g->memory_estimate : 0;
}

uint32_t ge_graph_vcount(const ge_graph_t* g) {
    return g ? g->n_vertices : 0;
}

size_t ge_graph_ecount(const ge_graph_t* g) {
    return g ? g->m_edges : 0;
}

// -----------------------------------------------------------------------------
// View lifecycle
// -----------------------------------------------------------------------------

ge_status_t ge_view_create_by_edge_mask(
    const ge_graph_t* g,
    const uint8_t* edge_mask_bits,
    size_t edge_mask_nbytes,
    ge_view_t** out
) {
    if (!g || !edge_mask_bits || !out) {
        set_error("invalid argument: g, edge_mask_bits, or out is NULL");
        return GE_ERR_INVALID_ARG;
    }
    
    // Count included edges
    size_t included_edges = 0;
    for (size_t i = 0; i < g->m_edges; i++) {
        if (edge_mask_bits[i / 8] & (1 << (i % 8))) {
            included_edges++;
        }
    }
    
    ge_view_t* v = (ge_view_t*)calloc(1, sizeof(ge_view_t));
    if (!v) {
        set_error("out of memory allocating view");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Create edge selector
    igraph_es_t es;
    igraph_vector_int_t edge_ids;
    igraph_error_t err = igraph_vector_int_init(&edge_ids, (igraph_integer_t)included_edges);
    if (err != IGRAPH_SUCCESS) {
        free(v);
        set_error("igraph error initializing edge_ids vector: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Populate edge IDs and build edge map
    v->edge_map = (uint32_t*)malloc(included_edges * sizeof(uint32_t));
    if (!v->edge_map && included_edges > 0) {
        igraph_vector_int_destroy(&edge_ids);
        free(v);
        set_error("out of memory allocating edge map");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    size_t idx = 0;
    for (size_t i = 0; i < g->m_edges; i++) {
        if (edge_mask_bits[i / 8] & (1 << (i % 8))) {
            VECTOR(edge_ids)[idx] = (igraph_integer_t)i;
            v->edge_map[idx] = (uint32_t)i;
            idx++;
        }
    }
    
    // Create subgraph from edge selection
    err = igraph_es_vector(&es, &edge_ids);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&edge_ids);
        free(v->edge_map);
        free(v);
        set_error("igraph error creating edge selector: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    igraph_vector_int_t vertex_map_vec;
    igraph_vector_int_init(&vertex_map_vec, 0);
    
    err = igraph_subgraph_from_edges(&g->g, &v->g, es, 0 /* delete_vertices */);
    igraph_es_destroy(&es);
    igraph_vector_int_destroy(&edge_ids);
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&vertex_map_vec);
        free(v->edge_map);
        free(v);
        set_error("igraph error creating subgraph: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    v->n_vertices = (uint32_t)igraph_vcount(&v->g);
    v->m_edges = included_edges;
    
    // Build vertex map (identity for now since we don't delete vertices)
    v->vertex_map = (uint32_t*)malloc(v->n_vertices * sizeof(uint32_t));
    if (!v->vertex_map && v->n_vertices > 0) {
        igraph_destroy(&v->g);
        free(v->edge_map);
        free(v);
        set_error("out of memory allocating vertex map");
        return GE_ERR_OUT_OF_MEMORY;
    }
    for (uint32_t i = 0; i < v->n_vertices; i++) {
        v->vertex_map[i] = i;
    }
    
    igraph_vector_int_destroy(&vertex_map_vec);
    
    v->memory_estimate = sizeof(ge_view_t) + 
                         v->n_vertices * (24 + sizeof(uint32_t)) +
                         v->m_edges * (8 + sizeof(uint32_t));
    
    *out = v;
    return GE_OK;
}

void ge_view_destroy(ge_view_t* v) {
    if (!v) return;
    igraph_destroy(&v->g);
    free(v->vertex_map);
    free(v->edge_map);
    free(v);
}

size_t ge_view_memory_bytes(const ge_view_t* v) {
    return v ? v->memory_estimate : 0;
}

// -----------------------------------------------------------------------------
// Result lifecycle and extraction
// -----------------------------------------------------------------------------

void ge_result_destroy(ge_result_t* r) {
    if (!r) return;
    
    ge_buffer_t* buf = r->buffers;
    while (buf != NULL) {
        ge_buffer_t* next = buf->next;
        free(buf->data);
        free(buf);
        buf = next;
    }
    free(r);
}

size_t ge_result_memory_bytes(const ge_result_t* r) {
    return r ? r->memory_estimate : 0;
}

ge_status_t ge_result_get_u32(
    const ge_result_t* r,
    const char* name,
    const uint32_t** data,
    size_t* len
) {
    if (!r || !name || !data || !len) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_buffer_t* buf = find_buffer(r, name);
    if (!buf || buf->elem_size != sizeof(uint32_t)) {
        set_error("buffer not found: %s", name);
        return GE_ERR_NOT_FOUND;
    }
    
    *data = (const uint32_t*)buf->data;
    *len = buf->len;
    return GE_OK;
}

ge_status_t ge_result_get_f64(
    const ge_result_t* r,
    const char* name,
    const double** data,
    size_t* len
) {
    if (!r || !name || !data || !len) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_buffer_t* buf = find_buffer(r, name);
    if (!buf || buf->elem_size != sizeof(double)) {
        set_error("buffer not found: %s", name);
        return GE_ERR_NOT_FOUND;
    }
    
    *data = (const double*)buf->data;
    *len = buf->len;
    return GE_OK;
}

ge_status_t ge_result_get_bytes(
    const ge_result_t* r,
    const char* name,
    const uint8_t** data,
    size_t* len
) {
    if (!r || !name || !data || !len) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_buffer_t* buf = find_buffer(r, name);
    if (!buf || buf->elem_size != 1) {
        set_error("buffer not found: %s", name);
        return GE_ERR_NOT_FOUND;
    }
    
    *data = (const uint8_t*)buf->data;
    *len = buf->len;
    return GE_OK;
}

// -----------------------------------------------------------------------------
// Algorithm: Connected Components
// -----------------------------------------------------------------------------

ge_status_t ge_run_components(
    const ge_graph_t* g,
    int weak,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t membership;
    igraph_vector_int_t csize;
    igraph_integer_t num_components;
    
    igraph_error_t err = igraph_vector_int_init(&membership, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error initializing membership vector: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_int_init(&csize, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&membership);
        set_error("igraph error initializing csize vector: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_connected_components(
        &g->g,
        &membership,
        &csize,
        &num_components,
        weak ? IGRAPH_WEAK : IGRAPH_STRONG
    );
    
    igraph_vector_int_destroy(&csize);
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&membership);
        set_error("igraph error computing components: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Create result
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&membership);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Copy membership to uint32 array
    size_t n = (size_t)igraph_vector_int_size(&membership);
    uint32_t* membership_u32 = (uint32_t*)malloc(n * sizeof(uint32_t));
    if (!membership_u32 && n > 0) {
        igraph_vector_int_destroy(&membership);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    for (size_t i = 0; i < n; i++) {
        membership_u32[i] = (uint32_t)VECTOR(membership)[i];
    }
    igraph_vector_int_destroy(&membership);
    
    ge_status_t status = add_buffer_u32(r, "membership", membership_u32, n);
    free(membership_u32);
    
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    // Add num_components
    uint32_t num_comp_u32 = (uint32_t)num_components;
    status = add_buffer_u32(r, "num_components", &num_comp_u32, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_components_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    int weak,
    ge_result_t** out
) {
    // For views, we run on the view's subgraph
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    // Create a temporary graph wrapper for the view
    ge_graph_t temp_g;
    temp_g.g = v->g;  // Note: shallow copy, don't destroy
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_components(&temp_g, weak, out);
}

// -----------------------------------------------------------------------------
// Algorithm: Shortest Path
// -----------------------------------------------------------------------------

ge_status_t ge_run_shortest_path(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    int return_vertices,
    int return_edges,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    if (src >= g->n_vertices || dst >= g->n_vertices) {
        set_error("vertex index out of bounds");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t vertices;
    igraph_vector_int_t edges;
    
    igraph_error_t err = igraph_vector_int_init(&vertices, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_int_init(&edges, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&vertices);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Set up weights if provided
    igraph_vector_t weights_vec;
    igraph_vector_t* weights_ptr = NULL;
    
    if (weights_or_null) {
        err = igraph_vector_init(&weights_vec, (igraph_integer_t)g->m_edges);
        if (err != IGRAPH_SUCCESS) {
            igraph_vector_int_destroy(&vertices);
            igraph_vector_int_destroy(&edges);
            set_error("igraph error initializing weights: %d", err);
            return GE_ERR_IGRAPH;
        }
        for (size_t i = 0; i < g->m_edges; i++) {
            VECTOR(weights_vec)[i] = weights_or_null[i];
        }
        weights_ptr = &weights_vec;
    }
    
    // Run shortest path (use dijkstra for weighted, BFS for unweighted)
    if (weights_ptr) {
        err = igraph_get_shortest_path_dijkstra(
            &g->g,
            &vertices,
            &edges,
            (igraph_integer_t)src,
            (igraph_integer_t)dst,
            weights_ptr,
            IGRAPH_OUT
        );
    } else {
        err = igraph_get_shortest_path(
            &g->g,
            &vertices,
            &edges,
            (igraph_integer_t)src,
            (igraph_integer_t)dst,
            IGRAPH_OUT
        );
    }
    
    if (weights_ptr) {
        igraph_vector_destroy(&weights_vec);
    }
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&vertices);
        igraph_vector_int_destroy(&edges);
        set_error("igraph error computing shortest path: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Create result
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&vertices);
        igraph_vector_int_destroy(&edges);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Check if path was found
    size_t path_len = (size_t)igraph_vector_int_size(&vertices);
    uint32_t found = (path_len > 0) ? 1 : 0;
    
    ge_status_t status = add_buffer_u32(r, "found", &found, 1);
    if (status != GE_OK) {
        igraph_vector_int_destroy(&vertices);
        igraph_vector_int_destroy(&edges);
        ge_result_destroy(r);
        return status;
    }
    
    if (found && return_vertices) {
        uint32_t* path_vertices = (uint32_t*)malloc(path_len * sizeof(uint32_t));
        if (!path_vertices) {
            igraph_vector_int_destroy(&vertices);
            igraph_vector_int_destroy(&edges);
            ge_result_destroy(r);
            set_error("out of memory");
            return GE_ERR_OUT_OF_MEMORY;
        }
        for (size_t i = 0; i < path_len; i++) {
            path_vertices[i] = (uint32_t)VECTOR(vertices)[i];
        }
        status = add_buffer_u32(r, "path_vertices", path_vertices, path_len);
        free(path_vertices);
        if (status != GE_OK) {
            igraph_vector_int_destroy(&vertices);
            igraph_vector_int_destroy(&edges);
            ge_result_destroy(r);
            return status;
        }
    }
    
    if (found && return_edges) {
        size_t edge_len = (size_t)igraph_vector_int_size(&edges);
        uint32_t* path_edges = (uint32_t*)malloc(edge_len * sizeof(uint32_t));
        if (!path_edges && edge_len > 0) {
            igraph_vector_int_destroy(&vertices);
            igraph_vector_int_destroy(&edges);
            ge_result_destroy(r);
            set_error("out of memory");
            return GE_ERR_OUT_OF_MEMORY;
        }
        for (size_t i = 0; i < edge_len; i++) {
            path_edges[i] = (uint32_t)VECTOR(edges)[i];
        }
        status = add_buffer_u32(r, "path_edges", path_edges, edge_len);
        free(path_edges);
        if (status != GE_OK) {
            igraph_vector_int_destroy(&vertices);
            igraph_vector_int_destroy(&edges);
            ge_result_destroy(r);
            return status;
        }
    }
    
    // Compute total cost (infinity if no path)
    double total_cost = found ? 0.0 : INFINITY;
    if (found) {
        if (weights_or_null) {
            size_t edge_len = (size_t)igraph_vector_int_size(&edges);
            for (size_t i = 0; i < edge_len; i++) {
                igraph_integer_t eid = VECTOR(edges)[i];
                total_cost += weights_or_null[eid];
            }
        } else {
            total_cost = (double)(path_len - 1);  // hop count
        }
    }
    
    status = add_buffer_f64(r, "total_cost", &total_cost, 1);
    
    igraph_vector_int_destroy(&vertices);
    igraph_vector_int_destroy(&edges);
    
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_shortest_path_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    int return_vertices,
    int return_edges,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    // Run on view's subgraph
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_shortest_path(&temp_g, src, dst, weights_or_null, return_vertices, return_edges, out);
}

// -----------------------------------------------------------------------------
// Algorithm: K-Shortest Paths
// -----------------------------------------------------------------------------

ge_status_t ge_run_ksp(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    uint32_t k,
    uint32_t max_candidates,
    ge_result_t** out
) {
    if (!g || !out || k == 0) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    if (src >= g->n_vertices || dst >= g->n_vertices) {
        set_error("vertex index out of bounds");
        return GE_ERR_INVALID_ARG;
    }
    
    // Set up weights
    igraph_vector_t weights_vec;
    igraph_vector_t* weights_ptr = NULL;
    
    if (weights_or_null) {
        igraph_error_t err = igraph_vector_init(&weights_vec, (igraph_integer_t)g->m_edges);
        if (err != IGRAPH_SUCCESS) {
            set_error("igraph error initializing weights: %d", err);
            return GE_ERR_IGRAPH;
        }
        for (size_t i = 0; i < g->m_edges; i++) {
            VECTOR(weights_vec)[i] = weights_or_null[i];
        }
        weights_ptr = &weights_vec;
    }
    
    // Use igraph's k-shortest paths (Yen's algorithm)
    igraph_vector_int_list_t paths;
    igraph_error_t err = igraph_vector_int_list_init(&paths, 0);
    if (err != IGRAPH_SUCCESS) {
        if (weights_ptr) igraph_vector_destroy(&weights_vec);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_get_k_shortest_paths(
        &g->g,
        weights_ptr,
        &paths,
        NULL,  // edges (not needed)
        (igraph_integer_t)k,
        (igraph_integer_t)src,
        (igraph_integer_t)dst,
        IGRAPH_OUT
    );
    
    if (weights_ptr) {
        igraph_vector_destroy(&weights_vec);
    }
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_list_destroy(&paths);
        set_error("igraph error computing k-shortest paths: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Create result
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_list_destroy(&paths);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    igraph_integer_t num_paths = igraph_vector_int_list_size(&paths);
    uint32_t num_paths_u32 = (uint32_t)num_paths;
    
    ge_status_t status = add_buffer_u32(r, "num_paths", &num_paths_u32, 1);
    if (status != GE_OK) {
        igraph_vector_int_list_destroy(&paths);
        ge_result_destroy(r);
        return status;
    }
    
    // Build ragged array encoding
    // path_offsets: length num_paths+1
    // path_vertices: concatenated vertices
    uint32_t* offsets = (uint32_t*)malloc((num_paths + 1) * sizeof(uint32_t));
    if (!offsets) {
        igraph_vector_int_list_destroy(&paths);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // First pass: compute offsets and total size
    size_t total_vertices = 0;
    offsets[0] = 0;
    for (igraph_integer_t i = 0; i < num_paths; i++) {
        igraph_vector_int_t* path = igraph_vector_int_list_get_ptr(&paths, i);
        total_vertices += (size_t)igraph_vector_int_size(path);
        offsets[i + 1] = (uint32_t)total_vertices;
    }
    
    // Allocate concatenated vertices
    uint32_t* all_vertices = (uint32_t*)malloc(total_vertices * sizeof(uint32_t));
    double* costs = (double*)malloc(num_paths * sizeof(double));
    if ((!all_vertices && total_vertices > 0) || (!costs && num_paths > 0)) {
        free(offsets);
        free(all_vertices);
        free(costs);
        igraph_vector_int_list_destroy(&paths);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Second pass: fill vertices and compute costs
    size_t vertex_idx = 0;
    for (igraph_integer_t i = 0; i < num_paths; i++) {
        igraph_vector_int_t* path = igraph_vector_int_list_get_ptr(&paths, i);
        igraph_integer_t path_len = igraph_vector_int_size(path);
        
        costs[i] = 0.0;
        for (igraph_integer_t j = 0; j < path_len; j++) {
            all_vertices[vertex_idx++] = (uint32_t)VECTOR(*path)[j];
        }
        
        // Compute cost (hop count if no weights)
        if (weights_or_null && path_len > 1) {
            // Would need edge info to compute weighted cost
            // For now, use hop count as approximation
            costs[i] = (double)(path_len - 1);
        } else {
            costs[i] = (double)(path_len > 0 ? path_len - 1 : 0);
        }
    }
    
    igraph_vector_int_list_destroy(&paths);
    
    // Add buffers
    status = add_buffer_u32(r, "path_offsets", offsets, num_paths + 1);
    free(offsets);
    if (status != GE_OK) {
        free(all_vertices);
        free(costs);
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_u32(r, "path_vertices", all_vertices, total_vertices);
    free(all_vertices);
    if (status != GE_OK) {
        free(costs);
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_f64(r, "path_costs", costs, num_paths);
    free(costs);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_ksp_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* weights_or_null,
    uint32_t k,
    uint32_t max_candidates,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_ksp(&temp_g, src, dst, weights_or_null, k, max_candidates, out);
}

// -----------------------------------------------------------------------------
// Algorithm: s-t Minimum Cut
// -----------------------------------------------------------------------------

ge_status_t ge_run_st_mincut(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t dst,
    const double* capacity_or_null,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    if (src >= g->n_vertices || dst >= g->n_vertices) {
        set_error("vertex index out of bounds");
        return GE_ERR_INVALID_ARG;
    }
    
    // Set up capacities
    igraph_vector_t capacity_vec;
    igraph_vector_t* capacity_ptr = NULL;
    
    if (capacity_or_null) {
        igraph_error_t err = igraph_vector_init(&capacity_vec, (igraph_integer_t)g->m_edges);
        if (err != IGRAPH_SUCCESS) {
            set_error("igraph error initializing capacity: %d", err);
            return GE_ERR_IGRAPH;
        }
        for (size_t i = 0; i < g->m_edges; i++) {
            VECTOR(capacity_vec)[i] = capacity_or_null[i];
        }
        capacity_ptr = &capacity_vec;
    }
    
    igraph_real_t cut_value;
    igraph_vector_int_t partition1;
    igraph_vector_int_t cut_edges;
    
    igraph_error_t err = igraph_vector_int_init(&partition1, 0);
    if (err != IGRAPH_SUCCESS) {
        if (capacity_ptr) igraph_vector_destroy(&capacity_vec);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_int_init(&cut_edges, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&partition1);
        if (capacity_ptr) igraph_vector_destroy(&capacity_vec);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_st_mincut(
        &g->g,
        &cut_value,
        &cut_edges,
        &partition1,
        NULL,  // partition2
        (igraph_integer_t)src,
        (igraph_integer_t)dst,
        capacity_ptr
    );
    
    if (capacity_ptr) {
        igraph_vector_destroy(&capacity_vec);
    }
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&partition1);
        igraph_vector_int_destroy(&cut_edges);
        set_error("igraph error computing st-mincut: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Create result
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&partition1);
        igraph_vector_int_destroy(&cut_edges);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Add cut value
    ge_status_t status = add_buffer_f64(r, "cut_value", &cut_value, 1);
    if (status != GE_OK) {
        igraph_vector_int_destroy(&partition1);
        igraph_vector_int_destroy(&cut_edges);
        ge_result_destroy(r);
        return status;
    }
    
    // Add source side vertices
    size_t n_source_side = (size_t)igraph_vector_int_size(&partition1);
    uint32_t* source_side = (uint32_t*)malloc(n_source_side * sizeof(uint32_t));
    if (!source_side && n_source_side > 0) {
        igraph_vector_int_destroy(&partition1);
        igraph_vector_int_destroy(&cut_edges);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    for (size_t i = 0; i < n_source_side; i++) {
        source_side[i] = (uint32_t)VECTOR(partition1)[i];
    }
    igraph_vector_int_destroy(&partition1);
    
    status = add_buffer_u32(r, "source_side", source_side, n_source_side);
    free(source_side);
    if (status != GE_OK) {
        igraph_vector_int_destroy(&cut_edges);
        ge_result_destroy(r);
        return status;
    }
    
    // Add cut edges
    size_t n_cut_edges = (size_t)igraph_vector_int_size(&cut_edges);
    uint32_t* cut_edges_arr = (uint32_t*)malloc(n_cut_edges * sizeof(uint32_t));
    if (!cut_edges_arr && n_cut_edges > 0) {
        igraph_vector_int_destroy(&cut_edges);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    for (size_t i = 0; i < n_cut_edges; i++) {
        cut_edges_arr[i] = (uint32_t)VECTOR(cut_edges)[i];
    }
    igraph_vector_int_destroy(&cut_edges);
    
    status = add_buffer_u32(r, "cut_edges", cut_edges_arr, n_cut_edges);
    free(cut_edges_arr);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_st_mincut_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t dst,
    const double* capacity_or_null,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_st_mincut(&temp_g, src, dst, capacity_or_null, out);
}

// -----------------------------------------------------------------------------
// Algorithm: BFS
// -----------------------------------------------------------------------------

ge_status_t ge_run_bfs(
    const ge_graph_t* g,
    uint32_t src,
    uint32_t max_depth,
    ge_mode_t mode,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    if (src >= g->n_vertices) {
        set_error("vertex index out of bounds");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t order;
    igraph_vector_int_t dist;
    igraph_vector_int_t father;
    
    igraph_error_t err = igraph_vector_int_init(&order, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_int_init(&dist, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&order);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_int_init(&father, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&order);
        igraph_vector_int_destroy(&dist);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_bfs(
        &g->g,
        (igraph_integer_t)src,
        NULL,  // roots
        mode_to_igraph(mode),
        0,     // unreachable
        NULL,  // restricted
        &order,
        NULL,  // rank
        &father,
        NULL,  // pred
        NULL,  // succ
        &dist,
        NULL,  // callback
        NULL   // extra
    );
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&order);
        igraph_vector_int_destroy(&dist);
        igraph_vector_int_destroy(&father);
        set_error("igraph error running BFS: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Filter by max_depth if specified
    size_t n = (size_t)igraph_vector_int_size(&order);
    size_t count = 0;
    
    // Count vertices within depth
    for (size_t i = 0; i < n; i++) {
        igraph_integer_t v = VECTOR(order)[i];
        if (v < 0) continue;  // -1 indicates unreachable
        if (max_depth > 0 && VECTOR(dist)[v] > (igraph_integer_t)max_depth) continue;
        count++;
    }
    
    // Create result
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&order);
        igraph_vector_int_destroy(&dist);
        igraph_vector_int_destroy(&father);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    uint32_t* visited = (uint32_t*)malloc(count * sizeof(uint32_t));
    uint32_t* depths = (uint32_t*)malloc(count * sizeof(uint32_t));
    uint32_t* parents = (uint32_t*)malloc(count * sizeof(uint32_t));
    
    if ((!visited || !depths || !parents) && count > 0) {
        free(visited);
        free(depths);
        free(parents);
        igraph_vector_int_destroy(&order);
        igraph_vector_int_destroy(&dist);
        igraph_vector_int_destroy(&father);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    size_t idx = 0;
    for (size_t i = 0; i < n && idx < count; i++) {
        igraph_integer_t v = VECTOR(order)[i];
        if (v < 0) continue;
        igraph_integer_t d = VECTOR(dist)[v];
        if (max_depth > 0 && d > (igraph_integer_t)max_depth) continue;
        
        visited[idx] = (uint32_t)v;
        depths[idx] = (uint32_t)d;
        igraph_integer_t f = VECTOR(father)[v];
        parents[idx] = (f < 0) ? UINT32_MAX : (uint32_t)f;
        idx++;
    }
    
    igraph_vector_int_destroy(&order);
    igraph_vector_int_destroy(&dist);
    igraph_vector_int_destroy(&father);
    
    ge_status_t status = add_buffer_u32(r, "visited", visited, count);
    free(visited);
    if (status != GE_OK) {
        free(depths);
        free(parents);
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_u32(r, "depths", depths, count);
    free(depths);
    if (status != GE_OK) {
        free(parents);
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_u32(r, "parents", parents, count);
    free(parents);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_bfs_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t src,
    uint32_t max_depth,
    ge_mode_t mode,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_bfs(&temp_g, src, max_depth, mode, out);
}

// -----------------------------------------------------------------------------
// Algorithm: Neighborhood Query
// -----------------------------------------------------------------------------

ge_status_t ge_run_neighborhood(
    const ge_graph_t* g,
    const uint32_t* seeds,
    size_t n_seeds,
    uint32_t hops,
    ge_mode_t mode,
    ge_result_t** out
) {
    if (!g || !seeds || !out || n_seeds == 0) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    // Use BFS to compute exact distances from seeds
    // This is more efficient and accurate than using igraph_neighborhood + igraph_distances
    
    uint32_t* min_dist = (uint32_t*)malloc(g->n_vertices * sizeof(uint32_t));
    if (!min_dist) {
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Initialize all distances to UINT32_MAX (unreachable)
    for (uint32_t i = 0; i < g->n_vertices; i++) {
        min_dist[i] = UINT32_MAX;
    }
    
    // BFS queue - simple array-based queue
    uint32_t* queue = (uint32_t*)malloc(g->n_vertices * sizeof(uint32_t));
    if (!queue) {
        free(min_dist);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    size_t queue_head = 0;
    size_t queue_tail = 0;
    
    // Initialize seeds at distance 0
    for (size_t i = 0; i < n_seeds; i++) {
        if (seeds[i] < g->n_vertices && min_dist[seeds[i]] == UINT32_MAX) {
            min_dist[seeds[i]] = 0;
            queue[queue_tail++] = seeds[i];
        }
    }
    
    // BFS traversal
    igraph_vector_int_t neighbors;
    igraph_error_t err = igraph_vector_int_init(&neighbors, 0);
    if (err != IGRAPH_SUCCESS) {
        free(min_dist);
        free(queue);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    while (queue_head < queue_tail) {
        uint32_t v = queue[queue_head++];
        uint32_t v_dist = min_dist[v];
        
        // Stop if we've reached the hop limit
        if (v_dist >= hops) {
            continue;
        }
        
        // Get neighbors of v
        err = igraph_neighbors(&g->g, &neighbors, (igraph_integer_t)v, mode_to_igraph(mode));
        if (err != IGRAPH_SUCCESS) {
            igraph_vector_int_destroy(&neighbors);
            free(min_dist);
            free(queue);
            set_error("igraph error getting neighbors: %d", err);
            return GE_ERR_IGRAPH;
        }
        
        igraph_integer_t n_neighbors = igraph_vector_int_size(&neighbors);
        for (igraph_integer_t i = 0; i < n_neighbors; i++) {
            igraph_integer_t u = VECTOR(neighbors)[i];
            if (u >= 0 && u < (igraph_integer_t)g->n_vertices) {
                if (min_dist[u] == UINT32_MAX) {
                    min_dist[u] = v_dist + 1;
                    queue[queue_tail++] = (uint32_t)u;
                }
            }
        }
    }
    
    igraph_vector_int_destroy(&neighbors);
    free(queue);
    
    // Count vertices in neighborhood (distance <= hops)
    size_t count = 0;
    for (uint32_t i = 0; i < g->n_vertices; i++) {
        if (min_dist[i] <= hops) {
            count++;
        }
    }
    
    ge_result_t* r = create_result();
    if (!r) {
        free(min_dist);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    uint32_t* vertices = (uint32_t*)malloc(count * sizeof(uint32_t));
    uint32_t* distances = (uint32_t*)malloc(count * sizeof(uint32_t));
    if ((!vertices || !distances) && count > 0) {
        free(vertices);
        free(distances);
        free(min_dist);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    size_t idx = 0;
    for (uint32_t i = 0; i < g->n_vertices && idx < count; i++) {
        if (min_dist[i] <= hops) {
            vertices[idx] = i;
            distances[idx] = min_dist[i];
            idx++;
        }
    }
    
    free(min_dist);
    
    ge_status_t status = add_buffer_u32(r, "vertices", vertices, count);
    free(vertices);
    if (status != GE_OK) {
        free(distances);
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_u32(r, "distances", distances, count);
    free(distances);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_neighborhood_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    const uint32_t* seeds,
    size_t n_seeds,
    uint32_t hops,
    ge_mode_t mode,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_neighborhood(&temp_g, seeds, n_seeds, hops, mode, out);
}

// -----------------------------------------------------------------------------
// Algorithm: Community Detection - Leiden
// -----------------------------------------------------------------------------

ge_status_t ge_run_communities_leiden(
    const ge_graph_t* g,
    double resolution,
    ge_result_t** out
) {
    // Ensure thread-local RNG is initialized (Leiden uses randomness)
    ge_ensure_thread_rng();
    
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t membership;
    igraph_integer_t n_communities;
    igraph_real_t quality;
    
    igraph_error_t err = igraph_vector_int_init(&membership, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_community_leiden(
        &g->g,
        NULL,  // edge_weights
        NULL,  // node_weights
        resolution,
        0.01,  // beta (randomness)
        0,     // start (from scratch)
        4,     // n_iterations
        &membership,
        &n_communities,
        &quality
    );
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&membership);
        set_error("igraph error running Leiden: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&membership);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Copy membership
    size_t n = (size_t)igraph_vector_int_size(&membership);
    uint32_t* membership_u32 = (uint32_t*)malloc(n * sizeof(uint32_t));
    if (!membership_u32 && n > 0) {
        igraph_vector_int_destroy(&membership);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    for (size_t i = 0; i < n; i++) {
        membership_u32[i] = (uint32_t)VECTOR(membership)[i];
    }
    igraph_vector_int_destroy(&membership);
    
    ge_status_t status = add_buffer_u32(r, "membership", membership_u32, n);
    free(membership_u32);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    // Add modularity
    status = add_buffer_f64(r, "modularity", &quality, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    // Add num_communities
    uint32_t n_comm_u32 = (uint32_t)n_communities;
    status = add_buffer_u32(r, "num_communities", &n_comm_u32, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_communities_leiden_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    double resolution,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_communities_leiden(&temp_g, resolution, out);
}

// -----------------------------------------------------------------------------
// Algorithm: Community Detection - Louvain
// -----------------------------------------------------------------------------

ge_status_t ge_run_communities_louvain(
    const ge_graph_t* g,
    double resolution,
    ge_result_t** out
) {
    // Ensure thread-local RNG is initialized (Louvain uses randomness)
    ge_ensure_thread_rng();
    
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t membership;
    igraph_vector_t modularity_vec;
    
    igraph_error_t err = igraph_vector_int_init(&membership, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_vector_init(&modularity_vec, 0);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&membership);
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_community_multilevel(
        &g->g,
        NULL,  // weights
        resolution,
        &membership,
        NULL,  // memberships (intermediate)
        &modularity_vec
    );
    
    // Get final modularity (last element in vector)
    igraph_real_t modularity = igraph_vector_size(&modularity_vec) > 0 ? 
        VECTOR(modularity_vec)[igraph_vector_size(&modularity_vec) - 1] : 0.0;
    igraph_vector_destroy(&modularity_vec);
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&membership);
        set_error("igraph error running Louvain: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&membership);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Copy membership
    size_t n = (size_t)igraph_vector_int_size(&membership);
    uint32_t* membership_u32 = (uint32_t*)malloc(n * sizeof(uint32_t));
    if (!membership_u32 && n > 0) {
        igraph_vector_int_destroy(&membership);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    for (size_t i = 0; i < n; i++) {
        membership_u32[i] = (uint32_t)VECTOR(membership)[i];
    }
    igraph_vector_int_destroy(&membership);
    
    ge_status_t status = add_buffer_u32(r, "membership", membership_u32, n);
    free(membership_u32);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    // Add modularity
    status = add_buffer_f64(r, "modularity", &modularity, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    // Count communities
    uint32_t max_comm = 0;
    for (size_t i = 0; i < n; i++) {
        if (membership_u32[i] > max_comm) {
            max_comm = membership_u32[i];
        }
    }
    uint32_t n_communities = max_comm + 1;
    status = add_buffer_u32(r, "num_communities", &n_communities, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_communities_louvain_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    double resolution,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_communities_louvain(&temp_g, resolution, out);
}

// -----------------------------------------------------------------------------
// Algorithm: K-Core Decomposition
// -----------------------------------------------------------------------------

ge_status_t ge_run_kcore(
    const ge_graph_t* g,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_int_t coreness;
    igraph_error_t err = igraph_vector_int_init(&coreness, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    err = igraph_coreness(&g->g, &coreness, IGRAPH_ALL);
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_int_destroy(&coreness);
        set_error("igraph error computing coreness: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_int_destroy(&coreness);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Copy coreness values and find max
    size_t n = (size_t)igraph_vector_int_size(&coreness);
    uint32_t* coreness_u32 = (uint32_t*)malloc(n * sizeof(uint32_t));
    if (!coreness_u32 && n > 0) {
        igraph_vector_int_destroy(&coreness);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    uint32_t max_core = 0;
    for (size_t i = 0; i < n; i++) {
        coreness_u32[i] = (uint32_t)VECTOR(coreness)[i];
        if (coreness_u32[i] > max_core) {
            max_core = coreness_u32[i];
        }
    }
    igraph_vector_int_destroy(&coreness);
    
    ge_status_t status = add_buffer_u32(r, "coreness", coreness_u32, n);
    free(coreness_u32);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    status = add_buffer_u32(r, "max_core", &max_core, 1);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_kcore_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_kcore(&temp_g, out);
}

// -----------------------------------------------------------------------------
// Algorithm: Betweenness Centrality
// -----------------------------------------------------------------------------

ge_status_t ge_run_betweenness(
    const ge_graph_t* g,
    uint32_t sample_size,
    int normalized,
    const double* weights_or_null,
    ge_result_t** out
) {
    if (!g || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    igraph_vector_t betweenness;
    igraph_error_t err = igraph_vector_init(&betweenness, 0);
    if (err != IGRAPH_SUCCESS) {
        set_error("igraph error: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Set up weights if provided
    igraph_vector_t weights_vec;
    igraph_vector_t* weights_ptr = NULL;
    
    if (weights_or_null) {
        err = igraph_vector_init(&weights_vec, (igraph_integer_t)g->m_edges);
        if (err != IGRAPH_SUCCESS) {
            igraph_vector_destroy(&betweenness);
            set_error("igraph error initializing weights: %d", err);
            return GE_ERR_IGRAPH;
        }
        for (size_t i = 0; i < g->m_edges; i++) {
            VECTOR(weights_vec)[i] = weights_or_null[i];
        }
        weights_ptr = &weights_vec;
    }
    
    if (sample_size > 0 && sample_size < g->n_vertices) {
        // Sampled betweenness using a subset of vertices
        igraph_vs_t vids;
        igraph_vector_int_t sources;
        
        err = igraph_vector_int_init(&sources, sample_size);
        if (err != IGRAPH_SUCCESS) {
            if (weights_ptr) igraph_vector_destroy(&weights_vec);
            igraph_vector_destroy(&betweenness);
            set_error("igraph error: %d", err);
            return GE_ERR_IGRAPH;
        }
        
        // Random sampling of source vertices
        for (uint32_t i = 0; i < sample_size; i++) {
            VECTOR(sources)[i] = (igraph_integer_t)(rand() % g->n_vertices);
        }
        
        err = igraph_vs_vector(&vids, &sources);
        if (err != IGRAPH_SUCCESS) {
            igraph_vector_int_destroy(&sources);
            if (weights_ptr) igraph_vector_destroy(&weights_vec);
            igraph_vector_destroy(&betweenness);
            set_error("igraph error: %d", err);
            return GE_ERR_IGRAPH;
        }
        
        // Use igraph_betweenness_subset for sampled computation
        err = igraph_betweenness_subset(
            &g->g,
            &betweenness,
            igraph_vss_all(),  // vertices to compute betweenness for
            g->directed ? IGRAPH_DIRECTED : IGRAPH_UNDIRECTED,
            vids,              // source vertices for paths
            igraph_vss_all(),  // target vertices for paths
            weights_ptr
        );
        
        igraph_vs_destroy(&vids);
        igraph_vector_int_destroy(&sources);
    } else {
        // Full betweenness computation
        err = igraph_betweenness(
            &g->g,
            &betweenness,
            igraph_vss_all(),
            g->directed ? IGRAPH_DIRECTED : IGRAPH_UNDIRECTED,
            weights_ptr
        );
    }
    
    if (weights_ptr) {
        igraph_vector_destroy(&weights_vec);
    }
    
    if (err != IGRAPH_SUCCESS) {
        igraph_vector_destroy(&betweenness);
        set_error("igraph error computing betweenness: %d", err);
        return GE_ERR_IGRAPH;
    }
    
    // Normalize if requested
    if (normalized && g->n_vertices > 2) {
        igraph_integer_t n = (igraph_integer_t)g->n_vertices;
        double norm_factor;
        if (g->directed) {
            norm_factor = 1.0 / ((n - 1) * (n - 2));
        } else {
            norm_factor = 2.0 / ((n - 1) * (n - 2));
        }
        for (igraph_integer_t i = 0; i < igraph_vector_size(&betweenness); i++) {
            VECTOR(betweenness)[i] *= norm_factor;
        }
    }
    
    ge_result_t* r = create_result();
    if (!r) {
        igraph_vector_destroy(&betweenness);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    // Copy scores
    size_t n = (size_t)igraph_vector_size(&betweenness);
    double* scores = (double*)malloc(n * sizeof(double));
    if (!scores && n > 0) {
        igraph_vector_destroy(&betweenness);
        ge_result_destroy(r);
        set_error("out of memory");
        return GE_ERR_OUT_OF_MEMORY;
    }
    
    for (size_t i = 0; i < n; i++) {
        scores[i] = VECTOR(betweenness)[i];
    }
    igraph_vector_destroy(&betweenness);
    
    ge_status_t status = add_buffer_f64(r, "scores", scores, n);
    free(scores);
    if (status != GE_OK) {
        ge_result_destroy(r);
        return status;
    }
    
    *out = r;
    return GE_OK;
}

ge_status_t ge_run_betweenness_view(
    const ge_graph_t* g,
    const ge_view_t* v,
    uint32_t sample_size,
    int normalized,
    const double* weights_or_null,
    ge_result_t** out
) {
    if (!g || !v || !out) {
        set_error("invalid argument");
        return GE_ERR_INVALID_ARG;
    }
    
    ge_graph_t temp_g;
    temp_g.g = v->g;
    temp_g.n_vertices = v->n_vertices;
    temp_g.m_edges = v->m_edges;
    temp_g.directed = g->directed;
    
    return ge_run_betweenness(&temp_g, sample_size, normalized, weights_or_null, out);
}

// -----------------------------------------------------------------------------
// Version info
// -----------------------------------------------------------------------------

const char* ge_shim_version(void) {
    return GE_SHIM_VERSION;
}

const char* ge_igraph_version(void) {
    const char* version;
    igraph_version(&version, NULL, NULL, NULL);
    return version;
}
