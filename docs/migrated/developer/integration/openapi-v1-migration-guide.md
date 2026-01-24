# OpenAPI v1.0.0 Migration Guide for Client Implementers

<!-- Migrated to wiki: API-Integration.md on 2026-01-24 -->
<!-- This document has been migrated to the TMI wiki. See migration summary at end of file. -->

**Document Version:** 1.0
**Publication Date:** 2025-11-01
**Target Audience:** Client application developers integrating with TMI API
**API Version:** v1.0.0 (upgraded from v0.104.0)

---

## Executive Summary

TMI API v1.0.0 introduces **breaking changes** to establish consistent patterns across all resources. This guide helps you migrate your client implementation from v0.104.0 to v1.0.0.

**Migration Complexity:** Medium
**Estimated Effort:** 4-8 hours for typical integration
**Required Actions:** Update request schemas, handle new response fields, migrate batch endpoints

---

## What Changed

### Version Information
- **Previous Version:** 0.104.0 (main branch)
- **New Version:** 1.0.0 (feature-openapifinal branch)
- **Breaking Changes:** 6 categories
- **New Features:** PATCH support for all resources, enhanced bulk operations

### Impact Summary

| Category | Impact | Action Required |
|----------|--------|----------------|
| Request Schemas | **HIGH** | Update POST/PUT request bodies |
| Response Schemas | **HIGH** | Handle new timestamp fields |
| Batch Endpoints | **MEDIUM** | Migrate to bulk endpoints |
| List Responses | **MEDIUM** | Handle Note summaries |
| Bulk Operations | **LOW** | Optional - use new capabilities |
| PATCH Support | **LOW** | Optional - use new endpoints |

---

## Breaking Changes You Must Address

### 1. Request Schema Changes (CRITICAL)

**What Changed:** POST and PUT operations now use `Input` schemas that exclude server-generated fields.

**Affected Resources:** Assets, Documents, Notes, Repositories

**Before (v0.104.0):**
```json
POST /threat_models/{id}/assets
{
  "id": "6ba7b810-9dad-11d1-beef-00c04fd430c8",  ❌ Don't send
  "name": "Customer Database",
  "type": "software",
  "metadata": [],  ❌ Don't send
  "description": "Primary customer data store"
}
```

**After (v1.0.0):**
```json
POST /threat_models/{id}/assets
{
  "name": "Customer Database",
  "type": "software",
  "description": "Primary customer data store"
}
```

**Migration Steps:**

1. **Remove these fields from POST/PUT requests:**
   - `id` (server-generated UUID)
   - `metadata` (managed via separate metadata endpoints)
   - `created_at` (server-generated timestamp)
   - `modified_at` (server-generated timestamp)

2. **Update your code:**

```javascript
// ❌ BEFORE (v0.104.0)
async function createAsset(tmId, assetData) {
  return await POST(`/threat_models/${tmId}/assets`, {
    id: uuid(),  // ❌ Remove - server generates
    metadata: [],  // ❌ Remove - use metadata endpoints
    ...assetData
  });
}

// ✅ AFTER (v1.0.0)
async function createAsset(tmId, assetData) {
  return await POST(`/threat_models/${tmId}/assets`, {
    name: assetData.name,
    type: assetData.type,
    description: assetData.description,
    // Only user-writable fields
  });
}
```

3. **If using code generation tools:**
   - Regenerate client SDKs from new OpenAPI spec
   - Use `AssetInput`, `DocumentInput`, `NoteInput`, `RepositoryInput` types for requests
   - Use `Asset`, `Document`, `Note`, `Repository` types for responses

---

### 2. Response Schema Changes (CRITICAL)

**What Changed:** All resources now include `created_at` and `modified_at` timestamps in responses.

**Affected Resources:** Assets, Documents, Notes, Repositories

**Before (v0.104.0):**
```json
{
  "id": "6ba7b810-9dad-11d1-beef-00c04fd430c8",
  "name": "Customer Database",
  "type": "software"
}
```

**After (v1.0.0):**
```json
{
  "id": "6ba7b810-9dad-11d1-beef-00c04fd430c8",
  "name": "Customer Database",
  "type": "software",
  "created_at": "2025-11-01T12:00:00.000Z",
  "modified_at": "2025-11-01T14:30:00.000Z"
}
```

**Migration Steps:**

1. **Update response type definitions:**

