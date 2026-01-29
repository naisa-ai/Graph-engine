# Graph-Engine

A high-performance, stateful graph analytics service built on [igraph](https://igraph.org/), exposed via gRPC. Designed for network topology analysis, path finding, and graph algorithms at scale.

## Overview

Graph-Engine provides a network service architecture that:

- **Avoids GPL pollution in clients** - Clients communicate via gRPC and don't link against igraph
- **Supports stateful graph management** - Multiple graphs loaded and versioned across calls
- **Enables concurrent access** - Many clients can query the same graph version simultaneously
- **Scales to production workloads** - Target: ~200K nodes, ~1M edges per graph

## Documentation

| Document | Description |
|----------|-------------|
| [Design Document](graph-engine-design.md) | Architecture, data structures, and implementation details |
| [API Reference](API_README.md) | Complete gRPC API documentation |
| [Go Client](clients/go/README.md) | Go client library usage |
| [Python Client](clients/py/README.md) | Python client library usage |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     Client Applications                      │
│  ┌─────────────────┐              ┌─────────────────┐       │
│  │   Go Client     │              │  Python Client  │       │
│  │  (MIT License)  │              │  (MIT License)  │       │
│  └────────┬────────┘              └────────┬────────┘       │
└───────────┼────────────────────────────────┼────────────────┘
            │         gRPC (network)         │
            ▼                                ▼
┌─────────────────────────────────────────────────────────────┐
│                   Graph-Engine Server                        │
│                      (GPL License)                           │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  gRPC Handlers → Service Layer → igraph C Shim       │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

## Features

### Graph Operations
- **Build & Publish** - Stream vertices and edges, publish immutable versions
- **Views** - Create filtered subgraphs with predicates, neighborhoods, or induced vertex sets
- **Versioning** - Atomic publish with readers never blocking on updates

### Algorithms
- Shortest Path (Dijkstra)
- K-Shortest Paths (Yen's algorithm)
- Connected Components (weak/strong)
- Distance Matrix
- BFS / Neighborhood Expansion
- Minimum s-t Cut
- Corridor Analysis
- K-Core Decomposition
- Betweenness Centrality (sampled)

### Operational APIs
- Health checks and metrics (Prometheus)
- Graph validation and introspection
- Job tracing for debugging
- Subgraph export (CSV, edge list)

## Quick Start

### Running the Server

```bash
# Build and run via Docker (recommended)
make docker
make run

# Or build locally (requires igraph installed)
make build-local
./build/graph-engined --config config.yaml
```

The server listens on:
- **gRPC**: `localhost:50051`
- **HTTP** (metrics/health): `localhost:8080`

### Go Client Example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/naisa-ai/graph-engine/clients/go/graphengine"
)

func main() {
    client, err := graphengine.NewClient("localhost:50051", graphengine.WithInsecure())
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Build and publish a graph
    graph, _ := graphengine.NewGraphBuilder(client, "my-network").
        Directed(true).
        AddVertices([]uint64{1, 2, 3, 4, 5}).
        AddEdges([]uint64{1, 2, 3, 4}, []uint64{2, 3, 4, 5}).
        Publish(ctx)

    // Find shortest path
    path, _ := client.ShortestPath(ctx, graph, 1, 5)
    fmt.Printf("Path: %v (cost: %.2f)\n", path.Vertices, path.TotalCost)
}
```

### Python Client Example

```python
from graphengine import GraphEngineClient, GraphBuilder

with GraphEngineClient("localhost:50051") as client:
    # Build and publish a graph
    graph = (GraphBuilder(client, "my-network")
        .directed(True)
        .add_vertices([1, 2, 3, 4, 5])
        .add_edges([1, 2, 3, 4], [2, 3, 4, 5])
        .publish())

    # Find shortest path
    path = client.shortest_path(graph, source=1, target=5)
    print(f"Path: {path.vertices} (cost: {path.total_cost})")
```

## Development

### Prerequisites

- Go 1.24+
- Docker (for containerized builds)
- Protocol Buffers compiler (`protoc`)

### Building

```bash
# Generate protobuf code
make proto

# Build server in Docker (no local igraph needed)
make docker

# Build server locally (requires igraph)
make build-local

# Run all tests
make test

# Run E2E tests only
make e2e
```

### Project Structure

```
.
├── cmd/graph-engined/     # Server entrypoint
├── internal/
│   ├── api/grpc/          # gRPC handlers
│   ├── service/           # Algorithm implementations
│   ├── shim/              # igraph C bindings (CGO)
│   └── config/            # Configuration
├── gen/                   # Generated protobuf code (server)
├── proto/                 # Proto definitions
├── clients/
│   ├── go/                # Go client library (separate module)
│   └── py/                # Python client library
└── e2e/                   # End-to-end tests
```

## Configuration

See [config.yaml](config.yaml) for all options:

```yaml
server:
  grpc_port: 50051
  http_port: 8080

limits:
  max_graphs: 10
  max_builds_pending: 5
  max_parallel_jobs: 4

igraph:
  max_parallel_calls_per_version: 1
  enable_shim: true
```

## License

This project uses a **dual-license model** due to igraph's GPL license:

| Component | License | Notes |
|-----------|---------|-------|
| Server (`cmd/`, `internal/`, `gen/`) | GPL-2.0-or-later | Links to igraph |
| Go Client (`clients/go/`) | MIT | Network-only, no igraph linking |
| Python Client (`clients/py/`) | MIT | Network-only, no igraph linking |
| Proto Definitions (`proto/`) | MIT | Interface definitions only |

**Key insight**: The client libraries communicate with the server over gRPC (network protocol) and do **not** link to igraph. This keeps clients free from GPL obligations.

See [LICENSE](LICENSE) for details.
