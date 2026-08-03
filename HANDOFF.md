# Handoff — 1.7.2 shipped (CATS triage + #662/#665), 2026-08-03

## Where things are

**1.7.2 is merged.** PR #677 squash-merged to `main` (commit `5e107bce`). **#662 and
#665 are CLOSED.** All 12 CI checks were green; locally the branch also passed build,
unit (2460+), lint (0), PG integration (84/0), and a clean security review.

What shipped in 1.7.2 (version bumped 1.7.1 → 1.7.2):

1. **#665 — `dberrors.ErrTransient` → 503.** `StoreErrorToRequestError` maps a transient
   DB fault to 503 with `Retry-After` (was 500), via `errors.Is` so it survives the retry
   helper's `%w` wrapper. `HandleRequestError` emits `Retry-After` for any 503 (default
   30s). `ListMyIdentities`/`DeleteMyIdentity` now render through `HandleRequestError`
   (they rendered manually and lacked the header + sanitization). Non-transient errors
   unaffected.

2. **#662 — public-path lint.** `isPublicPath()` is the middleware's single source of
   truth and `cmd/server/publicpath_lint_test.go` calls the same function, comparing every
   spec operation's auth marking (`security: []` ≡ `x-public-endpoint: true`) against it,
   plus a prefix-over-reach check. Divergences resolved: `x-public-endpoint: true` on
   `POST /oauth2/revoke`; `/oauth2/token` prefix → exact (narrows the unauthenticated
   surface); new **`x-auth-in-handler: true`** vendor extension on
   `GET /webhook-deliveries/{delivery_id}` (dual HMAC-or-JWT, auth in handler), honored by
   the lint.

3. **CATS 1.7.1 triage + FP rules.** Valid run `20260802T184152Z` (107,570 requests, 0
   transport / 0.08% unauth). **Zero 500s**; #651 confirmed fixed. 19 per-fuzzer FP rules
   added (70 → 89), true positives 138 → 1. `rule-baseline.json` regenerated from the valid
   run; all 89 `# rule N of 89:` comments renumbered.

## Open follow-ups

1. **Re-run CATS against the deployed 1.7.2.** The 1.7.1 cluster was still what got fuzzed
   this cycle (for the baseline corpus), so the #662/#665 changes were NOT exercised by a
   campaign. Redeploy 1.7.2 to the cluster (`make dev-restart CLUSTER=k3s`), confirm the
   root endpoint reports `1.7.2-development`, then fuzz `http://rp2:30080` directly (never a
   port-forward).

2. **#674 (Backlog)** — schema-composition hygiene: `additionalProperties: false` under
   `allOf` (`RepositoryBase`, `MinimalNode/Edge`) + `oneOf`/discriminator (`MinimalCell`)
   trip CATS' networknt validator, yielding 6 "schema mismatch" false positives even though
   the bodies conform. Currently suppressed by `RESPONSE_SCHEMA_COMPOSITION_FP_674`.
   Evaluate restructuring the schemas as deliberate future work.

3. **The one surfaced CATS true positive** (left unsuppressed on purpose): `POST
   /threat_models/{id}/chat/sessions` (SSE) is missing recommended security headers — the
   streaming response likely bypasses the header middleware. Decide: add the headers to the
   SSE path, or accept as a documented low-risk exception.

4. **CATS `webhook_id` fixture-decoy gap** (test-infra, not a TMI bug). The retained
   baseline run went invalid because the decoy for
   `/admin/webhooks/subscriptions/{webhook_id}` isn't working — the decoy override is
   present but the throwaway isn't seeded / the per-path section names the wrong parameter.
   A `.local/cats` seed/config fix; worth filing if it recurs.

## Gotchas carried forward

- **git push needs a physical SSH-key touch** on this machine; retry when the user is
  present, never work around it.
- **Never run two integration suites concurrently** (shared server/DB).
- **`rule-baseline.json` can be regenerated from any *valid* parsed DB** — it is derived
  from the DB records, not the raw report, so a retained raw report is not required. Only
  use a DB whose run passed all three validity gates (transport, credential, fixture).
- **Versioning is manual (#627):** keep `.version`, spec `info.version`, and the build in
  step; `feat:` → MINOR, else PATCH. 1.7.2 was a PATCH bump.