```typescript
// ❌ BEFORE (v0.104.0)
interface Asset {
  id: string;
  name: string;
  type: string;
  description?: string;
  metadata?: Metadata[];
}

// ✅ AFTER (v1.0.0)
interface Asset {
  id: string;
  name: string;
  type: string;
  description?: string;
  metadata?: Metadata[];
  created_at: string;  // RFC3339 timestamp
  modified_at: string;  // RFC3339 timestamp
}
```

2. **Benefits you can now leverage:**
   - Sort resources by creation or modification time
   - Implement cache invalidation strategies
   - Display "last updated" timestamps in UI
   - Track resource lifecycle for auditing

```javascript
// Example: Sort assets by most recently modified
const assets = await getAssets(tmId);
assets.sort((a, b) =>
  new Date(b.modified_at) - new Date(a.modified_at)
);
```

---

### 3. Batch to Bulk Endpoint Migration (CRITICAL for Threats)

**What Changed:** Removed `/batch` endpoints for threats. Use `/bulk` endpoints instead.

**Removed Endpoints:**
```
❌ DELETE /threat_models/{id}/threats/batch
❌ PATCH  /threat_models/{id}/threats/batch/patch
```

**Replacement Endpoints:**
```
✅ DELETE /threat_models/{id}/threats/bulk
✅ PATCH  /threat_models/{id}/threats/bulk
```

**Migration Steps:**

1. **Update endpoint paths:**

```javascript
// ❌ BEFORE (v0.104.0)
async function batchDeleteThreats(tmId, threatIds) {
  return await DELETE(`/threat_models/${tmId}/threats/batch`, {
    body: threatIds
  });
}

// ✅ AFTER (v1.0.0)
async function bulkDeleteThreats(tmId, threatIds) {
  return await DELETE(`/threat_models/${tmId}/threats/bulk`, {
    body: threatIds
  });
}
```

2. **Update batch PATCH operations:**

```javascript
// ❌ BEFORE (v0.104.0)
PATCH /threat_models/{id}/threats/batch/patch

// ✅ AFTER (v1.0.0)
PATCH /threat_models/{id}/threats/bulk
```

**Complete Bulk Endpoint Mapping:**

| Operation | v0.104.0 | v1.0.0 |
|-----------|----------|--------|
| Bulk Create | `POST /threats/bulk` | `POST /threats/bulk` (unchanged) |
| Bulk Upsert | `PUT /threats/bulk` | `PUT /threats/bulk` (unchanged) |
| Bulk Partial Update | `PATCH /threats/batch/patch` | `PATCH /threats/bulk` ⚠️ |
| Bulk Delete | `DELETE /threats/batch` | `DELETE /threats/bulk` ⚠️ |

---

### 4. List Response Changes (MEDIUM Impact)

**What Changed:** Note list endpoints now return summary schemas without the `content` field.

**Affected Endpoint:** `GET /threat_models/{id}/notes`

**Before (v0.104.0):**
```json
GET /threat_models/{id}/notes
[
  {
    "id": "uuid-1",
    "name": "Security Review Notes",
    "description": "Initial threat assessment",
    "content": "<potentially large content here>",
    "metadata": []
  }
]
```

**After (v1.0.0):**
```json
GET /threat_models/{id}/notes
[
  {
    "id": "uuid-1",
    "name": "Security Review Notes",
    "description": "Initial threat assessment",
    "metadata": [],
    "created_at": "2025-11-01T12:00:00.000Z",
    "modified_at": "2025-11-01T12:00:00.000Z"
  }
]
```

**Migration Steps:**

1. **Fetch individual notes to get content:**

```javascript
// ❌ BEFORE (v0.104.0) - content in list
const notes = await GET(`/threat_models/${tmId}/notes`);
const firstNoteContent = notes[0].content;  // Available in list

// ✅ AFTER (v1.0.0) - fetch individual note for content
const notes = await GET(`/threat_models/${tmId}/notes`);
const fullNote = await GET(`/threat_models/${tmId}/notes/${notes[0].id}`);
const firstNoteContent = fullNote.content;  // Fetch individual note
```

2. **Update your UI logic:**

```javascript
// Display note list (summary information)
function NoteList({ notes }) {
  return notes.map(note => (
    <NoteListItem
      key={note.id}
      name={note.name}
      description={note.description}
      modified={note.modified_at}
      onClick={() => fetchFullNote(note.id)}  // Fetch content on demand
    />
  ));
}

// Fetch full note when needed
async function fetchFullNote(noteId) {
  const fullNote = await GET(`/threat_models/${tmId}/notes/${noteId}`);
  return fullNote.content;  // Now has content field
}
```

