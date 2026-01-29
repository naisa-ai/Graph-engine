# Graph-engine Go Client

A Go client library for the Graph-engine service, providing idiomatic wrappers around the gRPC APIs.

## Installation

```bash
go get github.com/naisa-ai/graph-engine/clients/go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/naisa-ai/graph-engine/clients/go/graphengine"
)

func main() {
    // Create client
    client, err := graphengine.NewClient("localhost:50051",
        graphengine.WithInsecure(),
        graphengine.WithTimeout(30*time.Second),
    )
    if err != nil {
        log.Fatal(err)
    }
    defer client.Close()

    ctx := context.Background()

    // Build and publish a graph
    graph, err := graphengine.NewGraphBuilder(client, "my-network").
        Directed(true).
        AddVertices([]uint64{1, 2, 3, 4, 5}).
        AddEdges(
            []uint64{1, 2, 3, 4},  // sources
            []uint64{2, 3, 4, 5},  // destinations
        ).
        Publish(ctx)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Published graph: %s\n", graph.VersionId)

    // Find shortest path
    path, err := client.ShortestPath(ctx, graph, 1, 5)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("Path: %v (cost: %.2f)\n", path.Vertices, path.TotalCost)
}
```

## Features

### Connection Management

```go
// Basic connection
client, _ := graphengine.NewClient("localhost:50051", graphengine.WithInsecure())

// With TLS
client, _ := graphengine.NewClient("graph-engine.example.com:443",
    graphengine.WithTLS(&tls.Config{}),
)

// With retry and metadata
client, _ := graphengine.NewClient("localhost:50051",
    graphengine.WithInsecure(),
    graphengine.WithRetry(3, 100*time.Millisecond),
    graphengine.WithMetadata(map[string]string{
        "x-tenant-id": "tenant-123",
    }),
)
```

### Graph Building

```go
// Simple graph
graph, _ := graphengine.NewGraphBuilder(client, "network").
    AddVertices(nodeIDs).
    AddEdges(sources, destinations).
    Publish(ctx)

// Weighted graph
graph, _ := graphengine.NewGraphBuilder(client, "weighted-network").
    Directed(true).
    AddWeightedEdges(sources, destinations, weights).
    WithLabels(map[string]string{"env": "prod"}).
    Publish(ctx)

// Large graph with chunking
graph, _ := graphengine.NewGraphBuilder(client, "large-network").
    ChunkSize(50000).  // Upload in 50k chunks
    AddVertices(millionNodes).
    AddEdges(millionSources, millionDests).
    Publish(ctx)
```

### Views

```go
// Create induced subgraph
viewBuilder := graphengine.NewViewBuilder(client, graph)
view, vcount, ecount, _ := viewBuilder.
    InduceVertices([]uint64{1, 2, 3, 4, 5}).
    Create(ctx)

// Neighborhood expansion
view, _, _, _ := graphengine.NewViewBuilder(client, graph).
    Neighborhood([]uint64{100}, 2).  // 2-hop neighborhood of node 100
    Create(ctx)
```

### Algorithms

```go
// Shortest path
path, _ := client.ShortestPath(ctx, graph, source, target)
fmt.Printf("Path: %v, Cost: %.2f\n", path.Vertices, path.TotalCost)

// K shortest paths
paths, _ := client.KShortestPaths(ctx, graph, source, target, 5)
for i, p := range paths {
    fmt.Printf("Path %d: %v\n", i+1, p.Vertices)
}

// Connected components
components, _ := client.Components(ctx, graph)
fmt.Printf("Found %d components\n", components.NumComponents)

// Distance matrix
dm, _ := client.Distances(ctx, graph, sources, targets)
for i, src := range dm.Sources {
    for j, tgt := range dm.Targets {
        fmt.Printf("Distance %d->%d: %.2f\n", src, tgt, dm.Distances[i][j])
    }
}

// Minimum cut
cut, _ := client.MinCut(ctx, graph, source, target)
fmt.Printf("Cut value: %.2f, Cut edges: %v\n", cut.CutValue, cut.CutEdges)

// Corridor view
corridor, _ := client.Corridor(ctx, graph, source, target,
    graphengine.WithCorridorMethod(gepb.CorridorSpec_KSP_HULL),
    graphengine.WithCorridorK(3),
    graphengine.WithCorridorHops(2),
)
```

### Async Operations

```go
// Run algorithm asynchronously
job, _ := client.RunAsync(ctx, &gepb.RunRequest{...})

// Poll with callback
result, _ := client.WaitForJobWithCallback(ctx, job, 500*time.Millisecond,
    func(state gepb.GetJobResponse_State) {
        fmt.Printf("Job state: %v\n", state)
    },
)

// Cancel if needed
canceled, _ := client.CancelJob(ctx, job)
```

### Ops / Troubleshooting

```go
// Health check
health, _ := client.Health(ctx)
fmt.Printf("Status: %s, Version: %s\n", health.Status, health.Version)

// List graphs
graphs, _ := client.ListGraphs(ctx)
for _, g := range graphs {
    fmt.Printf("Graph: %s (%d vertices, %d edges)\n", 
        g.GraphName, g.VCount, g.ECount)
}

// Validate graph
validation, _ := client.ValidateGraph(ctx, graph, true /* deep */)
if !validation.Valid {
    fmt.Printf("Validation failed: %s\n", validation.Message)
}
for k, v := range validation.Metrics {
    fmt.Printf("  %s: %s\n", k, v)
}

// Export graph
data, _ := client.ExportGraphToBytes(ctx, graph, graphengine.ExportFormatCSV)
fmt.Printf("Exported %d bytes\n", len(data))

// Trace job
spans, _ := client.TraceJob(ctx, job)
for _, span := range spans {
    fmt.Printf("  %s: %v\n", span.Name, span.Duration)
}
```

### Error Handling

```go
path, err := client.ShortestPath(ctx, graph, src, dst)
if err != nil {
    if graphengine.IsNotFound(err) {
        fmt.Println("Graph or vertices not found")
    } else if graphengine.IsTimeout(err) {
        fmt.Println("Operation timed out")
    } else if graphengine.IsRetryable(err) {
        fmt.Println("Transient error, can retry")
    } else {
        fmt.Printf("Error: %v\n", err)
    }
}
```

## API Reference

### Client Options

| Option | Description |
|--------|-------------|
| `WithTimeout(d)` | Default RPC timeout |
| `WithInsecure()` | Disable TLS |
| `WithTLS(config)` | Enable TLS with config |
| `WithRetry(n, backoff)` | Retry configuration |
| `WithMetadata(map)` | Add metadata to all RPCs |
| `WithBlock()` | Block until connected |

### Algorithm Methods

| Method | Description |
|--------|-------------|
| `ShortestPath` | Single-pair shortest path |
| `KShortestPaths` | Top-k shortest paths |
| `Components` | Connected components |
| `Distances` | Distance matrix |
| `MinCut` | s-t minimum cut |
| `Corridor` | Create corridor view |
| `BFS` | Breadth-first search |
| `Neighborhood` | Multi-hop neighborhood |

### Ops Methods

| Method | Description |
|--------|-------------|
| `Health` | Service health check |
| `ListGraphs` | List all graphs |
| `DescribeGraph` | Graph details |
| `ValidateGraph` | Validate integrity |
| `CacheStats` | Cache statistics |
| `TraceJob` | Job trace spans |
| `ExportGraph` | Export graph data |

## License

Apache 2.0
