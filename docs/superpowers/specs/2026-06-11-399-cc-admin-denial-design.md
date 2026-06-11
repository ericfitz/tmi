# Design: Client-credentials tokens on /admin/* — verify, pin, document (#399)

**Issue:** [#399](https://github.com/ericfitz/tmi/issues/399) — investigate: step-up auth mechanism for client credentials grants on /admin/* writes
**Date:** 2026-06-11
**Status:** Approved

## Investigation conclusion

The threat in the issue body — a stolen client-credentials (CC) secret reaching every
`/admin/*` route with `auth_time=now` defeating step-up — **is already mitigated**, by work
that landed after #399 was filed. Three independent layers deny CC tokens on admin routes:

1. **Route authorization (decisive).** Every `/admin/*` operation in the OpenAPI spec declares
   `roles: ["admin"]` in `x-tmi-authz` (verified across all 66 admin operations: zero
   omissions). The authz table routes those through `RequireAdministrator`
   (`api/auth_helpers.go:47-58`), which categorically 403s any service-account token
   (subject `sa:*`, detected via the `isServiceAccount` context key set by JWT middleware,
   `cmd/server/jwt_auth.go`) with "Administrative operations require interactive
   authentication" — before the handler and before step-up middleware runs.
2. **Claims.** CC token minting (`auth/service.go`, client-credentials grant) sets
   `tmi_is_administrator=false` and filters the `administrators` group out of the merged
   groups claim.
3. **Step-up moot.** The `auth_time=now` weakness called out in the issue cannot be exercised
   on `/admin/*`: middleware order is JWT → Authz → StepUp, and Authz denies the service
   account first. Step-up only gates `/admin/*`, so `auth_time=now` for CC tokens is harmless
   everywhere it can reach.

A unit test already pins the helper behavior
(`api/authorization_middleware_test.go:468` — "returns 403 for service account even if
admin"). There is **no CC usage of `/admin/*` anywhere** in production code, tests, scripts,
or docs.

## Decisions

1. **Keep the blanket denial on all `/admin/*`** — reads included — which is stricter than
   the issue comment's `/admin/settings/*`-writes ask. No read-only data relocation (YAGNI:
   no machine consumer exists). If one appears (e.g., monitoring wants
   `GET /admin/timmy/status`), relocate that data to a non-`/admin` endpoint in its own
   issue.
2. **No mechanism changes.** No new middleware, no OpenAPI change, no change to `auth_time`
   semantics for CC tokens.
3. **Make the invariant durable and fix stale docs** (the remaining work, below).

## Work

### 1. Spec invariant test (new)

A unit test against the **embedded production** OpenAPI spec (`GetSwagger()` /
`LoadGlobalAuthzTable()`) asserting every `/admin/*` operation carries the `admin` role in
its authz rule. Today `api/authz_table_test.go` only tests synthetic spec JSON; a future
`/admin/foo` endpoint that forgets `x-tmi-authz.roles: ["admin"]` would silently bypass the
CC denial. This test turns that mistake into a build failure.

### 2. Integration test (extend)

End-to-end in `test/integration/workflows/client_credentials_test.go`: mint a real
`tmi_cc_*` credential, exchange via `POST /oauth2/token`, then call:

- all five `/admin/settings/*` operations (GET list, GET key, PUT key, DELETE key,
  POST reencrypt), and
- one representative write per remaining `/admin` sub-area (users, groups, quotas, webhooks,
  surveys),

asserting **403** with the "interactive authentication" denial (not 401, not 404) for every
call, even when the credential's owner is an administrator (use `charlie`).

### 3. Documentation corrections

- `CLAUDE.md` (Client Credentials section): "full API access as creating user" → "full API
  access as the creating user, **except `/admin/*`** — service-account tokens are
  categorically denied on admin routes; administrative operations require interactive
  authentication".
- `auth/service.go` comment on the CC `auth_time` claim ("the long-term mechanism for CC
  step-up is tracked in #399") → rewritten: CC tokens are categorically denied on `/admin/*`
  by `RequireAdministrator`, so `auth_time=now` is harmless; see #399 for the analysis.
- Wiki client-credentials page: document the `/admin/*` exclusion.

### 4. Close #399

Closing comment = the investigation summary above (the issue asked for an investigation; the
mechanism question is answered "already enforced at the authz layer; now pinned by tests").

## Testing

`make test-unit` (new invariant test), `make test-integration` (extended CC workflow test).
No DB-touching change → no oracle-db-admin dispatch. No OpenAPI change → no regeneration.

## Implementation shape

Two test files touched/added, two comment/doc edits, one wiki edit, issue close. No
production behavior change.
