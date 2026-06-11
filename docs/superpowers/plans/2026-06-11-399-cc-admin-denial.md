# CC-on-/admin/* Denial: Verify, Pin, Document (#399) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing categorical denial of client-credentials (service-account) tokens on `/admin/*` durable via tests, and correct stale documentation. No production behavior change.

**Architecture:** Two test additions — a spec-invariant unit test (every `/admin/*` operation must declare the `admin` authz role) and an end-to-end integration test (real CC token → 403 on admin endpoints) — plus comment/doc corrections.

**Tech Stack:** Go, testify, TMI integration framework (`test/integration/framework`), OAuth stub.

**Spec:** `docs/superpowers/specs/2026-06-11-399-cc-admin-denial-design.md` — read it first.

**Branch:** work on `dev/1.4.0`.

**Background facts (verified during design):**
- `RequireAdministrator` (`api/auth_helpers.go:47-58`) 403s any request whose gin context has `isServiceAccount=true` (set by JWT middleware for `sa:*` subjects, `cmd/server/jwt_auth.go`).
- All 66 `/admin/*` operations in `api-schema/tmi-openapi.json` declare `x-tmi-authz.roles: ["admin"]` (zero omissions today).
- Unit pin for the helper already exists: `api/authorization_middleware_test.go:468`.

---

### Task 1: Spec invariant test — every /admin/* operation requires the admin role (TDD)

**Files:**
- Create: `api/authz_admin_invariant_test.go`

- [ ] **Step 1: Write the test**

This test must use the EMBEDDED production spec (`LoadGlobalAuthzTable` path), not synthetic JSON, so it fails when a future `/admin/*` endpoint forgets the role.

```go
package api

import (
	"strings"
	"testing"
)

// TestAdminRoutesDeclareAdminRole pins the #399 security invariant: every
// /admin/* operation in the production OpenAPI spec must declare the "admin"
// role in x-tmi-authz. The admin role routes through RequireAdministrator,
// which categorically denies service-account (client-credentials) tokens —
// an /admin operation without the role would silently bypass that denial.
func TestAdminRoutesDeclareAdminRole(t *testing.T) {
	swagger, err := GetSwagger()
	if err != nil {
		t.Fatalf("load embedded OpenAPI spec: %v", err)
	}

	tbl, err := buildAuthzTable(swagger)
	if err != nil {
		t.Fatalf("build authz table: %v", err)
	}

	checked := 0
	for method, byPath := range tbl.byMethodPath {
		for path, rule := range byPath {
			if !strings.HasPrefix(path, "/admin") {
				continue
			}
			checked++
			hasAdmin := false
			for _, r := range rule.Roles {
				if r == RoleAuthzAdmin {
					hasAdmin = true
					break
				}
			}
			if !hasAdmin {
				t.Errorf("%s %s does not declare the admin role in x-tmi-authz; "+
					"every /admin/* operation must require it so service-account "+
					"tokens are denied (see #399)", method, path)
			}
		}
	}

	// Sanity: the spec has dozens of admin operations; zero means the table
	// or the prefix match broke, not that the invariant holds.
	if checked < 30 {
		t.Fatalf("only %d /admin operations found in the authz table; expected 60+ — the invariant check is not seeing the real spec", checked)
	}
}
```

NOTE for the implementer: `buildAuthzTable` and the `byMethodPath` field are unexported but this test is in package `api`, so direct access works. If the actual struct/lookup API differs (check `api/authz_table.go:80-86`), iterate via whatever accessor exists — the contract is "every /admin/* (method,path) rule contains RoleAuthzAdmin".

- [ ] **Step 2: Run the test — it must pass against today's spec**

Run: `make test-unit name=TestAdminRoutesDeclareAdminRole`
Expected: PASS with 60+ operations checked. (This is a pin, not a red-green cycle — the invariant already holds; the test exists to catch future regressions.)

To prove the test can fail, temporarily: not needed — but if you want confidence, run it once with the prefix changed to `/adminX` and confirm the `checked < 30` guard fires, then revert.

- [ ] **Step 3: Commit**

