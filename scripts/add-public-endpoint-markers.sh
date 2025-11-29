#!/bin/bash

# Add vendor extensions to public endpoints in OpenAPI spec
# This script adds x-public-endpoint and x-authentication-required markers
# to all endpoints that have security:[] (no authentication required)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
OPENAPI_SPEC="${PROJECT_ROOT}/docs/reference/apis/tmi-openapi.json"
BACKUP_SPEC="${OPENAPI_SPEC}.backup.$(date +%Y%m%d_%H%M%S)"

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1"
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# Create backup
log "Creating backup: ${BACKUP_SPEC}"
cp "${OPENAPI_SPEC}" "${BACKUP_SPEC}"

log "Adding vendor extensions to all public endpoints..."

# Use jq to add vendor extensions to all endpoints with security:[]
# This identifies public endpoints and marks them appropriately
jq '
  # Function to determine endpoint purpose based on path
  def endpoint_purpose($path):
    if ($path | startswith("/.well-known")) then "OIDC Discovery"
    elif ($path | startswith("/oauth2")) then "OAuth Flow"
    elif ($path | startswith("/saml")) then "SAML Flow"
    elif ($path == "/") then "Health Check"
    else "Public Endpoint"
    end;

  # Walk through all paths and methods
  .paths = (
    .paths | to_entries | map(
      .key as $path |
      .value as $methods |
      if ($methods | type == "object") then
        {
          key: $path,
          value: (
            $methods | to_entries | map(
              .key as $method |
              .value as $operation |
              if ($operation | type == "object") and ($operation.security == []) then
                {
                  key: $method,
                  value: (
                    $operation + {
                      "x-public-endpoint": true,
                      "x-authentication-required": false,
                      "x-public-endpoint-purpose": endpoint_purpose($path)
                    }
                  )
                }
              else
                {
                  key: $method,
                  value: $operation
                }
              end
            ) | from_entries
          )
        }
      else
        {key: $path, value: $methods}
      end
    ) | from_entries
  )
' "${OPENAPI_SPEC}" > "${OPENAPI_SPEC}.tmp"

# Verify the update worked
if jq empty "${OPENAPI_SPEC}.tmp" 2>/dev/null; then
    mv "${OPENAPI_SPEC}.tmp" "${OPENAPI_SPEC}"
    success "Vendor extensions added to all public endpoints"
else
    echo "ERROR: Generated invalid JSON. Restoring backup..."
    mv "${BACKUP_SPEC}" "${OPENAPI_SPEC}"
    rm -f "${OPENAPI_SPEC}.tmp"
    exit 1
fi

# Count updated endpoints
public_count=$(jq '[.paths[][] | select(."x-public-endpoint" == true)] | length' "${OPENAPI_SPEC}")
log "Updated ${public_count} public endpoint operations"

log "Backup saved to: ${BACKUP_SPEC}"
success "All done! Remember to run 'make validate-openapi' for full validation"
