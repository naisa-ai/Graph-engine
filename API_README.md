# Graph-Engine API Reference

This document describes the gRPC API for the Graph-Engine service.

## Services Overview

| Service | Purpose |
|---------|---------|
| `GraphEngine` | Core operations: build graphs, create views, run algorithms |
| `GraphEngineOps` | Operational APIs: health, monitoring, debugging |

---

## GraphEngine Service

### BeginBuild

Start a new graph build session.

```
rpc BeginBuild(BeginBuildRequest) returns (BeginBuildResponse)
```

**Request:**
```pseudo
BeginBuildRequest {
    graph_name: string           // Name of the graph (e.g., "network-topology")
    directed: bool               // true for directed graph, false for undirected
    node_id_format: enum {       // Optional: hint for node ID format
        NODE_ID_UINT64 = 0       // Default: numeric IDs
        NODE_ID_STRING = 1       // String-based IDs
    }
    schema: Schema {             // Optional: column definitions
        vertex_columns: [ColumnDef]
        edge_columns: [ColumnDef]
    }
    labels: map<string, string>  // Optional: metadata (tenant, env, etc.)
}

ColumnDef {
    name: string                 // Column name (e.g., "partition_id")
    type: enum {
        COL_BOOL, COL_U32, COL_U64, COL_F32, COL_F64, COL_STRING
    }
}
```

**Response:**
```pseudo
BeginBuildResponse {
    build_id: string             // UUID for this build session
}
```

**Example:**
```pseudo
// Request
{
    graph_name: "datacenter-network",
    directed: true,
    labels: {"tenant": "acme", "env": "prod"}
}

// Response
{
    build_id: "550e8400-e29b-41d4-a716-446655440000"
}
```

---

### Upload (Streaming)

Stream vertices and edges into an active build.

```
rpc Upload(stream UploadRequest) returns (UploadResponse)
```

**Request (streamed):**
```pseudo
UploadRequest {
    build_id: string             // From BeginBuild response
    
    // One of the following payloads per message:
    payload: oneof {
        vertices: VertexChunk {
            node_id_u64: [uint64]    // Numeric node IDs
            node_id_str: [string]    // OR string node IDs
        }
        
        edges: EdgeChunk {
            src_u64: [uint64]        // Source node IDs (numeric)
            dst_u64: [uint64]        // Destination node IDs (numeric)
            src_str: [string]        // OR source node IDs (string)
            dst_str: [string]        // OR destination node IDs (string)
            edge_id_u64: [uint64]    // Optional: stable edge IDs
            kind: [uint32]           // Optional: edge type/kind
            weight: [float]          // Optional: edge weights
        }
        
        vertex_columns: ColumnChunk  // Additional vertex attributes
        edge_columns: ColumnChunk    // Additional edge attributes
        finalize: bool               // Optional: explicit end marker
    }
}

ColumnChunk {
    name: string                 // Column name
    type: ColumnType             // Data type
    v_bool: [bool]               // Values (use matching type)
    v_u32: [uint32]
    v_u64: [uint64]
    v_f32: [float]
    v_f64: [double]
    v_string: [string]
}
```

**Response:**
```pseudo
UploadResponse {
    build_id: string
    received_vertices: uint64    // Total vertices received
    received_edges: uint64       // Total edges received
}
```

**Example:**
```pseudo
// Stream message 1: Vertices
{
    build_id: "550e8400-...",
    vertices: {
        node_id_u64: [1, 2, 3, 4, 5]
    }
}

// Stream message 2: Edges
{
    build_id: "550e8400-...",
    edges: {
        src_u64: [1, 1, 2, 3],
        dst_u64: [2, 3, 4, 5],
        kind: [1, 1, 2, 2],        // 1=physical, 2=logical
        weight: [1.0, 2.5, 1.0, 3.0]
    }
}

// Response (after stream ends)
{
    build_id: "550e8400-...",
    received_vertices: 5,
    received_edges: 4
}
```

---

### PublishBuild

Finalize and publish a build as the current graph version.

```
rpc PublishBuild(PublishBuildRequest) returns (PublishBuildResponse)
```

