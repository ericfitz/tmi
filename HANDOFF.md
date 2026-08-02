# Handoff — 1.7.1 sub-resource tenancy (#664), 2026-08-01

## Where things are

**PR #672 is open** (`dev/1.7.1/subresource-tenancy` → `main`, version 1.7.1) and CI is
running. The user chose **merge-after-CI**: squash-merge #672 the moment CI is green — the
Oracle-verification gap below is a documented follow-up (#671), not a merge blocker. After
merge, verify #664 auto-closed (squash-merge reads the PR body, which carries `Closes #664`).

```
gh pr checks 672            # confirm green
gh pr merge 672 --squash --delete-branch
gh issue view 664 --json state -q .state   # expect CLOSED
```

If CI fails, read the failing job; nothing on this branch is expected to fail (lint 0,
test-unit 2458, PG integration full suite + new tenancy tests all passed locally).

## What #672 does (the whole review process ran; it's solid)

Two-layer fix for #664 (a caller with any role on threat model A could read/update/patch/
delete — and via bulk, re-parent/overwrite — children of threat model B by naming B's child
id under A's path):

1. **Centralized parentage gate** (`api/authz_middleware.go`, `enforceChildParentage` +
   `gormChildParentageCheck`, called from `authzMiddlewareWithTable`). After the parent-ACL
   check, verifies the child id belongs to `{threat_model_id}` for all six families
   (threats, assets, documents, notes, repositories, diagrams) and every child sub-route
   (metadata, metadata/{key}, audit_trail, restore, request_access, collaborate). **404 not
   403** (no existence oracle). DB probe wrapped in `WithRetryableGormRead` + `dberrors.Classify`.
   `/restore` and `/audit_trail` are exempt from the `deleted_at IS NULL` filter so
   delete-recovery and audit history of soft-deleted children stay reachable.
2. **Threat bulk store scoping** (`api/threat_store_gorm.go`). The gate's non-UUID skip
   branch (`/threats/bulk`) left a **HIGH IDOR the security review caught**: threat
   `Update`/`BulkUpdate`/`Patch` predicated on `id` alone and `buildThreatUpdateMap` wrote
   `threat_model_id`, so `PUT /threat_models/{A}/threats/bulk` naming B's threat id
   re-parented it into A; `PATCH` overwrote in place. All three now scope by
   `threat_model_id`, `Patch` takes the parent id (interface + mock + both call sites
   updated), and the update map no longer writes `threat_model_id`. The metadata save is
   gated on `RowsAffected` to close a **second-order metadata IDOR** (metadata keyed on
   entity_id alone) caught by oracle-db-admin. Sibling families were already parent-scoped;
   threats were the sole outlier and the only family with a bulk-PATCH route.

New tests: `test/integration/workflows/subresource_tenancy_test.go`
(`TestSubResourceTenancy_Integration`) — cross-parent 404 across six families and verbs,
victim child intact, plus `PutBulkCannotReparent`/`PatchBulkCannotOverwrite`; unit coverage
in `api/authz_middleware_subresource_test.go`. All fail on revert of either layer.

Verified: lint 0 · test-unit 2458 · PG integration full suite + tenancy tests · oracle-db-admin
APPROVED WITH NOTES (its two blocking findings both fixed) · whole-branch review ready-to-merge.

## The one caveat (accepted by the user)

The new bulk-scoping subtests are **PostgreSQL-verified, not Oracle-executed**. The metadata
gate depends on `RowsAffected == 0` accuracy on ADB. This is blocked by **pre-existing OCI
infrastructure**, not by the change — see #671. oracle-db-admin reviewed the code. When #671
is fixed, run the tenancy subtests on ADB to close this out.

## Issues filed this session
- **#669** — `Threat.DeletedAt` is not `gorm.DeletedAt`; raw predicates on these models must
  spell out `deleted_at IS NULL` (audit the models or migrate the type).
- **#670** — `deleteAndSaveEntityMetadata` can commit a bare metadata delete if the insert
  half errors; more reachable on Oracle BYTE length semantics.
- **#671** — `make test-integration-oci`: an unrelated oracle test
  (`TestThreatModelAliasSequenceOracleIntegration`) hangs the OCI run for 10m via the
  fsnotify file-watchdog, AND `run_oci` never runs the `test/integration/workflows` package,
  so no workflow-level test is ever Oracle-executed. Both should be fixed so security/driver
  changes can be pinned on ADB.

## Closed this session
- **PR #668** merged → #651, #653, #658, #659, #660 closed (1.7.0 CATS follow-ups).
- **#664** — closing via #672 (merge pending CI).

## DO NEXT (after #672 merges), from the original session backlog

1. **Re-run CATS against 1.7.1.** Four 1.7.0 changes plus this branch should move the numbers.
   Check specifically that `PATCH`/`PUT /threat_models/{id}` now record a 2xx (#651
   `x-preserve-fixture`) — verification query in `test/cats/README.md`. Fuzz the cluster
   directly at `http://rp2:30080`, never through a port-forward.
2. **#662** — spec-vs-middleware public-path lint. Two real divergences documented in the
   issue: `POST /oauth2/revoke` (`security: []` but no `x-public-endpoint`) and
   `GET /webhook-deliveries/{delivery_id}` (dual-auth HMAC-or-JWT in the handler; the comment
   at `cmd/server/main.go:154` claiming `x-public-endpoint` is false).
3. **#665** — `ErrTransient` → 503. Cheap now: 503 is documented on all operations.

## Gotchas carried forward
- **git push needs a physical touch** on the SSH key (`sign_and_send_pubkey ... signing failed`
  is the expected touch-required behavior on this machine, not a repo problem). Retry when the
  user is present; never work around it.
- **Never run two integration suites concurrently** — shared server/DB; the second run's
  teardown kills the first's and produces a wall of `connection refused`.
- **`make test-integration-oci` currently can't complete** (see #671) — don't trust an OCI
  integration gate until #671 lands.

## SDD workspace (delete after #672 merges + is confirmed)
`.superpowers/sdd/2026-08-01-subresource-tenancy/` holds the ledger (`progress.md`), per-task
briefs/reports, review packages, and the PR body. The plan is
`docs/superpowers/plans/2026-08-01-subresource-tenancy.md`.
