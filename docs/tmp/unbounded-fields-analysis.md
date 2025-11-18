# Unbounded String Fields - Detailed Analysis

**Date**: 2025-01-18
**Total Unbounded Fields**: 108 occurrences (74 unique field definitions)

## Summary by Context

| Context | Occurrences | Unique Fields | Recommendation |
|---------|-------------|---------------|----------------|
| Query/Path Parameters | 20 | 20 | Add maxLength based on type |
| Request Body | 28 | 28 | Add maxLength based on type |
| Response | 33 | 33 | Add maxLength based on type |
| Schema Definition | 27 | 27 | Add maxLength based on type |

---

## 1. Query/Path Parameters (20 fields)

### OAuth Parameters
- `paths./oauth2/authorize.get.parameters.0.schema` - **idp** parameter
  - **Recommendation**: maxLength 100 (aligns with database VARCHAR(100))
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$` (identifier)

- `paths./oauth2/authorize.get.parameters.2.schema` - **state** parameter
  - **Recommendation**: maxLength 256 (OAuth state tokens typically 128-256)
  - **Pattern**: `^[a-zA-Z0-9_-]{1,256}$`

- `paths./oauth2/authorize.get.parameters.4.schema` - **client_callback** parameter
  - **Recommendation**: maxLength 1024 (URL)
  - **Pattern**: `^(https?|wss?)://[^\s]{1,1020}$`

- `paths./oauth2/callback.get.parameters.0.schema` - **code** parameter
  - **Recommendation**: maxLength 512 (OAuth authorization codes)
  - **Pattern**: `^[a-zA-Z0-9_-]{1,512}$`

- `paths./oauth2/callback.get.parameters.1.schema` - **state** parameter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9_-]{1,256}$`

- `paths./oauth2/token.post.parameters.0.schema` - **Content-Type** header
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-zA-Z0-9./_+-]{1,100}$`

- `paths./oauth2/providers/{idp}/groups.get.parameters.0.schema` - **idp** path param
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$`

### Threat Model Query Parameters
- `paths./threat_models.get.parameters.2.schema` - **name** filter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$` (Unicode)

- `paths./threat_models.get.parameters.3.schema` - **owner** filter
  - **Recommendation**: maxLength 256 (email)
  - **Pattern**: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

- `paths./threat_models.get.parameters.4.schema` - **created_by** filter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$` (Unicode name)

- `paths./threat_models.get.parameters.5.schema` - **framework** filter
  - **Recommendation**: maxLength 50 (aligns with database VARCHAR(50))
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,49}$`

- `paths./threat_models.get.parameters.10.schema` - **sort_by** parameter
  - **Recommendation**: maxLength 50
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_]{0,49}$` (field name)

### Threat Query Parameters
- `paths./threat_models/{threat_model_id}/threats.get.parameters.3.schema` - **name** filter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

- `paths./threat_models/{threat_model_id}/threats.get.parameters.4.schema` - **severity** filter
  - **Recommendation**: Make this an ENUM instead
  - **Values**: ["Unknown", "None", "Low", "Medium", "High", "Critical"]

- `paths./threat_models/{threat_model_id}/threats.get.parameters.5.schema` - **priority** filter
  - **Recommendation**: maxLength 16 (aligns with database VARCHAR(16))
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9 ]{0,15}$`

- `paths./threat_models/{threat_model_id}/threats.get.parameters.6.schema` - **status** filter
  - **Recommendation**: maxLength 256 (aligns with database VARCHAR(256))
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

- `paths./threat_models/{threat_model_id}/threats.get.parameters.8.schema` - **sort_by** parameter
  - **Recommendation**: maxLength 50
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_]{0,49}$`

- `paths./threat_models/{threat_model_id}/threats.get.parameters.9.schema` - **risk_level** filter
  - **Recommendation**: maxLength 50
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9 ]{0,49}$`

