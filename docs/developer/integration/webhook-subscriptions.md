# Webhook Subscriptions

This guide explains how to integrate with TMI's webhook subscription system to receive real-time notifications when threat models, diagrams, or documents change.

## Overview

TMI's webhook system allows you to subscribe to events and receive HTTP POST notifications when those events occur. This enables real-time integrations with external systems such as:

- Security Information and Event Management (SIEM) systems
- Ticketing systems (Jira, ServiceNow)
- CI/CD pipelines
- Custom notification services
- Analytics platforms

## Architecture

The webhook system consists of several components:

1. **Event Emission**: When CRUD operations occur on threat models, diagrams, or documents, events are emitted to Redis Streams
2. **Event Consumer**: Processes events from Redis Streams and creates delivery records
3. **Challenge Worker**: Verifies new subscriptions using challenge-response protocol
4. **Delivery Worker**: Delivers webhooks to subscribed endpoints with retries and exponential backoff
5. **Cleanup Worker**: Removes old delivery records and marks idle/broken subscriptions for deletion
6. **Rate Limiter**: Prevents abuse using Redis-based sliding window rate limiting

## Subscription Lifecycle

### 1. Create Subscription

Create a webhook subscription specifying:

- `name`: Descriptive name for the subscription
- `url`: HTTPS endpoint to receive webhooks (must pass validation)
- `events`: Array of event types to subscribe to
- `threat_model_id` (optional): Filter events to specific threat model
- `secret` (optional): Shared secret for HMAC-SHA256 signature verification

**Example Request:**

```http
POST /webhook/subscriptions
Authorization: Bearer <jwt-token>
Content-Type: application/json

{
  "name": "My SIEM Integration",
  "url": "https://api.example.com/webhooks/tmi",
  "events": ["threat_model.created", "threat_model.updated", "threat_model.deleted"],
  "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
  "secret": "my-shared-secret-key"
}
```

**Response:**

```json
{
  "id": "7d8f6e5c-4b3a-2190-8765-fedcba987654",
  "owner_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
  "name": "My SIEM Integration",
  "url": "https://api.example.com/webhooks/tmi",
  "events": ["threat_model.created", "threat_model.updated", "threat_model.deleted"],
  "status": "pending_verification",
  "challenges_sent": 0,
  "created_at": "2025-01-15T10:30:00Z",
  "modified_at": "2025-01-15T10:30:00Z",
  "publication_failures": 0
}
```

### 2. Verification (Challenge-Response)

After creation, the subscription enters `pending_verification` status. TMI sends a challenge request to your endpoint:

**Challenge Request:**

```http
POST https://api.example.com/webhooks/tmi
Content-Type: application/json
X-Webhook-Event: webhook.challenge
X-Webhook-Challenge: abc123def456

{
  "type": "webhook.challenge",
  "challenge": "abc123def456"
}
```

**Required Response:**

Your endpoint must return the challenge within the response body:

```json
{
  "challenge": "abc123def456"
}
```

- Maximum 3 challenge attempts
- Subscription becomes `active` on successful verification
- Fails if verification not completed

### 3. Active Subscription

Once verified, your subscription is `active` and will receive event notifications:

**Event Notification Example:**

```http
POST https://api.example.com/webhooks/tmi
Content-Type: application/json
X-Webhook-Event: threat_model.created
X-Webhook-Delivery-Id: 7fa85f64-5717-4562-b3fc-2c963f66afa6
X-Webhook-Subscription-Id: 7d8f6e5c-4b3a-2190-8765-fedcba987654
X-Webhook-Signature: sha256=5d41402abc4b2a76b9719d911017c592
User-Agent: TMI-Webhook/1.0

{
  "event_type": "threat_model.created",
  "threat_model_id": "550e8400-e29b-41d4-a716-446655440000",
  "resource_id": "550e8400-e29b-41d4-a716-446655440000",
  "resource_type": "threat_model",
  "owner_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "timestamp": "2025-01-15T14:30:00Z",
  "data": {
    "name": "Production API Threat Model",
    "description": "Threat model for production API services"
  }
}
```

### 4. Cleanup States

Subscriptions may transition to `pending_delete` status:

- **Idle Subscriptions**: No successful delivery in 90 days
- **Broken Subscriptions**: 10+ consecutive failures with no success in 7 days

