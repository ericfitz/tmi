# Session progress — 2026-09-19

## Landed (pushed to main)

- **PR #930 merged** (`0009f617`, 1.13.17) — `fix(api)`, refs #926 (open question settled; the issue stays open
  for retiring the migration). Eric's decision (ADR
  `docs/superpowers/specs/2026-09-19-adr-retire-none-unknown-severity.md`): severity `none` means
  `informational`, and `unknown` is retired: stored `unknown`/legacy `5` are cleared to NULL by the existing
  idempotent boot migration, a write of `unknown` is stored as unset, and `unknown` leaves the `severity`
  list-filter enum. Oracle review APPROVED WITH NOTES (typed `ELSE` added to the migration `CASE`);
  PostgreSQL integration 91/0/9; security review no findings. tmi-ux told of the client changes.
- **Deployed:** api.tmi.dev on 1.13.17 (main `0009f617`); Terraform applied no infrastructure changes; no
  stored `unknown` rows existed there. k3s-rp remains on 1.13.15. Four images 0 critical/high; `tmi-redis`
  carries glibc CVE-2026-19499 (High, no fix published, `strfmon` only, not reachable from Redis), tracked
  in #931.

# Session progress — 2026-09-18

## Landed (pushed to main)

- **PR #923 merged** (`a7cac3f9`, 1.13.13) — `fix(api)` closes #921 (prod 500 reported by tmi-tf-wh): three
  metadata key rules disagreed (spec and handler accepted `:`, the GORM `BeforeSave` hook allowed only
  `[a-zA-Z0-9_-]`/128), so a spec-valid key became a 500 on every metadata endpoint. One exported
  `validation.MetadataKeyPattern` (the spec pattern, 1-256) is reused everywhere, and
  `StoreErrorToRequestError` maps any model-hook `ValidationError` to 400. Oracle review APPROVED WITH NOTES;
  follow-up #922 (`/` keys are storable via bulk but not addressable at `/metadata/{key}`).
- **PR #927 merged** (`d4baf920`, 1.13.14) — `fix(api)` closes #925 (requested by Eric and tmi-ux): idempotent
  startup migration of legacy stored threat `severity`/`priority`/`status` values (old tmi-ux numeric keys and
  display strings) to canonical strings, in one READ COMMITTED transaction. Decision on write: normalize, not
  reject; the threat store canonicalizes the three fields on every write path and canonicalizes list filter
  values, and the fields stay free-form. Oracle review APPROVED WITH NOTES; PostgreSQL integration 91/0/9.
  Follow-up #926: retire the migration and the legacy `severityOrder` entries once every environment has run
  it, and settle `none` = informational vs unknown.
- **PR #928 merged** (`7a68fcda`, 1.13.15) — `fix(api)` closes #916. Eric's decision (option A, ADR
  `docs/superpowers/specs/2026-09-18-adr-unclassified-settings-keys-addressable.md`): the by-key admin settings
  endpoints hide a key only when the registry classifies it as internal, so admin-created custom keys can be
  read and deleted. PostgreSQL integration 91/0/9.
- **Deployed:** k3s-rp and api.tmi.dev on 1.13.15 (main `7a68fcda`; api.tmi.dev applied 2026-09-19). All five
  images 0 critical/high; Terraform applied no infrastructure changes; the boot migration canonicalized 34
  legacy threat severity/priority/status values.

## Landed (pushed to main)

- **PR #914 merged** (`4359a42e`, 1.13.9) — `fix(extract)`: `extract.ExtractWithDeadline` recovers extractor
  panics and returns `ErrMalformed`; govulncheck gate gains an allowlist with a review-by expiry
  (`scripts/ci-govulncheck.sh`, `.github/govulncheck-allow.txt`) for GO-2026-6452 (excelize v2.11.0, no fixed
  release; review by 2026-10-17). Security-gate change, merged on Eric's approval.
- **PR #912 merged** (`c91b16ea`, 1.13.10) — `fix(api)` closes #909: threat model list owner/reviewer filters
  drop the Oracle-invalid `JOIN ... AS`, `ListWithCounts` returns its query error instead of an empty 200, and
  the list read retries transient faults. Server half of #910: severity sort ranks legacy values. The
  integration runner now starts the OAuth stub itself. Oracle review APPROVED WITH NOTES; Oracle test verified
  red/green on ADB; PostgreSQL integration 91/0/9. #910 stays open for the tmi-ux half (default sort direction).
