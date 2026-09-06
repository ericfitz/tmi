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
