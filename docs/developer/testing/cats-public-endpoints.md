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

TMI uses a **two-phase approach** to handle public endpoints:

### Phase 1: CATS Script Configuration (Immediate)

The [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) script explicitly skips public endpoints using the `--skipPaths` flag:

```bash
# Public endpoints that must be accessible without authentication per RFCs
local public_paths=(
    "/"
    "/.well-known/jwks.json"
    "/.well-known/oauth-authorization-server"
    "/.well-known/oauth-protected-resource"
    "/.well-known/openid-configuration"
    "/oauth2/authorize"
    "/oauth2/callback"
    "/oauth2/introspect"
    "/oauth2/providers"
    "/oauth2/refresh"
    "/oauth2/token"
    "/saml/acs"
    "/saml/providers"
    "/saml/slo"
    "/saml/{provider}/login"
    "/saml/{provider}/metadata"
)

# Skip these paths when running full test suite
cats_cmd+=("--skipPaths=${skip_paths_arg}")
```

**When This Applies**: Only when running full CATS fuzzing (no specific path filter). If you specify a specific path with `-p/--path`, the skip logic is bypassed to allow targeted testing.

### Phase 2: OpenAPI Schema Markers (Future-Proof)

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
3. **Update CATS Script**: Add the path to `public_paths` array in `scripts/run-cats-fuzz.sh`
4. **Document**: Update this file with the new endpoint and its RFC justification

### Removing a Public Endpoint

1. **Update OpenAPI Spec**: Add `security: [{bearerAuth: []}]` to require authentication
2. **Remove from CATS Script**: Remove path from `public_paths` array
3. **Clean Vendor Extensions**: Re-run the marker script to remove extensions
4. **Document**: Update this file

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

## Future Improvements

**CATS Issue #185**: We've filed an issue requesting the ability to include/exclude (skip) tests based on OpenAPI tags:
- https://github.com/Endava/cats/issues/185
- If implemented, this would allow tag-based test filtering (e.g., skip auth tests on "OIDC Discovery" tag)
- Would provide a cleaner alternative to maintaining path lists in the script

## References

- **RFC 8414**: OAuth 2.0 Authorization Server Metadata - https://datatracker.ietf.org/doc/html/rfc8414
- **RFC 7517**: JSON Web Key (JWK) - https://datatracker.ietf.org/doc/html/rfc7517
- **RFC 6749**: OAuth 2.0 Authorization Framework - https://datatracker.ietf.org/doc/html/rfc6749
- **CATS Tool**: https://github.com/Endava/cats
- **CATS Issue #185**: Tag-based test filtering - https://github.com/Endava/cats/issues/185
- **OpenAPI Vendor Extensions**: https://swagger.io/docs/specification/openapi-extensions/

## Related Files

- [scripts/run-cats-fuzz.sh](../../../scripts/run-cats-fuzz.sh) - CATS fuzzing script with public endpoint handling
- [scripts/add-public-endpoint-markers.sh](../../../scripts/add-public-endpoint-markers.sh) - Script to add vendor extensions
- [docs/reference/apis/tmi-openapi.json](../../reference/apis/tmi-openapi.json) - OpenAPI specification with vendor extensions
