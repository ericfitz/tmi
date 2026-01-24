# CATS Fuzzer False Positives

<!-- Migrated to wiki: Testing.md#cats-false-positives on 2026-01-24 -->

This document describes known false positives from CATS (Contract-driven API Testing Suite) fuzzing that are expected behavior and not actual bugs.

## Overview

The CATS fuzzer tests API endpoints against the OpenAPI specification. Some "errors" reported by CATS are actually correct API behavior that CATS misinterprets as failures. These are documented here as tolerated false positives.

The `parse-cats-results.py` script automatically detects and marks these false positives using the `is_oauth_false_positive` flag (which covers all false positives, not just OAuth). Use the `test_results_filtered_view` in the SQLite database to exclude false positives from analysis.

## Tolerated False Positives

### 1. PrefixNumbersWithZeroFields (400 Bad Request)

**Affected Endpoints:** All endpoints with numeric fields (quotas, invocations, etc.)

**CATS Behavior:** CATS sends numeric values as strings with leading zeros (e.g., `"0095"` instead of `95`) and expects 2XX responses.

**Why 400 Is Correct:**
- JSON numbers with leading zeros are invalid per the JSON specification (RFC 8259)
- Sending `"0095"` as a string when an integer is expected is a type mismatch
- The API's strict type validation correctly rejects these malformed inputs
- Accepting leading zeros could cause octal interpretation bugs

**Example:** `max_requests_per_minute: "0095"` → 400 Bad Request (correct)

### 2. NoSQL Injection Detection (201 Created)

**Affected Endpoints:** `POST /addons` (name, description fields)

**CATS Behavior:** CATS reports "NoSQL injection vulnerability detected" when payloads like `{ $where: function() { return true; } }` are accepted and stored.

**Why This Is Not a Vulnerability:**
- TMI uses **PostgreSQL**, not MongoDB or any NoSQL database
- NoSQL operators like `$where` have no effect on SQL databases
- The payload is stored as a literal string, not executed
- This is a database-technology mismatch in CATS's detection logic

**Note:** TMI now validates addon name/description fields for template injection patterns (defense-in-depth), but NoSQL syntax is allowed since it's harmless in a SQL context.

### 3. POST /admin/administrators Validation Errors (400 Bad Request)

**Affected Endpoints:** `POST /admin/administrators`

**CATS Behavior:** `HappyPath`, `ExamplesFields`, and `CheckSecurityHeaders` fuzzers report errors when receiving 400 responses.

**Why 400 Is Correct:**
- This endpoint uses a `oneOf` schema requiring exactly one of: `email`, `provider_user_id`, or `group_name`
- CATS generates request bodies that may not satisfy the oneOf constraint
- The API correctly validates that exactly one identification field is provided
- Invalid oneOf combinations properly return 400 Bad Request

### 4. Connection Errors (Response Code 999)

**Affected Endpoints:** Various (commonly `PUT /threat_models/{id}/diagrams/{diagram_id}`)

**CATS Behavior:** CATS reports errors with HTTP code 999, indicating network-level connection issues.

**Why This Is Tolerated:**
- HTTP 999 is not a real HTTP status code - it indicates a connection error
- These often occur with URL encoding issues in path parameters (e.g., trailing `%`)
- The API may close connections for severely malformed requests
- This is a CATS/network issue, not an API bug

### 5. StringFieldsLeftBoundary on Optional Fields (201 Created)

**Affected Endpoints:** `POST /addons` (description field)

**CATS Behavior:** CATS sends empty strings for optional fields and expects 4XX responses.

**Why 201 Is Correct:**
- The `description` field is optional (marked as `required: false` in OpenAPI)
- Empty strings on optional fields are valid input
- The API correctly creates the resource with an empty description
- Returning 4XX for valid optional field values would be incorrect

### 6. GET Filter Parameters Returning Empty Results (200 OK)

**Affected Endpoints:**
- `GET /admin/groups` (filter by provider, group_name)
- `GET /admin/users` (filter by provider, email)
- `GET /admin/administrators` (filter by provider)

**CATS Behavior:** CATS sends fuzzing values (very long strings, special characters, XSS payloads) as filter parameters. When no records match these filters, the API returns 200 OK with an empty result set.

**Why This Is Correct:** Returning an empty result for a non-matching filter is standard REST API behavior. A filter parameter that matches no records is not an error condition - it simply means "no results found." Returning 400 or 404 would be incorrect.

**CATS Fuzzers Affected:**
- `VeryLargeStringsInFields`
- `RandomStringsInFields`
- `XssInjectionInStringFields` (on GET endpoints)

### 7. XSS on Query Parameters (200 OK)

**Affected Endpoints:** All list/search endpoints with filter parameters

