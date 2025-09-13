#!/bin/bash

# Test script to reproduce threat metadata issue
set -e

echo "Testing threat metadata operations..."

# First, start the dev environment if not already running
echo "Starting dev environment..."
make dev-start > /dev/null 2>&1 || echo "Dev environment already running"

# Wait for server to be ready
sleep 3

# Get auth token
echo "Getting auth token..."
AUTH_RESPONSE=$(make test-api auth=only 2>/dev/null | grep -E '"access_token":|"expires_in":' | head -2)
ACCESS_TOKEN=$(echo "$AUTH_RESPONSE" | grep '"access_token"' | sed 's/.*"access_token": "\([^"]*\)".*/\1/')

if [ -z "$ACCESS_TOKEN" ]; then
    echo "Failed to get access token"
    exit 1
fi

echo "Got access token: ${ACCESS_TOKEN:0:20}..."

# Create a threat model
echo "Creating threat model..."
TM_RESPONSE=$(curl -s -X POST "http://localhost:8080/threat_models" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Test Threat Model", "description": "Test for metadata"}')

TM_ID=$(echo "$TM_RESPONSE" | jq -r '.id')
echo "Created threat model: $TM_ID"

# Create a threat
echo "Creating threat..."
THREAT_RESPONSE=$(curl -s -X POST "http://localhost:8080/threat_models/$TM_ID/threats" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Test Threat", "threat_type": "Spoofing", "priority": "High", "severity": "High", "score": 10, "status": "Open", "mitigated": false}')

THREAT_ID=$(echo "$THREAT_RESPONSE" | jq -r '.id')
echo "Created threat: $THREAT_ID"

# Add metadata to the threat
echo "Adding metadata to threat..."
METADATA_RESPONSE=$(curl -s -X POST "http://localhost:8080/threat_models/$TM_ID/threats/$THREAT_ID/metadata/bulk" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '[{"key": "test", "value": "testval"}, {"key": "test-threat-metadata-key", "value": "test-threat-metadata-value"}]')

echo "Metadata creation response:"
echo "$METADATA_RESPONSE" | jq '.'

# Check if metadata was created by querying the metadata endpoint directly
echo "Checking metadata via direct endpoint..."
DIRECT_METADATA=$(curl -s -X GET "http://localhost:8080/threat_models/$TM_ID/threats/$THREAT_ID/metadata" \
  -H "Authorization: Bearer $ACCESS_TOKEN")

echo "Direct metadata query result:"
echo "$DIRECT_METADATA" | jq '.'

# Now get the threat model and check if threats have metadata
echo "Getting threat model..."
TM_RETRIEVED=$(curl -s -X GET "http://localhost:8080/threat_models/$TM_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN")

echo "Threat model retrieved:"
echo "$TM_RETRIEVED" | jq '.threats[0].metadata'

# Clean up
echo "Cleaning up..."
curl -s -X DELETE "http://localhost:8080/threat_models/$TM_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN" > /dev/null

echo "Test completed."