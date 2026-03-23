# Email Lookup Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace two internal email-based user lookups with provider+provider_user_id lookups to eliminate ambiguous identity resolution.

**Architecture:** Two targeted fixes in existing handlers. Site 1 replaces a `GetUserByEmail()` call with `GetUserByProviderID()`. Site 2 captures an already-available return value instead of using email as a substitute.

**Tech Stack:** Go, Gin framework, existing auth service methods

**Spec:** `docs/superpowers/specs/2026-03-22-email-lookup-refactor-design.md`

---

### Task 1: Fix auth_service_adapter.go — Me() handler

**Files:**
- Modify: `api/auth_service_adapter.go:111-154`

- [ ] **Step 1: Replace email-based lookup with provider+provider_user_id lookup**

Replace the entire fallback block (lines 111-154) that extracts `userEmail` and calls `GetUserByEmail()`. The new code should:
1. Extract `userProvider` from `c.Get("userProvider")`
2. Extract `userID` (provider_user_id) from `c.Get("userID")`
3. Return 401 if either is missing
4. Call `a.service.GetUserByProviderID(ctx, provider, providerUserID)`
5. On error, return 404 (same as current behavior)
6. Set user in context and delegate to `a.handlers.Me(c)`

Replace lines 111-154 with:

```go
	// User not in context, try to fetch using provider + provider_user_id from JWT middleware
	providerInterface, exists := c.Get("userProvider")
	if !exists {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated - no provider in context",
		})
		return
	}
	provider, ok := providerInterface.(string)
	if !ok || provider == "" {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid provider context",
		})
		return
	}

	providerUserIDInterface, exists := c.Get("userID")
	if !exists {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Authentication required")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "User not authenticated - no provider user ID in context",
		})
		return
	}
	providerUserID, ok := providerUserIDInterface.(string)
	if !ok || providerUserID == "" {
		SetWWWAuthenticateHeader(c, WWWAuthInvalidToken, "Invalid authentication token")
		c.JSON(http.StatusUnauthorized, Error{
			Error:            "unauthorized",
			ErrorDescription: "Invalid user context",
		})
		return
	}

	// Use the existing auth service to fetch user
	if a.service == nil {
		slogging.Get().WithContext(c).Error("AuthServiceAdapter: Auth service not available for user lookup (provider: %s, provider_user_id: %s)", provider, providerUserID)
		c.JSON(http.StatusInternalServerError, Error{
			Error:            "server_error",
			ErrorDescription: "Auth service unavailable",
		})
		return
	}

	// Fetch user by provider + provider_user_id (unambiguous lookup)
	user, err := a.service.GetUserByProviderID(c.Request.Context(), provider, providerUserID)
	if err != nil {
		c.JSON(http.StatusNotFound, Error{
			Error:            "not_found",
			ErrorDescription: "User not found",
		})
		return
	}

	// Set user in context and delegate to handlers
	c.Set(string(auth.UserContextKey), user)
	a.handlers.Me(c)
```

- [ ] **Step 2: Run lint**

Run: `make lint`
Expected: PASS (no new lint issues)

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/auth_service_adapter.go
git commit -m "fix(api): replace email-based user lookup with provider ID in Me() handler

Refs #211"
```

---

### Task 2: Fix addon_invocation_handlers.go — providerUserID assignment

**Files:**
- Modify: `api/addon_invocation_handlers.go:38,78`

- [ ] **Step 1: Capture provider_user_id from ValidateAuthenticatedUser return value**

At line 38, `ValidateAuthenticatedUser(c)` returns `(email, providerId, role, error)` but the second return value (providerId) is discarded with `_`. Capture it instead:

Change line 38 from:
```go
	userEmail, _, _, err := ValidateAuthenticatedUser(c)
```
to:
```go
	userEmail, providerID, _, err := ValidateAuthenticatedUser(c)
```

Then replace line 78 from:
```go
	providerUserID := userEmail // Temporary: use email until we fetch from auth.User
```
to:
```go
	providerUserID := providerID
```

- [ ] **Step 2: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 3: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/addon_invocation_handlers.go
git commit -m "fix(api): use provider_user_id instead of email for addon invocation tracking

Refs #211"
```

---

### Task 3: Final verification and close

- [ ] **Step 1: Run full build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 2: Run unit tests**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Squash or combine commits and close issue**

Create final commit referencing the issue for closure:

```bash
git commit --allow-empty -m "fix: replace email-based internal user lookups with provider ID

Two sites were using email addresses as lookup keys when unambiguous
provider+provider_user_id identifiers were already available:

- auth_service_adapter.go Me() handler: replaced GetUserByEmail() with
  GetUserByProviderID() using provider and provider_user_id from context
- addon_invocation_handlers.go: captured provider_user_id from
  ValidateAuthenticatedUser() instead of using email as substitute

Fixes #211"
```