**Request:**
```pseudo
PublishBuildRequest {
    build_id: string             // From BeginBuild
    artifacts: BatchArtifacts {  // Optional: pre-compute analytics
        compute_components: bool     // Compute connected components
        compute_communities: bool    // Compute communities (Leiden/Louvain)
        compute_kcore: bool          // Compute k-core decomposition
        compute_betweenness_sampled: bool  // Sampled betweenness centrality
    }
    allow_warnings: bool         // Publish even with warnings
}
```

**Response:**
```pseudo
PublishBuildResponse {
    graph: GraphRef {
        graph_name: string
        version_id: string       // New version ID
    }
    status: Status {
        code: int32              // 0 = success
        message: string
    }
}
```

---

### CreateView

Create a filtered view (subgraph) of a published graph.

```
rpc CreateView(CreateViewRequest) returns (CreateViewResponse)
```

**Request:**
```pseudo
CreateViewRequest {
    graph: GraphRef {
        graph_name: string
        version_id: string       // Optional: empty = current version
    }
    spec: ViewSpec {
        vfilter: VertexFilter {          // Filter vertices
            predicates: [Predicate]
        }
        efilter: EdgeFilter {            // Filter edges
            predicates: [Predicate]
        }
        induce_vertices_u64: [uint64]    // Include only these vertices
        induce_vertices_str: [string]
        neighborhood: NeighborhoodSpec { // Expand from seeds
            seeds_u64: [uint64]
            hops: uint32
            mode: enum { MODE_ALL, MODE_OUT, MODE_IN }
        }
        exclude_vertices_u64: [uint64]   // What-if: remove vertices
        exclude_edges_u64: [uint64]      // What-if: remove edges
    }
}

Predicate {
    column: string               // Column to filter on
    op: enum { OP_EQ, OP_IN, OP_RANGE, OP_EXISTS }
    value: oneof {
        b: bool
        u32: uint32
        u64: uint64
        f32: float
        f64: double
        s: string
        u32s: [uint32]           // For OP_IN
        u64s: [uint64]
        ss: [string]
        range_u32: {lo, hi}      // For OP_RANGE
        range_f64: {lo, hi}
    }
}
```

**Response:**
```pseudo
CreateViewResponse {
    view: ViewRef {
        view_id: string          // Handle to reference this view
    }
    vcount: uint64               // Vertex count in view
    ecount: uint64               // Edge count in view
}
```

**Example:**
```pseudo
// Request: Get physical edges in partition 5, within 2 hops of node 100
{
    graph: { graph_name: "datacenter-network" },
    spec: {
        efilter: {
            predicates: [
                { column: "kind", op: OP_EQ, u32: 1 }  // kind=1 (physical)
            ]
        },
        neighborhood: {
            seeds_u64: [100],
            hops: 2,
            mode: MODE_ALL
        }
    }
}

// Response
{
    view: { view_id: "view-abc123" },
    vcount: 45,
    ecount: 78
}
```

---

### Run

Execute an algorithm on a graph or view. Returns immediately with a job handle.

```
rpc Run(RunRequest) returns (RunResponse)
```

**Request:**
```pseudo
RunRequest {
    target: oneof {
        graph: GraphRef
        view: ViewRef
    }
    algo: AlgoSpec {
        kind: oneof {
            components: ComponentsSpec {
                mode: enum { WEAK, STRONG }
            }
            
            communities: CommunitiesSpec {
                method: enum { LEIDEN, LOUVAIN }
                resolution: double       // Optional
            }
            
            shortest_path: ShortestPathSpec {
                source_u64: uint64
                target_u64: uint64
                weight_column: string    // Empty = unweighted
                return_edges: bool
                return_vertices: bool
            }
            
            k_shortest_paths: KShortestPathsSpec {
                source_u64: uint64
                target_u64: uint64
                k: uint32
                weight_column: string
                max_candidates: uint32   // Guardrail
                max_corridor_edges: uint32
                per_path_timeout: Duration
            }
            
            distances: DistancesSpec {
                sources_u64: [uint64]
                targets_u64: [uint64]
                weight_column: string
            }
            
            bfs: BFSSpec {
                source_u64: uint64
                max_depth: uint32
                mode: enum { MODE_ALL, MODE_OUT, MODE_IN }
            }
            
            neighborhood: NeighborhoodQuerySpec {
                seeds_u64: [uint64]
                hops: uint32
                mode: enum { MODE_ALL, MODE_OUT, MODE_IN }
            }
            
            st_mincut: STMinCutSpec {
                source_u64: uint64
                target_u64: uint64
                capacity_column: string  // Empty = capacity 1
            }
            
            corridor: CorridorSpec {
                source_u64: uint64
                target_u64: uint64
                method: enum {
                    SHORTEST_PATH_HULL,  // Expand around shortest path
                    KSP_HULL,            // Expand around k shortest paths
                    COMMUNITY_AWARE      // Use precomputed communities
                }
                hops: uint32             // Hull expansion
                k: uint32                // For KSP_HULL
            }
        }
    }
    timeout: Duration            // Optional: max execution time
    priority: uint32             // Optional: scheduling priority
    allow_cache: bool            // Use cached result if available
}
```

**Response:**
```pseudo
RunResponse {
    job: JobRef {
        job_id: string           // Poll this to check status
    }
}
```

**Example:**
```pseudo
// Request: Find shortest path
{
    target: { graph: { graph_name: "datacenter-network" } },
    algo: {
        shortest_path: {
            source_u64: 100,
            target_u64: 500,
            weight_column: "latency",
            return_vertices: true
        }
    },
    timeout: { seconds: 5 }
}

// Response
{
    job: { job_id: "job-xyz789" }
}
```

---

### GetJob

Poll job status.

```
rpc GetJob(GetJobRequest) returns (GetJobResponse)
```

**Request:**
```pseudo
GetJobRequest {
    job: JobRef {
        job_id: string
    }
}
```

**Response:**
```pseudo
GetJobResponse {
    state: enum {
        PENDING,
        RUNNING,
        SUCCEEDED,
        FAILED,
        CANCELED
    }
    started_at: Timestamp
    finished_at: Timestamp       // Set when complete
    result: ResultRef {          // Set if SUCCEEDED
        result_id: string
    }
    status: Status {             // Set if FAILED
        code: int32
        message: string
    }
}
```

---

### CancelJob

Cancel a running job.

```
rpc CancelJob(CancelJobRequest) returns (CancelJobResponse)
```

**Request:**
```pseudo
CancelJobRequest {
    job: JobRef { job_id: string }
}
```

**Response:**
```pseudo
CancelJobResponse {
    canceled: bool
}
```

---

### GetResult (Streaming)

Stream result data back to client.

```
rpc GetResult(GetResultRequest) returns (stream ResultChunk)
```

**Request:**
```pseudo
GetResultRequest {
    result: ResultRef {
        result_id: string
    }
}
```

**Response (streamed):**
```pseudo
ResultChunk {
    payload: oneof {
        header: ResultHeader {
            result_id: string
            type: string             // "components", "shortest_path", etc.
            vcount: uint64
            ecount: uint64
            meta: map<string, string>
        }
        
        // Flat buffers for large results
        u32: U32Buffer { name: string, values: [uint32] }
        u64: U64Buffer { name: string, values: [uint64] }
        f64: F64Buffer { name: string, values: [double] }
        bytes: BytesBuffer { name: string, data: bytes }
        
        // Structured results for small data
        shortest_path: ShortestPathResult {
            vertices_u64: [uint64]
            edges_u64: [uint64]
            total_cost: double
        }
        
        st_mincut: STMinCutResult {
            cut_value: double
            source_side_vertices_u64: [uint64]
            cut_edges_u64: [uint64]
        }
        
        corridor: CorridorResult {
            view: ViewRef            // Handle to corridor view
            meta: map<string, string>
        }
        
        done: bool                   // Terminal marker
    }
}
```

**Example (Components result):**
```pseudo
// Chunk 1: Header
{ header: { result_id: "res-123", type: "components", vcount: 1000 } }

// Chunk 2: Membership array (vertex i belongs to component membership[i])
{ u32: { name: "membership", values: [0, 0, 1, 1, 1, 2, 2, ...] } }

// Chunk 3: Done
{ done: true }
```

**Example (Shortest Path result):**
```pseudo
// Single structured message
{
    shortest_path: {
        vertices_u64: [100, 150, 200, 350, 500],
        total_cost: 12.5
    }
}
{ done: true }
```

