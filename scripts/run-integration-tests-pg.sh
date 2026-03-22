#!/bin/bash
# run-integration-tests-pg.sh - Run TMI integration tests with PostgreSQL
#
# This script sets up the PostgreSQL environment and runs integration tests.
# Uses isolated test containers (tmi-postgresql-test, tmi-redis-test) so that
# the dev environment is not affected.
#
# Prerequisites:
#   - Docker installed and running
#   - Go installed
#
# Usage:
#   ./scripts/run-integration-tests-pg.sh [--cleanup]
#   make test-integration-pg
#   make test-integration-pg CLEANUP=true
#
# Options:
#   --cleanup    Stop server and clean test containers after tests (default: leave running)
#

set -e

# Parse arguments
CLEANUP_AFTER=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --cleanup)
            CLEANUP_AFTER=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--cleanup]"
            exit 1
            ;;
    esac
done

# Change to project root
cd "$(dirname "$0")/.."
PROJECT_ROOT="$(pwd)"
source "scripts/oauth-stub-lib.sh"

# Configuration
SERVER_PORT=8080

echo "=========================================="
echo "TMI Integration Tests - PostgreSQL"
echo "=========================================="
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "[INFO] Cleaning up..."

    # Conditionally clean test containers (never touches dev containers)
    if [ "$CLEANUP_AFTER" = "true" ]; then
        make clean-test-infrastructure 2>/dev/null || true
        echo "[SUCCESS] Cleanup completed (test containers removed)"
    else
        echo "[INFO] Test containers left running (use --cleanup to stop)"
        echo "[SUCCESS] Cleanup completed (test containers preserved)"
    fi
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Step 1: Clean test environment (does NOT touch dev containers)
echo "[INFO] Cleaning test environment..."
make clean-test-infrastructure 2>/dev/null || true

# Step 2: Start test PostgreSQL container (ephemeral, no volume mount)
echo "[INFO] Starting test PostgreSQL container..."
make start-test-database

# Step 3: Start test Redis container
echo "[INFO] Starting test Redis container..."
make start-test-redis

# Step 4: Wait for test database to be ready
echo "[INFO] Waiting for test database to be ready..."
make wait-test-database

# Step 5: Run migrations against test database
echo "[INFO] Running test database migrations..."
make migrate-test-database

# Step 6: Check server is running
echo "[INFO] Checking for running server on port $SERVER_PORT..."
if ! curl -s "http://localhost:$SERVER_PORT/" > /dev/null 2>&1; then
    echo "[ERROR] TMI server is not running on port $SERVER_PORT"
    echo "Start the server first with: make start-dev"
    exit 1
fi
echo "[SUCCESS] Server is ready!"

# Step 7: Ensure OAuth stub is running for workflow tests
echo "[INFO] Ensuring OAuth stub is running..."
if ensure_oauth_stub; then
    OAUTH_STUB_RUNNING=true
else
    echo "[WARNING] OAuth stub not available - workflow tests will be skipped"
    OAUTH_STUB_RUNNING=false
fi

# Step 10: Run integration tests
echo ""
echo "[INFO] Running integration tests..."
echo ""

TEST_EXIT_CODE=0
LOGGING_IS_TEST=true \
TEST_DB_HOST=localhost \
TEST_DB_PORT=5433 \
TEST_DB_USER=tmi_dev \
TEST_DB_PASSWORD=dev123 \
TEST_DB_NAME=tmi_dev \
TEST_REDIS_HOST=localhost \
TEST_REDIS_PORT=6380 \
TEST_SERVER_URL="http://localhost:$SERVER_PORT" \
    go test -v -timeout=10m ./api/... -run "Integration" \
    | tee integration-test.log \
    || TEST_EXIT_CODE=$?

# Step 11: Run workflow tests if OAuth stub is running
if [ "$OAUTH_STUB_RUNNING" = true ]; then
    echo ""
    echo "[INFO] Running workflow integration tests..."
    echo ""

    # Clear rate limit keys from test Redis to prevent auth flow rate limiting
    # during parallel test execution (token endpoint allows 20 req/min per IP)
    echo "[INFO] Clearing rate limit keys from test Redis..."
    docker exec tmi-redis-test redis-cli -n 1 --scan --pattern "auth:ratelimit:*" | xargs -r docker exec -i tmi-redis-test redis-cli -n 1 DEL 2>/dev/null || true
    docker exec tmi-redis-test redis-cli -n 1 --scan --pattern "ip:ratelimit:*" | xargs -r docker exec -i tmi-redis-test redis-cli -n 1 DEL 2>/dev/null || true

    WORKFLOW_EXIT_CODE=0
    # Run tests from within the integration module directory
    pushd test/integration > /dev/null
    go mod tidy 2>/dev/null || true
    INTEGRATION_TESTS=true \
    TMI_SERVER_URL="http://localhost:$SERVER_PORT" \
    TEST_DB_HOST=localhost \
    TEST_DB_PORT=5433 \
    TEST_DB_USER=tmi_dev \
    TEST_DB_PASSWORD=dev123 \
    TEST_DB_NAME=tmi_dev \
        go test -v -timeout=10m -p 1 ./workflows/... \
        | tee -a ../../integration-test.log \
        || WORKFLOW_EXIT_CODE=$?
    popd > /dev/null

    if [ $WORKFLOW_EXIT_CODE -ne 0 ]; then
        TEST_EXIT_CODE=$WORKFLOW_EXIT_CODE
    fi
fi

echo ""
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "=========================================="
    echo "[SUCCESS] Integration tests completed successfully"
    echo "=========================================="
else
    echo "=========================================="
    echo "[ERROR] Integration tests failed with exit code $TEST_EXIT_CODE"
    echo "=========================================="
    echo ""
    echo "[INFO] Failed test summary:"
    grep -E "FAIL:|--- FAIL" integration-test.log || true
fi

exit $TEST_EXIT_CODE
