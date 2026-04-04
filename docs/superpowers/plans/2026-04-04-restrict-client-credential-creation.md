# Restrict Client Credential Creation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restrict `POST /me/client_credentials` so only administrators and security reviewers (human users) can create client credentials.

**Architecture:** Add a role-check gate at the top of `CreateCurrentUserClientCredential()` that reads `isServiceAccount`, `tmiIsAdministrator`, and `tmiIsSecurityReviewer` from the Gin context. Reject with 403 if the caller is a service account or lacks the required role.

**Tech Stack:** Go, Gin, testify, jq (for OpenAPI spec edits)

**Spec:** `docs/superpowers/specs/2026-04-04-restrict-client-credential-creation-design.md`
**Issue:** [#226](https://github.com/ericfitz/tmi/issues/226)

---

### Task 1: Write failing tests for role restriction

**Files:**
- Modify: `api/client_credentials_handlers_test.go`

- [ ] **Step 1: Add four test cases for role-based access control**

Add these test cases inside the existing `TestCreateCurrentUserClientCredential` function, before the existing test cases (since the role check will be the first thing in the handler). Place them after the `validUserUUID` declaration on line 61.

```go
	t.Run("ForbiddenForNormalUser", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := testCredBody
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "user@example.com", "provider-id", validUserUUID, "tmi", nil)
		c.Set("isServiceAccount", false)
		c.Set("tmiIsAdministrator", false)
		c.Set("tmiIsSecurityReviewer", false)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "forbidden", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "administrators and security reviewers")
	})

	t.Run("ForbiddenForServiceAccount", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := testCredBody
		c, w := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "sa@example.com", "provider-id", validUserUUID, "tmi", nil)
		c.Set("isServiceAccount", true)
		c.Set("tmiIsAdministrator", false)
		c.Set("tmiIsSecurityReviewer", false)

		server.CreateCurrentUserClientCredential(c)

		assert.Equal(t, http.StatusForbidden, w.Code)
		var errResp Error
		err := json.Unmarshal(w.Body.Bytes(), &errResp)
		require.NoError(t, err)
		assert.Equal(t, "forbidden", errResp.Error)
		assert.Contains(t, errResp.ErrorDescription, "service accounts")
	})

	t.Run("AllowedForAdministrator", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := testCredBody
		c, _ := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "admin@example.com", "provider-id", validUserUUID, "tmi", nil)
		c.Set("isServiceAccount", false)
		c.Set("tmiIsAdministrator", true)
		c.Set("tmiIsSecurityReviewer", false)

		server.CreateCurrentUserClientCredential(c)

		// Should pass the role check and proceed to later logic.
		// With nilAuth server, it will eventually hit the 503 (auth service unavailable).
		// Any status other than 403 means the role gate passed.
		assert.NotEqual(t, http.StatusForbidden, c.Writer.Status())
	})

	t.Run("AllowedForSecurityReviewer", func(t *testing.T) {
		server := newTestServerWithNilAuth()
		body := testCredBody
		c, _ := CreateTestGinContextWithBody("POST", "/me/client_credentials", "application/json", []byte(body))
		SetFullUserContext(c, "reviewer@example.com", "provider-id", validUserUUID, "tmi", nil)
		c.Set("isServiceAccount", false)
		c.Set("tmiIsAdministrator", false)
		c.Set("tmiIsSecurityReviewer", true)

		server.CreateCurrentUserClientCredential(c)

		// Should pass the role check and proceed to later logic.
		assert.NotEqual(t, http.StatusForbidden, c.Writer.Status())
	})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestCreateCurrentUserClientCredential`

Expected: `ForbiddenForNormalUser` and `ForbiddenForServiceAccount` fail (get 400 instead of 403 because the role check doesn't exist yet). `AllowedForAdministrator` and `AllowedForSecurityReviewer` pass (they assert `NotEqual 403`, and the handler currently never returns 403 for role issues).

- [ ] **Step 3: Commit failing tests**

```bash
git add api/client_credentials_handlers_test.go
git commit -m "test: add failing tests for client credential role restriction

Tests for issue #226 — verify that POST /me/client_credentials rejects
normal users and service accounts with 403, and allows administrators
and security reviewers through.

Closes #226"
```

---

### Task 2: Implement the role check

**Files:**
- Modify: `api/client_credentials_handlers.go:17` (top of `CreateCurrentUserClientCredential`)

- [ ] **Step 1: Add role authorization gate**

Insert the following code in `CreateCurrentUserClientCredential()` immediately after the logger initialization on line 18 (`logger := slogging.Get().WithContext(c)`), before the existing request body parsing on line 22:

```go
	// Authorization: only administrators and security reviewers can create client credentials
	// Service accounts must have credentials provisioned by admins via /admin/users/{id}/client_credentials
	if IsServiceAccountRequest(c) {
		logger.Warn("Service account attempted to create client credential: %s", GetUserIdentityForLogging(c))
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Client credential creation is not available to service accounts - administrators must provision credentials via the admin API",
		})
		return
	}

	isAdmin, _ := c.Get("tmiIsAdministrator")
	isSecurityReviewer, _ := c.Get("tmiIsSecurityReviewer")
	isAdminBool, _ := isAdmin.(bool)
	isSecurityReviewerBool, _ := isSecurityReviewer.(bool)

	if !isAdminBool && !isSecurityReviewerBool {
		logger.Warn("Non-privileged user attempted to create client credential: %s", GetUserIdentityForLogging(c))
		c.JSON(http.StatusForbidden, Error{
			Error:            "forbidden",
			ErrorDescription: "Only administrators and security reviewers can create client credentials",
		})
		return
	}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `make test-unit name=TestCreateCurrentUserClientCredential`

Expected: All four new tests pass. All existing tests continue to pass (they don't set `tmiIsAdministrator`/`tmiIsSecurityReviewer`, so they will now get 403 instead of their previous expected status).

**Important:** Existing tests that don't set role context will now fail because they hit the new role gate. Each existing test that calls `CreateCurrentUserClientCredential` needs `c.Set("tmiIsAdministrator", true)` added after its `SetFullUserContext` call so it passes the role gate and continues testing its original concern (validation, quota, etc.).

- [ ] **Step 3: Fix existing tests to set admin context**

In `api/client_credentials_handlers_test.go`, for every existing test case in `TestCreateCurrentUserClientCredential` that calls `SetFullUserContext` (all the validation tests: MissingRequestBody, InvalidJSON, EmptyName, WhitespaceOnlyName, NameTooLong, DescriptionTooLong, ExpiredExpiresAt, InvalidUserUUID, EmptyUserUUID, and any others), add this line immediately after `SetFullUserContext(...)`:

```go
		c.Set("tmiIsAdministrator", true)
```

This allows those tests to pass the role gate and continue exercising their intended validation logic.

- [ ] **Step 4: Run tests again to verify all pass**

Run: `make test-unit name=TestCreateCurrentUserClientCredential`

Expected: ALL tests pass.

- [ ] **Step 5: Run full lint and unit test suite**

Run: `make lint && make test-unit`

Expected: No lint errors, all tests pass.

- [ ] **Step 6: Commit implementation**

```bash
git add api/client_credentials_handlers.go api/client_credentials_handlers_test.go
git commit -m "fix(api): restrict client credential creation to admins and security reviewers

POST /me/client_credentials now returns 403 for normal users and
service accounts. Only administrators and security reviewers can
create machine-to-machine credentials. Service account credentials
must be provisioned by admins via /admin/users/{id}/client_credentials.

Fixes #226"
```

---

### Task 3: Update OpenAPI specification

**Files:**
- Modify: `api-schema/tmi-openapi.json`

- [ ] **Step 1: Update POST operation description**

Using jq, update the description of the `POST /me/client_credentials` operation:

```bash
jq '.paths["/me/client_credentials"].post.description = "Creates a new OAuth 2.0 client credential for machine-to-machine authentication. Only administrators and security reviewers can create credentials. Service accounts cannot use this endpoint. The client_secret is ONLY returned once at creation and cannot be retrieved later."' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 2: Update 403 response description**

```bash
jq '.paths["/me/client_credentials"].post.responses["403"].description = "Forbidden - insufficient privileges (requires administrator or security reviewer role) or quota exceeded"' api-schema/tmi-openapi.json > api-schema/tmi-openapi.json.tmp && mv api-schema/tmi-openapi.json.tmp api-schema/tmi-openapi.json
```

- [ ] **Step 3: Validate the OpenAPI spec**

Run: `make validate-openapi`

Expected: Validation passes with no new errors.

- [ ] **Step 4: Regenerate API code**

Run: `make generate-api`

Expected: Code regenerated successfully. The description changes don't affect generated handler signatures.

- [ ] **Step 5: Build and test**

Run: `make build-server && make test-unit`

Expected: Build succeeds, all tests pass.

- [ ] **Step 6: Commit spec update**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "docs(api): document role restriction on client credential creation

Update OpenAPI spec to reflect that POST /me/client_credentials
requires administrator or security reviewer role."
```

---

### Task 4: Integration test and final verification

**Files:** None (uses existing infrastructure)

- [ ] **Step 1: Run integration tests**

Run: `make test-integration`

Expected: All integration tests pass. If any client credential integration tests exist that create credentials as a non-privileged user, they may need updating to use an admin user.

- [ ] **Step 2: Run full lint check**

Run: `make lint`

Expected: No lint errors.

- [ ] **Step 3: Verify build**

Run: `make build-server`

Expected: Clean build.
