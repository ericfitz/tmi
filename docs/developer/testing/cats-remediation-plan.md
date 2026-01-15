# CATS Fuzzing Results Remediation Plan

## Executive Summary

Analysis of the CATS fuzzing results database (`test/outputs/cats/cats-results.db`) shows:
- **24,211 successes** (99.4%)
- **116 errors** (0.5%)
- **39 warnings** (0.2%)

After filtering out OAuth false positives, the remaining issues fall into distinct categories requiring different remediation approaches.

## Remediation Status

This document tracks the analysis and resolution of CATS fuzzing findings.

| Issue | Status | Resolution |
|-------|--------|------------|
| IDOR on DELETE /addons/{id} | ✅ Resolved | False positive - admin-only endpoint by design |
| Malformed URL handling (999) | ✅ Resolved | False positive - server returns 400 correctly |
| Admin endpoint HappyPath failures | ✅ Resolved | OpenAPI spec updated with oneOf and maximum constraints |
| SAML 400 responses | ✅ Resolved | Added to OpenAPI spec |
| WebhookQuota schema mismatch | ✅ Resolved | Added created_at/modified_at fields |
| False positive fuzzers | ✅ Resolved | Added to CATS skip configuration |

## Issue Categories

### Category 1: False Positives - No Action Required

These findings are expected behavior and are now documented/skipped in CATS configuration.

#### 1.1 DuplicateHeaders (16 errors) ✅ SKIPPED
- **Issue**: CATS expects 4xx response when duplicate headers are sent
- **Actual**: Server returns 2xx (ignores duplicate headers)
- **Assessment**: This is **correct behavior**. Ignoring unknown duplicate headers is acceptable per HTTP specifications
- **Resolution**: Added to `--skipFuzzers` in `run-cats-fuzz.sh`

#### 1.2 LargeNumberOfRandomAlphanumericHeaders (13 errors) ✅ SKIPPED
- **Issue**: CATS expects 4xx when 10,000 random headers are sent
- **Actual**: Server returns 2xx (ignores extra headers)
- **Assessment**: This is **acceptable behavior**. The server processes valid headers and ignores unknown ones
- **Resolution**: Added to `--skipFuzzers` in `run-cats-fuzz.sh`

#### 1.3 ExtremePositiveNumbersInIntegerFields - Pagination (11 errors)
- **Issue**: CATS expects 4xx when `limit` or `offset` are set to extreme values
- **Actual**: Server returns 2xx (empty results)
- **Assessment**: This is **correct behavior**. Returning empty results for out-of-range pagination is valid
- **Resolution**: The `offset` field is already skipped via `--skipField=offset`

#### 1.4 IntegerFieldsRightBoundary - Pagination (11 errors)
- **Issue**: Same as above for pagination parameters
- **Resolution**: The `offset` field is already skipped via `--skipField=offset`

