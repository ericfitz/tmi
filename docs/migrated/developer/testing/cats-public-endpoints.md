# CATS Public Endpoints Handling

<!-- Migrated from: docs/developer/testing/cats-public-endpoints.md on 2025-01-24 -->

## Overview

This document describes how TMI handles public endpoints during CATS (Contract API Testing Service) security fuzzing to avoid false positives when testing authentication bypass scenarios.

## The Problem

CATS includes a `BypassAuthentication` fuzzer (Test 6) that tests all endpoints without authentication headers, expecting 401 or 403 responses. However, several TMI endpoints **must** be publicly accessible per RFC specifications:

- **RFC 8414**: OAuth 2.0 Authorization Server Metadata (`.well-known/*`)
- **RFC 7517**: JSON Web Key (JWK) endpoints (`jwks.json`)
- **RFC 6749**: OAuth 2.0 Authorization endpoints (`/oauth2/*`)
- **SAML 2.0**: SAML authentication flows (`/saml/*`)

Testing these endpoints for authentication bypass creates false positives, as returning 200 without authentication is **correct behavior** for these endpoints.

## Solution Architecture

TMI uses a **multi-layered approach** to reduce false positives in CATS security testing:

### UUID Field Skipping

CATS automatically skips all fields with `format: uuid` in the OpenAPI specification to avoid false positives from UUID validation fuzzers. This prevents tests that inject invalid characters into UUID fields from being flagged as errors when the API correctly returns 4xx responses.

**Implementation**: The `--skipFieldFormat=uuid` flag in [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) instructs CATS to skip replacement fuzzers (like `AbugidasInStringFields`, `ControlCharsInFields`, etc.) for all UUID-formatted fields.

**Rationale**: UUID fields have strict format requirements (RFC 4122). When CATS injects invalid characters into a UUID field and the API returns 400/403, this is **correct behavior**, not a vulnerability. Skipping these tests eliminates this category of false positives.

### Pagination Field Skipping

CATS tests pagination parameters with extreme values to detect boundary handling issues. However, some pagination behaviors are **correct** and should not be flagged as errors.

**Problem**: When CATS sends extreme offset values (e.g., `offset=9223372036854775807`), the API correctly returns:
- `200 OK` (request succeeded)
- Empty result array (no items at that offset)
- Preserved offset value in response

This is **standard pagination behavior** - not a bug or vulnerability. Major APIs (GitHub, Stripe, Slack) behave identically.

**Solution**: TMI skips the `offset` field from extreme value fuzzers to avoid false positives.

**Implementation**: The `--skipField=offset` flag in [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) excludes the offset parameter from boundary testing fuzzers like `ExtremePositiveNumbersInIntegerFields`.

**Rationale**:
- Extreme offsets don't cause crashes, timeouts, or resource exhaustion
- Returning empty results is the RESTful response (not 4XX errors)
- This is industry-standard pagination behavior
- The `limit` parameter is still tested (negative/extreme limits should fail validation)

### Error Leak Keywords Customization

CATS checks response bodies for error keywords that might indicate information leakage. However, some keywords are **legitimate** in standard protocol responses and should not be flagged as security issues.

**Problem**: CATS default error keywords include terms like "Unauthorized" and "Forbidden" which are:
- **Required** in OAuth 2.0 error responses (RFC 6749)
- **Standard** HTTP status text
- **Not** information leaks or security vulnerabilities

**Available Solution**: TMI has a custom error keywords file ([cats-error-keywords.txt](../../../cats-error-keywords.txt)) that excludes legitimate OAuth/auth terms while retaining detection of actual error leaks.

**Excluded Keywords** (legitimate in auth responses):
- `Unauthorized` - Standard 401 response term
- `Forbidden` - Standard 403 response term
- `InvalidToken` - OAuth error code (RFC 6749)
- `InvalidGrant` - OAuth error code (RFC 6749)
- `AuthenticationFailed` - OAuth error description
- `AuthenticationError` - OAuth error description
- `AuthorizationError` - OAuth error description