The cleanup worker (runs hourly) deletes subscriptions in `pending_delete` status.

## Event Types

### Threat Model Events

- `threat_model.created`: New threat model created
- `threat_model.updated`: Threat model modified
- `threat_model.deleted`: Threat model deleted

### Diagram Events

- `diagram.created`: New diagram created
- `diagram.updated`: Diagram modified
- `diagram.deleted`: Diagram deleted

### Document Events

- `document.created`: New document uploaded
- `document.updated`: Document modified
- `document.deleted`: Document deleted

## Security

### URL Validation

All webhook URLs must:

1. Use HTTPS (required)
2. Contain valid DNS hostname per RFC 1035, 1123, and 5890
3. Pass deny list checks (blocks localhost, private IPs, cloud metadata endpoints)

**Blocked Patterns (Examples):**

- `localhost`, `127.0.0.1`, `::1`
- Private IP ranges: `10.*`, `192.168.*`, `172.16-31.*`
- Link-local addresses: `169.254.*`, `fe80::`
- Cloud metadata endpoints:
  - AWS: `169.254.169.254`
  - GCP: `metadata.google.internal`
  - Azure: `169.254.169.254`
  - DigitalOcean: `169.254.169.254`

Administrators can add custom deny list patterns (glob or regex) via the deny list API.

### HMAC Signature Verification

If you provide a `secret` when creating the subscription, TMI includes an HMAC-SHA256 signature in the `X-Webhook-Signature` header:

```
X-Webhook-Signature: sha256=5d41402abc4b2a76b9719d911017c592
```

**Verification Example (Python):**

```python
import hmac
import hashlib

def verify_webhook(payload_body, signature_header, secret):
    """Verify webhook HMAC signature"""
    # Remove 'sha256=' prefix
    expected_signature = signature_header.replace('sha256=', '')

    # Calculate signature
    mac = hmac.new(
        secret.encode('utf-8'),
        payload_body.encode('utf-8'),
        hashlib.sha256
    )
    calculated_signature = mac.hexdigest()

    # Constant-time comparison
    return hmac.compare_digest(calculated_signature, expected_signature)

# Usage
is_valid = verify_webhook(request.body, request.headers['X-Webhook-Signature'], 'my-shared-secret-key')
if not is_valid:
    return 401  # Unauthorized
```

## Delivery Guarantees

### Retry Logic

Failed deliveries are retried with exponential backoff:

1. **Attempt 1**: Immediate
2. **Attempt 2**: After 1 minute
3. **Attempt 3**: After 5 minutes
4. **Attempt 4**: After 15 minutes
5. **Attempt 5**: After 30 minutes

After 5 attempts, the delivery is marked `failed`.

### Success Criteria

A delivery is considered successful when:

- HTTP status code is 2xx (200-299)
- Response received within 30 seconds

### Failure Tracking

The subscription tracks:

- `publication_failures`: Consecutive failure count
- `last_successful_use`: Timestamp of last successful delivery

This data is used for cleanup decisions.

## Rate Limits

### Default Quotas (Per Owner)

- **Max Subscriptions**: 10 active subscriptions
- **Subscription Creation**: 5 requests per minute, 100 requests per day
- **Event Publication**: 100 events per minute

### Custom Quotas

Administrators can configure custom quotas via the quota API:

```http
POST /webhook/quotas
Authorization: Bearer <admin-jwt-token>
Content-Type: application/json

{
  "owner_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
  "max_subscriptions": 50,
  "max_subscription_requests_per_minute": 10,
  "max_subscription_requests_per_day": 500,
  "max_events_per_minute": 1000
}
```

## Authorization Model

Webhook subscriptions use owner-based authorization with administrator bypass:

| Operation | Non-Admin Users | Administrators |
|-----------|----------------|----------------|
| **List subscriptions** | Own subscriptions only | All subscriptions |
| **Get subscription** | Own subscriptions only | All subscriptions |
| **Create subscription** | Yes (becomes owner) | Yes |
| **Delete subscription** | Own subscriptions only | All subscriptions |
| **Test subscription** | Own subscriptions only | All subscriptions |
| **List deliveries** | Own subscriptions only | All subscriptions |
| **Get delivery** | Own subscriptions only | All subscriptions |

