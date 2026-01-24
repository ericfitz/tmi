# Add-on Development Guide

**Audience:** Add-on Developers, Integration Partners
**Version:** 1.0
**Last Updated:** 2025-11-08

## Overview

This guide explains how to develop webhook services that integrate with TMI as add-ons. Add-ons receive invocations from TMI users, process them asynchronously, and report status back via callbacks.

## Quick Start

### 1. Register Webhook Subscription

First, create a webhook subscription (requires authentication):

```bash
POST /webhooks
Authorization: Bearer {jwt}
Content-Type: application/json

{
  "name": "My Add-on Service",
  "url": "https://my-service.example.com/webhooks/tmi",
  "events": [],  # Add-ons don't need subscription events
  "secret": "your-hmac-secret-128-chars-minimum"
}
```

Save the `webhook_id` from the response.

### 2. Register Add-on (Admin Required)

Have a TMI administrator register your add-on:

```bash
POST /addons
Authorization: Bearer {admin_jwt}
Content-Type: application/json

{
  "name": "STRIDE Analyzer",
  "webhook_id": "{webhook_id_from_step_1}",
  "description": "Automated STRIDE threat analysis",
  "icon": "material-symbols:security",
  "objects": ["threat_model", "asset"]
}
```

### 3. Implement Webhook Endpoint

Create an HTTPS endpoint that:
1. Receives POST requests from TMI
2. Verifies HMAC signature
3. Processes the invocation asynchronously
4. Calls back to update status

## Webhook Invocation Flow

### Step 1: Receive Invocation

Your webhook receives:

```http
POST /webhooks/tmi
Content-Type: application/json
X-Webhook-Event: addon.invoked
X-Invocation-Id: 550e8400-e29b-41d4-a716-446655440000
X-Addon-Id: 123e4567-e89b-12d3-a456-426614174000
X-Webhook-Signature: sha256=abc123...
User-Agent: TMI-Addon-Worker/1.0

{
  "event_type": "addon.invoked",
  "invocation_id": "550e8400-e29b-41d4-a716-446655440000",
  "addon_id": "123e4567-e89b-12d3-a456-426614174000",
  "threat_model_id": "789e0123-e45b-67c8-d901-234567890abc",
  "object_type": "asset",
  "object_id": "def01234-5678-90ab-cdef-1234567890ab",
  "timestamp": "2025-11-08T12:00:00Z",
  "payload": {
    "user_param_1": "value1",
    "user_param_2": "value2"
  },
  "callback_url": "https://tmi.example.com/invocations/550e8400-e29b-41d4-a716-446655440000/status"
}
```

### Step 2: Verify HMAC Signature

**CRITICAL:** Always verify the signature before processing:

```python
import hmac
import hashlib

def verify_signature(payload_bytes, signature_header, secret):
    """Verify HMAC-SHA256 signature"""
    expected = hmac.new(
        secret.encode('utf-8'),
        payload_bytes,
        hashlib.sha256
    ).hexdigest()

    expected_sig = f"sha256={expected}"

    # Constant-time comparison
    return hmac.compare_digest(signature_header, expected_sig)

# In your handler
payload_bytes = request.get_data()
signature = request.headers.get('X-Webhook-Signature')

if not verify_signature(payload_bytes, signature, WEBHOOK_SECRET):
    return 'Invalid signature', 401
```

```javascript
// Node.js example
const crypto = require('crypto');

function verifySignature(payloadBody, signatureHeader, secret) {
    const hmac = crypto.createHmac('sha256', secret);
    hmac.update(payloadBody);
    const expectedSig = `sha256=${hmac.digest('hex')}`;

    // Constant-time comparison
    return crypto.timingSafeEqual(
        Buffer.from(signatureHeader),
        Buffer.from(expectedSig)
    );
}

// In your Express handler
app.post('/webhooks/tmi', (req, res) => {
    const signature = req.headers['x-webhook-signature'];
    const payloadBody = JSON.stringify(req.body);

    if (!verifySignature(payloadBody, signature, WEBHOOK_SECRET)) {
        return res.status(401).send('Invalid signature');
    }

    // Process invocation...
});
```

