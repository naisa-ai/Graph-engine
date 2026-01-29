# Graph-engine Python Client

A Python client library for the Graph-engine service, providing both synchronous and asynchronous interfaces.

## Installation

```bash
pip install graphengine-client
```

With numpy support:
```bash
pip install graphengine-client[numpy]
```

## Quick Start

### Synchronous Client

```python
from graphengine import GraphEngineClient, GraphBuilder, GraphRef

# Connect to server
with GraphEngineClient("localhost:50051") as client:
    # Build and publish a graph
    graph = (GraphBuilder(client, "my-network")
        .directed(True)
        .add_vertices([1, 2, 3, 4, 5])
        .add_edges([1, 2, 3, 4], [2, 3, 4, 5])
        .publish())
    
    print(f"Published graph: {graph.version_id}")
    
    # Find shortest path
    path = client.shortest_path(graph, source=1, target=5)
    print(f"Path: {path.vertices} (cost: {path.total_cost})")
```

### Asynchronous Client

```python
import asyncio
from graphengine import AsyncGraphEngineClient, GraphRef

async def main():
    async with AsyncGraphEngineClient("localhost:50051") as client:
        graphs = await client.list_graphs()
        for g in graphs:
            print(f"{g.graph_name}: {g.vcount} vertices")

asyncio.run(main())
```

## Features

### Connection Options

```python
# Basic connection
client = GraphEngineClient("localhost:50051")

# With TLS
client = GraphEngineClient("graph-engine.example.com:443", secure=True)

# With custom credentials
import grpc
creds = grpc.ssl_channel_credentials()
client = GraphEngineClient("graph-engine.example.com:443", credentials=creds)

# With metadata and retry
client = GraphEngineClient(
    "localhost:50051",
    metadata={"x-tenant-id": "tenant-123"},
    max_retries=3,
    retry_backoff=0.1,
)
```

### Graph Building

```python
from graphengine import GraphBuilder

# Simple graph
graph = (GraphBuilder(client, "network")
    .add_vertices(node_ids)
    .add_edges(sources, destinations)
    .publish())

# Weighted graph with labels
graph = (GraphBuilder(client, "weighted-network")
    .directed(True)
    .add_weighted_edges(sources, destinations, weights)
    .with_labels({"env": "prod", "version": "1.0"})
    .publish())

# Using numpy arrays
import numpy as np
sources = np.array([1, 2, 3, 4])
destinations = np.array([2, 3, 4, 5])
weights = np.array([1.0, 2.0, 3.0, 4.0])

graph = (GraphBuilder(client, "numpy-graph")
    .add_vertices_numpy(np.arange(1, 6))
    .add_edges_numpy(sources, destinations, weights=weights)
    .publish())
```

### Views

```python
from graphengine import ViewBuilder

# Induced subgraph
view, vcount, ecount = (ViewBuilder(client, graph)
    .induce_vertices([1, 2, 3, 4, 5])
    .create())

# Neighborhood expansion
view, vcount, ecount = (ViewBuilder(client, graph)
    .neighborhood(seeds=[100], hops=2, mode="all")
    .create())

# Exclude vertices
view, vcount, ecount = (ViewBuilder(client, graph)
    .exclude_vertices([999])
    .create())
```

### Algorithms

```python
# Shortest path
path = client.shortest_path(graph, source=1, target=100)
print(f"Path: {path.vertices}")
print(f"Cost: {path.total_cost}")

# K shortest paths
paths = client.k_shortest_paths(graph, source=1, target=100, k=5)
for i, p in enumerate(paths):
    print(f"Path {i+1}: {p.vertices}")

# Connected components
result = client.components(graph)
print(f"Found {result.num_components} components")
print(f"Membership: {result.membership[:10]}...")

# Distance matrix
dm = client.distances(graph, sources=[1, 2], targets=[3, 4, 5])
print(dm.distances)  # 2x3 matrix
# With numpy
arr = dm.to_numpy()

# Minimum cut
cut = client.min_cut(graph, source=1, target=100)
print(f"Cut value: {cut.cut_value}")
print(f"Cut edges: {cut.cut_edges}")

# Corridor view
corridor = client.corridor(
    graph, 
    source=1, 
    target=100,
    method="ksp_hull",
    hops=2,
    k=3,
)
print(f"Corridor view: {corridor.view_id}")

# BFS
vertices = client.bfs(graph, source=1, max_depth=3)
print(f"Reachable: {vertices}")

# Neighborhood
vertices = client.neighborhood(graph, seeds=[1, 2], hops=2)
print(f"Neighborhood: {vertices}")
```

