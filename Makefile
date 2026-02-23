# Graph-engine Makefile

.PHONY: proto build run test test-unit test-docker test-all test-shim clean lint help e2e e2e-grpc e2e-go e2e-py docker docker-base docker-base-rebuild e2e-base e2e-go-base e2e-py-base e2e-base-rebuild

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

# Docker image for building/testing
DOCKER_IMAGE := graph-engine
DOCKER_TAG := latest
DOCKER_BUILD_IMAGE := $(DOCKER_IMAGE)-builder
DOCKER_BASE_IMAGE := $(DOCKER_IMAGE)-base

# E2E base images
E2E_GO_BASE_IMAGE := $(DOCKER_IMAGE)-e2e-go-base
E2E_PY_BASE_IMAGE := $(DOCKER_IMAGE)-e2e-py-base

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

build: docker ## Build the binary via Docker (default)

build-local: ## Build locally (requires igraph on host)
	@pkg-config --exists igraph || (echo "ERROR: igraph not found. Use 'make build' to build via Docker." && exit 1)
	@echo "Building $(BINARY_NAME) locally..."
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$(pkg-config --variable=includedir igraph) $$(pkg-config --cflags igraph)" \
	CGO_LDFLAGS="$$(pkg-config --libs igraph)" \
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/graph-engined
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

run: docker ## Run the server in Docker
	@echo "Starting $(BINARY_NAME) in Docker..."
	docker run --rm -p 50051:50051 $(DOCKER_IMAGE):$(DOCKER_TAG)

run-local: build-local ## Run the server locally (requires igraph on host)
	@echo "Starting $(BINARY_NAME)..."
	./$(BUILD_DIR)/$(BINARY_NAME)

test: test-docker e2e ## Run all tests (unit via Docker + E2E)
	@echo ""
	@echo "========================================="
	@echo "All tests complete!"
	@echo "========================================="

test-docker: docker-base ## Run unit tests in Docker (default, no host deps needed)
	@echo "Running unit tests in Docker..."
	@echo "Using base image: $(DOCKER_BASE_IMAGE):$(DOCKER_TAG)"
	docker run --rm \
		-v $(PWD):/app/src \
		-w /app/src \
		$(DOCKER_BASE_IMAGE):$(DOCKER_TAG) \
		go test -v -race ./...

test-unit: test-docker ## Alias for test-docker

test-local: ## Run unit tests locally (requires igraph on host)
	@pkg-config --exists igraph || (echo "ERROR: igraph not found. Use 'make test' to test via Docker." && exit 1)
	@echo "Running unit tests locally..."
	CGO_ENABLED=1 \
	CGO_CFLAGS="-I$$(pkg-config --variable=includedir igraph) $$(pkg-config --cflags igraph)" \
	CGO_LDFLAGS="$$(pkg-config --libs igraph)" \
	go test -v -race ./...

test-shim: docker-base ## Run shim-specific tests in Docker
	@echo "Running shim tests in Docker..."
	docker run --rm \
		-v $(PWD):/app/src \
		-w /app/src \
		$(DOCKER_BASE_IMAGE):$(DOCKER_TAG) \
		go test -v -race ./internal/shim/...

test-all: test ## Alias for 'test' - runs all tests

e2e: e2e-base ## Run all Docker-based E2E tests (gRPC, Go client, Python client)
	@echo "Running all E2E tests..."
	cd e2e && ./run.sh all

e2e-grpc: docker-base e2e-go-base ## Run raw gRPC E2E tests only
	@echo "Running raw gRPC E2E tests..."
	cd e2e && ./run.sh grpc

e2e-go: docker-base e2e-go-base ## Run Go client library E2E tests only
	@echo "Running Go client library E2E tests..."
	cd e2e && ./run.sh go

e2e-py: docker-base e2e-py-base ## Run Python client library E2E tests only
	@echo "Running Python client library E2E tests..."
	cd e2e && ./run.sh py

clean: ## Clean build artifacts and Docker images
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	rm -rf $(GEN_DIR)
	go clean
	@echo "Removing Docker images..."
	-docker rmi $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || true
	-docker rmi $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || true
	-docker rmi $(DOCKER_BUILD_IMAGE):test 2>/dev/null || true
	-docker rmi $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || true
	-docker rmi $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || true
	@echo "Cleaning e2e docker compose images..."
	-cd e2e && docker compose down --rmi local 2>/dev/null || true
	@echo "Clean complete."

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

