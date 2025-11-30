# Webhook Configuration and Operations

This guide covers deployment, configuration, and operational management of TMI's webhook subscription system.

## Architecture Overview

The webhook system consists of four worker threads and supporting infrastructure:

```
┌─────────────────────────────────────────────────────────────────┐
│                         TMI Server                              │
│                                                                 │
│  ┌──────────────┐    ┌─────────────────┐    ┌──────────────┐  │
│  │  Event       │    │  Redis Streams  │    │  PostgreSQL  │  │
│  │  Emitter     │───▶│  tmi:events     │◀───│  webhook_*   │  │
│  └──────────────┘    └─────────────────┘    │  tables      │  │
│                              │               └──────────────┘  │
│                              │                                 │
│                              ▼                                 │
│  ┌─────────────────────────────────────────────────────┐      │
│  │            Webhook Workers                          │      │
│  │                                                     │      │
│  │  ┌────────────────┐  ┌────────────────┐          │      │
│  │  │ Event Consumer │  │ Challenge      │          │      │
│  │  │ (XREADGROUP)   │  │ Worker         │          │      │
│  │  └────────────────┘  └────────────────┘          │      │
│  │                                                   │      │
│  │  ┌────────────────┐  ┌────────────────┐          │      │
│  │  │ Delivery       │  │ Cleanup        │          │      │
│  │  │ Worker         │  │ Worker         │          │      │
│  │  └────────────────┘  └────────────────┘          │      │
│  └───────────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────────────┘
```

## Prerequisites

### Required Services

1. **PostgreSQL** (tested with 13+)
   - Database schema migration 002_business_domain must be applied
   - Tables: `webhook_subscriptions`, `webhook_deliveries`, `webhook_quotas`, `webhook_url_deny_list`

2. **Redis** (tested with 6+)
   - Redis Streams support required
   - Used for: event queuing, rate limiting, deduplication

### Environment Variables

```bash
# Database Configuration
DATABASE_URL=postgres://user:pass@localhost:5432/tmi

# Redis Configuration
REDIS_URL=redis://localhost:6379/0

# Server Configuration
SERVER_PORT=8080
SERVER_HOST=0.0.0.0

# TLS (optional but recommended)
TLS_ENABLED=true
TLS_CERT_FILE=/path/to/cert.pem
TLS_KEY_FILE=/path/to/key.pem

# Logging
LOG_LEVEL=info  # debug, info, warn, error
LOG_FORMAT=json # json or text
```

## Database Schema Migration

Apply the webhook schema migration:

```bash
# Using migrate tool
./bin/migrate up

# Or manually apply
psql -d tmi -f auth/migrations/005_webhooks.up.sql
```

### Schema Verification

```sql
-- Verify tables exist
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name LIKE 'webhook_%';

-- Expected tables:
-- webhook_subscriptions
-- webhook_deliveries
-- webhook_quotas
-- webhook_url_deny_list

-- Check trigger exists
SELECT trigger_name
FROM information_schema.triggers
WHERE event_object_table = 'webhook_subscriptions';

-- Expected: webhook_subscription_change_notify
```

## Worker Configuration

### Worker Startup

Workers start automatically when the server starts if Redis and PostgreSQL are available:

```go
// In cmd/server/main.go
webhookConsumer, challengeWorker, deliveryWorker, cleanupWorker := startWebhookWorkers(ctx)
```

### Worker Intervals

Default intervals (configured in source code):

- **Event Consumer**: Continuous (XREADGROUP with 5-second block)
- **Challenge Worker**: Every 30 seconds
- **Delivery Worker**: Every 5 seconds
- **Cleanup Worker**: Every 1 hour

### Worker Health Checks

Monitor worker health through logs:

```bash
# Event consumer activity
tail -f /var/log/tmi/server.log | grep "webhook-consumer"

# Challenge verification
tail -f /var/log/tmi/server.log | grep "challenge worker"

# Delivery attempts
tail -f /var/log/tmi/server.log | grep "delivering webhook"

# Cleanup operations
tail -f /var/log/tmi/server.log | grep "cleanup worker"
```

## Redis Configuration

### Streams Setup

Redis Streams are created automatically:

```redis
# Check stream exists
XINFO STREAM tmi:events

# Check consumer groups
XINFO GROUPS tmi:events

# Expected group: webhook-consumers
```

### Consumer Group Management

```redis
# View consumers in group
XINFO CONSUMERS tmi:events webhook-consumers

# Remove stale consumer
XGROUP DELCONSUMER tmi:events webhook-consumers consumer-123456

# Reset consumer group (careful: reprocesses all messages)
XGROUP SETID tmi:events webhook-consumers 0
```

