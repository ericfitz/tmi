# CATS Fuzzing Findings Analysis and Remediation Plan

<!-- Migrated from: docs/developer/testing/cats-findings-plan.md on 2025-01-24 -->
<!-- Note: This document contains early CATS analysis. For the latest remediation status, see cats-remediation-plan.md -->

**Generated**: 2026-01-19
**Updated**: 2026-01-19 (with fixes implemented)
**Total Tests**: 64,986
**OAuth False Positives Filtered**: 34,686

## Executive Summary

| Category | Count | Action | Status |
|----------|-------|--------|--------|
| Successes | 30,195 | No action needed | ✓ |
| OAuth False Positives | 34,686 | Already filtered (expected behavior) | ✓ |
| **True Errors** | **13** | Investigate and fix | ✓ FIXED |
| **Warnings** | **92** | Analyze and triage | ✓ Documented |

The vast majority of findings are either successes or correctly identified OAuth false positives.

---

## True Errors (13 total) - RESOLVED

### 1. CheckDeletedResourcesNotAvailable: `/admin/groups/{internal_uuid}` GET (10 errors) - FIXED ✓

**Issue**: After deleting a group, GET requests return 200 instead of 404.

**Root Cause Analysis (CONFIRMED)**: The `DeleteGroupAndData` function was looking up groups by `(provider, group_name)` instead of by `internal_uuid`. When CATS created multiple groups with the same `group_name` (via different fuzzers), the DELETE operation would delete a **different group** with the same name, leaving the originally targeted group intact.

Bug trace:
1. CATS creates group A with UUID `aaa...` and name "test"
2. CATS creates group B with UUID `bbb...` and name "test"
3. CATS calls DELETE `/admin/groups/aaa...`
4. Handler looked up group by UUID (`aaa...`) → got name "test"
5. Handler called `Delete("test")` which looked up by name → found group B (`bbb...`)
6. Handler deleted group B, returned 204
7. CATS called GET `/admin/groups/aaa...` → 200 (group A still exists!)

**Fix Applied**: Changed `DeleteGroupAndData` to accept `internalUUID` string instead of `groupName`:
- [auth/repository/interfaces.go](auth/repository/interfaces.go) - Updated interface signature
- [auth/repository/deletion_repository.go](auth/repository/deletion_repository.go) - Updated implementation to query by `internal_uuid`
- [auth/group_deletion.go](auth/group_deletion.go) - Updated service wrapper
- [api/group_store.go](api/group_store.go) - Updated interface
- [api/group_store_gorm.go](api/group_store_gorm.go) - Updated implementation
- [api/admin_group_handlers.go](api/admin_group_handlers.go) - Updated handler to pass `internalUuid.String()`
- [auth/repository/deletion_repository_test.go](auth/repository/deletion_repository_test.go) - Updated tests

**Severity**: Medium → **FIXED**

---

### 2. RemoveFields: `/admin/administrators` POST (3 errors) - DOCUMENTED AS FALSE POSITIVE ✓

**Issue**: Removing `group_name`, `email`, or `provider_user_id` returns 400, but CATS expected a different response.

**Root Cause Analysis**: The OpenAPI spec uses `oneOf` to require exactly one of `email`, `provider_user_id`, or `group_name`. When CATS removes these fields:
- The API correctly returns 400 (validation error)
- CATS interprets this as an error because it expected the field to be optional

**Severity**: Low - This is **correct API behavior**

**Status**: **NO FIX NEEDED - FALSE POSITIVE**
- The API is correctly enforcing the `oneOf` constraint
- CATS doesn't fully understand complex `oneOf` schemas

**How to suppress**: Add to `run-cats-fuzz.sh`:
```bash
--skipFuzzersForPaths="/admin/administrators:RemoveFields"
```

**Technical explanation**: The `CreateAdministratorRequest` schema uses:
```yaml
oneOf:
  - required: [email]
  - required: [provider_user_id]
  - required: [group_name]
```
CATS's `RemoveFields` fuzzer tests what happens when each field is removed. Since the schema requires at least one of these fields, removing all of them correctly triggers a 400 response. CATS doesn't understand this is expected behavior for `oneOf` constraints.

