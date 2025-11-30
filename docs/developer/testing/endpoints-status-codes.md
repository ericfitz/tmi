# HTTP Status Codes Reference

This document provides a comprehensive reference for HTTP status codes used by the TMI API, including error response formats, common scenarios, and examples.

## Table of Contents

1. [Overview](#overview)
2. [Success Responses](#success-responses)
3. [Client Error Responses](#client-error-responses)
4. [Server Error Responses](#server-error-responses)
5. [Error Response Format](#error-response-format)
6. [Common Scenarios](#common-scenarios)
7. [Status Code Summary Table](#status-code-summary-table)

## Overview

TMI follows REST API conventions and HTTP standards for status codes. All responses include appropriate status codes, headers, and structured error messages when applicable.

### Design Principles

- **Consistency**: Status codes are used consistently across all endpoints
- **Clarity**: Error messages provide actionable information for developers
- **Security**: Error responses do not leak sensitive system information
- **Standards**: Follows RFC 7231 (HTTP/1.1 Semantics and Content) and REST best practices

## Success Responses

### 200 OK

**Usage**: Successful GET, PUT, or PATCH requests that return data.

**Common Endpoints**:
- `GET /threat_models/{id}` - Retrieve threat model
- `PUT /threat_models/{id}` - Update threat model (full update)
- `PATCH /threat_models/{id}` - Partial update threat model
- `GET /webhooks` - List webhook subscriptions

**Response Example**:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Payment System Threat Model",
  "description": "Security analysis for payment processing",
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-16T14:20:00Z",
  "owner": {
    "principal_type": "user",
    "provider": "google",
    "provider_id": "alice@example.com",
    "display_name": "Alice Smith",
    "email": "alice@example.com"
  }
}
```

### 201 Created

**Usage**: Successful POST requests that create new resources.

**Common Endpoints**:
- `POST /threat_models` - Create threat model
- `POST /threat_models/{id}/diagrams` - Create diagram
- `POST /webhooks` - Create webhook subscription
- `POST /addons` - Create add-on (admin only)

**Response Characteristics**:
- Includes `Location` header with URL of created resource
- Returns full representation of created resource
- Resource ID is server-generated (UUID v7)

**Response Example**:
```http
HTTP/1.1 201 Created
Location: /threat_models/550e8400-e29b-41d4-a716-446655440000
Content-Type: application/json

{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "New Threat Model",
  "created_at": "2024-01-16T15:00:00Z",
  "owner": {
    "provider_id": "alice@example.com",
    "display_name": "Alice Smith"
  }
}
```

### 202 Accepted

**Usage**: Request accepted for asynchronous processing.

**Common Endpoints**:
- `POST /addons/{id}/invoke` - Invoke add-on
- `POST /webhooks/{id}/test` - Test webhook

**Response Example**:
```json
{
  "delivery_id": "660e8400-e29b-41d4-a716-446655440000",
  "message": "Test delivery created and queued for sending"
}
```

**Notes**:
- Request is valid and accepted but not yet completed
- Response includes identifier for tracking async operation
- Client should poll for completion or rely on webhooks

### 204 No Content

**Usage**: Successful DELETE requests or updates with no response body.

**Common Endpoints**:
- `DELETE /threat_models/{id}` - Delete threat model
- `DELETE /webhooks/{id}` - Delete webhook subscription
- `DELETE /users/me?challenge={token}` - Delete user account

**Response Characteristics**:
- No response body
- Confirms successful completion
- Operation is idempotent (safe to retry)

**Response Example**:
```http
HTTP/1.1 204 No Content
```

### 302 Found

**Usage**: Temporary redirect for OAuth and SAML flows.

**Common Endpoints**:
- `GET /oauth2/authorize` - Redirect to OAuth provider
- `GET /oauth2/callback` - Redirect to client with tokens
- `GET /saml/{provider}/login` - Redirect to SAML IdP

**Response Example**:
```http
HTTP/1.1 302 Found
Location: https://accounts.google.com/o/oauth2/v2/auth?client_id=...
```

## Client Error Responses

### 400 Bad Request

**Usage**: Invalid request syntax, malformed JSON, or validation failures.

**Common Causes**:
- Malformed JSON in request body
- Missing required fields
- Invalid data types or formats
- Invalid UUID format
- Prohibited fields in request

**Error Codes**:
- `invalid_input` - General validation error
- `invalid_id` - Invalid UUID format
- `invalid_request` - Malformed request structure

**Response Example**:
```json
{
  "error": "invalid_input",
  "error_description": "Field 'name' is required but was not provided"
}
```

**Common Scenarios**:

#### Invalid UUID Format
```bash
# Request
DELETE /threat_models/not-a-uuid

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_id",
  "error_description": "Invalid threat model ID format, must be a valid UUID"
}
```

#### Missing Required Field
```bash
# Request
POST /threat_models
{
  "description": "Missing name field"
}

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "name is required"
}
```

#### Prohibited Field
```bash
# Request
PATCH /threat_models/{id}
[
  {"op": "replace", "path": "/id", "value": "new-id"}
]

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "Field 'id' is not allowed in PATCH requests. The ID is read-only and set by the server."
}
```

#### Invalid HTTPS Requirement
```bash
# Request
POST /webhooks
{
  "name": "My Webhook",
  "url": "http://example.com/hook",
  "events": ["threat_model.created"]
}

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "webhook URL must use HTTPS"
}
```

### 401 Unauthorized

**Usage**: Missing or invalid authentication credentials.

**Common Causes**:
- Missing `Authorization` header
- Invalid JWT token
- Expired JWT token
- Blacklisted token (user deleted)
- Invalid OAuth credentials

**Response Headers**:
```http
WWW-Authenticate: Bearer
```

**Response Example**:
```json
{
  "error": "unauthorized",
  "error_description": "Authentication required"
}
```

**Common Scenarios**:

#### Missing Authentication
```bash
# Request
GET /threat_models
# (No Authorization header)

# Response
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
{
  "error": "unauthorized",
  "error_description": "Authentication required"
}
```

#### Expired Token
```bash
# Request
GET /threat_models/{id}
Authorization: Bearer eyJhbGc...expired-token

# Response
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
{
  "error": "unauthorized",
  "error_description": "Token has expired"
}
```

#### Stale Session (User Deleted)
```bash
# Request
POST /threat_models
Authorization: Bearer eyJhbGc...valid-but-user-deleted

# Response
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
{
  "error": "unauthorized",
  "error_description": "Your session is no longer valid. Please log in again."
}
```

**Notes**:
- Token is blacklisted when user account is deleted
- User must re-authenticate via OAuth to get new token
- Foreign key constraint violations trigger this error

### 403 Forbidden

**Usage**: Valid authentication but insufficient permissions.

**Common Causes**:
- User lacks required role (reader/writer/owner)
- Not authorized to access resource
- Group-based access denied
- Administrator access required
- Attempting to modify authorization without owner role

**Error Codes**:
- `forbidden` - Access denied

**Response Example**:
```json
{
  "error": "forbidden",
  "error_description": "Insufficient permissions to access this threat model"
}
```

**Common Scenarios**:

#### Reader Attempting Write
```bash
# User has reader role, attempts to update
PUT /threat_models/{id}
Authorization: Bearer {valid-token-reader-role}

# Response
HTTP/1.1 403 Forbidden
{
  "error": "forbidden",
  "error_description": "Insufficient permissions to update this threat model"
}
```

#### Non-Owner Changing Authorization
```bash
# User has writer role, attempts to change authorization
PATCH /threat_models/{id}
[
  {"op": "add", "path": "/authorization/-", "value": {"provider_id": "new-user@example.com", "role": "writer"}}
]

# Response
HTTP/1.1 403 Forbidden
{
  "error": "forbidden",
  "error_description": "Only the owner can change ownership or authorization"
}
```

#### Non-Admin Accessing Admin Endpoint
```bash
# Regular user attempts to create add-on
POST /addons
Authorization: Bearer {valid-token-non-admin}

# Response
HTTP/1.1 403 Forbidden
{
  "error": "forbidden",
  "error_description": "Administrator access required"
}
```

#### Accessing Another User's Webhook
```bash
# User attempts to access webhook owned by different user
GET /webhooks/{webhook-id}
Authorization: Bearer {valid-token-different-user}

# Response
HTTP/1.1 403 Forbidden
{
  "error": "access denied",
  "error_description": "access denied"
}
```

### 404 Not Found

**Usage**: Requested resource does not exist.

**Common Causes**:
- Invalid resource ID
- Resource was deleted
- User lacks permission to see resource (authorization-based filtering)
- Endpoint does not exist

**Error Codes**:
- `not_found` - Resource not found

**Response Example**:
```json
{
  "error": "not_found",
  "error_description": "Threat model not found"
}
```

**Common Scenarios**:

#### Resource Does Not Exist
```bash
# Request
GET /threat_models/00000000-0000-0000-0000-000000000000

# Response
HTTP/1.1 404 Not Found
{
  "error": "not_found",
  "error_description": "Threat model not found"
}
```

#### Sub-Resource Not Found
```bash
# Request
GET /threat_models/{id}/diagrams/invalid-diagram-id

# Response
HTTP/1.1 404 Not Found
{
  "error": "not_found",
  "error_description": "Diagram not found"
}
```

#### Webhook Subscription Not Found
```bash
# Request
DELETE /webhooks/{non-existent-id}

# Response
HTTP/1.1 404 Not Found
{
  "error": "not_found",
  "error_description": "subscription not found"
}
```

**Notes**:
- For security, 404 may be returned instead of 403 to avoid leaking resource existence
- Authorization middleware may return 404 for resources user cannot access

### 409 Conflict

**Usage**: Request conflicts with current resource state.

**Common Causes**:
- Active collaboration session prevents deletion
- Resource in use by other operations
- Concurrent modification conflict
- Add-on has active invocations

**Error Codes**:
- `conflict` - Resource conflict

**Response Example**:
```json
{
  "error": "conflict",
  "error_description": "Cannot delete threat model while a diagram has an active collaboration session. Please end all collaboration sessions first."
}
```

**Common Scenarios**:

#### Active Collaboration Session
```bash
# Request
DELETE /threat_models/{id}
# (Diagram has active WebSocket collaboration session)

# Response
HTTP/1.1 409 Conflict
{
  "error": "conflict",
  "error_description": "Cannot delete threat model while a diagram has an active collaboration session. Please end all collaboration sessions first."
}
```

#### Add-on with Active Invocations
```bash
# Request
DELETE /addons/{id}
# (Add-on has running invocations)

# Response
HTTP/1.1 409 Conflict
{
  "error": "conflict",
  "error_description": "Cannot delete add-on with active invocations. Please wait for invocations to complete."
}
```

#### Duplicate Authorization Entry
```bash
# Request
PATCH /threat_models/{id}
{
  "authorization": [
    {"provider_id": "alice@example.com", "role": "writer"},
    {"provider_id": "alice@example.com", "role": "reader"}
  ]
}

# Response
HTTP/1.1 409 Conflict
{
  "error": "conflict",
  "error_description": "Duplicate authorization subject: alice@example.com appears multiple times"
}
```

### 422 Unprocessable Entity

**Usage**: Request is well-formed but semantically invalid (currently limited use in TMI).

**Common Causes**:
- Business rule violations
- Semantic validation failures
- Invalid state transitions

**Response Example**:
```json
{
  "error": "unprocessable_entity",
  "error_description": "Cannot transition from current state to requested state"
}
```

**Notes**:
- TMI primarily uses 400 for validation errors
- 422 reserved for semantic/business logic violations
- Less common than 400 in current implementation

### 429 Too Many Requests

**Usage**: Rate limit exceeded.

**Common Causes**:
- Too many API requests in time window
- Webhook subscription limit reached
- Too many webhook deliveries
- Subscription request rate limit exceeded

**Response Headers**:
```http
Retry-After: 60
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1642345678
```

**Response Example**:
```json
{
  "error": "rate_limit_exceeded",
  "error_description": "Too many requests. Please try again later."
}
```

**Common Scenarios**:

#### Webhook Subscription Limit
```bash
# Request
POST /webhooks
# (User has reached maximum subscriptions)

# Response
HTTP/1.1 429 Too Many Requests
{
  "error": "rate_limit_exceeded",
  "error_description": "Maximum number of webhook subscriptions reached"
}
```

#### Subscription Request Rate
```bash
# Request
POST /webhooks
# (Too many subscription operations in short time)

# Response
HTTP/1.1 429 Too Many Requests
{
  "error": "rate_limit_exceeded",
  "error_description": "Too many subscription requests. Please slow down."
}
```

## Server Error Responses

### 500 Internal Server Error

**Usage**: Unexpected server-side error.

**Common Causes**:
- Database connection failures
- Unexpected exceptions
- Configuration errors
- External service failures

**Error Codes**:
- `server_error` - Internal server error

**Response Example**:
```json
{
  "error": "server_error",
  "error_description": "Failed to create threat model"
}
```

**Security Notes**:
- Error messages are sanitized to prevent information disclosure
- Stack traces are removed before sending to client (CWE-209 protection)
- Detailed errors logged server-side only
- Generic message returned to client

**Common Scenarios**:

#### Database Failure
```bash
# Request
POST /threat_models
# (Database connection lost)

# Response
HTTP/1.1 500 Internal Server Error
{
  "error": "server_error",
  "error_description": "Failed to create threat model"
}
```

#### Administrator Store Not Initialized
```bash
# Request
POST /addons
# (GlobalAdministratorStore is nil)

# Response
HTTP/1.1 500 Internal Server Error
{
  "error": "server_error",
  "error_description": "Administrator store not initialized"
}
```

## Error Response Format

### Standard Error Structure

All error responses follow this structure:

```json
{
  "error": "error_code",
  "error_description": "Human-readable error message",
  "details": {
    "code": "specific_error_code",
    "context": {
      "field": "value",
      "another_field": "value"
    },
    "suggestion": "How to fix this error"
  }
}
```

### Error Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `error` | string | Yes | Machine-readable error code |
| `error_description` | string | Yes | Human-readable error message |
| `details` | object | No | Additional error context |
| `details.code` | string | No | Specific error sub-code |
| `details.context` | object | No | Key-value pairs with error context |
| `details.suggestion` | string | No | Suggested fix or workaround |

### Error Codes

Standard error codes used across TMI:

- `invalid_input` - Invalid request data (400)
- `invalid_id` - Invalid UUID format (400)
- `invalid_request` - Malformed request (400)
- `unauthorized` - Authentication required (401)
- `forbidden` - Insufficient permissions (403)
- `not_found` - Resource not found (404)
- `conflict` - Resource conflict (409)
- `rate_limit_exceeded` - Too many requests (429)
- `server_error` - Internal server error (500)

### Error Response Examples

#### Basic Error
```json
{
  "error": "not_found",
  "error_description": "Threat model not found"
}
```

#### Error with Details
```json
{
  "error": "invalid_input",
  "error_description": "Invalid authorization entry format",
  "details": {
    "code": "missing_provider",
    "context": {
      "entry_index": 2,
      "provider_id": "alice@example.com"
    },
    "suggestion": "Ensure each authorization entry includes both 'provider' and 'provider_id' fields"
  }
}
```

## Common Scenarios

### Authentication Scenarios

#### Successful Authentication
```bash
# Request
GET /threat_models
Authorization: Bearer eyJhbGc...valid-token

# Response
HTTP/1.1 200 OK
[...]
```

#### Missing Token
```bash
# Request
GET /threat_models
# (No Authorization header)

# Response
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
{
  "error": "unauthorized",
  "error_description": "Authentication required"
}
```

#### Invalid Token
```bash
# Request
GET /threat_models
Authorization: Bearer invalid-token

# Response
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer
{
  "error": "unauthorized",
  "error_description": "Invalid authentication token"
}
```

### Authorization Scenarios

#### Sufficient Permissions (Reader)
```bash
# Request
GET /threat_models/{id}
Authorization: Bearer {token-with-reader-role}

# Response
HTTP/1.1 200 OK
{...}
```

#### Insufficient Permissions (Reader Attempting Write)
```bash
# Request
PUT /threat_models/{id}
Authorization: Bearer {token-with-reader-role}

# Response
HTTP/1.1 403 Forbidden
{
  "error": "forbidden",
  "error_description": "Insufficient permissions to update this threat model"
}
```

#### Owner-Only Operation (Delete)
```bash
# Request
DELETE /threat_models/{id}
Authorization: Bearer {token-with-writer-role}

# Response
HTTP/1.1 403 Forbidden
{
  "error": "forbidden",
  "error_description": "Only the owner can delete a threat model"
}
```

### Validation Scenarios

#### Valid Request
```bash
# Request
POST /threat_models
{
  "name": "API Security Analysis",
  "description": "Threat model for REST API"
}

# Response
HTTP/1.1 201 Created
Location: /threat_models/{id}
{...}
```

#### Missing Required Field
```bash
# Request
POST /threat_models
{
  "description": "Missing name"
}

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "name is required"
}
```

#### Invalid Data Format
```bash
# Request
POST /threat_models/{id}/diagrams
{
  "name": "System Architecture",
  "cells": "not-an-array"
}

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "cells must be an array"
}
```

### Resource Lifecycle Scenarios

#### Create Resource
```bash
# Request
POST /threat_models
{
  "name": "New Model",
  "description": "Description"
}

