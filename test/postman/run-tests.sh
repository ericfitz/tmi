#!/bin/bash

# Run comprehensive TMI API test suite with OAuth authentication
# Requires: newman, jq, TMI server running on 8080 (unless --start-server is used)
#
# Usage:
#   ./test/postman/run-tests.sh [--start-server]
#   make test-api
#   make test-api START_SERVER=true
#
# Options:
#   --start-server    Start TMI server if not already running (default: expect running)

set -e

# Parse arguments
START_SERVER=false
while [[ $# -gt 0 ]]; do
    case $1 in
        --start-server)
            START_SERVER=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--start-server]"
            exit 1
            ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"

# Provide the real test data factory to every script in every run (see lib/factory-globals.sh)
source "${SCRIPT_DIR}/lib/factory-globals.sh"
FACTORY_GLOBALS_FILE="$(make_factory_globals_tmp "${SCRIPT_DIR}/test-data-factory.js")" || exit 1
trap 'rm -f "$FACTORY_GLOBALS_FILE"' EXIT

# Source shared OAuth stub helper
source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"

# Ensure OAuth stub is running (starts it via make target if needed)
cd "$PROJECT_ROOT"
ensure_oauth_stub || exit 1

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

# Check if TMI server is running
echo "Checking TMI server..."
if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
    if [ "$START_SERVER" = "true" ]; then
        echo "[INFO] Starting development server..."
        cd "$PROJECT_ROOT"
        make dev-up
        sleep 5

        # Verify server started
        if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
            echo "[ERROR] Failed to start TMI server"
            exit 1
        fi
        echo "✅ Server started successfully"
    else
        echo "[ERROR] TMI server is not running on port 8080"
        echo ""
        echo "Options:"
        echo "  1. Start manually: make dev-up"
        echo "  2. Auto-start: make test-api START_SERVER=true"
        echo ""
        exit 1
    fi
fi

# First run unauthorized (401) tests before any authentication
echo ""
echo "🔒 Running unauthorized (401) tests first..."
UNAUTHORIZED_COLLECTION="$SCRIPT_DIR/unauthorized-tests-collection.json"
UNAUTHORIZED_OUTPUT="$OUTPUT_DIR/unauthorized-results-$TIMESTAMP.json"

if [ -f "$UNAUTHORIZED_COLLECTION" ]; then
    echo "Running unauthorized tests without authentication..."
    newman run "$UNAUTHORIZED_COLLECTION" \
            --globals "$FACTORY_GLOBALS_FILE" \
            --env-var "baseUrl=http://127.0.0.1:8080" \
            --reporters cli,json \
            --reporter-json-export "$UNAUTHORIZED_OUTPUT" \
            --timeout-request 10000 \
            --delay-request 200 \
            --ignore-redirects
    
    UNAUTHORIZED_EXIT_CODE=$?
    if [ $UNAUTHORIZED_EXIT_CODE -eq 0 ]; then
        echo "✅ Unauthorized tests completed successfully"
    else
        echo "❌ Unauthorized tests failed (exit code: $UNAUTHORIZED_EXIT_CODE)"
        echo "Continuing with authenticated tests..."
    fi
else
    echo "⚠️ Unauthorized test collection not found at $UNAUTHORIZED_COLLECTION"
fi

# Pre-authenticate test users and store JWT tokens
echo ""
echo "🔑 Pre-authenticating test users..."

# authenticate_user comes from lib/auth.sh (validates cached tokens against the server)
source "${SCRIPT_DIR}/lib/auth.sh"

# Authenticate all test users
TOKEN_ALICE=$(authenticate_user "alice")
TOKEN_BOB=$(authenticate_user "bob") 
TOKEN_CHARLIE=$(authenticate_user "charlie")
TOKEN_DIANA=$(authenticate_user "diana")

# Verify we got all tokens
if [ -z "$TOKEN_ALICE" ] || [ -z "$TOKEN_BOB" ] || [ -z "$TOKEN_CHARLIE" ] || [ -z "$TOKEN_DIANA" ]; then
    echo "❌ Failed to authenticate all users"
    exit 1
fi

echo "✅ All users authenticated successfully"

# Run newman tests
echo ""
echo "Running Postman collection with newman..."
echo "User: postman-runner-$TIMESTAMP"
echo "Output: $OUTPUT_FILE"
echo "Log: $LOG_FILE"
echo ""

# Check if comprehensive collection exists, fallback to legacy
if [ ! -f "$COLLECTION_FILE" ]; then
    echo "⚠️ Comprehensive collection not found, using legacy collection"
    COLLECTION_FILE="$SCRIPT_DIR/legacy/tmi-postman-collection.json"
fi