---

## Warnings (92 total)

### 1. XssInjectionInStringFields (86 warnings) - AUTOMATIC FALSE POSITIVE FLAGGING ✓

**Affected Endpoints**:
| Endpoint | Method | Count |
|----------|--------|-------|
| `/admin/groups` | GET | 20 |
| `/admin/users` | GET | 20 |
| `/admin/groups` | POST | 16 |
| `/admin/administrators` | GET | 10 |
| `/invocations` | GET | 10 |
| `/oauth2/providers/{idp}/groups` | GET | 10 |

**Issue**: XSS payloads in query parameters are "accepted without validation"

**Root Cause Analysis**:
- These are query parameters (filters), not data stored in the database
- The API accepts the parameter values but they don't result in XSS because:
  1. Query parameters are used for filtering, not rendered in HTML
  2. TMI is a REST API - no HTML rendering
  3. Responses are JSON, not HTML

**Severity**: Low - **FALSE POSITIVE for API-only service**

**Status**: **NO FIX NEEDED - FALSE POSITIVE**
- XSS is a browser vulnerability, not an API vulnerability
- API returns JSON with `Content-Type: application/json`, not HTML
- The warning is valid only if these values were rendered in HTML without escaping

**Automatic False Positive Flagging**:

The parse script (`scripts/parse-cats-results.py`) can be enhanced to automatically flag XSS warnings on query parameters as false positives. Add this logic:

```python
# In parse-cats-results.py, add to the false positive detection:
def is_xss_query_param_false_positive(test):
    """XSS in query parameters is not exploitable for JSON APIs"""
    if test.fuzzer != "XssInjectionInStringFields":
        return False
    # Check if test was on a query parameter (not request body)
    if test.http_method == "GET":
        return True  # GET requests only have query params
    # For POST/PUT/PATCH, check if the field was in query params
    # (This would require more detailed request parsing)
    return False
```

**Alternative: CATS Configuration**:
```bash
# Skip XSS fuzzing entirely (if you're confident it's not needed)
--skipFuzzers=XssInjectionInStringFields

# Or skip for specific GET endpoints
--skipFuzzersForPaths="/admin/groups:XssInjectionInStringFields,/admin/users:XssInjectionInStringFields"
```

**Why this is safe for TMI**:
1. TMI is a pure REST API - no server-side HTML rendering
2. All responses use `Content-Type: application/json`
3. XSS exploits require a browser to interpret HTML/JS
4. JSON parsers don't execute embedded scripts
5. Frontend clients are responsible for sanitizing before rendering

---

### 2. ExtremePositiveNumbersInDecimalFields (2 warnings)

**Endpoint**: `/threat_models/{threat_model_id}/threats` POST

**Issue**: Sending extreme positive values in `score` field returns 400 with "Not matching response schema"

**Root Cause Analysis**:
- The API correctly rejects extreme values with 400
- The warning is about the response not matching the schema, not the rejection itself

**Severity**: Low - This is **correct API behavior**

**Recommendation**: **INVESTIGATE RESPONSE SCHEMA**
- Check if the 400 error response matches the `Error` schema in OpenAPI spec
- May need to ensure error responses include all required fields

---

### 3. ExtremeNegativeNumbersInDecimalFields (2 warnings)

**Same as above** - negative extreme values in `score` field

**Recommendation**: **Same as above**

---

### 4. InvalidValuesInEnumsFields (2 warnings)

**Endpoint**: `/oauth2/token` POST

**Issue**: Invalid `grant_type` enum value returns "Response content type not matching the contract"

**Root Cause Analysis**:
- The API correctly rejects invalid enum values with 400
- The warning indicates the response Content-Type may not match the spec

**Severity**: Low

**Recommendation**: **VERIFY CONTENT-TYPE**
- Check if `/oauth2/token` error responses return `application/json` as specified
- OAuth token endpoints sometimes return different content types for errors

