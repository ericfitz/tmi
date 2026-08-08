# Oracle Follow-up Bugs (#697 #699 #700 #704 #705 #706 #707) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the seven Oracle-compatibility follow-up bugs filed from the 1.8.1 Oracle test pass: JSONRaw/CLOB write gaps (#697), lowercase scan-struct tags (#699), empty-vs-NULL write symmetry (#700), unbacked groups upsert conflict target (#704), never-inserted ID back-fill (#705), client-side created_at echo (#706), and load-driven 500s from unclassified retry exhaustion (#707).

**Architecture:** All fixes follow established house patterns: `dberrors.Classify` as the single classification choke point, `StoreErrorToRequestError` for HTTP mapping, upsert-then-re-SELECT for Oracle MERGE (no RETURNING), parsed-model scanning for cross-engine column labels, and typed nullable Value() normalization mirroring the read-side #696/#698 fixes.

**Tech Stack:** Go, GORM (postgres/oracle/sqlite dialects), Gin, make targets only.

## Global Constraints

- MANDATORY: use make targets only — `make lint`, `make build-server`, `make test-unit name=X`, `make test-integration`. Never `go test` directly.
- Zero 500-error policy; Documented-Status-Code policy (503 is already documented on the affected POST operations — verified).
- Every new/changed function gets a `SEM@<sha>` marker via `/sem-annotate --update <files>` at the end of each task (or copy the style of neighbors).
- Structured logging only (`internal/slogging`), never `log` or `fmt.Println`.
- Branch: `dev/1.8.2/oracle-followups` off `main`. Conventional commits; each commit references its issue.
- oracle-db-admin subagent review is REQUIRED before completion (Task 8) — schema and repository changes are in scope.
- graphify: run `graphify update .` after code changes; subagents exploring code should prefer `graphify query` before grepping.
- Integration test functions must use the `_Integration` suffix.

## Verified Facts (do not re-derive)

- `internal/dberrors/classify_oracle_codes.go:113` already maps ORA-08177 → `ErrTransient`. Classification is NOT the gap in #707.
- `api/request_utils.go:583` `StoreErrorToRequestError(err, notFoundMsg, serverErrorMsg)` already maps `ErrTransient` → 503 (`ServiceUnavailableError`), `ErrDuplicate` → 409, `ErrConstraint` → 400, `ErrNotFound` → 404.
- `auth/db/retry.go` exhaustion paths (`retry.go:106` and `retry.go:182`) return `fmt.Errorf("transaction failed after %d attempts: %w", cfg.MaxRetries, lastErr)` — lastErr is whatever `fn` returned; if `fn` didn't classify, the result never matches `ErrTransient`.
- `api/alias_allocator.go` (`allocateNextAliasRowLocked`, and the sequence path) wraps raw GORM errors in `fmt.Errorf` WITHOUT `dberrors.Classify`. Under SERIALIZABLE, concurrent creates on one threat model contend on the `alias_counters` row → ORA-08177 → retries → exhaustion → unclassified error → 500. This is the #707 mechanism.
- Asset/note create handlers (`api/asset_sub_resource_handlers.go:196-198,443-445,630-635`, `api/note_sub_resource_handlers.go:222-223`) use bare `ServerError(...)` instead of `StoreErrorToRequestError`.
- `dberrors.Classify` is idempotent for ErrNotFound/ErrConstraint/ErrTransient/ErrPermission/ErrContextDone/ErrUndefinedObject (classify.go:27-32) — double-classification is safe.
- OpenAPI spec documents 503 (and 409, 400) on POST assets and POST notes — no spec change needed.
- `auth/repository/deletion_repository.go` has NO `gorm:"column:` tags anymore (verified by rg) — that #699 site is already gone; record as N/A on the issue.
- `models.Group` (api/models/models.go:346-354): `Provider`/`GroupName` have separate non-unique indexes only.
- `models.UserPreference.Preferences` is `JSONRaw` with `gorm:"not null"`.
- House pattern for #699: `GetGroupsForUser` (api/group_member_repository.go:567-606) — scan into the parsed model, partial projection, comment included.
- House pattern for #705: `ensureGroupExists` (api/database_store_gorm.go:147-161) — upsert then re-SELECT by natural key, never trust the struct PK.
- Dev cluster is currently docker-desktop running DB=oracle (no postgres pod). PG PVC data persists.

---

### Task 1: #697 — survey answers/ui_state through JSONRaw + user-preferences nil guard

