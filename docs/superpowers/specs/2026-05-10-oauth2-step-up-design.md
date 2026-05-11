# `/oauth2/step_up` — Guaranteed-fresh re-authentication for admin step-up (#397)

**Status:** Approved
**Date:** 2026-05-10
**Issue:** [#397](https://github.com/ericfitz/tmi/issues/397)
**Parent:** [#355](https://github.com/ericfitz/tmi/issues/355) (T7 step-up + in-band admin audit, Part 1 — closed)
**Threat reference:** T7 (Full-system compromise via admin-settings tampering) — `docs/THREAT_MODEL.md` §4

## Problem

#355 Part 1 implements step-up authentication via a fresh OAuth round-trip through the user's primary IdP (`/oauth2/authorize` again). The middleware checks an `auth_time` claim and returns 401 + `WWW-Authenticate` if stale. The client re-runs the existing OAuth authorize flow; if the user has a warm IdP session, the IdP silently re-mints without prompting the human.

This silent re-mint defeats the purpose of step-up: it degrades to "you have an active IdP session, click through" — exactly the failure mode an insider attack exploits (the insider already has a warm session by definition).

The fix is a dedicated `/oauth2/step_up` endpoint that adds `prompt=login&max_age=0` to the upstream authorize URL (and `ForceAuthn=true` for SAML). For providers that do not honor those parameters (notably GitHub), step-up is documented as "weak" and the audit row records the strength so operators can see which step-up events did not have a guaranteed fresh prompt.

## Goals

1. Force a fresh interactive re-authentication at the upstream IdP for OIDC-capable providers (Google, Microsoft, generic OIDC) and for SAML providers.
2. For providers that ignore `prompt=login` / `max_age=0` (currently only GitHub), short-circuit the round-trip and rotate tokens in-place with a `weak` strength marker in the audit table.
3. Reject step-up for non-human callers (client-credentials grants) with a clear error.
4. Surface a structured audit signal — `step_up_complete`, `step_up_failed`, `step_up_rejected` — into the existing `system_audit_entries` table.
5. Defend against cookie-theft-induced step-up: the user who completes step-up at the IdP MUST match the user who initiated it, by internal UUID.

## Non-goals

- TOTP / WebAuthn / hardware-key step-up. Out of scope; fresh-OAuth re-auth was committed in #355.
- Step-up for client-credentials grants. Tracked in [#399](https://github.com/ericfitz/tmi/issues/399).
- Periodic forced fresh re-auth on normal logins (not step-up). The issue body raises this as a future call; out of scope here.
- Dual-admin approval queue. Tracked in #396.
- Out-of-band audit alert sink. Tracked in #395.
- New configuration knobs. Strength classification is hardcoded; no `auth.step_up_strict` option.
- DB schema changes. No new tables or columns; uses `system_audit_entries` as-is.

## Architecture overview

```
Browser                  TMI server                           Upstream IdP
   |                         |                                       |
   | GET /admin/X (stale)    |                                       |
   |------------------------>|                                       |
   |  401 WWW-Authenticate   |                                       |
   |<------------------------|                                       |
   | GET /oauth2/step_up?... |                                       |
   |   (cookie: jwt)         |                                       |
   |------------------------>|                                       |
   |       [STRONG path]     |                                       |
   |   302 Location: <idp>?prompt=login&max_age=0&...                |
   |<------------------------|                                       |
   |    follow redirect      |                                       |
   |---------------------------------------------------------->|     |
   |    fresh login prompt; user re-auths                      |     |
   |<----------------------------------------------------------|     |
   |   302 to /oauth2/callback?code=...&state=...                    |
   |--------------------------------->|                              |
   |   302 to client_callback?code=&state=                           |
   |<---------------------------------|                              |
   | POST /oauth2/token (PKCE verifier)                              |
   |----------------------->|                                        |
   |   identity-match check + audit + blacklist old refresh + rotate |
   |   200 {tokens}; Set-Cookie (HttpOnly; new access+refresh)       |
   |<-----------------------|
   | retry GET /admin/X                                              |
   |----------------------->|                                        |
   |   200 OK                                                        |
   |<-----------------------|
   |       [WEAK path — github]                                      |
   | GET /oauth2/step_up?...                                         |
   |------------------------>|                                       |
   |   (no upstream redirect)                                        |
   |   short-circuit: rotate tokens in-place + audit (strength=weak) |
   |   200 OK JSON + Set-Cookie (HttpOnly; new access+refresh)       |
   |<------------------------|
```

## Endpoint specification

### `GET /oauth2/step_up`

Public endpoint (no JWT middleware on the route; the handler reads the cookie itself, mirroring how `/oauth2/authorize` operates). Rate-limited via the existing middleware.

**Query parameters:**

| Name | Required | Notes |
|---|---|---|
| `client_callback` | yes | URL the browser ends at after the round-trip completes. MUST match `OAuth.ClientCallbackAllowList` (T16 / #231). |
| `code_challenge` | yes | PKCE S256 challenge. Caller-generated. Same format as `/oauth2/authorize`. |
| `code_challenge_method` | optional | Defaults to `S256`. Only `S256` accepted. |
| `state` | optional | Caller-supplied; auto-generated if absent. |

**Server-side flow:**

1. **Read JWT** using the same priority order as the existing JWT middleware in `cmd/server/jwt_auth.go` (Priority 1: `Authorization: Bearer`; Priority 2: HttpOnly cookie). Reuse the existing extraction helper rather than reimplementing. On absence/invalidity/expiry → `401 invalid_token` (no audit row; the caller isn't authenticated yet). The existing access-cookie is left in place until `SetTokenCookies` overwrites it at step-up completion.
2. **Client-credentials check.** If the JWT's `sub` claim has prefix `sa:` → `400 unsupported_grant_type`. Write `system_audit_entry` with `field_path = auth.step_up_rejected`, summary `step-up rejected: client credentials grant`. Per [#399](https://github.com/ericfitz/tmi/issues/399), CC step-up needs a different mechanism.
3. **Provider lookup.** Read `idp` claim. Look up the provider via `Handlers.getProvider(providerID)`. If not found / disabled / not-available-in-production (e.g., `tmi` in prod) → `400 invalid_provider`. Audit `step_up_rejected/invalid_provider`.
4. **Validate client_callback** against `OAuth.ClientCallbackAllowList`. On miss → `400 invalid_request`. (Existing pattern.)
5. **Validate PKCE** params (presence, S256 method, `ValidateCodeChallengeFormat`). On invalid → `400 invalid_request`.
6. **Classify strength** via `ClassifyStepUpStrength(provider, providerID)`. Returns `Strong` or `Weak`. See "Strength classification" below.
7. **Weak path (short-circuit).** See "Weak short-circuit" below.
8. **Strong path.**
   - Generate state (if not supplied).
   - Build state payload: `{provider, client_callback, step_up: "true", original_user_uuid, original_email, step_up_strength: "strong"}`.
   - Store at `oauth_state:<state>` in Redis (10-min TTL). Same key namespace as `/oauth2/authorize` so the existing `/oauth2/callback` machinery picks it up.
   - Store PKCE challenge via existing `stateStore.StorePKCEChallenge`.
   - Build upstream authorize URL via `BuildStepUpAuthorizationURL(provider, state)`:
     - **OAuth/OIDC providers:** call `provider.GetAuthorizationURL(state)`, then append `&prompt=login&max_age=0` (URL-aware append: parse, set, re-encode).
     - **SAML provider:** call `provider.GetAuthorizationURLForceAuthn(state)` (new method on the SAML provider) which sets `ForceAuthn="true"` on the AuthnRequest XML.
   - `302 Location: <upstream-authorize-url>`.

**Response status codes (with RFC-aligned error codes for OpenAPI completeness):**

| HTTP | Body / error code | When |
|---|---|---|
| 302 | (Location header) | Strong path success; redirect to upstream IdP |
| 200 | `{result, provider, auth_time, message}` | Weak path success; tokens rotated in-place |
| 400 | `invalid_request` | Missing/malformed query param, invalid PKCE, invalid client_callback |
| 400 | `invalid_provider` | JWT's idp is not configured or is disabled |
| 400 | `unsupported_grant_type` | CC-grant caller (`sub` starts with `sa:`) |
| 400 | `unsupported_response_type` | (Defensive; reserved.) |
| 400 | `invalid_scope` | (Defensive; we don't accept `scope` and reject if supplied with a value.) |
| 401 | `invalid_token` | Missing/invalid/expired JWT cookie/header |
| 403 | `unauthorized_client` | (Defensive; reserved for future per-grant restrictions.) |
| 404 | (HTML error page) | Path not matched / parser fallthrough — matches `/oauth2/authorize` behavior |
| 429 | `rate_limited` | Per-IP rate limit exceeded |
| 500 | `server_error` | Internal failure (Redis marshal, audit DB unreachable on rejection path, etc.) |
| 503 | `temporarily_unavailable` | Redis store fails; `Retry-After: 30` header (existing pattern) |

`access_denied` (RFC 6749 §4.1.2.1) is returned by the *upstream IdP* on the callback when the user cancels re-auth. It is documented on `/oauth2/callback`, not on `/oauth2/step_up`.

### Strength classification

Hardcoded table in `auth/provider_step_up.go`:

```
Strong: google, microsoft, tmi, any provider whose Config.Issuer != "" AND Config.JWKSURL != "" (i.e., generic OIDC), all SAML providers.
Weak:   github (and any pure-OAuth2 non-OIDC provider not on the strong list).
```

Default for unrecognized providers is `Strong` if they look OIDC (have Issuer + JWKSURL); otherwise `Weak`. A unit test pins each known provider ID's classification so a misconfiguration is caught at build time.

### Weak short-circuit

When the classifier returns `Weak`:

1. Skip the upstream redirect entirely.
2. Load the JWT's user from the DB by internal_uuid (existing repository call).
3. Mint a new TokenPair via `GenerateTokensWithUserInfo(ctx, user, nil)` — produces fresh `auth_time = now`.
4. Read the previous refresh token from the `tmi_refresh_token` cookie (if present) and blacklist it via the existing `token_blacklist.go` mechanism. If the cookie is absent (server-side caller, e.g., test harness using only the access token), skip blacklist with a debug log.
5. Set HttpOnly cookies with the new TokenPair (`SetTokenCookies(c, tokenPair, h.cookieOpts)`) — same attributes (HttpOnly, Secure, SameSite, Path, Domain) as the login path.
6. Write `system_audit_entry`: `field_path = auth.step_up_complete`, `new_value = {"provider": "<id>", "strength": "weak", "mode": "short_circuit"}`, `summary = step-up completed (weak) via <provider> — upstream IdP does not honor prompt=login`.
7. Respond `200 OK` with JSON:
   ```json
   {
     "result": "step_up_weak_complete",
     "provider": "github",
     "auth_time": 1715357321,
     "message": "Provider does not support guaranteed fresh re-auth; tokens rotated and step-up window reset. Audit log records this as a weak step-up."
   }
   ```

**Trade-off recorded:** the short-circuit means a weak-provider admin can refresh their step-up window without human action. T7 wants `prompt=login` to defeat warm-session insiders. For GitHub-bound admins, that defense doesn't exist with either the strong or short-circuit path — the IdP wouldn't prompt either way. The short-circuit is honest about that; the audit row is the residual signal. Operators who require strict step-up for GitHub admins should not bind GitHub as an admin IdP.

## Callback flow changes

### `/oauth2/callback` (existing handler, modified)

The callback handler today retrieves the state from Redis, deletes it, builds the PKCE-bound auth-code record at `pkce:<code>`, and redirects to `client_callback`.

Step-up extends this flow:

1. `parseCallbackState` reads the new keys from the state payload: `step_up`, `original_user_uuid`, `original_email`, `step_up_strength`.
2. The callback handles the upstream IdP's `error=access_denied` response (RFC 6749 §4.1.2.1) by checking for `error=` on the query string before processing the code. If present:
   - If state indicates step-up → write `system_audit_entry`: `auth.step_up_failed/access_denied`. Redirect to `client_callback` with `error=access_denied`. Caller (tmi-ux) surfaces "you cancelled re-auth".
   - If state does not indicate step-up → existing behavior (unchanged for normal logins).
3. When the callback builds the `pkce:<code>` record, it copies the step-up marker fields into the JSON payload alongside `code_challenge` and `code_challenge_method`. TTL stays 10 minutes.
4. Other than the state-payload pass-through, the callback's redirect behavior is unchanged. The identity-match check happens at token mint, not at callback, because the callback runs before user info is fetched.

### `/oauth2/token` (existing handler, modified)

After the existing PKCE-verifier check and `findOrCreateUser` call, branch on whether the `pkce:<code>` record carried `step_up: "true"`:

**Step-up branch:**

1. Look up the re-authed user's internal_uuid. Step-up MUST NOT create users — if the user matched is brand new (`findOrCreateUser` returned `userMatchNone` with a newly-created row), treat that as identity_mismatch. (In practice, a step-up callback for a user who doesn't exist in TMI is impossible because the JWT-holder existed by definition; this is a defensive check.)
2. If `re_authed.InternalUUID != stateData.original_user_uuid`:
   - Write `system_audit_entry`: `auth.step_up_failed/identity_mismatch`, `new_value = {"reason": "identity_mismatch", "attempted_email": "<sha256-prefix-8 + last-6 tail>"}`. Actor fields = the *original* user (the one who initiated step-up). Summary `step-up failed: identity_mismatch`.
   - Do **not** mint or return tokens. Do **not** delete the original session's cookies.
   - Return `400 identity_mismatch` with body `{error: "identity_mismatch", error_description: "You must re-authenticate as the same user who initiated step-up."}`.
   - The caller (tmi-ux) surfaces a message naming the original email (it has the original JWT in cookie storage to read the email claim from).
3. If `re_authed.InternalUUID == stateData.original_user_uuid`:
   - Read the previous refresh token from the `tmi_refresh_token` cookie (if present). Blacklist it via `token_blacklist.go`. If absent, skip blacklist with a debug log.
   - Mint via `GenerateTokensWithUserInfo` (existing). Produces `auth_time = now`.
   - Set HttpOnly cookies via `SetTokenCookies(c, tokenPair, h.cookieOpts)`.
   - Write `system_audit_entry`: `auth.step_up_complete`, `new_value = {"provider": "<id>", "strength": "<strong|weak>", "mode": "round_trip"}`. Strength carried from the state payload.
   - Return `200 OK` with TokenPair (matches existing `/oauth2/token` response shape).

**Normal (non-step-up) branch:** unchanged.

## Audit row shapes

All step-up audit rows are written into `system_audit_entries` — no new table, no new columns. Field conventions:

| Event | HTTPMethod | HTTPPath | FieldPath | NewValueRedacted (JSON) | ChangeSummary |
|---|---|---|---|---|---|
| Strong success | `GET` | `/oauth2/step_up` | `auth.step_up_complete` | `{"provider":"google","strength":"strong","mode":"round_trip"}` | `step-up completed (strong) via google` |
| Weak success | `GET` | `/oauth2/step_up` | `auth.step_up_complete` | `{"provider":"github","strength":"weak","mode":"short_circuit"}` | `step-up completed (weak) via github — upstream IdP does not honor prompt=login` |
| Identity mismatch | `GET` | `/oauth2/step_up` | `auth.step_up_failed` | `{"reason":"identity_mismatch","attempted_email":"<sha256-prefix-8 + last-6 tail>"}` | `step-up failed: identity_mismatch` |
| User cancelled at IdP | `GET` | `/oauth2/step_up` | `auth.step_up_failed` | `{"reason":"access_denied"}` | `step-up failed: user cancelled at IdP` |
| State expired / replay | `GET` | `/oauth2/step_up` | `auth.step_up_failed` | `{"reason":"state_expired"}` | `step-up failed: state expired or replayed` |
| CC-grant rejected | `GET` | `/oauth2/step_up` | `auth.step_up_rejected` | `{"reason":"unsupported_grant_type","subject_prefix":"sa"}` | `step-up rejected: client credentials grant` |
| Invalid provider | `GET` | `/oauth2/step_up` | `auth.step_up_rejected` | `{"reason":"invalid_provider","provider":"<id>"}` | `step-up rejected: provider not configured` |

**Actor fields** denormalize the original step-up initiator (the JWT-holder at the start of the flow) for all events. For the identity_mismatch case, the *attempted* email is stored in `new_value` (Tier-2 redacted) so investigators can see what account was tried.

**Redaction reuse:** the existing `api/admin_audit_redaction.go` Tier-2 helper (sha256-prefix-8 + last-6 tail when length ≥ 24) is applied to `attempted_email` before write. No new redaction rules.

**Fail-open vs fail-closed:**
- Step-up *completion* audit write failure: log Error, continue. The user gets their new tokens. Matches the existing #355 policy. OOB alert sink (#395, deferred) is the tamper-evidence backstop.
- Step-up *rejection* audit write failure: log Error, return the rejection response anyway. The rejection happens regardless of audit success.

## Security considerations

### CSRF

`GET /oauth2/step_up` is a cookie-authenticated state-changing operation (creates Redis state, triggers a redirect chain that ultimately rotates tokens). Two defenses, both already in place from `/oauth2/authorize`:

- **client_callback allowlist** — an attacker cross-origin GET cannot redirect the post-step-up flow to an attacker-controlled URL.
- **Mandatory PKCE** — the attacker cannot complete `/oauth2/token` without the `code_verifier`, which lives client-side at the legitimate origin.

A CSRF-triggered step-up therefore lands at an allowlisted callback the attacker doesn't control and cannot complete token exchange. The audit row still gets written, providing investigability.

### Cookie security

New tokens are issued via the existing `SetTokenCookies(c, tokenPair, h.cookieOpts)`:
- `HttpOnly: true` — no JavaScript access (XSS protection)
- `Secure: true` (when configured) — HTTPS only
- `SameSite` matches existing setting
- `Path`, `Domain` match existing setting

The *old access token* remains technically valid until its 1-hour expiry — same property as a normal login. There's no way to invalidate an in-flight bearer JWT before its `exp` without a stateful check on every request. Step-up does not regress this property.

The old refresh token is blacklisted (Redis denylist) so even if the attacker still has the old cookie, refresh rotation is blocked.

### Identity binding

The identity-match check at `/oauth2/token` is the load-bearing defense against cookie theft + step-up laundering. Without it, an attacker who steals a cookie could initiate step-up and then re-auth as their own attacker-controlled IdP account, ending up with a fresh-`auth_time` JWT bound to the victim's TMI user. The check makes that path fail closed with an audit trail.

### State / replay

- `oauth_state:<state>` — 10-min TTL, single-use (deleted on first read by `parseCallbackState`)
- `pkce:<code>` — 10-min TTL, single-use (deleted by `/oauth2/token`)
- Replays / expired states get audited as `state_expired`

### Rate limiting

`/oauth2/step_up` is wired into the existing rate-limit middleware on the same path/IP key as `/oauth2/authorize`. Prevents step-up-spam from grinding the audit table.

## Out-of-scope risks (documented for completeness)

- **Old access token windows.** An attacker who steals the *access* cookie before step-up can use it on non-step-up-gated routes until its 1-hour expiry. Mitigation lives outside this issue (shorter JWT TTL, stateful per-request validation, refresh-token binding).
- **GitHub admin's lack of fresh-prompt enforcement.** Recorded as `strength: weak` in the audit table. Operators who require strict step-up should not bind GitHub as an admin IdP. We do not block GitHub admins (would be a breaking operator change).
- **Refresh-token cookie absent at step-up time.** Test harnesses and curl-style clients may carry only the access cookie. In that case, the old refresh token cannot be blacklisted. Logged at Debug; behavior otherwise unchanged. This is an operator-aware edge case, not a bypass.

## File / code layout

| File | New / Modified | Purpose |
|---|---|---|
| `auth/handlers_step_up.go` | NEW | `Handlers.StepUp(c *gin.Context)` — the GET handler. JWT/cookie read, strength classification, strong→302 / weak→short-circuit, audit row, response. |
| `auth/provider_step_up.go` | NEW | `ClassifyStepUpStrength(provider, providerID)`, `BuildStepUpAuthorizationURL(provider, providerID, state)`. |
| `auth/audit_step_up.go` | NEW | Step-up audit row helpers; thin wrapper around `SystemAuditRepository.Create()`. |
| `auth/saml/provider.go` | MODIFIED | Add `GetAuthorizationURLForceAuthn(state) (string, error)` that sets `ForceAuthn="true"`. |
| `auth/handlers_oauth.go` | MODIFIED | `parseCallbackState` reads new step-up state fields; `processOAuthCallback` propagates them into `pkce:<code>`; handles upstream `error=access_denied` for step-up. |
| `auth/handlers_token.go` | MODIFIED | Step-up branch after PKCE verify: identity-match, blacklist old refresh, rotate, audit. |
| `auth/state_store.go` | MODIFIED (small) | If needed: extend the PKCE-record JSON schema to carry step-up fields. (Likely just map-keys; no signature change.) |
| `cmd/server/main.go` | MODIFIED | Register `GET /oauth2/step_up` → `Handlers.StepUp`. Same middleware chain as `/oauth2/authorize` (rate-limit, public-endpoint marker; no JWT middleware on the route). |
| `api-schema/tmi-openapi.json` | MODIFIED | Add the operation with full response set + examples. |
| `internal/config/config.go` (or similar) | (none expected) | No new config fields. |

### Tests

| Test file | New / Modified | Coverage |
|---|---|---|
| `auth/handlers_step_up_test.go` | NEW | Unit: strong→302, weak→200, missing JWT→401, CC-grant→400, invalid client_callback→400, invalid PKCE→400, disabled provider→400, Redis unavailable→503. |
| `auth/provider_step_up_test.go` | NEW | Unit: `ClassifyStepUpStrength` per known provider (google/microsoft/github/tmi + generic OIDC + SAML stub). `BuildStepUpAuthorizationURL` asserts `prompt=login` and `max_age=0` appended; SAML returns `ForceAuthn=true`. |
| `auth/audit_step_up_test.go` | NEW | Unit: row shape for each event kind in §Audit; `attempted_email` redaction matches Tier-2 format. |
| `auth/handlers_oauth_step_up_callback_test.go` | NEW | Unit: step-up marker survives state→code transition; `pkce:<code>` carries step-up fields; access_denied path. |
| `auth/handlers_token_step_up_test.go` | NEW | Unit: identity-match success rotates + audits + blacklists old refresh; mismatch returns 400 + audit + no token; new HttpOnly cookies set. |
| `test/integration/workflows/step_up_oauth_round_trip_test.go` | NEW | Integration end-to-end against real Postgres + miniredis: strong-provider flow using TMI stub provider (with `prompt=login` assertion on the upstream URL); weak-provider short-circuit; audit rows persisted. |

## RFC alignment for OpenAPI completeness

Per request, every RFC 6749 §4.1.2.1 error response is enumerated in the OpenAPI spec with an example, even where TMI never returns it in practice (`unauthorized_client`, `unsupported_response_type`, `invalid_scope`). The example bodies use the existing `Error` schema (`error`, `error_description`). `access_denied` is documented on the `/oauth2/callback` operation, not on `/oauth2/step_up`, because TMI never originates it — the upstream IdP does.

## Verification gates

- `make lint`
- `make validate-openapi` — must remain `0 errors`, no new warnings beyond the existing baseline
- `make build-server`
- `make test-unit` — all new unit tests pass
- `make test-integration` — new integration test passes; no regressions
- `security-regression` skill — pass
- `oracle-db-admin` subagent — N/A (no DB schema changes). Will dispatch defensively if the implementation accidentally introduces any.

## Definition of done

1. `GET /oauth2/step_up` operational with both strong (302 redirect) and weak (200 short-circuit) paths.
2. Identity-match check enforced at `/oauth2/token`; mismatch yields 400 + audit row + no tokens.
3. All seven event kinds in §Audit produce correctly shaped `system_audit_entries` rows.
4. RFC 6749 error responses fully enumerated in OpenAPI with examples; spec validates clean.
5. Unit + integration tests cover all branches.
6. tmi-ux follow-up issue filed to update client-side step-up retry path to call `/oauth2/step_up` (not `/oauth2/authorize`).
7. Audit-trail wiki updated to describe the new event kinds (or follow-up filed if wiki updates land separately).
8. PR opens `dev/1.4.0`; commit message references `Closes #397`; manual `gh issue close 397` after merge (since the branch is not `main`).