# Response
HTTP/1.1 201 Created
Location: /threat_models/550e8400-e29b-41d4-a716-446655440000
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "New Model",
  "created_at": "2024-01-16T15:00:00Z",
  ...
}
```

#### Read Resource
```bash
# Request
GET /threat_models/550e8400-e29b-41d4-a716-446655440000

# Response
HTTP/1.1 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "New Model",
  ...
}
```

#### Update Resource (Full)
```bash
# Request
PUT /threat_models/550e8400-e29b-41d4-a716-446655440000
{
  "name": "Updated Model",
  "description": "New description",
  "threat_model_framework": "STRIDE",
  "authorization": [...]
}

# Response
HTTP/1.1 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Updated Model",
  "modified_at": "2024-01-16T16:00:00Z",
  ...
}
```

#### Update Resource (Partial)
```bash
# Request
PATCH /threat_models/550e8400-e29b-41d4-a716-446655440000
[
  {"op": "replace", "path": "/name", "value": "Partially Updated"}
]

# Response
HTTP/1.1 200 OK
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "Partially Updated",
  "modified_at": "2024-01-16T16:30:00Z",
  ...
}
```

#### Delete Resource
```bash
# Request
DELETE /threat_models/550e8400-e29b-41d4-a716-446655440000

# Response
HTTP/1.1 204 No Content
```

#### Access Deleted Resource
```bash
# Request
GET /threat_models/550e8400-e29b-41d4-a716-446655440000