```bash
git add api/authz_admin_invariant_test.go
git commit -m "test(api): pin invariant — every /admin operation declares the admin authz role

A future /admin endpoint without roles:[admin] would silently bypass the
categorical service-account denial in RequireAdministrator.

Refs #399.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Integration test — real CC token gets 403 on admin endpoints

**Files:**
- Modify: `test/integration/workflows/client_credentials_test.go`

- [ ] **Step 1: Write the test**

Append to `test/integration/workflows/client_credentials_test.go`. It follows the file's existing conventions (`INTEGRATION_TESTS` guard, `framework.AuthenticateAdmin()`, `framework.NewClient`). The CC exchange is a plain form POST to `/oauth2/token` (no framework helper exists; pattern matches `step_up_round_trip_test.go:288-310`).

```go
// TestClientCredentialsDeniedOnAdminRoutes pins the #399 invariant end to
// end: a service-account (client-credentials) token is categorically denied
// on /admin/* with 403, even when the credential's owner is an
// administrator. Covers all five /admin/settings operations plus one write
// per remaining /admin sub-area.
func TestClientCredentialsDeniedOnAdminRoutes(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test (set INTEGRATION_TESTS=true to run)")
	}

	serverURL := os.Getenv("TMI_SERVER_URL")
	if serverURL == "" {
		serverURL = "http://localhost:8080"
	}

	if err := framework.EnsureOAuthStubRunning(); err != nil {
		t.Fatalf("OAuth stub not running: %v\nPlease run: make start-oauth-stub", err)
	}

	// Owner is an administrator (charlie) — the denial must hold anyway.
	tokens, err := framework.AuthenticateAdmin()
	framework.AssertNoError(t, err, "Admin authentication failed")
	adminClient, err := framework.NewClient(serverURL, tokens)
	framework.AssertNoError(t, err, "Failed to create integration client")

	// 1. Create a client credential as the admin.
	createResp, err := adminClient.Do(framework.Request{
		Method: "POST",
		Path:   "/me/client_credentials",
		Body: map[string]interface{}{
			"name": "cc-admin-denial-test",
		},
	})
	framework.AssertNoError(t, err, "create client credential")
	if createResp.StatusCode != 201 {
		t.Fatalf("create credential: got %d, want 201: %s", createResp.StatusCode, string(createResp.Body))
	}
	var cred struct {
		ID           string `json:"id"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	framework.AssertNoError(t, json.Unmarshal(createResp.Body, &cred), "parse credential")
	t.Cleanup(func() {
		_, _ = adminClient.Do(framework.Request{Method: "DELETE", Path: "/me/client_credentials/" + cred.ID})
	})

	// 2. Exchange for a service-account access token.
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", cred.ClientID)
	form.Set("client_secret", cred.ClientSecret)
	resp, err := http.Post(serverURL+"/oauth2/token",
		"application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	framework.AssertNoError(t, err, "POST /oauth2/token")
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("token exchange: got %d, want 200: %s", resp.StatusCode, string(body))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	framework.AssertNoError(t, json.Unmarshal(body, &tok), "parse token response")

	// 3. Every admin call with the CC token must return 403 (not 401, not 404).
	adminCalls := []struct {
		method string
		path   string
		body   map[string]interface{}
	}{
		// all five /admin/settings operations
		{"GET", "/admin/settings", nil},
		{"GET", "/admin/settings/test.key", nil},
		{"PUT", "/admin/settings/test.key", map[string]interface{}{"value": "x", "setting_type": "string"}},
		{"DELETE", "/admin/settings/test.key", nil},
		{"POST", "/admin/settings/reencrypt", nil},
		// one representative write per remaining sub-area
		{"POST", "/admin/groups", map[string]interface{}{"name": "cc-denial-test-group"}},
		{"DELETE", "/admin/users/00000000-0000-0000-0000-000000000000", nil},
		{"PUT", "/admin/quotas/users/00000000-0000-0000-0000-000000000000", map[string]interface{}{}},
		{"POST", "/admin/webhooks", map[string]interface{}{}},
		{"POST", "/admin/surveys", map[string]interface{}{}},
	}

	ccClient, err := framework.NewClientWithToken(serverURL, tok.AccessToken)
	framework.AssertNoError(t, err, "create CC client")

	for _, call := range adminCalls {
		t.Run(call.method+" "+call.path, func(t *testing.T) {
			resp, err := ccClient.Do(framework.Request{
				Method: call.method,
				Path:   call.path,
				Body:   call.body,
			})
			framework.AssertNoError(t, err, "request failed")
			if resp.StatusCode != 403 {
				t.Errorf("got %d, want 403 (categorical service-account denial); body: %s",
					resp.StatusCode, string(resp.Body))
			}
		})
	}
}
```

NOTE for the implementer:
- Check the framework API: if `framework.NewClientWithToken` does not exist, look at how `framework.NewClient` consumes `tokens` and construct the equivalent (e.g., a `framework.TokenSet{AccessToken: tok.AccessToken}` literal, or set the Authorization header per-request). Do NOT add a production helper for this; keep it in the test/framework layer.
- Check the create-credential response shape against `api/client_credentials_handlers.go` (field names `client_id`/`client_secret` per CLAUDE.md; adjust if the handler differs).
- The exact request bodies for the representative writes don't matter — authz runs before validation, so even an invalid body must get 403, and asserting 403 (not 400) proves the ordering.
- Add the needed imports (`io`, `net/http`, `net/url`, `strings`) to the file's import block.

- [ ] **Step 2: Run the integration test**

Run: `make test-integration` (check `make list-targets` for running a single workflow test, e.g. `make test-integration name=TestClientCredentialsDeniedOnAdminRoutes`)
Expected: PASS — ten subtests, all 403.

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/client_credentials_test.go
git commit -m "test(integration): pin CC-token 403 on /admin routes end to end

Refs #399.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Documentation corrections

**Files:**
- Modify: `CLAUDE.md` (Client Credentials Grant section)
- Modify: `auth/service.go` (CC auth_time comment, ~line 1068)
- Modify: wiki (local checkout `/Users/efitz/Projects/tmi-wiki`)

- [ ] **Step 1: CLAUDE.md**

In the "Client Credentials Grant (Machine-to-Machine Authentication)" section, change:

```
**Pattern**: Like GitHub PATs - secret only shown once at creation, full API access as creating user.
```

to:

```
**Pattern**: Like GitHub PATs - secret only shown once at creation, full API access as the creating user **except `/admin/*`**: service-account tokens are categorically denied (403) on all admin routes; administrative operations require interactive (PKCE) authentication. See #399.
```

- [ ] **Step 2: auth/service.go comment**

Replace the comment above `AuthTime: jwt.NewNumericDate(time.Now())` in the client-credentials grant path (currently says "The long-term mechanism for CC step-up is tracked in #399 — we deliberately don't break existing admin-bound automation here"):

```go
		// CC grants set auth_time = now: each token mint counts as fresh
		// authentication. This is harmless for admin step-up because
		// service-account tokens are categorically denied on /admin/* by
		// RequireAdministrator before step-up runs (#399 investigation);
		// step-up only gates /admin/* routes.
```

- [ ] **Step 3: Wiki**

In `/Users/efitz/Projects/tmi-wiki`, find the client-credentials page (`grep -ril "client_credentials\|client credentials" /Users/efitz/Projects/tmi-wiki`) and add/update a short "Admin routes" subsection: service-account tokens receive 403 on every `/admin/*` endpoint regardless of the owner's roles; admin operations require interactive authentication; enforced by `RequireAdministrator` and pinned by `TestAdminRoutesDeclareAdminRole` + `TestClientCredentialsDeniedOnAdminRoutes`. Commit and push the wiki repo.

- [ ] **Step 4: Lint and commit**

Run: `make lint` (Go file touched → also `make build-server && make test-unit`)

```bash
git add CLAUDE.md auth/service.go
git commit -m "docs(auth): correct stale CC-grant docs — /admin is categorically denied

Refs #399.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Close out

- [ ] **Step 1: Full gates**

Run: `make lint && make build-server && make test-unit && make test-integration`
Expected: all green. (No DB change → no oracle-db-admin dispatch. No OpenAPI change → no regeneration.)

- [ ] **Step 2: Land and close**

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin"
gh issue comment 399 --body "Investigation complete — the threat is already mitigated and is now test-pinned.

**Finding:** every /admin/* operation declares x-tmi-authz roles:[admin], routing through RequireAdministrator (api/auth_helpers.go), which categorically 403s service-account tokens before handlers and before step-up. CC minting additionally sets tmi_is_administrator=false and strips the administrators group. The auth_time=now concern is unreachable on /admin/* (authz denies CC first; step-up only gates /admin/*).

**Decision:** keep the blanket denial on all /admin/* (reads included); no read-only relocation (no machine consumer exists — revisit per-endpoint if one appears).

**Landed:** spec-invariant unit test (TestAdminRoutesDeclareAdminRole), end-to-end integration test (TestClientCredentialsDeniedOnAdminRoutes), stale-doc corrections in CLAUDE.md / auth/service.go / wiki. Design: docs/superpowers/specs/2026-06-11-399-cc-admin-denial-design.md."
gh issue close 399
```

---

## Self-Review Notes (already applied)

- Spec coverage: invariant test (Task 1), integration test incl. all five /admin/settings ops + per-sub-area writes (Task 2), three doc corrections (Task 3), close with investigation summary (Task 4).
- No placeholders: full test code given; implementer notes flag the two genuinely uncertain framework APIs (`NewClientWithToken`, credential response shape) with concrete fallback instructions.
- Type consistency: `RoleAuthzAdmin` and `buildAuthzTable`/`byMethodPath` match `api/authz_table.go` as read during design.