**Key Points:**

- **Ownership**: Subscriptions are owned by the user who creates them (`owner_id`)
- **Non-admin users**: Can only view and manage their own webhook subscriptions
- **Administrators**: Can view, test, and delete all webhook subscriptions across all users
- **Rate limits**: Apply to non-admin users; administrators bypass rate limiting

## API Endpoints

### Subscriptions

- `POST /webhook/subscriptions` - Create subscription (authenticated users)
- `GET /webhook/subscriptions` - List subscriptions (owner-scoped for users, all for admins)
- `GET /webhook/subscriptions/{id}` - Get subscription details (owner or admin)
- `DELETE /webhook/subscriptions/{id}` - Delete subscription (owner or admin)

### Deliveries

- `GET /webhook/deliveries` - List delivery records (owner-scoped for users, all for admins)
- `GET /webhook/deliveries/{id}` - Get delivery details (owner or admin)

### Deny List (Admin Only)

- `GET /webhook/deny-list` - List deny list patterns
- `POST /webhook/deny-list` - Add deny list pattern
- `DELETE /webhook/deny-list/{id}` - Remove deny list pattern

### Quotas (Admin Only)

- `GET /webhook/quotas/{owner_id}` - Get owner quota
- `POST /webhook/quotas` - Create/update quota
- `DELETE /webhook/quotas/{owner_id}` - Reset to defaults

## Best Practices

### Endpoint Implementation

1. **Respond Quickly**: Return 200 OK immediately, process asynchronously
2. **Idempotency**: Use `X-Webhook-Delivery-Id` to detect duplicate deliveries
3. **Signature Verification**: Always verify HMAC signatures if using secrets
4. **Error Handling**: Return appropriate HTTP status codes (2xx for success, 4xx/5xx for failures)
5. **Logging**: Log all webhook receipts for debugging

### Security

1. **HTTPS Only**: Never expose HTTP endpoints for webhooks
2. **IP Allowlisting**: Consider restricting to TMI server IPs
3. **Secrets**: Use strong, random secrets (min 32 characters)
4. **Signature Verification**: Always verify signatures to prevent spoofing
5. **Input Validation**: Validate all incoming webhook data

### Monitoring

1. **Track Failures**: Monitor `publication_failures` count
2. **Last Success**: Check `last_successful_use` timestamp
3. **Delivery Status**: Query delivery records for debugging
4. **Rate Limits**: Monitor quota usage to avoid throttling

### Testing

1. **Challenge Response**: Test verification flow before production
2. **Event Handling**: Verify all event types are handled correctly
3. **Retry Logic**: Test failure scenarios and retry behavior
4. **Signature Verification**: Test with valid and invalid signatures

## Example Implementation

### Express.js (Node.js)

```javascript
const express = require('express');
const crypto = require('crypto');

const app = express();
app.use(express.json());

// Webhook endpoint
app.post('/webhooks/tmi', (req, res) => {
  const eventType = req.headers['x-webhook-event'];
  const deliveryId = req.headers['x-webhook-delivery-id'];
  const signature = req.headers['x-webhook-signature'];

  // Verify signature
  if (signature) {
    const secret = process.env.TMI_WEBHOOK_SECRET;
    const expectedSignature = 'sha256=' + crypto
      .createHmac('sha256', secret)
      .update(JSON.stringify(req.body))
      .digest('hex');

    if (signature !== expectedSignature) {
      console.error('Invalid signature');
      return res.status(401).send('Unauthorized');
    }
  }

  // Handle challenge
  if (eventType === 'webhook.challenge') {
    console.log('Responding to challenge');
    return res.json({ challenge: req.body.challenge });
  }

  // Handle events asynchronously
  processWebhook(eventType, deliveryId, req.body)
    .catch(err => console.error('Error processing webhook:', err));

  // Respond immediately
  res.status(200).send('OK');
});

async function processWebhook(eventType, deliveryId, payload) {
  // Check for duplicate delivery
  const isDuplicate = await checkDuplicate(deliveryId);
  if (isDuplicate) {
    console.log('Duplicate delivery, skipping');
    return;
  }

  // Process event based on type
  switch (eventType) {
    case 'threat_model.created':
      await handleThreatModelCreated(payload);
      break;
    case 'threat_model.updated':
      await handleThreatModelUpdated(payload);
      break;
    case 'threat_model.deleted':
      await handleThreatModelDeleted(payload);
      break;
    default:
      console.log('Unknown event type:', eventType);
  }
}

app.listen(3000, () => {
  console.log('Webhook server listening on port 3000');
});
```