### Step 3: Respond Quickly

Return `200 OK` immediately to TMI:

```python
@app.route('/webhooks/tmi', methods=['POST'])
def handle_invocation():
    # Verify signature
    if not verify_signature(...):
        return 'Invalid signature', 401

    payload = request.json
    invocation_id = payload['invocation_id']

    # Queue for async processing
    task_queue.enqueue(process_invocation, payload)

    # Respond immediately
    return '', 200  # TMI auto-completes the invocation
```

### Callback Modes

TMI supports two callback modes, controlled by the `X-TMI-Callback` response header:

**Auto-Complete Mode (Default)**

When your webhook returns a 2xx response without the `X-TMI-Callback` header (or with any value other than `async`), TMI automatically marks the invocation as `completed`. Use this mode when:
- Your webhook handles the work synchronously
- You don't need to report progress updates
- The invocation is "fire and forget"

```python
# Auto-complete mode - invocation marked complete immediately
return '', 200
```

**Async Callback Mode**

When your webhook returns the `X-TMI-Callback: async` header, TMI marks the invocation as `in_progress` and expects your service to call back with status updates. Use this mode when:
- Your processing takes significant time
- You want to report progress percentages
- You need to report success or failure after processing

```python
# Async callback mode - you must call back with status updates
return '', 200, {'X-TMI-Callback': 'async'}
```

**Important:** If you use async mode but never call back, the invocation will timeout after 15 minutes of inactivity and be marked as `failed`.

### Step 4: Update Status During Processing (Async Mode Only)

If using async callback mode, call back to TMI to update progress:

```python
import requests

def update_status(invocation_id, status, percent, message):
    """Update invocation status via callback"""
    callback_url = f"https://tmi.example.com/invocations/{invocation_id}/status"

    # Generate HMAC signature for request
    payload = json.dumps({
        "status": status,
        "status_percent": percent,
        "status_message": message
    })

    signature = generate_signature(payload.encode(), WEBHOOK_SECRET)

    response = requests.post(
        callback_url,
        json=payload,
        headers={
            'Content-Type': 'application/json',
            'X-Webhook-Signature': signature
        }
    )

    return response.status_code == 200

# During processing
def process_invocation(payload):
    invocation_id = payload['invocation_id']

    # Update: started processing
    update_status(invocation_id, "in_progress", 10, "Starting analysis...")

    # Do work...
    analyze_threats(payload)

    # Update: halfway done
    update_status(invocation_id, "in_progress", 50, "Analyzing assets...")

    # Do more work...
    generate_report(payload)

    # Update: completed
    update_status(invocation_id, "completed", 100, "Analysis complete")
```

### Step 5: Handle Failures

Report failures via callback:

```python
def process_invocation(payload):
    invocation_id = payload['invocation_id']

    try:
        # Process...
        analyze_threats(payload)

        update_status(invocation_id, "completed", 100, "Success")

    except ValidationError as e:
        update_status(invocation_id, "failed", 0, f"Validation error: {e}")

    except Exception as e:
        logger.exception("Processing failed")
        update_status(invocation_id, "failed", 0, f"Internal error: {e}")
```

## API Reference

### Status Update Endpoint

**Endpoint:** `POST /invocations/{invocation_id}/status`

**Authentication:** HMAC signature (no JWT required)

**Request:**
```json
{
  "status": "in_progress",        // Required: in_progress, completed, failed
  "status_percent": 75,            // Required: 0-100
  "status_message": "Processing..."  // Optional
}
```

**Headers:**
```
Content-Type: application/json
X-Webhook-Signature: sha256={hmac_hex}
```

**Response:** `200 OK`
```json
{
  "id": "uuid",
  "status": "in_progress",
  "status_percent": 75,
  "status_updated_at": "2025-11-08T12:05:00Z"
}
```

**Error Responses:**
- `400 Bad Request` - Invalid status or percent value
- `401 Unauthorized` - Missing or invalid HMAC signature
- `404 Not Found` - Invocation not found or expired (7-day TTL)
- `409 Conflict` - Invalid status transition (already completed/failed)

### Status Transitions

Valid transitions:
```
pending → in_progress → completed
                      → failed
```

