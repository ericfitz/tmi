# Design: Restrict Client Credential Creation to Privileged Roles

**Date:** 2026-04-04
**Issue:** [#226](https://github.com/ericfitz/tmi/issues/226)
**Status:** Approved

## Problem

Any authenticated user can create machine-to-machine client credentials via `POST /me/client_credentials`. Client credentials grant full API access as the creating user, so allowing unprivileged users to create them is a privilege escalation risk. Normal users (readers, writers) have no legitimate need for API automation credentials.

## Solution

Add an authorization gate at the top of `CreateCurrentUserClientCredential()` in `api/client_credentials_handlers.go` that rejects requests unless the caller is an **administrator** or **security reviewer** (human users only).

## Authorization Logic

Three checks at the top of the handler, in order:

1. **Block service accounts** — if `isServiceAccount` is `true` in the Gin context, return **403 Forbidden**. Service account credentials must be provisioned by admins via `/admin/users/{id}/client_credentials`.

2. **Allow administrators** — if `tmiIsAdministrator` is `true` in the Gin context, proceed to existing handler logic.

3. **Allow security reviewers** — if `tmiIsSecurityReviewer` is `true` in the Gin context, proceed to existing handler logic.

4. **Deny everyone else** — return **403 Forbidden** with a JSON error body: `"only administrators and security reviewers can create client credentials"`.

## Error Response

Standard 403 using the existing `ErrorResponse` pattern in the codebase. The error message clearly states the required roles.

## OpenAPI Spec Update

- Update the `POST /me/client_credentials` operation `description` to document the role restriction.
- Update the existing 403 response description to cover both quota exceeded and insufficient privileges.

## Scope — What Doesn't Change

- `GET /me/client_credentials` — any authenticated user can list their own credentials (returns empty if none).
- `DELETE /me/client_credentials/{id}` — any authenticated user can delete their own credentials.
- `/admin/users/{id}/client_credentials/*` — admin endpoints unchanged.
- Quota checks — still enforced after the role gate passes.

## Testing

- Unit test: verify 403 for a normal user (no admin or security reviewer claim) attempting POST.
- Unit test: verify 201 for an administrator.
- Unit test: verify 201 for a security reviewer.
- Unit test: verify 403 for a service account.
- Existing tests for GET and DELETE remain unchanged.

## Files Modified

1. `api/client_credentials_handlers.go` — add role check to `CreateCurrentUserClientCredential()`
2. `api-schema/tmi-openapi.json` — update POST operation description and 403 response
3. `api/client_credentials_handlers_test.go` (or new test file) — add unit tests for role restriction
