#!/bin/bash

# Run comprehensive TMI API test suite with OAuth authentication
# Requires: newman, jq, TMI server running on 8080

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="$SCRIPT_DIR/test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_FILE="$OUTPUT_DIR/newman-results-$TIMESTAMP.json"
LOG_FILE="$OUTPUT_DIR/test-log-$TIMESTAMP.txt"
COLLECTION_FILE="$SCRIPT_DIR/comprehensive-test-collection.json"

echo "=== TMI API Comprehensive Test Suite ==="
echo "Timestamp: $TIMESTAMP"
echo "Collection: $COLLECTION_FILE"

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

# Check if comprehensive collection exists, fallback to original
if [ ! -f "$COLLECTION_FILE" ]; then
    echo "⚠️ Comprehensive collection not found, using original collection"
    COLLECTION_FILE="$SCRIPT_DIR/tmi-postman-collection.json"
fi

# Copy test data factory and auth helper to make them available 
echo "📁 Setting up test utilities..."
cp "$SCRIPT_DIR/test-data-factory.js" /tmp/ 2>/dev/null || echo "⚠️ test-data-factory.js not found"
cp "$SCRIPT_DIR/multi-user-auth.js" /tmp/ 2>/dev/null || echo "⚠️ multi-user-auth.js not found"

newman run "$COLLECTION_FILE" \
    --env-var "loginHint=test-runner-$TIMESTAMP" \
    --env-var "baseUrl=http://127.0.0.1:8080" \
    --env-var "oauthStubUrl=http://127.0.0.1:8079" \
    --reporters cli,json,htmlextra \
    --reporter-json-export "$OUTPUT_FILE" \
    --reporter-htmlextra-export "$OUTPUT_DIR/test-report-$TIMESTAMP.html" \
    --timeout-request 10000 \
    --delay-request 200 \
    --ignore-redirects \
    2>&1 | tee -a "$LOG_FILE"

# Capture exit code
TEST_EXIT_CODE=${PIPESTATUS[0]}

echo ""
echo "=== Test Summary ==="

# Parse results with jq if available
if command -v jq &> /dev/null && [ -f "$OUTPUT_FILE" ]; then
    TOTAL_REQUESTS=$(jq '.run.stats.requests.total' "$OUTPUT_FILE")
    FAILED_REQUESTS=$(jq '.run.stats.requests.failed' "$OUTPUT_FILE") 
    TOTAL_ASSERTIONS=$(jq '.run.stats.assertions.total' "$OUTPUT_FILE")
    FAILED_ASSERTIONS=$(jq '.run.stats.assertions.failed' "$OUTPUT_FILE")
    TOTAL_TIME=$(jq '.run.timings.completed - .run.timings.started' "$OUTPUT_FILE")
    
    echo "📊 Test Statistics:"
    echo "   Total requests: $TOTAL_REQUESTS"
    echo "   Failed requests: $FAILED_REQUESTS"
    echo "   Success rate: $(echo "scale=2; (($TOTAL_REQUESTS - $FAILED_REQUESTS) / $TOTAL_REQUESTS) * 100" | bc -l 2>/dev/null || echo "N/A")%"
    echo "   Total assertions: $TOTAL_ASSERTIONS" 
    echo "   Failed assertions: $FAILED_ASSERTIONS"
    echo "   Assertion success rate: $(echo "scale=2; (($TOTAL_ASSERTIONS - $FAILED_ASSERTIONS) / $TOTAL_ASSERTIONS) * 100" | bc -l 2>/dev/null || echo "N/A")%"
    echo "   Total time: ${TOTAL_TIME}ms"
    
    # Status code coverage analysis
    echo ""
    echo "📈 Status Code Coverage:"
    jq -r '.run.executions[].response.code' "$OUTPUT_FILE" 2>/dev/null | sort | uniq -c | while read count code; do
        echo "   $code: $count requests"
    done
    
    # Failed test details
    if [ "$FAILED_ASSERTIONS" -gt 0 ]; then
        echo ""
        echo "❌ Failed Tests:"
        jq -r '.run.executions[] | select(.assertions[]?.error) | "   " + .item.name + ": " + (.assertions[] | select(.error) | .assertion)' "$OUTPUT_FILE" 2>/dev/null || echo "   (Details unavailable)"
    fi
else
    echo "Results saved to: $OUTPUT_FILE"
fi

echo ""
echo "📄 Reports Generated:"
echo "   JSON: $OUTPUT_FILE"
echo "   HTML: $OUTPUT_DIR/test-report-$TIMESTAMP.html"
echo "   Log: $LOG_FILE"
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