### Job Management

```python
from graphengine import JobRef

# Run algorithm asynchronously
job = client.run_async(request)

# Wait with callback
def on_progress(state):
    print(f"Job state: {state}")

result = client.wait_for_job(job, poll_interval=0.5, callback=on_progress)

# Cancel job
canceled = client.cancel_job(job)
```

### Ops / Troubleshooting

```python
# Health check
health = client.health()
print(f"Status: {health.status}")
print(f"Version: {health.version}")
print(f"Uptime: {health.uptime}")

# List graphs
graphs = client.list_graphs()
for g in graphs:
    print(f"{g.graph_name}: {g.vcount} vertices, {g.ecount} edges")

# Describe graph
details = client.describe_graph(graph)
print(f"Labels: {details.labels}")

# Validate graph
validation = client.validate_graph(graph, deep=True)
if not validation.valid:
    print(f"Validation failed: {validation.message}")
for warning in validation.warnings:
    print(f"Warning: {warning}")
for key, value in validation.metrics.items():
    print(f"  {key}: {value}")

# Cache stats
stats = client.cache_stats()
print(f"Results: {stats.results_items} items, {stats.results_bytes} bytes")

# Trace job
spans = client.trace_job(job)
for span in spans:
    print(f"  {span.name}: {span.duration}")

# Export graph
from graphengine import ExportFormat

data = client.export_graph_to_bytes(graph, format=ExportFormat.CSV)
print(data.decode("utf-8"))

# Streaming export
for chunk in client.export_graph(graph, format=ExportFormat.EDGE_LIST):
    process(chunk)
```

### Error Handling

```python
from graphengine import (
    GraphEngineError,
    NotFoundError,
    TimeoutError,
    ResourceExhaustedError,
)
from graphengine.exceptions import is_retryable

try:
    path = client.shortest_path(graph, 1, 100)
except NotFoundError:
    print("Graph or vertices not found")
except TimeoutError:
    print("Operation timed out")
except ResourceExhaustedError:
    print("Quota exceeded, try again later")
except GraphEngineError as e:
    if is_retryable(e):
        print("Transient error, can retry")
    else:
        print(f"Error: {e}")
```

## API Reference

### Client Options

| Option | Description | Default |
|--------|-------------|---------|
| `address` | Server address (host:port) | Required |
| `timeout` | Default timeout in seconds | 30.0 |
| `secure` | Use TLS | False |
| `credentials` | Custom gRPC credentials | None |
| `metadata` | Default metadata for RPCs | {} |
| `max_retries` | Max retry attempts | 3 |
| `retry_backoff` | Initial retry backoff (seconds) | 0.1 |

### Algorithm Methods

| Method | Description |
|--------|-------------|
| `shortest_path` | Single-pair shortest path |
| `k_shortest_paths` | Top-k shortest paths |
| `components` | Connected components |
| `distances` | Distance matrix |
| `min_cut` | s-t minimum cut |
| `corridor` | Create corridor view |
| `bfs` | Breadth-first search |
| `neighborhood` | Multi-hop neighborhood |

### Ops Methods

| Method | Description |
|--------|-------------|
| `health` | Service health check |
| `list_graphs` | List all graphs |
| `describe_graph` | Graph details |
| `validate_graph` | Validate integrity |
| `cache_stats` | Cache statistics |
| `trace_job` | Job trace spans |
| `export_graph` | Export graph data |

## Development

```bash
# Install dev dependencies
pip install -e ".[dev]"

# Run tests
pytest

# Type checking
mypy graphengine

# Linting
ruff check graphengine
```

## License

Apache 2.0
