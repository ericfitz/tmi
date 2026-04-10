# External Embedding APIs Design

**Issue:** #241 (sub-spec 2 of 3)
**Date:** 2026-04-10
**Scope:** New `embedding-automation` built-in group, automation middleware, embedding config-sharing endpoint, embedding ingestion endpoint, and bulk delete endpoint
**Depends on:** Sub-spec 1 (dual-index infrastructure) — completed

## Overview

Add API endpoints that allow external automation tools to push pre-computed embeddings into TMI's dual-index vector store. This enables specialized embedding models (e.g., code-specific models) to run outside the TMI process, offloading CPU/memory-intensive work and enabling model flexibility.

The endpoints live under a new `/automation/embeddings/` URL prefix, gated by a dedicated `embedding-automation` built-in group. This group represents a higher trust level than the existing `tmi-automation` group because it shares embedding provider API keys.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Authorization model | Dedicated `embedding-automation` group | Higher privilege than `tmi-automation` — shares API keys with TMI server |
| URL prefix | `/automation/embeddings/{threat_model_id}` | Separate from threat model resource tree; layered middleware (automation outer, embedding inner) |
| Config endpoint access | `embedding-automation` only | Exposes API keys — strictest access |
| Ingestion endpoint access | `embedding-automation` only | Consistent with config endpoint |
| Ingestion model | Batch of pre-computed vectors | Offloads embedding work from TMI; enables specialized models |
| Index invalidation | Immediate on ingestion/delete | `InvalidateIndex` is safe (skips active sessions); new data discoverable on next query |
| Delete return values | All delete methods return `(int64, error)` | Consistent count reporting in API responses |

## Section 1: `embedding-automation` Built-In Group

### Constants

**`api/validation/validators.go`** — add:
```go
EmbeddingAutomationGroupUUID = "00000000-0000-0000-0000-000000000005"
```

**`api/auth_utils.go`** — add:
```go
EmbeddingAutomationGroup = "embedding-automation"
```

### Group Variable

**`api/group_membership.go`** — add:
```go
GroupEmbeddingAutomation = BuiltInGroup{Name: EmbeddingAutomationGroup, UUID: uuid.MustParse(EmbeddingAutomationGroupUUID)}
```

### Seed

**`api/seed/seed.go`** — new `seedEmbeddingAutomationGroup(db)` function:
- Creates group with `Provider: "*"`, `GroupName: "embedding-automation"`, `InternalUUID: ...005`
- Called from `SeedDatabase` before the cleanup step
- Idempotent via `FirstOrCreate` pattern (same as all other seed functions)

### No Automatic Threat Model Access

Unlike `tmi-automation`, the `embedding-automation` group does NOT get automatic writer access to new threat models. Access to embedding endpoints is controlled solely by group membership — the threat model existence check is in the handler, not via the threat model authorization middleware.

## Section 2: OpenAPI Endpoints

### URL Prefix

`/automation/embeddings/{threat_model_id}`

All three endpoints are defined in `api-schema/tmi-openapi.json` and flow through the standard OpenAPI code generation pipeline.

### `GET /automation/embeddings/{threat_model_id}/config`

Returns embedding model configuration including API keys.

**Response 200** — `EmbeddingConfig`:
```json
{
  "text_embedding": {
    "provider": "openai",
    "model": "text-embedding-3-small",
    "api_key": "sk-...",
    "base_url": ""
  },
  "code_embedding": {
    "provider": "openai",
    "model": "code-embedding-model",
    "api_key": "sk-...",
    "base_url": ""
  }
}
```

- `text_embedding` is always present when Timmy is configured
- `code_embedding` is null/omitted if code embedding is not configured (`IsCodeIndexConfigured()` returns false)
- No live API calls — reads directly from server config

**Errors:** 401 (no auth), 403 (not in `embedding-automation` group), 404 (threat model not found)

### `POST /automation/embeddings/{threat_model_id}`

Ingests pre-computed embeddings.

**Request** — `EmbeddingIngestionRequest`:
```json
{
  "index_type": "code",
  "embeddings": [
    {
      "entity_type": "repository",
      "entity_id": "uuid",
      "chunk_index": 0,
      "chunk_text": "func main() {...}",
      "content_hash": "sha256hex",
      "embedding_model": "code-embedding-model",
      "embedding_dim": 768,
      "vector": [0.1, 0.2, ...]
    }
  ]
}
```

**Validation:**
- `index_type` must be "text" or "code" — 400 if invalid
- Entity type / index type consistency: repositories must use "code", all other entity types must use "text" — 422 if mismatched
- Vector dimensions must be consistent within the batch — 422 if inconsistent