### SAML Parameters
- `paths./saml/slo.get.parameters.0.schema` - **SAMLRequest** parameter
  - **Recommendation**: maxLength 10240 (Base64 SAML request ~7.5KB)
  - **Pattern**: `^[A-Za-z0-9+/]*={0,2}$`

### User Deletion Parameters
- `paths./users/me.delete.parameters.0.schema` - **confirmation** parameter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

---

## 2. Request Body Fields (28 fields)

### OAuth Token Requests
- `paths./oauth2/token.post.requestBody.content.application/json.schema.properties.code`
  - **Field**: Authorization code
  - **Recommendation**: maxLength 512
  - **Pattern**: `^[a-zA-Z0-9_-]{1,512}$`

- `paths./oauth2/token.post.requestBody.content.application/json.schema.properties.state`
  - **Field**: State parameter
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9_-]{1,256}$`

- `paths./oauth2/refresh.post.requestBody.content.application/json.schema.properties.refresh_token`
  - **Field**: Refresh token
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[a-zA-Z0-9_.-]{1,1024}$`

### Token Introspection
- `paths./oauth2/introspect.post.requestBody.content.application/x-www-form-urlencoded.schema.properties.token`
  - **Field**: JWT token to introspect
  - **Recommendation**: maxLength 4096 (JWTs can be 2-4KB)
  - **Pattern**: `^[a-zA-Z0-9_.-]{1,4096}$`

- `paths./oauth2/introspect.post.requestBody.content.application/x-www-form-urlencoded.schema.properties.token_type_hint`
  - **Field**: Token type hint
  - **Recommendation**: Make this an ENUM
  - **Values**: ["access_token", "refresh_token"]

### SAML Requests
- `paths./saml/acs.post.requestBody.content.application/x-www-form-urlencoded.schema.properties.SAMLResponse`
  - **Field**: Base64-encoded SAML response
  - **Recommendation**: maxLength 102400 (100KB)
  - **Pattern**: `^[A-Za-z0-9+/]*={0,2}$`

