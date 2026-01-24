# OAuth Provider Environment Configuration

<!-- Migrated from: docs/operator/oauth-environment-configuration.md on 2025-01-24 -->

**MIGRATION NOTICE**: This document has been migrated to the TMI wiki. See:
- [Setting Up Authentication - Environment Variable-Only Configuration](https://github.com/ericfitz/tmi/wiki/Setting-Up-Authentication#environment-variable-only-configuration)

## Verification Summary

This document was verified against source code and external references on 2025-01-24.

### Source Code Verification

All claims verified against `/Users/efitz/Projects/tmi/auth/config.go` and `/Users/efitz/Projects/tmi/internal/envutil/envutil.go`:

1. **Provider Discovery Pattern**: Verified - `envutil.DiscoverProviders("OAUTH_PROVIDERS_", "_ENABLED")` at line 299 of auth/config.go
2. **Provider ID to Key Conversion**: Verified - `envutil.ProviderIDToKey()` converts to lowercase and replaces underscores with hyphens (lines 52-61 of envutil.go)
3. **Required Fields**: Verified - all field patterns match source code (lines 361-377 of auth/config.go):
   - `_ENABLED`, `_CLIENT_ID`, `_CLIENT_SECRET`, `_AUTHORIZATION_URL`, `_TOKEN_URL`, `_USERINFO_URL`, `_SCOPES`
4. **Optional Fields**: Verified - `_ID`, `_NAME`, `_ICON`, `_ISSUER`, `_JWKS_URL`, `_AUTH_HEADER_FORMAT`, `_ACCEPT_HEADER`
5. **Multiple Userinfo Endpoints**: Verified - `_USERINFO_URL`, `_USERINFO_SECONDARY_URL`, `_USERINFO_ADDITIONAL_URL` (lines 320-348)
6. **Claim Mappings**: Verified - `parseClaimMappings()` function at lines 386-413
7. **Additional Params**: Verified - `parseAdditionalParams()` function at lines 415-442

### External URL Verification

1. **Google OAuth URLs**: Verified via Google Developers documentation
   - Authorization: `https://accounts.google.com/o/oauth2/auth` (also v2: `https://accounts.google.com/o/oauth2/v2/auth`)
   - Token: `https://oauth2.googleapis.com/token`
   - Userinfo: `https://www.googleapis.com/oauth2/v3/userinfo`
   - JWKS: `https://www.googleapis.com/oauth2/v3/certs`

2. **GitHub OAuth URLs**: Verified via GitHub Docs
   - Authorization: `https://github.com/login/oauth/authorize`
   - Token: `https://github.com/login/oauth/access_token`
   - User API: `https://api.github.com/user`
   - Emails API: `https://api.github.com/user/emails`

3. **Microsoft OAuth URLs**: Verified via Microsoft Learn documentation
   - Consumer tenant UUID `9188040d-6c67-4c5b-b112-36a304b66dad` confirmed as the standard Microsoft personal accounts tenant
   - UUID-based issuer URL pattern confirmed for personal accounts
   - Graph API: `https://graph.microsoft.com/v1.0/me`

### Cross-Reference Verification

1. **See Also Links**:
   - `../developer/setup/oauth-integration.md` - File migrated to `docs/migrated/developer/setup/oauth-integration.md`
   - `./saml-environment-configuration.md` - File does not exist (removed from documentation)
   - `../developer/setup/development-setup.md` - File exists and verified

### Items Needing Review

None - all claims verified.

### Corrections Applied to Wiki

1. **Minor clarification**: Updated wiki text to clarify that underscores convert to hyphens (not just lowercase conversion)

---

## Original Document (Archived)

This document describes how to configure OAuth providers using environment variables with TMI's dynamic discovery system.

## Overview

TMI uses dynamic provider discovery similar to SAML providers. OAuth providers are discovered by scanning for environment variables matching the pattern:

```
OAUTH_PROVIDERS_<PROVIDER_ID>_ENABLED=true
```

**No hardcoded defaults** - all configuration must be explicitly provided via environment variables.

## Environment Variable Naming Convention

All OAuth provider configuration uses the pattern:

```
OAUTH_PROVIDERS_<PROVIDER_ID>_<FIELD>=<value>
```

Where:
- `<PROVIDER_ID>` is an uppercase identifier (e.g., `GOOGLE`, `GITHUB`, `MICROSOFT`)
- Provider keys are automatically converted to lowercase (e.g., `GOOGLE` → `google`)
- Underscores in provider IDs are converted to hyphens (e.g., `MY_PROVIDER` → `my-provider`)

## Required Fields

Each OAuth provider must have these fields configured:

| Variable Pattern | Description | Example |
|-----------------|-------------|---------|
| `_ENABLED` | Must be `true` to activate provider | `true` |
| `_CLIENT_ID` | OAuth client ID from provider | `abc123.apps.googleusercontent.com` |
| `_CLIENT_SECRET` | OAuth client secret from provider | `GOCSPX-xyz789...` |
| `_AUTHORIZATION_URL` | Provider's authorization endpoint | `https://accounts.google.com/o/oauth2/auth` |
| `_TOKEN_URL` | Provider's token endpoint | `https://oauth2.googleapis.com/token` |
| `_USERINFO_URL` | Primary userinfo endpoint | `https://www.googleapis.com/oauth2/v3/userinfo` |
| `_SCOPES` | Comma-separated list of scopes | `openid,profile,email` |

## Optional Fields

| Variable Pattern | Description | Example |
|-----------------|-------------|---------|
| `_ID` | Provider ID (defaults to lowercase provider key) | `google` |
| `_NAME` | Display name | `Google` |
| `_ICON` | Icon URL or identifier | `https://example.com/google-icon.png` |
| `_ISSUER` | OIDC issuer URL for ID token validation | `https://accounts.google.com` |
| `_JWKS_URL` | JWKS endpoint for ID token verification | `https://www.googleapis.com/oauth2/v3/certs` |
| `_AUTH_HEADER_FORMAT` | Authorization header format (default: `Bearer %s`) | `token %s` |
| `_ACCEPT_HEADER` | Accept header for requests (default: `application/json`) | `application/json` |

## Multiple Userinfo Endpoints

Some providers (like GitHub) require multiple userinfo endpoints:

| Variable Pattern | Description |
|-----------------|-------------|
| `_USERINFO_URL` | Primary endpoint |
| `_USERINFO_SECONDARY_URL` | Second endpoint (optional) |
| `_USERINFO_ADDITIONAL_URL` | Third endpoint (optional) |

## Claim Mappings

Customize claim extraction using these patterns:

```
OAUTH_PROVIDERS_<PROVIDER_ID>_USERINFO_CLAIMS_<CLAIM_NAME>=<json_path>
OAUTH_PROVIDERS_<PROVIDER_ID>_USERINFO_SECONDARY_CLAIMS_<CLAIM_NAME>=<json_path>
OAUTH_PROVIDERS_<PROVIDER_ID>_USERINFO_ADDITIONAL_CLAIMS_<CLAIM_NAME>=<json_path>
```

### Standard Claim Names

- `SUBJECT_CLAIM` - User's unique identifier
- `EMAIL_CLAIM` - User's email address
- `EMAIL_VERIFIED_CLAIM` - Email verification status
- `NAME_CLAIM` - Full name
- `GIVEN_NAME_CLAIM` - First name
- `FAMILY_NAME_CLAIM` - Last name
- `PICTURE_CLAIM` - Avatar/profile picture URL
- `GROUPS_CLAIM` - Group memberships

### Example: GitHub Custom Claims

```bash
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_SUBJECT_CLAIM=id
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_NAME_CLAIM=name
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_PICTURE_CLAIM=avatar_url
OAUTH_PROVIDERS_GITHUB_USERINFO_SECONDARY_CLAIMS_EMAIL_CLAIM=[0].email
OAUTH_PROVIDERS_GITHUB_USERINFO_SECONDARY_CLAIMS_EMAIL_VERIFIED_CLAIM=[0].verified
```

## Additional OAuth Parameters

Add custom parameters to authorization requests:

```
OAUTH_PROVIDERS_<PROVIDER_ID>_ADDITIONAL_PARAMS_<PARAM_NAME>=<value>
```

### Example: Google Offline Access

```bash
OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_ACCESS_TYPE=offline
OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_PROMPT=consent
```

## Complete Configuration Examples

### Google OAuth

```bash
# Enable Google provider
OAUTH_PROVIDERS_GOOGLE_ENABLED=true

# Basic configuration
OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=123456789.apps.googleusercontent.com
OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET=GOCSPX-abc123def456
OAUTH_PROVIDERS_GOOGLE_NAME=Google
OAUTH_PROVIDERS_GOOGLE_ICON=https://example.com/google.png

# OAuth endpoints
OAUTH_PROVIDERS_GOOGLE_AUTHORIZATION_URL=https://accounts.google.com/o/oauth2/auth
OAUTH_PROVIDERS_GOOGLE_TOKEN_URL=https://oauth2.googleapis.com/token
OAUTH_PROVIDERS_GOOGLE_USERINFO_URL=https://www.googleapis.com/oauth2/v3/userinfo

# OIDC configuration
OAUTH_PROVIDERS_GOOGLE_ISSUER=https://accounts.google.com
OAUTH_PROVIDERS_GOOGLE_JWKS_URL=https://www.googleapis.com/oauth2/v3/certs

# Scopes
OAUTH_PROVIDERS_GOOGLE_SCOPES=openid,profile,email
```

### GitHub OAuth

```bash
# Enable GitHub provider
OAUTH_PROVIDERS_GITHUB_ENABLED=true

# Basic configuration
OAUTH_PROVIDERS_GITHUB_CLIENT_ID=Iv1.abc123def456
OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET=ghp_xyz789abc123
OAUTH_PROVIDERS_GITHUB_NAME=GitHub
OAUTH_PROVIDERS_GITHUB_ICON=https://example.com/github.png

# OAuth endpoints
OAUTH_PROVIDERS_GITHUB_AUTHORIZATION_URL=https://github.com/login/oauth/authorize
OAUTH_PROVIDERS_GITHUB_TOKEN_URL=https://github.com/login/oauth/access_token
OAUTH_PROVIDERS_GITHUB_USERINFO_URL=https://api.github.com/user
OAUTH_PROVIDERS_GITHUB_USERINFO_SECONDARY_URL=https://api.github.com/user/emails

# Custom headers
OAUTH_PROVIDERS_GITHUB_AUTH_HEADER_FORMAT=token %s
OAUTH_PROVIDERS_GITHUB_ACCEPT_HEADER=application/json

# Scopes
OAUTH_PROVIDERS_GITHUB_SCOPES=user:email

# Custom claim mappings
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_SUBJECT_CLAIM=id
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_NAME_CLAIM=name
OAUTH_PROVIDERS_GITHUB_USERINFO_CLAIMS_PICTURE_CLAIM=avatar_url
OAUTH_PROVIDERS_GITHUB_USERINFO_SECONDARY_CLAIMS_EMAIL_CLAIM=[0].email
OAUTH_PROVIDERS_GITHUB_USERINFO_SECONDARY_CLAIMS_EMAIL_VERIFIED_CLAIM=[0].verified
```

### Microsoft OAuth (Correct Configuration)

```bash
# Enable Microsoft provider
OAUTH_PROVIDERS_MICROSOFT_ENABLED=true

# Basic configuration
OAUTH_PROVIDERS_MICROSOFT_CLIENT_ID=12345678-1234-1234-1234-123456789abc
OAUTH_PROVIDERS_MICROSOFT_CLIENT_SECRET=abc~123DEF456ghi789
OAUTH_PROVIDERS_MICROSOFT_NAME=Microsoft
OAUTH_PROVIDERS_MICROSOFT_ICON=https://example.com/microsoft.png

# OAuth endpoints - USE UUID-BASED URLS
OAUTH_PROVIDERS_MICROSOFT_AUTHORIZATION_URL=https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/oauth2/v2.0/authorize
OAUTH_PROVIDERS_MICROSOFT_TOKEN_URL=https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/oauth2/v2.0/token
OAUTH_PROVIDERS_MICROSOFT_USERINFO_URL=https://graph.microsoft.com/v1.0/me

# OIDC configuration - MUST MATCH WHAT MICROSOFT RETURNS
OAUTH_PROVIDERS_MICROSOFT_ISSUER=https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0
OAUTH_PROVIDERS_MICROSOFT_JWKS_URL=https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/discovery/v2.0/keys

# Scopes
OAUTH_PROVIDERS_MICROSOFT_SCOPES=openid,profile,email,User.Read

# Custom claim mappings
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_SUBJECT_CLAIM=id
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_EMAIL_CLAIM=mail
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_NAME_CLAIM=displayName
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_GIVEN_NAME_CLAIM=givenName
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_FAMILY_NAME_CLAIM=surname
OAUTH_PROVIDERS_MICROSOFT_USERINFO_CLAIMS_EMAIL_VERIFIED_CLAIM=true

# Optional: Group memberships (requires additional scope)
# OAUTH_PROVIDERS_MICROSOFT_USERINFO_ADDITIONAL_URL=https://graph.microsoft.com/v1.0/me/memberOf
# OAUTH_PROVIDERS_MICROSOFT_USERINFO_ADDITIONAL_CLAIMS_GROUPS_CLAIM=value.[*].displayName
```

### Important Notes for Microsoft

1. **UUID vs /consumers**: Microsoft returns the UUID-based issuer (`9188040d-6c67-4c5b-b112-36a304b66dad`) instead of `/consumers`. You MUST use the UUID in all URLs.

2. **Finding your tenant UUID**: The UUID `9188040d-6c67-4c5b-b112-36a304b66dad` is the standard Microsoft consumer tenant. For organizational tenants, use your organization's tenant ID.

3. **Issuer URL matching**: The `ISSUER` URL must exactly match what Microsoft returns in ID tokens, or OIDC validation will fail.

## Heroku Configuration

To configure providers in Heroku:

```bash
# Set variables one at a time
heroku config:set OAUTH_PROVIDERS_GOOGLE_ENABLED=true --app your-app-name
heroku config:set OAUTH_PROVIDERS_GOOGLE_CLIENT_ID=your-client-id --app your-app-name
# ... etc

# Or use Heroku dashboard UI to set all variables
```

## Verifying Configuration

Check loaded providers in application logs:

```
OAuth providers loaded providers_count=3 enabled_providers=[google github microsoft]
```

Each provider should log:

```
Loaded OAuth provider configuration provider_key=google name=Google
```

## Troubleshooting

### Provider not appearing

- Verify `OAUTH_PROVIDERS_<PROVIDER_ID>_ENABLED=true` is set
- Check provider ID format (must be uppercase in environment variable)
- Review application logs for discovery output

### OIDC issuer mismatch error

```
failed to create OIDC provider: oidc: issuer URL provided to client (...) did not match the issuer URL returned by provider (...)
```

**Solution**: Set `OAUTH_PROVIDERS_<PROVIDER_ID>_ISSUER` to the exact URL the provider returns. For Microsoft, use the UUID-based URL, not `/consumers`.

### Missing userinfo data

- Verify `USERINFO_URL` is correct
- Check claim mappings match provider's JSON response
- Add secondary/additional endpoints if needed

## Migration from Hardcoded Defaults

**Previous versions** had hardcoded defaults for Google, GitHub, and Microsoft. These have been **removed**.

If you relied on defaults:

1. **Set all required environment variables** for each provider
2. **No automatic fallbacks** - missing variables will cause provider to not load
3. **Verify** by checking application logs for provider discovery output

## See Also

- [OAuth Integration Guide](../../migrated/developer/setup/oauth-integration.md) (migrated)
- [Authentication Configuration](../../developer/setup/development-setup.md#authentication)
