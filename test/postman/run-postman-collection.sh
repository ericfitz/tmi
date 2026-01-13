#!/bin/bash

# Run a single Postman collection with PKCE-based OAuth authentication
# Usage: ./run-postman-collection.sh <collection-name>
# Example: ./run-postman-collection.sh advanced-error-scenarios-collection

set -e

COLLECTION_NAME="$1"
if [ -z "$COLLECTION_NAME" ]; then
    echo "ERROR: Collection name required"
    echo "Usage: $0 <collection-name>"
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"
OUTPUT_DIR="$SCRIPT_DIR/test-results"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
COLLECTION_FILE="$SCRIPT_DIR/${COLLECTION_NAME}.json"

if [ ! -f "$COLLECTION_FILE" ]; then
    echo "ERROR: Collection not found: $COLLECTION_FILE"
    echo "Available collections:"
    ls -1 "$SCRIPT_DIR"/*.json 2>/dev/null | xargs -I {} basename {} .json | sed 's/^/  /'
    exit 1
fi

# Setup cleanup trap
cleanup() {
    echo "🧹 Cleaning up..."
    cd "$PROJECT_ROOT" 2>/dev/null || true
    if make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]" 2>/dev/null; then
        make stop-oauth-stub 2>/dev/null || true
        sleep 2
    fi
}
trap cleanup EXIT INT TERM

echo "=== Running Postman Collection: $COLLECTION_NAME ==="
echo "Timestamp: $TIMESTAMP"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Start OAuth stub
cd "$PROJECT_ROOT"
echo "Starting OAuth stub..."
if ! make check-oauth-stub 2>&1 | grep -q "\[SUCCESS\]"; then
    make start-oauth-stub
    sleep 3
fi

# Verify OAuth stub is running
if ! curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
    echo "ERROR: OAuth stub is not responding"
    exit 1
fi
echo "✅ OAuth stub is ready"

# Check if TMI server is running
if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
    echo "ERROR: TMI server is not running on port 8080"
    echo "Please run: make start-dev"
    exit 1
fi
echo "✅ TMI server is ready"

# Function to authenticate a user using PKCE flow via OAuth stub
authenticate_user() {
    local username="$1"
    echo "Authenticating $username..." >&2

    # Check for existing cached token
    local existing_token=$(curl -s "http://127.0.0.1:8079/creds?userid=$username" 2>/dev/null | jq -r '.access_token' 2>/dev/null)
    if [ "$existing_token" != "null" ] && [ -n "$existing_token" ] && [ "$existing_token" != "undefined" ]; then
        local token_parts=$(echo "$existing_token" | tr -cd '.' | wc -c)
        if [ "$token_parts" -eq 2 ]; then
            echo "✅ Using cached token for $username" >&2
            printf "%s" "$existing_token"
            return 0
        fi
    fi

    # Use OAuth stub's e2e flow (handles PKCE automatically)
    local flow_response=$(curl -s -X POST "http://127.0.0.1:8079/flows/start" \
        -H "Content-Type: application/json" \
        -d "{\"userid\": \"$username\"}")
    local flow_id=$(echo "$flow_response" | jq -r '.flow_id' 2>/dev/null)

    if [ "$flow_id" == "null" ] || [ -z "$flow_id" ]; then
        echo "❌ Failed to start OAuth flow for $username" >&2
        echo "Response: $flow_response" >&2
        return 1
    fi

    # Poll for completion (max 15 seconds)
    for i in $(seq 1 15); do
        local status_response=$(curl -s "http://127.0.0.1:8079/flows/$flow_id")
        local tokens_ready=$(echo "$status_response" | jq -r '.tokens_ready' 2>/dev/null)

        if [ "$tokens_ready" == "true" ]; then
            local token=$(echo "$status_response" | jq -r '.tokens.access_token' 2>/dev/null)
            if [ "$token" != "null" ] && [ -n "$token" ]; then
                echo "✅ Token obtained for $username" >&2
                printf "%s" "$token"
                return 0
            fi
        fi

        local status=$(echo "$status_response" | jq -r '.status' 2>/dev/null)
        if [ "$status" == "failed" ] || [ "$status" == "error" ]; then
            echo "❌ OAuth flow failed for $username" >&2
            return 1
        fi

        sleep 1
    done

    echo "❌ Timeout waiting for OAuth flow for $username" >&2
    return 1
}

# Authenticate test users
echo ""
echo "🔑 Authenticating test users..."
TOKEN_ALICE=$(authenticate_user "alice")
TOKEN_BOB=$(authenticate_user "bob")
TOKEN_CHARLIE=$(authenticate_user "charlie")
TOKEN_DIANA=$(authenticate_user "diana")

# Verify we got the primary tokens (alice and bob are required, others optional)
if [ -z "$TOKEN_ALICE" ] || [ -z "$TOKEN_BOB" ]; then
    echo "❌ Failed to authenticate required users (alice, bob)"
    exit 1
fi
echo "✅ Users authenticated"

# Run newman
echo ""
echo "🧪 Running collection: $COLLECTION_NAME"
RESULT_FILE="$OUTPUT_DIR/${COLLECTION_NAME}-results-$TIMESTAMP.json"
REPORT_FILE="$OUTPUT_DIR/${COLLECTION_NAME}-report-$TIMESTAMP.html"

# Check for HTML reporter
REPORTERS="cli,json"
EXTRA_ARGS=""
if npm list -g newman-reporter-htmlextra >/dev/null 2>&1; then
    REPORTERS="cli,json,htmlextra"
    EXTRA_ARGS="--reporter-htmlextra-export $REPORT_FILE"
fi

newman run "$COLLECTION_FILE" \
    --env-var "baseUrl=http://127.0.0.1:8080" \
    --env-var "oauthStubUrl=http://127.0.0.1:8079" \
    --env-var "token_alice=$TOKEN_ALICE" \
    --env-var "token_bob=$TOKEN_BOB" \
    --env-var "token_charlie=$TOKEN_CHARLIE" \
    --env-var "token_diana=$TOKEN_DIANA" \
    --reporters "$REPORTERS" \
    --reporter-json-export "$RESULT_FILE" \
    $EXTRA_ARGS \
    --timeout-request 10000 \
    --delay-request 200 \
    --ignore-redirects

TEST_EXIT_CODE=$?

echo ""
echo "=== Results ==="
echo "JSON: $RESULT_FILE"
if [ -f "$REPORT_FILE" ]; then
    echo "HTML: $REPORT_FILE"
fi

exit $TEST_EXIT_CODE
