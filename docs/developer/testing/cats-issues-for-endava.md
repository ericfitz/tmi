# CATS Issues to Report to Endava

This document describes potential bugs and enhancement requests for the CATS (Contract-driven Automatic Testing Suite) tool, based on extensive fuzzing of the TMI API.

## Test Environment

- **CATS Version**: 13.6.0
- **API**: TMI Collaborative Threat Modeling Interface
- **Total Tests**: 65,957
- **False Positive Rate**: 53% (34,996 tests)
- **Actual Issues Found**: 173 (0.26%)

---

## Issue 1: MassAssignment Fuzzer False Positives on Stateless Endpoints

### Summary

The `MassAssignment` fuzzer reports errors when undeclared fields are "accepted" (200 response) on endpoints that intentionally ignore unknown fields, such as OAuth token revocation.

### Reproduction

1. Fuzz an OAuth 2.0 `/oauth2/revoke` endpoint (RFC 7009 compliant)
2. MassAssignment adds fields like `{"token": "...", "admin": true}`
3. Server returns 200 (token successfully revoked)
4. CATS reports: "Undeclared field accepted" as an error

### Expected vs Actual Behavior

| Aspect | Expected | Actual |
|--------|----------|--------|
| Detection | Check if field affects server state | Only checks for non-4xx response |
| Result | Pass (field ignored) | Error (field "accepted") |

### Why This Is a False Positive

Per RFC 7009 Section 2.1, token revocation endpoints SHOULD accept and ignore unknown parameters:

> "The authorization server first validates the client credentials (in case of a confidential client) and then verifies whether the token was issued to the client making the revocation request. [...] The authorization server responds with HTTP status code 200 if the token has been revoked successfully or if the client submitted an invalid token."

The endpoint correctly:
1. Ignores the `admin: true` field
2. Revokes the token
3. Returns 200

No "mass assignment" vulnerability exists because the extra field has no effect.

### Suggested Fix

CATS should verify that undeclared fields actually **affect server state** before reporting mass assignment vulnerabilities. Options:

1. **Behavioral check**: Compare response bodies with/without the extra field
2. **Stateful check**: GET the resource after POST to verify field wasn't assigned
3. **OpenAPI extension**: Allow marking endpoints as "intentionally ignores extra fields" via `x-accepts-unknown-fields: true`

### Impact

- **43 false positives** on `/oauth2/revoke` alone
- Similar issues likely on other RFC-compliant endpoints

---

## Issue 2: ZeroWidthCharsInNamesFields False Positives on Lenient Parsers

### Summary

The `ZeroWidthCharsInNamesFields` fuzzer reports errors when inserting zero-width characters into field names results in a 200 response, even when the endpoint correctly treats malformed names as unknown fields.

### Reproduction

1. Fuzz `/oauth2/revoke` with body: `{"to\u200bken": "abc123"}`
2. Server treats `"to​ken"` (with zero-width space) as unknown field
3. Server processes valid `"token"` field if present, or ignores request
4. Returns 200 (per RFC 7009 - invalid tokens silently accepted)
5. CATS reports: "Unexpected response code: 200"

### Why This Is a False Positive

Zero-width characters in JSON field **names** create different field names entirely:
- `"token"` ≠ `"to\u200bken"` (these are different JSON keys)

The server correctly:
1. Parses the JSON
2. Finds no valid `"token"` field
3. Treats the malformed field name as unknown
4. Returns success per RFC 7009

### Suggested Fix

For endpoints that intentionally ignore unknown fields, CATS should not expect 4xx responses for malformed field names. Options:

1. Check if the **valid** field was processed (not just response code)
2. Support `x-ignores-unknown-fields: true` extension
3. Change default expectation from "should reject" to "should not process"

### Impact

- **18 false positives** on `/oauth2/revoke`

---

## Issue 3: CheckDeletedResourcesNotAvailable Targets Collection Endpoints

### Summary

The `CheckDeletedResourcesNotAvailable` fuzzer incorrectly tests collection endpoints (`/resources`) instead of item endpoints (`/resources/{id}`), expecting 404 after deleting an item.

### Reproduction

