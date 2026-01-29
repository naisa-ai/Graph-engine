# Graph-engine Go Client Makefile

PROTO_DIR := ../../proto
GEN_DIR := ./gen

.PHONY: proto clean test lint

# Generate Go code from proto files
proto:
	@echo "Generating Go proto code..."
	@mkdir -p $(GEN_DIR)
	protoc \
		--go_out=$(GEN_DIR) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_DIR) \
		--go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_DIR)/graphengine/v1/*.proto
	@echo "Proto generation complete"

# Clean generated files
clean:
	rm -rf $(GEN_DIR)

# Run tests
test:
	go test -v ./...

# Run tests with coverage
test-coverage:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run ./...

# Build
build:
	go build ./...

# Tidy dependencies
tidy:
	go mod tidy

# All: generate proto, tidy, and build
all: proto tidy build
