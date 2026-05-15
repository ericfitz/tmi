# Oracle VARCHAR2 BYTE-vs-CHAR Semantics Remediation

**Issue:** [#379](https://github.com/ericfitz/tmi/issues/379)
**Date:** 2026-05-14
**Author:** brainstorm collaboration (Eric Fitzgerald + Claude)
**Side-issue filed:** [#405](https://github.com/ericfitz/tmi/issues/405) (stale `auth/migrations/` references)

## Problem

TMI runs against PostgreSQL in development and Oracle Autonomous Database (ADB) in production. The two databases interpret `VARCHAR(N)` differently:

- **PostgreSQL** measures `VARCHAR(N)` in **characters**. `VARCHAR(2048)` admits 2048 characters regardless of encoding.
- **Oracle ADB** with default `NLS_LENGTH_SEMANTICS=BYTE` measures `VARCHAR2(N)` in **bytes**. Under AL32UTF8, a 4-byte-per-codepoint worst case means `VARCHAR2(2048)` admits as few as 512 characters before `ORA-12899: value too large for column`.

Tests that pass on PostgreSQL therefore do not guarantee the same payload size succeeds on Oracle. The failure mode is production-only and bites only when content drifts toward multi-byte UTF-8 (emoji, CJK ideographs, accented Latin).

The vulnerable surface in TMI is roughly **65 free-text VARCHAR fields** across 12 model files plus several mid-size identifier fields (names, emails, URLs). Discovered by `oracle-db-admin` subagent review during implementation of #361 (feedback APIs).

## Goals

1. Achieve cross-database length-semantics parity: a payload that fits on PostgreSQL fits on Oracle, and a payload that overflows on PostgreSQL overflows on Oracle.
2. Preserve cheap length validation at the application layer (OpenAPI middleware `maxLength`) by keeping declared-size columns where the size is meaningful.
3. Avoid hand-written SQL migrations; stay on GORM `AutoMigrate()` as the project's chosen schema-evolution mechanism.
4. Avoid degrading index/query performance.

## Non-goals

- Setting `NLS_LENGTH_SEMANTICS=CHAR` at the session level. It only affects future column creates, not existing tables — it would mask the real fix and add hidden state to connection setup.
- Reintroducing hand-written SQL migrations (see #405).
- Auditing whether other parts of the schema have separate Oracle-compatibility gaps (out of scope; addressed only as discovered).

## Approach

Two complementary changes:

1. **A new cross-dialect type, `DBVarchar` / `NullableDBVarchar`**, which emits `VARCHAR(N)` on PostgreSQL and `VARCHAR2(N CHAR)` on Oracle. Follows the established `DBText` / `DBBytes` pattern in `api/models/types.go`.
2. **Migration of the largest free-text columns to `DBText` / `NullableDBText`** (existing type that maps to `TEXT` on PostgreSQL and `CLOB` on Oracle). Applied only where the column is not indexed and not filtered — i.e., where the declared length is not load-bearing for anything but a sanity cap.

The classification rule:

- **`DBVarchar` / `NullableDBVarchar`**: any column that is indexed, filtered, queryable, enum-like, an identifier, a URL, or under 1000 characters.
- **`DBText` / `NullableDBText`**: any column that is large free-text prose (≥ 1000 characters declared), not indexed, not filtered, where the length cap is enforced by the OpenAPI layer rather than the database.

## Architecture

### New type: `DBVarchar`

Added to `api/models/types.go` alongside the existing `DBText` / `NullableDBText` / `DBBytes` types:

```go
type DBVarchar string

func (DBVarchar) GormDBDataType(db *gorm.DB, field *schema.Field) string {
    n := field.Size
    if n <= 0 {
        n = 255
    }
    switch db.Name() {
    case dialectPostgres:
        return fmt.Sprintf("varchar(%d)", n)
    case dialectOracle:
        return fmt.Sprintf("varchar2(%d char)", n)
    case dialectMySQL:
        return fmt.Sprintf("varchar(%d)", n)
    case dialectSQLServer:
        return fmt.Sprintf("nvarchar(%d)", n)
    case dialectSQLite:
        return fmt.Sprintf("varchar(%d)", n)
    default:
        return fmt.Sprintf("varchar(%d)", n)
    }
}

// Plus Scan(value any) error, Value() (driver.Value, error), String() string

type NullableDBVarchar struct {
    String string
    Valid  bool
}
// Plus same GormDBDataType, same Scan/Value/Ptr, plus NewNullableDBVarchar(*string) helper
```

Tag convention at usage sites:

```go
// Before
Name   string  `gorm:"type:varchar(256);not null;index:idx_threats_name"`
Status *string `gorm:"type:varchar(128);index:idx_threats_status"`

// After
Name   DBVarchar         `gorm:"size:256;not null;index:idx_threats_name"`
Status NullableDBVarchar `gorm:"size:128;index:idx_threats_status"`
```

The `size:` tag is GORM-native and read by `GormDBDataType` via `field.Size`. Declared size lives in the tag; dialect mapping lives in the type.

### Type-conversion ripple

`DBVarchar` is `string` under the hood (like `DBText`). Any code that reads model fields as plain `string` — store layer, handlers, tests — needs an explicit conversion: `string(threat.Name)`. The compiler catches every site at build time. The change is mechanical but real (≈ 65 fields × N consumers).

`NullableDBVarchar` replaces `*string`. The Ptr() helper and the `NewNullableDBVarchar(*string)` constructor mirror the `NullableDBText` API, so consumer rewrites stay uniform.

## Field classification

### Bucket A — `DBVarchar` / `NullableDBVarchar` (≈ 95 fields)

All `varchar(36)` UUID PKs and FKs. All enum/status fields (`varchar(8)` through `varchar(128)`). All names (`varchar(256)`). Emails (`varchar(320)`). Hashes and short identifiers (`varchar(50)`, `varchar(64)`, `varchar(100)`). Larger identifiers that are indexed or queried (`Group.GroupName varchar(500)`, `User.ProviderUserID varchar(500)`, `ClientCredential.ClientID varchar(1000)`, `Metadata.Value varchar(1024)`, `SystemAuditLog.FieldPath varchar(1024)`, `Webhook.Challenge varchar(1000)`, `RefreshTokenRecord.Token varchar(4000)`).

`ClientCredential.ClientID` and `RefreshTokenRecord.Token` are kept at their declared sizes per explicit user decision: both are ASCII content (no real byte/char divergence), both are uniquely indexed (CLOB would lose the unique index), and both have ample headroom for foreseeable claim/token sizes.

### Bucket B — `DBText` / `NullableDBText` (≈ 29 fields total: 21 in Batch 4, 8 in Batch 5)

All `*.Description varchar(2048)` fields: `ThreatModel`, `Diagram`, `Asset`, `Threat`, `Note`, `Repository`, `Group`, `Document`, `Team`, `Project`, `TeamNote`, `ProjectNote`, `WebhookTrigger`, `SystemSetting`, `SurveyTemplate`. Plus `ClientCredential.Description varchar(1024)`, `Threat.Mitigation varchar(1024)`, `UsabilityFeedback.Verbatim varchar(2048)`, `ContentFeedback.Verbatim varchar(2048)`, `SystemSetting.Value varchar(4000)`, `SurveyResponse.RevisionNotes varchar(4000)`, `SurveyAnswer.QuestionTitle varchar(1024)`.

All URL fields per user decision: `ThreatModel.IssueURI varchar(1000)`, `Threat.IssueURI varchar(1000)`, `Document.URI varchar(1000)`, `Repository.URI varchar(1000)`, `Webhook.URL varchar(1024)`, `Team.URI varchar(1000)`, `Project.URI varchar(1000)`, plus `SystemAuditLog.HTTPPath varchar(2048)`. None of these are indexed.

### Bucket C — leave alone

`text` columns (already `DBText`-equivalent). All `decimal`, `int`, `bool`, `timestamp` columns. Existing cross-dialect types: `JSONRaw`, `JSONMap`, `StringArray`, `CVSSArray`, `NullableSSVC`, `DBBytes`, `DBBool`.

### Sanity-checked edge cases

- `Metadata.Value varchar(1024)` is indexed via `idx_metadata_key_value` (priority 3, composite). Stays as `DBVarchar`. On Oracle, composite index key in AL32UTF8 worst case is within the default 6398-byte limit but worth verifying in Batch 0.
- `SystemAuditLog.FieldPath varchar(1024)` is indexed via `idx_sysaudit_field`. Stays as `DBVarchar`. ASCII content (JSON Pointer paths) — no realistic byte/char divergence.
- `RefreshTokenRecord.Token` retains the `varchar(4000)` declaration. If TMI ever ships JWT claims that approach 4 KB, the right migration is the **sha256-sidecar pattern** (CLOB token + `varchar(64)` hash with the unique index). Not needed today; track as a separate concern if claim size grows.

## Batching

Six pull requests, each independently shippable. Issue #379 is "Nice to Have" — no schedule pressure to bundle.

| Batch | Scope | Field count | Risk | Notes |
|-------|-------|------------:|------|-------|
| 0 | Add `DBVarchar` + `NullableDBVarchar` types and tests; verify GORM detects BYTE↔CHAR semantic difference | 0 model fields | Low | Foundation. **Verification gate for the whole project** (see Migration strategy). |
| 1 | UUID FK/PK columns (`varchar(36)`) | ≈ 70 | Low | ASCII content. Establishes the rewrite pattern across all models. |
| 2 | Enums and short identifiers (`varchar(8)` through `varchar(128)`, non-UUID) | ≈ 40 | Low | ASCII content. |
| 3 | Names, emails, mid-size identifiers (`varchar(256)`, `varchar(320)`, `varchar(500)`, `varchar(1000)`, `varchar(1024)` indexed) | ≈ 25 | Medium | First batch where multi-byte UTF-8 actually changes behavior. |
| 4 | Descriptions and large free-text → `NullableDBText` | ≈ 21 | High | Largest semantic change (`varchar` → `CLOB` on Oracle). Store-layer and handler consumers need updates. |
| 5 | URLs → `NullableDBText` | ≈ 8 | Medium | Same kind of change as Batch 4 but smaller blast radius. |

Each batch:

1. Merges to `dev/1.4.0` as its own PR.
2. Runs `make lint`, `make build-server`, `make test-unit`.
3. Runs `make test-integration` (PostgreSQL).
4. Runs `make test-integration-oci` (Oracle ADB).
5. Goes through `oracle-db-admin` subagent review; verdict must be `APPROVED` or `APPROVED WITH NOTES`.
6. Adds at least one multi-byte UTF-8 test case (emoji + CJK) per affected entity (Batches 3, 4, 5).

## Migration strategy

### PostgreSQL

GORM `AutoMigrate` handles all three migration shapes cleanly:

- **`varchar(N)` → `DBVarchar` (Batches 1–3):** column type stays `varchar(N)`. AutoMigrate sees no diff, issues no `ALTER`. No-op.
- **`varchar(N)` → `DBText` (Batches 4–5):** AutoMigrate emits `ALTER TABLE ... ALTER COLUMN ... TYPE text`. In-place cast, no data loss, no downtime.

Verification: `make test-integration` after each batch.

### Oracle ADB

**Shape 1: `VARCHAR2(N)` → `VARCHAR2(N CHAR)`** (Batches 1–3)

GORM-Oracle's column-type comparator normalizes `VARCHAR2(N)` and `VARCHAR2(N CHAR)` as equivalent and **skips** the `ALTER TABLE ... MODIFY`. Confirmed empirically by the Batch 0 detection probe (`scripts/oracle-char-probe/main.go`, commit 8d54f802): on a fresh ADB, AutoMigrating with `gorm:"type:varchar(256)"` produces `CHAR_USED='B'`, and re-AutoMigrating with `DBVarchar size:256` leaves the column at `'B'` — no `ALTER` emitted.

Therefore the sidecar SQL migration `scripts/oracle-migrate-varchar-char.sql` (added in Batch 0, commit 7f5cb9c2) is **mandatory** for every deploy of Batches 1–5 to any Oracle ADB instance with a pre-existing schema. The script iterates `USER_TAB_COLUMNS WHERE CHAR_USED = 'B' AND DATA_TYPE = 'VARCHAR2'` and re-issues `ALTER TABLE ... MODIFY (col VARCHAR2(N CHAR))` for each column. Idempotent: re-running it reports `converted=0 failed=0`.

Brand-new Oracle installs (no pre-existing TMI schema) are unaffected — `CREATE TABLE` emits `varchar2(N char)` directly from `DBVarchar.GormDBDataType` output, so the column is born CHAR-mode.

**Shape 2: `VARCHAR2(N)` → `CLOB`** (Batch 4)

GORM emits `ALTER TABLE ... MODIFY (col CLOB)`. Oracle supports inline conversion when no other constraints conflict; none of the Batch 4 columns have indexes, defaults, or check constraints (verified during inventory). Each conversion rebuilds the table segment for that column — on multi-million-row tables this could be a long lock, but TMI tables aren't there yet. Document in the deploy runbook: "expect a brief table lock on `<tables>`."

**Shape 3: URL columns** (Batch 5)

Audited the URL inventory against indexes: **none of the URL columns have indexes.** `Webhook.URL` is looked up by webhook ID, not by URL value. `ThreatModel.IssueURI` is display-only. Shape 3 reduces to Shape 2 for all migrated URLs. No special handling.

### Per-batch deployment order

1. Merge PR to `dev/1.4.0`.
2. CI: `make test-integration` (PG) — must pass.
3. CI: `make test-integration-oci` (Oracle ADB) — must pass.
4. `oracle-db-admin` subagent: `APPROVED` or `APPROVED WITH NOTES`.
5. Deploy to Heroku (PG); AutoMigrate runs on boot; smoke test.
6. Deploy to OCI ADB; AutoMigrate runs; smoke test; **verify `USER_TAB_COLUMNS.CHAR_USED = 'C'`** for the affected columns (per Batch 0 finding).

### Rollback

- **`DBVarchar` → `varchar`** rollback is trivial. Revert the model change; AutoMigrate is a no-op on PG and re-issues `MODIFY ... VARCHAR2(N BYTE)` on Oracle. Existing rows already conform.
- **`DBText` → `varchar`** rollback is harder. On Oracle, `CLOB` → `VARCHAR2(N CHAR)` succeeds only if every existing value fits the target length. If users write descriptions longer than the old cap between Batch 4 deploy and rollback decision, the `ALTER` fails. **Mitigation:** keep a length-check query in the deploy runbook for the first 24h after Batches 4 and 5 land. If no values exceed the old caps, rollback remains feasible.

## Testing

### Unit tests (Batch 0)

In `api/models/types_test.go` (or new file):

- `DBVarchar.GormDBDataType` returns `varchar(256)` for PG, `varchar2(256 char)` for Oracle, given `field.Size = 256`.
- Same for `NullableDBVarchar`.
- `Scan` accepts `string`, `[]byte`, and `nil`.
- `Value` returns the underlying string.
- `NullableDBVarchar.Ptr()` returns `nil` when `Valid = false`.

### Integration tests (Batches 3–5)

For each affected entity:

- A "multi-byte stress" test: submit a payload with an emoji-and-CJK string at exactly the OpenAPI `maxLength` cap. Verify the create/update succeeds on both PostgreSQL and Oracle (when `test-integration-oci` runs).
- A "boundary overflow" test: submit a payload one character over the OpenAPI cap. Verify the OpenAPI layer rejects with `400` before the value reaches the database — confirms the cap is enforced at the right layer.

### Schema-verification test (post-deploy)

`scripts/verify-oracle-char-semantics.sql`:

```sql
SELECT TABLE_NAME, COLUMN_NAME, DATA_LENGTH, CHAR_LENGTH, CHAR_USED
FROM USER_TAB_COLUMNS
WHERE DATA_TYPE = 'VARCHAR2' AND CHAR_USED = 'B'
ORDER BY TABLE_NAME, COLUMN_NAME;
```

Expected output after all batches: empty result set. Any rows are columns still in BYTE mode and need investigation.

## Risks and mitigations

| Risk | Likelihood | Mitigation |
|------|-----------:|-----------|
| GORM doesn't detect `VARCHAR2(N)` vs `VARCHAR2(N CHAR)` diff and skips the `ALTER` | Medium | Batch 0 verification protocol; fallback `oracle-migrate-varchar-char.sql` script. |
| `varchar` → `CLOB` conversion on Oracle takes a long table-rebuild lock | Low at current TMI scale | Document in runbook; schedule Batch 4/5 deploys in low-traffic windows. |
| Rollback from `DBText` blocked by oversized values | Low (window is hours, not days) | Length-check query in runbook for 24h post-deploy. |
| Type-conversion ripple introduces compile errors at unforeseen sites | Medium (the change spans 95 fields) | Compiler catches every site; rebuild + test cycle per batch. |
| Composite index containing `DBVarchar` field exceeds Oracle key-length budget | Low | Audit during Batch 0; specifically check `idx_metadata_key_value` and `idx_sysaudit_field`. |

## Open questions

None at spec-write time. The Batch 0 GORM-detection verification is an empirical question, not an open design question — both outcomes have defined paths forward.

## References

- Issue [#379](https://github.com/ericfitz/tmi/issues/379) — original report and option analysis
- Issue [#405](https://github.com/ericfitz/tmi/issues/405) — side-issue for stale `auth/migrations/` references
- Commit `1e9b5bca` — `oracle-db-admin` subagent review during #361 implementation that surfaced this concern
- [api/models/types.go:424–543](../../api/models/types.go#L424-L543) — existing `DBText` / `NullableDBText` implementation that `DBVarchar` mirrors
- [auth/db/gorm.go:97–125](../../auth/db/gorm.go#L97-L125) — dialect selection logic
- [auth/db/gorm_oracle.go](../../auth/db/gorm_oracle.go) — Oracle dialector setup
- [internal/dberrors/classify_oracle_codes.go](../../internal/dberrors/classify_oracle_codes.go) — ORA-12899 mapping (case 12899)
