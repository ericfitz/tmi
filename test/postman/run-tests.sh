#!/bin/bash

# Run comprehensive TMI API test suite with OAuth authentication
# Requires: newman, jq, TMI server running on 8080

set -e

# Setup cleanup trap
cleanup() {
    echo "🧹 Cleaning up..."
    cd "$PROJECT_ROOT" 2>/dev/null || true
    if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]" 2>/dev/null; then
        make stop-oauth-stub 2>/dev/null || true
        sleep 2  # Brief wait for cleanup
    fi
}
trap cleanup EXIT INT TERM

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"
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

# Check current OAuth stub status first
echo "Checking current OAuth stub status..."
cd "$PROJECT_ROOT"
if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
    echo "✅ OAuth stub is already running and healthy, keeping it..."
    # Test that it's actually responding
    if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
        echo "✅ OAuth stub is responding correctly, no restart needed"
        OAUTH_STUB_ALREADY_RUNNING=true
    else
        echo "⚠️ OAuth stub is running but not responding, restarting..."
        make stop-oauth-stub 2>/dev/null || true
        sleep 5  # Wait for graceful shutdown
        OAUTH_STUB_ALREADY_RUNNING=false
    fi
elif make check-oauth-stub 2>&1 | grep -q "\[WARNING\].*running"; then
    echo "OAuth stub is running without PID file, stopping it..."
    make stop-oauth-stub 2>/dev/null || true
    sleep 5  # Wait for graceful shutdown
    
    # If still running, force kill
    if make check-oauth-stub 2>&1 | grep -q "\[WARNING\].*running"; then
        echo "OAuth stub still running, force killing..."
        make kill-oauth-stub || true
        sleep 5  # Wait for force kill to complete
    fi
    OAUTH_STUB_ALREADY_RUNNING=false
else
    echo "OAuth stub is not running, proceeding to start..."
    OAUTH_STUB_ALREADY_RUNNING=false
fi

# Start OAuth stub only if needed
if [ "$OAUTH_STUB_ALREADY_RUNNING" != "true" ]; then
    echo "Starting OAuth stub..."
    cd "$PROJECT_ROOT"
    if make start-oauth-stub; then
        sleep 5  # Wait for startup to complete
        echo "✅ OAuth stub start command completed"
    else
        echo "❌ OAuth stub start command failed"
        echo -e "$(RED)[ERROR]$(NC) Failed to start OAuth stub"
        exit 1
    fi
else
    echo "✅ Using existing OAuth stub (preserving any stored credentials)"
fi

# Verify stub is running using make target
echo "Verifying OAuth stub status..."
if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
    echo "✅ OAuth stub is ready"
else
    echo "❌ OAuth stub failed to start properly"
    echo "Status check result:"
    make check-oauth-stub
    exit 1
fi

# Check if htmlextra reporter is available
echo "Checking Newman HTML reporter..."
if npm list -g newman-reporter-htmlextra >/dev/null 2>&1; then
    echo "✅ newman-reporter-htmlextra is available"
    HTML_REPORTER="htmlextra"
elif command -v newman-reporter-htmlextra >/dev/null 2>&1; then
    echo "✅ newman-reporter-htmlextra is available"
    HTML_REPORTER="htmlextra"
else
    echo "⚠️ newman-reporter-htmlextra not found, HTML reports will be disabled"
    echo "   To install: pnpm install -g newman-reporter-htmlextra"
    HTML_REPORTER=""
fi

# Check if TMI server is running
echo "Checking TMI server..."
if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
    echo "ERROR: TMI server is not running on port 8080"
    echo "Please run: make start-dev"
    exit 1
fi

# First run unauthorized (401) tests before any authentication
echo ""
echo "🔒 Running unauthorized (401) tests first..."
UNAUTHORIZED_COLLECTION="$SCRIPT_DIR/unauthorized-tests-collection.json"
UNAUTHORIZED_OUTPUT="$OUTPUT_DIR/unauthorized-results-$TIMESTAMP.json"