## Algorithm Output Reference

This section documents the expected output format for each algorithm.

### Components

Computes connected components (weak or strong) of the graph.

**Input (`ComponentsSpec`):**
```pseudo
{
    mode: WEAK | STRONG    // WEAK ignores edge direction, STRONG respects it
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `membership` | `U32Buffer` | Array where `membership[i]` = component ID for vertex at index `i` |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `mode` | `"weak"` or `"strong"` | Which mode was used |
| `num_components` | `"N"` | Total number of components found |
| `source` | `"igraph"` | Algorithm implementation used |

**Example:**
```pseudo
// Graph with 6 vertices in 3 components: {1,2,3}, {4,5}, {6}
// Header
{ header: { type: "components", vcount: 6, meta: { "num_components": "3", "mode": "weak" } } }
// Membership: vertices 0,1,2 -> component 0; vertices 3,4 -> component 1; vertex 5 -> component 2
{ u32: { name: "membership", values: [0, 0, 0, 1, 1, 2] } }
{ done: true }
```

---

### Communities

Detects community structure using Leiden or Louvain algorithm.

**Input (`CommunitiesSpec`):**
```pseudo
{
    method: LEIDEN | LOUVAIN   // Algorithm choice (Leiden recommended)
    resolution: double          // Higher = more smaller communities (default: 1.0)
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `membership` | `U32Buffer` | Array where `membership[i]` = community ID for vertex at index `i` |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `algorithm` | `"leiden"` or `"louvain"` | Algorithm used |
| `resolution` | `"1.0000"` | Resolution parameter |
| `num_communities` | `"N"` | Total communities found |
| `modularity` | `"0.4567"` | Modularity score (quality measure, higher is better) |
| `source` | `"igraph"` | Implementation used |

**Example:**
```pseudo
// Graph with 8 vertices, 2 communities detected
{ header: { type: "communities", vcount: 8, meta: { 
    "algorithm": "leiden", 
    "num_communities": "2", 
    "modularity": "0.4532",
    "resolution": "1.0000"
} } }
{ u32: { name: "membership", values: [0, 0, 0, 0, 1, 1, 1, 1] } }
{ done: true }
```

---

### Shortest Path

Finds the shortest path between two vertices.

**Input (`ShortestPathSpec`):**
```pseudo
{
    source_u64: uint64        // Source vertex ID
    target_u64: uint64        // Target vertex ID
    weight_column: string     // Edge weight column (empty = unweighted/hop count)
    return_edges: bool        // Include edge IDs in result
    return_vertices: bool     // Include vertex IDs in result
}
```

**Output (`ShortestPathResult`):**

| Field | Type | Description |
|-------|------|-------------|
| `vertices_u64` | `[uint64]` | Ordered list of vertex IDs in the path (source to target) |
| `edges_u64` | `[uint64]` | Ordered list of edge IDs in the path (if `return_edges=true`) |
| `total_cost` | `double` | Total path weight (or hop count if unweighted) |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `found` | `"true"` or `"false"` | Whether a path was found |

**Example:**
```pseudo
// Path from vertex 100 to 500: 100 -> 150 -> 200 -> 350 -> 500
{ header: { type: "shortest_path", meta: { "found": "true" } } }
{ shortest_path: { 
    vertices_u64: [100, 150, 200, 350, 500], 
    edges_u64: [1001, 1002, 1003, 1004],
    total_cost: 12.5 
} }
{ done: true }
```

**No Path Found:**
```pseudo
{ header: { type: "shortest_path", meta: { "found": "false" } } }
{ shortest_path: { vertices_u64: [], total_cost: 0.0 } }
{ done: true }
```

---

### K-Shortest Paths

Finds the k shortest paths between two vertices.

**Input (`KShortestPathsSpec`):**
```pseudo
{
    source_u64: uint64
    target_u64: uint64
    k: uint32                   // Number of paths to find
    weight_column: string       // Edge weight column
    max_candidates: uint32      // Guardrail: max candidates to explore
    max_corridor_edges: uint32  // Guardrail: max edges in search space
    per_path_timeout: Duration  // Timeout per path
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `paths` | `[U64Buffer]` | Array of paths, each containing vertex IDs |
| `costs` | `F64Buffer` | Cost of each path |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `k` | `"5"` | Requested number of paths |
| `paths_found` | `"3"` | Actual paths found (may be < k) |
| `candidates_explored` | `"150"` | Number of candidate paths explored |

**Example:**
```pseudo
// Finding 3 shortest paths from 1 to 10
{ header: { type: "ksp", meta: { "k": "3", "paths_found": "3", "candidates_explored": "45" } } }
// Path 1: 1 -> 3 -> 7 -> 10 (cost 5.0)
{ u64: { name: "path_0", values: [1, 3, 7, 10] } }
{ f64: { name: "cost_0", values: [5.0] } }
// Path 2: 1 -> 4 -> 7 -> 10 (cost 6.2)
{ u64: { name: "path_1", values: [1, 4, 7, 10] } }
{ f64: { name: "cost_1", values: [6.2] } }
// Path 3: 1 -> 3 -> 8 -> 10 (cost 7.1)
{ u64: { name: "path_2", values: [1, 3, 8, 10] } }
{ f64: { name: "cost_2", values: [7.1] } }
{ done: true }
```

---

### Distances

Computes pairwise distances between sets of source and target vertices.

**Input (`DistancesSpec`):**
```pseudo
{
    sources_u64: [uint64]     // Source vertex IDs
    targets_u64: [uint64]     // Target vertex IDs
    weight_column: string     // Edge weight column (empty = hop count)
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `distances` | `F64Buffer` | Flattened matrix: `distances[i * num_targets + j]` = distance from `sources[i]` to `targets[j]` |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `num_sources` | `"3"` | Number of source vertices |
| `num_targets` | `"4"` | Number of target vertices |

**Example:**
```pseudo
// Distances from sources [1, 2, 3] to targets [10, 20, 30, 40]
{ header: { type: "distances", meta: { "num_sources": "3", "num_targets": "4" } } }
// Matrix (3x4): 
//   1->10=2.0, 1->20=3.5, 1->30=4.0, 1->40=INF
//   2->10=1.5, 2->20=2.0, 2->30=3.0, 2->40=5.0
//   3->10=3.0, 3->20=1.0, 3->30=2.5, 3->40=3.5
{ f64: { name: "distances", values: [2.0, 3.5, 4.0, INF, 1.5, 2.0, 3.0, 5.0, 3.0, 1.0, 2.5, 3.5] } }
{ done: true }
```

Note: `INF` (infinity) indicates no path exists.

---

### BFS (Breadth-First Search)

Performs breadth-first traversal from a source vertex.

**Input (`BFSSpec`):**
```pseudo
{
    source_u64: uint64        // Starting vertex
    max_depth: uint32         // Maximum traversal depth (0 = unlimited)
    mode: MODE_ALL | MODE_OUT | MODE_IN   // Edge direction to follow
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `vertices` | `U64Buffer` | Visited vertex IDs in BFS order |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `source` | `"igraph"` | Implementation used |
| `mode` | `"all"`, `"out"`, or `"in"` | Traversal mode |
| `num_visited` | `"25"` | Total vertices visited |
| `max_depth_reached` | `"3"` | Deepest level reached |

**Example:**
```pseudo
// BFS from vertex 1 with max_depth=2
{ header: { type: "bfs", meta: { "mode": "all", "num_visited": "7", "max_depth_reached": "2" } } }
// Visited in BFS order: source (depth 0), then depth 1, then depth 2
{ u64: { name: "vertices", values: [1, 2, 3, 4, 5, 6, 7] } }
{ done: true }
```

---

### Neighborhood

Finds all vertices within N hops of seed vertices.

**Input (`NeighborhoodQuerySpec`):**
```pseudo
{
    seeds_u64: [uint64]       // Starting vertices
    hops: uint32              // Number of hops to expand
    mode: MODE_ALL | MODE_OUT | MODE_IN   // Edge direction to follow
}
```

**Output:**

| Field | Type | Description |
|-------|------|-------------|
| `vertices` | `U64Buffer` | All vertex IDs in the neighborhood (including seeds) |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `source` | `"igraph"` | Implementation used |
| `mode` | `"all"`, `"out"`, or `"in"` | Traversal mode |
| `hops` | `"2"` | Hops parameter |
| `num_vertices` | `"15"` | Total vertices in neighborhood |

**Example:**
```pseudo
// 2-hop neighborhood from seeds [1, 10]
{ header: { type: "neighborhood", meta: { "mode": "all", "hops": "2", "num_vertices": "12" } } }
{ u64: { name: "vertices", values: [1, 10, 2, 3, 11, 12, 4, 5, 13, 14, 15, 16] } }
{ done: true }
```

---

### ST-MinCut

Computes the minimum s-t cut (minimum edges to remove to disconnect source from target).

**Input (`STMinCutSpec`):**
```pseudo
{
    source_u64: uint64        // Source vertex
    target_u64: uint64        // Target vertex
    capacity_column: string   // Edge capacity column (empty = capacity 1)
}
```

**Output (`STMinCutResult`):**

| Field | Type | Description |
|-------|------|-------------|
| `cut_value` | `double` | Total capacity of cut edges |
| `source_side_vertices_u64` | `[uint64]` | Vertices on source side of cut |
| `cut_edges_u64` | `[uint64]` | Edge IDs that form the cut |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `source` | `"100"` | Source vertex ID |
| `target` | `"500"` | Target vertex ID |
| `weighted` | `"true"` or `"false"` | Whether capacities were used |

**Example:**
```pseudo
// Min-cut between vertices 100 and 500
{ header: { type: "st_mincut", meta: { "source": "100", "target": "500", "weighted": "false" } } }
{ st_mincut: { 
    cut_value: 2.0,                           // 2 edges cut
    source_side_vertices_u64: [100, 101, 102, 103],
    cut_edges_u64: [5001, 5002]               // The two edges forming the cut
} }
{ done: true }
```

---

### Corridor

Creates a "corridor" view containing the relevant subgraph between source and target.

**Input (`CorridorSpec`):**
```pseudo
{
    source_u64: uint64
    target_u64: uint64
    method: SHORTEST_PATH_HULL | KSP_HULL | COMMUNITY_AWARE
    hops: uint32              // Hull expansion radius
    k: uint32                 // For KSP_HULL: number of paths
}
```

**Output (`CorridorResult`):**

| Field | Type | Description |
|-------|------|-------------|
| `view` | `ViewRef` | Handle to the corridor view (use for subsequent queries) |
| `meta` | `map<string,string>` | Corridor metadata |

**Metadata:**

| Key | Value | Description |
|-----|-------|-------------|
| `view_id` | `"view-corridor-xyz"` | ID of created view |
| `method` | `"SHORTEST_PATH_HULL"` | Method used |
| `vcount` | `"45"` | Vertices in corridor |
| `ecount` | `"78"` | Edges in corridor |
| `path_found` | `"true"` | Whether a path was found |
| `path_length` | `"5"` | Length of shortest path (if found) |

**Example:**
```pseudo
// Corridor from 100 to 500 using shortest path hull with 1-hop expansion
{ header: { type: "corridor", meta: { 
    "method": "SHORTEST_PATH_HULL", 
    "vcount": "45", 
    "ecount": "78",
    "path_found": "true"
} } }
{ corridor: { 
    view: { view_id: "view-corridor-abc123" }, 
    meta: { "path_length": "5" }
} }
{ done: true }
```

---

### K-Core

Computes k-core decomposition (coreness value for each vertex).

**Input (`KCoreSpec`):**
```pseudo
{
    k: uint32    // 0 = full decomposition, >0 = filter to coreness >= k
}
```

**Output (`KCoreResult`):**

| Field | Type | Description |
|-------|------|-------------|
| `coreness` | `[uint32]` | Coreness value for each vertex (indexed by vertex index) |
| `max_core` | `uint32` | Highest coreness value found |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `max_core` | `"4"` | Maximum coreness value |
| `k` | `"0"` | Requested k parameter |

**Interpretation:**
- Coreness `k` means the vertex belongs to the k-core but not the (k+1)-core
- A k-core is a maximal subgraph where all vertices have degree >= k within that subgraph
- Higher coreness = more central/densely connected

**Example:**
```pseudo
// K-core decomposition of graph with 8 vertices
{ header: { type: "kcore", meta: { "max_core": "3", "k": "0" } } }
{ kcore: { 
    coreness: [1, 2, 2, 3, 3, 3, 2, 1],  // Per-vertex coreness
    max_core: 3                           // Highest coreness found
} }
{ done: true }
```

---

### Betweenness

Computes betweenness centrality for all vertices.

**Input (`BetweennessSpec`):**
```pseudo
{
    sample_size: uint32       // 0 = exact (all vertices), >0 = sample N sources
    normalized: bool          // Normalize scores to [0, 1]
    weight_column: string     // Edge weight column (empty = unweighted)
}
```

**Output (`BetweennessResult`):**

| Field | Type | Description |
|-------|------|-------------|
| `scores` | `[double]` | Betweenness score for each vertex (indexed by vertex index) |

**Metadata (`ResultHeader.meta`):**

| Key | Value | Description |
|-----|-------|-------------|
| `sample_size` | `"0"` | Sample size used (0 = exact) |
| `normalized` | `"true"` or `"false"` | Whether scores are normalized |
| `max_score` | `"156.5"` | Highest betweenness score |
| `min_score` | `"0.0"` | Lowest betweenness score |

**Interpretation:**
- Betweenness centrality measures how often a vertex lies on shortest paths between other vertices
- Higher score = more "bridge" or "bottleneck" role in the network
- Normalized scores range from 0 to 1

**Example:**
```pseudo
// Betweenness centrality for 6 vertices
{ header: { type: "betweenness", meta: { 
    "sample_size": "0", 
    "normalized": "false",
    "max_score": "8.0",
    "min_score": "0.0"
} } }
{ betweenness: { 
    scores: [0.0, 4.0, 8.0, 8.0, 4.0, 0.0]  // Vertex 2,3 are most central
} }
{ done: true }
```

---

## Result Streaming Summary

| Algorithm | Structured Message | Buffer Format |
|-----------|-------------------|---------------|
| Components | - | `U32Buffer("membership")` |
| Communities | - | `U32Buffer("membership")` |
| Shortest Path | `ShortestPathResult` | - |
| K-Shortest Paths | - | Multiple `U64Buffer`/`F64Buffer` per path |
| Distances | - | `F64Buffer("distances")` (flattened matrix) |
| BFS | - | `U64Buffer("vertices")` |
| Neighborhood | - | `U64Buffer("vertices")` |
| ST-MinCut | `STMinCutResult` | - |
| Corridor | `CorridorResult` | - |
| K-Core | `KCoreResult` | - |
| Betweenness | `BetweennessResult` | - |

**Notes:**
- All results start with a `ResultHeader` containing metadata
- All results end with `{ done: true }`
- Structured messages are used for small, well-defined results
- Buffers are used for large arrays (membership, scores, etc.)
- Vertex indices in arrays correspond to the internal vertex index order

---

### Release

Release server-side resources early (views, results).

```
rpc Release(ReleaseRequest) returns (ReleaseResponse)
```

**Request:**
```pseudo
ReleaseRequest {
    target: oneof {
        view: ViewRef { view_id: string }
        result: ResultRef { result_id: string }
    }
}
```

**Response:**
```pseudo
ReleaseResponse {
    released: bool
}
```

---

## GraphEngineOps Service

### Health

Basic health check.

```
rpc Health(HealthRequest) returns (HealthResponse)
```

**Request:**
```pseudo
HealthRequest { }
```

**Response:**
```pseudo
HealthResponse {
    status: string               // "SERVING", "NOT_SERVING"
    meta: map<string, string> {
        "version": "1.0.0",
        "uptime": "2h30m",
        "go_version": "go1.23",
        "pending_builds": "2"
    }
}
```

---

### ListGraphs

List all published graphs.

```
rpc ListGraphs(ListGraphsRequest) returns (ListGraphsResponse)
```

**Request:**
```pseudo
ListGraphsRequest { }
```

**Response:**
```pseudo
ListGraphsResponse {
    graphs: [GraphSummary {
        graph_name: string
        current_version_id: string
        vcount: uint64
        ecount: uint64
        published_at: Timestamp
    }]
}
```

---

### DescribeGraph

Get detailed graph information.

```
rpc DescribeGraph(DescribeGraphRequest) returns (DescribeGraphResponse)
```

**Request:**
```pseudo
DescribeGraphRequest {
    graph: GraphRef {
        graph_name: string
        version_id: string       // Optional
    }
}
```

**Response:**
```pseudo
DescribeGraphResponse {
    summary: GraphSummary
    schema: Schema {
        vertex_columns: [ColumnDef]
        edge_columns: [ColumnDef]
    }
    labels: map<string, string>
}
```

---

### CacheStats

Get cache statistics.

```
rpc CacheStats(CacheStatsRequest) returns (CacheStatsResponse)
```

**Request:**
```pseudo
CacheStatsRequest { }
```

**Response:**
```pseudo
CacheStatsResponse {
    results_items: uint64        // Cached results count
    results_bytes: uint64        // Memory used by results
    views_items: uint64          // Cached views count
    views_bytes: uint64          // Memory used by views
}
```

---

### ValidateGraph

Validate graph integrity.

```
rpc ValidateGraph(ValidateGraphRequest) returns (ValidateGraphResponse)
```

**Request:**
```pseudo
ValidateGraphRequest {
    graph: GraphRef
    deep: bool                   // true for thorough validation
}
```

**Response:**
```pseudo
ValidateGraphResponse {
    status: Status {
        code: int32              // 0 = valid
        message: string
    }
    warnings: [string]           // Non-fatal issues
    metrics: map<string, string> {
        "isolated_vertices": "15",
        "self_loops": "0",
        "max_degree": "342"
    }
}
```

---

### TraceJob

Get execution trace for debugging.

```
rpc TraceJob(TraceJobRequest) returns (TraceJobResponse)
```

**Request:**
```pseudo
TraceJobRequest {
    job: JobRef { job_id: string }
}
```

**Response:**
```pseudo
TraceJobResponse {
    spans: [TraceSpan {
        name: string             // "build_view", "run_dijkstra", etc.
        duration: Duration
        tags: map<string, string>
    }]
}
```

---

### ExportSubgraph (Streaming)

Export subgraph data for debugging.

```
rpc ExportSubgraph(ExportSubgraphRequest) returns (stream ExportChunk)
```

**Request:**
```pseudo
ExportSubgraphRequest {
    target: oneof {
        graph: GraphRef
        view: ViewRef
    }
    format: enum {
        EDGE_LIST,               // "src dst" per line
        CSV                      // CSV with headers
    }
}
```

**Response (streamed):**
```pseudo
ExportChunk {
    data: bytes                  // Chunk of export data
}
```

---

## Common Types Reference

```pseudo
GraphRef {
    graph_name: string
    version_id: string           // Empty = current version
}

ViewRef {
    view_id: string
}

ResultRef {
    result_id: string
}

JobRef {
    job_id: string
}

Status {
    code: int32                  // 0 = OK
    message: string
}

Duration {
    seconds: int64
    nanos: int32
}

Timestamp {
    seconds: int64
    nanos: int32
}
```

---

## Error Codes

| Code | Meaning |
|------|---------|
| 0 | OK |
| 3 | INVALID_ARGUMENT |
| 5 | NOT_FOUND |
| 8 | RESOURCE_EXHAUSTED |
| 12 | UNIMPLEMENTED |
| 13 | INTERNAL |
| 14 | UNAVAILABLE |

---

## Typical Workflow

```pseudo
// 1. Build and publish a graph
build_id = BeginBuild({ graph_name: "my-graph", directed: true })

Upload([
    { vertices: { node_id_u64: [1,2,3,4,5] } },
    { edges: { src_u64: [1,1,2], dst_u64: [2,3,4] } }
])

graph_ref = PublishBuild({ build_id, artifacts: { compute_components: true } })

// 2. Create a view (optional filtering)
view = CreateView({
    graph: graph_ref,
    spec: { efilter: { predicates: [{ column: "kind", op: OP_EQ, u32: 1 }] } }
})

// 3. Run algorithm
job = Run({
    target: { view: view },
    algo: { shortest_path: { source_u64: 1, target_u64: 5 } }
})

// 4. Poll for completion
while GetJob(job).state == RUNNING:
    sleep(100ms)

// 5. Get results
result = GetJob(job).result
for chunk in GetResult(result):
    process(chunk)

// 6. Cleanup
Release({ view: view })
Release({ result: result })
```
