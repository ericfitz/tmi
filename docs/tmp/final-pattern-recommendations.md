# Final Pattern Recommendations - Updated

**Date**: 2025-01-18
**Status**: Ready for Implementation

## User Clarifications Applied

### 1. ✅ LIST Operation Filter Parameters
**Rule**: All filter query parameters for LIST operations → **maxLength: 256**

**Affected Fields:**
- `paths./threat_models.get.parameters` (name, owner, created_by, framework filters)
- `paths./threat_models/{threat_model_id}/threats.get.parameters` (name, severity, priority, status, risk_level filters)
- All other LIST endpoint filter parameters

**Examples:**
```json
{
  "name": "name",
  "in": "query",
  "schema": {
    "type": "string",
    "maxLength": 256,
    "pattern": "^[\\u0020-\\uFFFF]{1,256}$"
  }
}
```

---

### 2. ✅ JSON Patch Operations
**Rule**: `path` and `from` fields → **maxLength: 512**

**Affected Fields:**
- `*.requestBody.content.application/json-patch+json.schema.items.properties.path`
- `*.requestBody.content.application/json-patch+json.schema.items.properties.from`

**Pattern**: `^(/[^/]{1,100}){1,10}$` (JSON Pointer with reasonable depth)

**Rationale**:
- 512 chars allows deep nested paths
- Pattern limits each segment to 100 chars and max 10 levels deep
- Prevents excessive nesting while allowing flexibility

---

### 3. ✅ OAuth User Names
**Rule**: **maxLength: 255** (align with database VARCHAR(255))

**Affected Fields:**
- `User.name` (response)
- `userinfo.name` (response)
- All OAuth provider name responses

**Pattern**: `^[\\u0020-\\uFFFF]{1,255}$` (Unicode)

**Database Alignment**: `auth/migrations/001_core_infrastructure.up.sql:2` → `name VARCHAR(255)`

---

### 4. ✅ Threat Model Framework - Pattern (NOT Enum)
**User Decision**: Allow custom frameworks, not restricted to enum

**Pattern**: `^[A-Za-z0-9_-]{1,30}$`

**maxLength**: 30

**Affected Fields:**
- `TMListItem.threat_model_framework`
- `ThreatModelBase.threat_model_framework`
- `ThreatModelInput.threat_model_framework`
- Filter parameter: `paths./threat_models.get.parameters.5.schema`

**Examples:**
- `STRIDE`
- `LINDDUN`
- `PASTA`
- `Custom-Framework`
- `My_TM_v2`

**Database**: VARCHAR(50) with default 'STRIDE' - compatible

---

### 5. ⚠️ Severity - Pattern (NOT Enum) - PENDING USER CHOICE

**User Decision**: Allow numerals and localized strings

**User Requirements:**
- Support numerals as text: "0", "1", "2", "3", "4", "5"
- Support localized strings: "Bajo", "Medio", "Alto" (Spanish), "低", "中", "高" (Chinese)
- Allow alphanumeric + hyphen + underscore + parentheses

**maxLength**: 50 (aligns with database VARCHAR(50))

**Affected Fields:**
- `Threat.severity` (schema)
- `ThreatBase.severity` (schema)
- Filter parameter: `paths./threat_models/{threat_model_id}/threats.get.parameters.4.schema`

**Option A - ASCII Only:**
```json
{
  "type": "string",
  "maxLength": 50,
  "pattern": "^[A-Za-z0-9_()-]{1,50}$",
  "description": "Severity level (numeric or custom string)"
}
```

**Option B - Unicode (Recommended for Localization):**
```json
{
  "type": "string",
  "maxLength": 50,
  "pattern": "^[\\u0020-\\uFFFF_().-]{1,50}$",
  "description": "Severity level (numeric, localized, or custom string)"
}
```

**Database Constraint Issue**:
```sql
severity VARCHAR(50) CHECK (severity IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical'))
```

⚠️ **Action Required**: If allowing custom severity values, create migration to remove CHECK constraint.

**PENDING**: User to choose Option A (ASCII) or Option B (Unicode)

---

### 6. ✅ Webhook Event Types - NEW ENUM

**User Decision**: Convert to enum (limited supported events)