### Rate Limiting Keys

Rate limiting uses Redis sorted sets:

```redis
# View subscription rate limit keys
KEYS webhook:ratelimit:sub:*

# View event rate limit keys
KEYS webhook:ratelimit:events:*

# Check current count
ZCOUNT webhook:ratelimit:sub:minute:<owner-id> -inf +inf

# Clear rate limit (emergency)
DEL webhook:ratelimit:sub:minute:<owner-id>
```

## Default Quotas

Built-in quota defaults (defined in source):

```go
const (
    DefaultMaxSubscriptions                 = 10
    DefaultMaxEventsPerMinute               = 100
    DefaultMaxSubscriptionRequestsPerMinute = 5
    DefaultMaxSubscriptionRequestsPerDay    = 100
)
```

### Custom Quotas

Set custom quotas via API (requires admin JWT):

```bash
curl -X POST https://tmi.example.com/webhook/quotas \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "owner_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "max_subscriptions": 50,
    "max_subscription_requests_per_minute": 10,
    "max_subscription_requests_per_day": 500,
    "max_events_per_minute": 1000
  }'
```

### Query Quotas

```sql
-- View all custom quotas
SELECT owner_id, max_subscriptions, max_events_per_minute
FROM webhook_quotas
ORDER BY created_at DESC;

-- Owner with most subscriptions
SELECT ws.owner_id, COUNT(*) as subscription_count
FROM webhook_subscriptions ws
WHERE ws.status = 'active'
GROUP BY ws.owner_id
ORDER BY subscription_count DESC
LIMIT 10;
```

## Deny List Management

The URL deny list prevents SSRF attacks by blocking webhook URLs matching specific patterns.

### Default Deny List

TMI includes built-in patterns (not in database):

- Localhost: `localhost`, `127.0.0.1`, `::1`
- Private IPs: `10.*`, `192.168.*`, `172.16.*` to `172.31.*`
- Link-local: `169.254.*`, `fe80::`
- Cloud metadata: AWS, GCP, Azure, DigitalOcean endpoints

### Add Custom Patterns

```bash
# Block specific domain (glob)
curl -X POST https://tmi.example.com/webhook/deny-list \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "pattern": "*.internal.company.com",
    "pattern_type": "glob",
    "description": "Block internal domains"
  }'

# Block IP range (regex)
curl -X POST https://tmi.example.com/webhook/deny-list \
  -H "Authorization: Bearer <admin-jwt>" \
  -H "Content-Type: application/json" \
  -d '{
    "pattern": "203\\.0\\.113\\..*",
    "pattern_type": "regex",
    "description": "Block TEST-NET-3 range"
  }'
```

### List Deny List Entries

```bash
curl https://tmi.example.com/webhook/deny-list \
  -H "Authorization: Bearer <admin-jwt>"
```

### Remove Entry

```bash
curl -X DELETE https://tmi.example.com/webhook/deny-list/<id> \
  -H "Authorization: Bearer <admin-jwt>"
```

## Operational Tasks

### Monitor Subscription Health

```sql
-- Active subscriptions
SELECT COUNT(*) as active_subscriptions
FROM webhook_subscriptions
WHERE status = 'active';

-- Pending verification (stuck?)
SELECT id, name, url, challenges_sent, created_at
FROM webhook_subscriptions
WHERE status = 'pending_verification'
  AND created_at < NOW() - INTERVAL '1 hour'
ORDER BY created_at;

-- High failure rate
SELECT id, name, url, publication_failures, last_successful_use
FROM webhook_subscriptions
WHERE status = 'active'
  AND publication_failures > 5
ORDER BY publication_failures DESC;

-- Idle subscriptions
SELECT id, name, url, last_successful_use
FROM webhook_subscriptions
WHERE status = 'active'
  AND (last_successful_use IS NULL OR last_successful_use < NOW() - INTERVAL '30 days')
ORDER BY last_successful_use NULLS FIRST;
```

### Monitor Delivery Performance

```sql
-- Delivery status breakdown
SELECT status, COUNT(*) as count
FROM webhook_deliveries
GROUP BY status
ORDER BY count DESC;

-- Recent failures
SELECT wd.id, wd.subscription_id, ws.url, wd.attempts, wd.error_message, wd.created_at
FROM webhook_deliveries wd
JOIN webhook_subscriptions ws ON ws.id = wd.subscription_id
WHERE wd.status = 'failed'
  AND wd.created_at > NOW() - INTERVAL '1 hour'
ORDER BY wd.created_at DESC
LIMIT 20;

-- Pending deliveries (backlog)
SELECT COUNT(*) as pending_count
FROM webhook_deliveries
WHERE status = 'pending'
   OR (status = 'retry' AND next_retry_at <= NOW());

-- Avg delivery attempts
SELECT AVG(attempts) as avg_attempts, MAX(attempts) as max_attempts
FROM webhook_deliveries
WHERE status != 'pending';
```

