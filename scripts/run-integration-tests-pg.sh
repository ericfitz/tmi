#!/bin/bash
# run-integration-tests-pg.sh - Run TMI integration tests with PostgreSQL
#
# This script sets up the PostgreSQL environment and runs integration tests.
#
# Prerequisites:
#   - Docker installed and running
#   - Go installed
#
# Usage:
#   ./scripts/run-integration-tests-pg.sh
#   make test-integration-pg
#

set -e

# Change to project root
cd "$(dirname "$0")/.."

# Configuration
CONFIG_FILE="config-test-integration-pg.yml"
SERVER_PORT=8080
LOG_FILE="logs/integration-test-server.log"

echo "=========================================="
echo "TMI Integration Tests - PostgreSQL"
echo "=========================================="
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "[INFO] Cleaning up..."

    # Stop server if running
    if [ -f .server.pid ]; then
        PID=$(cat .server.pid)
        if ps -p "$PID" > /dev/null 2>&1; then
            echo "[INFO] Stopping server (PID: $PID)..."
            kill "$PID" 2>/dev/null || true
            sleep 2
            if ps -p "$PID" > /dev/null 2>&1; then
                kill -9 "$PID" 2>/dev/null || true
            fi
        fi
        rm -f .server.pid
    fi

    # Stop containers
    make clean-everything 2>/dev/null || true

    echo "[SUCCESS] Cleanup completed"
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Step 1: Clean environment
echo "[INFO] Cleaning environment..."
make clean-everything

# Step 2: Start PostgreSQL container
echo "[INFO] Starting PostgreSQL container..."
make start-database

# Step 3: Start Redis container
echo "[INFO] Starting Redis container..."
make start-redis

# Step 4: Wait for database to be ready
echo "[INFO] Waiting for database to be ready..."
make wait-database

# Step 5: Run migrations
echo "[INFO] Running database migrations..."
make migrate-database

# Step 6: Build server if needed
if [ ! -f "bin/tmiserver" ]; then
    echo "[INFO] Building server..."
    make build-server
fi

# Step 7: Start server
echo "[INFO] Starting server with config: $CONFIG_FILE"
mkdir -p logs
./bin/tmiserver --config="$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
SERVER_PID=$!
echo $SERVER_PID > .server.pid
echo "[INFO] Server started with PID: $SERVER_PID"

# Step 8: Wait for server to be ready
echo "[INFO] Waiting for server to be ready on port $SERVER_PORT..."
TIMEOUT=60
while [ $TIMEOUT -gt 0 ]; do
    if curl -s "http://localhost:$SERVER_PORT/" > /dev/null 2>&1; then
        echo "[SUCCESS] Server is ready!"
        break
    fi
    sleep 2
    TIMEOUT=$((TIMEOUT - 2))
    echo "  Waiting... ($TIMEOUT seconds remaining)"
done

if [ $TIMEOUT -le 0 ]; then
    echo "[ERROR] Server failed to start within 60 seconds"
    echo "[INFO] Server log tail:"
    tail -50 "$LOG_FILE"
    exit 1
fi

# Step 9: Run integration tests
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
