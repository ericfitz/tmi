# Design: Retention, pruning, and tamper protection for system_audit_entries (#400)

**Issue:** [#400](https://github.com/ericfitz/tmi/issues/400) — feat(api): retention and pruning for system_audit_entries (T7 follow-up)
**Date:** 2026-06-11
**Status:** Approved
**Depends on:** #453 (`docs/superpowers/specs/2026-06-11-453-audit-age-floor-design.md`) — this design extends the age-floor trigger machinery built there and must be implemented after it.

## Problem

`system_audit_entries` (#355) records every `/admin/*` write as T7 evidence. It has:

- **No retention** — the table grows monotonically.
- **No tamper protection** — unlike `audit_entries`/`version_snapshots`, there is no
  append-only trigger, so an admin with the app's DB credentials can silently erase the very
  evidence the table exists to keep (`internal/dbschema/audit_append_only.go` covers only the
  threat-model audit tables).
- **No reader beyond `ListByActor`** (the full read API is #398).

## Decisions

1. **Retention configured by env var** — `SYSTEM_AUDIT_RETENTION_DAYS`, default **365**, hard
   minimum **90**. Consistent with the other three retention knobs; not tunable by the very
   admins the table audits (T7). Settings-table tunability rejected.
2. **Age-floored append-only trigger** — the #453 pattern, applied to `system_audit_entries`:
   UPDATEs always blocked; DELETEs allowed only for rows older than the floor
   (configured − 1, hard minimum **90** — higher than `audit_entries`' 30 because this table is
   pure evidence and nothing legitimate ever deletes young rows from it).
3. **Hard delete** — no archive table, no soft delete. A compliance-driven archive can be a
   future issue if a real requirement appears.
4. **No partitioning** — write volume is moderate (~34 `/admin/*` write endpoints at admin
   cadence). Revisit tracked in #454.

## Design

### 1. Configuration — `api/audit_store.go`

```go
const defaultSystemAuditRetentionDays = 365
const minSystemAuditRetentionDays    = 90
```

Exported reader `SystemAuditRetentionDays()` following the #453 pattern
(`AuditRetentionDays()` etc.): reads `SYSTEM_AUDIT_RETENTION_DAYS` with the default, clamps to
the 90-day minimum with a logged warning. Used by both the pruner and the trigger install so
they cannot drift. `GormAuditService` gains a `systemAuditRetentionDays` field populated in
`NewGormAuditService`.

### 2. Tamper protection — `internal/dbschema/audit_append_only.go`

`AuditFloorConfig` gains `SystemAuditRetentionDays int`. A third trigger
`tmi_system_audit_entries_no_mutate` is installed on both dialects with floor
`clampFloor(SystemAuditRetentionDays, 90)`:

- **PostgreSQL:** reuses the existing `tmi_audit_append_only_guard()` function (floor via
  trigger argument), one new `DROP TRIGGER IF EXISTS` + `CREATE TRIGGER` pair on
  `system_audit_entries`.
- **Oracle:** one new `CREATE OR REPLACE TRIGGER` mirroring the other two
  (`SYS_EXTRACT_UTC(SYSTIMESTAMP) - NUMTODSINTERVAL(n, 'DAY')`, `RAISE_APPLICATION_ERROR(-20001, ...)`
  with "append-only" in the message).

No `internal/dberrors` change — P0001/ORA-20001 already classify to `ErrAppendOnlyViolation`.
`cmd/server/main.go` passes the new field from `api.SystemAuditRetentionDays()`.

### 3. Pruning — `api/audit_service.go`, `api/audit_store.go`, `api/audit_pruner.go`

New interface method on `AuditServiceInterface`:

```go
// PruneSystemAuditEntries removes system audit entries older than the configured
// retention period (SYSTEM_AUDIT_RETENTION_DAYS, default 365, minimum 90).
// Returns the number of entries pruned.
PruneSystemAuditEntries(ctx context.Context) (int, error)
```

`GormAuditService` implementation mirrors `PruneAuditEntries`: compute
`cutoff = now().UTC() - systemAuditRetentionDays`, then a single
`DELETE FROM system_audit_entries WHERE created_at < cutoff` inside
`authdb.WithRetryableGormTransaction`. No exclusions (unlike threat-model audit, there is no
"deleted" tombstone row to preserve).

`AuditPruner.prune()` calls it as the fourth step, with errors routed through the
`pruneFailureMessage` helper from #453 so append-only violations produce the actionable
"align retention config and restart" message.

### 4. Out of scope

- Read/query API for investigators → #398.
- Out-of-band alert sink → #395.
- Partitioning / `DROP PARTITION` pruning → #454.
- Archive-before-delete → file only if a compliance requirement materializes.

## Testing

- **Unit:** floor derivation for the new `AuditFloorConfig` field (extend the #453 table
  test: default 365 → 364; configured 100 → 99; configured 30 → clamped to 90);
  `SystemAuditRetentionDays()` clamping (env 30 → 90 with warning).
- **Integration (PG):** trigger blocks UPDATE and young DELETE on `system_audit_entries`
  (classifying to `ErrAppendOnlyViolation`); end-to-end `PruneSystemAuditEntries` deletes a
  backdated row through the installed trigger; rows younger than retention survive.
- **Oracle:** `make test-integration-oci`; **mandatory `oracle-db-admin` review** of the third
  trigger before completion.

## Implementation shape

Small delta on top of #453: trigger file + floor config extended, one new prune method, one
new env var, one pruner call. No model/schema change (columns and indexes already exist), no
OpenAPI change, no dbtool impact.
