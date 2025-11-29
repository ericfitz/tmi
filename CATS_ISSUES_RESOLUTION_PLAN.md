# CATS Fuzz Testing Issues - Resolution Plan

## Executive Summary

Analysis of 173 CATS fuzz test reports for the `/invocations` endpoint revealed 5 major categories of issues. While 172 tests show "success" status, 154 tests exhibit unexpected behavior that should be addressed to improve API robustness and compliance with best practices.

**Endpoint Tested**: `GET /invocations` (Add-on invocations listing)

## Issue Categories (Priority Order)

### Priority 1: Accept Header Validation (30 occurrences)
**Issue**: Server returns 200 for unsupported Accept headers instead of 406/415
**Expected**: 406 Not Acceptable or 415 Unsupported Media Type
**Actual**: 200 OK with JSON response

**Examples**:
- `Accept: application/pdf` → 200 (should be 406)
- `Accept: image/jpeg` → 200 (should be 406)
- `Accept: application/xml` → 200 (should be 406)

**Impact**: Medium - Clients may receive unexpected response formats

**Resolution**:
1. Add Accept header validation middleware to check for `application/json`
2. Return 406 for unsupported Accept headers with error message listing supported types
3. Document 406 response in OpenAPI spec for `/invocations` endpoint

**Files to Modify**:
- `api/middleware.go` (add Accept header validation middleware)
- `docs/reference/apis/tmi-openapi.json` (document 406 response)
- `api/server.go` (register middleware)

**Code Location**: Apply globally or to specific endpoints requiring strict Accept validation

---

### Priority 2: Malformed Query Parameter Handling (44 occurrences)
**Issue**: Server returns 400 for malformed inputs but expects fuzzer to receive 2XX
**Expected**: 200/201/202/204 (by CATS fuzzer)
**Actual**: 400 Bad Request

**Examples**:
- `addon_id=552c0fb7-36ff-473fజ్ఞ‌ా-9249-b...` (Abugidas characters) → 400
- `limit=̵̡͚̬̱̤...` (Zalgo text) → 400
- `offset=\u200b` (zero-width characters) → 400
- `status=OTTHPUDTMOAQVSHGTFLFQMRDIRCQZE...` (oversized string) → 400
- `addon_id=` (empty UUID) → 400

**Impact**: Low - Server is actually behaving correctly by rejecting invalid input

**Resolution**:
**RECOMMENDATION**: Configure CATS to expect 400 for invalid query parameters instead of changing server behavior. The current 400 responses are correct and desirable.

**Alternative (if strict 2XX compliance desired)**:
1. Add lenient query parameter parsing that ignores invalid values
2. Use default values for malformed parameters
3. This is **NOT RECOMMENDED** as it weakens input validation

**Action**: Update CATS configuration to expect 400 for malformed query parameters

---

### Priority 3: Undocumented 400 Response (38 occurrences)
**Issue**: Server correctly returns 400, but it's not documented in OpenAPI spec
**Expected**: 400 (by fuzzer, but undocumented)
**Actual**: 400 Bad Request (matches fuzzer expectation but not in spec)

**Documented Responses**: 200, 401, 429
**Missing**: 400

**Impact**: Medium - API consumers lack documentation for validation errors

**Resolution**:
1. Add 400 Bad Request response to OpenAPI spec
2. Document common validation error scenarios
3. Include example error response for malformed query parameters

**Files to Modify**:
- `docs/reference/apis/tmi-openapi.json` (add 400 response schema)

**Example Response Documentation**:
```json
{
  "400": {
    "description": "Bad Request - Invalid query parameters",
    "content": {
      "application/json": {
        "schema": {
          "$ref": "#/components/schemas/Error"
        },
        "examples": {
          "invalid_uuid": {
            "value": {
              "msg": "Invalid format for parameter addon_id: invalid UUID"
            }
          },
          "invalid_enum": {
            "value": {
              "msg": "Invalid value for parameter status: must be one of [pending, in_progress, completed, failed]"
            }
          }
        }
      }
    }
  }
}
```

---

### Priority 4: HTTP Method Validation (34 occurrences)
**Issue**: Server returns 400 for unsupported HTTP methods instead of 405
**Expected**: 405 Method Not Allowed
**Actual**: 400 Bad Request

