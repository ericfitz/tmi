# CATS Public Endpoints Handling

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

**Solution**: TMI uses a custom error keywords file ([cats-error-keywords.txt](../../../cats-error-keywords.txt)) that excludes legitimate OAuth/auth terms while retaining detection of actual error leaks.

**Excluded Keywords** (legitimate in auth responses):
- `Unauthorized` - Standard 401 response term
- `Forbidden` - Standard 403 response term
- `InvalidToken` - OAuth error code (RFC 6749)
- `InvalidGrant` - OAuth error code (RFC 6749)
- `AuthenticationFailed` - OAuth error description
- `AuthenticationError` - OAuth error description
- `AuthorizationError` - OAuth error description

**Retained Keywords**: All other error patterns (stack traces, Java/Python/C# exceptions, database errors, etc.) are still detected to identify genuine information leaks.

**Implementation**: The `--errorLeaksKeywords` flag in [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) points to the custom keywords file.

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

## Maintaining Public Endpoints

### Adding a New Public Endpoint

1. **Update OpenAPI Spec**: Set `security: []` in the operation definition
2. **Add Vendor Extensions**: Run the marker script:
   ```bash
   ./scripts/add-public-endpoint-markers.sh
   ```
3. **Document**: Update this file with the new endpoint and its RFC justification

**Note**: No need to update `run-cats-fuzz.sh` - the `--skipFuzzersForExtension` parameter automatically picks up new endpoints marked with `x-public-endpoint: true`.

### Removing a Public Endpoint

1. **Update OpenAPI Spec**: Add `security: [{bearerAuth: []}]` to require authentication
2. **Clean Vendor Extensions**: Re-run the marker script to remove extensions
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

### Adding Keywords

If you discover new error patterns that should be detected, add them to [cats-error-keywords.txt](../../../cats-error-keywords.txt):

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
- TMI Usage: `--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication`
- This allows vendor extension-based test filtering, providing a cleaner alternative to maintaining path lists in shell scripts

## References

- **RFC 8414**: OAuth 2.0 Authorization Server Metadata - https://datatracker.ietf.org/doc/html/rfc8414
- **RFC 7517**: JSON Web Key (JWK) - https://datatracker.ietf.org/doc/html/rfc7517
- **RFC 6749**: OAuth 2.0 Authorization Framework - https://datatracker.ietf.org/doc/html/rfc6749
- **CATS Tool**: https://github.com/Endava/cats
- **CATS Issue #185**: Tag-based test filtering - https://github.com/Endava/cats/issues/185
- **OpenAPI Vendor Extensions**: https://swagger.io/docs/specification/openapi-extensions/

## Related Files

- [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) - CATS fuzzing script with UUID skipping, error keyword customization, and public endpoint handling
- [cats-error-keywords.txt](../../../cats-error-keywords.txt) - Custom error leak keywords file (excludes OAuth/auth terms)
- [scripts/add-public-endpoint-markers.sh](../../../scripts/add-public-endpoint-markers.sh) - Script to add vendor extensions to OpenAPI spec
- [docs/reference/apis/tmi-openapi.json](../../reference/apis/tmi-openapi.json) - OpenAPI specification with vendor extensions and UUID formats
