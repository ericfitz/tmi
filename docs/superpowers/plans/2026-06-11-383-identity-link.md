# Identity-Link Flow (#383) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Authenticated users can deliberately bind additional IdP identities to their account via an OAuth round-trip with explicit, CSRF-proof confirmation; logins via linked identities resolve to the owning account.

**Architecture:** New `linked_identities` table; `POST /me/identities/link/start` (step-up fresh) → second-IdP authorize URL with `prompt=select_account`; existing `/oauth2/callback` gains an `identity_link` state branch that stages a Redis pending link; `GET .../pending/{token}` + `POST .../confirm` (UUID-matched, step-up fresh, one-time) commit the bind; `GET /me/identities` + `DELETE /me/identities/{id}` manage them; Tier-1 login resolution extends to linked identities.

**Tech Stack:** Go, Gin, oapi-codegen, GORM, Redis (state/PKCE/pending), existing step-up machinery.

**Spec:** `docs/superpowers/specs/2026-06-11-383-identity-link-design.md` — read it first. The consent/CSRF model there is normative; do not weaken it for convenience.

**Branch:** work on `dev/1.4.0`.

**Key precedent files (read before starting):** `auth/handlers_step_up.go` (state storage + second round-trip), `auth/provider_step_up.go` (URL builder + strength classification), `auth/state_store.go` (PKCE), `auth/handlers_oauth_user.go` (tiered resolution), `auth/audit_step_up.go` (auditor shape), `auth/handlers_step_up_test.go:218` (test harness), `api/step_up_routes.go` (route table).

---

### Task 1: linked_identities model + store (TDD)

**Files:**
- Create: `api/models/linked_identity.go`
- Modify: `api/models/models.go` (AllModels list)
- Create: `auth/linked_identity_store.go`
- Create: `auth/linked_identity_store_test.go`

- [ ] **Step 1: Write the failing store test**

In-memory SQLite (pattern from existing auth store tests). Contract:

```go
func TestLinkedIdentityStore(t *testing.T) {
	db := /* sqlite + AutoMigrate(&models.User{}, &models.LinkedIdentity{}) */
	store := NewLinkedIdentityStore(db)
	ctx := context.Background()

	userA, userB := seedUser(t, db, "alice@x.com"), seedUser(t, db, "bob@x.com")

	// Create + fetch by (provider, sub)
	li, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: userA, Provider: "github", ProviderUserID: "gh-1",
		Email: "alice@users.github.com", Name: "alice-gh",
	})
	require.NoError(t, err)

	got, err := store.GetByProviderSub(ctx, "github", "gh-1")
	require.NoError(t, err)
	assert.Equal(t, userA, got.UserInternalUUID)

	// Global uniqueness: same (provider, sub) for ANOTHER user fails
	_, err = store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: userB, Provider: "github", ProviderUserID: "gh-1",
	})
	require.Error(t, err) // classify with dberrors.ErrConstraint via errors.Is if available

	// ListByUser, TouchLastUsed, Delete (scoped to owner)
	rows, err := store.ListByUser(ctx, userA)
	require.NoError(t, err)
	require.Len(t, rows, 1)

	require.NoError(t, store.TouchLastUsed(ctx, li.ID))

	require.NoError(t, store.Delete(ctx, li.ID, userA))
	_, err = store.GetByProviderSub(ctx, "github", "gh-1")
	assert.ErrorIs(t, err, ErrLinkedIdentityNotFound)

	// Delete scoped: deleting another user's row reports not-found
	li2, _ := store.Create(ctx, LinkedIdentityInput{UserInternalUUID: userA, Provider: "google", ProviderUserID: "g-1"})
	err = store.Delete(ctx, li2.ID, userB)
	assert.ErrorIs(t, err, ErrLinkedIdentityNotFound)
}
```

- [ ] **Step 2: Run to verify failure**

Run: `make test-unit name=TestLinkedIdentityStore` — FAIL (undefined types).

- [ ] **Step 3: Implement**

`api/models/linked_identity.go`:

```go
package models

import "time"

// LinkedIdentity binds an additional upstream IdP identity to an existing
// TMI account (#383). The global unique index on (provider, provider_user_id)
// enforces that an upstream identity belongs to exactly one TMI account —
// across both this table and (by resolution-order checks in auth) the users
// table. Email/Name are display caches only; email plays no role in binding.
type LinkedIdentity struct {
	ID               DBVarchar `gorm:"primaryKey;not null;size:36"`
	UserInternalUUID DBVarchar `gorm:"size:36;not null;index:idx_linked_user"`
	Provider         DBVarchar `gorm:"size:100;not null;uniqueIndex:uniq_linked_provider_sub,priority:1"`
	ProviderUserID   DBVarchar `gorm:"size:500;not null;uniqueIndex:uniq_linked_provider_sub,priority:2"`
	Email            DBVarchar `gorm:"size:320"`
	Name             DBVarchar `gorm:"size:256"`
	LinkedAt         time.Time `gorm:"not null;autoCreateTime"`
	LastUsedAt       *time.Time
}
```

(Check Oracle identifier-length conventions used by sibling models for the index names; ≤30 chars is safe — `uniq_linked_provider_sub` is 24.) Add to `models.AllModels()`. Add the table/indexes to `internal/dbschema/schema.go` expectations and check `cmd/dbtool`.

`auth/linked_identity_store.go`: `LinkedIdentityStore` interface + GORM impl with the five methods from the test (`Create`, `GetByProviderSub`, `ListByUser`, `TouchLastUsed`, `Delete(id, ownerUUID)`), `ErrLinkedIdentityNotFound` sentinel, FK note: do not add a DB-level FK with cascade without oracle-db-admin guidance — keep referential handling in code and ask in the review (Task 8). User deletion: add cleanup of linked_identities to the existing user-delete path (find it: `grep -rn "DeleteUser" auth/ api/ | grep -v _test` — wherever users table rows are deleted, delete linked rows in the same transaction).

- [ ] **Step 4: Run tests**

`make test-unit name=TestLinkedIdentityStore` — PASS; `make build-server && make test-unit`.

- [ ] **Step 5: Commit**

```bash
git add api/models/ auth/linked_identity_store.go auth/linked_identity_store_test.go internal/dbschema/ cmd/dbtool/
git commit -m "feat(auth): linked_identities table + store

Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Login resolution via linked identities (TDD)

**Files:**
- Modify: `auth/handlers_oauth_user.go`
- Modify: `auth/handlers_oauth_user_test.go`

- [ ] **Step 1: Write failing tests**

Extend the existing FindOrCreateUser test file (reuse its fixtures/stubs):

```go
// Login with a linked (provider, sub) resolves to the OWNING user and
// touches last_used_at; no new user is created.
func TestFindOrCreateUser_ResolvesLinkedIdentity(t *testing.T) { ... }

// The email-based rejections are UNCHANGED: a second-IdP login with a
// matching email but no link still fails (pin: existing
// TestFindOrCreateUser_UnverifiedEmailMatchRejected and the cross-provider
// 409 test must still pass untouched).
```

- [ ] **Step 2: Run to verify failure**

`make test-unit name=TestFindOrCreateUser_ResolvesLinkedIdentity` — FAIL.

- [ ] **Step 3: Implement**

In the Tier-1 section of `FindOrCreateUser`/`ResolveUser` (`auth/handlers_oauth_user.go:103-108`): after the `GetUserByProviderID` miss and BEFORE Tier 2, look up `linkedIdentityStore.GetByProviderSub(provider, sub)`; on hit, load the owning user by UUID, `TouchLastUsed` (non-fatal), and return it with a new match kind (e.g., `userMatchLinkedIdentity`) so logging/metrics distinguish it. JWT minting downstream already uses the login provider for the `idp` claim — verify and leave as-is. Inject the store into the handlers/service struct the same way other stores are injected (constructor + main.go wiring).

- [ ] **Step 4: Run tests**

`make test-unit name=TestFindOrCreateUser` — ALL pass, including the pre-existing rejection pins.

- [ ] **Step 5: Commit**

```bash
git add auth/
git commit -m "feat(auth): resolve logins through linked identities at Tier 1

Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Step-up route-table opt-in for non-admin routes (TDD)

**Files:**
- Modify: `api/step_up_routes.go`
- Modify: `api/step_up_routes_test.go`

- [ ] **Step 1: Write failing test**

In the synthetic-spec style of the existing tests: a non-`/admin` operation carrying `"x-tmi-authz-step-up": "required"` IS in the table; one without it is NOT; the `/admin/*` write default and the `optional` opt-out behave exactly as before (pin existing tests stay green).

- [ ] **Step 2: Run to verify failure** — `make test-unit name=TestStepUpRoute` (new case fails).

- [ ] **Step 3: Implement**