**CATS Behavior:** CATS sends XSS payloads like `<script>alert('XSS')</script>` in query parameters and flags "XSS payload accepted" when the API returns 200.

**Why This Is Not a Vulnerability:**
- These are **filter/search parameters**, not stored data
- TMI is a **JSON API**, not an HTML-rendering web application
- XSS requires an HTML context to execute - JSON responses don't render HTML
- The API correctly returns matching results (or empty results) as JSON
- Client applications must handle output encoding, not the API

### 8. PUT /diagrams Connection Errors (InvalidContentLengthHeaders, InvalidReferencesFields)

**Affected Endpoints:**
- `PUT /threat_models/{id}/diagrams/{diagram_id}`

**CATS Behavior:** CATS reports errors with HTTP code 999, indicating network-level connection issues during the test.

**Why This Is Tolerated:** HTTP 999 indicates a connection error, not an API response. These are transient network issues during fuzzing, not API bugs. The API may have closed the connection due to malformed requests or rate limiting.

### 9. POST /admin/groups Duplicate Rejection (409 Conflict)

**Affected Endpoints:**
- `POST /admin/groups`

**CATS Behavior:** CATS's `MaxLengthExactValuesInStringFields` fuzzer interprets a 409 Conflict response as a validation failure.

**Why This Is Correct:** When attempting to create a group that already exists, the API correctly returns:
- Status: 409 Conflict
- Code: `duplicate_group`
- Message: "Group already exists for this provider"

This is proper REST semantics for duplicate resource creation, not a boundary validation failure.

### 10. POST /admin/administrators User/Group Not Found (404)

**Affected Endpoints:**
- `POST /admin/administrators`

**CATS Behavior:** CATS generates random values for `email`, `provider_user_id`, and `group_name` fields. Since these reference existing resources that don't exist with the random values, the API returns 404.

**Why This Is Correct:** The endpoint creates administrator grants that reference existing users and groups. When the referenced user or group doesn't exist, 404 is the correct response. The OpenAPI spec has been updated to document this 404 response.

## Skipped Fuzzers

The following fuzzers are skipped in the CATS configuration due to false positives or known CATS bugs:

### Skipped Due to Valid API Behavior
- **DuplicateHeaders**: TMI ignores duplicate/unknown headers (valid per HTTP spec)
- **LargeNumberOfRandomAlphanumericHeaders**: TMI ignores extra headers (valid behavior)
- **EnumCaseVariantFields**: TMI uses case-sensitive enum validation (stricter is valid)

### Previously Skipped CATS Bugs (Now Fixed in CATS 13.6.0)

The following fuzzers were previously skipped due to CATS 13.5.0 bugs but are now re-enabled:

- **MassAssignmentFuzzer**: Was crashing with `JsonPath.InvalidModificationException` on array properties
  - Fixed in: [CATS 13.6.0](https://github.com/Endava/cats/releases/tag/cats-13.6.0)
  - Issue: [#191](https://github.com/Endava/cats/issues/191)

- **InsertRandomValuesInBodyFuzzer**: Was crashing with `IllegalArgumentException: count is negative` during HTML report generation
  - Fixed in: CATS 13.6.0
  - Issue: [#193](https://github.com/Endava/cats/issues/193)

Ensure you're running CATS 13.6.0 or later to avoid these issues.

## OAuth False Positives

CATS may flag legitimate 401/403 OAuth responses as "errors" when testing authentication flows. The parse script automatically detects and filters these using the `is_oauth_false_positive` flag.

See [cats-oauth-false-positives.md](../../../developer/testing/cats-oauth-false-positives.md) for details on OAuth-specific false positives.

## Public Endpoint Handling

TMI has 17 public endpoints (OAuth, OIDC, SAML) that are intentionally accessible without authentication. CATS's `BypassAuthentication` fuzzer is skipped for these endpoints using the `x-public-endpoint: true` vendor extension.

See [cats-public-endpoints.md](../../../developer/testing/cats-public-endpoints.md) for the complete list and documentation.

## Cacheable Endpoint Handling

6 discovery endpoints use `Cache-Control: public, max-age=3600` instead of `no-store`, which is correct per RFC 8414/7517/9728. CATS's `CheckSecurityHeaders` fuzzer is skipped for these endpoints using the `x-cacheable-endpoint: true` vendor extension.

See [cats-public-endpoints.md](../../../developer/testing/cats-public-endpoints.md#cacheable-endpoints) for details.

## Template Injection Protection

As of the latest update, TMI validates addon `name` and `description` fields for template injection patterns, providing defense-in-depth for downstream consumers. The following patterns are blocked:

| Pattern | Description | Example |
|---------|-------------|---------|
| `{{` / `}}` | Handlebars, Jinja2, Angular, Go templates | `{{constructor.constructor('alert(1)')()}}` |
| `${` | JavaScript template literals, Freemarker | `${alert(1)}` |
| `<%` / `%>` | JSP, ASP, ERB server templates | `<%=System.getProperty('user.home')%>` |
| `#{` | Spring EL, JSF EL expressions | `#{T(java.lang.Runtime).exec('calc')}` |
| `${{` | GitHub Actions context injection | `${{github.event.issue.title}}` |

This means CATS's `XssInjectionInStringFields` fuzzer should now receive 400 responses for template injection payloads on POST /addons.

## Running CATS Fuzzing

```bash
# Run full fuzzing
make cats-fuzz

# Analyze results (automatically filters false positives)
make analyze-cats-results

# View filtered results excluding false positives
make query-cats-results
```

## Investigating New Issues

When CATS reports new errors:

1. **Check HTTP status code**: 999 indicates network issues, not API bugs
2. **Check the fuzzer**: Some fuzzers (like `PrefixNumbersWithZeroFields`) expect incorrect behavior
3. **Check the request**: Random/fuzzing values may trigger expected validation errors
4. **Check the OpenAPI spec**: Ensure the spec documents all valid response codes
5. **Check existing documentation**: The issue may already be documented here

If an issue is determined to be a false positive, add detection logic to `parse-cats-results.py` and document it here with:
- Affected endpoint(s)
- CATS behavior description
- Explanation of why the behavior is correct
- Affected CATS fuzzers

## False Positive Detection in parse-cats-results.py

The `is_false_positive()` method in [scripts/parse-cats-results.py](../../../scripts/parse-cats-results.py) automatically detects the following categories:

1. Rate Limit (429) - Infrastructure, not API behavior
2. OAuth/Auth (401/403) - Expected auth failures during fuzzing
3. Validation (400) - Correct rejection of malformed input
4. Not Found (404) - Expected for random resource IDs
5. HTTP Methods (400/405) - Correct rejection of unsupported methods
6. Response Contract - Header mismatches (spec issues)
7. Duplicate Resources (409) - Correct duplicate detection
8. Invalid Content-Length (400 text/plain) - Go HTTP layer rejection
9. Injection Fuzzers - JSON API doesn't execute payloads
10. Header Validation (400) - Correct rejection of malformed headers
11. Transfer Encoding (501) - Correct for unsupported encodings
12. IDOR on admin endpoints - Admin users have full access by design
13. Leading zeros in numbers (400) - Correct type validation
14. oneOf schema validation (400) - Correct constraint enforcement
15. Connection errors (999) - Network issues, not API bugs
16. Optional field boundaries - Empty values are valid for optional fields

---

## Verification Summary (2026-01-24)

All references in this document have been verified:

### File References
- `scripts/parse-cats-results.py` - Verified exists with `is_false_positive()` method (line 463) and `is_oauth_false_positive` flag (line 82)
- `test_results_filtered_view` - Verified exists in parse script (line 191)
- `cats-oauth-false-positives.md` - Verified exists at `/Users/efitz/Projects/tmi/docs/developer/testing/cats-oauth-false-positives.md`
- `cats-public-endpoints.md` - Verified exists at `/Users/efitz/Projects/tmi/docs/developer/testing/cats-public-endpoints.md`

### Make Targets
- `make cats-fuzz` - Verified in Makefile (line 869)
- `make analyze-cats-results` - Verified in Makefile (line 960)
- `make query-cats-results` - Verified in Makefile (line 951)

### External References
- **RFC 8259** (JSON specification): Verified - JSON numbers with leading zeros are invalid per the grammar `int = zero / ( digit1-9 *DIGIT )`
- **RFC 8414** (OAuth 2.0 Authorization Server Metadata): Verified - Defines `.well-known/oauth-authorization-server` discovery endpoint
- **RFC 7517** (JSON Web Key): Verified - Defines JWK and JWKS format for public key distribution
- **RFC 6749** (OAuth 2.0 Authorization Framework): Verified - Core OAuth 2.0 specification
- **RFC 9728** (OAuth 2.0 Protected Resource Metadata): Verified - Published April 2025, defines `.well-known/oauth-protected-resource` endpoint

### CATS Version Information
- **CATS 13.6.0**: Verified released January 2025 with fix for issue #191 (MassAssignmentFuzzer)
- **Issue #191**: Verified - "MassAssignmentFuzzer crashes with InvalidModificationException on schemas with array properties" - Status: Closed
- **Issue #193**: Verified - "InsertRandomValuesInBodyFuzzer crashes with IllegalArgumentException: count is negative" - Status: Closed, fixed in 13.6.0
