# N+1 Query Elimination — Performance Design

**Date:** 2026-03-18
**Issue:** #190 — perf: eliminate N+1 query patterns causing slow Heroku response times
**Status:** Approved

## Problem

TMI on Heroku is significantly slower than local development due to excessive database round-trips. Locally each query is <1ms over Unix socket; on Heroku each crosses a network boundary at ~2-5ms per round-trip. N+1 query patterns multiply this latency severely.

Key symptoms:
- Login providers list: several seconds (sub-second locally)
- Threat model sub-entity lazy-loading: 10-20 seconds
- Threat model edit page appears to have missing sub-entities until they eventually load

## Approach

Restructured 2-phase plan grouped by blast radius and testing surface. All changes maintain Oracle compatibility (batch `IN` queries instead of GORM Preload where needed).

## Phase 1 — Zero-API-Change Backend Optimizations

### 1.1 Lightweight List Query

**Current state:** `ListWithCounts` (`database_store_gorm.go:297`) calls `convertToAPIModel()` per item, triggering the full N+1 cascade (authorization, metadata, threats, diagrams) for every threat model. The handler then discards all sub-resources when mapping to `TMListItem`. Additionally, 6 individual `SELECT COUNT(*)` queries run per threat model.

**Changes:**

1. **New method `convertToListItem(tm *models.ThreatModel) TMListItem`** on `GormThreatModelStore` — maps only top-level fields (name, description, owner, created_by, security_reviewer, framework, timestamps, status). No calls to `loadAuthorization`, `loadMetadata`, `loadThreats`, or `loadDiagramsDynamically`.

2. **New method `batchCounts(threatModelIDs []string) map[string]entityCounts`** — runs 6 queries (one per table: documents, repositories, diagrams, threats, notes, assets), each using:
   ```sql
   SELECT threat_model_id, COUNT(*) FROM <table>
   WHERE threat_model_id IN (?) AND deleted_at IS NULL
   GROUP BY threat_model_id
   ```
   Returns a map from threat model ID to a struct with all 6 counts. Replaces `6 × N` individual COUNT queries with 6 total queries regardless of N.

3. **New method `batchLoadAuthorizationLightweight(ids []string) map[string]authorizationWithOwner`** — returns per-threat-model authorization data including the owner (`User`) and `[]Authorization` list. Loads `threat_model_access` entries via `WHERE threat_model_id IN (?)`, then batch-resolves users/groups via `WHERE internal_uuid IN (?)`. Owner data comes from the already-preloaded `Owner` on the `models.ThreatModel`. This is needed because the existing list filter calls `AccessCheckWithGroups` which requires both `Owner` and `Authorization` fields in `AuthorizationData`.

4. **Refactored authorization filter:** The current `filter func(ThreatModel) bool` signature is replaced within `ListWithCounts` — instead of passing a full `ThreatModel` to the filter, the method builds `AuthorizationData` from the lightweight auth data and owner, then calls `AccessCheckWithGroups` directly. The `ThreatModelStoreInterface.ListWithCounts` signature does not change; the filter callback is still accepted but the internal implementation avoids `convertToAPIModel()`.

5. **`ListWithCounts` refactored** to:
   - Query threat models with `Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer")` (existing behavior, unchanged)
   - Call `batchLoadAuthorizationLightweight()` for all loaded IDs
   - Apply authorization filter using lightweight auth + owner data
   - Call `convertToListItem()` per passing model (cheap — no DB calls)
   - Collect all passing IDs, call `batchCounts()` once
   - Merge counts into results

**Impact:** For 10 threat models, goes from ~200-300 queries to ~11 (1 main query + 3 preload queries + 1 auth batch with up to 2 user/group resolution queries + 6 batch count queries). Oracle `IN` clause limit of 1000 elements is not a concern here — this is bounded by the paginated result set (max 1000 items per `TMListItem.maxItems` in the OpenAPI spec), and `batchCounts` chunks IDs into groups of 999 if needed.