**Files:**
- Modify: `api/survey_response_store_gorm.go:365-389` (Update path)
- Modify: `api/user_preferences_handlers.go:96-102`
- Test: `api/survey_response_store_gorm_test.go` (or nearest existing test file for this store), `api/user_preferences_handlers_test.go` (follow existing handler-test patterns in the file/siblings)

**Interfaces:** none new — same signatures.

- [ ] **Step 1: Write failing tests.** (a) A test that calls the survey-response Update path against the in-memory test DB and asserts the stored `answers`/`ui_state` round-trip through `models.JSONRaw` (assert the update map value type via a small refactor-visible behavior: after Update with empty-but-non-nil answers `{}`… simplest observable: Update with `map[string]any{}` marshals to `{}` and reads back as `{}`, and Update with nil clears to NULL/nil). (b) A test calling `GetCurrentUserPreferences` handler path with a user row whose `Preferences` is empty (`models.JSONRaw{}` / zero-length) asserting HTTP 200 with `{}` body, not 500.
- [ ] **Step 2: Run** `make test-unit name=<NewTestNames>` — expect FAIL (500 / type mismatch).
- [ ] **Step 3: Implement.** In `survey_response_store_gorm.go` change both assignment sites:

```go
updates["answers"] = models.JSONRaw(answersJSON)
...
updates["ui_state"] = models.JSONRaw(uiStateJSON)
```

(ui_state at :386 has the identical bare-[]byte defect; fix both. The `nil` branches stay as-is.) In `user_preferences_handlers.go` before the unmarshal:

```go
// Oracle hands a NULL/empty CLOB back as zero-length; return defaults
// instead of failing json.Unmarshal on empty input (#697, zero-500 policy).
if len(pref.Preferences) == 0 {
    c.JSON(http.StatusOK, UserPreferences{})
    return
}
```

- [ ] **Step 4: Run** `make test-unit name=<NewTestNames>` — expect PASS.
- [ ] **Step 5:** `make lint`, commit: `fix(api): route survey answers/ui_state updates through JSONRaw; guard empty preferences (#697)`

### Task 2: #705 — content-token upsert returns the stored ID

**Files:**
- Modify: `api/content_token_repository.go:130-166` (Upsert)
- Test: `api/content_token_repository_test.go` (follow existing tests for this repo)

- [ ] **Step 1: Write failing regression test:** upsert a token for `(user, provider)`, capture returned ID; upsert a second token (different access token) for the same pair; assert the second call returns the SAME ID as the first and that exactly one row exists.
- [ ] **Step 2: Run** `make test-unit name=<TestName>` — on SQLite the ON CONFLICT UPDATE branch keeps the row's original ID while the struct holds a fresh BeforeCreate UUID → expect FAIL.
- [ ] **Step 3: Implement.** Replace the back-fill at :163-164 with a read-back (house pattern, `ensureGroupExists`):

```go
// Read the row back rather than trusting the struct's PK: BeforeCreate
// populated row.ID client-side, but on the MERGE UPDATE branch (Oracle has
// no RETURNING) that UUID was never inserted (#705, same class as #703).
var stored models.UserContentToken
if err := r.db.WithContext(ctx).
    Where(&models.UserContentToken{UserID: row.UserID, ProviderID: row.ProviderID}).
    First(&stored).Error; err != nil {
    return dberrors.Classify(err)
}
token.ID = string(stored.ID)
```

- [ ] **Step 4: Run test** — PASS. **Step 5:** `make lint`, commit: `fix(api): content-token upsert returns stored ID, not client-side BeforeCreate UUID (#705)`

### Task 3: #706 — quota upserts report stored timestamps

**Files:**
- Modify: `api/user_api_quota_store_gorm.go:225-248`
- Modify: `api/addon_invocation_quota_store_gorm.go:135-158`
- Test: existing test files for these stores (find via `rg -l 'UserAPIQuotaStore|AddonInvocationQuotaStore' api/*_test.go`)

- [ ] **Step 1: Write failing regression tests** (one per store): upsert a quota, then upsert again with different limits; assert the second call's returned/echoed `CreatedAt` equals the first call's stored `CreatedAt` (not "now"). Use distinct clock values by setting the first row's CreatedAt in the past directly if needed.
- [ ] **Step 2: Run** — expect FAIL (second call echoes fresh autoCreateTime).
- [ ] **Step 3: Implement.** After the transaction succeeds, re-read by PK and use the stored row. `user_api_quota_store_gorm.go`:

```go
// Read the row back: on Oracle's MERGE UPDATE branch nothing is returned,
// so model still holds the client-side autoCreateTime for an insert that
// never happened (#706).
var stored models.UserAPIQuota
if err := s.db.WithContext(ctx).
    Where(&models.UserAPIQuota{UserInternalUUID: model.UserInternalUUID}).
    First(&stored).Error; err != nil {
    return UserAPIQuota{}, dberrors.Classify(err)
}
return s.modelToAPI(stored), nil
```

`addon_invocation_quota_store_gorm.go`: replace the `quota.CreatedAt = model.CreatedAt; quota.ModifiedAt = model.ModifiedAt` block with the same read-back into `stored` and assign `quota.CreatedAt = stored.CreatedAt; quota.ModifiedAt = stored.ModifiedAt`.
- [ ] **Step 4: Run tests** — PASS. **Step 5:** `make lint`, commit: `fix(api): quota upserts report stored created_at, not client-side value (#706)`

### Task 4: #707 — transient errors survive retry exhaustion and map to 503

**Files:**
- Modify: `auth/db/retry.go:106,182` (both exhaustion returns)
- Modify: `api/alias_allocator.go` (classify at every error-return in `allocateNextAliasRowLocked` and the sequence-path function)
- Modify: `api/asset_sub_resource_handlers.go:196-198` (single create), `:443-445` (bulk PUT), `:630-635` (batch), `api/note_sub_resource_handlers.go:222-223`
- Test: `auth/db/retry_test.go` (exists — follow its fake patterns), handler tests in `api/`

- [ ] **Step 1: Write failing tests.** (a) retry-exhaustion test: a `fn` that always returns an UNCLASSIFIED but retryable-looking error (e.g. an error whose string matches a transient pattern, or construct with a fake OraErr — reuse whatever `retry_test.go`/`classify_oracle_test.go` already use to simulate ORA-08177); assert `errors.Is(err, dberrors.ErrTransient)` on the final error from `WithRetryableGormTransaction`. (b) handler test: asset create whose store returns `fmt.Errorf("transaction failed after 3 attempts: %w", dberrors.Wrap(baseErr, dberrors.ErrTransient))` → assert HTTP 503; same for note create; also assert `ErrDuplicate` → 409 to lock in the mapping.
- [ ] **Step 2: Run** — expect FAIL (500s, no ErrTransient match).
- [ ] **Step 3: Implement.**
  - `auth/db/retry.go` both tails: `return fmt.Errorf("transaction failed after %d attempts: %w", cfg.MaxRetries, dberrors.Classify(lastErr))` (Classify is idempotent; this is the choke point that guarantees any retry-exhausted transient stays `ErrTransient` regardless of call-site discipline).
  - `api/alias_allocator.go`: wrap each returned DB error: `fmt.Errorf("alias_counters upsert: %w", dberrors.Classify(err))` etc. — keep the message prefixes, classify the inner error.
  - Handlers: replace bare `ServerError` after store-create failures with `StoreErrorToRequestError(err, "Threat model not found", "Failed to create asset")` (adjust messages per site; keep the existing `logger.Error` lines). Sweep BOTH files for any other `HandleRequestError(c, ServerError(` immediately following a store call and convert those too — the store errors are classified; swallowing them into unconditional 500s is the defect.
- [ ] **Step 4: Run tests + `make test-unit`** — PASS.
- [ ] **Step 5:** `make lint`, commit: `fix(api): classify retry-exhausted and alias-allocator errors so transient Oracle failures return 503 (#707)`

### Task 5: #699 — scan structs must survive Oracle's UPPERCASE result labels

**Files:**
- Modify: `api/project_store_gorm.go:437-468`
- Modify: `api/embedding_cleaner.go:30-35` + its query site
- Modify: `cmd/server/main.go:2433-2450` (`findUserByProviderIdentityGorm`)
- Modify: `cmd/dedup-group-members/main.go:118-135`
- Review only: `api/notifications/polling.go:19-23` (column tags on a parsed model — determine whether this table is Oracle-reachable; if it is PG-only LISTEN/NOTIFY infrastructure, add a comment saying why lowercase tags are safe; if Oracle-reachable, fix the same way)
- Create: lint check script/target `check-scan-struct-column-tags` wired into `make lint` (pattern: `rg -n 'gorm:"column:[a-z]' --glob '!*_test.go' api/ auth/ cmd/ internal/` excluding `api/models/`, `internal/dbschema/`, plus an allowlist for reviewed sites)
- Test: run existing store tests; the mechanical proof is Task 8's Oracle verification.

