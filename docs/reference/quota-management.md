# Quota Management API

TMI provides comprehensive quota management for administrators to control resource limits per user. This document describes all quota types and their management endpoints.

## Overview

TMI has three types of quotas that can be configured per user:

1. **User API Quotas** - Rate limits for general API requests
2. **Webhook Quotas** - Rate limits for webhook subscriptions and events
3. **Addon Invocation Quotas** - Rate limits for addon invocations

All quota endpoints require administrator privileges.

## User API Quotas

User API quotas control the rate limits for general API requests per user.

### Default Values

- `max_requests_per_minute`: 1000 (increased for development/testing)
- `max_requests_per_hour`: 60000 (optional)

### Endpoints

#### List All Custom User API Quotas

```http
GET /admin/quotas/users
```

**Query Parameters:**
- `limit` (integer, optional): Maximum number of results (1-100, default: 50)
- `offset` (integer, optional): Number of results to skip (default: 0)

**Response:** `200 OK`
```json
[
  {
    "user_id": "uuid",
    "max_requests_per_minute": 2000,
    "max_requests_per_hour": 120000,
    "created_at": "2025-12-03T20:00:00Z",
    "modified_at": "2025-12-03T20:00:00Z"
  }
]
```

#### Get User API Quota

```http
GET /admin/quotas/users/{user_id}
```

**Response:** `200 OK` (returns quota or default values)

#### Create/Update User API Quota

```http
PUT /admin/quotas/users/{user_id}
Content-Type: application/json

{
  "max_requests_per_minute": 2000,
  "max_requests_per_hour": 120000
}
```

**Response:** `200 OK` (updated) or `201 Created` (new)

#### Delete User API Quota

```http
DELETE /admin/quotas/users/{user_id}
```

**Response:** `204 No Content` (reverts to system defaults)

---

## Webhook Quotas

Webhook quotas control limits for webhook subscriptions and event delivery per user.

### Default Values

- `max_subscriptions`: 10
- `max_events_per_minute`: 12
- `max_subscription_requests_per_minute`: 10
- `max_subscription_requests_per_day`: 20

### Endpoints

#### List All Custom Webhook Quotas

```http
GET /admin/quotas/webhooks
```

**Query Parameters:**
- `limit` (integer, optional): Maximum number of results (1-100, default: 50)
- `offset` (integer, optional): Number of results to skip (default: 0)

**Response:** `200 OK`
```json
[
  {
    "owner_id": "uuid",
    "max_subscriptions": 20,
    "max_events_per_minute": 24,
    "max_subscription_requests_per_minute": 20,
    "max_subscription_requests_per_day": 40,
    "created_at": "2025-12-03T20:00:00Z",
    "modified_at": "2025-12-03T20:00:00Z"
  }
]
```

#### Get Webhook Quota

```http
GET /admin/quotas/webhooks/{user_id}
```

**Response:** `200 OK` (returns quota or default values)

#### Create/Update Webhook Quota

```http
PUT /admin/quotas/webhooks/{user_id}
Content-Type: application/json

{
  "max_subscriptions": 20,
  "max_events_per_minute": 24,
  "max_subscription_requests_per_minute": 20,
  "max_subscription_requests_per_day": 40
}
```

**Response:** `200 OK` (updated) or `201 Created` (new)

#### Delete Webhook Quota

```http
DELETE /admin/quotas/webhooks/{user_id}
```

**Response:** `204 No Content` (reverts to system defaults)

---

## Addon Invocation Quotas

Addon invocation quotas control limits for addon invocations per user.

### Default Values

- `max_active_invocations`: 1 (concurrent active invocations)
- `max_invocations_per_hour`: 10

### Endpoints

#### List All Custom Addon Invocation Quotas

```http
GET /admin/quotas/addons
```

**Query Parameters:**
- `limit` (integer, optional): Maximum number of results (1-100, default: 50)
- `offset` (integer, optional): Number of results to skip (default: 0)

**Response:** `200 OK`
```json
[
  {
    "owner_id": "uuid",
    "max_active_invocations": 5,
    "max_invocations_per_hour": 100,
    "created_at": "2025-12-03T20:00:00Z",
    "modified_at": "2025-12-03T20:00:00Z"
  }
]
```

#### Get Addon Invocation Quota

```http
GET /admin/quotas/addons/{user_id}
```

**Response:** `200 OK` (returns quota or default values)

#### Create/Update Addon Invocation Quota

```http
PUT /admin/quotas/addons/{user_id}
Content-Type: application/json

{
  "max_active_invocations": 5,
  "max_invocations_per_hour": 100
}
```

**Response:** `200 OK` (updated) or `201 Created` (new)

#### Delete Addon Invocation Quota

```http
DELETE /admin/quotas/addons/{user_id}
```

**Response:** `204 No Content` (reverts to system defaults)

---

## Common Response Codes

All quota endpoints use these standard HTTP response codes:

- `200 OK` - Request successful, returns quota data
- `201 Created` - Quota created successfully
- `204 No Content` - Quota deleted successfully
- `400 Bad Request` - Invalid request body or user ID
- `401 Unauthorized` - Missing or invalid authentication
- `403 Forbidden` - User is not an administrator
- `404 Not Found` - Quota not found (DELETE only)
- `500 Internal Server Error` - Server error

## Error Response Format

```json
{
  "error": "descriptive error message"
}
```

## Authentication

All quota management endpoints require:
1. Valid JWT bearer token
2. Administrator role

Include the token in the Authorization header:
```http
Authorization: Bearer <jwt_token>
```

## Best Practices

1. **List Before Modifying**: Use list endpoints to discover which users have custom quotas
2. **Pagination**: Always use `limit` and `offset` for large result sets
3. **Default Values**: Only set custom quotas when needed; defaults work for most users
4. **Monitoring**: Regularly review custom quotas to ensure they align with current requirements
5. **Documentation**: Document why specific users have custom quotas

## Examples

### Find All Users with Custom API Quotas

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "https://api.example.com/admin/quotas/users?limit=100"
```

### Set Higher Quota for Power User

```bash
curl -X PUT \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "max_requests_per_minute": 5000,
    "max_requests_per_hour": 300000
  }' \
  "https://api.example.com/admin/quotas/users/550e8400-e29b-41d4-a716-446655440000"
```

### Revert User to Default Quotas

```bash
curl -X DELETE \
  -H "Authorization: Bearer $TOKEN" \
  "https://api.example.com/admin/quotas/users/550e8400-e29b-41d4-a716-446655440000"
```

## Database Schema

Custom quotas are stored in three tables:

- `user_api_quotas` - User API rate limits
- `webhook_quotas` - Webhook subscription and event limits
- `addon_invocation_quotas` - Addon invocation limits

Each table includes:
- Primary key: user/owner UUID (foreign key to `users.internal_uuid`)
- Quota fields (specific to quota type)
- `created_at` and `modified_at` timestamps
- CASCADE DELETE: Quotas are automatically deleted when user is deleted