# Response
HTTP/1.1 404 Not Found
{
  "error": "not_found",
  "error_description": "Threat model not found"
}
```

### Webhook Scenarios

#### Create Webhook Subscription
```bash
# Request
POST /webhooks
{
  "name": "My Webhook",
  "url": "https://example.com/webhook",
  "events": ["threat_model.created", "threat_model.updated"]
}

# Response
HTTP/1.1 201 Created
{
  "id": "660e8400-e29b-41d4-a716-446655440000",
  "secret": "a1b2c3d4e5f6...",
  "status": "pending_verification",
  "challenge": "challenge-token",
  ...
}
```

#### Test Webhook
```bash
# Request
POST /webhooks/660e8400-e29b-41d4-a716-446655440000/test

# Response
HTTP/1.1 202 Accepted
{
  "delivery_id": "770e8400-e29b-41d4-a716-446655440000",
  "message": "Test delivery created and queued for sending"
}
```

#### Invalid HTTPS
```bash
# Request
POST /webhooks
{
  "name": "Insecure Webhook",
  "url": "http://example.com/webhook",
  "events": ["threat_model.created"]
}

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_input",
  "error_description": "webhook URL must use HTTPS"
}
```

### User Deletion Scenarios

#### Generate Challenge
```bash
# Request
DELETE /users/me
# (No challenge parameter)

