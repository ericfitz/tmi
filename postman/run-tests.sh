#!/bin/bash

# Run Postman collection tests with OAuth authentication
# Requires: newman, jq, TMI server running on 8080

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="$SCRIPT_DIR/test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="$OUTPUT_DIR/newman-results-$TIMESTAMP.json"
LOG_FILE="$OUTPUT_DIR/test-log-$TIMESTAMP.txt"

echo "=== TMI Postman Collection Test Runner ==="
echo "Timestamp: $TIMESTAMP"

# Create output directory if it doesn't exist
mkdir -p "$OUTPUT_DIR"

# Stop any existing OAuth stub
echo "Stopping existing OAuth stub..."
cd "$PROJECT_ROOT"
make oauth-stub-stop 2>&1 | tee -a "$LOG_FILE" || true

# Start OAuth stub
echo "Starting OAuth stub..."
make oauth-stub-start 2>&1 | tee -a "$LOG_FILE"

# Wait for stub to be ready
echo "Waiting for OAuth stub to be ready..."
for i in {1..10}; do
    if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
        echo "OAuth stub is ready"
        break
    fi
    echo "Waiting... ($i/10)"
    sleep 1
done

# Check if TMI server is running
echo "Checking TMI server..."
if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
    echo "ERROR: TMI server is not running on port 8080"
    echo "Please run: make dev-start"
    exit 1
fi

# Run newman tests
echo ""
echo "Running Postman collection with newman..."
echo "User: postman-runner-$TIMESTAMP"
echo "Output: $OUTPUT_FILE"
echo "Log: $LOG_FILE"
echo ""

newman run "$SCRIPT_DIR/tmi-postman-collection.json" \
    --env-var "loginHint=postman-runner-$TIMESTAMP" \
    --env-var "baseUrl=http://127.0.0.1:8080" \
    --env-var "oauthStubUrl=http://127.0.0.1:8079" \
    --reporters cli,json \
    --reporter-json-export "$OUTPUT_FILE" \
    --timeout-request 5000 \
    --delay-request 100 \
    2>&1 | tee -a "$LOG_FILE"

# Capture exit code
TEST_EXIT_CODE=${PIPESTATUS[0]}

echo ""
echo "=== Test Summary ==="

# Parse results with jq if available
if command -v jq &> /dev/null && [ -f "$OUTPUT_FILE" ]; then
    echo "Total requests: $(jq '.run.stats.requests.total' "$OUTPUT_FILE")"
    echo "Failed requests: $(jq '.run.stats.requests.failed' "$OUTPUT_FILE")"
    echo "Total assertions: $(jq '.run.stats.assertions.total' "$OUTPUT_FILE")"
    echo "Failed assertions: $(jq '.run.stats.assertions.failed' "$OUTPUT_FILE")"
    echo "Total time: $(jq '.run.timings.completed - .run.timings.started' "$OUTPUT_FILE")ms"
else
    echo "Results saved to: $OUTPUT_FILE"
fi

echo ""
echo "Full log saved to: $LOG_FILE"
echo ""

# Stop OAuth stub
echo "Stopping OAuth stub..."
cd "$PROJECT_ROOT"
make oauth-stub-stop 2>&1 | tee -a "$LOG_FILE" || true

# Exit with newman's exit code
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✅ All tests passed!"
else
    echo "❌ Some tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE