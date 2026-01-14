#!/bin/bash
# cats-create-test-data.sh
# Creates prerequisite test data for CATS fuzzing to eliminate false positives
#
# This script creates a complete object hierarchy (threat models, threats, diagrams, etc.)
# and stores their IDs in a reference file for CATS to use during fuzzing.
#
# Usage: ./scripts/cats-create-test-data.sh [OPTIONS]
# Options:
#   -u, --user USER      OAuth user (default: charlie)
#   -s, --server URL     Server URL (default: http://localhost:8080)
#   -i, --idp IDP        Identity provider (default: tmi)
#   -o, --output FILE    Output reference file (default: cats-test-data.json)
#   -h, --help           Show help

set -euo pipefail

# Configuration
DEFAULT_USER="charlie"
DEFAULT_SERVER="http://localhost:8080"
DEFAULT_IDP="tmi"
DEFAULT_OUTPUT="test/outputs/cats/cats-test-data.json"
OAUTH_STUB_PORT=8079
OAUTH_STUB_URL="http://localhost:${OAUTH_STUB_PORT}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Parse command line arguments
USER="${DEFAULT_USER}"
SERVER="${DEFAULT_SERVER}"
IDP="${DEFAULT_IDP}"
OUTPUT_FILE="${DEFAULT_OUTPUT}"

while [[ $# -gt 0 ]]; do
    case $1 in
        -u|--user)
            USER="$2"
            shift 2
            ;;
        -s|--server)
            SERVER="$2"
            shift 2
            ;;
        -i|--idp)
            IDP="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_FILE="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  -u, --user USER      OAuth user (default: ${DEFAULT_USER})"
            echo "  -s, --server URL     Server URL (default: ${DEFAULT_SERVER})"
            echo "  -i, --idp IDP        Identity provider (default: ${DEFAULT_IDP})"
            echo "  -o, --output FILE    Output reference file (default: ${DEFAULT_OUTPUT})"
            echo "  -h, --help           Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1" >&2
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
    exit 1
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

# Check if server is running
check_server() {
    log "Checking if TMI server is running at ${SERVER}..."
    if ! curl -s "${SERVER}/" > /dev/null 2>&1; then
        error "TMI server not responding at ${SERVER}. Please start with 'make start-dev'"
    fi
    success "Server is running"
}

# Start OAuth stub if needed
start_oauth_stub() {
    if ! pgrep -f "oauth-client-callback-stub" > /dev/null; then
        log "Starting OAuth stub..."
        cd "${PROJECT_ROOT}" && make start-oauth-stub > /dev/null 2>&1 &
        sleep 3
        log "OAuth stub started"
    else
        log "OAuth stub already running"
    fi
}

# Authenticate and get JWT token
authenticate() {
    log "Authenticating as ${USER}@tmi.local..."

    # Initiate OAuth flow
    local FLOW_RESPONSE
    FLOW_RESPONSE=$(curl -s -X POST "${OAUTH_STUB_URL}/flows/start" \
        -H "Content-Type: application/json" \
        -d "{\"userid\": \"${USER}\", \"idp\": \"${IDP}\", \"server\": \"${SERVER}\"}" 2>&1)

    if [ $? -ne 0 ]; then
        error "Failed to start OAuth flow. Is oauth-client-callback-stub running?"
    fi

    local FLOW_ID
    FLOW_ID=$(echo "${FLOW_RESPONSE}" | jq -r '.flow_id' 2>/dev/null)

    if [ -z "${FLOW_ID}" ] || [ "${FLOW_ID}" = "null" ]; then
        error "Failed to get flow ID from OAuth stub response"
    fi

    # Poll for completion (max 30 seconds)
    log "Waiting for OAuth flow to complete (flow_id: ${FLOW_ID})..."
    for i in {1..30}; do
        local STATUS
        STATUS=$(curl -s "${OAUTH_STUB_URL}/flows/${FLOW_ID}" | jq -r '.status' 2>/dev/null)

        if [ "${STATUS}" = "completed" ]; then
            break
        fi

        if [ $i -eq 30 ]; then
            error "OAuth flow timeout - flow did not complete in 30 seconds"
        fi

        sleep 1
    done

    # Get token
    local TOKEN_RESPONSE
    TOKEN_RESPONSE=$(curl -s "${OAUTH_STUB_URL}/flows/${FLOW_ID}")
    JWT_TOKEN=$(echo "${TOKEN_RESPONSE}" | jq -r '.tokens.access_token' 2>/dev/null)

    if [ -z "${JWT_TOKEN}" ] || [ "${JWT_TOKEN}" = "null" ]; then
        error "Failed to get JWT token from flow response"
    fi

    success "Successfully authenticated"
}

# Create threat model
create_threat_model() {
    log "Creating threat model..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Threat Model",
            "description": "Created by cats-create-test-data.sh for comprehensive API fuzzing. DO NOT DELETE.",
            "threat_model_framework": "STRIDE",
            "metadata": [
                {
                    "key": "version",
                    "value": "1.0"
                },
                {
                    "key": "purpose",
                    "value": "cats-fuzzing-test-data"
                }
            ]
        }')

    TM_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${TM_ID}" ] || [ "${TM_ID}" = "null" ]; then
        error "Failed to create threat model. Response: ${RESPONSE}"
    fi

    success "Created threat model: ${TM_ID}"
}

# Create threat
create_threat() {
    log "Creating threat..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/threats" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Threat",
            "description": "Test threat for CATS fuzzing",
            "threat_type": ["Tampering", "Information Disclosure"],
            "severity": "high",
            "priority": "high",
            "status": "identified"
        }')

    THREAT_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${THREAT_ID}" ] || [ "${THREAT_ID}" = "null" ]; then
        error "Failed to create threat. Response: ${RESPONSE}"
    fi

    success "Created threat: ${THREAT_ID}"
}

# Create diagram
create_diagram() {
    log "Creating diagram..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/diagrams" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Diagram",
            "type": "DFD-1.0.0"
        }')

    DIAGRAM_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${DIAGRAM_ID}" ] || [ "${DIAGRAM_ID}" = "null" ]; then
        error "Failed to create diagram. Response: ${RESPONSE}"
    fi

    success "Created diagram: ${DIAGRAM_ID}"
}

# Create document
create_document() {
    log "Creating document..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/documents" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Document",
            "uri": "https://docs.example.com/cats-test-document.pdf",
            "description": "Test document for CATS fuzzing"
        }')

    DOC_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${DOC_ID}" ] || [ "${DOC_ID}" = "null" ]; then
        error "Failed to create document. Response: ${RESPONSE}"
    fi

    success "Created document: ${DOC_ID}"
}

# Create asset
create_asset() {
    log "Creating asset..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/assets" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Asset",
            "description": "Test asset for CATS fuzzing",
            "type": "software"
        }')

    ASSET_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${ASSET_ID}" ] || [ "${ASSET_ID}" = "null" ]; then
        error "Failed to create asset. Response: ${RESPONSE}"
    fi

    success "Created asset: ${ASSET_ID}"
}

# Create note
create_note() {
    log "Creating note..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/notes" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Note",
            "content": "CATS test note for comprehensive API fuzzing"
        }')

    NOTE_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${NOTE_ID}" ] || [ "${NOTE_ID}" = "null" ]; then
        error "Failed to create note. Response: ${RESPONSE}"
    fi

    success "Created note: ${NOTE_ID}"
}

# Create repository
create_repository() {
    log "Creating repository..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/threat_models/${TM_ID}/repositories" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "uri": "https://github.com/example/cats-test-repo"
        }')

    REPO_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${REPO_ID}" ] || [ "${REPO_ID}" = "null" ]; then
        error "Failed to create repository. Response: ${RESPONSE}"
    fi

    success "Created repository: ${REPO_ID}"
}

# Create webhook subscription
create_webhook() {
    log "Creating webhook subscription..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/webhooks/subscriptions" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Webhook",
            "url": "https://webhook.site/cats-test-webhook",
            "events": ["threat_model.created", "threat.created"]
        }')

    WEBHOOK_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${WEBHOOK_ID}" ] || [ "${WEBHOOK_ID}" = "null" ]; then
        warn "Failed to create webhook (may not be implemented yet). Response: ${RESPONSE}"
        WEBHOOK_ID="00000000-0000-0000-0000-000000000000"
    else
        success "Created webhook: ${WEBHOOK_ID}"
    fi
}

# Create addon
create_addon() {
    log "Creating addon..."

    # Create a minimal addon (admin only - charlie has admin privileges)
    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/addons" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"CATS Test Addon\",
            \"webhook_id\": \"${WEBHOOK_ID}\",
            \"threat_model_id\": \"${TM_ID}\"
        }")

    ADDON_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${ADDON_ID}" ] || [ "${ADDON_ID}" = "null" ]; then
        warn "Failed to create addon. Response: ${RESPONSE}"
        ADDON_ID="00000000-0000-0000-0000-000000000001"
    else
        success "Created addon: ${ADDON_ID}"
    fi
}

