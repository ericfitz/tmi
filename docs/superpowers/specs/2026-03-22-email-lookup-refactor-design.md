# Replace Internal Email-Based User Lookups with UUID/Provider ID

**Issue:** [#211](https://github.com/ericfitz/tmi/issues/211)
**Date:** 2026-03-22
**Scope:** Category 3 only — internal lookups that use email as a key when better identifiers are available

## Problem

Two places in the TMI server code use email addresses as lookup keys for internal operations, even though unambiguous identifiers (provider+provider_user_id or internal UUID) are already available in context. Email addresses are ambiguous: users can change them, multiple providers can share them, and they create unnecessary coupling between display data and identity resolution.

## Sites to Fix

### Site 1: `api/auth_service_adapter.go:143` — Me() handler

**Current behavior:** When the `/me` endpoint is called and the user object isn't already in the Gin context (set by auth middleware), the handler extracts `userEmail` from context and calls `service.GetUserByEmail()` to look up the user.

**Problem:** Email-based lookup is ambiguous. If two users share an email across providers, or if a user's email changed since the JWT was issued, this returns the wrong user or fails.

**Fix:** Replace with provider+provider_user_id lookup. The JWT middleware already sets `userID` (provider_user_id) and `userProvider` in the Gin context. Use `service.GetUserByProviderID(provider, providerUserID)` instead — the same pattern used by `fetchAndSetUserObject` in `cmd/server/jwt_auth.go`.

**Fallback:** If provider or provider_user_id aren't available in context, return 401 Unauthorized (same behavior as the current email-missing path).

### Site 2: `api/addon_invocation_handlers.go:78` — providerUserID assignment

**Current behavior:** Line 78 assigns `providerUserID := userEmail` with the comment "Temporary: use email until we fetch from auth.User". This email value is then used as the provider user ID for addon invocation tracking.

**Problem:** Using email as provider_user_id is incorrect — these are different fields with different semantics. The provider_user_id is the OAuth provider's stable identifier (from JWT `sub` claim), while email is mutable display data.

**Fix:** Use `GetUserProviderID(c)` from `api/user_context_utils.go` to extract the actual provider_user_id from context. This value is set by the JWT middleware from the `sub` claim and is already available.

## What Is NOT Changing

- **Login identity resolution** (tiered matching in `auth/handlers_oauth_user.go`): The provider+email and email-only fallback tiers are intentional for completing sparse user records on first login.
- **JWT middleware fallback** (`cmd/server/jwt_auth.go:289`): The `GetUserByEmail` fallback when provider+provider_user_id lookup fails is an intentional safety net.
- **Authorization subject matching** (`api/auth_utils.go`): The flexible matching that accepts email as one of three identifiers is by design for backward compatibility.
- **OpenAPI specification**: No API changes.
- **Database schema**: No schema changes.

## Testing

- Both fixes use existing lookup functions (`GetUserByProviderID`, `GetUserProviderID`) that are already tested.
- Existing unit tests for these handlers set both `userEmail` and `userID` in the test context, so they will exercise the corrected paths.
- No new tests required — existing test coverage validates the corrected code paths.

## Risk Assessment

**Low risk.** Both changes replace an ambiguous lookup (email) with an unambiguous one (provider+provider_user_id) that the upstream middleware already provides. The fallback behavior (401 on missing identity) is unchanged.
