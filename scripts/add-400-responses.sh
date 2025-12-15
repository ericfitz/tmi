#!/bin/bash
# Add missing 400 Bad Request responses to OpenAPI specification
# Based on CATS fuzzer analysis

set -e

OPENAPI_FILE="docs/reference/apis/tmi-openapi.json"
BACKUP_FILE="${OPENAPI_FILE}.backup.$(date +%Y%m%d_%H%M%S)"

# Create backup
echo "Creating backup: ${BACKUP_FILE}"
cp "${OPENAPI_FILE}" "${BACKUP_FILE}"

# Define the 400 response schema
BAD_REQUEST_RESPONSE='{
  "description": "Bad Request - Invalid parameters, malformed UUIDs, or validation failures",
  "content": {
    "application/json": {
      "schema": {
        "$ref": "#/components/schemas/Error"
      }
    }
  }
}'

# Function to add 400 response to an endpoint
add_400_response() {
    local path="$1"
    local method="$2"

    echo "Adding 400 response to: ${method} ${path}"

    jq --arg path "$path" \
       --arg method "$method" \
       --argjson response "$BAD_REQUEST_RESPONSE" \
       'if .paths[$path][$method].responses then
          .paths[$path][$method].responses["400"] = $response
        else
          .
        end' \
       "${OPENAPI_FILE}" > "${OPENAPI_FILE}.tmp" && \
    mv "${OPENAPI_FILE}.tmp" "${OPENAPI_FILE}"
}

# Function to add 200 response to an endpoint
add_200_response() {
    local path="$1"
    local method="$2"

    echo "Adding 200 response to: ${method} ${path}"

    OK_RESPONSE='{
      "description": "Success - Token successfully revoked",
      "content": {
        "application/json": {
          "schema": {
            "type": "object",
            "properties": {
              "message": {
                "type": "string",
                "example": "Token revoked successfully"
              }
            }
          }
        }
      }
    }'

    jq --arg path "$path" \
       --arg method "$method" \
       --argjson response "$OK_RESPONSE" \
       'if .paths[$path][$method].responses then
          .paths[$path][$method].responses["200"] = $response
        else
          .
        end' \
       "${OPENAPI_FILE}" > "${OPENAPI_FILE}.tmp" && \
    mv "${OPENAPI_FILE}.tmp" "${OPENAPI_FILE}"
}

echo "Starting OpenAPI specification updates..."

# Addon Endpoints
add_400_response "/addons" "get"
add_400_response "/addons/{id}" "delete"
add_400_response "/addons/{id}" "get"

# Admin Endpoints
add_400_response "/admin/administrators" "get"
add_400_response "/admin/quotas/addons" "get"
add_400_response "/admin/quotas/users" "get"
add_400_response "/admin/quotas/webhooks" "get"

# Client Credentials
add_400_response "/users/me/client_credentials" "get"

# Collaboration
add_400_response "/collaboration/sessions" "get"

# Invocations
add_400_response "/invocations/{id}" "get"

# OAuth Endpoints
add_400_response "/oauth2/providers/{idp}/groups" "get"
add_200_response "/oauth2/revoke" "post"
add_400_response "/oauth2/revoke" "post"
add_400_response "/oauth2/userinfo" "get"

# Threat Model Core
add_400_response "/threat_models" "get"
add_400_response "/threat_models/{threat_model_id}" "delete"
add_400_response "/threat_models/{threat_model_id}" "get"

# Threat Model - Assets
add_400_response "/threat_models/{threat_model_id}/assets" "get"
add_400_response "/threat_models/{threat_model_id}/assets/{asset_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/assets/{asset_id}" "get"
add_400_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}" "get"

# Threat Model - Diagrams
add_400_response "/threat_models/{threat_model_id}/diagrams" "get"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}" "get"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate" "delete"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate" "get"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}" "get"

# Threat Model - Documents
add_400_response "/threat_models/{threat_model_id}/documents" "get"
add_400_response "/threat_models/{threat_model_id}/documents/{document_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/documents/{document_id}" "get"
add_400_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}" "get"

# Threat Model - Metadata
add_400_response "/threat_models/{threat_model_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/metadata/{key}" "get"

# Threat Model - Notes
add_400_response "/threat_models/{threat_model_id}/notes" "get"
add_400_response "/threat_models/{threat_model_id}/notes/{note_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/notes/{note_id}" "get"
add_400_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}" "get"

# Threat Model - Repositories
add_400_response "/threat_models/{threat_model_id}/repositories" "get"
add_400_response "/threat_models/{threat_model_id}/repositories/{repository_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/repositories/{repository_id}" "get"
add_400_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key}" "get"

# Threat Model - Threats
add_400_response "/threat_models/{threat_model_id}/threats" "get"
add_400_response "/threat_models/{threat_model_id}/threats/{threat_id}" "delete"
add_400_response "/threat_models/{threat_model_id}/threats/{threat_id}" "get"
add_400_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata" "get"
add_400_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}" "delete"
add_400_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}" "get"

# Users
add_400_response "/users/me" "get"

# Webhooks
add_400_response "/webhooks/deliveries" "get"
add_400_response "/webhooks/deliveries/{delivery_id}" "get"
add_400_response "/webhooks/subscriptions" "get"
add_400_response "/webhooks/subscriptions/{webhook_id}" "delete"
add_400_response "/webhooks/subscriptions/{webhook_id}" "get"
add_400_response "/webhooks/subscriptions/{webhook_id}/test" "post"

echo ""
echo "✅ Successfully updated OpenAPI specification"
echo "   Backup saved to: ${BACKUP_FILE}"
echo ""
echo "Next steps:"
echo "  1. Validate: make validate-openapi"
echo "  2. Regenerate API code: make generate-api"