# Create client credential
create_client_credential() {
    log "Creating client credential..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/users/me/client_credentials" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Credential",
            "description": "Test credential for CATS fuzzing"
        }')

    CLIENT_CRED_ID=$(echo "${RESPONSE}" | jq -r '.id' 2>/dev/null)

    if [ -z "${CLIENT_CRED_ID}" ] || [ "${CLIENT_CRED_ID}" = "null" ]; then
        error "Failed to create client credential. Response: ${RESPONSE}"
    fi

    success "Created client credential: ${CLIENT_CRED_ID}"
}

# Get current user's internal UUID for admin operations
get_current_user_uuid() {
    log "Getting current user's internal UUID..."

    local RESPONSE
    RESPONSE=$(curl -s -X GET "${SERVER}/users/me" \
        -H "Authorization: Bearer ${JWT_TOKEN}")

    USER_INTERNAL_UUID=$(echo "${RESPONSE}" | jq -r '.internal_uuid // .id' 2>/dev/null)

    if [ -z "${USER_INTERNAL_UUID}" ] || [ "${USER_INTERNAL_UUID}" = "null" ]; then
        warn "Could not get user internal UUID. Response: ${RESPONSE}"
        USER_INTERNAL_UUID="00000000-0000-0000-0000-000000000000"
    else
        success "Got user internal UUID: ${USER_INTERNAL_UUID}"
    fi
}

# Create admin group for testing
create_admin_group() {
    log "Creating admin group..."

    local RESPONSE
    RESPONSE=$(curl -s -X POST "${SERVER}/admin/groups" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d '{
            "name": "CATS Test Group",
            "description": "Test group for CATS fuzzing"
        }')

    ADMIN_GROUP_ID=$(echo "${RESPONSE}" | jq -r '.id // .internal_uuid' 2>/dev/null)

    if [ -z "${ADMIN_GROUP_ID}" ] || [ "${ADMIN_GROUP_ID}" = "null" ]; then
        warn "Failed to create admin group (may already exist or require admin). Response: ${RESPONSE}"
        # Try to get existing group
        RESPONSE=$(curl -s -X GET "${SERVER}/admin/groups" \
            -H "Authorization: Bearer ${JWT_TOKEN}")
        ADMIN_GROUP_ID=$(echo "${RESPONSE}" | jq -r '.groups[0].internal_uuid // .groups[0].id // empty' 2>/dev/null)
        if [ -z "${ADMIN_GROUP_ID}" ] || [ "${ADMIN_GROUP_ID}" = "null" ]; then
            ADMIN_GROUP_ID="00000000-0000-0000-0000-000000000000"
        else
            success "Using existing admin group: ${ADMIN_GROUP_ID}"
        fi
    else
        success "Created admin group: ${ADMIN_GROUP_ID}"
    fi
}

# Get list of admin users for testing
get_admin_users() {
    log "Getting admin users list..."

    local RESPONSE
    RESPONSE=$(curl -s -X GET "${SERVER}/admin/users" \
        -H "Authorization: Bearer ${JWT_TOKEN}")

    ADMIN_USER_UUID=$(echo "${RESPONSE}" | jq -r '.users[0].internal_uuid // .users[0].id // empty' 2>/dev/null)

    if [ -z "${ADMIN_USER_UUID}" ] || [ "${ADMIN_USER_UUID}" = "null" ]; then
        warn "Could not get admin user UUID, using current user UUID"
        ADMIN_USER_UUID="${USER_INTERNAL_UUID}"
    else
        success "Got admin user UUID: ${ADMIN_USER_UUID}"
    fi
}

# Create metadata entries
create_metadata() {
    log "Creating metadata entries..."

    METADATA_KEY="cats-test-key"
    local METADATA_VALUE='{"value": "cats-test-value"}'

    # Threat metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/threats/${THREAT_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Diagram metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/diagrams/${DIAGRAM_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Document metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/documents/${DOC_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Asset metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/assets/${ASSET_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Note metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/notes/${NOTE_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Repository metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/repositories/${REPO_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    # Threat model metadata
    curl -s -X PUT "${SERVER}/threat_models/${TM_ID}/metadata/${METADATA_KEY}" \
        -H "Authorization: Bearer ${JWT_TOKEN}" \
        -H "Content-Type: application/json" \
        -d "${METADATA_VALUE}" > /dev/null 2>&1

    success "Created all metadata entries"
}

