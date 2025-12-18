# RateMyOpenAPI Remediation Status

**Overall Score: 99/100**

## Completed ✅

### 1. Server URL Protocol Requirement
- **Status**: Fixed
- **Action**: Removed `http://localhost:{port}` from server list
- **Remaining**: Only `https://api.tmi.dev` in servers array
- **Note**: Local development uses server override at runtime

### 2. Retry-After Headers
- **Status**: Fully implemented across all 174 endpoints
- **Scripts**:
  - `scripts/add-retry-after-headers.py` - Added Retry-After to inline 429 responses
  - `scripts/clean-redundant-ref-headers.py` - Removed redundant headers from $ref responses
- **Coverage**:
  - 140 endpoints using `$ref` to `#/components/responses/TooManyRequests` (component includes Retry-After)
  - 34 inline 429 responses have explicit Retry-After headers
- **Cleanup**: Removed 140 redundant local headers from responses using `$ref` (DRY principle)
- **RFC Compliance**: All 429 responses now comply with RFC 6585

## Decisions Needed

### 1. Server URL Protocol Requirement
**Issue**: RateMyOpenAPI requires all server URLs to use HTTPS

**Current State**:
```json
{
  "servers": [
    {
      "url": "http://localhost:{port}",
      "description": "Local development server (HTTP on localhost only)"
    },
    {
      "url": "https://api.tmi.dev",
      "description": "Production server"
    }
  ]
}
```

**Options**:
- **A**: Remove `http://localhost` from OpenAPI spec (developers configure locally)
- **B**: Keep `http://localhost` and accept the validation warning (practical for development)
- **C**: Change localhost to use HTTPS with self-signed certs (added complexity)

**Recommendation**: Option B - Accept the warning. HTTP on localhost is standard practice for development.

---

### 2. Inline Schemas (138 instances)
**Issue**: RateMyOpenAPI recommends moving inline schemas to `components/schemas` for better SDK generation

**Examples**:
- Request bodies: `/oauth2/token`, `/oauth2/refresh`, `/saml/acs`
- Response schemas: `/threat_models` (200), `/webhooks/subscriptions` (200)
- Bulk operation schemas across all resources

**Current Pattern**:
```json
{
  "paths": {
    "/oauth2/token": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "schema": { /* inline schema here */ }
            }
          }
        }
      }
    }
  }
}
```

**Recommended Pattern**:
```json
{
  "paths": {
    "/oauth2/token": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {
              "schema": {
                "$ref": "#/components/schemas/TokenRequest"
              }
            }
          }
        }
      }
    }
  },
  "components": {
    "schemas": {
      "TokenRequest": { /* schema definition */ }
    }
  }
}
```

**Impact**:
- **Benefits**: Better SDK generation, schema reuse, improved documentation
- **Scope**: 138 inline schemas across the entire API
- **Complexity**: Major refactoring of OpenAPI spec

**Options**:
- **A**: Create automated script to extract and reference inline schemas
- **B**: Manually refactor high-value endpoints first (OAuth, CRUD operations)
- **C**: Accept current structure (loses 2% on SDK Generation score)

**Recommendation**: Defer until SDK generation becomes a priority. Current inline approach is valid OpenAPI 3.0.

---

### 3. Retry-After Header Coverage
**Issue**: Only admin endpoints have `Retry-After` headers; 174 other endpoints with 429 responses are missing it

**Affected Endpoints**:
- OAuth endpoints (`/oauth2/token`, `/oauth2/authorize`, etc.)
- Public discovery endpoints (`.well-known/*`)
- SAML endpoints
- All CRUD resource endpoints

**Options**:
- **A**: Add `Retry-After` to all 429 responses (consistent with RFC 6585)
- **B**: Add only to authenticated endpoints (skip public endpoints)
- **C**: Keep current state (admin endpoints only)

**Recommendation**: Option A - Add to all 429 responses for RFC compliance.

## Summary

| Issue | Status | Decision Required |
|-------|--------|-------------------|
| 401 on public endpoints | False positive | No - ignore warning |
| Retry-After headers | Partial (admin only) | Yes - extend to all endpoints? |
| Server URL protocol | Validation warning | Yes - keep HTTP localhost? |
| Inline schemas | Valid but not optimal | Yes - refactor or accept? |

