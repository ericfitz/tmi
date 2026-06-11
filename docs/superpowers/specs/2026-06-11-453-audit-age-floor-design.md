# Design: Age-floored append-only audit triggers (#453)

**Issue:** [#453](https://github.com/ericfitz/tmi/issues/453) — bug(db): audit prune/delete paths are blocked on Oracle by append-only triggers
**Date:** 2026-06-11
**Status:** Approved

## Problem

The T19 append-only triggers (`tmi_audit_entries_no_mutate`, `tmi_version_snapshots_no_mutate`,
installed by `internal/dbschema/audit_append_only.go`) block **every** UPDATE/DELETE on
`audit_entries` and `version_snapshots` — on **both** PostgreSQL (P0001) and Oracle (ORA-20001).
The issue was filed as Oracle-only; investigation shows the PG trigger has identical semantics.

This blocks the legitimate retention paths in `api/audit_store.go`:

- `PruneAuditEntries` — deletes entries older than `AUDIT_RETENTION_DAYS` (default 365)
- `PruneVersionSnapshots` / `pruneObjectVersions` / `executePrune` — deletes snapshots older
  than `VERSION_RETENTION_DAYS` (default 90; count boundary is intersected with the time
  boundary, so count-based pruning never targets younger rows)
- `executePrune` additionally **UPDATEs** `audit_entries.version` to NULL for pruned snapshots

Nobody noticed because retention defaults are long, dev databases are young, and the daily
`AuditPruner` logs failures non-fatally. The documented escape hatch (manual
`ALTER TABLE ... DISABLE TRIGGER` by an `audit_admin` role) was never implemented or wired to
anything.

## Decisions

1. **Aged-out pruning is a supported, in-app operation.** The trigger itself enforces a minimum
   row age for DELETEs. UPDATEs remain blocked unconditionally, forever.
2. **Audit rows become fully immutable.** `executePrune` stops nulling `audit_entries.version`;
   the version number is historical fact. A missing snapshot means "content pruned" — the read
   path already returns `410 Gone` (`api/audit_handlers.go` RollbackToVersion).
3. **Age floors are config-derived, baked into the trigger SQL at boot.** Triggers are already
   re-created on every server start; the floor literal comes from the same configuration the
   pruner reads, so the two cannot drift.

**Security property preserved (T19):** there is no bypass flag or privileged session state — an
attacker holding the app's DB credentials still cannot delete or modify any row younger than the
floor, and cannot UPDATE any row ever. Trigger replacement requires DDL, which was always outside
T19's scope.

## Design

### 1. Trigger change — `internal/dbschema/audit_append_only.go`

A DELETE is allowed iff the row is older than its table's floor; everything else raises.

PostgreSQL (guard function):

```sql
IF TG_OP = 'DELETE' AND OLD.created_at < now() - make_interval(days => <floor>) THEN
  RETURN OLD;
END IF;
RAISE EXCEPTION '...' USING ERRCODE = 'P0001';
```

Oracle (per-table trigger):

```sql
IF DELETING AND :OLD.created_at < SYS_EXTRACT_UTC(SYSTIMESTAMP) - <floor> THEN
  NULL;  -- permit
ELSE
  RAISE_APPLICATION_ERROR(-20001, '...');
END IF;
```

`created_at` values are written in UTC; the Oracle comparison must be UTC-correct
(`SYS_EXTRACT_UTC` vs column type semantics to be confirmed by the mandatory oracle-db-admin
review).

Floor values:

| Table | Floor source | Default config | Installed floor (default) |
|---|---|---|---|
| `audit_entries` | `AUDIT_RETENTION_DAYS` | 365 | 364 |
| `version_snapshots` | `min(VERSION_RETENTION_DAYS, TOMBSTONE_RETENTION_DAYS)` | min(90, 30) = 30 | 29 |

The `version_snapshots` floor must not exceed the tombstone retention because `PurgeTombstones`
(`api/audit_store.go`) legitimately deletes snapshots of sub-resources purged
`TOMBSTONE_RETENTION_DAYS` (30) days after soft-deletion; mutations stop at soft-delete, so
those snapshots are at least 30 days old but can be far younger than `VERSION_RETENTION_DAYS`.
Snapshots are rollback payloads, not the tamper-evident record — `audit_entries` carries the
evidence and keeps the long floor.

Two guards on the configured value:

- **Hard minimum.** 30 days for `audit_entries`, 7 days for `version_snapshots`. If configured
  retention is lower, install the minimum floor and log a warning. Misconfiguration cannot gut
  tamper resistance.
- **Skew margin: 1 day.** Installed floor = configured days − 1, so app-clock vs DB-clock
  disagreement at the cutoff boundary cannot block a legitimate prune.

`InstallAuditAppendOnlyTriggers` gains the two floor parameters; `cmd/server/main.go` passes
them from the same config source `NewGormAuditService` reads. On PG, the single guard function
takes the floor as a trigger argument (`TG_ARGV[0]`, days as integer), so each table's
`CREATE TRIGGER` passes its own floor. The stale "operator escape hatch / DISABLE TRIGGER is
the only supported path" comment is rewritten to describe the age-floor policy.

### 2. Prune-path changes — `api/audit_store.go`, `api/audit_pruner.go`

- Delete the `Update("version", nil)` block in `executePrune`. No replacement; readers already
  handle missing snapshots with 410.
- Remove `DeleteThreatModelAudit` from the `AuditService` interface and `GormAuditService`.
  It is called nowhere, and "delete all audit history for a TM regardless of age" is
  incompatible with the age-floor policy by construction.
- In the pruner, detect `dberrors.ErrAppendOnlyViolation` and log a loud, actionable error
  naming the probable floor/retention mismatch (e.g., retention lowered without server restart),
  instead of a generic prune failure.

### 3. Out of scope

- `system_audit_entries` has **no** trigger protection today; adding it plus its retention is
  #400's design, which reuses this age-floor pattern.
- Partitioning (`DROP PARTITION`-style pruning) is considered in #400; unnecessary for these
  two tables at current write rates.
- Pre-existing orphan leak: the threat-model hard-delete cascade (`api/tombstone_store.go`)
  never deletes the children's `version_snapshots`, so they accumulate forever; only the
  sub-resource purge path cleans up. File a follow-up issue; not fixed here.

## Testing

- **Integration (PG):** prune of aged rows succeeds end-to-end; DELETE younger than floor is
  blocked; any UPDATE is blocked; pruner logs the actionable error on violation; rollback of an
  entry whose snapshot was pruned returns 410; floor clamps to 30 when retention is configured
  lower.
- **Oracle (`make test-integration-oci`):** same suite; trigger SQL verified on a real ADB
  connection (acceptance criterion from the issue).
- **Mandatory:** `oracle-db-admin` subagent review of the trigger SQL and UTC comparison
  semantics before completion.

## Implementation shape

Single focused change: trigger SQL + install signature, two deletions in the store, pruner
error handling, tests. No OpenAPI change. No new config keys (reuses `AUDIT_RETENTION_DAYS`,
`VERSION_RETENTION_DAYS`). Check `cmd/dbtool` for trigger awareness; no column/index changes.
