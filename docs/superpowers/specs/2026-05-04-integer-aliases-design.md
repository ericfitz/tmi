# Integer Aliases for Threat Models and Sub-Objects — Design

**Issue:** [#374](https://github.com/ericfitz/tmi/issues/374)
**Date:** 2026-05-04
**Branch target:** `dev/1.4.0`

## Summary

Replace the user-supplied `alias: string[]` field on `ThreatModel` (shipped in #108) with a server-assigned monotonically-increasing **integer alias**, and extend the same alias concept to every TM sub-object (documents, assets, repositories, diagrams, notes, threats). Aliases are read-only, immutable, and unique within their scope (globally for `threat_model`; per `(threat_model_id, type)` for sub-objects).

This implementation lands the **data layer**: schema, allocation logic, migration, and API exposure as a read-only field. It explicitly **does not** add `{id}` path-parameter resolution that accepts integer aliases — that work is deferred to a separate follow-up issue once tmi-ux #305 confirms whether server-side resolution is needed (the client may resolve aliases locally via cached lookups).

## Goals

- Every threat-model and TM sub-object has a server-assigned integer `alias` available on every GET response.
- Aliases are monotonically increasing within their scope (global for ThreatModel, per `(TM, type)` for sub-objects). High-water mark — never reused after deletion.
- Counter allocation is concurrent-write-safe across multiple replicas via DB-level row locking.
- Backfill of existing rows runs at server startup, idempotent, single-runner via cross-DB advisory lock.
- The legacy `alias: string[]` user-defined nicknames feature is removed; the column shares the new column's name.
- Triple-layer protection against client write attempts: OpenAPI `readOnly`, handler whitelist, GORM `<-:create` tag.
- Cross-DB compatible (PostgreSQL dev, Oracle ADB prod).

## Non-goals

- **Path-parameter resolution accepting integer aliases.** Deferred to a follow-up issue. All `{id}` segments continue to require UUIDs in this implementation. Detailed in "Out of scope."
- **Client/data migration of legacy `alias` string-array values.** Per the issue, no known production consumers; the column is dropped, data is destroyed.
- **User-facing nickname or custom-alias fields.** If wanted later, separate issue.
- **Per-deployment alias formats / prefixes.** Server stores and returns plain integers. Display prefixes (`TM-`, `DFD-`, etc.) are a tmi-ux concern.
- **Alias retroactive assignment for soft-deleted rows that bypass `created_at` ordering.** Migration assigns aliases by `created_at ASC, id ASC` regardless of soft-delete state.

## Architecture

### Components

| Component | Location | Responsibility |
|---|---|---|
| `alias_counters` table | new in DB schema | Persistent next-alias counters, keyed by `(parent_id, object_type)` |
| `AliasCounter` GORM model | `api/models/models.go` (new struct) | GORM mapping for the counter table |
| `AllocateNext` helper | new file `api/alias_allocator.go` | Single function: locks the counter row, returns next alias |
| `Alias int` GORM field | added to `ThreatModel`, `Diagram`, `Threat`, `Asset`, `Repository`, `Note`, `Document` models | The alias column on each table |
| Repository Create methods | existing `*_store_gorm.go` files | Modified to call `AllocateNext` inside their existing transaction |
| Alias backfill | `api/alias_backfill.go` (new) | Server-startup migration; runs under cross-DB advisory lock |
| Advisory-lock primitive | `internal/dbschema/advisory_lock.go` (new) | Cross-DB wrapper for `pg_advisory_lock` (PG) / `DBMS_LOCK.REQUEST` (Oracle) |
| OpenAPI updates | `api-schema/tmi-openapi.json` | Replace `ThreatModelBase.alias` schema; add `alias` (readOnly integer) to sub-object schemas |
| Handler whitelists | existing `Update*Request` structs in handlers | `alias` is omitted (struct-level whitelist) |
| `getFieldErrorMessage` switches | `api/threat_model_handlers.go` and equivalents for sub-resources | Add `case "alias"` returning a descriptive 400 message |

### Data model — `alias_counters` table

| Column | Type (PG / Oracle) | Notes |
|---|---|---|
| `parent_id` | `varchar(36)` / `VARCHAR2(36)` | Either a TM UUID for sub-object counters, or the sentinel `'__global__'` for the ThreatModel global counter |
| `object_type` | `varchar(16)` / `VARCHAR2(16)` | One of `threat_model`, `diagram`, `threat`, `asset`, `repository`, `note`, `document` |
| `next_alias` | `integer` / `NUMBER(10)` | Next value to assign (1-based). Updated to `next_alias + 1` on each allocation |

Constraints:
- PK: `(parent_id, object_type)`.
- CHECK: `(parent_id != '__global__' OR object_type = 'threat_model')` — the global sentinel is reserved for the ThreatModel global counter.

Allocation sequence (inside an existing repository transaction):
```sql
-- Step 1: lock the counter row (or create it if missing).
INSERT INTO alias_counters (parent_id, object_type, next_alias)
  VALUES ($1, $2, 1)
  ON CONFLICT (parent_id, object_type) DO NOTHING;
SELECT next_alias FROM alias_counters
  WHERE parent_id = $1 AND object_type = $2
  FOR UPDATE;

-- Step 2: bump the counter (in the same transaction).
UPDATE alias_counters SET next_alias = next_alias + 1
  WHERE parent_id = $1 AND object_type = $2;

-- Step 3: caller uses the captured next_alias on its INSERT.
```

For Oracle the `ON CONFLICT` clause becomes a `MERGE` statement; `SELECT ... FOR UPDATE` is identical syntax.

The repository's `WithRetryableGormTransaction` wrapper provides automatic retry on serialization failure / deadlock — this covers the rare case where two replicas contend for the same row.

### Data model — `alias` column on existing tables

Each of `threat_models`, `diagrams`, `threats`, `assets`, `repositories`, `notes`, `documents` gains:

```go
Alias int32 `gorm:"column:alias;not null;default:0;<-:create" json:"alias"`
```

- `not null` + `default:0` so AutoMigrate adds the column to existing rows without violating constraints; the backfill replaces the 0s with real values.
- `<-:create` enforces sticky-on-creation at the GORM layer — UPDATE statements never include the column even if a developer passes one with `Alias` set.
- `int32` matches the SQL `INTEGER` type on both PG and Oracle (4-byte signed integer).

Indexes:
- `threat_models`: unique index on `alias`.
- Each sub-object table: unique index on `(threat_model_id, alias)`.

Note: the unique index is built only after the backfill completes (since backfill needs to run in `alias=0` state and would violate uniqueness during partial completion). This is implemented by deferring `AddUniqueIndex` to a post-backfill step, OR by relying on a non-unique index during AutoMigrate and adding the unique constraint after backfill via a second migration step. See "Migration" below.

### Allocation flow

`api/alias_allocator.go` (new file):

```go
package api

import (
    "context"
    "errors"
    "fmt"

    "github.com/ericfitz/tmi/api/models"
    "gorm.io/gorm"
    "gorm.io/gorm/clause"
)

// AllocateNextAlias atomically reserves the next alias value for the given
// (parent_id, object_type) scope. MUST be called inside a transaction; the
// caller's transaction owns the lock until commit.
//
// For the ThreatModel global counter, pass parent_id="__global__".
func AllocateNextAlias(ctx context.Context, tx *gorm.DB, parentID, objectType string) (int32, error) {
    // Insert the counter row if it doesn't exist yet, with next_alias = 1.
    // ON CONFLICT DO NOTHING is portable to PG; for Oracle the dialect
    // generates MERGE.
    if err := tx.WithContext(ctx).Clauses(
        clause.OnConflict{DoNothing: true},
    ).Create(&models.AliasCounter{
        ParentID: parentID, ObjectType: objectType, NextAlias: 1,
    }).Error; err != nil {
        return 0, fmt.Errorf("upsert alias_counters row: %w", err)
    }

    // Lock the row and read current value.
    var counter models.AliasCounter
    if err := tx.WithContext(ctx).
        Clauses(clause.Locking{Strength: "UPDATE"}).
        Where("parent_id = ? AND object_type = ?", parentID, objectType).
        First(&counter).Error; err != nil {
        return 0, fmt.Errorf("lock alias_counters row: %w", err)
    }

    allocated := counter.NextAlias

    // Bump the counter for the next caller.
    if err := tx.WithContext(ctx).
        Model(&models.AliasCounter{}).
        Where("parent_id = ? AND object_type = ?", parentID, objectType).
        Update("next_alias", counter.NextAlias+1).Error; err != nil {
        return 0, fmt.Errorf("bump alias_counters row: %w", err)
    }

    return allocated, nil
}
```

Repository Create methods (e.g., `GormThreatRepository.Create`, `GormNoteRepository.Create`, etc.) are modified to call `AllocateNextAlias` inside their existing transactional wrapper:

```go
return WithRetryableGormTransaction(ctx, s.db, func(tx *gorm.DB) error {
    alias, err := AllocateNextAlias(ctx, tx, threatModelID, "threat")
    if err != nil { return err }
    threat.Alias = alias
    return tx.Create(&threat).Error
})
```

For ThreatModel itself: the parent_id is `"__global__"` and object_type is `"threat_model"`.

### Migration

Migration runs at server startup as part of the existing GORM AutoMigrate flow. Three phases:

**Phase 1: schema additions and removal (always runs).**

In `auth/db/gorm.go` (or wherever AutoMigrate is invoked):

1. Drop the legacy `alias` column from `threat_models` if it exists. Use `db.Migrator().HasColumn(&ThreatModel{}, "alias")` + a check on the column's data type — if it's still `varchar/text` (legacy), drop it. If it's already `bigint` (new), skip.
   - Implementation: a small pre-AutoMigrate hook that checks via `information_schema.columns` (PG) or `USER_TAB_COLUMNS` (Oracle) and runs an explicit `ALTER TABLE threat_models DROP COLUMN alias` when needed.
2. AutoMigrate runs against the new model definitions, adding the `alias bigint NOT NULL DEFAULT 0` column to all 7 tables and creating the `alias_counters` table.
3. **Unique indexes are NOT added at this point** — they would fail because all rows have alias=0. They are added in Phase 3.

**Phase 2: backfill (runs once per table, idempotent).**

Implementation in `api/alias_backfill.go`:

```go
// RunAliasBackfill brings all aliased tables to a fully-populated state.
// Idempotent: skips tables whose rows all have alias > 0.
// Acquires a cross-DB advisory lock to serialize across replicas.
func RunAliasBackfill(ctx context.Context, db *gorm.DB) error {
    release, err := AcquireMigrationLock(ctx, db, "alias_backfill")
    if err != nil { return err }
    defer release()

    if err := backfillThreatModelAliases(ctx, db); err != nil { return err }
    for _, t := range []struct{ name, parentField string }{
        {"diagrams", "threat_model_id"},
        {"threats", "threat_model_id"},
        {"assets", "threat_model_id"},
        {"repositories", "threat_model_id"},
        {"notes", "threat_model_id"},
        {"documents", "threat_model_id"},
    } {
        if err := backfillSubObjectAliases(ctx, db, t.name, t.parentField); err != nil {
            return err
        }
    }
    return nil
}
```

Each helper:
1. Checks `SELECT 1 FROM <table> WHERE alias = 0 LIMIT 1`. If empty, returns immediately.
2. For ThreatModel: assigns aliases globally ordered by `created_at ASC, id ASC` using a window function (`ROW_NUMBER() OVER (ORDER BY created_at, id)`). Soft-deleted rows participate.
3. For sub-objects: assigns aliases per parent TM ordered by `created_at ASC, id ASC` using `ROW_NUMBER() OVER (PARTITION BY threat_model_id ORDER BY created_at, id)`. Soft-deleted rows participate.
4. After bulk update, reads `SELECT MAX(alias) ...` for each scope and writes `next_alias = MAX + 1` into `alias_counters`.

For PostgreSQL, the bulk update uses a CTE:
```sql
WITH numbered AS (
    SELECT id, ROW_NUMBER() OVER (
        PARTITION BY threat_model_id
        ORDER BY created_at ASC, id ASC
    ) AS row_num
    FROM threats
)
UPDATE threats t
SET alias = numbered.row_num
FROM numbered
WHERE t.id = numbered.id AND t.alias = 0;
```

For Oracle, the equivalent uses a MERGE with an analytic function — semantically identical, syntactically different. The implementation uses driver-specific code paths (`db.Dialector.Name()` switch) since this is a one-time bulk operation.

**Phase 3: unique-index addition (runs once after backfill).**

After Phase 2 completes successfully, add the unique constraints. Idempotent via `db.Migrator().HasIndex` checks. The seven indexes:

| Index name | Table | Columns |
|---|---|---|
| `uniq_threat_models_alias` | `threat_models` | `alias` |
| `uniq_diagrams_tm_alias` | `diagrams` | `(threat_model_id, alias)` |
| `uniq_threats_tm_alias` | `threats` | `(threat_model_id, alias)` |
| `uniq_assets_tm_alias` | `assets` | `(threat_model_id, alias)` |
| `uniq_repositories_tm_alias` | `repositories` | `(threat_model_id, alias)` |
| `uniq_notes_tm_alias` | `notes` | `(threat_model_id, alias)` |
| `uniq_documents_tm_alias` | `documents` | `(threat_model_id, alias)` |

All names are ≤ 30 bytes for Oracle 11c compatibility.

### Cross-DB advisory lock

`internal/dbschema/advisory_lock.go` (new):

```go
package dbschema

// AcquireMigrationLock takes an exclusive, server-wide named lock that's
// released by calling the returned function (or when the connection
// closes). On PG uses pg_try_advisory_lock; on Oracle uses DBMS_LOCK.REQUEST.
//
// The lock is held only during the migration; concurrent replicas waiting
// for the lock will see it released and proceed normally. A typical
// startup-time backfill takes seconds, so the wait is bounded.
func AcquireMigrationLock(ctx context.Context, db *gorm.DB, name string) (release func(), err error)
```

Implementation switches on `db.Dialector.Name()`:
- `postgres`: hash `name` to int64 (pg_advisory_lock takes a `bigint`), call `SELECT pg_advisory_lock($1)`. Release with `pg_advisory_unlock($1)`.
- `oracle`: call `DBMS_LOCK.REQUEST` with a named lock handle obtained from `DBMS_LOCK.ALLOCATE_UNIQUE`. Release with `DBMS_LOCK.RELEASE`.

Both calls block until the lock is acquired. The function takes a `context.Context` and respects cancellation.

### API schema changes

**`ThreatModelBase` in `api-schema/tmi-openapi.json`:**

Replace the existing `alias` property:

```diff
- "alias": {
-   "type": "array",
-   "description": "Alternative names or identifiers for the threat model",
-   "items": { ... },
-   "minItems": 1, "maxItems": 20, "uniqueItems": true
- },
+ "alias": {
+   "type": "integer",
+   "format": "int32",
+   "minimum": 1,
+   "readOnly": true,
+   "description": "Server-assigned monotonically-increasing identifier, globally unique across all threat models. Immutable after creation."
+ },
```

**Each sub-object schema** (`Document`, `Asset`, `Repository`, `DfdDiagram`, `Note`, `Threat`) gains the same field:

```yaml
"alias": {
  "type": "integer",
  "format": "int32",
  "minimum": 1,
  "readOnly": true,
  "description": "Server-assigned monotonically-increasing identifier, unique within (parent threat model, object type). Immutable after creation."
}
```

The field appears in all response shapes (single + list + nested). List-shape schemas (`NoteListItem`, etc.) gain the field too.

**`{id}` path parameters: unchanged.** They continue to require UUIDs. (See "Out of scope.")

### Handler-level write protection

Three layers stop a client from setting `alias` on PUT/PATCH:

1. **OpenAPI validator middleware** (already in place via `oapi-codegen-gin-middleware`): rejects bodies with `alias` set on input shapes since the field is `readOnly: true` in the schema.
2. **`UpdateXxxRequest` struct whitelist** (already a TMI pattern in `threat_model_handlers.go` and equivalents): the alias field is omitted from these whitelist structs, so even if the request body had it, the handler won't bind it.
3. **`getFieldErrorMessage`** (new case): explicit 400 with descriptive message if a JSON Patch operation targets `/alias`. Pattern:
   ```go
   case "alias":
       return "The alias is read-only and assigned by the server."
   ```
4. **GORM `<-:create` tag** (mechanical safety net): even if a developer accidentally sends an alias to the repository's Update method, GORM will exclude the column from UPDATE statements.

The error message style matches the existing one for `id`, `created_at`, `created_by`.

## Data flow

### Creating a new threat model
1. POST `/threat_models` with body.
2. Handler validates, builds the `ThreatModel` struct.
3. Repository's `Create` opens a transaction.
4. Inside transaction: `AllocateNextAlias(ctx, tx, "__global__", "threat_model")` returns the next int32.
5. `tm.Alias = allocated`.
6. `tx.Create(&tm)` inserts the row (alias column now has a real value).
7. Commit.
8. Handler returns the `ThreatModel` with `alias` populated in the JSON response.

### Creating a new sub-object (e.g., Note)
1. POST `/threat_models/{tm_id}/notes` with body.
2. Handler validates, builds the `Note` struct.
3. Repository's `Create` opens a transaction.
4. Inside transaction: `AllocateNextAlias(ctx, tx, tm_id, "note")` returns the next int32.
5. `note.Alias = allocated`.
6. `tx.Create(&note)`.
7. Commit.

### Reading
- `alias` is a normal column read on every GET. It's included in every response shape.

### Server startup migration
1. AutoMigrate adds columns and the `alias_counters` table (Phase 1).
2. `AcquireMigrationLock(ctx, db, "alias_backfill")` blocks until exclusive lock is held.
3. Backfill code runs for each aliased table, skipping tables with no `alias=0` rows (Phase 2).
4. Unique indexes are added (Phase 3).
5. Lock released.
6. Subsequent server starts: AutoMigrate is a no-op for the columns; lock acquired, all tables skipped, lock released within milliseconds.

## Error handling

| Failure | Response |
|---|---|
| Body includes `alias` on POST | OpenAPI validator → 400 with field error |
| Body includes `alias` on PUT | Whitelist struct rejects → 400 with `getFieldErrorMessage` |
| JSON Patch targets `/alias` | `getFieldErrorMessage` → 400 with explicit message |
| Counter row contention (deadlock) | `WithRetryableGormTransaction` retries automatically |
| Backfill fails mid-run | Lock released by deferred release; next startup retries (idempotent) |
| Advisory lock acquisition fails (e.g., DB restart) | Logged at Error; startup aborts so an operator notices |
| Two replicas race on `alias_counters` row insertion (Phase 0 of allocator) | `ON CONFLICT DO NOTHING` is idempotent; second SELECT FOR UPDATE returns the row written by the first |

## Testing

### Unit tests

**`api/alias_allocator_test.go`** (new):
- `TestAllocateNextAlias_FirstCall` — creates the counter row, returns 1.
- `TestAllocateNextAlias_SequentialCalls` — returns 1, 2, 3, 4 sequentially.
- `TestAllocateNextAlias_DifferentScopes` — `(tmA, "note")` and `(tmB, "note")` are independent.
- `TestAllocateNextAlias_GlobalThreatModel` — `__global__` + `threat_model` allocates 1, 2, 3.
- `TestAllocateNextAlias_NoReuseAfterRollback` — if the caller's outer transaction rolls back, the alias is "lost" (high-water mark preserved). Use a fake gorm.DB that simulates rollback.

**`api/alias_backfill_test.go`** (new):
- `TestBackfillThreatModels_AssignsByCreatedAtThenID` — seeds 5 TMs with controlled `created_at` and `id` values; verifies aliases are assigned 1..5 in the right order.
- `TestBackfillSubObjects_PerScope` — seeds 2 TMs, each with 3 notes; verifies aliases are 1,2,3 within each TM (not 1..6).
- `TestBackfillSoftDeletedRowsParticipate` — seeds rows with `deleted_at` set; verifies they get aliases.
- `TestBackfillIdempotent` — runs twice; second run is a no-op (no rows have alias=0 after the first run).
- `TestBackfillSetsCounterTable` — after backfill, alias_counters has the correct `next_alias` values for each scope.

**Existing repository tests** (`api/threat_model_repository_test.go`, `api/note_store_gorm_test.go`, etc.):
- Add a happy-path "creating an entity allocates an alias" assertion to the existing Create tests.
- Add a "concurrent create from two goroutines yields distinct aliases" test for at least one repository (probably ThreatModel or Note) — using a real DB connection with two goroutines.

**Handler tests:**
- `TestPutThreatModel_RejectsAliasInBody` — PUT with `{"alias": 99, "name": "..."}` → 400.
- `TestPatchThreatModel_RejectsAliasOperation` — JSON Patch `[{"op": "replace", "path": "/alias", "value": 99}]` → 400.
- Equivalent tests for at least one sub-resource (e.g., Note).

### Integration tests

`test/integration/workflows/alias_test.go` (new):
- `TestAliasAssignedOnCreate` — POST a TM, GET it, verify `alias` is present and integer.
- `TestAliasMonotonicAcrossSubObjects` — create 3 notes in a TM, verify aliases 1,2,3.
- `TestAliasIsolatedPerThreatModel` — create 2 TMs each with 1 note, verify both notes have alias=1.
- `TestAliasIsImmutableViaPut` — PUT body with alias → 400.
- `TestAliasIsImmutableViaPatch` — JSON Patch on `/alias` → 400.
- `TestAliasIncreasesAfterDelete` — create 3 notes, delete #2, create another note → new note has alias=4 (no reuse).

### Schema validation

- `make validate-openapi` — confirms `readOnly: true` on the new fields.
- `internal/dbschema/schema_test.go` — gains entries for `alias_counters` and the new `alias` column on the 7 entity tables.

## Out of scope (deferred)

A separate follow-up issue will be filed during the implementation phase covering:

- **Path-parameter resolution.** Adding a resolver middleware so `{id}` segments accept either a UUID or an integer alias. The OpenAPI parameter schemas would change from `format: uuid` to `oneOf: [{format: uuid}, {type: integer}]` (or a string with a regex pattern). This requires a route-shape map (which path-segment-name maps to which entity table) similar to the existing `x-tmi-authz` table.
- **The corresponding integration tests.** Mixed UUID/alias paths, malformed `{id}` returning 400, alias-not-found returning 404, etc.

The follow-up will land when tmi-ux #305 confirms whether server-side resolution is genuinely needed (the alternative — client-side resolution from a cached lookup table — may cover the use case).

## Files touched

| File | Status |
|---|---|
| `api-schema/tmi-openapi.json` | modify — replace `ThreatModelBase.alias`; add `alias` to 6 sub-object schemas; add `alias` to all list-item schemas |
| `api/api.go` | regenerate — output of `make generate-api` |
| `api/models/models.go` | modify — replace `Alias StringArray` with `Alias int32` on ThreatModel; add `Alias int32` to Diagram/Threat/Asset/Repository/Note/Document; add new `AliasCounter` struct; register `AliasCounter` in `AllModels()` |
| `api/alias_allocator.go` | new — `AllocateNextAlias` helper |
| `api/alias_allocator_test.go` | new — unit tests |
| `api/alias_backfill.go` | new — Phase 2 backfill logic |
| `api/alias_backfill_test.go` | new — backfill unit tests |
| `internal/dbschema/advisory_lock.go` | new — cross-DB advisory lock primitive |
| `internal/dbschema/schema.go` | modify — add `alias_counters` expected table; add `alias` column entries to existing 7 tables |
| `internal/dbschema/schema_test.go` | modify — count assertions |
| `auth/db/gorm.go` (or wherever AutoMigrate runs) | modify — add the legacy `alias` column drop step before AutoMigrate; add the post-AutoMigrate backfill call; add the post-backfill unique-index step |
| `api/threat_model_store_gorm.go` (or equivalent) | modify — call `AllocateNextAlias("__global__", "threat_model")` in Create |
| `api/note_store_gorm.go` | modify — call `AllocateNextAlias(tmID, "note")` in Create |
| `api/threat_store_gorm.go` | modify — call `AllocateNextAlias(tmID, "threat")` in Create |
| `api/asset_store_gorm.go` | modify |
| `api/repository_store_gorm.go` (or whichever owns Repository CRUD) | modify |
| `api/document_store_gorm.go` | modify |
| `api/database_store_gorm.go` (or wherever Diagram CRUD lives) | modify |
| `api/threat_model_handlers.go` | modify — add `case "alias"` to `getFieldErrorMessage`; remove `Alias` from `UpdateThreatModelRequest` (if present) |
| Sub-resource handlers (note/threat/diagram/asset/repository/document) | modify — add `case "alias"` to their respective field-error messages |
| `test/integration/workflows/alias_test.go` | new — end-to-end integration tests |

## Implementation sequence (preview for the plan)

1. Add `AliasCounter` GORM model + register in `AllModels()`. Add `Alias int32` field to all 7 entity models with `<-:create` tag. (Schema-only, with `default:0`.)
2. Update `internal/dbschema/schema.go` with the new table + new columns.
3. Implement `AllocateNextAlias` + unit tests.
4. Implement `AcquireMigrationLock` for both PG and Oracle.
5. Implement `RunAliasBackfill` + unit tests.
6. Wire into AutoMigrate flow: drop legacy column, run AutoMigrate, run backfill, add unique indexes.
7. Modify each repository's Create method to call `AllocateNextAlias`.
8. Update OpenAPI spec: replace `ThreatModelBase.alias`, add `alias` to sub-object schemas, regenerate.
9. Update handler whitelists + `getFieldErrorMessage`.
10. Integration tests.
11. Oracle-db-admin subagent review (mandatory — schema changes, advisory lock, dialect-specific bulk update).
12. Final gates: lint, build, unit tests, integration tests.
13. File the follow-up issue for path-param resolution.

## Oracle compatibility notes

This change is heavily DB-touching. Key Oracle-specific concerns the subagent will weigh in on:

- **`pg_advisory_lock` vs. `DBMS_LOCK.REQUEST`** semantics — release timing, naming, scope.
- **`ON CONFLICT DO NOTHING` translation** — Oracle uses MERGE; the GORM dialect handles this for INSERT but the bulk-update CTE in backfill needs explicit dialect branching.
- **`ROW_NUMBER() OVER ...` in UPDATE** — both DBs support this but Oracle requires MERGE syntax for the UPDATE-from-CTE pattern.
- **`bigint` → `NUMBER(19)` mapping** for the alias column — GORM Oracle dialect should handle this; verify.
- **Bulk DML lock escalation on Oracle** during backfill — for very large tables (10K+ rows per TM) the row-locking escalates to table locks. Acceptable for a one-time startup operation; not a concern for normal allocation flow.
- **Index naming conflicts** — the new unique index names (e.g., `idx_threats_tm_alias`) must be ≤ 30 bytes for Oracle 11c or ≤ 128 bytes for newer versions. Verify against the project's target Oracle version.
- **Column drop on Oracle** — `ALTER TABLE threat_models DROP COLUMN alias` is supported but Oracle requires `DROP COLUMN` to be in its own statement; can't combine with other operations.

The subagent's verdict is required before merge.