**On success:**
1. Convert to `[]models.TimmyEmbedding` with `ThreatModelID` and `IndexType` set
2. Store via `GlobalTimmyEmbeddingStore.CreateBatch()`
3. Call `vectorManager.InvalidateIndex(threatModelID, indexType)`

**Response 201:** `{"ingested": N}`

**Errors:** 401, 403, 404 (threat model), 400 (invalid request/index_type), 422 (entity/index type mismatch or dimension inconsistency)

### `DELETE /automation/embeddings/{threat_model_id}`

Bulk delete with query parameter filters.

**Query parameters** (all optional, at least one required):
- `entity_type` — filter by entity type
- `entity_id` — filter by entity ID (must be paired with `entity_type`; 400 if provided alone)
- `index_type` — filter by index type ("text" or "code")

**Delete routing:**
- `entity_type` + `entity_id` provided: call `DeleteByEntity(ctx, threatModelID, entityType, entityID)`
- `index_type` only: call `DeleteByThreatModelAndIndexType(ctx, threatModelID, indexType)`
- Combinations: chain filters appropriately

**On success:**
- Call `InvalidateIndex` for affected index types
- Return 200 with `{"deleted": N}`

**Errors:** 401, 403, 404 (threat model), 400 (no filters provided)

## Section 3: Authorization Middleware

Two middleware functions in `api/automation_middleware.go`:

### `AutomationMiddleware`

Applied to `/automation/*` route group.

1. Resolve caller identity via `ResolveMembershipContext(c)`
2. Check membership in `embedding-automation` OR `tmi-automation` group
3. 403 if neither — `{"code": "forbidden", "message": "automation group membership required"}`
4. `c.Next()` if authorized

This is the outer gate — any automation account can reach the `/automation` namespace.

### `EmbeddingAutomationMiddleware`

Applied to `/automation/embeddings/*` route group (nested inside the outer group).

