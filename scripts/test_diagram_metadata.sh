#!/bin/bash

# Test diagram metadata functionality

SERVER_URL="http://localhost:8080"
AUTH_TOKEN=""

echo "=== Testing Diagram Metadata Loading ==="

# First, get an auth token using the test OAuth provider with a random user
FIXED_USER="test$(date +%y%m%d%H%M%S)"
echo "Getting auth token for user: $FIXED_USER"
# Generate new OAuth token by visiting the OAuth authorize endpoint
curl -sL "http://localhost:8080/oauth2/authorize?idp=test&login_hint=$FIXED_USER&client_callback=http://localhost:8079/&scope=openid" > /dev/null
sleep 2
OAUTH_RESPONSE=$(curl -s "http://localhost:8079/creds?userid=$FIXED_USER")
AUTH_TOKEN=$(echo "$OAUTH_RESPONSE" | jq -r '.access_token')
if [ "$AUTH_TOKEN" = "null" ] || [ -z "$AUTH_TOKEN" ]; then
    echo "❌ Failed to get auth token"
    echo "Response: $OAUTH_RESPONSE"
    exit 1
fi
echo "✅ Got auth token: ${AUTH_TOKEN:0:20}..."

# Create a threat model first
echo "Creating threat model..."
THREAT_MODEL_RESPONSE=$(curl -s -X POST "${SERVER_URL}/threat_models" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name": "Test Threat Model", "description": "For testing diagram metadata"}')

THREAT_MODEL_ID=$(echo "$THREAT_MODEL_RESPONSE" | jq -r '.id')
if [ "$THREAT_MODEL_ID" = "null" ] || [ -z "$THREAT_MODEL_ID" ]; then
    echo "❌ Failed to create threat model"
    echo "Response: $THREAT_MODEL_RESPONSE"
    exit 1
fi
echo "✅ Created threat model: $THREAT_MODEL_ID"

# Create a diagram
echo "Creating diagram..."
DIAGRAM_RESPONSE=$(curl -s -X POST "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"name": "Test Diagram", "type": "DFD-1.0.0"}')

DIAGRAM_ID=$(echo "$DIAGRAM_RESPONSE" | jq -r '.id')
if [ "$DIAGRAM_ID" = "null" ] || [ -z "$DIAGRAM_ID" ]; then
    echo "❌ Failed to create diagram"
    echo "Response: $DIAGRAM_RESPONSE"
    exit 1
fi
echo "✅ Created diagram: $DIAGRAM_ID"

# Add metadata to the diagram
echo "Adding metadata to diagram..."
METADATA_RESPONSE=$(curl -s -X POST "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}/metadata" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"key": "creator", "value": "Test Creator"}')

if echo "$METADATA_RESPONSE" | jq -e '.key' > /dev/null 2>&1; then
    echo "✅ Added metadata: $(echo "$METADATA_RESPONSE" | jq -c '.')"
    
    # Wait for database persistence
    echo "Waiting for database persistence..."
    sleep 3
    
    # Verify in database using Docker
    echo "Checking database for metadata entry..."
    DB_CHECK=$(docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -t -c "SELECT key, value FROM metadata WHERE entity_type = 'diagram' AND entity_id = '$DIAGRAM_ID' AND key = 'creator';")
    if [[ "$DB_CHECK" =~ creator.*Test\ Creator ]]; then
        echo "✅ Metadata confirmed in database: $DB_CHECK"
    else
        echo "❌ Metadata NOT found in database!"
        echo "Database query result: '$DB_CHECK'"
    fi
else
    echo "❌ Failed to add metadata"
    echo "Response: $METADATA_RESPONSE"
    exit 1
fi

# Add another metadata entry
echo "Adding second metadata entry..."
METADATA_RESPONSE2=$(curl -s -X POST "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}/metadata" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"key": "tool", "value": "TMI"}')

if echo "$METADATA_RESPONSE2" | jq -e '.key' > /dev/null 2>&1; then
    echo "✅ Added second metadata: $(echo "$METADATA_RESPONSE2" | jq -c '.')"
    
    # Wait for database persistence
    echo "Waiting for database persistence..."
    sleep 3
    
    # Verify second metadata in database using Docker
    echo "Checking database for second metadata entry..."
    DB_CHECK2=$(docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -t -c "SELECT key, value FROM metadata WHERE entity_type = 'diagram' AND entity_id = '$DIAGRAM_ID' AND key = 'tool';")
    if [[ "$DB_CHECK2" =~ tool.*TMI ]]; then
        echo "✅ Second metadata confirmed in database: $DB_CHECK2"
    else
        echo "❌ Second metadata NOT found in database!"
        echo "Database query result: '$DB_CHECK2'"
    fi
else
    echo "❌ Failed to add second metadata"
    echo "Response: $METADATA_RESPONSE2"
    exit 1
fi

# Check what diagrams endpoint is available
echo "Checking available diagram endpoints..."
echo "Trying nested endpoint: /threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}"
DIAGRAM_GET_RESPONSE=$(curl -s "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}" \
  -H "Authorization: Bearer ${AUTH_TOKEN}")

# Also check database directly to see what loadDiagramMetadata should return
echo ""
echo "Direct database check - what loadDiagramMetadata() should find:"
ALL_METADATA=$(docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -t -c "SELECT key, value FROM metadata WHERE entity_type = 'diagram' AND entity_id = '$DIAGRAM_ID' ORDER BY key;")
echo "All metadata for diagram $DIAGRAM_ID:"
echo "$ALL_METADATA"

echo "Diagram response:"
echo "$DIAGRAM_GET_RESPONSE" | jq '.'

# Check if metadata field exists and has content
METADATA_FIELD=$(echo "$DIAGRAM_GET_RESPONSE" | jq '.metadata')
if [ "$METADATA_FIELD" = "null" ] || [ "$METADATA_FIELD" = "[]" ]; then
    echo ""
    echo "❌ PROBLEM: Diagram metadata field is empty or null!"
    echo "Expected: metadata with creator and tool keys"
    echo "Actual: $METADATA_FIELD"
    
    # Let's also check the metadata directly
    echo ""
    echo "Checking metadata directly via API..."
    DIRECT_METADATA=$(curl -s "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}/metadata" \
      -H "Authorization: Bearer ${AUTH_TOKEN}")
    echo "Direct metadata API response:"
    echo "$DIRECT_METADATA" | jq '.'
    
else
    echo ""
    echo "✅ SUCCESS: Diagram has metadata:"
    echo "$METADATA_FIELD" | jq '.'
fi

# Clean up
echo ""
echo "Cleaning up..."
curl -s -X DELETE "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" > /dev/null
echo "✅ Cleaned up test data"