**Why this changed:** The `content` field can contain large amounts of data. Excluding it from list responses improves performance and reduces bandwidth usage.

---

## New Capabilities You Can Use

### 5. Enhanced Bulk Operations (Optional)

**What's New:** Assets, Documents, and Repositories now support bulk upsert (PUT).

**New Capabilities:**

```javascript
// Bulk upsert assets (create or replace)
PUT /threat_models/{id}/assets/bulk
[
  {"name": "Asset 1", "type": "software", "description": "..."},
  {"name": "Asset 2", "type": "data", "description": "..."}
]

// Bulk upsert documents
PUT /threat_models/{id}/documents/bulk
[
  {"name": "Doc 1", "uri": "https://example.com/doc1"},
  {"name": "Doc 2", "uri": "https://example.com/doc2"}
]

// Bulk partial update threats (new PATCH method)
PATCH /threat_models/{id}/threats/bulk
[
  {"id": "uuid-1", "severity": "critical"},
  {"id": "uuid-2", "mitigated": true}
]

// Bulk delete threats (new DELETE method)
DELETE /threat_models/{id}/threats/bulk
["uuid-1", "uuid-2", "uuid-3"]
```

**When to use:**
- **POST** - Create multiple new resources (fails if any ID exists)
- **PUT** - Create or fully replace multiple resources (upsert semantics)
- **PATCH** - Partially update specific fields on multiple resources
- **DELETE** - Remove multiple resources by ID array

---

### 6. Universal PATCH Support (Optional)

**What's New:** All resources now support JSON Patch (RFC 6902) for partial updates.

**New Endpoints:**
```
PATCH /threat_models/{id}/assets/{asset_id}
PATCH /threat_models/{id}/documents/{document_id}
PATCH /threat_models/{id}/notes/{note_id}
PATCH /threat_models/{id}/repositories/{repository_id}
```

**Example Use Cases:**

```javascript
// Update asset name without touching other fields
PATCH /threat_models/{id}/assets/{asset_id}
[
  {"op": "replace", "path": "/name", "value": "Updated Database Name"}
]

// Add classification tag to asset
PATCH /threat_models/{id}/assets/{asset_id}
[
  {"op": "add", "path": "/classification/-", "value": "pii"}
]

// Conditionally update sensitivity (test before replace)
PATCH /threat_models/{id}/assets/{asset_id}
[
  {"op": "test", "path": "/sensitivity", "value": "medium"},
  {"op": "replace", "path": "/sensitivity", "value": "high"}
]

// Multiple operations in one request
PATCH /threat_models/{id}/notes/{note_id}
[
  {"op": "replace", "path": "/name", "value": "Updated Note Title"},
  {"op": "replace", "path": "/description", "value": "New description"}
]
```

**Benefits:**
- More efficient than GET-modify-PUT pattern
- Reduces race conditions in concurrent scenarios
- Atomic operations with test-before-replace semantics
- Precise field-level updates

---

## Migration Checklist

Use this checklist to ensure your migration is complete:

### Phase 1: Update Request Bodies
- [ ] Remove `id` from POST requests for Assets, Documents, Notes, Repositories
- [ ] Remove `metadata` from POST requests (use metadata endpoints instead)
- [ ] Remove `created_at` and `modified_at` from POST/PUT requests
- [ ] Update type definitions to use `*Input` schemas for requests

### Phase 2: Update Response Handling
- [ ] Add `created_at` field to Asset response type
- [ ] Add `modified_at` field to Asset response type
- [ ] Add `created_at` field to Document response type
- [ ] Add `modified_at` field to Document response type
- [ ] Add `created_at` field to Note response type
- [ ] Add `modified_at` field to Note response type
- [ ] Add `created_at` field to Repository response type
- [ ] Add `modified_at` field to Repository response type

### Phase 3: Migrate Batch Endpoints
- [ ] Replace `DELETE /threats/batch` with `DELETE /threats/bulk`
- [ ] Replace `PATCH /threats/batch/patch` with `PATCH /threats/bulk`
- [ ] Test bulk PATCH operations
- [ ] Test bulk DELETE operations

### Phase 4: Handle List Response Changes
- [ ] Update Note list handler to expect `NoteListItem` (no content field)
- [ ] Fetch individual notes to access content field
- [ ] Update UI to fetch content on demand