1. Resolve caller identity via `ResolveMembershipContext(c)` (already resolved by outer middleware, but re-resolved for safety since Gin middleware doesn't share state)
2. Check membership in `embedding-automation` group only
3. 403 if not — `{"code": "forbidden", "message": "embedding-automation group membership required"}`
4. `c.Next()` if authorized

This is the inner gate — only the higher-privilege embedding automation accounts proceed.

## Section 4: Handlers

New file `api/timmy_embedding_automation_handlers.go`.

### Handler Struct

```go
type EmbeddingAutomationHandler struct {
    config        config.TimmyConfig
    vectorManager *VectorIndexManager
}
```

Injected with server config and vector manager during route setup.

### GetEmbeddingConfig

1. Extract `threat_model_id` from path
2. Verify threat model exists via store — 404 if not
3. Build `EmbeddingConfig` from `TimmyConfig` fields
4. Return 200

### IngestEmbeddings

1. Extract `threat_model_id`, verify threat model exists — 404 if not
2. Bind and validate request body — 400 if malformed
3. Validate `index_type` — 400 if invalid
4. Validate entity type / index type consistency — 422 if mismatched
5. Validate vector dimension consistency — 422 if inconsistent
6. Convert to `[]models.TimmyEmbedding`, set `ThreatModelID` and `IndexType`
7. `GlobalTimmyEmbeddingStore.CreateBatch()`
8. `vectorManager.InvalidateIndex(threatModelID, indexType)`
9. Return 201 with count

### DeleteEmbeddings

1. Extract `threat_model_id`, verify threat model exists — 404 if not
2. Read query params — 400 if no filters
3. Execute appropriate delete (routes to store methods based on which params are provided)
4. `InvalidateIndex` for affected index types
5. Return 200 with count

## Section 5: Embedding Store Additions

### Updated Interface

All delete methods return `(int64, error)` for row count reporting:

```go
type TimmyEmbeddingStore interface {
    ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
    CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
    DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error)
    DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error)
    DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error)
}
```

### New Method: DeleteByThreatModelAndIndexType

GORM implementation:
```go
func (s *GormTimmyEmbeddingStore) DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error) {
    result := s.db.WithContext(ctx).
        Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
        Delete(&models.TimmyEmbedding{})
    return result.RowsAffected, result.Error
}
```

Uses `map[string]any` for the `Where` clause — GORM translates this to parameterized queries on all backends (PostgreSQL, Oracle, SQLite). No raw SQL.

### Updated Delete Signatures

`DeleteByEntity` and `DeleteByThreatModel` change from `error` to `(int64, error)`. The existing `result.RowsAffected` is already computed in both implementations — just return it alongside the error. Update all callers to handle the new return value (use `_` where count is not needed).

## Section 6: Route Registration

The endpoints are defined in the OpenAPI spec and generated into the `ServerInterface`. Route registration follows the standard OpenAPI-driven pattern:

1. Endpoints added to `api-schema/tmi-openapi.json`
2. `make generate-api` produces new `ServerInterface` methods
3. Methods implemented on the `Server` struct, delegating to `EmbeddingAutomationHandler`
4. `AutomationMiddleware` applied to `/automation/*` path group
5. `EmbeddingAutomationMiddleware` applied to `/automation/embeddings/*` path group

Middleware is applied in the router setup (same pattern as `TimmyEnabledMiddleware` on `/chat/*`).

## Section 7: Testing

### Unit Tests

**Middleware tests** (`api/automation_middleware_test.go`):
- `AutomationMiddleware` allows `embedding-automation` members
- `AutomationMiddleware` allows `tmi-automation` members
- `AutomationMiddleware` rejects non-members
- `EmbeddingAutomationMiddleware` allows `embedding-automation` members
- `EmbeddingAutomationMiddleware` rejects `tmi-automation`-only members

**Handler tests** (`api/timmy_embedding_automation_handlers_test.go`):
- GetEmbeddingConfig: returns text config; returns text + code when configured; 404 for missing threat model
- IngestEmbeddings: valid batch returns 201; calls InvalidateIndex; rejects invalid index_type (400); rejects entity/index mismatch (422); rejects inconsistent dimensions (422); 404 for missing threat model
- DeleteEmbeddings: deletes by entity; deletes by index_type; 400 with no filters; 404 for missing threat model

**Embedding store tests** (update `api/timmy_embedding_store_test.go`):
- `DeleteByThreatModelAndIndexType` filters correctly
- Updated `DeleteByEntity` and `DeleteByThreatModel` return row counts

**Seed test:**
- Verify `embedding-automation` group is seeded correctly

## Files Changed

| File | Change Type |
|------|-------------|
| `api/validation/validators.go` | Modified: add `EmbeddingAutomationGroupUUID` |
| `api/auth_utils.go` | Modified: add `EmbeddingAutomationGroup` constant |
| `api/group_membership.go` | Modified: add `GroupEmbeddingAutomation` variable |
| `api/seed/seed.go` | Modified: add `seedEmbeddingAutomationGroup` |
| `api-schema/tmi-openapi.json` | Modified: add 3 endpoints + schemas |
| `api/api.go` | Regenerated: new `ServerInterface` methods |
| `api/automation_middleware.go` | New: `AutomationMiddleware` + `EmbeddingAutomationMiddleware` |
| `api/timmy_embedding_automation_handlers.go` | New: `EmbeddingAutomationHandler` with 3 methods |
| `api/timmy_embedding_store.go` | Modified: updated delete signatures + new method |
| `api/timmy_embedding_store_gorm.go` | Modified: updated delete implementations + new method |
| `api/server.go` | Modified: route registration for automation middleware |
| `cmd/server/main.go` | Modified: create and wire `EmbeddingAutomationHandler` |
| `api/automation_middleware_test.go` | New: middleware tests |
| `api/timmy_embedding_automation_handlers_test.go` | New: handler tests |
| `api/timmy_embedding_store_test.go` | Modified: new method tests + updated signatures |

## Relationship to Other Sub-Specs

- **Sub-spec 1** (Dual-Index Infrastructure): Provides `IndexTypeText`/`IndexTypeCode` constants, `InvalidateIndex` method, `ListByThreatModelAndIndexType`, and dual-index config. All used by this sub-spec.
- **Sub-spec 3** (Query Decomposition + Reranking): Independent of this sub-spec. Builds on sub-spec 1's dual-index search in `buildTier2Context`.

## Automation Workflow

After this sub-spec is implemented, the external embedding workflow is:

1. Admin creates a user account, adds it to the `embedding-automation` group, and provisions client credentials
2. Automation authenticates via client credentials grant (`POST /oauth2/token`)
3. Automation calls `GET /automation/embeddings/{threat_model_id}/config` to discover embedding model config
4. Automation subscribes to `repository.created` / `document.created` webhook events
5. On event: clone repo / fetch document, chunk, embed using the configured model
6. Push embeddings via `POST /automation/embeddings/{threat_model_id}`
7. TMI invalidates the in-memory index; next Timmy query picks up the new embeddings