if [ -f "$UNAUTHORIZED_COLLECTION" ]; then
    echo "Running unauthorized tests without authentication..."
    if [ ! -z "$HTML_REPORTER" ]; then
        newman run "$UNAUTHORIZED_COLLECTION" \
            --env-var "baseUrl=http://127.0.0.1:8080" \
            --reporters cli,json,htmlextra \
            --reporter-json-export "$UNAUTHORIZED_OUTPUT" \
            --reporter-htmlextra-export "$OUTPUT_DIR/unauthorized-report-$TIMESTAMP.html" \
            --timeout-request 10000 \
            --delay-request 200 \
            --ignore-redirects
    else
        newman run "$UNAUTHORIZED_COLLECTION" \
            --env-var "baseUrl=http://127.0.0.1:8080" \
            --reporters cli,json \
            --reporter-json-export "$UNAUTHORIZED_OUTPUT" \
            --timeout-request 10000 \
            --delay-request 200 \
            --ignore-redirects
    fi
    
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

# Function to authenticate a user and extract JWT token using PKCE flow
authenticate_user() {
    local username="$1"
    echo "Checking existing token for $username..." >&2

    # First, check if we already have a valid token in the OAuth stub
    local existing_token_response=$(curl -s "http://127.0.0.1:8079/creds?userid=$username" 2>/dev/null)
    local existing_token=$(echo "$existing_token_response" | jq -r '.access_token' 2>/dev/null)

    # Check if token exists and is valid (basic validation - not expired)
    if [ "$existing_token" != "null" ] && [ "$existing_token" != "" ] && [ "$existing_token" != "undefined" ]; then
        # Basic JWT token validation (check if it has 3 parts)
        local token_parts_count=$(echo "$existing_token" | tr -cd '.' | wc -c)
        if [ "$token_parts_count" -eq 2 ]; then
            echo "✅ Using existing cached token for $username" >&2
            printf "%s" "$existing_token"
            return 0
        fi
    fi

    echo "🔄 No valid cached token found, authenticating $username..." >&2

    # Use OAuth stub's automated e2e flow which handles PKCE
    local flow_response=$(curl -s -X POST "http://127.0.0.1:8079/flows/start" \
        -H "Content-Type: application/json" \
        -d "{\"userid\": \"$username\"}")
    local flow_id=$(echo "$flow_response" | jq -r '.flow_id' 2>/dev/null)

    if [ "$flow_id" == "null" ] || [ -z "$flow_id" ]; then
        echo "❌ Failed to start OAuth flow for $username" >&2
        echo "Response: $flow_response" >&2
        return 1
    fi

    # Poll for flow completion (max 10 seconds)
    for i in 1 2 3 4 5 6 7 8 9 10; do
        local status_response=$(curl -s "http://127.0.0.1:8079/flows/$flow_id")
        local status=$(echo "$status_response" | jq -r '.status' 2>/dev/null)
        local tokens_ready=$(echo "$status_response" | jq -r '.tokens_ready' 2>/dev/null)

        if [ "$tokens_ready" == "true" ]; then
            local token=$(echo "$status_response" | jq -r '.tokens.access_token' 2>/dev/null)
            if [ "$token" != "null" ] && [ -n "$token" ]; then
                echo "✅ Token retrieved for $username" >&2
                printf "%s" "$token"
                return 0
            fi
        fi

        if [ "$status" == "failed" ]; then
            local error=$(echo "$status_response" | jq -r '.error' 2>/dev/null)
            echo "❌ OAuth flow failed for $username: $error" >&2
            return 1
        fi

        sleep 1
    done

    echo "❌ Timeout waiting for OAuth flow completion for $username" >&2
    return 1
}

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

