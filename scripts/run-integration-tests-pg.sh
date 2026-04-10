#!/bin/bash
# run-integration-tests-pg.sh - Run TMI integration tests with PostgreSQL
#
# Runs integration tests against the development environment (dev server,
# dev PostgreSQL on port 5432, dev Redis on port 6379).
#
# Prerequisites:
#   - Development environment running: make start-dev
#   - Go installed
#
# Usage:
#   ./scripts/run-integration-tests-pg.sh
#   make test-integration-pg
#

set -e

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

# Step 1: Check server is running
echo "[INFO] Checking for running server on port $SERVER_PORT..."
if ! curl -s "http://localhost:$SERVER_PORT/" > /dev/null 2>&1; then
    echo "[ERROR] TMI server is not running on port $SERVER_PORT"
    echo "Start the server first with: make start-dev"
    exit 1
fi
echo "[SUCCESS] Server is ready!"

# Step 2: Ensure OAuth stub is running for workflow tests
echo "[INFO] Ensuring OAuth stub is running..."
if ensure_oauth_stub; then
    OAUTH_STUB_RUNNING=true
else
    echo "[WARNING] OAuth stub not available - workflow tests will be skipped"
    OAUTH_STUB_RUNNING=false
fi

# Step 3: Run integration tests
echo ""
echo "[INFO] Running integration tests..."
echo ""

TEST_EXIT_CODE=0
LOGGING_IS_TEST=true \
TEST_DB_HOST=localhost \
TEST_DB_PORT=5432 \
TEST_DB_USER=tmi_dev \
TEST_DB_PASSWORD=dev123 \
TEST_DB_NAME=tmi_dev \
TEST_REDIS_HOST=localhost \
TEST_REDIS_PORT=6379 \
TEST_SERVER_URL="http://localhost:$SERVER_PORT" \
    go test -v -timeout=10m ./api/... -run "Integration" \
    | tee integration-test.log \
    || TEST_EXIT_CODE=$?

# Step 4: Run workflow tests if OAuth stub is running
if [ "$OAUTH_STUB_RUNNING" = true ]; then
    echo ""
    echo "[INFO] Running workflow integration tests..."
    echo ""

    # Clear rate limit keys from Redis to prevent auth flow rate limiting
    echo "[INFO] Clearing rate limit keys from Redis..."
    docker exec tmi-redis redis-cli --scan --pattern "auth:ratelimit:*" | xargs -r docker exec -i tmi-redis redis-cli DEL 2>/dev/null || true
    docker exec tmi-redis redis-cli --scan --pattern "ip:ratelimit:*" | xargs -r docker exec -i tmi-redis redis-cli DEL 2>/dev/null || true

    WORKFLOW_EXIT_CODE=0
    # Run tests from within the integration module directory
    pushd test/integration > /dev/null
    go mod tidy 2>/dev/null || true
    INTEGRATION_TESTS=true \
    TMI_SERVER_URL="http://localhost:$SERVER_PORT" \
    TEST_DB_HOST=localhost \
    TEST_DB_PORT=5432 \
    TEST_DB_USER=tmi_dev \
    TEST_DB_PASSWORD=dev123 \
    TEST_DB_NAME=tmi_dev \
        go test -v -timeout=15m -p 1 ./workflows/... \
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
