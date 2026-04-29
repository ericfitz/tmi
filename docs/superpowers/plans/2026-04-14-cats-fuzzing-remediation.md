# CATS Fuzzing Remediation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all true-positive CATS fuzzing findings — 500 errors (server bugs), type coercion issues, method restriction gaps, and SSRF false-positive classification — and mark confirmed false positives so they don't recur.

**Architecture:** Each task targets one endpoint group. 500 errors are highest priority. Non-500 issues are lower priority. False positives are documented with rationale for future CATS runs.

**Tech Stack:** Go, Gin, oapi-codegen, OpenAPI 3.0.3, GORM, PostgreSQL

---

## Findings Summary

| Category | Count | Endpoints | Action |
|----------|-------|-----------|--------|
| **500 errors (MUST FIX)** | 15 | DELETE /admin/users/{uuid}, DELETE /admin/users/{uuid}/client_credentials/{cred_id} | Fix server bugs |
| **Type coercion** | 12 | POST /admin/groups, PATCH /admin/users/{uuid}, POST /me/client_credentials | Investigate; add OpenAPI `type: string` enforcement if needed |
| **Method restriction** | 2 | PUT/PATCH /saml/slo returning 200 | Fix: should return 405 |
| **SSRF false positives** | 3 | GET /oauth2/authorize with localhost callback URLs | Classify as false positives (SSRF validation already exists) |
| **OAuth endpoint behavior** | 105 | /oauth2/introspect, /token, /callback, /refresh | Correct behavior per RFCs — classify as false positives |
| **Webhook edge cases** | 6 | /admin/webhooks/.../test, /webhook-deliveries/{id} | Correct behavior — classify as false positives |
| **Warning: correct validation** | 71 | /saml/slo, /oauth2/authorize, /.well-known/*, chat/refresh_sources, documents/request_access | Correct behavior — no action needed |

---

## Task 1: Fix DELETE /admin/users/{internal_uuid} returning 500 (CRITICAL)

**Files:**
- Modify: `api/admin_user_handlers.go:271-309`
- Test: `api/admin_user_handlers_test.go` (add test for DELETE)

**Context:** ALL 12 CATS tests (HappyPath, AcceptLanguageHeaders, ExtraHeaders, NewFields, CheckSecurityHeaders, InvalidReferencesFields) against DELETE /admin/users/{internal_uuid} return HTTP 500. Even HappyPath fails, meaning the handler fundamentally cannot complete a delete operation during CATS testing. The `openapi_types.UUID` is a type alias for `uuid.UUID` so this is NOT a type mismatch.

Possible root causes to investigate:
1. The CATS-authenticated user (charlie) might be trying to delete themselves, triggering an unhandled error
2. The detached context (`context.Background()`) might lose database connection context
3. The cascade delete in `deleteUserCore` might fail on FK constraints for users with complex relationships
4. The `GlobalUserStore` might not be properly initialized for the delete path
5. The error string from `deleteUserCore` might not contain "user not found" in some failure paths

- [ ] **Step 1: Reproduce the 500 locally**

Start the dev environment and attempt a DELETE request as the admin user (charlie):

```bash
make start-dev
# Get charlie's JWT
curl -X POST http://localhost:8079/flows/start -H 'Content-Type: application/json' -d '{"userid": "charlie"}'
# Wait, then get token
TOKEN=$(curl -s "http://localhost:8079/creds?userid=charlie" | jq -r '.access_token')

# List users to find a deletable UUID
curl -s -H "Authorization: Bearer $TOKEN" http://localhost:8080/admin/users | jq '.items[0].internal_uuid'

# Attempt DELETE
curl -v -X DELETE -H "Authorization: Bearer $TOKEN" http://localhost:8080/admin/users/<uuid>
```

Check `logs/tmi.log` for the error message that triggers the 500.

- [ ] **Step 2: Fix the root cause**

Based on the investigation from Step 1, fix the handler. At minimum, the handler should never return 500 for foreseeable errors. Ensure:

1. All error paths from `GlobalUserStore.Delete()` return appropriate 4xx responses
2. The error matching at line 286 uses a more robust check than `strings.Contains(err.Error(), "user not found")`
3. Add a self-deletion guard (admin cannot delete their own account — return 409 Conflict)

Example fix pattern for `api/admin_user_handlers.go`:

```go
func (s *Server) DeleteAdminUser(c *gin.Context, internalUuid openapi_types.UUID) {
    logger := slogging.Get().WithContext(c)

    // Prevent self-deletion
    actorUserID := c.GetString("userInternalUUID")
    actorEmail := c.GetString("userEmail")
    if actorUserID == internalUuid.String() {
        HandleRequestError(c, &RequestError{
            Status:  http.StatusConflict,
            Code:    "self_deletion",
            Message: "Cannot delete your own account",
        })
        return
    }

    deleteCtx, deleteCancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer deleteCancel()
    stats, err := GlobalUserStore.Delete(deleteCtx, internalUuid)
    if err != nil {
        errMsg := err.Error()
        if strings.Contains(errMsg, "user not found") {
            HandleRequestError(c, &RequestError{
                Status:  http.StatusNotFound,
                Code:    "not_found",
                Message: "User not found",
            })
        } else if strings.Contains(errMsg, "context") {
            logger.Error("Delete user timeout: %v", err)
            c.Header("Retry-After", "60")
            HandleRequestError(c, &RequestError{
                Status:  http.StatusServiceUnavailable,
                Code:    "timeout",
                Message: "User deletion timed out — please retry",
            })
        } else {
            logger.Error("Failed to delete user: %v", err)
            HandleRequestError(c, &RequestError{
                Status:  http.StatusInternalServerError,
                Code:    "server_error",
                Message: "Failed to delete user",
            })
        }
        return
    }

    logger.Info("[AUDIT] Admin user deletion: internal_uuid=%s, email=%s, deleted_by=%s (email=%s), transferred=%d, deleted=%d",
        internalUuid, stats.UserEmail, actorUserID, actorEmail, stats.ThreatModelsTransferred, stats.ThreatModelsDeleted)

    c.Status(http.StatusNoContent)
}
```

- [ ] **Step 3: Write a unit test for the DELETE handler**

Add a test in `api/admin_user_handlers_test.go` that covers:
- Successful deletion (204)
- User not found (404)
- Self-deletion attempt (409)

- [ ] **Step 4: Run unit tests**

Run: `make test-unit name=TestDeleteAdminUser`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/admin_user_handlers.go api/admin_user_handlers_test.go
git commit -m "fix(api): fix DELETE /admin/users returning 500 for all requests"
```

---

## Task 2: Fix DELETE /admin/users/{uuid}/client_credentials/{cred_id} returning 500 (CRITICAL)

**Files:**
- Modify: `api/admin_user_credentials_handlers.go:223-258`
- Test: `api/admin_user_credentials_handlers_test.go` (add test)

**Context:** 3 CATS tests (all InvalidReferencesFields) return 500. CATS sends paths with `?`, `??`, `/?/` appended to the user UUID portion. The fuzzed paths like `/admin/users/<uuid>?/client_credentials/{credential_id}` may cause Gin to parse the UUID incorrectly or pass a malformed UUID that downstream code doesn't handle.

- [ ] **Step 1: Reproduce the 500 locally**

Test with the same fuzzed paths CATS used:

```bash
TOKEN=$(curl -s "http://localhost:8079/creds?userid=charlie" | jq -r '.access_token')

# Normal path
curl -v -X DELETE -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/admin/users/<uuid>/client_credentials/<cred_id>"

# Fuzzed paths (what CATS sent)
curl -v -X DELETE -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8080/admin/users/<uuid>?/client_credentials/<cred_id>"
```

Check `logs/tmi.log` for errors.

- [ ] **Step 2: Fix the handler**

The `getAutomationUser` helper at line 19 passes `openapi_types.UUID` to `GlobalUserStore.Get()`. While this is technically the same type (alias), ensure the helper handles all error paths without returning 500. Also at line 245, ensure `ccService.Delete()` errors are handled.

The fix should ensure that if Gin passes a garbled UUID (from the fuzzed path), the handler returns 400 or 404, not 500.

- [ ] **Step 3: Write a unit test**

Add test coverage for `DeleteAdminUserClientCredential` covering:
- Successful deletion (204)
- User not found (404)
- Credential not found (404)
- Invalid UUID format (400)

- [ ] **Step 4: Run unit tests**

Run: `make test-unit name=TestDeleteAdminUserClientCredential`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/admin_user_credentials_handlers.go api/admin_user_credentials_handlers_test.go
git commit -m "fix(api): fix DELETE /admin/users/{uuid}/client_credentials returning 500"
```

---

## Task 3: Fix PUT/PATCH /saml/slo returning 200 instead of 405

**Files:**
- Investigate: `api/api.go` (generated router registration, lines ~19594-19595)
- Investigate: `api/server.go` (middleware chain)
- Possibly modify: `api/server.go` or add method-restriction middleware

**Context:** CATS sends PUT and PATCH requests to `/saml/slo` and gets 200 OK. The router only registers GET and POST for this route. The response should be 405 Method Not Allowed. This suggests either a wildcard handler, method fallthrough, or middleware is catching these methods.

- [ ] **Step 1: Investigate the root cause**

Check whether Gin is falling through to a catch-all route or if middleware is handling the request before routing:

```bash
# Test locally
curl -v -X PUT http://localhost:8080/saml/slo -d 'SAMLRequest=test'
curl -v -X PATCH http://localhost:8080/saml/slo -d 'SAMLRequest=test'
```

Check if there's a `NoRoute` or `NoMethod` handler, or if the HEAD method middleware at `api/head_method_middleware.go` is involved.

- [ ] **Step 2: Fix the method restriction**

If Gin's `HandleMethodNotAllowed` is not enabled, enable it in the router setup in `api/server.go`:

```go
router.HandleMethodNotAllowed = true
```

This tells Gin to return 405 for routes that exist but don't support the requested method.

- [ ] **Step 3: Verify the fix**

```bash
curl -v -X PUT http://localhost:8080/saml/slo -d 'SAMLRequest=test'
# Expected: 405 Method Not Allowed
```

- [ ] **Step 4: Run unit tests**

Run: `make test-unit`
Expected: PASS (ensure no regressions)

- [ ] **Step 5: Commit**

```bash
git add api/server.go
git commit -m "fix(api): return 405 for unsupported HTTP methods on /saml/slo"
```

---

## Task 4: Investigate type coercion for numeric values in string fields

**Files:**
- Investigate: `api/admin_group_handlers.go` (POST /admin/groups)
- Investigate: `api/admin_user_handlers.go` (PATCH /admin/users/{uuid})
- Investigate: `api/client_credentials_handlers.go` (POST /me/client_credentials)
- Investigate: OpenAPI spec `api-schema/tmi-openapi.json`

**Context:** CATS sends numeric values (e.g., `9223372036854775807`) for string fields like `name`, `group_name`, `description`. The server accepts them (201/200). This is technically valid JSON behavior (Go's JSON decoder converts numbers to strings if the target type is `string`), but CATS flags it because it may indicate missing input validation.

- [ ] **Step 1: Determine if this is a real problem**

Check whether the OpenAPI spec defines these fields with `type: string`. If so, the OpenAPI validation middleware should reject numeric values. If it doesn't, the issue is in middleware configuration, not handler code.

```bash
jq '.components.schemas.GroupInput.properties.name' api-schema/tmi-openapi.json
jq '.components.schemas.GroupInput.properties.group_name' api-schema/tmi-openapi.json
jq '.components.schemas.AdminUserUpdate.properties.name' api-schema/tmi-openapi.json
jq '.components.schemas.ClientCredentialInput.properties.name' api-schema/tmi-openapi.json
```

- [ ] **Step 2: Decide on action**

**If the OpenAPI spec correctly defines `type: string`:**
The OpenAPI validation middleware should be rejecting these. Investigate why it isn't — the middleware may not be strict about type coercion. This is a known limitation of many OpenAPI validators that accept JSON numbers for string fields.

**Decision point:** Is accepting numeric values in string fields a security risk? For `name` and `description` fields, the practical risk is low — the number becomes a string like `"9223372036854775807"`. The user would see a nonsensical name but no privilege escalation or data corruption occurs.

**Recommended action:** Accept this as low-risk and document as a CATS false positive. The OpenAPI spec is correct; Go's JSON marshaling is well-defined for this case. If stricter validation is desired, add explicit string-type checks in handlers, but this is not required.

- [ ] **Step 3: Document the decision**

If accepting as false positive, add to CATS configuration or analysis notes. If fixing, add validation to the handlers.

- [ ] **Step 4: Commit**

```bash
git commit -m "docs(test): document type coercion CATS findings as accepted behavior"
```

---

## Task 5: Classify SSRF findings on /oauth2/authorize as false positives

**Files:**
- Investigate: `auth/handlers_oauth.go` (Authorize handler)
- Investigate: existing SSRF validation (search for `ssrf` or `validateURL`)

**Context:** CATS sends SSRF payloads in the `client_callback` URL field: `http://localhost:6379` (Redis), `http://localhost:5432` (PostgreSQL), `http://localhost:22` (SSH). Response codes 952/957 indicate CATS couldn't connect — the server redirected to the callback URL and CATS tried to follow.

The `client_callback` parameter controls where the OAuth redirect goes after authorization. This is a client-specified redirect and the server intentionally redirects there.

- [ ] **Step 1: Verify SSRF protections exist**

Check if TMI already has SSRF validation on callback URLs (there is a prior plan `2026-04-05-ssrf-uri-validation.md`):

```bash
grep -r "ssrf\|SSRF\|validateCallback\|allowed.*callback\|callback.*valid" auth/ api/ --include="*.go"
```

- [ ] **Step 2: Assess the finding**

The `client_callback` is a user-provided URL for OAuth redirect. The server does a 302 redirect — it does NOT make a server-side request to this URL. This is not an SSRF vulnerability because:
1. The server never fetches the callback URL — it returns it in a `Location` header
2. The browser makes the request, not the server
3. This is standard OAuth 2.0 behavior per RFC 6749

- [ ] **Step 3: Document as false positive**

Add a note explaining why these SSRF findings are false positives for the authorize endpoint.

- [ ] **Step 4: Commit**

```bash
git commit -m "docs(test): classify /oauth2/authorize SSRF CATS findings as false positives"
```

---

## Task 6: Classify /oauth2/introspect findings as RFC-compliant behavior

**Files:**
- No code changes needed
- Document: CATS false positive analysis

**Context:** 82 CATS errors on POST /oauth2/introspect — ALL return 200. Per RFC 7662 Section 2.2, the introspection endpoint MUST return HTTP 200 with `{"active": false}` for any invalid, expired, or unrecognizable token. The endpoint correctly:
- Returns `{"active": false}` for numeric tokens, boolean tokens, mass assignment payloads
- Returns `{"active": false}` for oversized strings, unicode strings, zero-width chars
- Responds to PUT/PATCH methods (only 2 of the 82 — investigate if needed)

CATS expects a 4xx for malformed input, but RFC 7662 explicitly requires 200.

- [ ] **Step 1: Verify the PUT/PATCH behavior**

The HttpMethods fuzzer found PUT and PATCH return 200. Check if `/oauth2/introspect` should only accept POST (it should, per RFC). This is the same issue as Task 3 (/saml/slo) — `HandleMethodNotAllowed` should fix both.

- [ ] **Step 2: Classify the 80 remaining introspect errors as false positives**

These are correct RFC 7662 behavior. Add `x-public-endpoint` or equivalent vendor extension if not already present, or document in CATS analysis.

- [ ] **Step 3: Commit**

```bash
git commit -m "docs(test): classify /oauth2/introspect CATS findings as RFC 7662 compliant"
```

---

## Task 7: Classify remaining OAuth/webhook findings as false positives

**Files:**
- No code changes needed
- Document: CATS false positive analysis

**Context:** These findings are all correct server behavior:

| Endpoint | Count | Response | Why Correct |
|----------|-------|----------|-------------|
| POST /oauth2/token | 16 | 400 | Missing/invalid required fields → 400 per OAuth spec |
| POST /oauth2/callback | 3 | 400 | Missing code/state params → 400 |
| POST /oauth2/refresh | 4 | 400 | Missing/invalid refresh_token → 400 |
| POST /admin/webhooks/.../test | 1 | 404 | Webhook subscription not found → 404 |
| POST /admin/webhooks/.../test | 1 | 400 | Oversized event_type → 400 (OpenAPI validation) |
| GET /webhook-deliveries/{id} | 4 | 404 | Auth bypass / invalid headers → delivery not found → 404 |

CATS flags these as "errors" because it expected 2xx for happy-path tests, but these endpoints require valid tokens/parameters that CATS can't generate.

- [ ] **Step 1: Document all as false positives**

Create a summary document or update CATS configuration to suppress these known false positives in future runs.

- [ ] **Step 2: Commit**

```bash
git commit -m "docs(test): classify OAuth and webhook CATS findings as expected behavior"
```

---

## Task 8: Run CATS re-validation after fixes

**Files:**
- No code changes
- Verify: all fixes from Tasks 1-7

- [ ] **Step 1: Run CATS fuzzing**

```bash
make cats-fuzz
```

- [ ] **Step 2: Analyze results**

```bash
make analyze-cats-results
```

- [ ] **Step 3: Verify 500 errors are eliminated**

```bash
sqlite3 test/outputs/cats/cats-results.db \
  "SELECT COUNT(*) FROM test_results_filtered_view WHERE result = 'error' AND response_code = 500;"
# Expected: 0
```

- [ ] **Step 4: Verify PUT/PATCH method restriction**

```bash
sqlite3 test/outputs/cats/cats-results.db \
  "SELECT path, fuzzer, response_code FROM test_results_filtered_view WHERE fuzzer = 'HttpMethods' AND result = 'error';"
# Expected: no 200 responses for undocumented methods
```

- [ ] **Step 5: Commit any additional fixes**

If new issues surface, fix and commit before closing.

---

## False Positive Summary (No Action Required)

These CATS warnings represent correct server behavior and need no code changes:

| Path | Warnings | Response | Rationale |
|------|----------|----------|-----------|
| /.well-known/oauth-authorization-server | 3 | 200 | Discovery endpoints accept any request |
| /.well-known/openid-configuration | 3 | 200 | Discovery endpoints accept any request |
| /oauth2/authorize | 4 | 404 | Unicode IDP name → provider not found → 404 |
| /saml/slo | 19 | 404 | Fuzzed SAMLRequest → invalid SAML → 404 |
| /threat_models/.../refresh_sources | 24 | 400 | Invalid JSON body / random path vars → 400 |
| /threat_models/.../request_access | 24 | 400 | Invalid JSON body / random path vars → 400 |

**Total: 77 warnings, all correct behavior.**
