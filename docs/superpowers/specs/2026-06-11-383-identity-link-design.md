# Design: Interactive identity-link flow for binding a second IdP (#383)

**Issue:** [#383](https://github.com/ericfitz/tmi/issues/383) — feat(auth): interactive identity-link flow for binding a second IdP (T1 part 2)
**Date:** 2026-06-11
**Status:** Approved

## Problem

A user authenticated under one IdP cannot deliberately attach a second IdP to their account;
a second-IdP login attempt with the same email is (correctly) rejected with 409 (#290/#346
Part 1). Legitimate multi-IdP users need an explicit, consent-bearing link flow. Per the
issue's scope comment this is a **usability fix** — T1's takeover primitive is already closed
— but the link operation itself is account-takeover-adjacent and gets matching controls.

## Consent model (the core of the design)

Both sides must prove **control**, and the bind requires an **explicit, visible confirmation**:

- **Identity A (the TMI account):** initiates `link/start` authenticated with **step-up-fresh
  auth_time (≤ 5 min)**, and later commits the bind with an authenticated, step-up-fresh
  confirm POST. A leaked bearer token alone cannot plant a backdoor identity.
- **Identity B (the second IdP identity):** *knowing* B is worthless — the flow requires
  completing B's login at the IdP and authorizing TMI's OAuth app. Only someone who controls
  B gets a code. `prompt=select_account` (and `prompt=consent` where the provider supports
  it, classified per provider like step-up strength) forces a visible chooser/consent moment
  instead of a silent redirect.
- **Anti link-CSRF (the swapped-direction attack):** an attacker could start a link flow on
  their own account and trick a victim into completing the IdP round with the victim's B.
  Defense: the callback **binds nothing** — it stages a *pending link* whose one-time token
  is delivered only to the allowlisted `client_callback` (legitimate tmi-ux), and the
  pending/confirm endpoints require a JWT whose UUID **matches the pending link's user
  UUID**. A victim's browser session ≠ the attacker's UUID → 403; the pending link expires
  unconsumed. The attacker never possesses the token.
- **No custom text on the IdP consent screen:** standard OAuth/OIDC deliberately has no
  RP-supplied free-text parameter (phishing vector); consumer IdPs don't render RFC 9396
  authorization_details. The "this links B to account A and grants B sign-in access" message
  is rendered by **tmi-ux** on the confirmation screen — the explicit mutual-consent
  artifact, both identities named, before anything commits.
- **No account merge, ever:** a `(provider, sub)` already bound to ANY TMI account (primary
  or linked) → 409 `identity_already_bound`.
- **No email matching anywhere in the link path.** Explicit consent replaces the Tier-2/3
  email heuristics; B's email is cached for display only. B's `email_verified` is irrelevant
  to the bind.

## Schema — new `linked_identities` table

| Column | Notes |
|---|---|
| `id` | PK, uuid varchar(36) |
| `user_internal_uuid` | indexed; FK → `users.internal_uuid` |
| `provider` | **global uniqueIndex `uniq_linked_provider_sub` priority 1** |
| `provider_user_id` | uniqueIndex priority 2 — an upstream identity belongs to exactly one TMI account |
| `email`, `name` | display cache only |
| `linked_at` | autoCreateTime |
| `last_used_at` | nullable; touched on login via this identity |

A user may link multiple identities, including several from one provider. Schema change →
**oracle-db-admin review** + `internal/dbschema/schema.go` / `cmd/dbtool` sync.

## API surface (user-scoped, `/me/identities*`)

| Operation | Auth | Purpose |
|---|---|---|
| `POST /me/identities/link/start?idp={provider}` | JWT + step-up fresh; service accounts denied | Create `oauth_state:{state}` (Redis, 10-min TTL) with `identity_link: true` + user UUID; return authorize URL (with `prompt=select_account`/`consent` per provider classification) + expiry. PKCE is absent — the server exchanges the code confidentially; the binding mechanism is the pending-token + UUID-matched step-up-fresh confirm. |
| `/oauth2/callback` (existing, new branch) | none (browser GET) | On `identity_link` marker: exchange code, discard IdP tokens, keep `(provider, sub, email, name)`; preliminary 409 check; stage **pending link** in Redis (5-min TTL, one-time token, bound to user UUID from state); redirect to allowlisted `client_callback` with the pending token |
| `GET /me/identities/link/pending/{token}` | JWT; UUID must match pending link | Both sides for the confirmation screen: B's `(provider, sub-suffix, email, name)` + A's primary identity |
| `POST /me/identities/link/confirm` | JWT + step-up fresh; UUID match; token consumed one-time | Commit the bind (insert `linked_identities`, re-check 409 inside the transaction), audit |
| `GET /me/identities` | JWT | Primary identity (from `users`) + linked rows |
| `DELETE /me/identities/{id}` | JWT + step-up fresh | Unlink a linked identity owned by the caller; primary is not unlinkable; others' rows → 404 |

OpenAPI: user-scoped URL pattern; the per-TM conventions apply (snake_case, error responses).
**New mechanism:** the step-up route table today only gates `/admin/*` writes. Add an opt-IN
extension `x-tmi-authz-step-up: "required"` honored on non-admin routes by
`BuildStepUpRouteTable`, applied to link/start, link/confirm, and the unlink DELETE.

## Login resolution change — `auth/handlers_oauth_user.go`

Tier 1 extends: exact `(provider, provider_user_id)` match against `users`, **then against
`linked_identities`** → resolve to the owning user (touch `last_used_at`; mint JWT carrying
the owner's **primary identity** claims — the JWT is scoped to the account owner, not the
login identity used). Rationale: JWTs represent the authenticated user, not the credential
used to authenticate; the used identity is resolved to the owner at login time and needs no
separate JWT claim. Tiers 2/3 and the #290/#346 rejections are untouched — a GitHub login
with a matching email but no link is still rejected; the link flow is the only path to
multi-IdP.

**Token/session impact: none.** Claims carry the IdP used at login; linking changes no
claims; no rotation or cache invalidation (verified against `auth/service.go` Claims).

## Audit

`IdentityLinkAuditor` parallel to `StepUpAuditor` (`auth/audit_step_up.go` pattern), writing
system-audit events with redacted `(idp, sub)` of **both sides**:
`auth.identity_link_complete`, `auth.identity_link_failed`, `auth.identity_link_rejected`,
`auth.identity_unlink`. These flow through `SystemAuditRepository` and therefore also fire
the #395 out-of-band webhook alert automatically.

## Client work (tmi-ux)

The confirmation screen and flow wiring are client work: handle the `client_callback`
redirect carrying the pending token, fetch pending details, render both identities with the
"grants sign-in access to your account" copy, POST confirm, plus an identities management
view (list/unlink). **File a tmi-ux companion issue at implementation start** (via the
file-client-bug skill's repo conventions, as a feature).

## Testing

- **Unit (step-up harness pattern, miniredis):** start (state+PKCE stored, URL prompt
  params, 401 on stale auth_time, 403 for service accounts); callback branch (pending link
  staged, IdP tokens discarded, upstream error paths, 409 precheck); pending GET (UUID
  mismatch → 403/404, expiry); confirm (one-time consumption, UUID match, stale auth_time
  401, 409 race re-check); list/unlink (ownership, primary not unlinkable); resolution
  (login via linked identity lands on owner; `last_used_at` touched; email-match rejections
  unchanged — pin `TestFindOrCreateUser_UnverifiedEmailMatchRejected` still passes); audit
  events for all four outcomes.
- **Integration (OAuth stub):** full happy path — login as alice (tmi provider), link a
  second stub identity, confirm, sign in with the second identity → same account UUID
  (issue acceptance); conflict path → 409; unlink → second identity no longer signs in to
  A's account.
- **API change battery:** postman/newman, CATS + analysis (zero-500: bad tokens/states must
  4xx), `security-review` skill (this is auth surface).
- **Oracle:** `make test-integration-oci`; oracle-db-admin review of the new table.

## Out of scope

- Account merge (absorbing an existing TMI account's identity) — never supported.
- Changing the primary identity; admin management of others' linked identities; per-identity
  permissions. Each would be its own issue.