# Copy test data factory and auth helper to make them available 
echo "📁 Setting up test utilities..."
cp "$SCRIPT_DIR/test-data-factory.js" /tmp/ 2>/dev/null || echo "⚠️ test-data-factory.js not found"
cp "$SCRIPT_DIR/multi-user-auth.js" /tmp/ 2>/dev/null || echo "⚠️ multi-user-auth.js not found"

newman run "$COLLECTION_FILE" \
    --globals "$FACTORY_GLOBALS_FILE" \
    --env-var "loginHint=test-runner-$TIMESTAMP" \
    --env-var "baseUrl=http://127.0.0.1:8080" \
    --env-var "oauthStubUrl=http://127.0.0.1:8079" \
    --env-var "token_alice=$TOKEN_ALICE" \
    --env-var "token_bob=$TOKEN_BOB" \
    --env-var "token_charlie=$TOKEN_CHARLIE" \
    --env-var "token_diana=$TOKEN_DIANA" \
    --env-var "RESPONSE_TIME_MULTIPLIER=${RESPONSE_TIME_MULTIPLIER:-1}" \
    --reporters cli,json \
    --reporter-json-export "$OUTPUT_FILE" \
    --timeout-request 10000 \
    --delay-request 200 \
    --ignore-redirects \
    2>&1 | tee -a "$LOG_FILE"

# Capture exit code
TEST_EXIT_CODE=${PIPESTATUS[0]}

# Run new collections if they exist
echo ""
echo "🔄 Running additional test collections..."
NEW_COLLECTIONS=(
    "discovery-complete-tests-collection.json"
    "oauth-complete-flow-collection.json"
    "document-crud-tests-collection.json" 
    "source-crud-tests-collection.json"
    "complete-metadata-tests-collection.json"
    "advanced-error-scenarios-collection.json"
)

for collection in "${NEW_COLLECTIONS[@]}"; do
    if [ -f "$SCRIPT_DIR/$collection" ]; then
        echo "Running $collection..."
        COLLECTION_OUTPUT="$OUTPUT_DIR/$(basename "$collection" .json)-results-$TIMESTAMP.json"
        
        newman run "$SCRIPT_DIR/$collection" \
            --globals "$FACTORY_GLOBALS_FILE" \
            --env-var "baseUrl=http://127.0.0.1:8080" \
            --env-var "oauthStubUrl=http://127.0.0.1:8079" \
            --env-var "token_alice=$TOKEN_ALICE" \
            --env-var "token_bob=$TOKEN_BOB" \
            --env-var "token_charlie=$TOKEN_CHARLIE" \
            --env-var "token_diana=$TOKEN_DIANA" \
            --env-var "RESPONSE_TIME_MULTIPLIER=${RESPONSE_TIME_MULTIPLIER:-1}" \
            --reporters cli,json \
            --reporter-json-export "$COLLECTION_OUTPUT" \
            --timeout-request 10000 \
            --delay-request 200 \
            --ignore-redirects \
            2>&1 | tee -a "$LOG_FILE"
        
        # Update exit code if any collection fails
        COLLECTION_EXIT_CODE=${PIPESTATUS[0]}
        if [ $COLLECTION_EXIT_CODE -ne 0 ]; then
            TEST_EXIT_CODE=$COLLECTION_EXIT_CODE
        fi
    else
        echo "⚠️ Collection not found: $collection"
    fi
done

echo ""
echo "=== Test Summary ==="

# Parse unauthorized test results first
if command -v jq &> /dev/null && [ -f "$UNAUTHORIZED_OUTPUT" ]; then
    echo "🔒 Unauthorized Tests (401):"
    UNAUTH_REQUESTS=$(jq '.run.stats.requests.total' "$UNAUTHORIZED_OUTPUT")
    UNAUTH_FAILED_REQUESTS=$(jq '.run.stats.requests.failed' "$UNAUTHORIZED_OUTPUT") 
    UNAUTH_ASSERTIONS=$(jq '.run.stats.assertions.total' "$UNAUTHORIZED_OUTPUT")
    UNAUTH_FAILED_ASSERTIONS=$(jq '.run.stats.assertions.failed' "$UNAUTHORIZED_OUTPUT")
    echo "   Requests: $UNAUTH_REQUESTS, Failed: $UNAUTH_FAILED_REQUESTS"
    echo "   Assertions: $UNAUTH_ASSERTIONS, Failed: $UNAUTH_FAILED_ASSERTIONS"
    echo ""
fi

# Parse authenticated test results
if command -v jq &> /dev/null && [ -f "$OUTPUT_FILE" ]; then
    echo "🔑 Authenticated Tests:"
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
echo "   Log: $LOG_FILE"
echo ""

# Exit with newman's exit code
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✅ All tests passed!"
else
    echo "❌ Some tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE