#!/bin/bash
# ==============================================================================
# TMI Test User Cleanup Script
# ==============================================================================
#
# Deletes all test users from the local TMI database via the admin API.
# Preserves the charlie@tmi.local admin account and any non-TMI provider users.
#
# PREREQUISITES:
# --------------
# 1. TMI server must be running (make start-dev)
# 2. OAuth callback stub must be running (make start-oauth-stub)
# 3. charlie@tmi.local must exist and be an administrator
#
# AUTHENTICATION FLOW:
# --------------------
# This script performs OAuth 2.0 authentication using the PKCE flow:
#
# 1. Initialize OAuth flow via POST /oauth/init on the callback stub
#    - Generates code_verifier and code_challenge (S256)
#    - Returns authorization URL with state, code_challenge, and login_hint
#
# 2. Execute authorization request to TMI server
#    - GET /oauth2/authorize?idp=tmi&login_hint=charlie&...
#    - TMI server authenticates user and redirects to callback stub
#    - Callback stub receives authorization code
#
# 3. Token exchange (handled by callback stub)
#    - Callback stub exchanges code + code_verifier for tokens
#    - Tokens stored and accessible via GET /creds?userid=charlie
#
# 4. Retrieve JWT access token for API calls
#    - GET /creds?userid=charlie returns access_token
#    - Token used in Authorization: Bearer header for admin API calls
#
# API ENDPOINTS USED:
# -------------------
# - GET  /admin/users              - List all users (paginated, 50 per page)
# - DELETE /admin/users/{uuid}     - Delete user and cascade all related data
#
# USAGE:
# ------
#   ./scripts/delete-test-users.sh
#
# Run multiple times if there are more than 50 test users (API pagination).
#
# ==============================================================================

set -e

API_BASE="http://localhost:8080"
OAUTH_STUB="http://localhost:8079"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== TMI Test User Cleanup Script ==="
echo ""

# Check if TMI server is running
if ! curl -s "$API_BASE/" > /dev/null 2>&1; then
    echo -e "${RED}Error: TMI server is not running at $API_BASE${NC}"
    echo "Start it with: make start-dev"
    exit 1
fi

# Check if OAuth stub is running
if ! curl -s "$OAUTH_STUB/" > /dev/null 2>&1; then
    echo -e "${YELLOW}Starting OAuth stub...${NC}"
    make -C "$(dirname "$0")/.." start-oauth-stub > /dev/null 2>&1
    sleep 2
fi

# Get JWT for charlie (admin user) via OAuth PKCE flow
echo "Authenticating as charlie@tmi.local..."

# Step 1: Initialize OAuth flow (generates PKCE code_verifier/code_challenge)
INIT_RESPONSE=$(curl -s -X POST "$OAUTH_STUB/oauth/init" \
    -H 'Content-Type: application/json' \
    -d '{"userid": "charlie"}')

AUTH_URL=$(echo "$INIT_RESPONSE" | jq -r '.authorization_url')

if [ -z "$AUTH_URL" ] || [ "$AUTH_URL" = "null" ]; then
    echo -e "${RED}Error: Failed to initialize OAuth flow${NC}"
    echo "$INIT_RESPONSE"
    exit 1
fi

# Step 2: Execute authorization request (stub receives callback with code)
curl -s -L "$AUTH_URL" > /dev/null 2>&1

# Step 3: Retrieve the access token (stub already exchanged code for tokens)
TOKEN=$(curl -s "$OAUTH_STUB/creds?userid=charlie" | jq -r '.access_token')

if [ -z "$TOKEN" ] || [ "$TOKEN" = "null" ]; then
    echo -e "${RED}Error: Failed to get access token${NC}"
    exit 1
fi

echo -e "${GREEN}Authenticated successfully${NC}"
echo ""

# List all users via admin API
echo "Fetching user list..."
USERS_RESPONSE=$(curl -s "$API_BASE/admin/users" \
    -H "Authorization: Bearer $TOKEN")

# Check for error response
if echo "$USERS_RESPONSE" | jq -e '.error' > /dev/null 2>&1; then
    echo -e "${RED}Error fetching users:${NC}"
    echo "$USERS_RESPONSE" | jq .
    exit 1
fi

# Get array of test users (provider=tmi or email ends with @tmi.local), excluding charlie
TEST_USERS=$(echo "$USERS_RESPONSE" | jq -r '
    .users[] |
    select((.provider == "tmi" or (.email | endswith("@tmi.local"))) and .email != "charlie@tmi.local") |
    "\(.internal_uuid)|\(.email)"
')

# Count users - handle empty string properly
if [ -z "$TEST_USERS" ]; then
    TOTAL_COUNT=0
else
    TOTAL_COUNT=$(echo "$TEST_USERS" | wc -l | tr -d ' ')
fi

if [ "$TOTAL_COUNT" -eq 0 ]; then
    echo -e "${GREEN}No test users to delete${NC}"
    exit 0
fi

echo "Found $TOTAL_COUNT test users to delete"
echo ""

# Delete each user via admin API
DELETED=0
FAILED=0

while IFS='|' read -r UUID EMAIL; do
    if [ -z "$UUID" ]; then
        continue
    fi

    echo -n "Deleting $EMAIL... "

    RESPONSE=$(curl -s -w "\n%{http_code}" -X DELETE "$API_BASE/admin/users/$UUID" \
        -H "Authorization: Bearer $TOKEN")

    HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
    BODY=$(echo "$RESPONSE" | sed '$d')

    if [ "$HTTP_CODE" = "204" ]; then
        echo -e "${GREEN}OK${NC}"
        ((DELETED++)) || true
    else
        echo -e "${RED}FAILED (HTTP $HTTP_CODE)${NC}"
        if [ -n "$BODY" ]; then
            echo "  Response: $BODY"
        fi
        ((FAILED++)) || true
    fi
done <<< "$TEST_USERS"

echo ""
echo "=== Summary ==="
echo -e "Deleted: ${GREEN}$DELETED${NC}"
echo -e "Failed:  ${RED}$FAILED${NC}"
echo -e "Skipped: charlie@tmi.local (admin account)"

# Hint if there might be more users (pagination)
if [ "$TOTAL_COUNT" -eq 50 ]; then
    echo ""
    echo -e "${YELLOW}Note: API returns max 50 users per request. Run again if more exist.${NC}"
fi