### Cleanup Operations

```sql
-- Old deliveries (manual cleanup if worker not running)
DELETE FROM webhook_deliveries
WHERE created_at < NOW() - INTERVAL '30 days';

-- Orphaned deliveries (subscription deleted)
DELETE FROM webhook_deliveries
WHERE subscription_id NOT IN (SELECT id FROM webhook_subscriptions);

-- Force delete broken subscription
DELETE FROM webhook_subscriptions
WHERE id = '<subscription-id>';
```

### Force Subscription Verification

```sql
-- Reset subscription to retry verification
UPDATE webhook_subscriptions
SET status = 'pending_verification',
    challenges_sent = 0,
    challenge = NULL
WHERE id = '<subscription-id>';
```

### Force Retry Failed Delivery

```sql
-- Reset delivery for immediate retry
UPDATE webhook_deliveries
SET status = 'pending',
    next_retry_at = NULL,
    error_message = NULL
WHERE id = '<delivery-id>';
```

## Scaling Considerations

### Horizontal Scaling

The event consumer uses Redis consumer groups for horizontal scaling:

1. **Multiple Server Instances**: Each server runs its own consumer with unique ID
2. **Consumer Group**: All consumers join the `webhook-consumers` group
3. **Message Distribution**: Redis distributes messages across consumers
4. **Idempotency**: Delivery records prevent duplicate processing

### Vertical Scaling

Adjust worker concurrency in source code:

```go
// In webhook_delivery_worker.go
// Increase concurrent delivery workers
for i := 0; i < 10; i++ {  // Default is 1, increase for more throughput
    go w.processLoop(ctx)
}
```

### Database Performance

Optimize database for webhook workload:

```sql
-- Existing indexes
-- (defined in migration 005_webhooks.up.sql)

-- Additional index for delivery performance
CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_status_retry
ON webhook_deliveries(status, next_retry_at)
WHERE status = 'retry';

-- Index for cleanup queries
CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_last_use
ON webhook_subscriptions(status, last_successful_use)
WHERE status = 'active';
```

### Redis Memory

Configure Redis with appropriate memory limits:

```redis
# In redis.conf
maxmemory 2gb
maxmemory-policy allkeys-lru

# Stream retention (auto-trim old events)
# In webhook_event_consumer.go XADD call
XADD tmi:events MAXLEN ~ 10000 * field value
```

## Backup and Disaster Recovery

### Database Backup

Webhook tables to include in backups:

```bash
# Backup webhook tables
pg_dump -t webhook_subscriptions \
        -t webhook_deliveries \
        -t webhook_quotas \
        -t webhook_url_deny_list \
        tmi > webhook_backup.sql

# Restore
psql tmi < webhook_backup.sql
```

### Redis Backup

Redis Stream data is transient - no backup needed for `tmi:events`.

Rate limiting keys expire automatically - no backup needed.

### Subscription Export

Export active subscriptions for audit:

```sql
COPY (
  SELECT id, owner_id, threat_model_id, name, url, events,
         status, created_at, last_successful_use
  FROM webhook_subscriptions
  WHERE status = 'active'
) TO '/tmp/webhook_subscriptions_export.csv' CSV HEADER;
```

## Monitoring and Alerting

### Key Metrics to Monitor

1. **Subscription Health**
   - Total active subscriptions
   - Pending verification count (alert if > 10 for > 1 hour)
   - High failure rate (alert if > 5 subscriptions with > 10 failures)

2. **Delivery Performance**
   - Pending delivery backlog (alert if > 1000)
   - Failed deliveries (alert if > 100/hour)
   - Average delivery latency (alert if > 30s)

3. **Worker Health**
   - Event consumer lag (check Redis XPENDING)
   - Worker crash/restart events
   - Redis connection errors

4. **Rate Limiting**
   - Rate limit rejections (alert on sudden spike)
   - Quota exhaustion events

### Sample Prometheus Queries

```promql
# Active subscriptions
webhook_subscriptions_total{status="active"}

# Delivery success rate
rate(webhook_deliveries_total{status="delivered"}[5m]) /
rate(webhook_deliveries_total[5m])

# Delivery latency (p99)
histogram_quantile(0.99, webhook_delivery_duration_seconds_bucket)

# Event consumer lag
redis_stream_pending_messages{stream="tmi:events"}
```

