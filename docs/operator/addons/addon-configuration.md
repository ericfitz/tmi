# Add-ons Configuration Guide

**Audience:** System Operators, Administrators
**Version:** 1.0
**Last Updated:** 2025-11-08

## Overview

Add-ons enable extensibility in TMI by allowing administrators to register webhook-based integrations that users can invoke on-demand. This guide covers configuration, quota management, and operational considerations for the add-ons feature.

## Administrator Configuration

### Setting Up Administrators

Administrators are the only users who can create and delete add-ons. Configure administrators in your YAML configuration file:

**config-development.yml:**
```yaml
administrators:
  - subject: "admin@example.com"
    subject_type: "user"
  - subject: "security-team"
    subject_type: "group"
  - subject: "platform-admins"
    subject_type: "group"
```

**Fields:**
- `subject`: Email address (for users) or group name (for groups)
- `subject_type`: Either `"user"` or `"group"`

**User-based Admins:**
- Identified by email address from JWT claims
- Matches against `userEmail` in authentication context

**Group-based Admins:**
- Identified by group name from JWT `groups` claim
- All members of the group are administrators
- Useful for centralized team management

### Admin Verification

On server startup, administrators from the config file are loaded into the database. You can verify admin status:

```bash
# Check server logs for admin loading
grep "Administrator created" logs/tmi.log
```

## Database Schema

### Tables Created

The add-ons feature creates three tables via migration `006_addons.up.sql`:

1. **administrators** - Admin user/group ACL
2. **addons** - Add-on registrations
3. **addon_invocation_quotas** - Per-user rate limits

### Schema Details

**administrators:**
```sql
CREATE TABLE administrators (
    user_id UUID NOT NULL,
    subject VARCHAR(255) NOT NULL,
    subject_type VARCHAR(20) NOT NULL,  -- 'user' or 'group'
    granted_at TIMESTAMPTZ NOT NULL,
    granted_by UUID,
    notes TEXT,
    PRIMARY KEY (user_id, subject, subject_type)
);
```

**addons:**
```sql
CREATE TABLE addons (
    id UUID PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL,
    name VARCHAR(255) NOT NULL,
    webhook_id UUID NOT NULL,  -- FK to webhook_subscriptions
    description TEXT,
    icon VARCHAR(60),
    objects TEXT[],
    threat_model_id UUID
);
```

**addon_invocation_quotas:**
```sql
CREATE TABLE addon_invocation_quotas (
    owner_id UUID PRIMARY KEY,
    max_active_invocations INT NOT NULL DEFAULT 1,
    max_invocations_per_hour INT NOT NULL DEFAULT 10,
    created_at TIMESTAMPTZ NOT NULL,
    modified_at TIMESTAMPTZ NOT NULL
);
```

## Rate Limits and Quotas

### Default Quotas

All users have the following default limits:
- **Active invocations:** 1 concurrent (pending or in_progress)
- **Hourly rate:** 10 invocations per hour (sliding window)

### Custom Quotas

Set custom quotas for specific users via database:

```sql
-- Set custom quota for user
INSERT INTO addon_invocation_quotas (owner_id, max_active_invocations, max_invocations_per_hour)
VALUES ('user-uuid-here', 5, 50)
ON CONFLICT (owner_id) DO UPDATE
SET max_active_invocations = EXCLUDED.max_active_invocations,
    max_invocations_per_hour = EXCLUDED.max_invocations_per_hour;
```

### Monitoring Rate Limits

Check Redis for rate limit data:

```bash
# View hourly rate limit entries for a user
redis-cli ZRANGE addon:ratelimit:hour:{user_id} 0 -1 WITHSCORES

# Check active invocation for a user
redis-cli GET addon:active:{user_id}
```

## Invocation Storage (Redis)

### Configuration

Invocations are stored in Redis with a 7-day TTL:

**Key Pattern:** `addon:invocation:{invocation_id}`
**TTL:** 604,800 seconds (7 days)
**Format:** JSON

### Data Structure

```json
{
  "id": "uuid",
  "addon_id": "uuid",
  "threat_model_id": "uuid",
  "object_type": "asset",
  "object_id": "uuid",
  "invoked_by": "user_id",
  "payload": "{\"key\":\"value\"}",
  "status": "in_progress",
  "status_percent": 50,
  "status_message": "Processing...",
  "created_at": "2025-11-08T12:00:00Z",
  "status_updated_at": "2025-11-08T12:01:30Z"
}
```

### Monitoring Invocations

```bash
# Count all invocations
redis-cli KEYS "addon:invocation:*" | wc -l

# View specific invocation
redis-cli GET addon:invocation:{invocation_id}

# Scan for invocations (paginated)
redis-cli SCAN 0 MATCH "addon:invocation:*" COUNT 100
```

## Webhook Integration

### Prerequisites

Before creating add-ons, you must have:
1. Active webhook subscription (status: `active`)
2. Webhook URL accessible from TMI server
3. Webhook secret configured for HMAC verification

### Add-on Registration

Admins register add-ons via API:

```bash
POST /addons
Authorization: Bearer {admin_jwt}
Content-Type: application/json

{
  "name": "STRIDE Analysis",
  "webhook_id": "uuid-of-webhook-subscription",
  "description": "Performs automated STRIDE threat analysis",
  "icon": "material-symbols:security",
  "objects": ["threat_model", "asset"],
  "threat_model_id": "uuid"  # Optional: scope to specific TM
}
```