# Response
HTTP/1.1 200 OK
{
  "challenge": "delete_my_account_abc123",
  "expires_at": "2024-01-16T17:00:00Z"
}
```

#### Delete with Challenge
```bash
# Request
DELETE /users/me?challenge=delete_my_account_abc123

# Response
HTTP/1.1 204 No Content
```

#### Invalid Challenge
```bash
# Request
DELETE /users/me?challenge=wrong-challenge

# Response
HTTP/1.1 400 Bad Request
{
  "error": "invalid_challenge",
  "error_description": "Invalid or expired challenge"
}
```

## Status Code Summary Table

| Code | Name | Usage | Common Endpoints |
|------|------|-------|------------------|
| 200 | OK | Successful GET/PUT/PATCH with data | GET /threat_models/{id}, PUT /threat_models/{id} |
| 201 | Created | Successful POST creating resource | POST /threat_models, POST /webhooks |
| 202 | Accepted | Async operation accepted | POST /addons/{id}/invoke |
| 204 | No Content | Successful DELETE or update with no body | DELETE /threat_models/{id} |
| 302 | Found | Temporary redirect (OAuth/SAML) | GET /oauth2/authorize |
| 400 | Bad Request | Invalid input, validation error | All endpoints (validation) |
| 401 | Unauthorized | Missing/invalid authentication | All protected endpoints |
| 403 | Forbidden | Insufficient permissions | All endpoints (authorization) |
| 404 | Not Found | Resource does not exist | All endpoints (resource access) |
| 409 | Conflict | Resource conflict | DELETE with active sessions |
| 422 | Unprocessable Entity | Semantic validation error | Limited use |
| 429 | Too Many Requests | Rate limit exceeded | All endpoints (rate limiting) |
| 500 | Internal Server Error | Unexpected server error | All endpoints (failures) |

## Best Practices for Developers

### Handling Errors

1. **Check Status Code First**: Always check HTTP status code before parsing response body
2. **Parse Error Responses**: Extract `error` and `error_description` fields for debugging
3. **Log Error Details**: Include `details` object in logs when available
4. **Retry Logic**: Implement exponential backoff for 5xx errors and 429
5. **User Feedback**: Show `error_description` to users (safe for display)

### Authentication

1. **Include Bearer Token**: Always include `Authorization: Bearer {token}` header
2. **Handle 401**: Redirect to login on 401 responses
3. **Token Refresh**: Implement token refresh before expiration
4. **Blacklisted Tokens**: Handle 401 with "session no longer valid" by forcing re-login

### Authorization

1. **Check Permissions**: Verify user role before attempting operations
2. **Handle 403**: Show appropriate "access denied" message to users
3. **Owner Operations**: Ensure user has owner role for delete/transfer operations
4. **Group-Based Access**: Consider group memberships in authorization checks

### Validation

1. **Client-Side Validation**: Validate required fields before sending requests
2. **UUID Format**: Validate UUID format (RFC 4122) before sending
3. **HTTPS URLs**: Ensure webhook URLs use HTTPS
4. **Field Constraints**: Check field length and format requirements

### Resource Management

1. **Check Conflicts**: Handle 409 conflicts (active sessions, concurrent updates)
2. **Idempotent Operations**: Safe to retry DELETE (returns 204 or 404)
3. **Pagination**: Use limit/offset parameters for large result sets
4. **Rate Limiting**: Respect 429 responses and `Retry-After` header

## Security Considerations

### Information Disclosure

- Stack traces are never included in error responses (CWE-209 protection)
- Detailed error messages logged server-side only
- Error messages sanitized before sending to client
- Generic "server_error" message for unexpected exceptions

### Authentication Security

- JWT tokens validated on every request
- Expired tokens rejected with 401
- Blacklisted tokens (deleted users) rejected with 401
- WWW-Authenticate header included in 401 responses

### Authorization Security

- Role-based access control (reader/writer/owner)
- Group-based access control for shared resources
- Administrator-only endpoints protected
- 404 may be returned instead of 403 to prevent information leakage

### Rate Limiting

- Webhook subscription limits per user
- Webhook delivery rate limits
- API request rate limits (429 Too Many Requests)
- Retry-After header indicates cooldown period

## Related Documentation

- [Integration Testing Guide](integration-testing.md) - Testing API endpoints
- [Client Integration Guide](../integration/client-integration-guide.md) - Implementing API clients
- [OAuth Integration](../setup/oauth-integration.md) - Authentication setup
- [OpenAPI Specification](../../reference/apis/tmi-openapi.json) - Complete API reference

## Changelog

- **2024-01-16**: Initial documentation created
- Coverage: All standard HTTP status codes used by TMI
- Includes examples from threat models, webhooks, add-ons, and user deletion
