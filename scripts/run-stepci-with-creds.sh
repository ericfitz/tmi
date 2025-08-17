#!/bin/bash

# run-stepci-with-creds.sh
# Helper script to run StepCI tests with pre-generated credential files

set -e

# Check if credential files exist
if [[ ! -f "tmp/alice.json" || ! -f "tmp/bob.json" || ! -f "tmp/chuck.json" ]]; then
    echo "❌ Credential files not found. Please run 'make stepci-prep' first."
    echo "   Expected files: tmp/alice.json, tmp/bob.json, tmp/chuck.json"
    exit 1
fi

# Extract tokens from credential files
echo "🔐 Loading credentials from tmp/ directory..."
export ALICE_TOKEN=$(jq -r '.access_token' tmp/alice.json)
export BOB_TOKEN=$(jq -r '.access_token' tmp/bob.json)
export CHUCK_TOKEN=$(jq -r '.access_token' tmp/chuck.json)

# Verify tokens were extracted
if [[ "$ALICE_TOKEN" == "null" || "$BOB_TOKEN" == "null" || "$CHUCK_TOKEN" == "null" ]]; then
    echo "❌ Failed to extract access tokens from credential files"
    echo "   Alice token: ${ALICE_TOKEN:0:20}..."
    echo "   Bob token: ${BOB_TOKEN:0:20}..."
    echo "   Chuck token: ${CHUCK_TOKEN:0:20}..."
    exit 1
fi

echo "✅ Successfully loaded credentials:"
echo "   Alice: ${ALICE_TOKEN:0:20}..."
echo "   Bob: ${BOB_TOKEN:0:20}..."
echo "   Chuck: ${CHUCK_TOKEN:0:20}..."

# Run StepCI with the provided arguments
echo "🚀 Running StepCI tests with credentials..."
exec stepci "$@"