# Write reference file
write_reference_file() {
    log "Writing test data reference to ${OUTPUT_FILE}..."

    cat > "${OUTPUT_FILE}" <<EOF
{
  "version": "1.0.0",
  "created_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "server": "${SERVER}",
  "user": {
    "provider_user_id": "${USER}",
    "provider": "${IDP}",
    "email": "${USER}@tmi.local"
  },
  "objects": {
    "threat_model": {
      "id": "${TM_ID}",
      "name": "CATS Test Threat Model"
    },
    "threat": {
      "id": "${THREAT_ID}",
      "threat_model_id": "${TM_ID}",
      "name": "CATS Test Threat"
    },
    "diagram": {
      "id": "${DIAGRAM_ID}",
      "threat_model_id": "${TM_ID}",
      "title": "CATS Test Diagram"
    },
    "document": {
      "id": "${DOC_ID}",
      "threat_model_id": "${TM_ID}",
      "name": "CATS Test Document"
    },
    "asset": {
      "id": "${ASSET_ID}",
      "threat_model_id": "${TM_ID}",
      "name": "CATS Test Asset"
    },
    "note": {
      "id": "${NOTE_ID}",
      "threat_model_id": "${TM_ID}"
    },
    "repository": {
      "id": "${REPO_ID}",
      "threat_model_id": "${TM_ID}",
      "url": "https://github.com/example/cats-test-repo"
    },
    "addon": {
      "id": "${ADDON_ID}",
      "threat_model_id": "${TM_ID}",
      "name": "CATS Test Addon"
    },
    "webhook": {
      "id": "${WEBHOOK_ID}",
      "url": "https://webhook.site/cats-test-webhook"
    },
    "client_credential": {
      "id": "${CLIENT_CRED_ID}",
      "name": "CATS Test Credential"
    },
    "metadata_key": "cats-test-key"
  }
}
EOF

    success "Test data reference written to ${OUTPUT_FILE}"

    # Also create YAML version for CATS (which requires path-based format)
    local YAML_FILE="${OUTPUT_FILE%.json}.yml"
    cat > "${YAML_FILE}" <<YAMLEOF
# CATS Reference Data - Path-based format for parameter replacement
# Generated: $(date -u +%Y-%m-%dT%H:%M:%SZ)
# See: https://endava.github.io/cats/docs/getting-started/running-cats/

# All paths - global parameter substitution
all:
  id: ${TM_ID}
  threat_model_id: ${TM_ID}
  threat_id: ${THREAT_ID}
  diagram_id: ${DIAGRAM_ID}
  document_id: ${DOC_ID}
  asset_id: ${ASSET_ID}
  note_id: ${NOTE_ID}
  repository_id: ${REPO_ID}
  webhook_id: ${WEBHOOK_ID}
  addon_id: ${ADDON_ID}
  client_credential_id: ${CLIENT_CRED_ID}
  key: ${METADATA_KEY}
  # Admin resource IDs
  internal_uuid: ${USER_INTERNAL_UUID}
  user_id: ${ADMIN_USER_UUID}
  user_uuid: ${ADMIN_USER_UUID}
  group_id: ${ADMIN_GROUP_ID}
YAMLEOF
    success "YAML version written to ${YAML_FILE}"
}

# Print summary
print_summary() {
    echo ""
    success "✅ Test data creation complete!"
    echo ""
    log "Created objects:"
    log "  - Threat Model:      ${TM_ID}"
    log "  - Threat:            ${THREAT_ID}"
    log "  - Diagram:           ${DIAGRAM_ID}"
    log "  - Document:          ${DOC_ID}"
    log "  - Asset:             ${ASSET_ID}"
    log "  - Note:              ${NOTE_ID}"
    log "  - Repository:        ${REPO_ID}"
    log "  - Addon:             ${ADDON_ID}"
    log "  - Webhook:           ${WEBHOOK_ID}"
    log "  - Client Credential: ${CLIENT_CRED_ID}"
    log "  - Metadata entries:  7 keys created"
    echo ""
    log "Reference file: ${OUTPUT_FILE}"
    echo ""
    log "Next step: Run CATS fuzzing with: make cats-fuzz"
    log "           CATS will use IDs from ${OUTPUT_FILE} for path parameters"
    echo ""
}

# Main execution
main() {
    log "🔧 CATS Test Data Creation"
    log "   User:   ${USER}@tmi.local"
    log "   Server: ${SERVER}"
    log "   Output: ${OUTPUT_FILE}"
    echo ""

    check_server
    start_oauth_stub
    authenticate

    # Get user info for admin operations
    get_current_user_uuid

    create_threat_model
    create_threat
    create_diagram
    create_document
    create_asset
    create_note
    create_repository
    create_webhook
    create_addon
    create_client_credential
    create_metadata

    # Admin resources (requires admin privileges)
    create_admin_group
    get_admin_users

    write_reference_file
    print_summary
}

# Run main
main
