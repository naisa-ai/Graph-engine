# Graph-engine Makefile

.PHONY: proto build build-cgo build-nocgo run test test-unit test-all test-shim clean lint help e2e e2e-grpc e2e-go e2e-py shim-deps docker docker-cgo docker-nocgo

# Binary name
BINARY_NAME := graph-engined
BUILD_DIR := build
GEN_DIR := gen

# Shim paths
SHIM_DIR := internal/shim
SHIM_SRC := $(SHIM_DIR)/ge_igraph_shim.c
SHIM_HDR := $(SHIM_DIR)/ge_igraph_shim.h

# Proto paths
PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name "*.proto")

# Go build flags
LDFLAGS := -ldflags="-s -w"

# CGO flags for igraph (populated by pkg-config)
CGO_CFLAGS := $(shell pkg-config --cflags igraph 2>/dev/null)
CGO_LDFLAGS := $(shell pkg-config --libs igraph 2>/dev/null)
IGRAPH_AVAILABLE := $(shell pkg-config --exists igraph && echo "yes" || echo "no")

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

proto: ## Generate Go code from proto files
	@echo "Generating proto files..."
	@mkdir -p $(GEN_DIR)
	PATH="$$PATH:$$(go env GOPATH)/bin" protoc \
		--proto_path=$(PROTO_DIR) \
		--proto_path=. \
		--go_out=$(GEN_DIR) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) \
		--go-grpc_opt=paths=source_relative \
		$(PROTO_FILES)
	@echo "Proto generation complete"

build: ## Build the binary (auto-detects igraph availability)
ifeq ($(IGRAPH_AVAILABLE),yes)
	@$(MAKE) build-cgo
else
	@$(MAKE) build-nocgo
endif

build-cgo: shim-deps ## Build with CGO enabled (requires igraph)
	@echo "Building $(BINARY_NAME) with CGO (igraph enabled)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go build $(LDFLAGS) -tags cgo -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/graph-engined
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME) (with igraph support)"

build-nocgo: ## Build without CGO (pure Go fallback only)
	@echo "Building $(BINARY_NAME) without CGO (pure Go fallback only)..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/graph-engined
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME) (pure Go only)"

run: build ## Run the server locally
	@echo "Starting $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME)

test: test-unit e2e ## Run all tests (unit + all E2E tests)
	@echo ""
	@echo "========================================="
	@echo "All tests complete!"
	@echo "========================================="

test-unit: ## Run unit tests only (no integration)
	@echo "Running unit tests..."
ifeq ($(IGRAPH_AVAILABLE),yes)
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go test -v -race ./...
else
	CGO_ENABLED=0 go test -v -race ./...
endif

test-shim: shim-deps ## Run shim-specific tests (requires igraph)
	@echo "Running shim tests..."
	CGO_ENABLED=1 \
	CGO_CFLAGS="$(CGO_CFLAGS)" \
	CGO_LDFLAGS="$(CGO_LDFLAGS)" \
	go test -v -race ./internal/shim/...

test-all: test ## Alias for 'test' - runs all tests

e2e: ## Run all Docker-based E2E tests (gRPC, Go client, Python client)
	@echo "Running all E2E tests..."
	cd e2e && ./run.sh all

e2e-grpc: ## Run raw gRPC E2E tests only
	@echo "Running raw gRPC E2E tests..."
	cd e2e && ./run.sh grpc

e2e-go: ## Run Go client library E2E tests only
	@echo "Running Go client library E2E tests..."
	cd e2e && ./run.sh go

e2e-py: ## Run Python client library E2E tests only
	@echo "Running Python client library E2E tests..."
	cd e2e && ./run.sh py

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BUILD_DIR)
	rm -rf $(GEN_DIR)
	go clean

lint: ## Run golangci-lint
	@echo "Running linter..."
	golangci-lint run ./...

# Development helpers
.PHONY: deps
deps: ## Download dependencies
	go mod download
	go mod tidy

.PHONY: fmt
fmt: ## Format Go code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet
	go vet ./...

# Proto dependencies check
.PHONY: proto-deps
proto-deps: ## Check proto dependencies
	@which protoc > /dev/null || (echo "protoc not found. Install with: brew install protobuf" && exit 1)
	@which protoc-gen-go > /dev/null || (echo "protoc-gen-go not found. Install with: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" && exit 1)
	@which protoc-gen-go-grpc > /dev/null || (echo "protoc-gen-go-grpc not found. Install with: go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest" && exit 1)
	@echo "All proto dependencies are installed"

# igraph/shim dependencies check
.PHONY: shim-deps shim-info
shim-deps: ## Check igraph dependencies for C shim
	@pkg-config --exists igraph || (echo ""; \
		echo "ERROR: igraph not found via pkg-config"; \
		echo ""; \
		echo "Install igraph:"; \
		echo "  macOS:   brew install igraph"; \
		echo "  Ubuntu:  apt-get install libigraph-dev"; \
		echo "  Fedora:  dnf install igraph-devel"; \
		echo ""; \
		echo "After installation, verify with: pkg-config --modversion igraph"; \
		exit 1)
	@echo "igraph found: $(shell pkg-config --modversion igraph)"
	@echo "CFLAGS: $(CGO_CFLAGS)"
	@echo "LDFLAGS: $(CGO_LDFLAGS)"

shim-info: ## Show igraph/shim build configuration
	@echo "========================================="
	@echo "igraph Shim Build Configuration"
	@echo "========================================="
	@echo "igraph available: $(IGRAPH_AVAILABLE)"
ifeq ($(IGRAPH_AVAILABLE),yes)
	@echo "igraph version:   $(shell pkg-config --modversion igraph)"
	@echo "CGO_CFLAGS:       $(CGO_CFLAGS)"
	@echo "CGO_LDFLAGS:      $(CGO_LDFLAGS)"
else
	@echo ""
	@echo "igraph not found - build will use pure Go fallback"
endif
	@echo ""
	@echo "Shim source files:"
	@echo "  Header: $(SHIM_HDR)"
	@echo "  Source: $(SHIM_SRC)"
	@test -f $(SHIM_HDR) && echo "  Header exists: yes" || echo "  Header exists: no"
	@test -f $(SHIM_SRC) && echo "  Source exists: yes" || echo "  Source exists: no"

# Docker build targets
DOCKER_IMAGE := graph-engine
DOCKER_TAG := latest

.PHONY: docker docker-cgo docker-nocgo docker-info
docker: docker-cgo ## Build Docker image (with igraph, default)

docker-cgo: ## Build Docker image with igraph support
	@echo "Building Docker image with igraph support..."
	docker build \
		--build-arg VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev") \
		--build-arg BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		-t $(DOCKER_IMAGE):cgo \
		.
	@echo "Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

docker-nocgo: ## Build Docker image without igraph (pure Go)
	@echo "Building Docker image without igraph (pure Go)..."
	docker build \
		-f Dockerfile.nocgo \
		--build-arg VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev") \
		--build-arg BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(DOCKER_IMAGE):nocgo \
		.
	@echo "Docker image built: $(DOCKER_IMAGE):nocgo"

docker-info: ## Show Docker image info
	@echo "========================================="
	@echo "Docker Images"
	@echo "========================================="
	@docker images $(DOCKER_IMAGE) 2>/dev/null || echo "No $(DOCKER_IMAGE) images found"