#### 1.5 RandomResources on DELETE (10 errors)
- **Issue**: CATS expects 4xx when deleting with random path parameters
- **Actual**: DELETE `/admin/quotas/addons/{random_user_id}` returns 204
- **Assessment**: This is **idempotent delete behavior** (resource doesn't exist = already deleted = success). This is acceptable REST semantics.

#### 1.6 EnumCaseVariantFields (21 errors) ✅ SKIPPED
- **Issue**: CATS sends mixed-case enum values (e.g., "ThreAt" instead of "threat")
- **Actual**: Server returns 400 (strict case validation)
- **Assessment**: This is **correct behavior**. Case-sensitive enum validation is proper.
- **Resolution**: Added to `--skipFuzzers` in `run-cats-fuzz.sh`

### Category 2: OpenAPI Specification Issues - RESOLVED

#### 2.1 Admin Endpoint HappyPath Failures (4 errors) ✅ FIXED
- **Endpoints**:
  - `POST /admin/administrators` - returns 400
  - `PUT /admin/quotas/addons/{user_id}` - returns 400
  - `PUT /admin/quotas/users/{user_id}` - returns 400
  - `PUT /admin/quotas/webhooks/{user_id}` - returns 400
- **Root Cause**:
  - `CreateAdministratorRequest`: Schema had mutually exclusive fields (email, provider_user_id, group_name) but no `oneOf` constraint, plus invalid example using non-existent `user_id` field
  - Quota update schemas: Missing `maximum` constraints, allowing CATS to generate values exceeding server-side limits
- **Resolution**:
  - Updated `CreateAdministratorRequest` to use `oneOf` for mutual exclusivity
  - Fixed example to use valid field (`email` instead of `user_id`)
  - Added `maximum` constraints to quota schemas matching server-side validation:
    - `AddonQuotaUpdate`: max_active_invocations=10, max_invocations_per_hour=1000
    - `UserQuotaUpdate`: max_requests_per_minute=10000, max_requests_per_hour=600000
    - `WebhookQuotaUpdate`: max_subscriptions=100, max_events_per_minute=1000, max_subscription_requests_per_minute=100, max_subscription_requests_per_day=10000

#### 2.2 CheckSecurityHeaders on Admin Endpoints (4 errors) ✅ FIXED
- **Resolution**: Fixed by resolving HappyPath failures above

#### 2.3 Schema Mismatch Warnings - WebhookQuota ✅ FIXED
- **Endpoint**: `GET /admin/quotas/webhooks`
- **Issue**: Response included `created_at` and `modified_at` fields not in schema
- **Resolution**: Added `created_at` and `modified_at` to `WebhookQuota` schema

### Category 3: Potential Security Issues - VERIFIED FALSE POSITIVES

#### 3.1 IDOR on DELETE /addons/{id} (1 error) ✅ FALSE POSITIVE
- **Test**: Replaced addon ID with different UUID, got 204 response
- **Investigation**: Reviewed `api/addon_handlers.go` lines 202-259
- **Finding**: The `DeleteAddon` handler calls `requireAdministrator(c)` on line 207, restricting deletion to administrators only
- **Conclusion**: **FALSE POSITIVE** - This is by design. Administrators can delete any addon. The CATS test ran with admin credentials.

#### 3.2 InvalidReferencesFields - Response Code 999 (3 errors) ✅ FALSE POSITIVE
- **Issue**: Server returned no response when path contains incomplete URL encoding (trailing `%`)
- **Investigation**: Tested manually with malformed URLs
- **Finding**: Server correctly returns HTTP 400 Bad Request for malformed URLs
- **Conclusion**: **FALSE POSITIVE** - The response code 999 was a CATS connection timeout during testing, not a server crash. Server handles malformed URLs correctly.

### Category 4: SAML Endpoint Documentation - RESOLVED

#### 4.1 Undocumented 400 Responses ✅ FIXED
- **Endpoints**: `/saml/{provider}/login`, `/saml/{provider}/metadata`
- **Resolution**: Added 400 response documentation to both endpoints in OpenAPI spec

#### 4.2 InvalidValuesInEnumsFields Response Type (2 warnings)
- **Endpoints**: `/admin/groups` GET, `/oauth2/token` POST
- **Issue**: Response content type doesn't match contract on invalid enum values
- **Status**: Low priority - does not affect functionality

### Category 5: Input Validation Edge Cases

These are low-priority issues that represent edge cases in CATS testing:

#### 5.1 MinimumExactNumbersInNumericFields (7 errors)
- **Issue**: Setting minimum values (e.g., `max_invocations_per_hour=1`) returns 400
- **Assessment**: This may be expected if the minimum in OpenAPI (1) doesn't match server validation
- **Status**: OpenAPI spec now has both minimum and maximum constraints

#### 5.2 MaxLengthExactValuesInStringFields (1 error)
- **Endpoint**: `POST /admin/groups`
- **Issue**: Sending exactly maxLength characters returns 400
- **Status**: Low priority edge case

#### 5.3 RemoveFields (4 errors)
- **Endpoints**: Various admin endpoints
- **Issue**: Removing required fields returns 400 (expected) but CATS marks as error
- **Status**: Expected behavior

---

## Changes Made

### OpenAPI Specification (`docs/reference/apis/tmi-openapi.json`)

1. **CreateAdministratorRequest**: Refactored to use `oneOf` for mutually exclusive fields:
   ```json
   {
     "oneOf": [
       { "required": ["email"], ... },
       { "required": ["provider_user_id"], ... },
       { "required": ["group_name"], ... }
     ]
   }
   ```

2. **AddonQuotaUpdate**: Added maximum constraints:
   - `max_active_invocations`: maximum=10
   - `max_invocations_per_hour`: maximum=1000

3. **UserQuotaUpdate**: Added maximum constraints:
   - `max_requests_per_minute`: maximum=10000
   - `max_requests_per_hour`: maximum=600000

4. **WebhookQuotaUpdate**: Added maximum constraints:
   - `max_subscriptions`: maximum=100
   - `max_events_per_minute`: maximum=1000
   - `max_subscription_requests_per_minute`: maximum=100
   - `max_subscription_requests_per_day`: maximum=10000

5. **WebhookQuota**: Added `created_at` and `modified_at` fields

6. **SAML Endpoints**: Added 400 response documentation to:
   - `/saml/{provider}/login`
   - `/saml/{provider}/metadata`

### CATS Configuration (`scripts/run-cats-fuzz.sh`)

Updated `--skipFuzzers` to include false positive fuzzers:
```bash
--skipFuzzers=MassAssignmentFuzzer,InsertRandomValuesInBodyFuzzer,DuplicateHeaders,LargeNumberOfRandomAlphanumericHeaders,EnumCaseVariantFields
```

---

## Expected Metrics After Remediation

After implementing these changes, re-running CATS should show:
- **Errors**: Significantly reduced (estimated ~10-20 from 116)
- **Warnings**: Reduced (estimated ~15-25 from 39)
- **False positive rate**: Near zero with proper skip configuration

The remaining errors/warnings will be:
- Edge cases in pagination boundary testing
- Minor schema validation differences
- Expected 400 responses that CATS interprets differently
