# CATS Fuzzer False Positives

This document describes known false positives from CATS (Contract-driven API Testing Suite) fuzzing that are expected behavior and not actual bugs.

## Overview

The CATS fuzzer tests API endpoints against the OpenAPI specification. Some "errors" reported by CATS are actually correct API behavior that CATS misinterprets as failures. These are documented here as tolerated false positives.

## Tolerated False Positives

### 1. GET Filter Parameters Returning Empty Results (200 OK)

**Affected Endpoints:**
- `GET /admin/groups` (filter by provider, group_name)
- `GET /admin/users` (filter by provider, email)

**CATS Behavior:** CATS sends fuzzing values (very long strings, special characters, etc.) as filter parameters. When no records match these filters, the API returns 200 OK with an empty result set.

**Why This Is Correct:** Returning an empty result for a non-matching filter is standard REST API behavior. A filter parameter that matches no records is not an error condition - it simply means "no results found." Returning 400 or 404 would be incorrect.

**CATS Fuzzers Affected:**
- `VeryLargeStringsInFields`
- `RandomStringsInFields`

### 2. PUT /diagrams Connection Errors (InvalidContentLengthHeaders, InvalidReferencesFields)

**Affected Endpoints:**
- `PUT /threat_models/{id}/diagrams/{diagram_id}`

**CATS Behavior:** CATS reports errors with HTTP code 999, indicating network-level connection issues during the test.

**Why This Is Tolerated:** HTTP 999 indicates a connection error, not an API response. These are transient network issues during fuzzing, not API bugs. The API may have closed the connection due to malformed requests or rate limiting.

### 3. POST /admin/groups Duplicate Rejection (409 Conflict)

**Affected Endpoints:**
- `POST /admin/groups`

**CATS Behavior:** CATS's `MaxLengthExactValuesInStringFields` fuzzer interprets a 409 Conflict response as a validation failure.

**Why This Is Correct:** When attempting to create a group that already exists, the API correctly returns:
- Status: 409 Conflict
- Code: `duplicate_group`
- Message: "Group already exists for this provider"

This is proper REST semantics for duplicate resource creation, not a boundary validation failure.

### 4. POST /admin/administrators User/Group Not Found (404)

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

### Skipped Due to CATS Bugs
- **MassAssignmentFuzzer**: CATS 13.5.0 crashes with `JsonPath.InvalidModificationException` on array properties ([CATS Issue #191](https://github.com/Endava/cats/issues/191))
- **InsertRandomValuesInBodyFuzzer**: CATS 13.5.0 throws `IllegalArgumentException: count is negative` during HTML report generation ([CATS Issue #193](https://github.com/Endava/cats/issues/193))

## OAuth False Positives

CATS may flag legitimate 401/403 OAuth responses as "errors" when testing authentication flows. The parse script automatically detects and filters these using the `is_oauth_false_positive` flag.

See [cats-oauth-false-positives.md](cats-oauth-false-positives.md) for details on OAuth-specific false positives.

## Public Endpoint Handling

TMI has 17 public endpoints (OAuth, OIDC, SAML) that are intentionally accessible without authentication. CATS's `BypassAuthentication` fuzzer is skipped for these endpoints using the `x-public-endpoint: true` vendor extension.

See [cats-public-endpoints.md](cats-public-endpoints.md) for the complete list and documentation.

## Cacheable Endpoint Handling

6 discovery endpoints use `Cache-Control: public, max-age=3600` instead of `no-store`, which is correct per RFC 8414/7517/9728. CATS's `CheckSecurityHeaders` fuzzer is skipped for these endpoints using the `x-cacheable-endpoint: true` vendor extension.

See [cats-public-endpoints.md](cats-public-endpoints.md#cacheable-endpoints) for details.

## Running CATS Fuzzing

```bash
# Run full fuzzing
make cats-fuzz

# Analyze results (automatically filters OAuth false positives)
make analyze-cats-results

# View filtered results excluding false positives
make query-cats-results
```

## Investigating New Issues

When CATS reports new errors:

1. **Check HTTP status code**: 999 indicates network issues, not API bugs
2. **Check the request**: Random/fuzzing values may trigger expected validation errors
3. **Check the OpenAPI spec**: Ensure the spec documents all valid response codes
4. **Check existing documentation**: The issue may already be documented here

If an issue is determined to be a false positive, add it to this document with:
- Affected endpoint(s)
- CATS behavior description
- Explanation of why the behavior is correct
- Affected CATS fuzzers
