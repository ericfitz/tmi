#!/bin/bash

# Test diagram PATCH functionality

SERVER_URL="http://localhost:8080"
AUTH_TOKEN=""

echo "=== Testing Diagram PATCH Operation ==="

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
  -d '{"name": "Test Threat Model", "description": "For testing diagram PATCH"}')

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

# Show original diagram
echo "Original diagram:"
echo "$DIAGRAM_RESPONSE" | jq '.'

# Test PATCH operation - this should reproduce the 500 error
echo ""
echo "Testing PATCH operation..."
PATCH_RESPONSE=$(curl -s -X PATCH "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -H "Content-Type: application/json-patch+json" \
  -d '[{"op": "replace", "path": "/name", "value": "Patched Diagram Name"}]')

echo "PATCH response:"
echo "$PATCH_RESPONSE" | jq '.'

# Check if PATCH worked or failed
if echo "$PATCH_RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo ""
    echo "❌ PATCH failed with error:"
    echo "$PATCH_RESPONSE" | jq -r '.error_description'
    
    # Let's also try to get the diagram to see its current state
    echo ""
    echo "Getting current diagram state..."
    CURRENT_DIAGRAM=$(curl -s "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}/diagrams/${DIAGRAM_ID}" \
      -H "Authorization: Bearer ${AUTH_TOKEN}")
    echo "Current diagram:"
    echo "$CURRENT_DIAGRAM" | jq '.'
    
else
    echo ""
    echo "✅ PATCH succeeded:"
    echo "$PATCH_RESPONSE" | jq '.'
    
    # Verify the name was actually changed
    UPDATED_NAME=$(echo "$PATCH_RESPONSE" | jq -r '.name')
    if [ "$UPDATED_NAME" = "Patched Diagram Name" ]; then
        echo "✅ Name was successfully updated to: $UPDATED_NAME"
    else
        echo "❌ Name was not updated correctly. Expected: 'Patched Diagram Name', Got: '$UPDATED_NAME'"
    fi
fi

# Clean up
echo ""
echo "Cleaning up..."
curl -s -X DELETE "${SERVER_URL}/threat_models/${THREAT_MODEL_ID}" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" > /dev/null
echo "✅ Cleaned up test data"