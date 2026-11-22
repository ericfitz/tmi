# ADR: Upgrade deploys preserve existing configuration

Date: 2026-11-22. Status: accepted. Issue: #1205. Related: #1191, #1201
(`2026-11-20-adr-settings-precedence-row-wins.md`).

## Human-made architectural decisions (Eric, 2026-11-22)

1. A **new, initial deployment** ends up as a working system.
2. An **upgrade deployment** leaves the existing configuration unchanged. No deploy path
   overwrites a configuration row that someone chose.

## How the code meets it

- Server startup (`SettingsService.SeedDefaults`) only inserts missing keys, as `seeded`.
  The only rows it deletes are the retired keys listed in
  `internal/dbschema/dead_setting_prune.go`.
- `tmi-dbtool --import-config` without `--overwrite` skips existing `explicit` rows. It
  replaces `seeded` and NULL-origin rows and creates missing ones, stamping each row it
  writes `explicit`. A seeded row only holds a registry default, so replacing it does not
  change configuration anyone chose. Replacing it is also what lets a fresh deploy's
  import take effect, because the server seeds defaults before the import runs. The
  replace is conditional (`origin IS NULL OR origin <> 'explicit'`), so a row an admin
  sets during the import is not clobbered. This matches the dev `--restore-snapshot`
  path (`applySnapshotSettings`).
- `scripts/deploy-aws.sh --config-export` no longer passes `--overwrite`. Replacing
  explicit rows takes the opt-in `--overwrite-config` flag, which logs a warning.

## Consequence

Since #1201, explicit rows outrank env and ConfigMap values. An upgrade therefore keeps
both the stored rows and the values in effect for every explicit key. To change an
explicit key during a deploy, change it on purpose through the admin API or with
`--overwrite-config`.