### 1.2 Lightweight Middleware Authorization

**Current state:** `ThreatModelMiddleware` (`middleware.go:328`) calls `ThreatModelStore.Get(id)` on every request to `/threat_models/{id}/*`. This loads the full model with all sub-resources just to check if the user has the required role. When the client opens a threat model and lazy-loads 6 sub-entity types in parallel, the middleware alone triggers 6 full model loads = 120-180 DB round-trips before any handler code runs.

**Changes:**

1. **New interface method `GetAuthorization(id string) ([]Authorization, error)`** on `ThreatModelStoreInterface` — loads only `threat_model_access` rows with batch user/group resolution. Validates the threat model exists (returns not-found if deleted/missing).

2. **New method `GetAuthorizationCached(id string) ([]Authorization, error)`** — checks Redis first using key `auth:{threat_model_id}` with `AuthCacheTTL` (15 minutes). On miss, calls `GetAuthorization`, caches result, returns it.

3. **Cache invalidation:** Any write to `threat_model_access` (in `saveAuthorizationTx`, or when a threat model is updated/deleted) invalidates the cache key `auth:{threat_model_id}`.

4. **New method `GetOwnerForThreatModel(id string) (User, error)`** — lightweight query to load only the threat model's owner (single Preload). Needed because `GetUserRole` requires both authorization entries and the owner to build `AuthorizationData`.

5. **`ThreatModelMiddleware` refactored** to:
   - Replace `ThreatModelStore.Get(id)` with `GetAuthorizationCached(id)` + `GetOwnerForThreatModel(id)` (owner can also be cached alongside auth data in the same Redis key to avoid a second query)
   - Build `AuthorizationData` from auth entries + owner, call `GetUserRole`-equivalent logic
   - **Context change:** Middleware currently sets `c.Set("threatModel", threatModel)` (`middleware.go:397`). After this change, the full `ThreatModel` is no longer set. The `getExistingThreatModel` helper (`threat_model_handlers.go:1149`) checks context first, then falls back to `ThreatModelStore.Get(id)`. After this change, it will always take the fallback path, adding one `Get()` call to the `PatchThreatModel` and `UpdateThreatModel` handlers. This is acceptable — these are write operations (infrequent) and the `Get()` call benefits from response caching (Section 2.3).

6. **Restore routes:** `GetAuthorizationIncludingDeleted(id)` added for the restore path.

**Impact:** Every sub-resource request goes from ~20-30 DB queries down to 0-2 queries (often cached to 0). For 6 parallel sub-resource loads: ~120-180 queries eliminated. Update/Patch handlers add one `Get()` call but this is offset by response caching.

### 1.3 Batch User/Group Lookup in `loadAuthorization`

**Current state:** `loadAuthorization` (`database_store_gorm.go:698-749`) issues a separate `WHERE internal_uuid = ?` query per access entry to resolve user or group identity.

**Changes:**

1. After loading access entries, collect all unique `UserInternalUUID` and `GroupInternalUUID` values.
2. Batch-fetch users: `SELECT * FROM users WHERE internal_uuid IN (?)` — build `map[string]models.User`.
3. Batch-fetch groups: `SELECT * FROM groups WHERE internal_uuid IN (?)` — build `map[string]models.Group`.
4. Look up from maps instead of individual queries.

**Result:** Goes from `1 + N` queries to exactly 3 queries regardless of access entry count.

## Phase 2 — N+1 Elimination in Get Path

### 2.1 Batch Threat Metadata Loading

**Current state:** `loadThreats` (`database_store_gorm.go:797`) calls `loadThreatMetadata(tm.ID)` per threat.

**Changes:**

1. After loading all threats, collect all threat IDs.
2. Single query: `SELECT * FROM metadata WHERE entity_type = 'threat' AND entity_id IN (?) ORDER BY key ASC`
3. Build `map[string][]Metadata` keyed by entity_id, assign to each threat.

**Result:** Goes from `1 + N` queries to 2 queries regardless of threat count.