Invalid transitions (return 409):
- `completed → in_progress`
- `failed → in_progress`
- `completed → failed`
- `failed → completed`

## Payload Structure

### Invocation Payload Fields

| Field | Type | Description |
|-------|------|-------------|
| `event_type` | string | Always `"addon.invoked"` |
| `invocation_id` | uuid | Unique invocation identifier |
| `addon_id` | uuid | Add-on identifier |
| `threat_model_id` | uuid | Threat model context (always present) |
| `object_type` | string | Optional: asset, threat, diagram, etc. |
| `object_id` | uuid | Optional: specific object ID |
| `timestamp` | RFC3339 | When invocation was created |
| `payload` | object | User-provided data (max 1KB) |
| `callback_url` | string | Status update endpoint URL |

### User Payload

The `payload` field contains user-provided data (max 1KB). Structure is defined by your add-on:

**Example: STRIDE Analyzer**
```json
{
  "payload": {
    "analysis_type": "full",
    "include_recommendations": true,
    "severity_threshold": "medium"
  }
}
```

**Example: Compliance Checker**
```json
{
  "payload": {
    "framework": "NIST",
    "version": "1.1",
    "output_format": "json"
  }
}
```

## Testing Your Add-on

### Local Development

1. Use ngrok or similar to expose local server:
```bash
ngrok http 8000
# Use HTTPS URL for webhook registration
```

2. Register webhook with ngrok URL:
```bash
POST /webhooks
{
  "url": "https://abc123.ngrok.io/webhooks/tmi",
  ...
}
```

3. Invoke add-on and check logs:
```bash
POST /addons/{addon_id}/invoke
{
  "threat_model_id": "...",
  "payload": {"test": true}
}
```

### Testing HMAC Verification

Generate test signatures:

```python
import hmac
import hashlib
import json

payload = json.dumps({"status": "completed", "status_percent": 100})
secret = "your-webhook-secret"

mac = hmac.new(secret.encode(), payload.encode(), hashlib.sha256)
signature = f"sha256={mac.hexdigest()}"

print(f"X-Webhook-Signature: {signature}")
```

### Test Status Updates

```bash
# Manual status update test
curl -X POST https://tmi.example.com/invocations/{id}/status \
  -H "Content-Type: application/json" \
  -H "X-Webhook-Signature: sha256=..." \
  -d '{
    "status": "completed",
    "status_percent": 100,
    "status_message": "Test completed"
  }'
```

## Best Practices

### 1. Idempotency

Handle duplicate invocations gracefully:

```python
# Store processing state
cache = {}

def process_invocation(payload):
    invocation_id = payload['invocation_id']

    # Check if already processed
    if invocation_id in cache:
        logger.info(f"Duplicate invocation: {invocation_id}")
        return cache[invocation_id]

    # Process...
    result = do_work(payload)

    # Cache result
    cache[invocation_id] = result
    return result
```

### 2. Progress Updates

Update status regularly for long-running operations:

```python
def long_operation(invocation_id):
    update_status(invocation_id, "in_progress", 0, "Starting...")

    for i, step in enumerate(steps):
        process_step(step)
        percent = int((i + 1) / len(steps) * 100)
        update_status(invocation_id, "in_progress", percent, f"Step {i+1}/{len(steps)}")

    update_status(invocation_id, "completed", 100, "Done")
```

### 3. Error Handling

Provide useful error messages:

```python
def process_invocation(payload):
    try:
        validate_payload(payload)
    except ValidationError as e:
        # User-friendly error
        update_status(invocation_id, "failed", 0,
                     f"Invalid input: {e}. Please check your parameters.")
        return

    try:
        do_work(payload)
    except ExternalServiceError as e:
        # Temporary failure
        update_status(invocation_id, "failed", 0,
                     f"External service unavailable: {e}. Please retry later.")
        return
```

### 4. Timeouts

Set reasonable timeouts to prevent stuck invocations:

```python
from timeout_decorator import timeout

@timeout(300)  # 5 minute timeout
def process_invocation(payload):
    try:
        # Process...
        result = do_work(payload)
        update_status(invocation_id, "completed", 100, "Success")
    except TimeoutError:
        update_status(invocation_id, "failed", 0,
                     "Processing timeout after 5 minutes")
```

