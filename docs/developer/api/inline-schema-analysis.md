# Inline Schema Analysis

Analysis of 138 inline schemas flagged by RateMyOpenAPI for potential extraction to `components/schemas`.

## Common Patterns Found

### 1. JSON Patch Operations (8 instances)
**Pattern**: All PATCH endpoints using `application/json-patch+json`

**Schema** (identical across all):
```json
{
  "type": "array",
  "items": {
    "type": "object",
    "properties": {
      "op": {"type": "string", "enum": ["add", "replace", "remove", "move", "copy", "test"]},
      "path": {"type": "string", "pattern": "^(/[^/]*)*$"},
      "value": {"nullable": true}
    },
    "required": ["op", "path"]
  }
}
```

**Affected Endpoints**:
- `/threat_models/{threat_model_id}` PATCH
- `/threat_models/{threat_model_id}/assets/{asset_id}` PATCH
- `/threat_models/{threat_model_id}/diagrams/{diagram_id}` PATCH
- `/threat_models/{threat_model_id}/documents/{document_id}` PATCH
- `/threat_models/{threat_model_id}/notes/{note_id}` PATCH
- `/threat_models/{threat_model_id}/repositories/{repository_id}` PATCH
- `/threat_models/{threat_model_id}/threats/{threat_id}` PATCH
- `/threat_models/{threat_model_id}/threats/bulk` PATCH

**Recommendation**: Extract as `#/components/schemas/JsonPatchDocument`

---

### 2. OAuth Request Schemas (Unique)
Each OAuth endpoint has a unique schema tailored to its RFC specification:

- **`/oauth2/token`** (RFC 6749 §4.1.3): 8 properties (grant_type, code, state, redirect_uri, client_id, client_secret, code_verifier, refresh_token)
- **`/oauth2/refresh`** (RFC 6749 §6): 1 property (refresh_token)
- **`/oauth2/introspect`** (RFC 7662 §2.1): 2 properties (token, token_type_hint)
- **`/oauth2/revoke`** (RFC 7009 §2.1): 2 properties (token, token_type_hint)

**Recommendation**: Extract each as separate schemas:
- `TokenRequest`
- `TokenRefreshRequest`
- `TokenIntrospectionRequest`
- `TokenRevocationRequest`

---

### 3. Quota Admin Schemas (3 unique)
Each quota type has different properties:

- **Addon Quotas**: `max_active_invocations`, `max_invocations_per_hour`
- **User Quotas**: `max_requests_per_hour`, `max_requests_per_minute`
- **Webhook Quotas**: `max_events_per_minute`, `max_subscription_requests_per_day`, `max_subscription_requests_per_minute`, `max_subscriptions`

**Recommendation**: Extract as:
- `AddonQuotaUpdate`
- `UserQuotaUpdate`
- `WebhookQuotaUpdate`

---

### 4. Metadata Bulk Operations (Already using $ref)
**Status**: Already correctly using `$ref` to `#/components/schemas/Metadata`

Example:
```json
{
  "type": "array",
  "items": {"$ref": "#/components/schemas/Metadata"},
  "maxItems": 20
}
```

**Note**: RateMyOpenAPI is flagging the wrapping array schema as "inline", but the actual Metadata object is already extracted. This is acceptable - the array wrapper is endpoint-specific (different maxItems values).

---

### 5. SAML Request Schemas (2 instances)
- **`/saml/acs`**: Form-encoded SAML assertion response
- **`/saml/slo`**: Form-encoded SAML logout request

Both have unique schemas with SAML-specific fields.

**Recommendation**: Extract as:
- `SamlAssertionConsumerRequest`
- `SamlSingleLogoutRequest`

---

### 6. List/Array Response Schemas (~21 instances)
Many GET endpoints return arrays wrapped in objects:

```json
{
  "type": "object",
  "properties": {
    "items": {"type": "array", "items": {"$ref": "#/components/schemas/ThreatModel"}},
    "total": {"type": "integer"},
    "limit": {"type": "integer"},
    "offset": {"type": "integer"}
  }
}
```

**Pattern**: Pagination wrapper around entity lists

**Affected**: `/threat_models`, `/webhooks/subscriptions`, `/admin/users`, etc.

**Recommendation**: Extract as generic pagination wrapper:
- `PaginatedResponse` (with generic items type)

---

## Summary

| Pattern | Count | Recommendation |
|---------|-------|----------------|
| JSON Patch (identical) | 8 | **High priority** - Extract as `JsonPatchDocument` |
| OAuth requests (unique) | 4 | **Medium priority** - Extract for SDK clarity |
| Quota schemas (unique) | 3 | **Low priority** - Simple objects, rarely change |
| Metadata bulk (already $ref) | ~21 | **No action** - Already using $ref correctly |
| SAML requests | 2 | **Low priority** - Rarely used, unique structures |
| Pagination wrappers | ~21 | **Medium priority** - Would improve consistency |
| Discovery responses | 3 | **Low priority** - Static, rarely change |
| Rate limit headers | ~492 | **No action** - Header schemas, not bodies |

## Recommendation Tiers

### High Value (Do First)
1. **JSON Patch**: 8 identical schemas → 1 component schema
   - Clear duplication, used across multiple resources
   - Improves SDK generation significantly

### Medium Value (Consider)
2. **OAuth Requests**: 4 unique schemas for OAuth endpoints
   - Improves SDK type safety for OAuth clients
   - Makes RFC compliance more explicit

3. **Pagination Wrappers**: Generic pagination pattern
   - Would require careful handling of generic types
   - Improves consistency across list endpoints

### Low Value (Defer)
4. **Quota Schemas**: Simple, unique objects
5. **SAML Requests**: Rarely used, SAML-specific
6. **Discovery Responses**: Static RFC-defined structures

