# Redis Schema Documentation

This document defines the Redis key patterns, data structures, and validation rules for the TMI application.

## Key Naming Conventions

All Redis keys follow a hierarchical naming pattern using colons as separators:
`{namespace}:{type}:{identifier}:{sub-identifier}`

### Authentication & Session Keys

| Key Pattern                       | Data Type | TTL          | Description        |
| --------------------------------- | --------- | ------------ | ------------------ |
| `session:{user_id}:{session_id}`  | Hash      | 24 hours     | User session data  |
| `auth:token:{token_id}`           | String    | Token expiry | JWT token cache    |
| `auth:refresh:{refresh_token_id}` | Hash      | 30 days      | Refresh token data |
| `auth:state:{state}`              | Hash      | 10 minutes   | OAuth state data   |
| `blacklist:token:{jti}`           | String    | Token expiry | Revoked JWT tokens |

### Rate Limiting Keys

| Key Pattern                           | Data Type | TTL      | Description                          |
| ------------------------------------- | --------- | -------- | ------------------------------------ |
| `rate_limit:global:{ip}:{endpoint}`   | String    | 1 minute | Global rate limiting per IP/endpoint |
| `rate_limit:user:{user_id}:{action}`  | String    | 1 minute | User-specific rate limiting          |
| `rate_limit:api:{api_key}:{endpoint}` | String    | 1 hour   | API key rate limiting                |

### Cache Keys

| Key Pattern                     | Data Type | TTL        | Description        |
| ------------------------------- | --------- | ---------- | ------------------ |
| `cache:user:{user_id}`          | Hash      | 5 minutes  | User profile cache |
| `cache:threat_model:{model_id}` | Hash      | 10 minutes | Threat model cache |
| `cache:diagram:{diagram_id}`    | String    | 10 minutes | Diagram data cache |

### Temporary Operation Keys

| Key Pattern            | Data Type | TTL        | Description       |
| ---------------------- | --------- | ---------- | ----------------- |
| `temp:export:{job_id}` | Hash      | 1 hour     | Export job status |
| `temp:import:{job_id}` | Hash      | 1 hour     | Import job status |
| `lock:{resource}:{id}` | String    | 30 seconds | Distributed locks |

## Data Structure Specifications

### Session Hash Structure

```
session:{user_id}:{session_id} = {
    "user_id": "string",
    "email": "string",
    "created_at": "RFC3339 timestamp",
    "last_accessed": "RFC3339 timestamp",
    "ip_address": "string",
    "user_agent": "string",
    "roles": "JSON array"
}
```

### OAuth State Hash Structure

```
auth:state:{state} = {
    "provider": "string",
    "redirect_uri": "string",
    "code_challenge": "string",
    "created_at": "RFC3339 timestamp"
}
```

### Rate Limit String Structure

```
rate_limit:*:*:* = "count:timestamp"
Example: "5:1642598400"
```

## Validation Rules

1. **Key Pattern Validation**

   - All keys must match their defined patterns
   - No spaces allowed in keys
   - Use lowercase for namespace and type components
   - UUIDs should be lowercase

2. **TTL Requirements**

   - All temporary data MUST have a TTL
   - Session data: 24 hours max
   - Cache data: 1 hour max
   - Lock data: 5 minutes max
   - OAuth state: 10 minutes max

3. **Data Type Consistency**

   - String keys must contain string values
   - Hash keys must contain hash structures
   - No mixing of data types for the same key pattern

4. **Value Validation**
   - Timestamps must be in RFC3339 format
   - UUIDs must be valid UUID v4 format
   - JSON data must be valid JSON
   - IP addresses must be valid IPv4 or IPv6

## Memory Management

1. **Key Expiration Policy**

   - Use TTL for all temporary data
   - Monitor keys without TTL regularly
   - Clean up expired sessions daily

2. **Memory Limits**

   - Max value size: 512KB
   - Max hash fields: 1000
   - Max list length: 10000

3. **Eviction Policy**
   - Use `allkeys-lru` for cache instances
   - Use `volatile-lru` for mixed workloads

## Monitoring Requirements

1. **Key Metrics**

   - Total key count by pattern
   - Memory usage by key pattern
   - TTL distribution
   - Hit/miss ratios for cache keys

2. **Health Checks**
   - Redis connectivity
   - Memory usage percentage
   - Slow query log monitoring
   - Client connection count

## Security Considerations

1. **Sensitive Data**

   - Never store passwords in Redis
   - Encrypt sensitive tokens before storage
   - Use short TTLs for authentication data

2. **Access Control**

   - Use Redis ACL for user separation
   - Limit commands available to application user
   - Use TLS for Redis connections

3. **Key Namespace Isolation**
   - Use database separation for different environments
   - Prefix keys with environment (dev/staging/prod)
