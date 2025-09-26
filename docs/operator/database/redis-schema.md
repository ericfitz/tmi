# Redis Schema Documentation

This document defines the Redis key patterns, data structures, and caching strategies for the TMI (Threat Modeling Interface) application. The schema reflects the current implementation including comprehensive caching layers and real-time collaboration support.

## Key Naming Conventions

All Redis keys follow a hierarchical naming pattern using colons as separators:
`{namespace}:{type}:{identifier}:{sub-identifier}`

## Authentication & Session Management

### Authentication Keys

| Key Pattern                       | Data Type | TTL          | Description                    |
| --------------------------------- | --------- | ------------ | ------------------------------ |
| `session:{user_id}:{session_id}`  | Hash      | 24 hours     | User session data              |
| `auth:token:{token_id}`           | String    | Token expiry | JWT token cache                |
| `auth:refresh:{refresh_token_id}` | Hash      | 30 days      | Refresh token data             |
| `auth:state:{state}`              | Hash      | 10 minutes   | OAuth state data for PKCE flow |
| `blacklist:token:{jti}`           | String    | Token expiry | Revoked JWT tokens             |

### Rate Limiting Keys

| Key Pattern                           | Data Type | TTL      | Description                          |
| ------------------------------------- | --------- | -------- | ------------------------------------ |
| `rate_limit:global:{ip}:{endpoint}`   | String    | 1 minute | Global rate limiting per IP/endpoint |
| `rate_limit:user:{user_id}:{action}`  | String    | 1 minute | User-specific rate limiting          |
| `rate_limit:api:{api_key}:{endpoint}` | String    | 1 hour   | API key rate limiting                |

## Caching Strategy

The TMI application implements a comprehensive caching layer with different TTL strategies based on data access patterns and consistency requirements.

### Core Entity Cache Keys

| Key Pattern                     | Data Type | TTL        | Description        |
| ------------------------------- | --------- | ---------- | ------------------ |
| `cache:user:{user_id}`          | JSON      | 15 minutes | User profile cache |
| `cache:threat_model:{model_id}` | JSON      | 10 minutes | Threat model cache |
| `cache:diagram:{diagram_id}`    | JSON      | 2 minutes  | Diagram data cache |

### Sub-Resource Cache Keys

| Key Pattern                                | Data Type | TTL        | Description              |
| ------------------------------------------ | --------- | ---------- | ------------------------ |
| `cache:threat:{threat_id}`                 | JSON      | 5 minutes  | Individual threat cache  |
| `cache:document:{document_id}`             | JSON      | 5 minutes  | Document reference cache |
| `cache:source:{source_id}`                 | JSON      | 5 minutes  | Source repository cache  |
| `cache:metadata:{entity_type}:{entity_id}` | JSON      | 7 minutes  | Entity metadata cache    |
| `cache:cells:{diagram_id}`                 | JSON      | 2 minutes  | Diagram cells cache      |
| `cache:auth:{threat_model_id}`             | JSON      | 15 minutes | Authorization data cache |

### List Cache Keys

| Key Pattern                                             | Data Type | TTL       | Description          |
| ------------------------------------------------------- | --------- | --------- | -------------------- |
| `cache:list:{entity_type}:{parent_id}:{offset}:{limit}` | JSON      | 5 minutes | Paginated list cache |

Examples:

- `cache:list:threats:f1e46642-4b90-4332-a665-ef36d2ae0c74:0:50`
- `cache:list:diagrams:f1e46642-4b90-4332-a665-ef36d2ae0c74:20:10`

### Temporary Operation Keys

| Key Pattern            | Data Type | TTL        | Description       |
| ---------------------- | --------- | ---------- | ----------------- |
| `temp:export:{job_id}` | Hash      | 1 hour     | Export job status |
| `temp:import:{job_id}` | Hash      | 1 hour     | Import job status |
| `lock:{resource}:{id}` | String    | 30 seconds | Distributed locks |

## Data Structure Specifications

### Session Hash Structure

```json
session:{user_id}:{session_id} = {
    "user_id": "uuid",
    "email": "string",
    "name": "string",
    "created_at": "RFC3339 timestamp",
    "last_accessed": "RFC3339 timestamp",
    "ip_address": "string",
    "user_agent": "string",
    "roles": "JSON array of role strings"
}
```

### OAuth State Hash Structure