**Retained Keywords**: All other error patterns (stack traces, Java/Python/C# exceptions, database errors, etc.) are still detected to identify genuine information leaks.

**Note**: The custom error keywords file is available but currently not used by the CATS fuzzing script. The script uses CATS default error leak detection. To enable custom keywords, the `--errorLeaksKeywords` flag would need to be added to [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh).

### Public Endpoint Skipping

TMI uses OpenAPI vendor extensions to mark public endpoints and automatically exclude authentication bypass tests on those endpoints.

#### OpenAPI Vendor Extension-Based Approach (Current)

The [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) script uses the `--skipFuzzersForExtension` flag to skip the `BypassAuthentication` fuzzer on endpoints marked with `x-public-endpoint: true`:

```bash
cats_cmd+=(
    # Skip BypassAuthentication fuzzer on public endpoints marked in OpenAPI spec
    # Public endpoints (OAuth, OIDC, SAML) are marked with x-public-endpoint: true
    # per RFCs 8414, 7517, 6749, and SAML 2.0 specifications
    "--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication"
)
```

**How It Works**:
1. All 17 public endpoints in TMI are marked with `x-public-endpoint: true` in the OpenAPI specification
2. CATS reads the vendor extension and automatically skips the `BypassAuthentication` fuzzer for those endpoints
3. All other fuzzers (boundary testing, malformed input, etc.) still run on public endpoints
4. The OpenAPI specification serves as the single source of truth for public endpoint status

**Benefits**:
- **Declarative**: Public endpoint status defined in OpenAPI spec, not shell script
- **Surgical**: Only skips authentication tests, all other security tests still run
- **Self-documenting**: Schema clearly identifies public vs protected endpoints
- **Maintainable**: Add/remove public endpoints by updating OpenAPI spec only
- **Tool-friendly**: Other security scanners can use the same vendor extensions

**When This Applies**: All CATS fuzzing runs automatically use this approach.

#### OpenAPI Vendor Extensions

All public endpoints in [tmi-openapi.json](../../reference/apis/tmi-openapi.json) are marked with vendor extensions:

```json
{
  "/.well-known/openid-configuration": {
    "get": {
      "security": [],
      "x-public-endpoint": true,
      "x-authentication-required": false,
      "x-public-endpoint-purpose": "OIDC Discovery",
      "summary": "OpenID Connect Discovery Configuration",
      "description": "Returns OpenID Connect provider configuration metadata as per RFC 8414"
    }
  }
}
```

**Vendor Extensions**:
- `x-public-endpoint`: Boolean flag indicating this endpoint is intentionally public
- `x-authentication-required`: Boolean flag (false for public endpoints)
- `x-public-endpoint-purpose`: String describing why this endpoint is public (e.g., "OIDC Discovery", "OAuth Flow", "Health Check")

**Benefits**:
1. **Self-documenting**: Schema clearly identifies public vs protected endpoints
2. **Tool-friendly**: Other security scanners can discover public endpoints automatically
3. **Future-compatible**: If CATS adds support for vendor extensions, we're ready
4. **Audit trail**: Clear justification for why each endpoint is public

## Public Endpoint Categories

TMI has 17 public endpoint operations across 4 categories:

### 1. OIDC Discovery (5 endpoints)
- `GET /` - Health check / root endpoint
- `GET /.well-known/openid-configuration` - OpenID Connect discovery
- `GET /.well-known/oauth-authorization-server` - OAuth server metadata
- `GET /.well-known/oauth-protected-resource` - Protected resource metadata
- `GET /.well-known/jwks.json` - JSON Web Key Set

**RFC Compliance**: RFC 8414 requires these endpoints be publicly accessible

### 2. OAuth Flow (6 endpoints)
- `GET /oauth2/authorize` - OAuth authorization endpoint
- `GET /oauth2/callback` - OAuth callback handler
- `GET /oauth2/providers` - List available OAuth providers
- `POST /oauth2/token` - Token exchange endpoint
- `POST /oauth2/refresh` - Token refresh endpoint
- `POST /oauth2/introspect` - Token introspection endpoint

**RFC Compliance**: RFC 6749 OAuth 2.0 framework

### 3. SAML Flow (6 endpoints)
- `GET /saml/providers` - List available SAML providers
- `GET /saml/{provider}/login` - Initiate SAML login
- `GET /saml/{provider}/metadata` - SAML provider metadata
- `GET /saml/slo` - Single Logout (GET binding)
- `POST /saml/slo` - Single Logout (POST binding)
- `POST /saml/acs` - Assertion Consumer Service

**RFC Compliance**: SAML 2.0 Web Browser SSO Profile

## Cacheable Endpoints

Some discovery endpoints intentionally use `Cache-Control: public, max-age=3600` instead of the default `no-store` security header. This is **correct behavior** per OAuth/OIDC RFCs, as these endpoints return static configuration metadata that benefits from caching.

### The Problem

CATS `CheckSecurityHeaders` fuzzer expects all endpoints to return `Cache-Control: no-store` (or similar non-caching directives). However, discovery endpoints legitimately use public caching to:
- Reduce server load from repeated metadata requests
- Improve client performance during OAuth/OIDC flows
- Follow RFC recommendations for metadata caching

### Solution

TMI marks cacheable endpoints with `x-cacheable-endpoint: true` in the OpenAPI specification, and CATS is configured to skip the `CheckSecurityHeaders` fuzzer on these endpoints.

### Cacheable Endpoints (6 endpoints)

| Endpoint | Purpose | RFC Reference |
|----------|---------|---------------|
| `GET /.well-known/openid-configuration` | OIDC discovery metadata | RFC 8414 |
| `GET /.well-known/oauth-authorization-server` | OAuth AS metadata | RFC 8414 |
| `GET /.well-known/oauth-protected-resource` | Protected resource metadata | RFC 9728 |
| `GET /.well-known/jwks.json` | JSON Web Key Set | RFC 7517 |
| `GET /oauth2/providers` | OAuth provider list | Static config |
| `GET /saml/providers` | SAML provider list | Static config |

### Implementation

The [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) script uses:

```bash
"--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders"
```

This skips **only** the `CheckSecurityHeaders` fuzzer on endpoints marked with `x-cacheable-endpoint: true`. All other security fuzzers still run on these endpoints.

### Vendor Extensions

Each cacheable endpoint includes:

```json
{
  "x-cacheable-endpoint": true,
  "x-cacheable-endpoint-reason": "OIDC discovery metadata is static configuration data, cacheable per RFC 8414"
}
```

### Verifying Cacheable Endpoints

List all cacheable endpoints:
```bash
jq -r '.paths | to_entries[] | .key as $path | .value | to_entries[] | select(.value."x-cacheable-endpoint" == true) | "\($path) [\(.key | ascii_upcase)]"' docs/reference/apis/tmi-openapi.json
```

## Maintaining Public Endpoints

### Adding a New Public Endpoint

1. **Update OpenAPI Spec**: Set `security: []` in the operation definition
2. **Add Vendor Extensions**: Manually add the following vendor extensions to the endpoint in [tmi-openapi.json](../../reference/apis/tmi-openapi.json):
   ```json
   "x-public-endpoint": true,
   "x-authentication-required": false,
   "x-public-endpoint-purpose": "<purpose description>"
   ```
3. **Document**: Update this file with the new endpoint and its RFC justification

**Note**: No need to update `run-cats-fuzz.sh` - the `--skipFuzzersForExtension` parameter automatically picks up new endpoints marked with `x-public-endpoint: true`.

### Removing a Public Endpoint

1. **Update OpenAPI Spec**: Add `security: [{bearerAuth: []}]` to require authentication
2. **Remove Vendor Extensions**: Remove the `x-public-endpoint`, `x-authentication-required`, and `x-public-endpoint-purpose` fields from the endpoint in [tmi-openapi.json](../../reference/apis/tmi-openapi.json)
3. **Document**: Update this file

**Note**: The `--skipFuzzersForExtension` parameter will automatically stop skipping the endpoint once the vendor extension is removed.

### Auditing Public Endpoints

List all current public endpoints:
```bash
jq -r '.paths | to_entries[] | .key as $path | .value | to_entries[] | select(.value.security == []) | "\($path) [\(.key | ascii_upcase)]"' docs/reference/apis/tmi-openapi.json | sort
```

Verify vendor extensions are present:
```bash
jq '[.paths[][] | select(."x-public-endpoint" == true)] | length' docs/reference/apis/tmi-openapi.json
```

## Testing Public Endpoints

### Manual Testing

Test a specific public endpoint with CATS:
```bash
# This will include BypassAuthentication tests on the specified path
./scripts/run-cats-fuzz.sh -p /.well-known/openid-configuration
```

Test all endpoints including public ones (skips public paths):
```bash
# Public paths are automatically skipped to avoid false positives
./scripts/run-cats-fuzz.sh
```

### Verifying Public Access

Ensure public endpoints are accessible without authentication:
```bash
# Should return 200 OK without Authorization header
curl -i http://localhost:8080/.well-known/openid-configuration

# Should return 401 Unauthorized without Authorization header
curl -i http://localhost:8080/threat_models
```

## Maintaining Error Keywords

**Note**: The custom error keywords file ([cats-error-keywords.txt](../../../cats-error-keywords.txt)) is prepared but not currently used by the CATS fuzzing script. The script uses CATS default error leak detection. If custom keywords are needed, add `--errorLeaksKeywords=cats-error-keywords.txt` to the CATS command in [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh).

### Adding Keywords

If you enable custom error keywords and discover new error patterns that should be detected, add them to [cats-error-keywords.txt](../../../cats-error-keywords.txt):

```bash
# Edit the file and add the keyword on a new line
echo "NewErrorPattern" >> cats-error-keywords.txt
```

### Excluding Keywords

If a keyword causes false positives in legitimate responses:

1. **Verify it's legitimate**: Confirm the keyword appears in standard protocol responses (check RFCs)
2. **Remove from file**: Delete or comment out the line in [cats-error-keywords.txt](../../../cats-error-keywords.txt)
3. **Document the reason**: Add a comment explaining why it's excluded

**Example**:
```bash
# Excluded: Standard OAuth 2.0 error term per RFC 6749
# Unauthorized
```

### Syncing with CATS Defaults

CATS may add new error keywords in future releases. To sync:

1. Check CATS source: [WordUtils.java](https://github.com/Endava/cats/blob/main/src/main/java/com/endava/cats/util/WordUtils.java)
2. Compare with [cats-error-keywords.txt](../../../cats-error-keywords.txt)
3. Add new keywords while preserving TMI exclusions

## Additional CATS Configuration

### Other Field Formats

In addition to UUID fields, CATS can skip other field formats if they generate false positives:

```bash
# Skip multiple formats at once
--skipFieldFormat=uuid,date-time,email,uri,ipv4,ipv6

# Skip specific field names
--skipField=created_at,modified_at,internal_uuid

# Skip all tests on specific fields (prefix with !)
--skipField=!password,!secret_key
```

**TMI Current Configuration**:
- Format skipping: `--skipFieldFormat=uuid` (all UUID fields)
- Field skipping: `--skipField=offset` (pagination offset parameter)

These can be extended if other formats or fields generate false positives.

### Fuzzer-Specific Configuration

To skip specific fuzzers entirely, use `--skipFuzzers`:

```bash
# Skip specific fuzzers
cats --skipFuzzers=AbugidasInStringFields,ControlCharsInFields ...

# View all available fuzzers
cats --list
```

## Implementation History

**CATS Issue #185** ✅ **Implemented**: The CATS team added support for skipping fuzzers based on vendor extensions:
- Feature request: https://github.com/Endava/cats/issues/185
- Implementation: `--skipFuzzersForExtension` parameter
- TMI Usage:
  - `--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication` (skip auth bypass tests on public endpoints)
  - `--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders` (skip security header checks on cacheable discovery endpoints)
- This allows vendor extension-based test filtering, providing a cleaner alternative to maintaining path lists in shell scripts

## IDOR False Positives

The CATS `InsecureDirectObjectReferences` fuzzer tests for Insecure Direct Object Reference vulnerabilities by replacing ID fields with alternative values and checking if the API returns success (indicating potential unauthorized access).

### Filter Parameters Are Not IDOR Vulnerabilities

TMI has several endpoints that accept **filter parameters** (query parameters that narrow results). When CATS changes these filter values, the API correctly returns 200 with different (or empty) results - this is **expected REST API behavior**, not an IDOR vulnerability.

**Affected Endpoints**:

| Endpoint | Filter Parameter | Behavior |
|----------|------------------|----------|
| `GET /addons` | `threat_model_id` | Returns addons filtered by threat model (empty if no match) |
| `GET /invocations` | `addon_id` | Returns invocations filtered by addon (empty if no match) |
| `GET /webhooks/subscriptions` | various filters | Returns subscriptions matching filters |
| `GET /threat_models` | various filters | Returns threat models matching filters |

**Why This Is Correct Behavior**:

1. **Filter parameters narrow results, not authorize access** - Changing a filter returns different data, not unauthorized data
2. **Empty results are valid** - A filter that matches nothing returns an empty list (200), not an error
3. **Addons are globally visible** - They are admin-only to create/modify but visible to all authenticated users
4. **No sensitive data exposure** - The user only sees data they are authorized to see

**Resolution**: The CATS results parser (`scripts/parse-cats-results.py`) automatically marks these as false positives in the `is_false_positive()` function. They appear in the database with `is_oauth_false_positive = 1` and are excluded from the `test_results_filtered_view`.

### Admin Endpoint Access

Admin endpoints (`/admin/*`) are also marked as IDOR false positives because:

1. The test user (e.g., `charlie`) is an administrator
2. Administrators have full access to admin resources regardless of ID values
3. Returning 200 for modified IDs is correct admin behavior

## References

- **RFC 8414**: OAuth 2.0 Authorization Server Metadata - https://datatracker.ietf.org/doc/html/rfc8414
- **RFC 7517**: JSON Web Key (JWK) - https://datatracker.ietf.org/doc/html/rfc7517
- **RFC 6749**: OAuth 2.0 Authorization Framework - https://datatracker.ietf.org/doc/html/rfc6749
- **RFC 9728**: OAuth 2.0 Protected Resource Metadata - https://datatracker.ietf.org/doc/html/rfc9728
- **CATS Tool**: https://github.com/Endava/cats
- **CATS Issue #185**: Tag-based test filtering - https://github.com/Endava/cats/issues/185
- **OpenAPI Vendor Extensions**: https://swagger.io/docs/specification/openapi-extensions/

## Related Files

- [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) - CATS fuzzing script with UUID skipping and public endpoint handling
- [cats-error-keywords.txt](../../../cats-error-keywords.txt) - Custom error leak keywords file (excludes OAuth/auth terms, available but not currently used)
- [docs/reference/apis/tmi-openapi.json](../../reference/apis/tmi-openapi.json) - OpenAPI specification with vendor extensions and UUID formats

---

## Verification Summary

*Verified on 2025-01-24*

| Item | Status | Notes |
|------|--------|-------|
| File paths | Verified | `scripts/run-cats-fuzz.sh`, `cats-error-keywords.txt`, `tmi-openapi.json` exist |
| Make targets | Verified | `cats-fuzz`, `cats-fuzz-user`, `cats-fuzz-path`, `analyze-cats-results` in Makefile |
| `--skipFuzzersForExtension` flag | Verified | Confirmed in run-cats-fuzz.sh lines 374, 380 |
| `--skipFieldFormat=uuid` flag | Verified | Confirmed in run-cats-fuzz.sh line 365 |
| `--skipField=offset` flag | Verified | Confirmed in run-cats-fuzz.sh line 366 |
| 17 public endpoints count | Verified | Confirmed via jq query on OpenAPI spec |
| 6 cacheable endpoints count | Verified | Confirmed via jq query on OpenAPI spec |
| `x-public-endpoint` extension | Verified | Found 35 occurrences (17 endpoints x 2 properties each + extras) |
| `x-cacheable-endpoint` extension | Verified | Found 12 occurrences (6 endpoints x 2 properties each) |
| RFC 8414 | Verified | OAuth 2.0 Authorization Server Metadata |
| RFC 7517 | Verified | JSON Web Key (JWK) |
| RFC 6749 | Verified | OAuth 2.0 Authorization Framework |
| RFC 9728 | Verified | OAuth 2.0 Protected Resource Metadata |
| CATS GitHub repo | Verified | Active project at github.com/Endava/cats |
| CATS Issue #185 | Verified | Closed as completed, feature implemented |
| `is_false_positive()` function | Verified | Found in scripts/parse-cats-results.py |
| `is_oauth_false_positive` column | Verified | Found in parse-cats-results.py schema |
| `test_results_filtered_view` | Verified | Found in parse-cats-results.py |

**Corrections Made:**
1. Removed reference to non-existent `scripts/add-public-endpoint-markers.sh`
2. Updated "Adding a New Public Endpoint" section with manual vendor extension instructions
3. Updated "Removing a Public Endpoint" section with manual removal instructions
4. Clarified that `cats-error-keywords.txt` is available but not currently used by the script
5. Added note about enabling custom error keywords if needed