### Flask (Python)

```python
from flask import Flask, request, jsonify
import hmac
import hashlib
import json

app = Flask(__name__)

@app.route('/webhooks/tmi', methods=['POST'])
def webhook():
    event_type = request.headers.get('X-Webhook-Event')
    delivery_id = request.headers.get('X-Webhook-Delivery-Id')
    signature = request.headers.get('X-Webhook-Signature')

    # Verify signature
    if signature:
        secret = os.environ['TMI_WEBHOOK_SECRET']
        payload = request.get_data()
        expected_sig = 'sha256=' + hmac.new(
            secret.encode('utf-8'),
            payload,
            hashlib.sha256
        ).hexdigest()

        if not hmac.compare_digest(signature, expected_sig):
            return 'Unauthorized', 401

    # Handle challenge
    if event_type == 'webhook.challenge':
        return jsonify({'challenge': request.json['challenge']})

    # Process event asynchronously
    process_webhook.delay(event_type, delivery_id, request.json)

    return 'OK', 200

@celery.task
def process_webhook(event_type, delivery_id, payload):
    # Check for duplicate delivery
    if is_duplicate(delivery_id):
        logger.info(f'Duplicate delivery {delivery_id}, skipping')
        return

    # Process based on event type
    handlers = {
        'threat_model.created': handle_threat_model_created,
        'threat_model.updated': handle_threat_model_updated,
        'threat_model.deleted': handle_threat_model_deleted,
    }

    handler = handlers.get(event_type)
    if handler:
        handler(payload)
    else:
        logger.warning(f'Unknown event type: {event_type}')

if __name__ == '__main__':
    app.run(port=3000)
```

## Troubleshooting

### Subscription Not Receiving Events

1. **Check Status**: Verify subscription is `active` (not `pending_verification` or `pending_delete`)
2. **Event Filter**: Confirm `events` array includes the expected event types
3. **Threat Model Filter**: If `threat_model_id` is set, events are filtered to that model only
4. **Rate Limits**: Check if quota is exceeded
5. **Delivery Failures**: Query delivery records for error messages

### Verification Failed

1. **Challenge Response**: Ensure endpoint returns `{"challenge": "<value>"}` JSON
2. **Response Time**: Respond within 30 seconds
3. **HTTP Status**: Return 2xx status code
4. **Network Access**: Ensure TMI server can reach your endpoint
5. **HTTPS**: Verify SSL/TLS certificate is valid

### High Failure Rate

1. **Endpoint Availability**: Ensure endpoint is accessible and responding
2. **Response Time**: Return 2xx within 30 seconds
3. **Error Codes**: Avoid 4xx/5xx responses
4. **Signature Verification**: Check HMAC calculation if using secrets
5. **Payload Processing**: Handle all event types gracefully

### Missing Webhooks

1. **Deduplication**: TMI deduplicates events within 60-second windows
2. **Event Emission**: Verify CRUD operations are actually emitting events
3. **Redis Availability**: Check Redis Streams are operational
4. **Worker Health**: Verify event consumer and delivery workers are running

## Monitoring and Observability

TMI exposes metrics for webhook operations (stubs in current implementation):

- `webhook_subscriptions_total`: Total subscriptions by status
- `webhook_deliveries_total`: Total deliveries by status
- `webhook_delivery_duration_seconds`: Delivery latency histogram
- `webhook_challenge_attempts_total`: Challenge verification attempts
- `webhook_events_emitted_total`: Events emitted by type
- `webhook_rate_limit_exceeded_total`: Rate limit rejections

These metrics can be scraped by Prometheus for monitoring.

## See Also

- [OpenAPI Specification](../../reference/apis/tmi-openapi.json) - Complete API reference
- [OAuth Integration](../setup/oauth-integration.md) - Authentication setup
- [Database Schema](../../reference/schema/database-schema.md) - Webhook tables
- [Redis Integration](../../operator/redis-configuration.md) - Redis Streams setup
