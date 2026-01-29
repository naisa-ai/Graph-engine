#!/bin/bash
# E2E Test Runner Script
# Runs the Graph-engine service in Docker and executes API tests
#
# Usage:
#   ./run.sh           # Run all test suites (grpc, go, py)
#   ./run.sh grpc      # Run only raw gRPC tests
#   ./run.sh go        # Run only Go client tests
#   ./run.sh py        # Run only Python client tests
#   ./run.sh all       # Same as no argument - run all test suites

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# Parse arguments
TEST_SUITE="${1:-all}"

# Determine which profile to use and which container to track
case "$TEST_SUITE" in
    grpc)
        PROFILE="grpc"
        CONTAINER="e2e-client"
        SUITE_NAME="Raw gRPC"
        ;;
    go)
        PROFILE="go"
        CONTAINER="e2e-go-client"
        SUITE_NAME="Go Client Library"
        ;;
    py)
        PROFILE="py"
        CONTAINER="e2e-py-client"
        SUITE_NAME="Python Client Library"
        ;;
    all)
        PROFILE="all"
        CONTAINER=""
        SUITE_NAME="All Test Suites"
        ;;
    *)
        echo "Unknown test suite: $TEST_SUITE"
        echo "Usage: $0 [grpc|go|py|all]"
        exit 1
        ;;
esac

echo "========================================="
echo "Graph-Engine E2E Tests - $SUITE_NAME"
echo "========================================="

# Clean up any previous runs
docker compose down --remove-orphans 2>/dev/null || true

# Build and run
echo "Building and starting services..."

if [ "$TEST_SUITE" = "all" ]; then
    # Run all test suites sequentially
    EXIT_CODE=0
    
    echo ""
    echo "--- Running Raw gRPC Tests ---"
    docker compose --profile grpc up --build --abort-on-container-exit --exit-code-from e2e-client || EXIT_CODE=$?
    docker compose down --remove-orphans
    
    if [ $EXIT_CODE -eq 0 ]; then
        echo ""
        echo "--- Running Go Client Library Tests ---"
        docker compose --profile go up --build --abort-on-container-exit --exit-code-from e2e-go-client || EXIT_CODE=$?
        docker compose down --remove-orphans
    fi
    
    if [ $EXIT_CODE -eq 0 ]; then
        echo ""
        echo "--- Running Python Client Library Tests ---"
        docker compose --profile py up --build --abort-on-container-exit --exit-code-from e2e-py-client || EXIT_CODE=$?
        docker compose down --remove-orphans
    fi
else
    docker compose --profile "$PROFILE" up --build --abort-on-container-exit --exit-code-from "$CONTAINER"
    EXIT_CODE=$?
    
    # Clean up
    echo "Cleaning up..."
    docker compose down --remove-orphans
fi

echo "========================================="
if [ $EXIT_CODE -eq 0 ]; then
    echo "E2E Tests PASSED - $SUITE_NAME"
else
    echo "E2E Tests FAILED - $SUITE_NAME (exit code: $EXIT_CODE)"
fi
echo "========================================="

exit $EXIT_CODE