### 2.2 Batch Diagram Loading

**Current state:** `loadDiagramsDynamically` (`database_store_gorm.go:849-887`) plucks diagram IDs, then calls `DiagramStore.Get(diagramID)` per diagram with its own preloads.

**Changes:**

1. Add `GetBatch(ids []string) ([]DfdDiagram, error)` to `DiagramStoreInterface` — loads all diagrams in a single query with preloads.
2. `loadDiagramsDynamically` calls `DiagramStore.GetBatch(diagramIDs)` instead of looping.
3. In-memory store implementation loops over `Get` (no performance concern for tests).

**Result:** Goes from `1 + N×M` queries to 2 queries.

### 2.3 Response Caching for GET

**Changes:**

1. Cache full `ThreatModel` API response (serialized JSON) in Redis after successful `Get(id)`, using key `tm:{id}` with `ThreatModelCacheTTL` (10 minutes).
2. On cache hit, return cached response — skip all DB queries.
3. Write-through invalidation uses two distinct operations:
   - `InvalidateThreatModelCache(id)` — deletes `tm:{id}` response cache key. Called from:
     - Threat model update/patch/delete/restore
     - Authorization modifications
     - Any sub-resource create/update/delete (handlers know parent ID from URL)
   - `InvalidateAuthCache(id)` — deletes `auth:{id}` auth cache key. Called only when authorization actually changes:
     - Threat model update/patch (if authorization field is modified)
     - Threat model delete/restore
     - NOT called on sub-resource writes (sub-resources don't affect authorization)
4. Cache failures are non-fatal — fall through to DB.

**Not cached:** List responses (user-specific auth filtering + varied query params make cache key management complex for limited benefit after Section 1.1 fixes).

## Expected Impact Summary

| Path | Before (queries) | After (queries) |
|------|-------------------|-----------------|
| GET /threat_models (10 items) | ~200-300 | ~10 |
| Middleware per sub-resource request | ~20-30 | 0-2 (cached) |
| GET /threat_models/{id} | ~20-30 | ~12 (cold) / 0 (cached) |
| 6 parallel sub-resource loads | ~120-180 | 0-12 |

Phases 1-2 should reduce typical page load times from 10-20s to 1-2s on Heroku.

## Files Modified

- `api/database_store_gorm.go` — Store implementation: new batch methods, refactored list/auth loading
- `api/store.go` — Interface: `GetAuthorization`, `GetAuthorizationCached`, `GetAuthorizationIncludingDeleted`, `GetOwnerForThreatModel` on threat model store; `GetBatch` on diagram store
- `api/middleware.go` — Lightweight auth-only middleware query
- `api/cache_service.go` — Auth caching, response caching, invalidation methods
- `api/threat_model_handlers.go` — Invalidation calls on writes
- Sub-resource handlers — Invalidation calls on sub-resource writes

## In-Memory Store Implementations

All new interface methods must also be implemented on the in-memory stores used by unit tests:

- `GetAuthorization(id)` — filter in-memory auth entries for the given ID
- `GetAuthorizationCached(id)` — delegate directly to `GetAuthorization` (no Redis in tests)
- `GetAuthorizationIncludingDeleted(id)` — same as `GetAuthorization` but without deleted_at filter (for restore path)
- `GetOwnerForThreatModel(id)` — return owner from in-memory threat model
- `GetBatch(ids)` on `DiagramStoreInterface` — loop over `Get` per ID
- `batchLoadAuthorizationLightweight` is an internal method on `GormThreatModelStore`, not on the interface — no in-memory implementation needed

## Constraints

- All batch queries use `WHERE ... IN (?)` for Oracle compatibility (no GORM Preload on cross-table joins)
- Oracle `IN` clause limit of 1000 elements: batch methods chunk IDs into groups of 999 when the input exceeds this threshold
- No OpenAPI spec changes required (`TMListItem` already exists as a lightweight list schema)
- Redis failures are non-fatal throughout
