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

# Source shared OAuth stub helper
source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"

echo "=== Running Postman Collection: $COLLECTION_NAME ==="
echo "Timestamp: $TIMESTAMP"

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Ensure OAuth stub is running (starts it via make target if needed)
cd "$PROJECT_ROOT"
ensure_oauth_stub || exit 1
echo "OAuth stub is ready"

# Check if TMI server is running
if ! curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
    echo "ERROR: TMI server is not running on port 8080"
    echo "Please run: make dev-up"
    exit 1
fi
echo "✅ TMI server is ready"

# Provide the real test data factory to every script in the run (see lib/factory-globals.sh)
source "${SCRIPT_DIR}/lib/factory-globals.sh"
FACTORY_GLOBALS_FILE="$(make_factory_globals_tmp "${SCRIPT_DIR}/test-data-factory.js")" || exit 1
trap 'rm -f "$FACTORY_GLOBALS_FILE"' EXIT

# authenticate_user comes from lib/auth.sh (validates cached tokens against the server)
source "${SCRIPT_DIR}/lib/auth.sh"

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

newman run "$COLLECTION_FILE" \
    --globals "$FACTORY_GLOBALS_FILE" \
    --env-var "baseUrl=http://127.0.0.1:8080" \
    --env-var "oauthStubUrl=http://127.0.0.1:8079" \
    --env-var "token_alice=$TOKEN_ALICE" \
    --env-var "token_bob=$TOKEN_BOB" \
    --env-var "token_charlie=$TOKEN_CHARLIE" \
    --env-var "token_diana=$TOKEN_DIANA" \
    --env-var "RESPONSE_TIME_MULTIPLIER=${RESPONSE_TIME_MULTIPLIER:-1}" \
    --reporters cli,json \
    --reporter-json-export "$RESULT_FILE" \
    --timeout-request 10000 \
    --delay-request 200 \
    --ignore-redirects

TEST_EXIT_CODE=$?

echo ""
echo "=== Results ==="
echo "JSON: $RESULT_FILE"

exit $TEST_EXIT_CODE
