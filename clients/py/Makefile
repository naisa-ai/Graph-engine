# Graph-engine Python Client Makefile

PROTO_DIR := ../../proto
GEN_DIR := ./graphengine/gen

.PHONY: proto clean test lint format install dev-install

# Generate Python code from proto files
proto:
	@echo "Generating Python proto code..."
	@rm -rf $(GEN_DIR)
	@mkdir -p $(GEN_DIR)
	python3 -m grpc_tools.protoc \
		--python_out=$(GEN_DIR) \
		--grpc_python_out=$(GEN_DIR) \
		--pyi_out=$(GEN_DIR) \
		-I $(PROTO_DIR) \
		$(PROTO_DIR)/graphengine/v1/*.proto
	@# Move files from nested directory to gen root
	@if [ -d "$(GEN_DIR)/graphengine/v1" ]; then \
		mv $(GEN_DIR)/graphengine/v1/* $(GEN_DIR)/; \
		rm -rf $(GEN_DIR)/graphengine; \
	fi
	@# Fix import statements at line start only (do not modify binary descriptor data)
	@# Change 'from graphengine.v1 import' to 'from . import' for relative imports
	@echo "Fixing import statements in generated files..."
	@for f in $(GEN_DIR)/*.py; do \
		if [ -f "$$f" ]; then \
			sed -i.bak 's/^from graphengine\.v1 import/from . import/g' "$$f"; \
			rm -f "$$f.bak"; \
		fi \
	done
	@# Create __init__.py with exports
	@echo "from .graph_engine_pb2 import *" > $(GEN_DIR)/__init__.py
	@echo "from .graph_engine_pb2_grpc import *" >> $(GEN_DIR)/__init__.py
	@echo "from .graph_engine_ops_pb2 import *" >> $(GEN_DIR)/__init__.py
	@echo "from .graph_engine_ops_pb2_grpc import *" >> $(GEN_DIR)/__init__.py
	@echo "Proto generation complete"

# Clean generated files
clean:
	rm -rf $(GEN_DIR)
	rm -rf dist/
	rm -rf build/
	rm -rf *.egg-info
	find . -type d -name __pycache__ -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete

# Install package in development mode
dev-install:
	pip install -e ".[dev]"

# Install package
install:
	pip install -e .

# Run tests
test:
	pytest -v tests/

# Run tests with coverage
test-coverage:
	pytest -v --cov=graphengine --cov-report=html tests/

# Type checking
typecheck:
	mypy graphengine/

# Lint
lint:
	ruff check graphengine/

# Format code
format:
	ruff format graphengine/

# Build package
build:
	python -m build

# All: generate proto, install dev, and test
all: proto dev-install test