### Phase 5: Testing
- [ ] Test POST requests without read-only fields
- [ ] Verify responses include new timestamp fields
- [ ] Test bulk endpoints with new methods (PUT, PATCH, DELETE)
- [ ] Verify note list returns summaries, individual note returns full data
- [ ] Test error handling for deprecated endpoints

### Phase 6: Optional Enhancements
- [ ] Implement PATCH for efficient partial updates
- [ ] Use bulk upsert (PUT) operations where beneficial
- [ ] Leverage timestamps for cache invalidation
- [ ] Sort/filter resources by creation/modification time

---

## Code Generation

If you use code generation tools (oapi-codegen, OpenAPI Generator, Swagger Codegen):

1. **Download the new OpenAPI specification:**
   ```bash
   curl https://api.tmi.example.com/openapi.json > tmi-openapi-v1.json
   ```

2. **Regenerate your client SDK:**
   ```bash
   # Example with oapi-codegen
   oapi-codegen -package tmiclient tmi-openapi-v1.json > tmiclient.go

   # Example with OpenAPI Generator
   openapi-generator generate -i tmi-openapi-v1.json -g typescript-fetch -o ./src/client
   ```

3. **Review generated types:**
   - New types: `AssetBase`, `AssetInput`, `DocumentBase`, `DocumentInput`, etc.
   - Updated types: `Asset`, `Document`, `Note`, `Repository` (now include timestamps)
   - New types: `NoteListItem` (summary schema)

4. **Update your code to use new types:**
   ```typescript
   // Use Input types for requests
   function createAsset(data: AssetInput): Promise<Asset> { }

   // Response types include timestamps
   interface Asset {
     id: string;
     name: string;
     created_at: string;  // New field
     modified_at: string;  // New field
   }
   ```

---

## Testing Your Migration

### 1. Request Schema Validation

Test that POST/PUT requests work without server-generated fields:

```bash
# Should succeed
curl -X POST https://api.tmi.example.com/threat_models/{id}/assets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Test Asset",
    "type": "software"
  }'

# Should fail or ignore these fields
curl -X POST https://api.tmi.example.com/threat_models/{id}/assets \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "id": "custom-id",
    "name": "Test Asset",
    "type": "software",
    "created_at": "2025-01-01T00:00:00Z"
  }'
```

### 2. Response Schema Validation

Verify responses include timestamps:

```bash
curl https://api.tmi.example.com/threat_models/{id}/assets/{asset_id} \
  -H "Authorization: Bearer $TOKEN" | jq '.created_at, .modified_at'

# Expected output:
# "2025-11-01T12:00:00.000Z"
# "2025-11-01T14:30:00.000Z"
```

### 3. Batch to Bulk Migration

Test that old batch endpoints are removed:

```bash
# Should return 404
curl -X DELETE https://api.tmi.example.com/threat_models/{id}/threats/batch

# Should succeed
curl -X DELETE https://api.tmi.example.com/threat_models/{id}/threats/bulk \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '["uuid-1", "uuid-2"]'
```

### 4. List Response Validation

Verify note list returns summaries:

```bash
# List should NOT include content field
curl https://api.tmi.example.com/threat_models/{id}/notes \
  -H "Authorization: Bearer $TOKEN" | jq '.[0] | has("content")'
# Expected: false

# Individual note SHOULD include content field
curl https://api.tmi.example.com/threat_models/{id}/notes/{note_id} \
  -H "Authorization: Bearer $TOKEN" | jq 'has("content")'
# Expected: true
```

---

## Common Issues and Solutions

### Issue 1: "Field 'id' is read-only"

**Symptom:** POST requests fail with validation error about read-only fields.

**Solution:** Remove `id`, `metadata`, `created_at`, `modified_at` from request body.

```javascript
// ❌ Wrong
const asset = { id: uuid(), name: "Asset", type: "software" };

// ✅ Correct
const asset = { name: "Asset", type: "software" };
```

---

### Issue 2: "Missing required field: created_at"

**Symptom:** Response parsing fails because timestamps are missing.

**Solution:** Update your response type definitions to include timestamps.

```typescript
// ❌ Old type
interface Asset {
  id: string;
  name: string;
}

// ✅ New type
interface Asset {
  id: string;
  name: string;
  created_at: string;
  modified_at: string;
}
```

---

### Issue 3: "Endpoint /threats/batch not found"

**Symptom:** Batch operations return 404.

**Solution:** Replace `/batch` with `/bulk` in endpoint paths.