**Approach per site (house pattern from `GetGroupsForUser`, api/group_member_repository.go:567):** scan into the parsed GORM model with a partial projection, OR — where the projection includes join aliases that exist on no model (e.g. `team_name`) — scan into an ad-hoc struct WITHOUT `column:` tags whose exported field names resolve to the right DBName via the dialect NamingStrategy, and verify the alias is emitted through `ColumnName(dialect, ...)` so the label matches on both engines. Copy the explanatory comment style from GetGroupsForUser.

- [ ] **Step 1:** Fix `project_store_gorm.go`: keep the ad-hoc struct but delete all eight `gorm:"column:..."` tags (GORM derives DBNames from field names via the dialect naming strategy for anonymous structs; `TeamID` → `team_id`/`TEAM_ID`, `TeamName` → matches the alias — emit the alias with `ColumnName(query.Name(), "team_name")` in the Select so it's uppercase on Oracle). Add the #699 comment.
- [ ] **Step 2:** Fix `embedding_cleaner.go` (`idleThreatModel`): drop the tags, same reasoning; check the query emits plain column names (SELECT over threat_models — labels come back per engine; untagged fields + naming strategy match both).
- [ ] **Step 3:** Fix `cmd/server/main.go` `findUserByProviderIdentityGorm`: drop the `gorm:"column:internal_uuid"` tag (field `InternalUUID` derives correctly), keep the ColumnMap usage.
- [ ] **Step 4:** Fix `cmd/dedup-group-members/main.go`: raw SQL with alias `cnt` — drop tags; `Count` field derives `cnt`? NO — `Count` derives `count`. Rename field to `Cnt` or change the SQL alias to `count`… Oracle uppercases unquoted aliases, so use untagged fields AND rely on naming strategy: rename struct fields so derived names equal the SQL aliases (`Cnt int64` for `AS cnt`). Verify each field↔alias pair.
- [ ] **Step 5:** Add the lint check target + wire into `make lint` (mirror `check-unsafe-union-methods` in the Makefile). Allowlist `scripts/` (Oracle catalog queries legitimately use UPPERCASE tags — the check only flags lowercase `column:[a-z]` anyway, so scripts' UPPERCASE tags pass; still scope the check to `api/ auth/ cmd/ internal/`).
- [ ] **Step 6:** `make lint && make build-server && make test-unit` — PASS. Commit: `fix(api): remove lowercase column tags from ad-hoc scan structs; add lint guard (#699)`

### Task 6: #704 — back groups(provider, group_name) with a unique index

**Files:**
- Modify: `api/models/models.go:347-348` (Group tags)
- Modify: `api/database_store_gorm.go:135-161` (`ensureGroupExists` — duplicate fallback)
- Modify: `api/group_repository.go:337-351` (`UpsertGroup` — duplicate fallback + read-back)
- Modify: migration path — find where AutoMigrate runs (`rg -n 'AutoMigrate' cmd/ internal/ auth/`) and add a pre-migration dedupe for `groups` (precedent: `cmd/dedup-group-members`); keep the earliest row per `(provider, group_name)`, repoint `group_members.group_internal_uuid` and `threat_model_access` (grep for FKs referencing groups) to the survivor, delete losers — all inside one transaction per duplicate set.
- Modify: `cmd/dbtool/` if it surfaces schema (CLAUDE.md rule: dbtool must track schema changes — check `rg -n 'groups' cmd/dbtool/`)
- Test: unit test for the dedupe helper + fallback path.

- [ ] **Step 1: Schema tags:**

```go
Provider  DBVarchar `gorm:"size:100;not null;index:idx_groups_provider;uniqueIndex:uniq_groups_provider_group_name,priority:1"`
GroupName DBVarchar `gorm:"size:500;not null;index:idx_groups_group_name;uniqueIndex:uniq_groups_provider_group_name,priority:2"`
```

Oracle index-name limit is fine (30+ chars OK on 23ai, 128-byte limit); name length 31 chars — verify against the project's identifier-limit conventions (other uniqueIndex names in models.go).
- [ ] **Step 2: Pre-migration dedupe** so AutoMigrate's index creation cannot hit ORA-01452 / PG 23505. Write failing unit test against SQLite: seed two groups with same (provider, group_name) + memberships/access rows pointing at each, run dedupe, assert one survivor, repointed children, index creation succeeds.
- [ ] **Step 3: Duplicate fallback in both upserts:** after `Create` returns error, if `errors.Is(dberrors.Classify(err), dberrors.ErrDuplicate)` → fall through to the existing re-SELECT (ensureGroupExists already re-SELECTs on success; extend so the ErrDuplicate path also re-SELECTs and returns that UUID instead of erroring). In `UpsertGroup`, add the same catch + re-SELECT (and it currently never reads back — keep interface, just don't fail on the race). `IsRetryable` does NOT retry ErrDuplicate, so this catch is the only recovery.
- [ ] **Step 4:** `make test-unit` + `make lint` + `make build-server`. Commit: `fix(db): back groups(provider,group_name) with a unique index, dedupe first, tolerate upsert races (#704)`
- [ ] **Step 5 (deferred to Task 8):** run the `pg_indexes` query against the PG dev DB and record the answer on the issue.

### Task 7: #700 — write-side empty→NULL symmetry for nullable strings

**Files:**
- Modify: `api/models/types.go` (`NullableDBVarchar.Value`, `NullableDBText.Value`)
- Modify: `api/team_store_gorm.go:345-365` (typed updates map values)
- Modify: any other raw `*string`/string update-map sites feeding nullable columns found by `rg -n 'updates\[' api/*_store_gorm.go` — convert to `models.NewNullableDBVarchar(...)` / NullableDBText equivalents where the column is a nullable type
- Create: PG backfill — add a `dbtool` subcommand (or extend an existing cleanup command) running `UPDATE <table> SET <col> = NULL WHERE <col> = ''` for every NullableDBVarchar/NullableDBText column (enumerate from models); PG-only, idempotent
- Test: unit tests for Value() normalization; existing store tests must stay green.

- [ ] **Step 0: Audit models.go:808** (`GroupMember.UserInternalUUID` inside `uniqueIndex:idx_gm_group_user_type`): confirm no writer ever writes `Valid=true, String=""` (values are UUIDs or NULL). Record the audit conclusion in the commit message. If a writer can produce empty-valid, stop and surface before proceeding.
- [ ] **Step 1: Failing tests:** `NullableDBVarchar{Valid: true, String: ""}.Value()` returns `nil, nil`; same for NullableDBText.
- [ ] **Step 2: Implement** (mirror JSONRaw.Value comment):

```go
func (v NullableDBVarchar) Value() (driver.Value, error) {
    // Empty normalizes to NULL for cross-engine symmetry: Oracle stores ''
    // as NULL regardless, so PG must not persist '' that Oracle-parity reads
    // (#698) then report as NULL-like (#700). Mirrors JSONRaw.Value.
    if !v.Valid || v.String == "" {
        return nil, nil
    }
    return v.String, nil
}
```

- [ ] **Step 3:** Convert `team_store_gorm.go` update-map values: `"description": models.NewNullableDBText(team.Description)`-style (check constructor names in types.go; create the missing constructor if only the Varchar one exists), `"email_address": models.NewNullableDBVarchar(...)`, etc. Survey-response `answers`/`ui_state` already done in Task 1.
- [ ] **Step 4:** dbtool backfill subcommand; unit-test the SQL generation, not live execution.
- [ ] **Step 5:** Do NOT do the issue's item 4 (removing `Valid && String != ""` idioms) — the issue says after #698 soaks. Note it in the PR.
- [ ] **Step 6:** `make test-unit`, `make lint`, commit: `chore(api): normalize empty string to NULL on nullable write path; typed update maps; PG backfill tool (#700)`

### Task 8: Verification, reviews, version, PR

- [ ] `make lint && make build-server && make test-unit` — all green.
- [ ] `graphify update .` and `/sem-annotate --update <changed files>`.
- [ ] Dispatch **oracle-db-admin** subagent over the full diff (schema change #704, repo changes, Value() semantics). Fix BLOCKING items; fold notes.
- [ ] Run **security-review** skill; stop and report if findings.
- [ ] PG verification: `make dev-up CLUSTER=docker-desktop` (default DB=postgres), then run `SELECT indexname, indexdef FROM pg_indexes WHERE tablename='groups';` via the db skill — record answer on #704. Run `make test-integration`.
- [ ] Oracle verification: `make dev-up CLUSTER=docker-desktop DB=oracle`, OAuth flow as alice, check `/me` groups and `GET /projects` return populated fields (#699), create asset/note sanity (201).
- [ ] Version bump to 1.8.2 in `.version` AND `api-schema/tmi-openapi.json` info.version; `make build-server` prints 1.8.2.
- [ ] Commit remaining, push branch, open PR to main titled `fix(db): Oracle follow-up batch (#697 #699 #700 #704 #705 #706 #707)` with `Fixes #697` … `Fixes #707` lines in the body.
- [ ] Comment the pg_indexes answer on #704 and the deletion_repository N/A finding on #699.
