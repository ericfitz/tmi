# Research Findings Summary

**Date**: 2025-01-18
**Project**: TMI OpenAPI String Pattern Constraints

## Questions Answered

### 1. Note Markdown Content API Limit

**Research**: GitHub Issue Comments and PR Bodies

**Finding**: GitHub uses **65,536 characters (64KB)** as the maximum length for issue comments and PR descriptions.

**Recommendation**: **65,536 characters (64KB)**

**Rationale**:
- Industry-standard limit for markdown content in collaboration tools
- GitHub's limit: 65,536 4-byte unicode characters (262,144 bytes)
- Sufficient for comprehensive threat modeling notes
- Aligns with common database TEXT field limits
- Your PostgreSQL TEXT field can handle this easily

**Pattern**: `^[\u0020-\uFFFF\n\r\t]{1,65536}$`

---

### 2. OAuth Scope Array Limit

**User Decision**: **20 items maximum** ✅ Approved

**Per-Scope Pattern**: `^[\x21\x23-\x5B\x5D-\x7E]{3,64}$` (RFC 6749)

**Rationale**:
- RFC 6749 defines valid characters for OAuth scope tokens
- Excludes space (0x20), double quote (0x22), backslash (0x5C)
- 3-64 characters covers all practical OAuth scopes
- 20-item array limit prevents excessive permission requests

---

### 3. AntV/X6 Dash Patterns

**Research**: AntV X6 Documentation

**Finding**: X6 uses standard SVG `strokeDasharray` format

**Format**: Comma-separated numbers (e.g., `"10,2"`, `"5"`, `"10,5,2,5"`)

**Recommended Pattern**: `^[0-9]+(\.[0-9]+)?(,[0-9]+(\.[0-9]+)?)*$`

**Rationale**:
- Supports integer values: `"5"` or `"10,2"`
- Supports decimal values: `"5.5,2.5"`
- Supports complex patterns: `"10,5,2,5"` (dash, gap, dash, gap)
- Standard SVG specification compliance
- maxLength: 64 characters (aligns with current spec)

---

### 4. HTTP Auth Methods (Bearer Methods)

**Research**: RFC 9728 - OAuth 2.0 Protected Resource Metadata

**Finding**: **Yes, there is a standard!**

**Standard Values** (RFC 9728):
- `"header"` - Authorization header field (MUST support)
- `"body"` - Form-encoded body parameter (MAY support)
- `"query"` - URI query parameter (MAY support, not recommended)

**Recommendation**: **Make it an ENUM** ✅

**OpenAPI Definition**:
```json
{
  "bearer_methods_supported": {
    "type": "array",
    "items": {
      "type": "string",
      "enum": ["header", "body", "query"]
    },
    "description": "OAuth 2.0 bearer token methods supported (RFC 9728)"
  }
}
```

**Reference**: RFC 6750 defines the three methods in Sections 2.1, 2.2, and 2.3

---

## XML Validation Library Research

### Go XML Validation Options

#### Option 1: Native Go `encoding/xml` (Current Approach)

**Pros**:
- Built-in, no external dependencies
- Simple API: `xml.Unmarshal()`
- Good performance

**Cons**:
- No XSD validation support
- No built-in protection against XML bombs
- Limited entity expansion controls

**Security Enhancement Approach**:
```go
import "encoding/xml"

// Create decoder with security settings
decoder := xml.NewDecoder(reader)
decoder.Strict = true  // Enable strict parsing
// Note: Go doesn't have DisallowDTD, but entities are disabled by default
```

**Recommended Security Additions**:
1. Size check before parsing (enforce 100KB limit)
2. Well-formedness validation (basic XML structure)
3. No external entity processing (default in Go)

#### Option 2: `github.com/lestrrat-go/libxml2` (XSD Validation)

**Repository**: https://github.com/lestrrat-go/libxml2

**Pros**:
- XSD schema validation support
- DOM and XPath support
- Binds to mature libxml2 C library
- Well-established security model (libxml2 is hardened)

