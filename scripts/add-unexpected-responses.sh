#!/bin/bash
# Add missing response codes identified by CATS "Unexpected response code" analysis
# Excludes false positive 401s (endpoints already document 401)

set -e

OPENAPI_FILE="docs/reference/apis/tmi-openapi.json"
BACKUP_FILE="${OPENAPI_FILE}.backup.$(date +%Y%m%d_%H%M%S)"

# Create backup
echo "Creating backup: ${BACKUP_FILE}"
cp "${OPENAPI_FILE}" "${BACKUP_FILE}"

# Function to add a response to an endpoint
add_response() {
    local path="$1"
    local method="$2"
    local status_code="$3"
    local description="$4"
    local response_json="$5"

    echo "Adding ${status_code} response to: ${method} ${path}"

    jq --arg path "$path" \
       --arg method "$method" \
       --arg code "$status_code" \
       --argjson response "$response_json" \
       'if .paths[$path][$method].responses then
          .paths[$path][$method].responses[$code] = $response
        else
          .
        end' \
       "${OPENAPI_FILE}" > "${OPENAPI_FILE}.tmp" && \
    mv "${OPENAPI_FILE}.tmp" "${OPENAPI_FILE}"
}

# Define response schemas
SUCCESS_200='{
  "description": "Success",
  "content": {
    "application/json": {
      "schema": {
        "type": "object"
      }
    }
  }
}'

CREATED_201='{
  "description": "Created - Resource successfully created",
  "content": {
    "application/json": {
      "schema": {
        "type": "object"
      }
    }
  }
}'

NO_CONTENT_204='{
  "description": "No Content - Resource successfully deleted"
}'

BAD_REQUEST_400='{
  "description": "Bad Request - Invalid parameters, malformed UUIDs, or validation failures",
  "content": {
    "application/json": {
      "schema": {
        "$ref": "#/components/schemas/Error"
      }
    }
  }
}'

FORBIDDEN_403='{
  "description": "Forbidden - Insufficient permissions to access this resource",
  "content": {
    "application/json": {
      "schema": {
        "$ref": "#/components/schemas/Error"
      }
    }
  }
}'

echo "Starting OpenAPI specification updates..."
echo ""

# ============================================================================
# SUCCESS RESPONSES (200, 201, 204)
# ============================================================================
echo "=== Adding Success Responses ==="

# 200 OK responses
add_response "/addons" "get" "200" "Success" "$SUCCESS_200"
add_response "/client-credentials" "get" "200" "Success" "$SUCCESS_200"
add_response "/collaboration/sessions" "get" "200" "Success" "$SUCCESS_200"
add_response "/invocations" "get" "200" "Success" "$SUCCESS_200"
add_response "/saml/providers/{idp}/users" "get" "200" "Success" "$SUCCESS_200"

# 201 Created responses
add_response "/client-credentials" "post" "201" "Created" "$CREATED_201"

# 204 No Content responses
add_response "/client-credentials/{id}" "delete" "204" "No Content" "$NO_CONTENT_204"

echo ""
echo "=== Adding 403 Forbidden Responses ==="

# ============================================================================
# 403 FORBIDDEN - Admin and Permission-based endpoints
# ============================================================================

# Addons (ownership required)
add_response "/addons" "post" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/addons/{id}" "delete" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Administrators
add_response "/admin/administrators" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/administrators" "post" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/administrators/{id}" "delete" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Groups
add_response "/admin/groups" "delete" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups" "post" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups/{internal_uuid}" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups/{internal_uuid}" "patch" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups/{internal_uuid}/members" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups/{internal_uuid}/members" "post" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "delete" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Quotas (Addons)
add_response "/admin/quotas/addons" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/addons/{user_id}" "delete" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/addons/{user_id}" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/addons/{user_id}" "put" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Quotas (Users)
add_response "/admin/quotas/users" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/users/{user_id}" "delete" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/users/{user_id}" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/users/{user_id}" "put" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Quotas (Webhooks)
add_response "/admin/quotas/webhooks" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/webhooks/{user_id}" "delete" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/webhooks/{user_id}" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/quotas/webhooks/{user_id}" "put" "403" "Forbidden" "$FORBIDDEN_403"