- **PR #917 merged** (`dcd8d846`, 1.13.11) — `fix(db)` closes #906. Eric's architectural decision: keep
  SERIALIZABLE as the wrapper default and proactively opt 31 provably insert-only transactions down to READ
  COMMITTED (ADR `docs/superpowers/specs/2026-09-17-adr-transaction-isolation-opt-down.md`, inventory of 141
  sites alongside it). First-ever alias counter allocation absorbs ORA-00001 (`api/alias_allocator.go`), a real
  finding from the Oracle review, with a deterministic Oracle regression test. Oracle review APPROVED WITH
  NOTES (applied); security-review clean; oracle-tagged api suite 16 pass on ADB.
- Filed #911 (Backlog): remaining swallowed list/count errors and related store cleanups. #913 (tmi-tf-wh:
  list/cancel own webhook deliveries) is in the backlog.
- SEM markers re-anchored for the entities touched by the three PRs.

# Session progress — 2026-09-17

## Landed (pushed to main)

- **PR #902 merged** (`b01ccb8e`, 1.13.5) — Oracle follow-ups, one commit per issue: `chore(dbschema)` the users
  provider-lookup index restore runs on `context.WithoutCancel` (#895); `fix(api)` every create/update/delete
  fallback maps `dberrors.ErrTransient` to the documented 503 via `WriteErrorToRequestError`, user creation at
  login retries transient faults at READ COMMITTED, and the OCI runner saves the dev server pod log plus honors
  `TMI_TEST_HTTP_RUN` / `TMI_TEST_ORACLE_RUN` / `TMI_TEST_WORKFLOW_RUN` (#900); `fix(dbschema)` the METADATA
  table INITRANS raise is skipped on Autonomous Database, which ignores ALTER TABLE's physical attributes,
  detected via `SYS_CONTEXT('USERENV','CLOUD_SERVICE')` (#897); `test(oci)` the workflow suite's direct-DB
  helpers open the ADB through godror (`-tags oracle`, UTC session pinned) so admin drain/seed hit the server's
  database (#898). All four closed. Oracle review APPROVED WITH NOTES after one blocking timezone fix. The six
  originally failing OCI cases pass on tmiadb; PostgreSQL integration 91/0/9.
- Filed #903: false ORA-08177 exhaustion on THREAT_MODEL_ACCESS / USERS inserts on ADB (now a 503, root cause
  visible in the captured pod log).
- Deploy: k3s-rp from main 1.13.5; api.tmi.dev deployed at 1.13.6 (main `86449c22`).
- **PR #907 merged** (`4c9d5815`, 1.13.7) — `fix(db)` closes #903. Measured on tmiadb with a single session and
  no concurrent writer: INI_TRANS already 10/20, so not ITL exhaustion; false ORA-08177 tracks index leaf splits
  and delayed block cleanout inside the SERIALIZABLE transaction (120 creates: 20 hits, 2 exhausted = 503).
  `GormThreatModelStore.Create` now runs at READ COMMITTED (insert-only on a fresh UUID; #801/#900 pattern);
  60 creates, 0 hits. New oracle-tagged regression `TestThreatModelCreateFalse08177OracleIntegration`. Oracle
  review APPROVED WITH NOTES (applied); PostgreSQL integration 91/0/9. Not deployed.
- Filed #906 (Backlog): decide whether the retryable transaction wrappers should default to READ COMMITTED with
  SERIALIZABLE opt-in. Reverses #451/#449, so it needs Eric's architectural decision and an ADR.

# Session progress — 2026-09-16

## Landed (pushed to main)

- **PR #899 merged** (`23e80035`, 1.13.3) — `chore(dbschema)`: `installOracleAppendOnly` compares the
  installed trigger source against the intended DDL and skips `CREATE OR REPLACE TRIGGER` when they match,
  so steady-state boots issue no DDL on the three audit tables. Oracle integration test
  `TestAuditAppendOnlyTriggersSteadyStateNoDDLOracleIntegration` passed on ADB. Closes #893.
  Oracle review APPROVED WITH NOTES (notes applied).
- **PR #896 merged** (`9d11bae9`, 1.13.2) — `chore(dbschema)`: `withDDLRetry` backoff honors the migration
  context (#891) and `BackfillSystemSettingOrigin` takes a ctx (#892). Both closed.
- Oracle ADB verification pass of the #890 batch done (`make test-integration-oci`): #845/#758/#763/#807
  code paths show no Oracle regressions. Filed #897 (ADB ignores METADATA INITRANS), #898 (workflow suite
  cascades when the reused ADB already has admins), #900 (one `POST /threat_models` 500 under ORA-08177
  contention; the OCI runner captures no server pod log, so the root cause was lost).
- Not deployed: api.tmi.dev and k3s-rp remain on 1.12.3.
- **PR #890 merged** (`9d72eab2`, 1.13.0) — backlog batch, five issues in one squash:
  `fix(settings)` ReEncryptAll runs in one transaction (#845); `chore(dbschema)` migration context threaded
  through the schema-evolution helpers, dbtool `--schema` cancels on Ctrl-C (#758); `feat(api)`
  `SystemSetting.origin` (seeded|explicit, readOnly) on the admin settings API (#803); `chore(db)`
  `make check-oracle-ddl-via-gorm` lint plus `dbschema.ExecDDL` (#763); `feat(dbtool)` schema fingerprint
  preflight with `--skip-schema-check` (#807). All five closed. Oracle reviews APPROVED WITH NOTES;
  follow-ups filed as #891 (ctx-aware withDDLRetry backoff), #892 (ctx in BackfillSystemSettingOrigin),
  #893 (skip append-only trigger DDL on steady-state boots). Oracle ADB verification of the batch still
  pending (`make test-integration-oci`). Not deployed.
- **PR #886 merged** (`32e22d40`, 1.12.0) — `feat(auth)`: optional `addon_id` on client credentials
  (requires `direct_write` + existing addon) so a direct_write automation's writes are not delivered back
  to its own addon's webhook subscription; `tmi_addon_id` claim feeds the #876 source-addon context.
  ADR: `docs/superpowers/specs/2026-09-16-adr-client-credential-addon-link.md`. Closes #883.
  Oracle review APPROVED WITH NOTES (notes applied).
- **PR #887 merged** (`f643ff94`, 1.12.1) — `fix(api)`: HTML-injection event-handler regex now requires
  `<tag ...` context, so prose like `deletion_protection = true` is accepted. Fixes #885.
- **PR #888 merged** (`57fd42ef`, 1.12.2) — `test(webhooks)`: Oracle integration test proving map-keyed
  `UpdateStatus` resolves on ADB (#881 premise was false: `SkipQuoteIdentifiers` folds unquoted keys). Closes #881.
- Deployed: k3s-rp on 1.12.x from main. api.tmi.dev still on 1.11.1 until the AWS push runs.

# Session progress — 2026-09-05 (later session)

## Landed (pushed to main)

- **PR #852 merged** (`2693b99b`, 1.9.19) — `build(scripts)`: `docker buildx create` now passes
  `--config ~/.docker/buildkitd.toml` so the `tmi` builder gets the GC policy that caps the build cache.
  Existing builders must be recreated (`docker buildx rm tmi`) to pick it up; creation fails on a
  machine without that file.
- **PR #853 merged** (`45eb517f`, 1.9.20) — `docs(claude)`: CLAUDE.md trimmed 29.1k → 23.6k chars
  (directory layout, WebSocket tour, request-flow diagram, container target lists, generic Go line
  removed; condensed into Claude memory files `tmi-code-map` / `tmi-container-builds`). CATS gates and
  gotchas moved to the project-local skill `.claude/skills/cats-tmi/SKILL.md`; CLAUDE.md keeps a pointer.
  `oracle-db-admin` agent description quoted as a YAML block scalar.

## Machine-local changes this session (not in git)

- `/doctor` run: `security-guidance` plugin disabled; `aws-cloudformation` and `amazon-bedrock` skills off;
  Claude Code auto-update re-enabled; allow rules added in `.claude/settings.local.json` for
  `make deploy-aws`, `scripts/deploy-aws.sh`, `gh pr merge`, `gh issue close`, the deps `bump.py`
  script, and `ls ~/.keys`. Global rule clarified: listing `~/.keys/` is fine, dumping contents is not.
- `PROGRESS.md` is now tracked (staged); to be included in a PR opportunistically next session.

---

# Session progress — 2026-08-23

## Landed

- **PR #812 merged** (`630c1302`) — Phase A config registry consolidation, 1.9.0.
  Approved 4 held runs (#797 friction), all 14 checks green. #809 auto-closed.
- **CATS upstream**: issue https://github.com/Endava/cats/issues/209 and
  PR https://github.com/Endava/cats/pull/210 filed. Cross-referenced on TMI
  #596 and #785. `CATS-UPSTREAM-BUG-DRAFT.md` marked FILED.
- **PR #816 open** — config-reference.md restart semantics + spec §1 amendment.

## Decisions taken (user)

1. **#809 endgame → delete** the two dead `rate_limit.*` keys. Filed as **#813**.
2. **Spec §1 → amend to measured reality** (89 Static / 11 Hot). Done in #816.
3. **#797 → GitHub App** (`BUMP_APP_ID`/`BUMP_APP_KEY`). Blocked on user.

## #813 — change map (investigated, not yet started)

Branch off main **after #816 lands**. No config-reference.md overlap
(`rate_limit.*` are DB-only, absent from that table — only
`server.disable_rate_limiting` appears, and it stays).

**Remove the defs:**
- `internal/config/setting_defs_misc.go:385-400` — the two `SettingDef`s
- `internal/config/classification_registry.go:210-211` — the two classifications
  (plus the explanatory comment block above them)

**Pinned lists to update:**
- `internal/config/registry_coverage_test.go:106` — `seededKeys` golden list, 9 → 7
- `internal/config/seed_projection_test.go:15-16`
- NOT the transitional ratchet — the rate_limit keys are absent from it
  (only `server.disable_rate_limiting` is there, a different key)
- NOT the 70-key `migratable_settings_equivalence_test.go` baseline — these
  keys have no `Get`, so they never appear in `GetMigratableSettings()`

**Test fixtures using the keys as sample data (retarget to a surviving key):**
- `api/models/system_setting_test.go:131-132`
- `api/settings_service_test.go:279-280,657`
- `api/config_handlers_test.go:970,1003`
- `api/provider_settings_adapter_test.go:19,33`
- `internal/dbschema/system_setting_origin_backfill_test.go:178,188,243,254,261`

**Doc/example references:**
- `api/models/system_setting.go:16` — doc comment example
- `api-schema/tmi-openapi.json:6980,7039` — spec examples → regenerate `api/api.go`
- `scripts/dev-config.py:47-48`

**Row cleanup (the real work):**
New `dbschema` function following `BackfillSystemSettingOrigin`'s shape
(`internal/dbschema/system_setting_origin_backfill.go:122`), wired at all three
call sites that run the others:
- `cmd/server/main.go:576` (non-fatal warn)
- `auth/config_adapter.go:321` (non-fatal warn)
- `cmd/dbtool/schema.go:139` (surfaced as error — admin remediation path)

Delete is DML, so the Oracle PrepareStmt DDL no-op hazard does not apply.
Decision: delete unconditionally (the keys leave the product; an explicit-origin
row left behind recreates exactly the list-but-404 shape #809 was about), but log
key names + row count at INFO — never values.

**Gates:** lint, build, unit, `make generate-api` after the spec edit, and
**oracle-db-admin review is mandatory** (seed path + data cleanup).

### #813 ordering constraint (found during investigation)

`expectedSeedValues()` (`internal/dbschema/system_setting_origin_backfill.go:80`)
derives from `models.DefaultSystemSettings()`, which projects the registry. Once
the two defs are removed, a surviving `rate_limit.*` row with NULL origin no
longer matches any known default, so `BackfillSystemSettingOrigin` would stamp it
**explicit**.

Therefore the prune must run **before** `BackfillSystemSettingOrigin` at all three
call sites, not after. Otherwise every upgrade stamps two rows explicit and then
deletes them — harmless but wasteful, and it muddies the backfill's row count in
the logs. Flag this explicitly for the oracle-db-admin review.

## #813 — implementation complete (commit 24731679)

Branch `dev/1.9.2/delete-dead-rate-limit-settings`, rebased onto main @ 93c6cb08 (1.9.1).

**Gates:** build clean · lint 0 issues · unit **2750 passed / 0 failed** ·
integration **85 passed / 0 failed** (matches the #812 baseline; first run was
correctly refused as NOT-a-pass because the OAuth stub was down and workflow
tests skipped — restarted the stub and re-ran).

**Two findings during implementation:**

1. **Ordering is load-bearing.** `expectedSeedValues()` derives from the
   registry, so once the defs are gone a surviving NULL-origin `rate_limit` row
   matches no known default and `BackfillSystemSettingOrigin` would stamp it
   explicit right before the prune deletes it. Prune runs first at all three
   call sites; `TestPruneRetiredSystemSettings_RunsBeforeOriginBackfill` pins it.

2. **Soft-delete trap that the tests would not have caught.** If
   `models.SystemSetting` gained a `gorm.DeletedAt`, `Delete` becomes an UPDATE
   and the row survives as soft-deleted — still `VisibilityInternal`, still the
   LIST-shows-it/GET-404s shape of #809. Every other test would keep passing,
   because GORM's `Count` filters soft-deleted rows exactly like `Find`. Verified
   no such field exists (statically and via `Unscoped()`), and added
   `TestPruneRetiredSystemSettings_HardDeletes` so a future soft-delete fails
   loudly.

**Guardrails watched to fail:** re-adding the classification entry, and swapping
the IN-list for `LIKE '%rate_limit%'` (which destroys
`server.disable_rate_limiting`, the live setting).

**Outstanding:** oracle-db-admin verdict, then push + PR.

## #813 — PR #819 open (branch at 1.9.2)

Four commits on `dev/1.9.2/delete-dead-rate-limit-settings`, plus CI's version
bump to 1.9.2. Held wave approved by hand (#797, third time today).

**oracle-db-admin: APPROVED WITH NOTES, no blocking issues.** The reviewer
traced the bind path through GORM v1.31.2 and gorm-oracle v1.1.3 source and
confirmed: the IN-list DELETE binds correctly on godror with accurate
RowsAffected; the PrepareStmt no-op hazard is DDL-only (Oracle does DDL work at
parse, DML at execute) so the package comment's claim is right; no ''/NULL
exposure; ordering correct at all three sites; all three inherit the
cross-replica migration advisory lock.

Notes N1, N2, N4 fixed in-branch; N3 deferred to **#818** (Backlog).

**N2 was the valuable one.** My `deleted_at` test reasoned about GORM core,
which diverts Delete to an UPDATE only for a field typed `gorm.DeletedAt`.
gorm-oracle diverts on `LookUpField("deleted_at") != nil` — by NAME, any type.
TMI's tombstoned models use plain `*time.Time`, so that shape on SystemSetting
is realistic, and it would leave SQLite and Postgres hard-deleting, the test
green, and **Oracle alone silently not deleting** — reproducing #809 on the one
platform the change exists to fix. Assertion is now by column name; planting
`DeletedAt *time.Time` trips it while the rest of the test still passes on
SQLite, which is exactly the point.

**Real-Postgres confirmation** from the dev-env teardown snapshot: the `.prev`
export (pre-#813 DB) carried `rate_limit: {requests_per_hour: 1000,
requests_per_minute: 100}`; the current export has no `rate_limit:` block at all,
while every adjacent `server.*` rate-limiting key survived. 62 -> 60 settings.

## Branch hygiene — verified, safe to prune

`dev/1.8.1/backlog-batch` is fully merged. Three confirmations: PR #693 merged
head SHA `81d9dbee` == current branch tip (nothing pushed after); all 17 commit
subjects appear in squash commit `f55124e3`; and since that squash's parent IS
the merge-base `0240c1fc`, the squash diff and the branch cumulative diff are
byte-identical (33 files, +3289/-1437). The 17 "unmerged" SHAs are squash-merge
artifact, the same shape that makes `release/*` branches look unmerged.

Awaiting user go-ahead to delete `dev/1.8.1/backlog-batch`,
`dev/1.9.0/config-model-redesign` (#812) and `dev/1.9.1/config-spec-amendment`
(#816), local and remote. `release/1.3.5` stays — release branches are permanent.

## Still needs the user

- **#797 GitHub App** — `BUMP_APP_ID`/`BUMP_APP_KEY`. Cost three manual
  approval cycles today (#812, #816, #819).
- **CATS**: upstream issue Endava/cats#209 and PR Endava/cats#210 are open and
  awaiting maintainer response.

## Branch prune 2026-08-23 — recovery SHAs

All deleted branches are recoverable by SHA until git GC:
`git branch <name> <sha> && git push origin <name>`

| Branch | Tip SHA | Merged via | Squash commit |
| --- | --- | --- | --- |
| dev/1.8.0 | e1522f56 | PR #692 | 0240c1fc |
| dev/1.8.1/backlog-batch | 81d9dbee | PR #693 | f55124e3 |
| dev/1.9.0/config-model-redesign | 25727549 | PR #812 | 630c1302 |
| dev/1.9.1/config-spec-amendment | 957806ac | PR #816 | 93c6cb08 |
| fix/627-version-bump-workflow | 02178930 | PR #796 | e66ea634 |
| release/1.3.5 | 74890311 | (no PR — see below) | — |

For the five dev/fix branches, each PR's head SHA matched the branch tip
exactly, so nothing was pushed after the merge.

`release/1.3.5` deleted at explicit user direction ("we're not going to revert
to that branch and have no further deployments to support"), overriding the
previous never-delete-release-branches rule. It carried 19 commits with no
patch-equivalent in main — all dependency bumps and the deps-bump CI workflow
iteration. Checked before deleting: the substantive piece (App-token minting in
`.github/workflows/deps-bump.yml`) is already present in main, so nothing unique
was lost. The rest were bumps main has long superseded.