**Cons**:
- Requires CGO (C bindings)
- Alpha-grade API (may change)
- Deployment complexity (needs libxml2 installed)
- Cross-platform build challenges

**Usage Example**:
```go
import "github.com/lestrrat-go/libxml2/xsd"

schema, err := xsd.Parse(schemaFile)
if err != nil {
    // Handle schema parsing error
}

err = schema.Validate(xmlDoc)
if err != nil {
    // Validation failed
}
```

#### Option 3: `github.com/GoComply/xsd2go` (Code Generation)

**Repository**: https://github.com/GoComply/xsd2go

**Approach**: Generate Go structs from XSD schemas at build time

**Pros**:
- Type-safe XML parsing
- No runtime XSD validation overhead
- Pure Go (no CGO)

**Cons**:
- Requires XSD schema at build time
- Generated code can be large
- No runtime schema validation

### Recommended Approach for TMI

**For SAML Metadata Validation**:

#### Recommended: Enhanced Native Go Approach

**Rationale**:
1. SAML library (`github.com/crewjam/saml`) already validates structure
2. Adding CGO dependency complicates deployment
3. SAML metadata structure is well-defined
4. Security can be achieved with proper size limits and entity controls

**Implementation** ([auth/saml/provider.go:258-282](auth/saml/provider.go#L258-L282)):

```go
// fetchIDPMetadata fetches and parses IdP metadata
func fetchIDPMetadata(config *SAMLConfig) (*saml.EntityDescriptor, error) {
    var metadataXML []byte
    var err error

    if config.IDPMetadataXML != "" {
        metadataXML = []byte(config.IDPMetadataXML)
    } else if config.IDPMetadataURL != "" {
        metadataXML, err = fetchMetadataFromURL(config.IDPMetadataURL)
        if err != nil {
            return nil, fmt.Errorf("failed to fetch metadata from URL: %w", err)
        }
    } else {
        return nil, fmt.Errorf("no IdP metadata configured")
    }

    // SECURITY: Validate size before parsing (prevent XML bombs)
    const maxMetadataSize = 102400 // 100KB
    if len(metadataXML) > maxMetadataSize {
        return nil, fmt.Errorf("metadata exceeds maximum size of %d bytes", maxMetadataSize)
    }

    // SECURITY: Validate well-formedness with strict decoder
    decoder := xml.NewDecoder(bytes.NewReader(metadataXML))
    decoder.Strict = true
    decoder.CharsetReader = nil // Disable charset conversion to prevent attacks

    // Parse metadata with security settings
    metadata := &saml.EntityDescriptor{}
    if err := decoder.Decode(metadata); err != nil {
        return nil, fmt.Errorf("failed to parse IdP metadata: %w", err)
    }

    // SECURITY: Validate expected structure
    if metadata.EntityID == "" {
        return nil, fmt.Errorf("invalid metadata: missing EntityID")
    }

    return metadata, nil
}
```

**Security Enhancements**:
1. ✅ Size check (100KB limit) - prevents large payload DoS
2. ✅ Strict parsing mode - rejects malformed XML
3. ✅ Charset reader disabled - prevents encoding attacks
4. ✅ Structure validation - ensures required fields present
5. ✅ Go's `encoding/xml` disables external entities by default (XXE protection)

**Additional Consideration**:

If TMI eventually needs XSD validation for SAML compliance, evaluate `github.com/lestrrat-go/libxml2` at that time. For current requirements, enhanced native parsing is sufficient and maintains deployment simplicity.

---

## Additional Enum Opportunities

Based on unbounded field analysis, the following fields should be converted to enums:

### OAuth/OIDC Standards (RFC-Defined)
1. **response_types_supported** (OIDC Discovery)
   - Values: `["code", "token", "id_token", "code token", "code id_token", "token id_token", "code token id_token"]`

2. **subject_types_supported** (OIDC Discovery)
   - Values: `["public", "pairwise"]`

3. **id_token_signing_alg_values_supported** (OIDC Discovery)
   - Values: `["RS256", "RS384", "RS512", "ES256", "ES384", "ES512", "PS256", "PS384", "PS512", "none"]`

4. **grant_types_supported** (OIDC Discovery)
   - Values: `["authorization_code", "implicit", "refresh_token", "client_credentials", "password", "urn:ietf:params:oauth:grant-type:jwt-bearer"]`

5. **token_endpoint_auth_methods_supported** (OIDC Discovery)
   - Values: `["client_secret_basic", "client_secret_post", "client_secret_jwt", "private_key_jwt", "none"]`

6. **bearer_methods_supported** (RFC 9728)
   - Values: `["header", "body", "query"]`

7. **token_type_hint** (Token Introspection)
   - Values: `["access_token", "refresh_token"]`

### JWKS Standards
8. **jwks.keys[].kty** (Key Type)
   - Values: `["RSA", "EC", "oct"]`

9. **jwks.keys[].use** (Key Use)
   - Values: `["sig", "enc"]`

### Application-Specific
10. **severity** (Threat Query Filter)
    - Values: `["Unknown", "None", "Low", "Medium", "High", "Critical"]`
    - Note: Already defined in database schema

11. **threat_model_framework** (Threat Model Schema)
    - Values: `["STRIDE", "LINDDUN", "PASTA", "OCTAVE", "TRIKE", "VAST"]`
    - Note: Database has VARCHAR(50) with default 'STRIDE'

12. **webhook_test_response.status**
    - Values: `["success", "failure"]`

---

## Pattern Summary

### Approved Patterns

| Category | Pattern | Max Length | Unicode |
|----------|---------|------------|---------|
| UUID | `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$` | 36 | No |
| DateTime | `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z\|[+-][0-9]{2}:[0-9]{2})$` | 35 | No |
| URI | `^[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\s]{1,1000}$` | 1000 | No |
| URL | `^(https?\|wss?\|file)://[^\s]{1,1020}$` | 1024 | No |
| Email | `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$` | 255 | No |
| Name | `^[\u0020-\uFFFF]{1,256}$` | 256 | Yes |
| Description | `^[\u0020-\uFFFF\n\r\t]{0,1024}$` | 1024 | Yes |
| Note Content | `^[\u0020-\uFFFF\n\r\t]{1,65536}$` | 65536 | Yes |
| Base64 SVG | `^[A-Za-z0-9+/]*={0,2}$` | 102400 | No |
| Identifier (strict) | `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$` | 100 | No |
| OAuth Scope | `^[\x21\x23-\x5B\x5D-\x7E]{3,64}$` | 64 | No |
| Version | `^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?(\+[a-zA-Z0-9.]+)?$` | 32 | No |
| Color | `^(#[0-9a-fA-F]{6}\|#[0-9a-fA-F]{3}\|rgb\([0-9]{1,3},[0-9]{1,3},[0-9]{1,3}\)\|[a-z]+)$` | 32 | No |
| Font Family | `^[a-zA-Z0-9 ,'-]{1,64}$` | 64 | No |
| Dash Pattern | `^[0-9]+(\.[0-9]+)?(,[0-9]+(\.[0-9]+)?)*$` | 64 | No |
| JSON Path | `^(/[^/]+)*$` | 256 | No |
| JWT Token | `^[a-zA-Z0-9_.-]{1,4096}$` | 4096 | No |

---

## Next Steps

1. **User Review**: Approve final recommendations
2. **Implementation**: Apply patterns using jq to OpenAPI spec
3. **XML Security**: Enhance SAML metadata parsing with security checks
4. **Validation**: Run `make validate-openapi`
5. **Code Generation**: Run `make generate-api`
6. **Testing**: Run `make test-unit && make test-integration`

---

**Document Version**: 1.0
**Status**: Ready for Implementation