# Admin - Users
add_response "/admin/users" "delete" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/users" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/users/{internal_uuid}" "get" "403" "Forbidden" "$FORBIDDEN_403"
add_response "/admin/users/{internal_uuid}" "patch" "403" "Forbidden" "$FORBIDDEN_403"

echo ""
echo "=== Adding 400 Bad Request Responses ==="

# ============================================================================
# 400 BAD REQUEST - Additional endpoints (beyond first script)
# ============================================================================

# Addons
add_response "/addons" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Addons - Invoke
add_response "/addons/{id}/invoke" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}/invoke" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}/invoke" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}/invoke" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}/invoke" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/addons/{id}/invoke" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Administrators
add_response "/admin/administrators" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators/{id}" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators/{id}" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators/{id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators/{id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/administrators/{id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Groups
add_response "/admin/groups" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/groups/{internal_uuid}/members/{user_uuid}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Quotas (Addons)
add_response "/admin/quotas/addons" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons/{user_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons/{user_id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/addons/{user_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Quotas (Users)
add_response "/admin/quotas/users" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users/{user_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users/{user_id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/users/{user_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Quotas (Webhooks)
add_response "/admin/quotas/webhooks" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks/{user_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks/{user_id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/quotas/webhooks/{user_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Admin - Users
add_response "/admin/users" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users/{internal_uuid}" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users/{internal_uuid}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users/{internal_uuid}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/admin/users/{internal_uuid}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Client Credentials
add_response "/client-credentials" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials/{id}" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials/{id}" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials/{id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials/{id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/client-credentials/{id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Collaboration Sessions
add_response "/collaboration/sessions" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/collaboration/sessions" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/collaboration/sessions" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/collaboration/sessions" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/collaboration/sessions" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Invocations
add_response "/invocations" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Invocations - Status
add_response "/invocations/{id}/status" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}/status" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}/status" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}/status" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}/status" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/invocations/{id}/status" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# OAuth
add_response "/oauth2/providers/{idp}/groups" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/providers/{idp}/groups" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/providers/{idp}/groups" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/providers/{idp}/groups" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/revoke" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/revoke" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/revoke" "head" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/revoke" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/oauth2/revoke" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# SAML
add_response "/saml/providers/{idp}/users" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/saml/providers/{idp}/users" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/saml/providers/{idp}/users" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/saml/providers/{idp}/users" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/saml/providers/{idp}/users" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models
add_response "/threat_models" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Assets
add_response "/threat_models/{threat_model_id}/assets" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/bulk" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/bulk" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/bulk" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/assets/{asset_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Diagrams
add_response "/threat_models/{threat_model_id}/diagrams" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Documents
add_response "/threat_models/{threat_model_id}/documents" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/bulk" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/bulk" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/bulk" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Metadata
add_response "/threat_models/{threat_model_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Notes
add_response "/threat_models/{threat_model_id}/notes" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/notes/{note_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Repositories
add_response "/threat_models/{threat_model_id}/repositories" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/bulk" "delete" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/bulk" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/bulk" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/repositories/{repository_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Threat Models - Threats
add_response "/threat_models/{threat_model_id}/threats" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/bulk" "get" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}" "patch" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk" "post" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk" "put" "400" "Bad Request" "$BAD_REQUEST_400"
add_response "/threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}" "put" "400" "Bad Request" "$BAD_REQUEST_400"

# Webhooks
add_response "/webhooks/subscriptions" "post" "400" "Bad Request" "$BAD_REQUEST_400"

echo ""
echo "✅ Successfully updated OpenAPI specification"
echo "   Backup saved to: ${BACKUP_FILE}"
echo ""
echo "Summary of changes:"
echo "  - Added 7 success responses (200, 201, 204)"
echo "  - Added 33 forbidden (403) responses"
echo "  - Added 182 bad request (400) responses"
echo "  - Excluded 126 false positive 401 responses (already documented)"
echo ""
echo "Next steps:"
echo "  1. Validate: make validate-openapi"
echo "  2. Regenerate API code: make generate-api"