In `BuildStepUpRouteTable` (`api/step_up_routes.go:57-86`): in addition to the `/admin/` write-method default, register any operation (any path, any method) whose `x-tmi-authz-step-up` extension equals `"required"`. Update the file's policy comment.

- [ ] **Step 4: Run** — `make test-unit name=TestStepUpRoute` — PASS (old + new).

- [ ] **Step 5: Commit**

```bash
git add api/step_up_routes.go api/step_up_routes_test.go
git commit -m "feat(api): x-tmi-authz-step-up required opt-in for non-admin routes

Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: OpenAPI — five /me/identities operations

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Add schemas and paths**

Backup first (`cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.$(date +%Y%m%d_%H%M%S).backup`). Model on existing `/me/*` operations (tags, bearerAuth, error refs, `x-tmi-authz` — copy the exact authz extension used by `GET /me/preferences`). Operations:

| Method/Path | operationId | Notes |
|---|---|---|
| `POST /me/identities/link/start` | `startIdentityLink` | query param `idp` (required, string); 200 → `{link_state, authorization_url, expires_at}`; `"x-tmi-authz-step-up": "required"` |
| `GET /me/identities/link/pending/{token}` | `getPendingIdentityLink` | 200 → `{pending: {provider, provider_user_id, email, name}, account: {provider, email}}`; 404 expired/unknown/mismatch |
| `POST /me/identities/link/confirm` | `confirmIdentityLink` | body `{token}`; 201 → `LinkedIdentity` schema; 409 `identity_already_bound`; `"x-tmi-authz-step-up": "required"` |
| `GET /me/identities` | `listMyIdentities` | 200 → `{primary: Identity, linked: [LinkedIdentity]}` |
| `DELETE /me/identities/{id}` | `deleteMyIdentity` | 204; 404 not-owned/unknown; cannot target primary (404 — primary has no linked-identity id); `"x-tmi-authz-step-up": "required"` |

Schemas: `LinkedIdentity` (`id` uuid, `provider`, `provider_user_id`, `email`, `name`, `linked_at`, `last_used_at` nullable), plus the two small response wrappers. snake_case properties throughout. Service accounts: the `/me` authz already permits SAs generally — add the SA denial in the handlers (Task 5) since these are interactive-human flows; document it in the operation descriptions ("service-account tokens are rejected").

- [ ] **Step 2: Validate + generate**

```bash
jq empty api-schema/tmi-openapi.json && make validate-openapi && make generate-api
make build-server   # EXPECTED FAIL: 5 unimplemented ServerInterface methods (Task 5/6)
```

No commit yet — Tasks 5/6 make it green; commit there.

---

### Task 5: Link flow handlers — start, callback branch, pending, confirm (TDD)

**Files:**
- Create: `auth/handlers_identity_link.go`
- Create: `auth/handlers_identity_link_test.go`
- Modify: `auth/handlers_oauth.go` (callback branch)
- Modify: `api/server_auth.go` (+ server wiring for the generated methods)

- [ ] **Step 1: Write failing tests (step-up harness pattern)**

Clone `newStepUpTestHarness` (`auth/handlers_step_up_test.go:218`) into `newIdentityLinkTestHarness` (miniredis, stub provider, stub user repo, in-memory audit writer, seeded authenticated user). Cases (names final, bodies per harness conventions):

```go
TestIdentityLinkStart_StoresStateAndReturnsURL          // state in redis has identity_link=true + user UUID; URL has prompt=select_account; PKCE stored
TestIdentityLinkStart_RejectsServiceAccount             // 403
TestIdentityLinkStart_RejectsUnknownProvider            // 400
TestIdentityLinkCallback_StagesPendingLink              // code exchange → pending in redis (5m TTL); IdP tokens NOT stored; redirect to client_callback with token
TestIdentityLinkCallback_AlreadyBoundIsRejected         // (provider,sub) in users OR linked_identities → redirect with error=identity_already_bound; audit rejected
TestIdentityLinkCallback_UpstreamErrorAudited           // error=access_denied → audit failed, redirect with error
TestPendingIdentityLink_RequiresMatchingUser            // other user's JWT → 404; matching → both sides returned
TestConfirmIdentityLink_BindsOnce                       // bind committed; token consumed (second confirm → 404); audit complete with both (idp,sub) redacted
TestConfirmIdentityLink_RaceRecheck409                  // (provider,sub) bound between callback and confirm → 409; audit rejected
```

Step-up freshness for start/confirm is enforced by the route table (Task 3) at the API layer — harness tests the handler contracts; add one routing-level test in `api/` asserting the three operations are in the step-up table from the real spec (extend Task 3's production-spec assertions or #399's invariant-test style).

- [ ] **Step 2: Run to verify failures** — `make test-unit name=TestIdentityLink` — FAIL.

- [ ] **Step 3: Implement**

`auth/handlers_identity_link.go` — mirror `handlers_step_up.go` structure:

- **Start:** resolve provider (registry/config, as step-up does at lines 233-238); generate state; store `oauth_state:{state}` with `{"identity_link":"true","provider":idp,"client_callback":cb,"link_user_uuid":user.InternalUUID}` (10-min TTL); store PKCE challenge; build URL via a `BuildIdentityLinkAuthorizationURL` that sets `prompt=select_account` (and `consent` for providers classified to honor it — reuse/extend `ClassifyStepUpStrength`); return `{link_state, authorization_url, expires_at}`. Reject `isServiceAccount` contexts with 403.
- **Callback branch:** in the shared OAuth callback handler where the `step_up` marker is branched (find it: `grep -n "step_up" auth/handlers_oauth.go`), add the `identity_link` branch: validate state + PKCE; exchange code; fetch UserInfo; **discard tokens**; check `GetUserByProviderID` and `GetByProviderSub` for foreign binding (→ redirect to client_callback with `error=identity_already_bound`; audit rejected); generate one-time pending token (crypto/rand, 32 bytes, base64url); store `identity_link_pending:{token}` in Redis with `{user_uuid, provider, provider_user_id, email, name}` (5-min TTL); redirect to the **allowlist-validated** client_callback with `link_pending={token}`.
- **Pending GET:** load `identity_link_pending:{token}`; 404 if absent or `user_uuid != ` authenticated UUID (no distinguishable response for mismatch vs missing); return both sides (truncate `provider_user_id` for display, e.g. first 8 chars + `…`).
- **Confirm POST:** load + same UUID check; **delete the Redis key first** (one-time even on failure paths after this point); inside a transaction: re-check both stores for foreign binding (409 on hit), insert `linked_identities`; audit `auth.identity_link_complete` with redacted `(idp,sub)` of both sides; return 201 with the row.
- **Audit:** `IdentityLinkAuditor` in `auth/audit_identity_link.go`, same shape as `StepUpAuditor` (`auth/audit_step_up.go:60-155`), field paths `auth.identity_link_complete|failed|rejected`, `auth.identity_unlink`.
- **API layer:** implement the generated `StartIdentityLink`, `GetPendingIdentityLink`, `ConfirmIdentityLink` ServerInterface methods in `api/server_auth.go` delegating to the auth handlers (follow how existing auth endpoints delegate there).

- [ ] **Step 4: Run** — `make test-unit name=TestIdentityLink` PASS; `make build-server && make test-unit && make lint` green.

- [ ] **Step 5: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go api/server_auth.go auth/
git commit -m "feat(auth): identity-link flow — start, pending-link staging, explicit confirm

Consent model per spec: step-up-fresh start/confirm, IdP-proven control of
the second identity, pending token deliverable only to allowlisted client
callback, UUID-matched one-time confirm. Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: List + unlink handlers (TDD)

**Files:**
- Create: `api/my_identities_handlers.go`
- Create: `api/my_identities_handlers_test.go`

- [ ] **Step 1: Failing tests** — gin-handler tests (existing `/me` handler test pattern): list returns primary (from user record) + linked rows; unlink own row → 204 + audit `auth.identity_unlink`; unlink unknown/foreign id → 404; service account → 403.

- [ ] **Step 2: Run to verify failure.**

- [ ] **Step 3: Implement** `ListMyIdentities` + `DeleteMyIdentity` on `*Server`, delegating to the linked-identity store; unlink audits via the auditor.

- [ ] **Step 4: Run** — targeted tests PASS; full `make build-server && make test-unit && make lint`.

- [ ] **Step 5: Commit**

```bash
git add api/my_identities_handlers.go api/my_identities_handlers_test.go
git commit -m "feat(api): list and unlink linked identities under /me/identities

Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Integration test (OAuth stub, two identities)

**Files:**
- Create: `test/integration/workflows/identity_link_test.go`

- [ ] **Step 1: Write the test**

Conventions from existing workflow tests + the OAuth stub (`login_hint` gives deterministic identities). Flow:

1. Authenticate `alice` (login_hint=alice) → account UUID A (from `GET /me`).
2. `POST /me/identities/link/start?idp=tmi` → follow `authorization_url` through the stub **as a different upstream identity** (`login_hint=alice-alt`) → the redirect lands on the stub's callback with `link_pending` token. (Verify the stub harness can drive a flow without minting a TMI session for alice-alt — read `scripts/oauth-client-callback-stub.py` endpoints; the `/flows/start` automation may need the raw redirect captured. If the stub cannot express this, extend the stub script minimally — it is test infrastructure.)
3. `GET /me/identities/link/pending/{token}` as alice → both sides present.
4. `POST /me/identities/link/confirm` as alice → 201.
5. Fresh login via the stub as `alice-alt` → `GET /me` returns **UUID A** (issue acceptance).
6. `GET /me/identities` → primary + 1 linked.
7. Conflict: bob attempts to link `alice-alt`'s identity → callback redirect carries `error=identity_already_bound`.
8. `DELETE /me/identities/{id}` as alice → 204; fresh `alice-alt` login now creates/resolves per normal rules, NOT account A.
9. Step-up freshness: note — tokens minted seconds earlier are fresh, so the stale-auth_time 401 path stays unit-tested (cannot age a token in integration without waiting out the window).

- [ ] **Step 2: Run** — `make test-integration` green.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/identity_link_test.go scripts/oauth-client-callback-stub.py
git commit -m "test(integration): identity-link happy path, conflict, and unlink via OAuth stub

Refs #383.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 8: Gates, reviews, companion issue, close-out

- [ ] **Step 1: Full gates** — `make lint && make build-server && make test-unit && make test-integration`.

- [ ] **Step 2: MANDATORY — oracle-db-admin review** — new table + unique index; ask explicitly: FK-vs-code-managed referential integrity for `user_internal_uuid` (user-delete cleanup), index name lengths, varchar(500) in a composite unique index on Oracle (key length limits!) — if blocked, the reviewer's guidance may require a hash column for the unique key; implement what they prescribe.

- [ ] **Step 3: API battery + security review** — postman/newman (add the five operations), `make cats-fuzz` + analysis (zero-500: bad tokens, bad states, replayed confirms must 4xx), then the `security-review` skill — direct it at the consent/CSRF model: pending-token entropy and TTL, client_callback allowlist enforcement on the link branch, one-time consumption races, step-up gating actually firing on the three routes, no IdP token persistence. Stop and surface findings.

- [ ] **Step 4: Oracle ADB** — `make test-integration-oci`.

- [ ] **Step 5: tmi-ux companion issue** — file a FEATURE issue in ericfitz/tmi-ux (use the file-client-bug skill's repo/project conventions, type feature): confirmation screen (render both identities + "grants sign-in access" copy), client_callback handling of `link_pending`/`error=identity_already_bound`, identities management view (list/unlink), step-up re-auth UX on 401 challenges. Link it to ericfitz/tmi#383 and reference the spec path.

- [ ] **Step 6: Wiki** — auth documentation page: the link flow sequence (with the consent/CSRF rationale), endpoint reference, prompt behavior per provider, unlink semantics, audit events. Commit + push wiki.

- [ ] **Step 7: Land and close**

```bash
git pull --rebase && git push && git status
gh issue comment 383 --body "Implemented on dev/1.4.0. Consent model: step-up-fresh start + explicit UUID-matched one-time confirm; control of the second identity proven at the IdP (prompt=select_account); pending-link token deliverable only to the allowlisted client callback (closes link-CSRF); no email matching in the link path; no account merge (global unique (provider,sub) → 409). Logins via linked identities resolve at Tier 1. tmi-ux companion issue: <link>. Design: docs/superpowers/specs/2026-06-11-383-identity-link-design.md."
gh issue close 383
```

---

## Self-Review Notes (already applied)

- Spec coverage: table/store (T1), resolution (T2), step-up opt-in mechanism (T3), OpenAPI (T4), flow handlers + auditor (T5), list/unlink (T6), end-to-end incl. acceptance criteria from the issue (T7), reviews + tmi-ux issue + close (T8).
- The issue's original `GET /me/identities/link/callback` endpoint is deliberately NOT built — IdPs require pre-registered redirect URIs, so the existing `/oauth2/callback` branches on the `identity_link` state marker (step-up precedent). The closing comment explains the deviation.
- Build-red window: Task 4 ends red by design; Task 5 commits spec+generated+handlers atomically.
- Known-unknowns flagged with verification commands: callback branch location, harness adaptation, stub capabilities for a second upstream identity, Oracle unique-key length on varchar(500).