```json
auth:state:{state} = {
    "provider": "string (google|github|microsoft|apple|facebook|twitter)",
    "redirect_uri": "string",
    "code_challenge": "string (PKCE)",
    "code_challenge_method": "S256",
    "created_at": "RFC3339 timestamp"
}
```

### Rate Limit String Structure

```
rate_limit:*:*:* = "count:timestamp"
Example: "5:1642598400"
```

### Cache Data Structures

#### Entity Cache (JSON)

All cached entities are stored as JSON-serialized versions of their API structures:

```json
cache:threat_model:{id} = {
    "id": "uuid",
    "name": "string",
    "description": "string",
    "owner_email": "string",
    "created_by": "string",
    "threat_model_framework": "CIA|STRIDE|LINDDUN|DIE|PLOT4ai",
    "issue_url": "string",
    "document_count": 0,
    "source_count": 0,
    "diagram_count": 0,
    "threat_count": 0,
    "created_at": "RFC3339 timestamp",
    "modified_at": "RFC3339 timestamp"
}
```

#### Metadata Cache (JSON Array)

```json
cache:metadata:{entity_type}:{entity_id} = [
    {
        "id": "uuid",
        "entity_type": "threat_model|threat|diagram|document|source|cell",
        "entity_id": "uuid",
        "key": "string (alphanumeric, dash, underscore only)",
        "value": "string",
        "created_at": "RFC3339 timestamp",
        "modified_at": "RFC3339 timestamp"
    }
]
```

#### Authorization Data Cache (JSON)

```json
cache:auth:{threat_model_id} = {
    "threat_model_id": "uuid",
    "owner_email": "string",
    "user_permissions": {
        "user@example.com": "owner|writer|reader"
    },
    "cached_at": "RFC3339 timestamp"
}
```

#### List Cache (JSON)

```json
cache:list:{entity_type}:{parent_id}:{offset}:{limit} = {
    "items": [...], // Array of entity objects
    "total_count": 0,
    "offset": 0,
    "limit": 50,
    "cached_at": "RFC3339 timestamp"
}
```

#### Diagram Cells Cache (JSON Array)

```json
cache:cells:{diagram_id} = [
    {
        "id": "string",
        "type": "string",
        "data": {...}, // Arbitrary JSON object
        "metadata": [
            {"key": "string", "value": "string"}
        ]
    }
]
```

## Cache TTL Strategy

The caching layer uses differentiated TTL strategies based on data characteristics and access patterns:

### TTL Configuration

| Cache Type        | TTL        | Justification                                                |
| ----------------- | ---------- | ------------------------------------------------------------ |
| **Threat Models** | 10 minutes | Core entities, moderate update frequency                     |
| **Diagrams**      | 2 minutes  | High collaboration, real-time updates                        |
| **Sub-resources** | 5 minutes  | Threats, documents, sources - balanced consistency           |
| **Authorization** | 15 minutes | Security-critical, infrequent changes                        |
| **Metadata**      | 7 minutes  | Flexible data, moderate update frequency                     |
| **Lists**         | 5 minutes  | Paginated results, balance between performance and freshness |

### Cache Invalidation

The application implements proactive cache invalidation through the `CacheService`:

- **Entity Updates**: Individual entity caches are invalidated on modification
- **Metadata Changes**: Entity-specific metadata caches are cleared
- **Authorization Updates**: Auth data cache is invalidated on role changes
- **Cascade Invalidation**: Parent entity updates trigger related cache clearing

## Validation Rules

### Key Pattern Validation

1. All keys must match defined hierarchical patterns
2. No spaces allowed in keys
3. Use lowercase for namespace and type components
4. UUIDs must be lowercase and valid UUID v4 format
5. Entity types must match defined values: `threat_model|threat|diagram|document|source|cell`

### TTL Requirements

1. All cache data MUST have explicit TTL
2. Session data: 24 hours maximum
3. Cache data: Variable based on entity type (2-15 minutes)
4. Lock data: 30 seconds maximum
5. OAuth state: 10 minutes maximum
6. Rate limit data: 1 minute to 1 hour based on scope

### Data Validation

1. **Timestamps**: All timestamps in RFC3339 format
2. **UUIDs**: Valid UUID v4 format for all entity IDs
3. **JSON**: All cached entities stored as valid JSON
4. **Metadata Keys**: Match regex `^[a-zA-Z0-9_-]+$`
5. **Entity Types**: Must match database enum values
6. **Framework Types**: Must match supported values (CIA, STRIDE, LINDDUN, DIE, PLOT4ai)

## Memory Management