# Local development helpers (require igraph on host)
.PHONY: shim-info
shim-info: ## Show igraph/shim build configuration (requires igraph on host)
	@pkg-config --exists igraph || (echo "igraph not installed on host. Use Docker for builds/tests." && exit 0)
	@echo "========================================="
	@echo "igraph Shim Build Configuration (Host)"
	@echo "========================================="
	@echo "igraph version:   $$(pkg-config --modversion igraph)"
	@echo "CGO_CFLAGS:       $$(pkg-config --cflags igraph)"
	@echo "CGO_LDFLAGS:      $$(pkg-config --libs igraph)"
	@echo ""
	@echo "Shim source files:"
	@echo "  Header: $(SHIM_HDR)"
	@echo "  Source: $(SHIM_SRC)"
	@test -f $(SHIM_HDR) && echo "  Header exists: yes" || echo "  Header exists: no"
	@test -f $(SHIM_SRC) && echo "  Source exists: yes" || echo "  Source exists: no"

# Docker build targets
.PHONY: docker docker-base docker-base-rebuild docker-info
docker: ## Build Docker image
	@echo "Building Docker image..."
	docker build \
		--build-arg VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev") \
		--build-arg BUILD_TIME=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
		-t $(DOCKER_IMAGE):$(DOCKER_TAG) \
		.
	@echo "Docker image built: $(DOCKER_IMAGE):$(DOCKER_TAG)"

docker-base: ## Build base Docker image (cached, only rebuilds if go.mod/go.sum changed)
	@if docker images -q $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null | grep -q .; then \
		echo "Base image $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) already exists."; \
		echo "Run 'make docker-base-rebuild' to force rebuild."; \
	else \
		echo "Building base Docker image..."; \
		docker build -f Dockerfile.base -t $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) .; \
		echo "Base image built: $(DOCKER_BASE_IMAGE):$(DOCKER_TAG)"; \
	fi

docker-base-rebuild: ## Force rebuild the base Docker image
	@echo "Force rebuilding base Docker image..."
	docker build -f Dockerfile.base -t $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) .
	@echo "Base image rebuilt: $(DOCKER_BASE_IMAGE):$(DOCKER_TAG)"

docker-info: ## Show Docker image info
	@echo "========================================="
	@echo "Docker Images"
	@echo "========================================="
	@echo "Production image:"
	@docker images $(DOCKER_IMAGE):$(DOCKER_TAG) 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "Base image (for unit testing):"
	@docker images $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "E2E Go base image:"
	@docker images $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || echo "  Not found"
	@echo ""
	@echo "E2E Python base image:"
	@docker images $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null || echo "  Not found"

# E2E base image targets
e2e-base: docker-base e2e-go-base e2e-py-base ## Build all E2E base images (server + clients)
	@echo "All E2E base images ready."

e2e-go-base: ## Build E2E Go client base image
	@if docker images -q $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null | grep -q .; then \
		echo "E2E Go base image $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) already exists."; \
		echo "Run 'make e2e-base-rebuild' to force rebuild."; \
	else \
		echo "Building E2E Go base image..."; \
		docker build -f e2e/Dockerfile.go-base -t $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) .; \
		echo "E2E Go base image built: $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG)"; \
	fi

e2e-py-base: ## Build E2E Python client base image
	@if docker images -q $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) 2>/dev/null | grep -q .; then \
		echo "E2E Python base image $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) already exists."; \
		echo "Run 'make e2e-base-rebuild' to force rebuild."; \
	else \
		echo "Building E2E Python base image..."; \
		docker build -f e2e/Dockerfile.py-base -t $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) .; \
		echo "E2E Python base image built: $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG)"; \
	fi

e2e-base-rebuild: ## Force rebuild all E2E base images
	@echo "Force rebuilding all E2E base images..."
	docker build -f Dockerfile.base -t $(DOCKER_BASE_IMAGE):$(DOCKER_TAG) .
	docker build -f e2e/Dockerfile.go-base -t $(E2E_GO_BASE_IMAGE):$(DOCKER_TAG) .
	docker build -f e2e/Dockerfile.py-base -t $(E2E_PY_BASE_IMAGE):$(DOCKER_TAG) .
	@echo "All E2E base images rebuilt."
