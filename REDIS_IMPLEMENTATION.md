# Redis Schema Validation Implementation

This document summarizes the Redis schema validation and key management system that has been implemented for the TMI application.

## Overview

We have implemented a comprehensive Redis schema validation system that ensures:

- All Redis keys follow defined patterns
- Keys have appropriate TTLs
- Data types are consistent
- Redis health is monitored

## Components Implemented

### 1. Redis Schema Documentation (`REDIS_SCHEMA.md`)

- Defines all Redis key patterns used in the application
- Specifies data types, TTLs, and structures for each key type
- Documents security considerations and best practices

### 2. Redis Key Validator (`auth/db/redis_validator.go`)

- Validates keys against defined patterns using regex
- Checks TTL compliance
- Verifies data type consistency
- Provides comprehensive validation reporting

### 3. Redis Key Builder (`auth/db/redis_keys.go`)

- Helper functions to construct valid Redis keys
- Ensures consistent key formatting
- Provides parsing functions for key components

### 4. Redis Health Checker (`auth/db/redis_health.go`)

- Performs comprehensive health checks
- Monitors memory usage
- Validates key patterns and TTLs
- Measures performance metrics
- Provides detailed health reports

### 5. Integration with check-db Tool (`cmd/check-db/main.go`)

- Extended to include Redis health checks
- Reports Redis statistics alongside PostgreSQL validation
- Provides unified database health reporting

## Key Patterns Implemented

### Authentication & Sessions

- `session:{user_id}:{session_id}` - User session data (Hash, 24h TTL)
- `auth:token:{token_id}` - JWT token cache (String, token expiry TTL)
- `auth:refresh:{refresh_token_id}` - Refresh tokens (Hash, 30d TTL)
- `auth:state:{state}` - OAuth state (Hash, 10m TTL)
- `blacklist:token:{jti}` - Revoked tokens (String, token expiry TTL)

### Rate Limiting

- `rate_limit:global:{ip}:{endpoint}` - Global rate limits (String, 1m TTL)
- `rate_limit:user:{user_id}:{action}` - User rate limits (String, 1m TTL)
- `rate_limit:api:{api_key}:{endpoint}` - API key limits (String, 1h TTL)

### Caching

- `cache:user:{user_id}` - User profile cache (Hash, 5m TTL)
- `cache:threat_model:{model_id}` - Threat model cache (Hash, 10m TTL)
- `cache:diagram:{diagram_id}` - Diagram cache (String, 10m TTL)

### Temporary Operations

- `temp:export:{job_id}` - Export job status (Hash, 1h TTL)
- `temp:import:{job_id}` - Import job status (Hash, 1h TTL)
- `lock:{resource}:{id}` - Distributed locks (String, 30s TTL)

## Usage Examples

### 1. Building Valid Keys

```go
keyBuilder := db.NewRedisKeyBuilder()
sessionKey := keyBuilder.SessionKey(userID, sessionID)
rateLimitKey := keyBuilder.RateLimitGlobalKey(ip, endpoint)
```

### 2. Validating Keys

```go
validator := db.NewRedisKeyValidator()
err := validator.ValidateKey(key)
err = validator.ValidateKeyWithTTL(ctx, client, key)
```

### 3. Health Checking

```go
healthChecker := db.NewRedisHealthChecker(client)
result := healthChecker.CheckHealth(ctx)
healthChecker.LogHealthCheck(result)
```

### 4. Running Database Checks

```bash
# Check both PostgreSQL and Redis
go run cmd/check-db/main.go

# Output includes:
# - PostgreSQL schema validation
# - Redis health status
# - Key statistics
# - Performance metrics
```

## Testing

Comprehensive tests have been implemented in `auth/db/redis_validator_test.go` covering:

- Key pattern validation
- TTL validation
- Data type validation
- Key builder functionality
- Pattern parsing

Run tests with:

```bash
go test ./auth/db -run TestRedis -v
```

## Demo Application

A demonstration application is available at `examples/redis_validation_demo.go` that shows:

- Creating valid and invalid keys
- Running validation
- Performing health checks
- Displaying pattern documentation

## Best Practices Enforced

1. **TTL Requirements**: All temporary data must have appropriate TTLs
2. **Key Naming**: Consistent hierarchical naming with colons as separators
3. **Data Type Consistency**: Each key pattern has a defined data type
4. **Memory Management**: Maximum TTLs enforced to prevent memory bloat
5. **Security**: No passwords stored, sensitive data encrypted, short TTLs for auth data

## Monitoring and Alerts

The health checker provides metrics for:

- Connection latency
- Memory usage percentage
- Invalid key patterns
- Missing TTLs
- Performance degradation

These can be integrated with monitoring systems to alert on:

- Memory usage > 75% (warning) or > 90% (critical)
- High latency (> 100ms ping, > 50ms write, > 20ms read)
- Invalid key patterns detected
- Keys without required TTLs

## Future Enhancements

1. **Automated Cleanup**: Implement scheduled jobs to clean up expired keys
2. **Key Migration**: Tools to migrate keys when patterns change
3. **Performance Optimization**: Key sharding for high-traffic patterns
4. **Advanced Monitoring**: Integration with Prometheus/Grafana
5. **Schema Versioning**: Support for evolving key patterns over time