```javascript
// ❌ Old endpoint
DELETE /threat_models/{id}/threats/batch

// ✅ New endpoint
DELETE /threat_models/{id}/threats/bulk
```

---

### Issue 4: "Note content missing in list"

**Symptom:** Note list doesn't include content field.

**Solution:** Fetch individual notes to get content.

```javascript
// List notes (summary)
const notes = await GET('/threat_models/{id}/notes');

// Get full note with content
const fullNote = await GET(`/threat_models/{id}/notes/${notes[0].id}`);
console.log(fullNote.content);
```

---

## Additional Resources

- **OpenAPI Specification:** [tmi-openapi.json](../reference/apis/tmi-openapi.json) <!-- VERIFIED: File exists at docs/reference/apis/tmi-openapi.json -->
- **API Design Documentation:** See OpenAPI description field for design rationale <!-- VERIFIED: Description field contains v1.0.0 design info -->
- **Schema Analysis:** <!-- NEEDS-REVIEW: File openapi-schema-analysis.md does not exist at docs/reference/apis/ -->
- **Normalization Plan:** <!-- NEEDS-REVIEW: File openapi-schema-normalization-plan.md does not exist at docs/reference/apis/ -->
- **Integration Guide:** [client-integration-guide.md](./client-integration-guide.md) <!-- VERIFIED: File exists at docs/developer/integration/client-integration-guide.md -->

---

## Support

If you encounter issues during migration:

1. Check this migration guide for common issues
2. Review the OpenAPI specification for schema details
3. Test your implementation against the v1.0.0 API
4. File issues at: [GitHub Issues](https://github.com/ericfitz/tmi/issues)

---

## Document Revision History

| Version | Date | Changes |
|---------|------|---------|
| 1.0 | 2025-11-01 | Initial migration guide for v1.0.0 release |

---

## Verification Summary

**Verified on:** 2026-01-24

### Schema Verifications (All PASSED)

| Item | Status | Details |
|------|--------|---------|
| `AssetInput` schema | VERIFIED | Exists in OpenAPI spec at line 3030 |
| `DocumentInput` schema | VERIFIED | Exists in OpenAPI spec at line 3085 |
| `NoteInput` schema | VERIFIED | Exists in OpenAPI spec at line 3147 |
| `RepositoryInput` schema | VERIFIED | Exists in OpenAPI spec at line 3304 |
| `NoteListItem` schema | VERIFIED | Exists in OpenAPI spec at line 3159, excludes `content` field |
| API version 1.0.0 | VERIFIED | OpenAPI spec shows `"version": "1.0.0"` |
| `created_at`/`modified_at` timestamps | VERIFIED | Present in all response schemas |

### Endpoint Verifications (All PASSED)

| Item | Status | Details |
|------|--------|---------|
| `/threats/bulk` endpoint | VERIFIED | Exists in OpenAPI spec at line 12234 |
| `/threats/batch` endpoints | VERIFIED REMOVED | No matches found - correctly deprecated |
| PATCH on `/assets/{asset_id}` | VERIFIED | Exists in OpenAPI spec at line 22831 |
| JSON Patch (RFC 6902) support | VERIFIED | Content type `application/json-patch+json` |

### File Reference Verifications

| Item | Status | Details |
|------|--------|---------|
| `tmi-openapi.json` | VERIFIED | File exists at docs/reference/apis/ |
| `client-integration-guide.md` | VERIFIED | File exists at docs/developer/integration/ |
| `openapi-schema-analysis.md` | NOT FOUND | File does not exist |
| `openapi-schema-normalization-plan.md` | NOT FOUND | File does not exist |
| GitHub Issues URL | VERIFIED | https://github.com/ericfitz/tmi/issues exists |

### External Tool Verifications

| Item | Status | Details |
|------|--------|---------|
| oapi-codegen | VERIFIED | Go OpenAPI code generator, confirmed via web search |
| OpenAPI Generator | VERIFIED | Multi-language code generator, confirmed via web search |
| Swagger Codegen | VERIFIED | Code generation tool, confirmed via web search |

### Items Requiring Review

1. **Broken file references**: Links to `openapi-schema-analysis.md` and `openapi-schema-normalization-plan.md` are dead links - these files do not exist in the repository

### Migration Information

This document was migrated to the TMI wiki at:
- **Wiki page**: `API-Integration.md` (v1.0.0 migration content integrated)
- **Migration date**: 2026-01-24
- **New location**: `docs/migrated/developer/integration/openapi-v1-migration-guide.md`
