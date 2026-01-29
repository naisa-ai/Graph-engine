# Graph-engine Client Libraries Makefile

.PHONY: all proto clean test

# Build all clients
all: proto-go proto-py

# Generate proto for Go client
proto-go:
	@echo "=== Generating Go client protos ==="
	$(MAKE) -C go proto

# Generate proto for Python client
proto-py:
	@echo "=== Generating Python client protos ==="
	$(MAKE) -C py proto

# Run all proto generation
proto: proto-go proto-py

# Clean all generated files
clean:
	$(MAKE) -C go clean
	$(MAKE) -C py clean

# Run all tests
test: test-go test-py

test-go:
	@echo "=== Running Go client tests ==="
	$(MAKE) -C go test

test-py:
	@echo "=== Running Python client tests ==="
	$(MAKE) -C py test

# Build Go client
build-go:
	$(MAKE) -C go build

# Build Python client (package)
build-py:
	$(MAKE) -C py build

# Install Python client in dev mode
install-py:
	$(MAKE) -C py dev-install
