# Handoff — 1.7.0 CATS follow-ups, 2026-08-01

## PUSH IS PENDING

Two commits are on `dev/1.7.0/cats-followup` and **not pushed**. `git push` failed with

```
sign_and_send_pubkey: signing failed for ED25519 "/Users/efitz/.ssh/github-ericfitz" from agent
git@github.com: Permission denied (publickey).
```

which is the expected touch-required behaviour, not a repo problem. Re-run:

```
git push -u origin dev/1.7.0/cats-followup
```

then open a PR. `Closes #651 #658 #659 #660` are in the first commit body and `Closes #653` in the
second, but they only auto-close from `main` — with squash-merge the PR body is what counts, so
repeat them there.

## What landed

`main` was at 1.6.5; this branch is **1.7.0** (`.version` and `info.version` both bumped by hand —
versioning is still manual, #627). MINOR rather than PATCH because `Project.team`'s generated type
changes.

| # | Change |
|---|---|
| #659 | `TeamSummary` schema; `Project.team` narrowed to `{id, name, description}` |
| #659 | Response examples completed: **611 baselined gaps → 96**, 93 → 13 operations |
| #658 | `DELETE .../threats/bulk` reads the query parameter the spec declares, atomically and scoped to its parent |
| #660 | Redis blacklist failures retry, then answer 503 + `Retry-After` instead of 500 |
| #651 | `x-preserve-fixture` keeps anchor decoys alive through their own campaign |
| — | Bulk handler limits moved to `api/bulk_limits.go` and aligned to the spec's `maxItems` |
| #653 | `TestIdentityLink` suffix entropy (separate commit) |

### Verification

`make validate-openapi` (0 errors) · `make generate-api` (v2.7.1) · `make build-server` ·
`make lint` (0 issues) · `make test-unit` (**2457 passed**) · `make test-dev-scripts` (**177
passed**) · `make test-integration` (**83 passed, 0 failed**) · `oracle-db-admin` **APPROVED WITH
NOTES** · security review **no HIGH or MEDIUM findings**.

## Three things worth not re-deriving

**The previous session's #658 triage was half wrong.** It recorded all 10 findings as a CATS defect
and said "do not re-triage these". `ThreatIdsQueryParam` declares `explode: false`, so the
comma-joined value CATS sent was *correct* and the 3 `threats/bulk` findings were our bug:
`BulkDeleteThreats` read `threat_ids` from a JSON body while the spec declares a required query
parameter and no body. **Every spec-conforming bulk delete had been failing with 400** — the
endpoint had never once served a successful call, including from tmi-ux, which generates its types
from the spec. The other 7 findings (`explode: true` params) are the genuine CATS defect, now
Endava/cats#207.

**The Oracle review caught two blocking issues in that fix, both real.** The first implementation
looped `Delete` per id: each committed separately, so an abort partway through left the batch
applied to an arbitrary prefix that could never be retried (the survivors no longer satisfy
`deleted_at IS NULL`, so a retry 404s forever), and on ADB a mid-batch abort is routine —
`ORA-08177` under SERIALIZABLE, `ORA-00060`, autoscale connection drops. Worse, it matched on threat
id alone, so a caller with writer access to one threat model could delete threats out of another.
`ThreatRepository.BulkSoftDelete` now does one `UPDATE` predicated on `threat_model_id` in one
transaction and rolls the whole batch back unless every id matches. The re-review confirmed the SQL
by dry-running GORM's statement builder.

**The residual 96 example gaps are unsatisfiable, not unexercised.** 90 sit under a `oneOf`/`anyOf`
union — the checker unions every branch, so one example cell would have to carry both node-only
(`position`, `size`, `ports`) and edge-only (`source`, `target`, `vertices`) properties, producing a
document that satisfies CATS and describes nothing real. 5 are free-form objects with no declared
properties. 1 is `subscriptions.secret`, declared on the read schema but never emitted
(`webhook_handlers.go` passes `includeSecret=false` on both GET paths). Do not "finish" these.

## DO NEXT

1. **Push, PR, merge.** Nothing else is blocked on it.
2. **#664 — cross-tenant sub-resource access.** The one finding here that is a live security bug.
   Authorization is granted against `{threat_model_id}`, but **no** single-item sub-resource handler
   verifies the child belongs to that parent — 20 handlers across threat, document, asset,
   repository and note. `authz_middleware_subresource_test.go:197` says so in a comment
   (`// child IDs aren't checked by middleware`). The bulk-delete path is fixed; the rest is not.
   Prefer centralized enforcement, and answer 404 rather than 403 so it does not become an existence
   oracle.
3. **Re-run CATS** against 1.7.0. Four of this branch's changes should move the numbers, and #651 is
   the interesting one: check that `PATCH`/`PUT /threat_models/{id}` finally record a 2xx. The
   verification query is in `test/cats/README.md`.
4. **#662** — the spec-vs-middleware lint. The previous handoff listed this as "believed correct,
   unverified"; it is **not** correct. Two real divergences, both middleware-more-permissive:
   `POST /oauth2/revoke` (fine in substance, `security: []` but no `x-public-endpoint` — the two
   spec signals disagree with each other) and `GET /webhook-deliveries/{delivery_id}` (dual-auth
   HMAC-or-JWT enforced in the handler; the comment at `cmd/server/main.go:154` claiming it is
   `x-public-endpoint` in OpenAPI is simply false).
5. **#665** (`ErrTransient` → 503) is now cheap, because 503 is documented on all 328 operations.

### Open issues

**Filed this session**: #662 (public-path lint) · #663 (bulk-limit drift guard) · #664 (cross-tenant
sub-resources, **security**) · #665 (`ErrTransient` → 503) · #666 (metadata bulk write
amplification) · #667 (CATS rule numbering: 70 rules, docs say 48) · Endava/cats#207 (upstream)

**Closed by this branch**: #651 · #653 · #658 · #659 · #660

**Older, unchanged**: #652 `chat/sessions` SSE error envelope · #642 test-artifact disk retention ·
#633 restore `/me` coverage · #634 rewrite `set-server-setting.py` · #631 k8s Secrets vs DB config ·
#608 CATS deletes its own fixtures · #596 CATS drops required nested array-item uuid fields · #627
automated versioning

### Still not filed

- **Bulk-limit spec/code mismatch is resolved, but the caps themselves were never designed.** The
  spec caps metadata bulk at 20 for the threat-model subtree and 100 everywhere else, while every
  entity schema allows `maxItems: 100` on its own `metadata`. The handler now mirrors that split
  faithfully, but nobody has decided whether the split is intended.

## Gotcha for the next session

**Never run two integration suites at once.** Both use the same test server and database; the
second run's teardown kills the first's server, and you get a wall of `connection refused` that
looks like 38 real failures. One run cost ~10 minutes to re-do for exactly this reason.

## Reproducing the analysis

```
uv run scripts/check-response-examples.py                    # what is missing
uv run scripts/check-response-examples.py --update-baseline  # after fixing gaps
```

The example-completion pass was a one-off script, not committed. It walked the baseline, synthesised
a schema-valid value per gap, and refused any gap under a union, any free-form object, and
`subscriptions.secret`. Re-deriving it is not necessary — the remaining 96 are the ones it correctly
declined.