### Log Monitoring

Key log patterns to alert on:

```bash
# Worker failures
grep "ERROR.*webhook" /var/log/tmi/server.log

# High retry rate
grep "delivery.*failed.*will retry" /var/log/tmi/server.log | wc -l

# Redis connection issues
grep "Redis.*connection.*failed" /var/log/tmi/server.log

# Challenge verification failures
grep "challenge verification failed" /var/log/tmi/server.log
```

## Troubleshooting

### Workers Not Starting

**Symptom**: No webhook activity in logs

**Diagnosis**:
```bash
# Check server logs for startup errors
grep "webhook.*worker" /var/log/tmi/server.log | grep -i error

# Check Redis availability
redis-cli ping

# Check PostgreSQL connectivity
psql -d tmi -c "SELECT 1"
```

**Solutions**:
1. Verify Redis URL is correct and accessible
2. Verify PostgreSQL URL is correct
3. Check webhook migration is applied
4. Restart server process

### Events Not Being Delivered

**Symptom**: Subscriptions active but no deliveries

**Diagnosis**:
```sql
-- Check if events are being emitted
SELECT event_type, COUNT(*)
FROM webhook_deliveries
WHERE created_at > NOW() - INTERVAL '1 hour'
GROUP BY event_type;

-- Check consumer group lag
-- (Redis command)
XPENDING tmi:events webhook-consumers
```

**Solutions**:
1. Verify CRUD operations are emitting events
2. Check event consumer worker is running
3. Verify Redis Streams connectivity
4. Check delivery worker is running

### High Failure Rate

**Symptom**: Many deliveries failing

**Diagnosis**:
```sql
-- Top failure reasons
SELECT error_message, COUNT(*) as count
FROM webhook_deliveries
WHERE status = 'failed'
  AND created_at > NOW() - INTERVAL '24 hours'
GROUP BY error_message
ORDER BY count DESC
LIMIT 10;
```

**Solutions**:
1. Check webhook endpoint availability
2. Verify network connectivity from server
3. Check for firewall/security group issues
4. Verify SSL/TLS certificates
5. Check endpoint response times (timeout = 30s)

### Memory Issues

**Symptom**: High Redis memory usage

**Diagnosis**:
```redis
# Check memory usage
INFO memory

# Check stream length
XLEN tmi:events

# Check rate limit key count
SCAN 0 MATCH webhook:ratelimit:* COUNT 1000
```

**Solutions**:
1. Add MAXLEN to XADD to auto-trim stream
2. Clean up old rate limit keys (TTL should auto-expire)
3. Increase Redis memory limit
4. Tune delivery worker interval to process faster

## Security Hardening

### URL Validation

1. **Deny List**: Maintain comprehensive deny list
2. **Egress Filtering**: Restrict server outbound connections
3. **Network Segmentation**: Isolate webhook workers

### Secrets Management

1. **HMAC Secrets**: Encourage users to set strong secrets
2. **Secret Rotation**: Provide API for secret updates
3. **Secret Storage**: Secrets stored plaintext in DB (consider encryption at rest)

### Access Control

1. **Owner Scoping**: Subscriptions are owner-scoped by default
2. **Admin APIs**: Deny list and quota APIs require admin role
3. **Rate Limiting**: Prevents abuse via request flooding

### Audit Logging

Log all webhook operations:

```sql
-- Admin operations (create/delete deny list, quotas)
SELECT * FROM audit_log
WHERE action LIKE 'webhook.%'
  AND timestamp > NOW() - INTERVAL '7 days'
ORDER BY timestamp DESC;
```

## Performance Tuning

### Database Connection Pool

```go
// In auth/database.go
db.SetMaxOpenConns(25)       // Increase for more concurrent workers
db.SetMaxIdleConns(5)
db.SetConnMaxLifetime(time.Hour)
```

### Redis Pipelining

Consider batching Redis operations in delivery worker:

```go
// Instead of individual commands
pipe := r.redisClient.Pipeline()
// ... batch operations
pipe.Exec(ctx)
```

### Delivery Worker Batch Size

Increase batch size in `processPendingDeliveries()`:

```go
// Default: 50 deliveries per batch
deliveries, err := GlobalWebhookDeliveryStore.ListPending(100)
```

## See Also

- [Webhook Integration Guide](../developer/integration/webhook-subscriptions.md) - Developer documentation
- [Database Schema Reference](../reference/schema/database-schema.md) - Schema details
- [Redis Configuration](redis-configuration.md) - Redis setup
- [Observability](observability.md) - Metrics and monitoring
