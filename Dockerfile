# =============================================================================
# Graph-engine Dockerfile (with igraph support)
# =============================================================================
# This Dockerfile builds graph-engine with CGO enabled and igraph linked.
# For a pure-Go build without igraph, use Dockerfile.nocgo instead.
#
# Build:
#   docker build -t graph-engine .
#
# Build with version info:
#   docker build --build-arg VERSION=v1.0.0 --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) -t graph-engine .
# =============================================================================

# Build stage
FROM golang:1.23-bookworm AS builder

WORKDIR /app

# Install build dependencies including igraph
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    make \
    pkg-config \
    libigraph-dev \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Verify igraph installation
RUN pkg-config --modversion igraph && \
    echo "igraph CFLAGS: $(pkg-config --cflags igraph)" && \
    echo "igraph LDFLAGS: $(pkg-config --libs igraph)"

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled
ARG VERSION=dev
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=1 \
    CGO_CFLAGS="$(pkg-config --cflags igraph)" \
    CGO_LDFLAGS="$(pkg-config --libs igraph)" \
    go build \
    -ldflags="-s -w -X 'github.com/naisa-ai/graph-engine/internal/api/grpc.Version=${VERSION}' -X 'github.com/naisa-ai/graph-engine/internal/api/grpc.BuildTime=${BUILD_TIME}'" \
    -tags cgo \
    -o /app/graph-engined \
    ./cmd/graph-engined

# Verify the binary was built with igraph support
RUN ldd /app/graph-engined | grep -q igraph && echo "igraph linked successfully" || echo "Warning: igraph not linked"

# Runtime stage
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies (igraph shared library)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    libigraph3 \
    && rm -rf /var/lib/apt/lists/* \
    && rm -rf /tmp/* /var/tmp/*

# Create non-root user
RUN useradd -r -s /bin/false appuser

# Copy binary from builder
COPY --from=builder /app/graph-engined /app/graph-engined

# Copy default config
COPY config.yaml /app/config.yaml

# Set ownership
RUN chown -R appuser:appuser /app

# Switch to non-root user
USER appuser

# Expose gRPC port
EXPOSE 50051

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/app/graph-engined", "--help"] || exit 1

# Labels
LABEL org.opencontainers.image.title="graph-engine"
LABEL org.opencontainers.image.description="Graph Engine Service with igraph support"
LABEL org.opencontainers.image.source="https://github.com/naisa-ai/graph-engine"

# Run the service
ENTRYPOINT ["/app/graph-engined"]
CMD ["-config", "/app/config.yaml"]