# Run newman with conditional HTML reporter
if [ ! -z "$HTML_REPORTER" ]; then
    echo "Running with HTML report generation..."
    newman run "$COLLECTION_FILE" \
        --env-var "loginHint=test-runner-$TIMESTAMP" \
        --env-var "baseUrl=http://127.0.0.1:8080" \
        --env-var "oauthStubUrl=http://127.0.0.1:8079" \
        --env-var "token_alice=$TOKEN_ALICE" \
        --env-var "token_bob=$TOKEN_BOB" \
        --env-var "token_charlie=$TOKEN_CHARLIE" \
        --env-var "token_diana=$TOKEN_DIANA" \
        --reporters cli,json,htmlextra \
        --reporter-json-export "$OUTPUT_FILE" \
        --reporter-htmlextra-export "$OUTPUT_DIR/test-report-$TIMESTAMP.html" \
        --timeout-request 10000 \
        --delay-request 200 \
        --ignore-redirects \
        2>&1 | tee -a "$LOG_FILE"
else
    echo "Running without HTML report (htmlextra not available)..."
    newman run "$COLLECTION_FILE" \
        --env-var "loginHint=test-runner-$TIMESTAMP" \
        --env-var "baseUrl=http://127.0.0.1:8080" \
        --env-var "oauthStubUrl=http://127.0.0.1:8079" \
        --env-var "token_alice=$TOKEN_ALICE" \
        --env-var "token_bob=$TOKEN_BOB" \
        --env-var "token_charlie=$TOKEN_CHARLIE" \
        --env-var "token_diana=$TOKEN_DIANA" \
        --reporters cli,json \
        --reporter-json-export "$OUTPUT_FILE" \
        --timeout-request 10000 \
        --delay-request 200 \
        --ignore-redirects \
        2>&1 | tee -a "$LOG_FILE"
fi

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
        
        if [ ! -z "$HTML_REPORTER" ]; then
            newman run "$SCRIPT_DIR/$collection" \
                --env-var "baseUrl=http://127.0.0.1:8080" \
                --env-var "oauthStubUrl=http://127.0.0.1:8079" \
                --env-var "token_alice=$TOKEN_ALICE" \
                --env-var "token_bob=$TOKEN_BOB" \
                --env-var "token_charlie=$TOKEN_CHARLIE" \
                --env-var "token_diana=$TOKEN_DIANA" \
                --reporters cli,json,htmlextra \
                --reporter-json-export "$COLLECTION_OUTPUT" \
                --reporter-htmlextra-export "$OUTPUT_DIR/$(basename "$collection" .json)-report-$TIMESTAMP.html" \
                --timeout-request 10000 \
                --delay-request 200 \
                --ignore-redirects \
                2>&1 | tee -a "$LOG_FILE"
        else
            newman run "$SCRIPT_DIR/$collection" \
                --env-var "baseUrl=http://127.0.0.1:8080" \
                --env-var "oauthStubUrl=http://127.0.0.1:8079" \
                --env-var "token_alice=$TOKEN_ALICE" \
                --env-var "token_bob=$TOKEN_BOB" \
                --env-var "token_charlie=$TOKEN_CHARLIE" \
                --env-var "token_diana=$TOKEN_DIANA" \
                --reporters cli,json \
                --reporter-json-export "$COLLECTION_OUTPUT" \
                --timeout-request 10000 \
                --delay-request 200 \
                --ignore-redirects \
                2>&1 | tee -a "$LOG_FILE"
        fi
        
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
HTML_REPORT="$OUTPUT_DIR/test-report-$TIMESTAMP.html"
if [ -f "$HTML_REPORT" ]; then
    echo "   HTML: $HTML_REPORT"
else
    echo "   HTML: ❌ Report not generated (check newman-reporter-htmlextra installation)"
fi
echo "   Log: $LOG_FILE"
echo ""

# Note: OAuth stub cleanup is handled by the cleanup trap

# Exit with newman's exit code
if [ $TEST_EXIT_CODE -eq 0 ]; then
    echo "✅ All tests passed!"
else
    echo "❌ Some tests failed (exit code: $TEST_EXIT_CODE)"
fi

exit $TEST_EXIT_CODE