- `paths./saml/acs.post.requestBody.content.application/x-www-form-urlencoded.schema.properties.RelayState`
  - **Field**: SAML relay state
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9_-]{1,256}$`

- `paths./saml/slo.post.requestBody.content.application/x-www-form-urlencoded.schema.properties.SAMLRequest`
  - **Field**: Base64-encoded SAML logout request
  - **Recommendation**: maxLength 10240 (10KB)
  - **Pattern**: `^[A-Za-z0-9+/]*={0,2}$`

### JSON Patch Operations
- `*.requestBody.content.application/json-patch+json.schema.items.properties.path`
  - **Field**: JSON path (RFC 6901)
  - **Recommendation**: maxLength 256
  - **Pattern**: `^(/[^/]+)*$` (JSON Pointer format)

- `*.requestBody.content.application/json-patch+json.schema.items.properties.from`
  - **Field**: Source JSON path for move/copy
  - **Recommendation**: maxLength 256
  - **Pattern**: `^(/[^/]+)*$`

### Metadata Updates
- `paths./*/metadata/{key}.put.requestBody.content.application/json.schema.properties.value`
  - **Field**: Metadata value
  - **Recommendation**: maxLength 65535 (aligns with database CHECK constraint)
  - **Pattern**: `^[\u0020-\uFFFF]{1,65535}$`

### Bulk Patch Operations
- `*.requestBody.content.application/json.schema.properties.patches.items.properties.operations.items.properties.path`
  - **Field**: JSON path
  - **Recommendation**: maxLength 256
  - **Pattern**: `^(/[^/]+)*$`

- `*.requestBody.content.application/json.schema.properties.patches.items.properties.operations.items.properties.from`
  - **Field**: Source path
  - **Recommendation**: maxLength 256
  - **Pattern**: `^(/[^/]+)*$`

---

## 3. Response Fields (33 fields)

### OIDC Discovery Responses
- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.scopes_supported.items`
  - **Field**: OAuth scope tokens (RFC 6749)
  - **Recommendation**: maxLength 64 per item, maxItems 20
  - **Pattern**: `^[\x21\x23-\x5B\x5D-\x7E]{3,64}$`

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.response_types_supported.items`
  - **Field**: OAuth response types
  - **Recommendation**: Make this an ENUM
  - **Values**: ["code", "token", "id_token", "code token", "code id_token", "token id_token", "code token id_token"]

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.subject_types_supported.items`
  - **Field**: Subject identifier types
  - **Recommendation**: Make this an ENUM
  - **Values**: ["public", "pairwise"]

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.id_token_signing_alg_values_supported.items`
  - **Field**: JWT signing algorithms
  - **Recommendation**: Make this an ENUM
  - **Values**: ["RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "none"]

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.claims_supported.items`
  - **Field**: OIDC claims
  - **Recommendation**: maxLength 64
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_]{0,63}$`

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.grant_types_supported.items`
  - **Field**: OAuth grant types
  - **Recommendation**: Make this an ENUM
  - **Values**: ["authorization_code", "implicit", "refresh_token", "client_credentials", "password", "urn:ietf:params:oauth:grant-type:jwt-bearer"]

- `paths./.well-known/openid-configuration.get.responses.200.content.application/json.schema.properties.token_endpoint_auth_methods_supported.items`
  - **Field**: Token endpoint auth methods
  - **Recommendation**: Make this an ENUM
  - **Values**: ["client_secret_basic", "client_secret_post", "client_secret_jwt", "private_key_jwt", "none"]

### JWKS Response
- `paths./.well-known/jwks.json.get.responses.200.content.application/json.schema.properties.keys.items.properties.kty`
  - **Field**: Key type
  - **Recommendation**: Make this an ENUM
  - **Values**: ["RSA", "EC", "oct"]

- `paths./.well-known/jwks.json.get.responses.200.content.application/json.schema.properties.keys.items.properties.use`
  - **Field**: Key use
  - **Recommendation**: Make this an ENUM
  - **Values**: ["sig", "enc"]

- `paths./.well-known/jwks.json.get.responses.200.content.application/json.schema.properties.keys.items.properties.alg`
  - **Field**: Algorithm
  - **Recommendation**: Make this an ENUM (same as signing algorithms above)

- `paths./.well-known/jwks.json.get.responses.200.content.application/json.schema.properties.keys.items.properties.kid`
  - **Field**: Key ID
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9_-]{1,256}$`

### Token Introspection Response
- `paths./oauth2/introspect.post.responses.200.content.application/json.schema.properties.sub`
  - **Field**: Subject identifier
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$` (email format)

- `paths./oauth2/introspect.post.responses.200.content.application/json.schema.properties.email`
  - **Field**: User email
  - **Recommendation**: maxLength 255
  - **Pattern**: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

- `paths./oauth2/introspect.post.responses.200.content.application/json.schema.properties.name`
  - **Field**: User's full name
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$` (Unicode)

### UserInfo Response
- `paths./oauth2/userinfo.get.responses.200.content.application/json.schema.properties.sub`
  - **Field**: Subject identifier
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

- `paths./oauth2/userinfo.get.responses.200.content.application/json.schema.properties.name`
  - **Field**: User display name
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$` (Unicode)

### Groups Response
- `paths./oauth2/providers/{idp}/groups.get.responses.200.content.application/json.schema.properties.groups.items.properties.name`
  - **Field**: Group name
  - **Recommendation**: maxLength 255 (aligns with database VARCHAR(255))
  - **Pattern**: `^[\u0020-\uFFFF]{1,255}$`

- `paths./oauth2/providers/{idp}/groups.get.responses.200.content.application/json.schema.properties.groups.items.properties.display_name`
  - **Field**: Group display name
  - **Recommendation**: maxLength 255
  - **Pattern**: `^[\u0020-\uFFFF]{1,255}$` (Unicode)

### Providers Response
- `paths./oauth2/providers.get.responses.200.content.application/json.schema.properties.providers.items.properties.id`
  - **Field**: Provider identifier
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$`

- `paths./oauth2/providers.get.responses.200.content.application/json.schema.properties.providers.items.properties.name`
  - **Field**: Provider display name
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

- `paths./oauth2/providers.get.responses.200.content.application/json.schema.properties.providers.items.properties.provider`
  - **Field**: OAuth provider name
  - **Recommendation**: maxLength 50 (aligns with database VARCHAR(50))
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,49}$`

- `paths./oauth2/providers.get.responses.200.content.application/json.schema.properties.providers.items.properties.client_id`
  - **Field**: OAuth client ID
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9_.-]{1,256}$`

- `paths./oauth2/providers.get.responses.200.content.application/json.schema.properties.providers.items.properties.icon`
  - **Field**: Icon identifier
  - **Recommendation**: maxLength 60 (aligns with database VARCHAR(60))
  - **Pattern**: `^[a-z][a-z0-9_-]{0,59}$`

### Collaboration Session Responses
- `paths./diagrams/{diagram_id}/collaboration.post.responses.200.content.application/json.schema.properties.join_url`
  - **Field**: URL to join session
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^(https?|wss?)://[^\s]{1,1020}$`

- `paths./diagrams/{diagram_id}/collaboration.post.responses.409.content.application/json.schema.properties.error`
  - **Field**: Error message
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

### Token Response
- `paths./oauth2/callback.get.responses.200.content.application/json.schema.properties.idp`
  - **Field**: Identity provider ID
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$`

- `paths./oauth2/callback.get.responses.200.content.application/json.schema.properties.iss`
  - **Field**: Token issuer
  - **Recommendation**: maxLength 1000 (URL)
  - **Pattern**: `^[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\s]{1,1000}$`

### Generic Message Responses
- `*.responses.*.content.application/json.schema.properties.message`
  - **Field**: Generic message
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[\u0020-\uFFFF]{1,1024}$`

---

## 4. Schema Definitions (27 fields)

### Core Schemas
- `components.schemas.ApiInfo.properties.service.properties.name`
  - **Field**: Service name
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

- `components.schemas.ApiInfo.properties.api.properties.version`
  - **Field**: API version
  - **Recommendation**: maxLength 32
  - **Pattern**: `^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?(\+[a-zA-Z0-9.]+)?$` (semver)

### OAuth Metadata
- `components.schemas.OAuthProtectedResourceMetadata.properties.scopes_supported.items`
  - **Field**: OAuth scope tokens
  - **Recommendation**: maxLength 64 per item
  - **Pattern**: `^[\x21\x23-\x5B\x5D-\x7E]{3,64}$` (RFC 6749)

- `components.schemas.OAuthProtectedResourceMetadata.properties.bearer_methods_supported.items`
  - **Field**: Bearer token methods (RFC 9728)
  - **Recommendation**: Make this an ENUM
  - **Values**: ["header", "body", "query"]

- `components.schemas.OAuthProtectedResourceMetadata.properties.resource_name`
  - **Field**: Resource name
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

### Cell/Shape Identifiers
- `components.schemas.Cell.properties.shape`
  - **Field**: Shape type identifier (AntV/X6)
  - **Recommendation**: maxLength 64
  - **Pattern**: `^[a-z][a-z0-9-]{0,63}$` (kebab-case)

- `components.schemas.MarkupElement.properties.tagName`
  - **Field**: SVG/HTML tag name
  - **Recommendation**: maxLength 32
  - **Pattern**: `^[a-z][a-z0-9]{0,31}$` (HTML tag format)

- `components.schemas.MarkupElement.properties.selector`
  - **Field**: CSS selector
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[a-zA-Z0-9.#\[\]= :_-]{1,256}$`

### Error Schemas
- `components.schemas.Error.properties.error`
  - **Field**: Error code
  - **Recommendation**: maxLength 64
  - **Pattern**: `^[a-z][a-z0-9_]{0,63}$` (snake_case)

- `components.schemas.Error.properties.error_description`
  - **Field**: Error description
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[\u0020-\uFFFF]{1,1024}$`

- `components.schemas.Error.properties.details.properties.code`
  - **Field**: Machine-readable error code
  - **Recommendation**: maxLength 64
  - **Pattern**: `^[A-Z][A-Z0-9_]{0,63}$` (UPPER_SNAKE_CASE)

- `components.schemas.Error.properties.details.properties.suggestion`
  - **Field**: Error resolution suggestion
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[\u0020-\uFFFF]{1,1024}$`

### Token Schemas
- `components.schemas.AuthTokenResponse.properties.access_token`
  - **Field**: JWT access token
  - **Recommendation**: maxLength 4096
  - **Pattern**: `^[a-zA-Z0-9_.-]{1,4096}$`

- `components.schemas.AuthTokenResponse.properties.refresh_token`
  - **Field**: Refresh token
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[a-zA-Z0-9_.-]{1,1024}$`

### Threat Model Schemas
- `components.schemas.TMListItem.properties.owner`
  - **Field**: Owner email
  - **Recommendation**: maxLength 255
  - **Pattern**: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

- `components.schemas.TMListItem.properties.threat_model_framework`
  - **Field**: Threat model framework
  - **Recommendation**: Make this an ENUM
  - **Values**: ["STRIDE", "LINDDUN", "PASTA", "OCTAVE", "TRIKE", "VAST"]

- `components.schemas.ThreatModelBase.properties.threat_model_framework`
  - **Field**: Threat model framework
  - **Recommendation**: Make this an ENUM (same values as above)

- `components.schemas.ThreatModelInput.properties.threat_model_framework`
  - **Field**: Threat model framework
  - **Recommendation**: Make this an ENUM (same values as above)

### Webhook Schemas
- `components.schemas.WebhookDelivery.properties.event_type`
  - **Field**: Event type
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-z][a-z0-9._]{0,99}$` (dot notation)

- `components.schemas.WebhookDelivery.properties.last_error`
  - **Field**: Error message
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[\u0020-\uFFFF]{0,1024}$` (allow empty)

- `components.schemas.WebhookTestRequest.properties.event_type`
  - **Field**: Event type for test
  - **Recommendation**: maxLength 100
  - **Pattern**: `^[a-z][a-z0-9._]{0,99}$`

- `components.schemas.WebhookTestResponse.properties.status`
  - **Field**: Test status
  - **Recommendation**: Make this an ENUM
  - **Values**: ["success", "failure"]

- `components.schemas.WebhookTestResponse.properties.message`
  - **Field**: Result message
  - **Recommendation**: maxLength 1024
  - **Pattern**: `^[\u0020-\uFFFF]{1,1024}$`

### Deletion Challenge
- `components.schemas.DeletionChallenge.properties.challenge_text`
  - **Field**: Challenge string for deletion confirmation
  - **Recommendation**: maxLength 256
  - **Pattern**: `^[\u0020-\uFFFF]{1,256}$`

---

## Recommendations Summary

### Convert to Enums (10 fields)
1. `severity` filter → ["Unknown", "None", "Low", "Medium", "High", "Critical"]
2. `token_type_hint` → ["access_token", "refresh_token"]
3. `bearer_methods_supported[]` → ["header", "body", "query"]
4. `response_types_supported[]` → Standard OAuth response types
5. `subject_types_supported[]` → ["public", "pairwise"]
6. `id_token_signing_alg_values_supported[]` → Standard JWT algorithms
7. `grant_types_supported[]` → Standard OAuth grant types
8. `token_endpoint_auth_methods_supported[]` → Standard auth methods
9. `jwks.keys[].kty` → ["RSA", "EC", "oct"]
10. `jwks.keys[].use` → ["sig", "enc"]
11. `threat_model_framework` → ["STRIDE", "LINDDUN", "PASTA", "OCTAVE", "TRIKE", "VAST"]
12. `WebhookTestResponse.status` → ["success", "failure"]

### Add Patterns (64 remaining fields)
See detailed recommendations above for each field.

---

**Document Version**: 1.0
**Status**: Ready for User Review
