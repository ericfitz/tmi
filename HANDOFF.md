# Handoff — 1.7.2 CATS triage + #662/#665, 2026-08-03

## What this branch (`dev/1.7.2/cats-triage-662-665`) contains

Four bundled pieces, all local-validated (build, unit 2460+, lint 0, PG integration
84/0, security-review clean). Version bumped **1.7.1 → 1.7.2** (`.version` + spec
`info.version`, verified by `make build-server`).

1. **CATS re-run against 1.7.1 + triage (no code, test data only).** Ran a full valid
   campaign (`20260802T184152Z`, 107,570 requests, 0 transport / 0.08% unauth, all 6
   preserved anchors survived). **Zero 500s.** #651 confirmed fixed
   (`PATCH`/`PUT /threat_models/{id}` now record 2xx). Every true positive verified benign.
   Added **19 false-positive rules** to `test/cats/false-positives.yaml` (70 → 89), split
   per-fuzzer, each dry-run-verified with **no 5xx suppression**; true positives 138 → 1.
   Regenerated `test/cats/rule-baseline.json` from the valid run and renumbered all 89
   `# rule N of 89:` comments. Doc counts updated (README, CLAUDE.md).

   The **one remaining true positive** is a minor real observation, left surfaced (not
   suppressed): `POST /threat_models/{id}/chat/sessions` (SSE) is missing recommended
   security headers — its streaming response likely bypasses the header middleware.

2. **#665 — `dberrors.ErrTransient` → 503 (fix).** `StoreErrorToRequestError` now maps a
   transient DB fault to 503 with `Retry-After` (was 500), via `errors.Is` so it survives
   the retry helper's `%w` wrapper. `HandleRequestError` emits `Retry-After` for any 503
   (default 30s). Also routed `ListMyIdentities`/`DeleteMyIdentity` through
   `HandleRequestError` (they rendered errors manually and so lacked the header +
   sanitization). Tests updated/renamed to the 503 contract.

3. **#662 — public-path lint (chore).** Extracted the middleware's public decision into
   `isPublicPath()` (same-package, so `TestPublicPathLint` calls the exact function the
   server uses). New `cmd/server/publicpath_lint_test.go` compares every spec operation's
   auth marking (`security: []` ≡ `x-public-endpoint: true`) against `isPublicPath`, plus a
   prefix-over-reach check. Divergences resolved: added `x-public-endpoint: true` to
   `POST /oauth2/revoke`; converted `/oauth2/token` from a prefix to an exact entry (narrows
   the unauthenticated surface); introduced a new **`x-auth-in-handler: true`** vendor
   extension on `GET /webhook-deliveries/{delivery_id}` (dual HMAC-or-JWT, auth in handler)
   and taught the lint to honor it; fixed the false comment at the old `main.go:154`.

4. **Docs (pre-existing, carried from last session):**
   `docs/superpowers/plans/2026-08-01-subresource-tenancy.md` (the #664 plan) and this
   HANDOFF.

## Issues

- **#674** (filed this session, Backlog) — schema-composition hygiene: `additionalProperties:
  false` under `allOf` (`RepositoryBase`, `MinimalNode/Edge`) + `oneOf`/discriminator
  (`MinimalCell`) trip CATS' networknt validator, producing 6 "schema mismatch" false
  positives even though the response bodies conform. Currently suppressed by the FP rule
  `RESPONSE_SCHEMA_COMPOSITION_FP_674`. Evaluate restructuring the schemas later.
- **#662, #665** — resolved on this branch; close after the PR merges (squash-merge from a
  branch does not auto-close; comment + `gh issue close`).

## DO NEXT

1. **`git push` needs a physical SSH-key touch** on this machine — that is expected, not a
   repo problem. Push `dev/1.7.2/cats-triage-662-665`, open the PR, let CI run.
2. **Re-run CATS against the deployed 1.7.2** once it is redeployed to the cluster — the
   campaign this session fuzzed the still-1.7.1 cluster (for the baseline corpus), so it did
   NOT exercise the #662/#665 changes. Fuzz `http://rp2:30080` directly, never a
   port-forward.
3. Consider filing a CATS test-infra follow-up: the retained baseline run went **invalid**
   because the `webhook_id` fixture decoy for `/admin/webhooks/subscriptions/{webhook_id}`
   isn't working (the decoy override is present but the throwaway isn't seeded / the per-path
   section names the wrong parameter). Not a TMI bug; a `.local/cats` seed/config gap.

## Gotchas carried forward

- **git push needs a physical touch** on the SSH key; retry when the user is present, never
  work around it.
- **Never run two integration suites concurrently** (shared server/DB).
- **CATS baseline can be regenerated from any *valid* parsed DB** (`rule-baseline.json` is
  derived from the DB records, not the raw report) — a retained raw report is not required.
  Use `latest.db` only if the run it points at passed all three validity gates.
