# Handoff — 1.7.2 CATS re-run clean; 1.7.3 fixes the SSE Cache-Control TP, 2026-08-03

## Where things are

**1.7.2 shipped** (PR #677, commit `5e107bce`; #662/#665 closed). This session
redeployed 1.7.2 to the k3s cluster and ran the CATS campaign the previous handoff
asked for, then fixed the one true positive it surfaced.

### CATS re-run against deployed 1.7.2 — DONE, clean

Redeployed 1.7.2 to k3s (`make dev-restart CLUSTER=k3s`; root endpoint confirmed
`1.7.2-development`) and fuzzed `http://rp2:30080` directly.

- Run `20260803T162441Z`, 107,554 requests.
- **All three validity gates PASSED**: transport 0.07% (≪1%), non-FP 401 0.08% (≪5%),
  all 6 seeded fixtures survived. `latest.db` updated.
- **Zero 500s** across the whole run (verified via `response_code_stats_view`; highest
  5xx is 501, all documented/`success`). Zero-500 policy holds.
- **1 true positive**, exactly the one the prior handoff pre-flagged (below).

### The true positive — FIXED on branch `fix/sse-security-headers-cache-control` (1.7.3)

`CheckSecurityHeaders` on `POST /threat_models/{id}/chat/sessions` (SSE). Root cause was
**not** a middleware bypass (the prior handoff's guess): the recorded 200 response carried
every OWASP header — CSP, X-Frame-Options, X-Content-Type-Options, X-XSS-Protection — and
a Cache-Control. The single defect: `NewSSEWriter` (`api/timmy_sse.go`) overwrote the
`SecurityHeaders` middleware's `no-store, no-cache, must-revalidate` with a bare
`no-cache`, dropping `no-store`, which CATS requires for sensitive responses.

Fix: removed the override so the middleware's stronger value stands (no-store correct for
a sensitive stream; no-cache already included keeps SSE uncached). Added regression test
`TestSSE_PreservesStrongCacheControl`. Version bumped 1.7.2 → 1.7.3 (`.version` + spec
`info.version`); `api.go` regenerated (embedded-spec re-encode only, verified zero
Go-code change).

**Verified against the cluster**: redeployed the fix, targeted re-fuzz
(`cats_tool.py run --path '/threat_models/{threat_model_id}/chat/sessions'`, run
`20260803T171451Z`) → `CheckSecurityHeaders` = **success**, Cache-Control now
`no-store, no-cache, must-revalidate`, **zero true positives** on the path.

Gates run: `make lint` (0), full `make test-unit` (2463 passed), build @1.7.3,
`make validate-openapi` (0 errors), security-review (clean), SEM markers refreshed.

## NEEDS THE USER

- **Push is blocked on the hardware SSH-key touch.** Commit `99100b1e` sits on branch
  `fix/sse-security-headers-cache-control` (5 files + this handoff). Push it and open a
  PR when you're at the machine:
  `git push -u origin fix/sse-security-headers-cache-control` then `gh pr create`.
  It's a security fix → per CLAUDE.md it may go direct to main, but the repo is PR-only
  in practice; a squash-merge PR keeps 1.7.3 as the conventional-commit subject.

## Open follow-ups (unchanged from before, still deferred)

1. **#674 (Backlog)** — schema-composition hygiene: `additionalProperties: false` under
   `allOf` + `oneOf`/discriminator trips CATS' networknt validator (6 suppressed
   `RESPONSE_SCHEMA_COMPOSITION_FP_674` "schema mismatch" FPs). Evaluate restructuring.

2. **CATS `webhook_id` fixture-decoy gap** (test-infra, not a TMI bug). The decoy for
   `/admin/webhooks/subscriptions/{webhook_id}` isn't seeded / names the wrong parameter.
   A `.local/cats` seed/config fix; file if it recurs.

## Gotchas carried forward

- **git push needs a physical SSH-key touch** on this machine; retry when the user is
  present, never work around it.
- **Never run two integration suites concurrently** (shared server/DB).
- **`rule-baseline.json` can be regenerated from any *valid* parsed DB** — derived from
  the DB records, not the raw report.
- **Versioning is manual (#627):** keep `.version`, spec `info.version`, and the build in
  step; `feat:` → MINOR, else PATCH. 1.7.3 was a PATCH bump.
