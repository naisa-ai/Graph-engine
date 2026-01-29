# =============================================================================
# Graph-engine Dockerfile (with igraph support)
# =============================================================================
# This Dockerfile builds graph-engine with CGO enabled and igraph linked.
# igraph is built from source with thread-safety enabled.
#
# Build:
#   docker build -t graph-engine .
#
# Build with version info:
#   docker build --build-arg VERSION=v1.0.0 --build-arg BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ) -t graph-engine .
# =============================================================================

# Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /app

# igraph version to build
ARG IGRAPH_VERSION=0.10.15

# Install build dependencies for igraph and Go
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    make \
    pkg-config \
    gcc \
    g++ \
    libc6-dev \
    cmake \
    ninja-build \
    curl \
    libxml2-dev \
    libglpk-dev \
    libblas-dev \
    liblapack-dev \
    && rm -rf /var/lib/apt/lists/*

# Build igraph from source with thread-safety enabled
# Key flags:
#   - IGRAPH_ENABLE_TLS=ON: Enable thread-local storage for thread safety
#   - IGRAPH_USE_INTERNAL_ARPACK=ON: Use bundled ARPACK (external ARPACK is not thread-safe)
#   - IGRAPH_OPENMP_SUPPORT=ON: Enable OpenMP parallelization
RUN curl -sL https://github.com/igraph/igraph/releases/download/${IGRAPH_VERSION}/igraph-${IGRAPH_VERSION}.tar.gz | tar xz \
    && cd igraph-${IGRAPH_VERSION} \
    && mkdir build && cd build \
    && cmake .. -GNinja \
        -DCMAKE_BUILD_TYPE=Release \
        -DCMAKE_INSTALL_PREFIX=/usr/local \
        -DIGRAPH_ENABLE_TLS=ON \
        -DIGRAPH_USE_INTERNAL_ARPACK=ON \
        -DIGRAPH_USE_INTERNAL_BLAS=OFF \
        -DIGRAPH_USE_INTERNAL_LAPACK=OFF \
        -DIGRAPH_USE_INTERNAL_GLPK=OFF \
        -DIGRAPH_GRAPHML_SUPPORT=ON \
        -DIGRAPH_OPENMP_SUPPORT=ON \
        -DBUILD_SHARED_LIBS=ON \
    && ninja \
    && ninja install \
    && ldconfig \
    && cd /app \
    && rm -rf igraph-${IGRAPH_VERSION}

# Verify igraph installation
RUN pkg-config --modversion igraph && \
    echo "igraph CFLAGS: $(pkg-config --cflags igraph)" && \
    echo "igraph LDFLAGS: $(pkg-config --libs igraph)"

# Set environment for pkg-config
ENV PKG_CONFIG_PATH="/usr/local/lib/pkgconfig"
ENV LD_LIBRARY_PATH="/usr/local/lib"

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Ensure go.mod is tidy
RUN go mod tidy

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

# Install runtime dependencies
# Note: We copy the igraph shared library from builder instead of using libigraph3
# to ensure we have the thread-safe version
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    libxml2 \
    libglpk40 \
    libblas3 \
    liblapack3 \
    libgomp1 \
    && rm -rf /var/lib/apt/lists/* \
    && rm -rf /tmp/* /var/tmp/*

# Copy igraph shared library from builder
COPY --from=builder /usr/local/lib/libigraph.so* /usr/local/lib/
RUN ldconfig

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
LABEL org.opencontainers.image.description="Graph Engine Service with thread-safe igraph support"
LABEL org.opencontainers.image.source="https://github.com/naisa-ai/graph-engine"

# Run the service
ENTRYPOINT ["/app/graph-engined"]
CMD ["-config", "/app/config.yaml"]
