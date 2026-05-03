# Usability + Content Feedback APIs — Design

**Issues:** [#362](https://github.com/ericfitz/tmi/issues/362), [#361](https://github.com/ericfitz/tmi/issues/361)
**Date:** 2026-05-03
**Branch target:** `dev/1.4.0`

## Summary

Two parallel feedback APIs that unblock tmi-ux #657 and #658:

1. **Usability feedback** (`#362`) — top-level, lightweight thumbs-up/down on UI surfaces, with rich client-identification metadata. Accessible to any authenticated user; admin-only for read.
2. **Content feedback** (`#361`) — threat-model-scoped feedback on AI/automation-generated artifacts (notes, diagrams, threats, threat classifications). Includes a structured false-positive taxonomy. Reader+ on the parent threat model can submit and read.

Plus a small bookkeeping change: `notes`, `diagrams`, and `threats` get an `auto_generated bool` column, set by handlers when the request actor is a service account (sticky on creation).

Both feedback resources are immutable after submission — POST + GET only, no PATCH/PUT/DELETE.

## Goals

- Get tmi-ux #657 unblocked: usable `POST /usability_feedback` with surface tagging and client metadata.
- Get tmi-ux #658 unblocked: usable `POST /threat_models/{id}/feedback` with target-typed false-positive taxonomy.
- Capture which artifacts were AI-generated so the content-feedback UI knows when to prompt.
- Maintain TMI's `x-tmi-authz` discipline — every new operation declares its role gate.
- Keep schema portable across PostgreSQL and Oracle ADB.

## Non-goals

- Screenshot/attachment uploads on feedback rows. Deferred to a follow-up if tmi-ux needs them.
- Editing or deleting feedback. Submitter cannot retract; admins cannot redact via API. (DB tooling remains available for genuinely abusive content.)
- Admin override on per-TM GET endpoints. Admins who want per-TM feedback need reader access to the TM. Cross-TM analytics use DB tooling.
- Backfilling `auto_generated` for existing rows. Defaults to `false` for everything pre-feature; new rows get the bit from the request actor.
- Server-side enforcement of "verbatim required when false_positive_reason=X." That validation lives in tmi-ux.

## Architecture

### Components

| Component | Location | Purpose |
|---|---|---|
| `UsabilityFeedback` schema | `api-schema/tmi-openapi.json` | API object shape |
| `ContentFeedback` schema | `api-schema/tmi-openapi.json` | API object shape |
| Endpoints (6 total) | `api-schema/tmi-openapi.json` | OpenAPI operations + `x-tmi-authz` |
| `usability_feedback` table | DB schema | persistent store for usability feedback |
| `content_feedback` table | DB schema | persistent store for content feedback |
| `notes.auto_generated`, `diagrams.auto_generated`, `threats.auto_generated` | DB schema | sticky AI-origin marker columns |
| `UsabilityFeedbackRepository` interface + `GormUsabilityFeedbackRepository` | `api/usability_feedback_repository.go`, `api/usability_feedback_store_gorm.go` | DAL |
| `ContentFeedbackRepository` interface + `GormContentFeedbackRepository` | `api/content_feedback_repository.go`, `api/content_feedback_store_gorm.go` | DAL |
| `UsabilityFeedbackHandlers` | `api/usability_feedback_handlers.go` | Gin handlers for the two `/usability_feedback*` routes |
| `ContentFeedbackHandlers` | `api/content_feedback_handlers.go` | Gin handlers for the three `/threat_models/{id}/feedback*` routes |
| `auto_generated` field-setting in existing handlers | `api/threat_handlers.go`, `api/note_handlers.go` (or sub-resource equivalents), `api/diagram_handlers.go` | reads `c.Get("isServiceAccount")` and sets the column on create |

### Data model — `usability_feedback`

| Column | Type (PG / Oracle) | Required | Validation |
|---|---|---|---|
| `id` | `uuid` / `RAW(16)` | yes (server-assigned) | — |
| `sentiment` | `text` / `VARCHAR2(8)` | yes | enum `up`, `down` (CHECK constraint) |
| `verbatim` | `text` / `CLOB` | no | ≤ 2 KB enforced at the handler layer |
| `surface` | `text` / `VARCHAR2(32)` | yes | `^[a-z][a-z0-9_.-]{0,31}$` |
| `client_id` | `text` / `VARCHAR2(32)` | yes | `^[a-z][a-z0-9_-]{0,31}$` |
| `client_version` | `text` / `VARCHAR2(32)` | no | ≤ 32 chars |
| `client_build` | `text` / `VARCHAR2(12)` | no | hex 7-12 chars |
| `user_agent` | `text` / `VARCHAR2(512)` | no | ≤ 512 chars |
| `user_agent_data` | `jsonb` / `CLOB` (JSON-typed where supported) | no | valid JSON object, ≤ 4 KB |
| `viewport` | `text` / `VARCHAR2(11)` | no | `^\d{1,5}x\d{1,5}$` |
| `created_by` | `uuid` / `RAW(16)` | yes (server-assigned, FK to `users.internal_uuid`) | — |
| `created_at` | `timestamptz` / `TIMESTAMP WITH TIME ZONE` | yes (server-assigned, default now) | — |

Indexes:
- PK on `id`
- non-unique on `created_by` (admin queries by submitter)
- non-unique on `surface` (analytics aggregation)
- non-unique on `created_at DESC` (recent feedback queries)

### Data model — `content_feedback`

| Column | Type (PG / Oracle) | Required | Validation |
|---|---|---|---|
| `id` | `uuid` / `RAW(16)` | yes (server-assigned) | — |
| `threat_model_id` | `uuid` / `RAW(16)` | yes | FK to `threat_models.id`, ON DELETE CASCADE |
| `target_type` | `text` / `VARCHAR2(24)` | yes | enum `note`, `diagram`, `threat`, `threat_classification` (CHECK constraint) |
| `target_id` | `uuid` / `RAW(16)` | yes | id of the targeted entity (no FK — handler validates) |
| `target_field` | `text` / `VARCHAR2(64)` | conditional | required iff `target_type='threat_classification'`, NULL otherwise |
| `sentiment` | `text` / `VARCHAR2(8)` | yes | enum `up`, `down` (CHECK constraint) |
| `verbatim` | `text` / `CLOB` | no | ≤ 2 KB |
| `false_positive_reason` | `text` / `VARCHAR2(32)` | conditional | enum (8 values, see below); allowed only when `sentiment='down' AND target_type='threat'`, NULL otherwise |
| `false_positive_subreason` | `text` / `VARCHAR2(40)` | conditional | enum (depends on reason); allowed only for reasons that have subreasons, NULL otherwise |
| `client_id` | `text` / `VARCHAR2(32)` | yes | `^[a-z][a-z0-9_-]{0,31}$` |
| `client_version` | `text` / `VARCHAR2(32)` | no | ≤ 32 chars |
| `created_by` | `uuid` / `RAW(16)` | yes (server-assigned, FK to `users.internal_uuid`) | — |
| `created_at` | `timestamptz` / `TIMESTAMP WITH TIME ZONE` | yes (server-assigned, default now) | — |

`false_positive_reason` enum:
- `detection_misfired` — has subreasons
- `real_but_mitigated` — no subreason
- `real_but_not_exploitable` — no subreason
- `out_of_scope` — has subreasons
- `intended_behavior` — has subreasons
- `duplicate` — no subreason
- `already_remediated` — no subreason
- `detection_rule_flawed` — has subreasons

`false_positive_subreason` enum (must match reason):
- `detection_misfired`: `code_does_not_exist`, `trigger_conditions_not_met`
- `out_of_scope`: `component_outside_threat_model`
- `intended_behavior`: `sanctioned_by_design`
- `detection_rule_flawed`: `not_a_real_risk`, `needs_tuning`

Indexes:
- PK on `id`
- FK on `threat_model_id` with `ON DELETE CASCADE`
- non-unique on `(threat_model_id, target_type, target_id)` for "show me feedback on this entity" queries
- non-unique on `(threat_model_id, false_positive_reason)` for false-positive analytics
- non-unique on `created_at DESC`

### Data model — `auto_generated` columns

Three existing GORM models get one new field each. Schema migration is performed by GORM AutoMigrate (TMI's standard mechanism), not by handwritten SQL files.

The new field on each model:
```go
AutoGenerated bool `gorm:"column:auto_generated;not null;default:false;<-:create" json:"auto_generated"`
```

The `gorm:"<-:create"` tag ensures the column is settable only on INSERT — UPDATE statements via `db.Save` / `db.Updates` will not change it, which mechanically enforces the sticky-on-creation semantics.

GORM AutoMigrate will materialize the column as:
- PostgreSQL: `auto_generated BOOLEAN NOT NULL DEFAULT false`
- Oracle: `auto_generated NUMBER(1) NOT NULL DEFAULT 0 CHECK (auto_generated IN (0,1))` (the GORM Oracle dialect handles the bool→NUMBER(1) translation automatically)

The corresponding `internal/dbschema/schema.go` `TableSchema` entries for `notes`, `diagrams`, `threats` gain the new column descriptor so `make test-unit` schema validation stays accurate.

The OpenAPI schema for each entity gets a new `auto_generated: boolean` (read-only) field. On POST/PUT/PATCH the value is **set by the handler from `c.Get("isServiceAccount")`** — a client-provided value is ignored. This is sticky: the column is set at creation and never updated by subsequent edits.

### API surface

#### `POST /usability_feedback`
- **Authz:** `{"ownership": "none"}` — any authenticated user.
- **Request body:** `UsabilityFeedbackInput` (all the writable columns).
- **Response 201:** the created `UsabilityFeedback` with server-assigned id, created_at, created_by.
- **Errors:** 400 (validation), 401 (no/invalid token), 413 (oversized verbatim or user_agent_data).

#### `GET /usability_feedback`
- **Authz:** `{"ownership": "none", "roles": ["admin"]}`.
- **Query params:** `limit` (default 50, max 1000 — `LimitQueryParam`), `offset` (default 0 — `OffsetQueryParam`), `sentiment`, `client_id`, `surface`, `created_after` (ISO8601), `created_before` (ISO8601). All filters optional.
- **Response 200:** `{ "items": [UsabilityFeedback...], "total": int }`.

#### `GET /usability_feedback/{id}`
- **Authz:** `{"ownership": "none", "roles": ["admin"]}`.
- **Response 200:** `UsabilityFeedback`. **404** if not found.

#### `POST /threat_models/{threat_model_id}/feedback`
- **Authz:** `{"ownership": "reader"}` — reader+ on the parent TM.
- **Request body:** `ContentFeedbackInput`.
- **Response 201:** the created `ContentFeedback`.
- **Errors:** 400 (validation, including target-id existence check), 401, 403, 404 (TM not found), 413.

#### `GET /threat_models/{threat_model_id}/feedback`
- **Authz:** `{"ownership": "reader"}`.
- **Query params:** `limit` (default 20, max 100 — `PaginationLimit`), `offset`, `target_type`, `target_id`, `sentiment`, `false_positive_reason`. All filters optional.
- **Response 200:** `{ "items": [ContentFeedback...], "total": int }`.

#### `GET /threat_models/{threat_model_id}/feedback/{feedback_id}`
- **Authz:** `{"ownership": "reader"}`.
- **Response 200:** `ContentFeedback`. **404** if not found or wrong TM.

### Validation rules summary

**UsabilityFeedback (POST):**
- `sentiment` in (`up`, `down`).
- `surface` matches `^[a-z][a-z0-9_.-]{0,31}$`.
- `client_id` matches `^[a-z][a-z0-9_-]{0,31}$`.
- `client_version` ≤ 32 chars (when present).
- `client_build` matches `^[0-9a-f]{7,12}$` (when present).
- `user_agent` ≤ 512 chars (when present).
- `user_agent_data` is a JSON object with serialized size ≤ 4 KB (when present).
- `viewport` matches `^\d{1,5}x\d{1,5}$` (when present).
- `verbatim` ≤ 2 KB (when present).

**ContentFeedback (POST):**
- `sentiment` in (`up`, `down`).
- `target_type` in (`note`, `diagram`, `threat`, `threat_classification`).
- `target_id` is a valid UUID.
- `target_field`: required and ≤ 64 chars when `target_type='threat_classification'`; **must be NULL/absent** otherwise.
- `false_positive_reason`: allowed only when `target_type='threat' AND sentiment='down'`. Must be NULL/absent otherwise.
- `false_positive_subreason`: allowed only when `false_positive_reason` is a reason that has subreasons (4 of 8). Must be a valid subreason for the chosen reason. NULL/absent otherwise.
- `client_id` matches `^[a-z][a-z0-9_-]{0,31}$`.
- `client_version` ≤ 32 chars (when present).
- `verbatim` ≤ 2 KB (when present).
- The targeted entity must exist within the parent threat model (handler issues a SELECT on the target table to validate). If not found → 400.

### `x-tmi-authz` summary

| Operation | Rule |
|---|---|
| `POST /usability_feedback` | `{"ownership": "none"}` |
| `GET /usability_feedback` | `{"ownership": "none", "roles": ["admin"]}` |
| `GET /usability_feedback/{id}` | `{"ownership": "none", "roles": ["admin"]}` |
| `POST /threat_models/{threat_model_id}/feedback` | `{"ownership": "reader"}` |
| `GET /threat_models/{threat_model_id}/feedback` | `{"ownership": "reader"}` |
| `GET /threat_models/{threat_model_id}/feedback/{feedback_id}` | `{"ownership": "reader"}` |

`make validate-openapi` enforces that every new operation has an `x-tmi-authz` annotation; the validator will reject the schema otherwise.

## Data flow

### Submitting usability feedback
1. Client sends `POST /usability_feedback` with body and JWT.
2. JWT middleware validates token; sets `userInternalUUID`, `userEmail`, `isServiceAccount`.
3. AuthzMiddleware sees `ownership: none`, no roles → passes.
4. Handler validates body fields per the rules above.
5. Handler builds the row with `id = uuid.New()`, `created_by = userInternalUUID`, `created_at = now()`.
6. Handler calls `usabilityFeedbackRepo.Create(ctx, row)`.
7. Repository INSERTs the row.
8. Handler returns 201 with the created object.

### Submitting content feedback
1. Client sends `POST /threat_models/{tmid}/feedback` with body and JWT.
2. JWT middleware validates.
3. AuthzMiddleware sees `ownership: reader` → calls `enforceOwnership(c, reader)`. The middleware extracts `tmid`, looks up the parent TM ACL, and confirms the user has reader+ role. On reject → 403/404.
4. Handler validates body fields.
5. Handler verifies the target exists by selecting from the table that matches `target_type` (`notes`, `diagrams`, or `threats`) by `target_id` AND `threat_model_id = tmid`. For `target_type=threat_classification`, the lookup hits the `threats` table (the classification is a field on the threat, not a separate entity). If not found → 400.
6. Handler builds the row.
7. `contentFeedbackRepo.Create(ctx, row)` INSERTs.
8. Returns 201.

### Reading feedback (admin or per-TM reader)
1. Standard JWT + Authz flow.
2. Handler parses filters and pagination params.
3. Repository builds a filtered SELECT with the appropriate WHERE clauses and `LIMIT/OFFSET`.
4. Handler returns `{items, total}`.

### Auto-marking AI-generated content
1. Service account submits `POST /threat_models/{id}/threats` (or notes/diagrams).
2. JWT middleware sets `isServiceAccount = true`.
3. The existing handler reads `c.Get("isServiceAccount")` and sets `threat.AutoGenerated = true` on the model before calling the repository.
4. Subsequent edits to the same row by humans do NOT change the `auto_generated` column (sticky-on-create). The handler explicitly omits the column from update statements (or, equivalently, the GORM model uses `gorm:"<-:create"` to prevent updates).

## Error handling

| Failure | Response |
|---|---|
| Missing/invalid JWT | 401 with `WWW-Authenticate` |
| Authz role/ownership reject | 403 (or 404 if existence-leak risk per current TMI convention) |
| Body validation error (regex, enum, conditional rule) | 400 with field-specific message |
| `verbatim`, `user_agent_data`, etc. exceeds size cap | 413 (Payload Too Large) — alternative: 400; pick 400 for simplicity if the framework's 413 path is awkward |
| `target_id` doesn't exist in the parent TM | 400 with `target_id not found` |
| GET by id, not found | 404 |
| GORM/DB transient error | 503 with retry-after; logged as Warn |
| GORM/DB permanent error (constraint violation) | 500; logged as Error |

## Testing

### Unit tests
- `api/usability_feedback_handlers_test.go` — happy path, every validation rule, body too large, missing JWT, list filters, pagination.
- `api/content_feedback_handlers_test.go` — happy path for each `target_type`, conditional-field rules, false-positive reason↔subreason validity matrix, target-id-not-in-TM rejection, list filters.
- `api/usability_feedback_store_gorm_test.go` — repository CRUD against the test DB.
- `api/content_feedback_store_gorm_test.go` — repository CRUD; FK cascade on TM delete.
- Existing `*_handlers_test.go` for threats/notes/diagrams gain test cases that confirm `auto_generated` is set to `true` when called with a service-account JWT and `false` otherwise; sticky on update.

### Integration tests
- `test/integration/usability_feedback_test.go` — end-to-end POST + GET flows; admin-only enforcement on GET.
- `test/integration/content_feedback_test.go` — end-to-end with TM ACL; reader/writer/owner all can submit; non-member rejected; cross-TM access rejected; cascade on TM delete.

### Schema validation
- `make validate-openapi` confirms `x-tmi-authz` is present on all 6 new operations.
- `make lint` runs `check-unsafe-union-methods`, `check-direct-http-client`, etc.; new code must be clean.
- New `internal/dbschema/schema.go` entries for `usability_feedback`, `content_feedback`, plus the `auto_generated` column additions on `notes`, `diagrams`, `threats`. `make test-unit` runs schema validation against the live DB.

## Out of scope (explicit, with rationale)

- Screenshot / attachment uploads (deferred per #362; introduces object-storage subsystem).
- DELETE/PATCH on feedback rows (immutability is intentional per analytics use case).
- Admin global override on per-TM GET endpoints (deferred; would require `x-tmi-authz` schema extension).
- Backfilling `auto_generated` on existing rows (per #361 §1, the bit is meaningful for new content going forward; pre-feature content is treated as human-authored, which is the truth in 99% of cases).
- Server-side enforcement of "verbatim required when false_positive_reason=X" (UX rule, lives in tmi-ux).
- WebSocket or real-time broadcast of new feedback rows.
- Aggregate / analytics endpoints (e.g., "thumbs-up rate by surface over the last 30 days"). The data is in the DB; admins can query directly. If a follow-up issue requests it, design then.

## Files touched

| File | Status |
|---|---|
| `api-schema/tmi-openapi.json` | modify — add 2 schemas, 6 operations, 3 `auto_generated` field additions |
| `api/models/usability_feedback.go` | new — GORM model for `usability_feedback` table |
| `api/models/content_feedback.go` | new — GORM model for `content_feedback` table |
| `api/models/models.go` | modify — register the two new models in `AllModels()` |
| `api/models/note.go`, `api/models/diagram.go`, `api/models/threat.go` | modify — add `AutoGenerated bool` field with `gorm:"<-:create"` to prevent updates |
| `internal/dbschema/schema.go` | modify — add 2 expected-table entries; add `auto_generated` column to existing notes/diagrams/threats entries |
| `api/usability_feedback_repository.go` | new — interface |
| `api/usability_feedback_store_gorm.go` | new — implementation |
| `api/usability_feedback_handlers.go` | new — 3 handlers (POST, GET list, GET single) |
| `api/usability_feedback_handlers_test.go` | new |
| `api/usability_feedback_store_gorm_test.go` | new |
| `api/content_feedback_repository.go` | new — interface |
| `api/content_feedback_store_gorm.go` | new — implementation |
| `api/content_feedback_handlers.go` | new — 3 handlers |
| `api/content_feedback_handlers_test.go` | new |
| `api/content_feedback_store_gorm_test.go` | new |
| `api/api.go` | regenerated by `make generate-api` |
| `api/threat_handlers.go` (or sub-resource handlers for threats) | modify — set `AutoGenerated` on create |
| `api/note_handlers.go` (or sub-resource handlers for notes) | modify — same |
| `api/diagram_handlers.go` | modify — same |
| Existing GORM models for `Note`, `Diagram`, `Threat` | modify — add `AutoGenerated` field |
| `cmd/server/main.go` | modify — register repositories with the server's DI |
| `test/integration/usability_feedback_test.go` | new |
| `test/integration/content_feedback_test.go` | new |

## Implementation sequence (preview for the plan)

1. GORM models: add `UsabilityFeedback`, `ContentFeedback`, plus the `AutoGenerated` field on `Note`/`Diagram`/`Threat`. Register the two new models in `models.AllModels()`. Update `internal/dbschema/schema.go` so schema validation matches what AutoMigrate will produce.
2. Generate OpenAPI: add schemas, add 6 operations + `x-tmi-authz`, add `auto_generated` (read-only) to 3 entity schemas, run `make generate-api`.
3. UsabilityFeedback: repository interface → GORM impl → handlers → tests (TDD).
4. ContentFeedback: repository interface → GORM impl → handlers → tests (TDD).
5. Wire `auto_generated` setting into the existing threat / note / diagram create-handlers.
6. Integration tests.
7. Oracle DB review (mandatory — schema changes touch FKs, CHECK constraints, JSONB-vs-CLOB choice).
8. Lint, build, unit + integration tests.

## Oracle compatibility notes

This change is heavily DB-touching, so the `oracle-db-admin` subagent will be dispatched as part of the implementation. Up-front items the subagent will weigh in on:

- `BOOLEAN` column → Oracle uses `NUMBER(1)` with a CHECK constraint. GORM dialect handles this if configured correctly; verify.
- `jsonb` vs `CLOB`: PostgreSQL's `jsonb` type doesn't exist on Oracle. Use `CLOB` with a CHECK constraint that calls `JSON_OBJECT_T` parsing, OR (for Oracle 21c+) use the native `JSON` data type. GORM tag and migration must be database-aware.
- `text` columns with no length cap → on Oracle map to `CLOB`, on PG map to `text`. Confirm GORM model tag handling.
- ON DELETE CASCADE FK semantics differ slightly between PG and Oracle (Oracle has stricter constraints on referenced tables); subagent will validate.
- CHECK constraints on enum columns: both DBs support, but constraint naming must be unique per Oracle's flat namespace.
- Index naming: Oracle has a 30-character identifier limit (older versions) or 128 (12.2+); confirm the target Oracle version and pick names accordingly.
- Generated UUIDs: Oracle has no native UUID type; we use `RAW(16)` with `SYS_GUID()` or app-side generation. App-side (GORM `uuid.New()`) is the existing TMI pattern.

The subagent's verdict is required before merge.
