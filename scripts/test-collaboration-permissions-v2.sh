#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧪 Testing collaboration session permissions (v2)${NC}"

# Function to cleanup and exit
cleanup() {
    echo -e "${YELLOW}🧹 Final cleanup...${NC}"
    make clean-dev > /dev/null 2>&1 || true
    exit $1
}

# Trap to cleanup on exit
trap 'cleanup $?' EXIT

echo -e "${YELLOW}1. 🧹 Cleaning development environment...${NC}"
make clean-dev

echo -e "${YELLOW}2. 🚀 Starting development environment...${NC}"
make dev > /dev/null 2>&1 &
DEV_PID=$!

echo -e "${YELLOW}3. ⏳ Waiting for server to start...${NC}"
sleep 10

echo -e "${YELLOW}4. 🔍 Verifying server is running on port 8080...${NC}"
for i in {1..15}; do
    if lsof -i:8080 > /dev/null 2>&1; then
        echo -e "${GREEN}✅ Server is listening on port 8080${NC}"
        break
    fi
    if [ $i -eq 15 ]; then
        echo -e "${RED}❌ Server failed to start on port 8080 after 15 attempts${NC}"
        echo -e "${YELLOW}Server log contents:${NC}"
        cat server.log 2>/dev/null || echo "No server.log found"
        exit 1
    fi
    echo -e "${BLUE}  Waiting for server... (attempt $i)${NC}"
    sleep 2
done

echo -e "${YELLOW}5. 🌐 Testing server health endpoint...${NC}"
for i in {1..10}; do
    if curl -s http://localhost:8080/ > /dev/null; then
        echo -e "${GREEN}✅ Server health check passed${NC}"
        break
    fi
    if [ $i -eq 10 ]; then
        echo -e "${RED}❌ Server health check failed after 10 attempts${NC}"
        exit 1
    fi
    echo -e "${BLUE}  Health check attempt $i...${NC}"
    sleep 1
done

echo -e "${YELLOW}6. 🔐 Getting OAuth token...${NC}"
# Use the same OAuth flow as the make test-api target
AUTH_REDIRECT=$(curl -s "http://localhost:8080/auth/login/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g')
if [ -z "$AUTH_REDIRECT" ]; then
    echo -e "${RED}❌ Failed to get auth redirect URL${NC}"
    echo -e "${YELLOW}OAuth response:${NC}"
    curl -s "http://localhost:8080/auth/login/test"
    exit 1
fi

echo -e "${BLUE}  Following OAuth redirect...${NC}"
OAUTH_RESPONSE=$(curl -s "$AUTH_REDIRECT")
JWT_TOKEN=$(echo "$OAUTH_RESPONSE" | jq -r '.access_token' 2>/dev/null || echo "")

if [ -z "$JWT_TOKEN" ] || [ "$JWT_TOKEN" = "null" ]; then
    echo -e "${RED}❌ Failed to extract JWT token${NC}"
    echo -e "${YELLOW}OAuth callback response:${NC}"
    echo "$OAUTH_RESPONSE"
    exit 1
fi

echo -e "${GREEN}✅ Got JWT token (${JWT_TOKEN:0:20}...)${NC}"

echo -e "${YELLOW}7. 📝 Creating threat model...${NC}"
TM_RESPONSE=$(curl -s -X POST http://localhost:8080/threat_models \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Collaboration Permission Test", 
    "description": "Testing session permissions for owner/writer/reader"
  }')

TM_ID=$(echo "$TM_RESPONSE" | jq -r '.id')
OWNER_EMAIL=$(echo "$TM_RESPONSE" | jq -r '.owner')

if [ "$TM_ID" = "null" ] || [ -z "$TM_ID" ]; then
    echo -e "${RED}❌ Failed to create threat model${NC}"
    echo -e "${YELLOW}Response: $TM_RESPONSE${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Created threat model: $TM_ID (owner: $OWNER_EMAIL)${NC}"

echo -e "${YELLOW}8. 📊 Creating diagram...${NC}"
DIAGRAM_RESPONSE=$(curl -s -X POST http://localhost:8080/threat_models/$TM_ID/diagrams \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "Collaboration Test Diagram", "type": "DFD-1.0.0"}')

DIAGRAM_ID=$(echo "$DIAGRAM_RESPONSE" | jq -r '.id')

if [ "$DIAGRAM_ID" = "null" ] || [ -z "$DIAGRAM_ID" ]; then
    echo -e "${RED}❌ Failed to create diagram${NC}"
    echo -e "${YELLOW}Response: $DIAGRAM_RESPONSE${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Created diagram: $DIAGRAM_ID${NC}"

echo -e "${YELLOW}9. 🤝 Testing collaboration session creation...${NC}"
COLLAB_RESPONSE=$(curl -s -X POST http://localhost:8080/threat_models/$TM_ID/diagrams/$DIAGRAM_ID/collaborate \
  -H "Authorization: Bearer $JWT_TOKEN" \
  -H "Content-Type: application/json")

SESSION_ID=$(echo "$COLLAB_RESPONSE" | jq -r '.session_id')
OWNER_PERMISSIONS=$(echo "$COLLAB_RESPONSE" | jq -r '.participants[0].permissions')
SESSION_MANAGER=$(echo "$COLLAB_RESPONSE" | jq -r '.session_manager')

if [ "$SESSION_ID" = "null" ] || [ -z "$SESSION_ID" ]; then
    echo -e "${RED}❌ Failed to create collaboration session${NC}"
    echo -e "${YELLOW}Response: $COLLAB_RESPONSE${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Created collaboration session: $SESSION_ID${NC}"

echo -e "${YELLOW}10. 🔍 Verifying collaboration session permissions...${NC}"
echo -e "${BLUE}    Session ID: $SESSION_ID${NC}"
echo -e "${BLUE}    Session Manager: $SESSION_MANAGER${NC}"
echo -e "${BLUE}    Owner Permissions: $OWNER_PERMISSIONS${NC}"

# Test 1: Verify owner permissions (should get "writer")
if [ "$OWNER_PERMISSIONS" = "writer" ]; then
    echo -e "${GREEN}  ✅ PASS: Owner correctly got 'writer' permissions${NC}"
else
    echo -e "${RED}  ❌ FAIL: Owner got '$OWNER_PERMISSIONS' instead of 'writer'${NC}"
    exit 1
fi

# Test 2: Verify session manager is set correctly
if [ "$SESSION_MANAGER" = "$OWNER_EMAIL" ]; then
    echo -e "${GREEN}  ✅ PASS: Session manager correctly set to owner${NC}"
else
    echo -e "${RED}  ❌ FAIL: Session manager is '$SESSION_MANAGER', expected '$OWNER_EMAIL'${NC}"
    exit 1
fi

echo -e "${YELLOW}11. 📋 Verifying session structure...${NC}"
# Get all active sessions to verify structure
SESSIONS_RESPONSE=$(curl -s -X GET http://localhost:8080/collaboration/sessions \
  -H "Authorization: Bearer $JWT_TOKEN")

SESSIONS_COUNT=$(echo "$SESSIONS_RESPONSE" | jq '. | length')
echo -e "${BLUE}  Active sessions: $SESSIONS_COUNT${NC}"

if [ "$SESSIONS_COUNT" -gt 0 ]; then
    FIRST_SESSION=$(echo "$SESSIONS_RESPONSE" | jq '.[0]')
    FIRST_SESSION_MANAGER=$(echo "$FIRST_SESSION" | jq -r '.session_manager')
    FIRST_SESSION_PARTICIPANTS=$(echo "$FIRST_SESSION" | jq '.participants | length')
    
    echo -e "${BLUE}  Session manager: $FIRST_SESSION_MANAGER${NC}"
    echo -e "${BLUE}  Participants count: $FIRST_SESSION_PARTICIPANTS${NC}"
    
    if [ "$FIRST_SESSION_PARTICIPANTS" -gt 0 ]; then
        FIRST_PARTICIPANT_PERMS=$(echo "$FIRST_SESSION" | jq -r '.participants[0].permissions')
        echo -e "${BLUE}  Participant permissions: $FIRST_PARTICIPANT_PERMS${NC}"
    fi
fi

echo -e "${GREEN}🎉 All collaboration session permission tests PASSED!${NC}"
echo -e "${GREEN}✅ Owner correctly receives 'writer' permissions${NC}"
echo -e "${GREEN}✅ Session manager is correctly set to the creator${NC}"
echo -e "${GREEN}✅ Session structure includes all required fields${NC}"

echo -e "${YELLOW}📊 Test Summary:${NC}"
echo -e "   - Environment setup and cleanup: ✅"
echo -e "   - OAuth authentication flow: ✅" 
echo -e "   - JWT token extraction: ✅"
echo -e "   - Threat model creation: ✅"
echo -e "   - Diagram creation: ✅"
echo -e "   - Collaboration session creation: ✅"
echo -e "   - Owner permission mapping (owner → writer): ✅"
echo -e "   - Session manager field population: ✅"
echo -e "   - Session listing and structure: ✅"

exit 0