### Invocation Flow

1. User invokes add-on: `POST /addons/{id}/invoke`
2. TMI checks rate limits (1 active, 10/hour)
3. TMI creates invocation in Redis (status: `pending`)
4. TMI queues invocation for webhook worker
5. Worker sends HTTPS POST to webhook URL with HMAC signature
6. Webhook processes and calls back to update status
7. Invocation status updated via `POST /invocations/{id}/status`

### Webhook Payload

Webhooks receive:

```json
{
  "event_type": "addon_invocation",
  "invocation_id": "uuid",
  "addon_id": "uuid",
  "threat_model_id": "uuid",
  "object_type": "asset",
  "object_id": "uuid",
  "timestamp": "2025-11-08T12:00:00Z",
  "payload": { /* user data, max 1KB */ },
  "callback_url": "https://tmi.example.com/invocations/{id}/status"
}
```

**Headers:**
- `Content-Type: application/json`
- `X-Webhook-Event: addon_invocation`
- `X-Invocation-Id: {uuid}`
- `X-Addon-Id: {uuid}`
- `X-Webhook-Signature: sha256={hmac_hex}`
- `User-Agent: TMI-Addon-Worker/1.0`

## Deletion Behavior

### Add-on Deletion

**Blocked Deletion:**
- DELETE will fail with `409 Conflict` if add-on has active invocations
- Active = status is `pending` or `in_progress`

**Successful Deletion:**
- Only allowed when no active invocations exist
- Cascades when webhook is deleted (ON DELETE CASCADE)

### Webhook Cascade

When a webhook subscription is deleted:
- All associated add-ons are automatically deleted
- No blocking check (webhook owner's decision)

## Troubleshooting

### Common Issues

**Issue: Users hitting rate limits**
```
Error: "Hourly invocation limit exceeded: 10/10"
```
**Solution:** Check if legitimate usage, increase quota in database

**Issue: Invocations stuck in pending**
```
Status: pending, no status updates
```
**Solution:**
- Check if webhook worker is running
- Verify webhook URL is accessible
- Check webhook subscription status (must be `active`)

**Issue: Status update returns 401 Unauthorized**
```
Error: "Invalid webhook signature"
```
**Solution:**
- Verify webhook secret matches
- Check HMAC signature generation in webhook service
- Ensure request body hasn't been modified

### Logs

Check logs for add-on operations:

```bash
# Add-on creation/deletion
grep "Add-on created\|Add-on deleted" logs/tmi.log

# Invocations
grep "Add-on invoked\|Invocation created" logs/tmi.log

# Rate limits
grep "rate limit exceeded" logs/tmi.log

# Worker activity
grep "addon invocation worker\|addon invocation sent" logs/tmi.log
```

## Security Considerations

### HMAC Verification

- All webhook invocations include HMAC-SHA256 signatures
- Status updates MUST verify HMAC before accepting changes
- Use constant-time comparison to prevent timing attacks
- Signature format: `sha256={hex_encoded_hmac}`

### Access Control

- Only administrators can create/delete add-ons
- All authenticated users can invoke add-ons and view their invocations
- Admins can view all invocations (for support/debugging)

### Payload Validation

- User payloads limited to 1KB to prevent abuse
- XSS prevention on add-on name and description
- Icon format strictly validated (Material Symbols or FontAwesome)

## Performance Tuning

### Redis Configuration

For high-volume deployments:

```conf
# Increase max memory
maxmemory 2gb

# Use LRU eviction (don't evict invocations)
maxmemory-policy allkeys-lru

# Persistence for invocation durability
save 900 1
save 300 10
save 60 10000
```

### Rate Limit Tuning

Adjust quotas based on usage patterns:

```sql
-- High-volume users
UPDATE addon_invocation_quotas
SET max_active_invocations = 5,
    max_invocations_per_hour = 100
WHERE owner_id = 'high-volume-user-uuid';
```

## Monitoring Metrics

### Key Metrics to Track

1. **Invocation Rate:** Invocations created per hour
2. **Success Rate:** Completed / (Completed + Failed)
3. **Average Duration:** Time from created to completed
4. **Rate Limit Hits:** 429 responses per hour
5. **Active Invocations:** Current count by status

### Sample Queries

```sql
-- Count add-ons by webhook
SELECT webhook_id, COUNT(*)
FROM addons
GROUP BY webhook_id;

-- Check quota usage
SELECT owner_id, max_invocations_per_hour
FROM addon_invocation_quotas
ORDER BY max_invocations_per_hour DESC;
```

## Backup and Recovery

### Database Backup

Include add-on tables in regular backups:

```bash
pg_dump -t administrators -t addons -t addon_invocation_quotas tmi_prod > addons_backup.sql
```

### Redis Backup

Invocations are ephemeral (7-day TTL), but for critical deployments:

```bash
# Enable RDB snapshots
redis-cli BGSAVE

# Or enable AOF for durability
redis-cli CONFIG SET appendonly yes
```

## Related Documentation

- [Webhook Configuration](../webhook-configuration.md) - Webhook setup guide
- [Add-on Development Guide](../../developer/addons/addon-development-guide.md) - Building webhook services
- [API Reference](../../reference/apis/tmi-openapi.json) - Complete API specification