1. POST to `/me/client_credentials` to create a credential
2. DELETE `/me/client_credentials/{id}` to delete it
3. GET `/me/client_credentials` (collection endpoint)
4. Returns 200 with remaining credentials
5. CATS reports: "Unexpected response code: 200"

### Expected Behavior

After deleting `/me/client_credentials/{id}`:
- GET `/me/client_credentials/{id}` → 404 ✓
- GET `/me/client_credentials` → 200 with list ✓

### Suggested Fix

This fuzzer should only test item endpoints (those with `{id}` path parameters), not collection endpoints.

### Impact

- **13 false positives** on `/me/client_credentials`

---

## Issue 4: NonRestHttpMethods Triggers Rate Limiting

### Summary

The `NonRestHttpMethods` fuzzer tests 17 WebDAV methods (ORDERPATCH, COPY, PROPFIND, etc.) against every endpoint, generating massive request volume that triggers rate limiting.

### Reproduction

1. Run CATS against any API with rate limiting
2. NonRestHttpMethods sends 17 requests per endpoint for WebDAV methods
3. Rate limiter triggers 429 responses
4. CATS reports these as test results (not errors)

### Evidence

```
/|ORDERPATCH|429|RATE_LIMIT_429
/|COPY|429|RATE_LIMIT_429
/|PROPFIND|429|RATE_LIMIT_429
... (187 total rate-limited requests)
```

### Suggested Enhancement

Add option to disable WebDAV method testing for REST APIs:
```
--skipWebDAVMethods
```

Or reduce the test surface:
```
--nonRestMethods=CONNECT,OPTIONS  # Only test specific methods
```

### Impact

- **187 false positives** from rate limiting
- Interferes with other test results due to rate limit state

---

## Issue 5: ResponseHeadersMatchContractHeaders Reports Optional Headers as Errors

### Summary

The `ResponseHeadersMatchContractHeaders` fuzzer reports **errors** when response headers defined in the OpenAPI contract are not returned, even when those headers are optional (not marked with `required: true`).

### Result Type

**Error** (should be warning or info)

### Reproduction

1. Define optional response headers in OpenAPI:
   ```yaml
   responses:
     200:
       headers:
         X-RateLimit-Limit:
           description: "Maximum requests allowed"
           schema:
             type: integer
           # Note: No "required: true"
   ```
2. API returns 200 without the `X-RateLimit-Limit` header
3. CATS reports: "Missing response headers" as **error**

### Evidence

```
/admin/users|200|The following response headers defined in the contract are missing: X-RateLimit-Limit
/me|200|The following response headers defined in the contract are missing: X-RateLimit-Limit
```

### Why This Is a Bug