### 5. Security

- **Always verify HMAC signatures** before processing
- Use HTTPS for all callbacks
- Don't log secrets or sensitive user data
- Validate all input from payload
- Use constant-time comparison for signatures

## Troubleshooting

### Issue: Not receiving invocations

**Check:**
1. Webhook status is `active` (not `pending_verification`)
2. Webhook URL is accessible from TMI server
3. HTTPS certificate is valid
4. Firewall allows TMI server IP

**Test:**
```bash
# Check webhook from TMI server
curl -X POST https://your-webhook-url.example.com/webhooks/tmi \
  -H "Content-Type: application/json" \
  -d '{"test": true}'
```

### Issue: Status updates return 401

**Cause:** Invalid HMAC signature

**Fix:**
1. Ensure you're using the same secret as webhook registration
2. Sign the **exact** request body (JSON string)
3. Include `sha256=` prefix in signature
4. Use constant-time comparison

**Debug:**
```python
# Log signature details
logger.debug(f"Payload: {payload}")
logger.debug(f"Secret: {secret[:10]}...") # Don't log full secret
logger.debug(f"Generated signature: {signature}")
```

### Issue: Invocations stuck in pending

**Cause:** Webhook not responding with 200 OK

**Fix:** Ensure your endpoint returns 200 within 30 seconds

```python
# Bad: slow synchronous processing
def handle_invocation():
    process_invocation(request.json)  # Blocks!
    return '', 200  # Timeout!

# Good: async processing
def handle_invocation():
    task_queue.enqueue(process_invocation, request.json)
    return '', 200  # Returns immediately
```

## Example Implementation

Complete Python Flask example:

```python
from flask import Flask, request
import hmac
import hashlib
import json
import requests

app = Flask(__name__)
WEBHOOK_SECRET = "your-128-char-secret"
TMI_BASE_URL = "https://tmi.example.com"

def verify_signature(payload_bytes, signature, secret):
    expected = hmac.new(
        secret.encode(),
        payload_bytes,
        hashlib.sha256
    ).hexdigest()
    return hmac.compare_digest(f"sha256={expected}", signature)

def generate_signature(payload_bytes, secret):
    mac = hmac.new(secret.encode(), payload_bytes, hashlib.sha256)
    return f"sha256={mac.hexdigest()}"

def update_status(invocation_id, status, percent, message=""):
    payload = json.dumps({
        "status": status,
        "status_percent": percent,
        "status_message": message
    })

    signature = generate_signature(payload.encode(), WEBHOOK_SECRET)

    requests.post(
        f"{TMI_BASE_URL}/invocations/{invocation_id}/status",
        data=payload,
        headers={
            'Content-Type': 'application/json',
            'X-Webhook-Signature': signature
        }
    )

@app.route('/webhooks/tmi', methods=['POST'])
def handle_invocation():
    # Verify signature
    payload_bytes = request.get_data()
    signature = request.headers.get('X-Webhook-Signature')

    if not verify_signature(payload_bytes, signature, WEBHOOK_SECRET):
        return 'Unauthorized', 401

    # Parse payload
    data = request.json
    invocation_id = data['invocation_id']

    # Queue for async processing
    task_queue.enqueue(process_invocation, data)

    # Respond immediately
    return '', 200

def process_invocation(data):
    invocation_id = data['invocation_id']
    user_payload = data['payload']

    try:
        # Start
        update_status(invocation_id, "in_progress", 10, "Starting analysis")

        # Process
        result = analyze_threats(user_payload)

        # Progress
        update_status(invocation_id, "in_progress", 75, "Generating report")

        # Finish
        update_status(invocation_id, "completed", 100, "Analysis complete")

    except Exception as e:
        update_status(invocation_id, "failed", 0, f"Error: {e}")

if __name__ == '__main__':
    app.run(port=8000)
```

## Support

For questions or issues:
- Check [Operator Guide](../../operator/addons/addon-configuration.md) for configuration
- Review [Design Document](addons-design.md) for architecture details
- File issues at https://github.com/ericfitz/tmi/issues