---

## Summary of Actions

### Must Fix (Priority: High)

| Issue | Endpoint | Action |
|-------|----------|--------|
| Deleted groups return 200 | `/admin/groups/{internal_uuid}` GET | Ensure deleted groups return 404 |

### Should Investigate (Priority: Medium)

| Issue | Endpoint | Action |
|-------|----------|--------|
| Error response schema mismatch | `/threat_models/{id}/threats` POST | Verify 400 responses match Error schema |
| Content-type mismatch | `/oauth2/token` POST | Verify error Content-Type is application/json |

### No Action Needed (False Positives)

| Issue | Count | Reason |
|-------|-------|--------|
| RemoveFields on oneOf schema | 3 | CATS doesn't understand oneOf constraints |
| XSS in query parameters | 86 | API returns JSON, not HTML - XSS not applicable |

---

## Implementation Plan

### Phase 1: Fix Critical Issues

1. **Investigate `/admin/groups/{internal_uuid}` GET behavior**
   - Check if groups are soft-deleted vs hard-deleted
   - Ensure GET returns 404 for deleted/non-existent groups
   - Add integration test to verify behavior

### Phase 2: Verify Response Schemas

2. **Review error response schemas**
   - Ensure all 400 responses match the `Error` schema
   - Verify Content-Type headers on error responses

### Phase 3: Document False Positives

3. **Update CATS documentation**
   - Add `RemoveFields` on `/admin/administrators` to known false positives
   - Document XSS warnings as non-applicable to JSON APIs
   - Consider adding CATS skip rules for these scenarios

---

## CATS Configuration Improvements

Consider adding these to `run-cats-fuzz.sh`:

```bash
# Skip XSS fuzzing on query-only parameters (not stored data)
--skipFuzzers=XssInjectionInStringFields

# Or more granularly, skip for specific paths
--skipFuzzersForPaths="/admin/groups:XssInjectionInStringFields"
```

**Note**: Only skip if you're confident these are false positives. For POST/PUT endpoints that store data, XSS detection can still be valuable for defense-in-depth.

---

## Verification Summary

**Verified on**: 2025-01-24

### File References Verified

| File | Status |
|------|--------|
| `auth/repository/interfaces.go` | Exists, `DeleteGroupAndData` signature uses `internalUUID string` |
| `auth/repository/deletion_repository.go` | Exists, implementation queries by `internal_uuid` |
| `auth/group_deletion.go` | Exists |
| `api/group_store.go` | Exists |
| `api/group_store_gorm.go` | Exists |
| `api/admin_group_handlers.go` | Exists |
| `auth/repository/deletion_repository_test.go` | Exists |
| `scripts/run-cats-fuzz.sh` | Exists, contains `--skipFuzzers` configuration |
| `scripts/parse-cats-results.py` | Exists, contains `is_false_positive()` method |

### Make Targets Verified

| Target | Status |
|--------|--------|
| `make cats-fuzz` | Verified in Makefile (line 869) |
| `make parse-cats-results` | Verified in Makefile (line 935) |
| `make query-cats-results` | Verified in Makefile (line 951) |
| `make analyze-cats-results` | Verified in Makefile (line 960) |

### Code Behavior Verified

- **DeleteGroupAndData fix**: Confirmed in `auth/repository/deletion_repository.go` lines 130-214. Function accepts `internalUUID string` and queries by `internal_uuid`, not `group_name`.
- **XSS false positive logic**: Confirmed in `scripts/parse-cats-results.py` lines 720-729. XSS on GET requests are marked as false positives for JSON APIs.
- **CATS skip configuration**: Confirmed in `scripts/run-cats-fuzz.sh` lines 374-392. Includes `--skipFuzzersForExtension` for public and cacheable endpoints.

### External Tool Verified

- **CATS installation**: `brew tap endava/tap && brew install cats` per [CATS documentation](https://endava.github.io/cats/docs/getting-started/installation/)
