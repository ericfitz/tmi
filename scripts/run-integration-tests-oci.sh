#!/bin/bash
# run-integration-tests-oci.sh - Run TMI integration tests with OCI Autonomous Database
#
# This script sets up the Oracle environment and runs integration tests.
#
# Prerequisites:
#   1. Oracle Instant Client installed
#   2. Wallet extracted to ./wallet directory
#   3. Database user created in OCI ADB
#
# Usage:
#   ./scripts/run-integration-tests-oci.sh [--cleanup]
#   make test-integration-oci
#   make test-integration-oci CLEANUP=true
#
# Options:
#   --cleanup    Stop server and clean Redis container after tests (default: leave running)
#
# Configuration:
#   Edit the variables below to match your environment

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

# ============================================================================
# CONFIGURATION
# ============================================================================
# Source OCI environment variables from oci-env.sh
# This file contains secrets and is gitignored.
# Copy oci-env.sh.example to oci-env.sh and edit with your values.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OCI_ENV_FILE="$SCRIPT_DIR/oci-env.sh"

if [ -f "$OCI_ENV_FILE" ]; then
    source "$OCI_ENV_FILE"
else
    echo "[ERROR] OCI environment file not found: $OCI_ENV_FILE"
    echo ""
    echo "To set up OCI configuration:"
    echo "  cp scripts/oci-env.sh.example scripts/oci-env.sh"
    echo "  # Edit scripts/oci-env.sh with your values"
    exit 1
fi
# ============================================================================
# END CONFIGURATION
# ============================================================================

# Change to project root
cd "$(dirname "$0")/.."

# Configuration
CONFIG_FILE="config-test-integration-oci.yml"
SERVER_PORT=8081
LOG_FILE="logs/integration-test-server-oci.log"

echo "=========================================="
echo "TMI Integration Tests - OCI Autonomous DB"
echo "=========================================="
echo ""

# Validate configuration
if [ -z "$ORACLE_PASSWORD" ]; then
    echo "[ERROR] ORACLE_PASSWORD is not set"
    echo "Set ORACLE_PASSWORD environment variable or edit this script"
    exit 1
fi

if [ ! -d "$DYLD_LIBRARY_PATH" ]; then
    echo "[ERROR] Oracle Instant Client not found at: $DYLD_LIBRARY_PATH"
    echo "Edit this script and set DYLD_LIBRARY_PATH to your Instant Client location"
    exit 1
fi

if [ ! -d "$TNS_ADMIN" ]; then
    echo "[ERROR] Wallet directory not found at: $TNS_ADMIN"
    echo "Extract your OCI wallet to the ./wallet directory"
    exit 1
fi

echo "[INFO] Configuration:"
echo "  DYLD_LIBRARY_PATH: $DYLD_LIBRARY_PATH"
echo "  TNS_ADMIN: $TNS_ADMIN"
echo "  Config file: $CONFIG_FILE"
echo ""

# Cleanup function
cleanup() {
    echo ""
    echo "[INFO] Cleaning up..."

    # Always stop OAuth stub (lightweight, doesn't affect next test run)
    if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
        make stop-oauth-stub 2>/dev/null || true
    fi

    # Always stop the integration test server to avoid port conflicts
    if [ -f .server.pid ]; then
        PID=$(cat .server.pid)
        echo "[INFO] Stopping integration test server (PID: $PID)..."
        kill "$PID" 2>/dev/null || true
        sleep 2
        if ps -p "$PID" > /dev/null 2>&1; then
            kill -9 "$PID" 2>/dev/null || true
        fi
        rm -f .server.pid
        echo "[SUCCESS] Integration test server stopped"
    fi

    # Conditionally clean Redis
    if [ "$CLEANUP_AFTER" = "true" ]; then
        make stop-redis 2>/dev/null || true
        echo "[SUCCESS] Full cleanup completed (server stopped, Redis removed)"
    else
        echo "[INFO] Redis left running (use --cleanup to stop)"
        echo "[SUCCESS] Cleanup completed (server stopped, Redis preserved)"
    fi
}

# Set trap for cleanup on exit
trap cleanup EXIT

# Step 1: Stop any existing server
echo "[INFO] Stopping any existing server..."
make stop-server 2>/dev/null || true

# Step 2: Start Redis container
echo "[INFO] Starting Redis container..."
make start-redis

# Step 3: Build server if needed
if [ ! -f "bin/tmiserver" ]; then
    echo "[INFO] Building server..."
    make build-server
fi

# Step 4: Start server with OCI config
echo "[INFO] Starting server with OCI configuration..."
mkdir -p logs
./bin/tmiserver --config="$CONFIG_FILE" > "$LOG_FILE" 2>&1 &
SERVER_PID=$!
echo $SERVER_PID > .server.pid
echo "[INFO] Server started with PID: $SERVER_PID"

# Step 5: Wait for server to be ready
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

# Step 6: Run integration tests
echo ""
echo "[INFO] Running integration tests..."
echo ""

TEST_EXIT_CODE=0
LOGGING_IS_TEST=true \
TEST_SERVER_URL="http://localhost:$SERVER_PORT" \
TEST_REDIS_HOST=localhost \
TEST_REDIS_PORT=6379 \
    go test -v -timeout=10m ./api/... -run "Integration" \
    | tee integration-test-oci.log \
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
    grep -E "FAIL:|--- FAIL" integration-test-oci.log || true
fi

exit $TEST_EXIT_CODE