**Supported Event Types** (from [api/events.go:15-55](api/events.go#L15-L55)):

**Threat Model Events:**
- `threat_model.created`
- `threat_model.updated`
- `threat_model.deleted`

**Diagram Events:**
- `diagram.created`
- `diagram.updated`
- `diagram.deleted`

**Document Events:**
- `document.created`
- `document.updated`
- `document.deleted`

**Note Events:**
- `note.created`
- `note.updated`
- `note.deleted`

**Repository Events:**
- `repository.created`
- `repository.updated`
- `repository.deleted`

**Asset Events:**
- `asset.created`
- `asset.updated`
- `asset.deleted`

**Threat Events:**
- `threat.created`
- `threat.updated`
- `threat.deleted`

**Metadata Events:**
- `metadata.created`
- `metadata.updated`
- `metadata.deleted`

**OpenAPI Definition:**
```json
{
  "event_type": {
    "type": "string",
    "enum": [
      "threat_model.created", "threat_model.updated", "threat_model.deleted",
      "diagram.created", "diagram.updated", "diagram.deleted",
      "document.created", "document.updated", "document.deleted",
      "note.created", "note.updated", "note.deleted",
      "repository.created", "repository.updated", "repository.deleted",
      "asset.created", "asset.updated", "asset.deleted",
      "threat.created", "threat.updated", "threat.deleted",
      "metadata.created", "metadata.updated", "metadata.deleted"
    ],
    "description": "Webhook event type following resource_name.action pattern"
  }
}
```

**Affected Fields:**
- `components.schemas.WebhookDelivery.properties.event_type`
- `components.schemas.WebhookTestRequest.properties.event_type`
- Any webhook subscription filter parameters

**Total Events**: 24 event types (8 resources × 3 actions)

---

## Updated Enum Conversions Summary

**Convert to Enums (11 fields, up from 10):**

1. ✅ `bearer_methods_supported[]` → `["header", "body", "query"]` (RFC 9728)
2. ✅ `response_types_supported[]` → OAuth standard values
3. ✅ `subject_types_supported[]` → `["public", "pairwise"]`
4. ✅ `id_token_signing_alg_values_supported[]` → JWT algorithms
5. ✅ `grant_types_supported[]` → OAuth grant types
6. ✅ `token_endpoint_auth_methods_supported[]` → Auth methods
7. ✅ `jwks.keys[].kty` → `["RSA", "EC", "oct"]`
8. ✅ `jwks.keys[].use` → `["sig", "enc"]`
9. ✅ `token_type_hint` → `["access_token", "refresh_token"]`
10. ✅ `webhook_test_response.status` → `["success", "failure"]`
11. ✅ **NEW: `event_type`** → 24 webhook event types (see above)

**Use Patterns Instead (2 fields):**
- ❌ `threat_model_framework` → Pattern: `^[A-Za-z0-9_-]{1,30}$`
- ❌ `severity` → Pattern: (pending user choice - ASCII or Unicode)

---

## Complete Pattern Summary (Updated)

| Category | Pattern | Max Length | Notes |
|----------|---------|------------|-------|
| **UUID** | `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$` | 36 | RFC 4122 |
| **DateTime** | `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,6})?(Z\|[+-][0-9]{2}:[0-9]{2})$` | 35 | RFC 3339 |
| **URI** | `^[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\\s]{1,1000}$` | 1000 | Relaxed |
| **URL** | `^(https?\|wss?\|file)://[^\\s]{1,1020}$` | 1024 | With file:// |
| **Email** | `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$` | 255 | RFC 5322 |
| **Name (OAuth)** | `^[\\u0020-\\uFFFF]{1,255}$` | **255** | **Updated** |
| **Name (General)** | `^[\\u0020-\\uFFFF]{1,256}$` | 256 | Unicode |
| **Description** | `^[\\u0020-\\uFFFF\\n\\r\\t]{0,1024}$` | 1024 | Multiline |
| **Note Content** | `^[\\u0020-\\uFFFF\\n\\r\\t]{1,65536}$` | 65536 | Markdown |
| **Base64 SVG** | `^[A-Za-z0-9+/]*={0,2}$` | 102400 | 100KB |
| **Identifier (strict)** | `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$` | 100 | Must start letter |
| **OAuth Scope** | `^[\\x21\\x23-\\x5B\\x5D-\\x7E]{3,64}$` | 64 | RFC 6749 |
| **Version** | `^[0-9]+\\.[0-9]+\\.[0-9]+(-[a-zA-Z0-9.]+)?(\\+[a-zA-Z0-9.]+)?$` | 32 | Semver |
| **Color** | `^(#[0-9a-fA-F]{6}\|#[0-9a-fA-F]{3}\|rgb\\([0-9]{1,3},[0-9]{1,3},[0-9]{1,3}\\)\|[a-z]+)$` | 32 | CSS |
| **Font Family** | `^[a-zA-Z0-9 ,'-]{1,64}$` | 64 | CSS |
| **Dash Pattern** | `^[0-9]+(\\.[0-9]+)?(,[0-9]+(\\.[0-9]+)?)*$` | 64 | SVG (decimals) |
| **JSON Path** | `^(/[^/]{1,100}){1,10}$` | **512** | **Updated** |
| **JWT Token** | `^[a-zA-Z0-9_.-]{1,4096}$` | 4096 | - |
| **Filter (LIST)** | `^[\\u0020-\\uFFFF]{1,256}$` | **256** | **NEW** |
| **Framework** | `^[A-Za-z0-9_-]{1,30}$` | **30** | **NEW** |
| **Severity** | (pending user choice) | **50** | **PENDING** |

---

## Implementation Checklist

### Phase 1: Direct Pattern Applications (Ready)
- [x] UUID fields (211)
- [x] DateTime fields (42)
- [x] URI fields (32) - 1000 char limit
- [x] URL fields (3) - include file://
- [x] Email (1)
- [x] Names (20) - **255 for OAuth, 256 for general**
- [x] Descriptions (14)
- [x] Note content (1) - 65536 chars
- [x] Base64 SVG (2) - 100KB
- [x] Identifiers (21)
- [x] OAuth scopes (1)
- [x] Versions (2)
- [x] Colors (6)
- [x] Fonts (2)
- [x] Dash patterns (2) - **decimal support**

### Phase 2: Special Cases (Ready)
- [x] JSON Patch paths - **512 chars**
- [x] LIST filter parameters - **256 chars default**
- [x] Threat model framework - **30 chars, pattern not enum**

### Phase 3: Enum Conversions (Ready)
- [x] Bearer methods (RFC 9728)
- [x] OAuth/OIDC discovery fields (7 fields)
- [x] JWKS fields (2 fields)
- [x] **Webhook event_type** - **NEW: 24 event types**
- [x] Webhook test status
- [x] Token type hint

### Phase 4: Pending User Decision
- [ ] **Severity pattern**: ASCII-only or Unicode?
- [ ] **Severity database**: Create migration to remove CHECK constraint?

### Phase 5: Unbounded Fields (Ready when approved)
- [x] Query/Path parameters (20) - categorized by type
- [x] Request body fields (28) - categorized by type
- [x] Response fields (33) - categorized by type
- [x] Schema definitions (27) - categorized by type

**See**: [docs/tmp/unbounded-fields-analysis.md](docs/tmp/unbounded-fields-analysis.md) for complete breakdown

---

## Database Migration Note

### Required Migration for Severity Field

If you choose to allow custom severity values (Option A or B), you'll need to create a migration:

**File**: `auth/migrations/007_remove_severity_constraint.up.sql`
```sql
-- Remove CHECK constraint on severity to allow custom values
ALTER TABLE threats DROP CONSTRAINT IF EXISTS threats_severity_check;

-- Add comment explaining the change
COMMENT ON COLUMN threats.severity IS 'Severity level - accepts numeric strings (0-5), standard values (Unknown, None, Low, Medium, High, Critical), or custom localized values';
```

**File**: `auth/migrations/007_remove_severity_constraint.down.sql`
```sql
-- Restore original CHECK constraint
ALTER TABLE threats ADD CONSTRAINT threats_severity_check
CHECK (severity IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical'));
```

---

## Questions for Final Approval

1. **Severity Pattern**: ASCII-only `^[A-Za-z0-9_()-]{1,50}$` or Unicode `^[\\u0020-\\uFFFF_().-]{1,50}$`?

2. **Severity Migration**: Should I create migration 007 to remove the CHECK constraint?

3. **Ready to implement?** All other patterns are finalized and ready.

---

**Document Version**: 2.0 (Updated with user clarifications)
**Status**: Awaiting severity pattern decision, otherwise ready for implementation