Per OpenAPI 3.0 Specification, Section [Header Object](https://spec.openapis.org/oas/v3.0.3#header-object):

> "All traits that are affected by the location MUST be applicable to a location of `header` (for example, `style`). **The `required` property is taken from the JSON Schema definition and defaults to false.**"

Headers without `required: true` are **optional**. Missing optional headers should not be reported as errors.

### Suggested Fix

1. Only report **errors** for headers marked `required: true`
2. Report missing optional headers as **warning** or **info**
3. Or add option: `--strictHeaders=true` to enable current behavior

### Impact

- **57 errors** for missing optional headers (plus 172 auth-related FPs)
- Causes teams to either skip this fuzzer or add unnecessary headers

---

## Issue 6: Multiple Fuzzers Have Inverted Success/Failure Logic for Input Validation

### Summary

Several fuzzers that inject malformed or potentially malicious input have **inverted logic**: they report **errors** when the API correctly rejects bad input (400), but expect **success** (2XX). This affects at least 8 fuzzers and generates ~2,700 false positives.

### Result Type

**Error** (should be success when API rejects malformed input)

### Affected Fuzzers

| Fuzzer | FPs | Primary Issue |
|--------|-----|---------------|
| `PrefixNumbersWithZeroFields` | 93 | API rejects invalid JSON (leading zeros) |
| `BidirectionalOverrideFields` | 519 | API rejects Unicode BiDi override chars |
| `ZalgoTextInFields` | 395 | API rejects Zalgo text injection |
| `HangulFillerFields` | 896 | API rejects Korean filler chars |
| `AbugidasInStringFields` | 439 | API rejects Indic script injection |
| `FullwidthBracketsFields` | 440 | API rejects CJK bracket injection |
| `ZeroWidthCharsInValuesFields` | 2,338 | API rejects zero-width chars |

**Note**: Many of these FPs are also mixed with auth failures (401) from the same inverted logic - CATS expects 2XX even when auth middleware correctly rejects requests.

### Field Analysis

Investigation of the 400 responses reveals they fall into two categories:

#### Category 1: Technical Fields (Rejection is Correct)

| Field Type | Example Fields | 400 Count | Should Allow Unicode? |
|------------|----------------|-----------|----------------------|
| Technical IDs | `token`, `client_id`, `client_secret`, `email` | ~300 | ❌ No - reject is correct |
| Enums/Constrained | `status`, `severity`, `priority`, `sort_by` | ~125 | ❌ No - reject is correct |
| Numeric/Date | `limit`, `offset`, `expires_at`, timestamps | ~270 | ❌ No - reject is correct |
| URIs/URLs | `uri`, `url`, `issue_uri`, `client_callback` | ~30 | ❌ No - reject is correct |
| Parameters | `parameters#subPath`, `parameters#refValue` | ~30 | ❌ No - reject is correct |

**These 400s are correct API behavior** - the API properly rejects malicious Unicode in technical fields.

#### Category 2: User Text Fields (Nuanced)

| Field | 400 Count | Notes |
|-------|-----------|-------|
| `name` | 222 | Threat model names, addon names, etc. |
| `description` | 210 | User-written descriptions |
| `content` | 10 | Note/document content |
| `mitigation` | 8 | Threat mitigation text |
| `notes` | 5 | General notes |
| `value` | 74 | Metadata key-value pairs |

**These 400s require nuanced analysis:**

- **Zero-width chars, BiDi overrides, Hangul fillers**: Correctly rejected (security risk)
- **Abugidas (Indic scripts)**: TMI injects U+200C (Zero Width Non-Joiner) which is blocked, but this char is needed for proper Indic script rendering
- **Zalgo text**: TMI blocks ALL combining diacritical marks (U+0300-U+036F) which also blocks legitimate French (é, ñ), Vietnamese (ế), etc.

**TMI-specific note**: Our `UnicodeNormalizationMiddleware` blocks combining marks to prevent Zalgo attacks, but this may be overly aggressive for user-facing text fields. This is a TMI configuration choice, not a CATS bug.

### Reproduction

1. Fuzzer injects malformed/malicious content into request fields
2. API returns 400 (correctly rejecting invalid input)
3. CATS reports: "Unexpected response code: 400" with expected "Should return 2XX"

### Evidence

**PrefixNumbersWithZeroFields** (invalid JSON per RFC 8259):
```
/admin/quotas/addons/{user_id}|Send numeric field with leading zeros: [01000]|
  Expected: Should return 2XX
  Actual: 400
  Result: error
```

**BidirectionalOverrideFields** (Unicode attack vector):
```
BidirectionalOverrideFields|Unexpected response code: 400|Should return [2XX]|381 occurrences
```

**ZalgoTextInFields** (text corruption attack):
```
ZalgoTextInFields|Unexpected response code: 400|Should return [2XX]|326 occurrences
```

### Why This Is a Bug

These fuzzers are designed to test how APIs handle potentially dangerous input:

| Fuzzer | Security Purpose |
|--------|------------------|
| `PrefixNumbersWithZeroFields` | Test for octal interpretation vulnerabilities |
| `BidirectionalOverrideFields` | Test for text direction spoofing attacks |
| `ZalgoTextInFields` | Test for display corruption/DoS |
| `HangulFillerFields` | Test for invisible character injection |
| `ZeroWidthCharsInValuesFields` | Test for hidden content injection |

When an API **correctly rejects** these inputs with 400, that demonstrates **proper input validation** - exactly what security testing should verify. The current logic is backwards.

For `PrefixNumbersWithZeroFields` specifically, RFC 8259 Section 6 explicitly prohibits leading zeros:
> "Leading zeros are not allowed."

### Expected Behavior

| API Response | What It Means | CATS Should Report |
|--------------|---------------|-------------------|
| 400 | API rejects malformed input | ✅ **Success** - proper validation |
| 2XX | API accepts malformed input | ⚠️ **Warning** - potential vulnerability |

### Suggested Fix

Invert the logic for these negative-testing fuzzers:
- **Success**: API returns 4XX (rejects bad input)
- **Warning/Error**: API returns 2XX (accepts bad input that could be exploited)

Alternatively, add a flag like `--expectRejection` to indicate that 4XX responses are the desired outcome for security fuzzers.

### Impact

- **~2,700 errors** that are actually correct API behavior (excluding auth FPs)
- Discourages proper input validation
- Teams skip these fuzzers entirely, losing security coverage
- Creates noise that obscures real issues

---

## Issue 7: HappyPath Fuzzer Doesn't Understand Two-Step Workflows

### Summary

The `HappyPath` fuzzer doesn't understand multi-step API workflows (like challenge-response deletion), sending random parameters that cause expected 400 responses.

### Reproduction

1. OpenAPI spec defines DELETE `/me` with optional `challenge` query parameter
2. Workflow: First call (no challenge) → get challenge string → Second call (with challenge) → delete
3. HappyPath sends: `DELETE /me?challenge=RANDOMSTRING`
4. Server returns 400 (invalid challenge)
5. CATS reports error

### Evidence

```
DELETE http://localhost:8080/me?challenge=NBETBNUTSWSMKTRDWWMPJAIILXZAELQQUQDI
Response: 400 (challenge is invalid - wasn't issued by server)
```

### Why This Is Expected

The `challenge` parameter must match a server-issued challenge. Random values should return 400.

### Suggested Enhancement

Support OpenAPI extension to describe workflow dependencies:
```yaml
x-workflow-step: 2
x-depends-on-step: 1
x-step-1-provides: challenge
```

Or support Arazzo workflow definitions for multi-step operations.

### Impact

- **2 false positives** on `/me` DELETE
- Affects any API with challenge-response or CSRF token patterns

---

## Summary of Recommended CATS Enhancements

| Issue | Type | Result Type | Priority | Estimated FP Reduction |
|-------|------|-------------|----------|----------------------|
| **Inverted logic for input validation fuzzers** | Bug | Error | **High** | **~2,700 FPs** |
| MassAssignment on stateless endpoints | Bug | Error | High | ~50 FPs |
| ZeroWidthCharsInNamesFields on lenient parsers | Bug | Error | Medium | ~20 FPs |
| CheckDeletedResourcesNotAvailable on collections | Bug | Error | Medium | ~15 FPs |
| ResponseHeadersMatchContractHeaders optional headers | Bug | Error | Medium | ~60 FPs |
| NonRestHttpMethods rate limiting | Enhancement | Error | Low | ~200 FPs |
| HappyPath multi-step workflows | Enhancement | Error | Low | ~5 FPs |

**Note**: The "inverted logic" issue (Issue 6) consolidates 8 fuzzers: `PrefixNumbersWithZeroFields`, `BidirectionalOverrideFields`, `ZalgoTextInFields`, `HangulFillerFields`, `AbugidasInStringFields`, `FullwidthBracketsFields`, and `ZeroWidthCharsInValuesFields`.

## Workarounds Currently Used

Until these issues are addressed, TMI uses the following CATS configuration:

```bash
# Skip fuzzers with high FP rates
--skipFuzzers=BidirectionalOverrideFields,ResponseHeadersMatchContractHeaders,...

# Skip specific fuzzers on marked endpoints
--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication
--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders
--skipFuzzersForExtension=x-skip-deleted-resource-check=true:CheckDeletedResourcesNotAvailable
```

## Contact

These issues were identified during security testing of the TMI project:
- Repository: https://github.com/ericfitz/tmi
- CATS configuration: `scripts/run-cats-fuzz.sh`
- False positive analysis: `scripts/parse-cats-results.py`