### Key Expiration Policy

1. All temporary data uses explicit TTL
2. Automated cleanup of expired keys
3. Redis configured with `volatile-lru` eviction policy
4. Monitor key count and memory usage by pattern

### Memory Optimization

1. **Value Size Limits**: Maximum 512KB per cached entity
2. **List Pagination**: Cache paginated results to prevent large memory usage
3. **Compression**: JSON entities are stored uncompressed for development simplicity
4. **Key Monitoring**: Track memory usage by key pattern

### Performance Patterns

1. **Write-Through Caching**: Entities cached immediately after creation/update
2. **Cache-Aside**: Read operations check cache first, populate on miss
3. **Bulk Invalidation**: Efficient clearing of related cache entries
4. **List Caching**: Paginated list results cached with offset/limit keys

## Monitoring & Observability

The TMI application includes comprehensive Redis monitoring through OpenTelemetry integration, providing detailed metrics and tracing for all Redis operations.

### OpenTelemetry Metrics

The application automatically instruments Redis operations with the following metrics:

| Metric Name                          | Type      | Description                              |
| ------------------------------------ | --------- | ---------------------------------------- |
| `redis_operations_total`             | Counter   | Total number of Redis operations         |
| `redis_operation_duration_seconds`   | Histogram | Duration of Redis operations             |
| `redis_cache_hits_total`             | Counter   | Total number of cache hits              |
| `redis_cache_misses_total`           | Counter   | Total number of cache misses            |
| `redis_memory_usage_bytes`           | Gauge     | Redis memory usage in bytes             |
| `redis_connections_active`           | Gauge     | Number of active Redis connections      |
| `redis_keyspace_operations_total`    | Counter   | Total keyspace operations by type       |

### Distributed Tracing

All Redis operations are traced with the following span attributes:

- **Operation Context**: `db.system=redis`, `db.operation=GET/SET/DEL`
- **Cache Classification**: `tmi.cache.type` (threat_model, diagram, auth, etc.)
- **Key Information**: Sanitized key patterns (sensitive data redacted)
- **Performance Data**: Duration, value size, hit/miss status
- **Error Information**: Detailed error context for failed operations

### Key Metrics

1. **Cache Hit Ratios**: Per entity type and overall
2. **Memory Usage**: By key pattern and total utilization
3. **Key Count Distribution**: Track key patterns and growth
4. **TTL Distribution**: Monitor expiration patterns
5. **Cache Invalidation Rate**: Track proactive invalidations
6. **Operation Latency**: Histogram buckets from 0.1ms to 500ms
7. **Connection Pool Stats**: Active, idle, and total connections

### Health Checks

1. **Redis Connectivity**: Connection pool health monitoring
2. **Memory Pressure**: Alert on high memory utilization
3. **Slow Queries**: Monitor Redis slow log
4. **Connection Count**: Track active client connections
5. **Key Growth**: Alert on unexpected key count increases
6. **Pool Exhaustion**: Monitor connection pool utilization
7. **Network Latency**: Track Redis operation response times

### Performance Monitoring

1. **Cache Response Times**: Measure GET/SET operation latency with histogram buckets
2. **Hit Rate by Entity**: Track cache effectiveness per entity type (threat_model, diagram, auth, etc.)
3. **Invalidation Impact**: Monitor cache misses after invalidation
4. **Memory Efficiency**: Track memory usage vs hit rates
5. **Operation Classification**: Separate metrics for read vs write operations
6. **Key Pattern Analysis**: Monitor key distribution and access patterns

## Security & Access Control

### Data Security

1. **No Sensitive Storage**: Passwords never stored in Redis
2. **Token Security**: JWT tokens stored with appropriate expiry
3. **Short TTLs**: Security-critical auth data uses short TTLs
4. **Data Isolation**: Environment-specific database separation

### Access Control

1. **Redis ACL**: Application-specific user with limited command access
2. **TLS Encryption**: Secure transport for Redis connections
3. **Network Isolation**: Redis accessible only from application servers
4. **Audit Logging**: Log cache access patterns and invalidations

### Development vs Production

**Development Environment**:

- Database 0 for caching
- Longer TTLs for debugging
- Additional logging enabled
- Memory usage monitoring relaxed

**Production Environment**:

- Dedicated cache cluster
- Strict TTL enforcement
- Comprehensive monitoring
- Automated failover support

This Redis schema supports the TMI application's caching requirements with performance optimization, security considerations, and operational monitoring capabilities.