**Examples**:
- `DIFF /invocations` → 400 (should be 405)
- `VERIFY /invocations` → 400 (should be 405)
- `PUBLISH /invocations` → 400 (should be 405)
- `VIEW /invocations` → 400 (should be 405)
- `PURGE /invocations` → 400 (should be 405)

**Impact**: Low - Incorrect HTTP status code for method validation

**Resolution**:
1. Configure Gin router to return 405 for unsupported methods
2. Add `Allow` header listing supported methods (GET)
3. Document 405 response in OpenAPI spec

**Files to Modify**:
- `api/server.go` (configure Gin router's HandleMethodNotAllowed)
- `docs/reference/apis/tmi-openapi.json` (document 405 response)

**Code Example**:
```go
router.HandleMethodNotAllowed = true
router.NoMethod(func(c *gin.Context) {
    c.Header("Allow", "GET")
    c.JSON(405, gin.H{"msg": "Method not allowed"})
})
```

---

### Priority 5: Authentication Error Information Leak (1 occurrence)
**Issue**: 401 response reveals internal implementation details
**Scenario**: Request without Authorization header
**Response**:
```json
{
  "details": null,
  "error": "unauthorized",
  "error_description": "missing Authorization header"
}
```

**Impact**: Low - Minor information disclosure

**Resolution**:
1. Simplify 401 error messages to avoid revealing authentication mechanism details
2. Use generic message: `"Authentication required"`
3. Remove `error_description` field or use standardized OAuth2 error codes only

**Files to Modify**:
- `auth/handlers.go` or JWT middleware
- Standardize error responses to match OpenAPI Error schema

**Recommended Response**:
```json
{
  "msg": "Authentication required"
}
```

---

## Implementation Priority

### Phase 1: Quick Wins (1-2 hours)
1. ✅ **Document 400 response** in OpenAPI spec (Priority 3)
2. ✅ **Document 406 response** in OpenAPI spec (Priority 1)
3. ✅ **Document 405 response** in OpenAPI spec (Priority 4)

### Phase 2: Code Changes (2-4 hours)
1. ✅ **Add Accept header validation** middleware (Priority 1)
2. ✅ **Configure HTTP method validation** to return 405 (Priority 4)
3. ✅ **Simplify authentication errors** (Priority 5)

### Phase 3: Validation & Testing (1-2 hours)
1. Run CATS again to verify fixes
2. Add unit tests for new middleware
3. Validate OpenAPI spec changes

### Phase 4: Optional Enhancement
1. Configure CATS to expect 400 for malformed inputs (Priority 2)
2. Update CATS ignore rules if needed

---

## Success Criteria

After implementing fixes, CATS should report:
- ✅ 30 Accept header tests pass (406 response documented and returned)
- ✅ 34 HTTP method tests pass (405 response documented and returned)
- ✅ 38 validation error tests pass (400 response documented)
- ✅ 1 auth error test passes (simplified error message)
- ⚠️ 44 malformed input tests remain as "unexpected" (acceptable - server correctly rejects invalid input)

**Target**: Reduce unexpected behaviors from 154 to 44 (71% improvement)

---

## Files to Modify Summary

1. **[docs/reference/apis/tmi-openapi.json](docs/reference/apis/tmi-openapi.json)** - Add 400, 405, 406 responses
2. **[api/middleware.go](api/middleware.go)** - Add Accept header validation middleware
3. **[api/server.go](api/server.go)** - Configure method not allowed handler
4. **[auth/handlers.go](auth/handlers.go)** or JWT middleware - Simplify error messages

---

## CATS Configuration Recommendation

Update CATS to treat 400 as expected for malformed query parameters:

```yaml
# cats-config.yml (if using config file)
ignoreResponseCodes:
  - 400  # For malformed input tests
```

Or use command-line flag:
```bash
cats --ignore-response-codes 400
```

---

## References

- CATS Report Directory: `cats-report/`
- Total Reports Analyzed: 173
- OpenAPI Spec: [docs/reference/apis/tmi-openapi.json](docs/reference/apis/tmi-openapi.json)
- Endpoint: `GET /invocations`

---

## Notes

- **19 tests fully pass** - These should continue to pass after fixes
- **Server validation is working correctly** - Most "issues" are actually correct behavior
- **Focus on documentation and HTTP compliance** - Main gaps are in OpenAPI spec completeness and RFC 7231 compliance (405 vs 400)
