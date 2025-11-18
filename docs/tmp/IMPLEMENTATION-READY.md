# Implementation Ready - Final Approved Specification

**Date**: 2025-01-18
**Status**: ✅ APPROVED - Ready for Implementation

## User Decisions - Final

### 1. ✅ Severity Field
- **Pattern**: Unicode `^[\u0020-\uFFFF_().-]{1,50}$`
- **maxLength**: 50
- **Database Migration**: Create migration 007 to remove CHECK constraint

### 2. ✅ Webhook Event Types
- **Type**: ENUM (exact matching only)
- **Values**: 24 event types in `{resource}.{action}` format
- **NO resource-only patterns** (e.g., `"diagram"` alone - handler doesn't support this)

### 3. ✅ Unicode Support for All Name/Description/Mitigation Fields
All `name`, `description`, and `mitigation` fields must support Unicode.

---

## Database Migration Required

### Migration 007: Remove Severity CHECK Constraint

**File**: `auth/migrations/007_remove_severity_constraint.up.sql`

```sql
-- Remove CHECK constraint on severity to allow custom and localized values
-- This allows:
--   - Numeric values: "0", "1", "2", "3", "4", "5"
--   - English values: "Unknown", "None", "Low", "Medium", "High", "Critical"
--   - Localized values: "Bajo", "Medio", "Alto", "低", "中", "高"
--   - Custom values with parentheses: "Risk(3)", "Custom-Level"

ALTER TABLE threats DROP CONSTRAINT IF EXISTS threats_severity_check;

-- Add comment explaining the change
COMMENT ON COLUMN threats.severity IS
  'Severity level - accepts numeric strings (0-5), standard values (Unknown, None, Low, Medium, High, Critical), custom values, or localized strings. Supports Unicode characters, alphanumeric, hyphens, underscores, parentheses, periods.';
```

**File**: `auth/migrations/007_remove_severity_constraint.down.sql`

```sql
-- Restore original CHECK constraint
-- WARNING: This will fail if any custom severity values exist in the database

ALTER TABLE threats ADD CONSTRAINT threats_severity_check
  CHECK (severity IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical'));

-- Remove comment
COMMENT ON COLUMN threats.severity IS NULL;
```

---

## OpenAPI Changes Summary

### Enums to Add (11 total)

1. **bearer_methods_supported** (RFC 9728)
   ```json
   {
     "type": "array",
     "items": {
       "type": "string",
       "enum": ["header", "body", "query"]
     }
   }
   ```

2. **event_type** (WebhookDelivery, WebhookTestRequest) - **24 values**
   ```json
   {
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
     "description": "Webhook event type following {resource}.{action} pattern"
   }
   ```

3-11. **OAuth/OIDC Standard Enums**:
   - response_types_supported
   - subject_types_supported
   - id_token_signing_alg_values_supported
   - grant_types_supported
   - token_endpoint_auth_methods_supported
   - token_type_hint
   - jwks.keys[].kty
   - jwks.keys[].use
   - webhook_test_response.status

---

## Fields Requiring Unicode Pattern Addition

### Name Fields (36 occurrences)
**Fields currently WITHOUT Unicode pattern:**

**Response Fields:**
- `application/json.schema.properties.name` (userinfo, introspect responses)
- `groups.items.properties.name`
- `providers.items.properties.name`

**Schema Fields:**
- `CreateDiagramRequest.properties.name`
- `DiagramListItem.properties.name`
- `ThreatModelInput.properties.name`
- `User.properties.name` (OAuth - **255 max**)
- `WebhookSubscription.properties.name`
- `WebhookSubscriptionInput.properties.name`
- `operator.properties.name`
- `service.properties.name`
- `sourceMarker.properties.name` (32 max)
- `targetMarker.properties.name` (32 max)
- Plus ~23 more schema name fields

**Pattern to Apply**: `^[\u0020-\uFFFF]{1,{maxLength}}$`
- Standard: 256 chars
- OAuth names: 255 chars
- Marker names: 32 chars

### Description Fields (12 occurrences)
**Fields currently WITHOUT Unicode pattern:**

- `AssetBase.properties.description`
- `BaseDiagram.properties.description`
- `BaseDiagramInput.properties.description`
- `DocumentBase.properties.description`
- `NoteBase.properties.description`
- `NoteListItem.properties.description`
- `RepositoryBase.properties.description`
- `TMListItem.properties.description`
- `ThreatBase.properties.description`
- `ThreatModelInput.properties.description`
- Plus 2 more

**Pattern to Apply**: `^[\u0020-\uFFFF\n\r\t]{0,1024}$`
- maxLength: 1024
- Allows empty (min 0)
- Multiline support

### Mitigation Field (1 occurrence)
**Field**: `ThreatBase.properties.mitigation`

**Current State**:
```json
{
  "type": "string",
  "description": "Recommended or planned mitigation(s) for the threat",
  "maxLength": 1024
}
```

**Add Pattern**: `^[\u0020-\uFFFF\n\r\t]{0,1024}$`

**Database**: TEXT (no limit, compatible)

---

## Special Pattern Updates

### 1. Severity Field
**Current** (has CHECK constraint):
```json
{
  "type": "string",
  "maxLength": 50
}
```

**Updated**:
```json
{
  "type": "string",
  "maxLength": 50,
  "pattern": "^[\\u0020-\\uFFFF_().-]{1,50}$",
  "description": "Severity level (numeric, localized, or custom string with alphanumeric, hyphens, underscores, parentheses, periods)"
}
```

### 2. Threat Model Framework
**Add Pattern**:
```json
{
  "type": "string",
  "maxLength": 30,
  "pattern": "^[A-Za-z0-9_-]{1,30}$",
  "description": "Threat modeling framework (e.g., STRIDE, LINDDUN, PASTA, or custom)"
}
```

### 3. JSON Patch Paths
**Update maxLength to 512**:
```json
{
  "path": {
    "type": "string",
    "maxLength": 512,
    "pattern": "^(/[^/]{1,100}){1,10}$",
    "description": "JSON Pointer path (RFC 6901)"
  },
  "from": {
    "type": "string",
    "maxLength": 512,
    "pattern": "^(/[^/]{1,100}){1,10}$",
    "description": "Source path for move/copy operations"
  }
}
```

### 4. LIST Filter Parameters
**All query filters → maxLength 256**:
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

### 5. AntV/X6 Dash Patterns
**Update to support decimals**:
```json
{
  "strokeDasharray": {
    "type": "string",
    "maxLength": 64,
    "pattern": "^[0-9]+(\\.[0-9]+)?(,[0-9]+(\\.[0-9]+)?)*$",
    "description": "SVG stroke dash pattern (supports decimals)"
  }
}
```

---

## Complete Pattern Reference

| Field Type | Pattern | Max Length | Notes |
|------------|---------|------------|-------|
| **UUID** | `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$` | 36 | RFC 4122 |
| **DateTime** | `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\\.[0-9]{1,6})?(Z\|[+-][0-9]{2}:[0-9]{2})$` | 35 | RFC 3339 |
| **URI** | `^[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\\s]{1,1000}$` | 1000 | Relaxed |
| **URL** | `^(https?\|wss?\|file)://[^\\s]{1,1020}$` | 1024 | With file:// |
| **Email** | `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$` | 255 | RFC 5322 |
| **Name (OAuth)** | `^[\\u0020-\\uFFFF]{1,255}$` | 255 | Unicode |
| **Name (General)** | `^[\\u0020-\\uFFFF]{1,256}$` | 256 | Unicode |
| **Name (Marker)** | `^[\\u0020-\\uFFFF]{1,32}$` | 32 | Unicode |
| **Description** | `^[\\u0020-\\uFFFF\\n\\r\\t]{0,1024}$` | 1024 | Unicode, multiline |
| **Mitigation** | `^[\\u0020-\\uFFFF\\n\\r\\t]{0,1024}$` | 1024 | Unicode, multiline |
| **Note Content** | `^[\\u0020-\\uFFFF\\n\\r\\t]{1,65536}$` | 65536 | Markdown |
| **Severity** | `^[\\u0020-\\uFFFF_().-]{1,50}$` | 50 | **Unicode, custom** |
| **Framework** | `^[A-Za-z0-9_-]{1,30}$` | 30 | Alphanumeric |
| **Base64 SVG** | `^[A-Za-z0-9+/]*={0,2}$` | 102400 | 100KB |
| **Identifier** | `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$` | 100 | Strict |
| **OAuth Scope** | `^[\\x21\\x23-\\x5B\\x5D-\\x7E]{3,64}$` | 64 | RFC 6749 |
| **Version** | `^[0-9]+\\.[0-9]+\\.[0-9]+(-[a-zA-Z0-9.]+)?(\\+[a-zA-Z0-9.]+)?$` | 32 | Semver |
| **Color** | `^(#[0-9a-fA-F]{6}\|#[0-9a-fA-F]{3}\|rgb\\([0-9]{1,3},[0-9]{1,3},[0-9]{1,3}\\)\|[a-z]+)$` | 32 | CSS |
| **Font** | `^[a-zA-Z0-9 ,'-]{1,64}$` | 64 | CSS |
| **Dash Pattern** | `^[0-9]+(\\.[0-9]+)?(,[0-9]+(\\.[0-9]+)?)*$` | 64 | SVG decimals |
| **JSON Path** | `^(/[^/]{1,100}){1,10}$` | 512 | RFC 6901 |
| **JWT Token** | `^[a-zA-Z0-9_.-]{1,4096}$` | 4096 | - |
| **Filter (LIST)** | `^[\\u0020-\\uFFFF]{1,256}$` | 256 | Unicode |

---

## Implementation Steps

### Step 1: Create Database Migration
```bash
# Create migration files
touch auth/migrations/007_remove_severity_constraint.up.sql
touch auth/migrations/007_remove_severity_constraint.down.sql

# Add SQL content (see above)
```

### Step 2: Update OpenAPI Spec with jq
Due to 432KB file size, use jq for efficient updates:

```bash
# Backup
cp docs/reference/apis/tmi-openapi.json docs/reference/apis/tmi-openapi.json.backup

# Apply patterns (jq scripts to be created)
./scripts/apply-openapi-patterns.sh
```

### Step 3: Validate OpenAPI
```bash
make validate-openapi
```

### Step 4: Regenerate API Code
```bash
make generate-api
```

### Step 5: Run Tests
```bash
make lint
make build-server
make test-unit
make test-integration
```

### Step 6: Verify XML Security Enhancement
Enhance SAML XML parsing in `auth/saml/provider.go:258-282` with:
- Size check (100KB limit)
- Strict parsing mode
- CharsetReader disabled
- Structure validation

---

## Summary of Changes

### Patterns Added: ~470 fields
- 211 UUID
- 42 DateTime
- 32 URI
- 36+ Name (with Unicode)
- 12+ Description (with Unicode)
- 1 Mitigation (with Unicode)
- 1 Severity (with Unicode)
- Plus all other categories

### Enums Added: 11 fields
- 1 bearer_methods_supported (3 values)
- 1 event_type (24 values)
- 9 OAuth/OIDC standards

### Unbounded Fields Fixed: 74 unique fields
All given appropriate maxLength + patterns

### Database Migrations: 1
- Migration 007: Remove severity CHECK constraint

### Code Enhancements: 1
- SAML XML security hardening

---

**Ready to Implement**: ✅
**Approvals**: All user decisions confirmed
**Documentation**: Complete in docs/tmp/

---

**Next Action**: Proceed with implementation?
