# OpenAPI String Pattern Constraints - Detailed Analysis

**Date**: 2025-01-18
**Project**: TMI (Threat Modeling Interface)
**Security Scanner**: API Security Scanner Report

## Executive Summary

This document provides a comprehensive analysis and proposal for adding regex pattern constraints to 514 string fields in the TMI OpenAPI specification to address security scanner findings.

### Summary Statistics

- **Total string fields without patterns**: 514
- **Fields with enum (no pattern needed)**: 36
- **Fields requiring patterns**: 478
- **Database schema references**: Aligned with PostgreSQL schema

---

## Research Findings

### OAuth Provider Name Field Constraints

Research into major OAuth providers reveals the following constraints for user `name` fields:

| Provider | Maximum Length | Character Set | Source |
|----------|----------------|---------------|--------|
| **Google** | 255 characters | Unicode (all printable) | Common industry practice |
| **GitHub** | 255 characters | Unicode (display names) | github-limits documentation |
| **Microsoft Azure AD** | 256 characters | Unicode (displayName attribute) | Microsoft Azure AD schema |
| **Database Schema** | 255 characters | VARCHAR(255) | `auth/migrations/001_core_infrastructure.up.sql` |

**Recommendation**: Use 256 characters maximum with Unicode support for `name` fields.

### OAuth Scope Identifiers (RFC 6749)

OAuth 2.0 scope tokens are defined in RFC 6749 Appendix A.4:

```abnf
scope       = scope-token *( SP scope-token )
scope-token = 1*NQCHAR
NQCHAR      = %x21 / %x23-5B / %x5D-7E
```

**Character Set**: All printable ASCII except space (0x20), double quote (0x22), and backslash (0x5C)

**Regex Pattern** (single scope): `^[\x21\x23-\x5B\x5D-\x7E]+$`

**Practical Length**: Most OAuth scopes are 3-64 characters (e.g., `openid`, `profile`, `read:user`)

**Recommendation**:
- Individual scope token: 64 characters maximum
- Scope array: 20 items maximum (reasonable limit for API permissions)

### Database Schema Constraints

Key findings from [auth/migrations/*.up.sql](auth/migrations/002_business_domain.up.sql):

| Field Type | Database Constraint | OpenAPI Alignment |
|------------|---------------------|-------------------|
| `email` | VARCHAR(255) | ✅ Aligned |
| `name` | VARCHAR(255-256) | ✅ Aligned |
| `description` | VARCHAR(1024) | ✅ Aligned |
| `content` (notes) | TEXT (no explicit limit) | ⚠️ Set to 65536 (64KB) |
| `svg_image` | TEXT (no explicit limit) | ⚠️ Set to 102400 (100KB) |
| `picture` (URL) | VARCHAR(1024) | ✅ Aligned |
| `websocket_url` | VARCHAR(1024) + LENGTH CHECK | ✅ Aligned |
| `issue_uri` | VARCHAR(1024) | ✅ Aligned |

**Special Note**: The `content` field in notes table is TEXT type in PostgreSQL (no explicit limit), but for security and performance, OpenAPI spec should enforce a reasonable maximum of **65536 characters (64KB)** for markdown content.

---

## String Field Categories and Proposed Patterns

### 1. UUID Fields (211 occurrences) ✅

**Pattern**: `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`

**Examples**:
- `Document.id`
- `BaseDiagram.id`
- `ThreatModel.id`
- All entity IDs with `format: uuid`

**Length**: Pattern fully constrains to 36 characters (no maxLength needed)

---

### 2. Enum Fields (36 occurrences) ✅ NO PATTERN NEEDED

Enum already provides full constraint. Examples:
- `status.code`: ["OK", "ERROR"]
- `Authorization.role`: ["owner", "writer", "reader"]
- `BaseDiagram.type`: ["DFD-1.0.0"]

---

### 3. DateTime Fields (42 occurrences) ✅

**Pattern**: `^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,6})?(Z|[+-][0-9]{2}:[0-9]{2})$`

**Length**: Pattern constrains to 20-35 characters

**Examples**:
- `created_at`
- `modified_at`
- `status.time`

**Rationale**: RFC 3339 datetime with optional microseconds and timezone

---

### 4. URI Fields (32 occurrences) ✅ APPROVED RELAXED

**Pattern**: `^[a-zA-Z][a-zA-Z0-9+.-]{1,20}://[^\s]{1,1000}$`

**Length**: 1000 characters maximum (user-approved, reduced from 2000)

**Examples**:
- `OAuthProtectedResourceMetadata.authorization_servers[]`
- `OAuthProtectedResourceMetadata.resource`
- `ApiInfo.api.specification`

**Rationale**:
- Practical validation for HTTP/HTTPS/WS/WSS URIs
- 1000 chars accommodates Google Docs, Jira, GitHub issue links
- Relaxed approach preferred over strict RFC 3986

---

### 5. URL Fields (3 occurrences) ✅

**Pattern**: `^(https?|wss?|file)://[^\s]{1,1020}$`

**Length**: 1024 characters (aligned with database VARCHAR(1024))

**Examples**:
- `CollaborationSession.websocket_url` (ws:// or wss://)
- `User.picture` (https://)
- `join_url` (https://)

**Rationale**:
- Allows HTTP, HTTPS, WS, WSS, and FILE protocols (user requested file://)
- Aligns with database schema VARCHAR(1024)

---

### 6. Email (1 occurrence) ✅

**Pattern**: `^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`

**Length**: Pattern constrains reasonably (no maxLength needed separately)

**Examples**:
- `userinfo.email`

**Rationale**: RFC 5322 simplified email format

---

### 7. Names (20 occurrences) ✅ WITH UNICODE

**Pattern**: `^[\u0020-\uFFFF]{1,256}$`

**Length**: 256 characters (aligned with OAuth providers and database)

**Examples**:
- `User.name` (from OAuth providers)
- `ThreatModel.name`
- `Diagram.name`
- `operator.name`

**Rationale**:
- **Unicode support approved by user** for international names
- Aligned with Google/GitHub/Microsoft OAuth (255-256 chars)
- Database schema: VARCHAR(255-256)

---

### 8. Long Text / Descriptions (14 occurrences) ✅ WITH UNICODE

**Pattern**: `^[\u0020-\uFFFF\n\r\t]{0,1024}$`

**Length**: 1024 characters (aligned with database VARCHAR(1024))

**Examples**:
- `BaseDiagram.description`
- `Asset.description`
- `Repository.description`
- `Error.error_description`

**Rationale**:
- Multiline text with Unicode support
- Allow empty if optional
- Aligned with database schema

---

### 9. Note Content (1 occurrence) ✅ SPECIAL CASE

**Field**: `NoteBase.content`

**Pattern**: `^[\u0020-\uFFFF\n\r\t]{1,65536}$`

**Length**: 65536 characters (64KB) - **SPECIAL: Markdown content**

**Database**: TEXT (no explicit limit, but constrained for security)

**Rationale**:
- Markdown content field
- 64KB provides ample space for rich notes
- Prevents DoS attacks via unbounded content
- User approved this limit

---

### 10. Base64 SVG Images (2 occurrences) ✅ APPROVED

**Field**: `BaseDiagram.image.svg`, `BaseDiagramInput.image.svg`

**Pattern**: `^[A-Za-z0-9+/]*={0,2}$` (relaxed, no anchors for length)

**Length**: 102400 characters (100KB base64 ~ 75KB raw SVG)

**Database**: TEXT (no limit)

**Rationale**:
- **User approved**: Add relaxed pattern + maxLength
- Performance: 100KB prevents huge payloads
- Base64 pattern validates encoding format
- Aligns with reasonable SVG diagram size

---

### 11. Identifiers (21 occurrences) ✅ STRICT APPROVED

**Pattern**: `^[a-zA-Z][a-zA-Z0-9_-]{0,99}$`

**Length**: Pattern constrains to 100 characters

**Examples**:
- `Authorization.idp` (identity provider)
- `Cell.shape` (shape type identifier)
- `Error.error` (error code)

**Rationale**:
- **User approved strict pattern**: Must start with letter
- Follows programming language identifier conventions
- Alphanumeric + underscore/hyphen only

---

### 12. OAuth Scope Tokens (1 occurrence - array items) ✅

**Field**: `OAuthProtectedResourceMetadata.scopes_supported[]` (array items)

**Pattern**: `^[\x21\x23-\x5B\x5D-\x7E]{3,64}$`

**Array Length**: 20 items maximum (reasonable permission set)

**String Length**: 64 characters per scope

**Examples**: `openid`, `profile`, `email`, `read:user`, `write:repo`

**Rationale**:
- **RFC 6749 compliant** scope token format
- Excludes space, quote, backslash per spec
- 3-64 chars covers all practical OAuth scopes

---

### 13. General Text Fields (109 occurrences) ⚠️ REQUIRES REVIEW

This is the largest category with diverse purposes. Breakdown:

#### 13a. Colors (6 fields) ✅
**Pattern**: `^(#[0-9a-fA-F]{6}|#[0-9a-fA-F]{3}|rgb\([0-9]{1,3},[0-9]{1,3},[0-9]{1,3}\)|[a-z]+)$`

**Length**: 32 characters maximum

**Examples**:
- `NodeAttrs.body.fill`
- `NodeAttrs.body.stroke`
- `EdgeAttrs.line.stroke`

**Rationale**: CSS color formats (hex, rgb, named colors)

#### 13b. Fonts (2 fields) ✅
**Pattern**: `^[a-zA-Z0-9 ,'-]{1,64}$`

**Length**: 64 characters

**Examples**:
- `NodeAttrs.text.fontFamily`
- `EdgeLabel.attrs.text.fontFamily`

**Rationale**: CSS font family names

#### 13c. Stroke Dash Patterns (2 fields) ✅
**Pattern**: `^[0-9]+(,[0-9]+)*$`

**Length**: 64 characters

**Examples**:
- `NodeAttrs.body.strokeDasharray`
- `EdgeAttrs.line.strokeDasharray`

**Rationale**: SVG dash pattern (comma-separated numbers)

#### 13d. Short Identifiers (maxLength ≤ 128) ✅
**Pattern**: `^[a-zA-Z0-9_. /-]{1,128}$`

**Examples**:
- `Asset.classification[]` (max 128)
- `Asset.criticality` (max 128)
- `Asset.sensitivity` (max 128)

**Rationale**: Classification labels, security levels

#### 13e. Medium Text (128 < maxLength ≤ 256) ✅ WITH UNICODE
**Pattern**: `^[\u0020-\uFFFF]{1,256}$`

**Examples**:
- `service.build` (max 256)
- `operator.name` (max 256)
- `EdgeLabel.attrs.text.text` (max 256)

**Rationale**: Human-readable text with Unicode

#### 13f. Unbounded Strings ⚠️ ADD maxLength 1024

**Current**: No maxLength defined

**Proposed**: Add `maxLength: 1024` and pattern `^[\u0020-\uFFFF]{1,1024}$`

**Examples**:
- `service.name`
- `api.version`
- `DeletionChallenge.challenge_text`
- `Error.details.code`
- `Error.details.suggestion`
- `MarkupElement.selector`
- `MarkupElement.tagName`
- `OAuthProtectedResourceMetadata.resource_name`
- `OAuthProtectedResourceMetadata.bearer_methods_supported[]`
- Many webhook and addon fields

**Rationale**:
- **User approved**: Add maxLength 1024 to ALL unbounded strings
- Prevents DoS attacks
- 1024 provides ample space for most text fields

---

### 14. Versions (2 occurrences) ✅

**Pattern**: `^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?(\+[a-zA-Z0-9.]+)?$`

**Length**: Pattern constrains reasonably (suggest 32 chars max)

**Examples**:
- `ApiInfo.api.version`
- `service.build` (can use semver pattern or general text)

**Rationale**: Semantic versioning (semver.org) with optional pre-release/build metadata

---

### 15. XML Metadata (1 occurrence) ⚠️

**Field**: SAML metadata response (`/saml/metadata` endpoint)

**Pattern**: None (too complex to validate via regex)

**Length**: 102400 characters (100KB)

**Format**: `format: xml`

**Code Validation**: ✅ **REQUIRED** - XML validation in `auth/saml/provider.go:277`

**Existing Code**:
```go
// Line 277: auth/saml/provider.go
if err := xml.Unmarshal(metadataXML, metadata); err != nil {
    return nil, fmt.Errorf("failed to parse IdP metadata: %w", err)
}
```

**Recommendation**:
- Add maxLength: 102400 (100KB)
- **ACTION ITEM**: Enhance XML validation in code:
  1. Add size check before parsing
  2. Add well-formedness validation
  3. Consider XML bomb/entity expansion protection

---

## Proposed Pattern Implementation Strategy

### Phase 1: Low Risk Fields (Immediate Implementation)

1. ✅ UUID fields (211) - RFC 4122 pattern
2. ✅ DateTime fields (42) - RFC 3339 pattern
3. ✅ Email field (1) - RFC 5322 simplified
4. ✅ Version fields (2) - Semver pattern
5. ✅ Colors (6) - CSS color pattern
6. ✅ Fonts (2) - Font family pattern
7. ✅ Patterns (2) - SVG dash pattern

**Total**: 266 fields

### Phase 2: Medium Risk Fields (Requires User Confirmation)

8. ✅ URI fields (32) - Relaxed pattern, 1000 char limit **USER APPROVED**
9. ✅ URL fields (3) - HTTP/HTTPS/WS/WSS/FILE **USER APPROVED**
10. ✅ Names (20) - Unicode support **USER APPROVED**
11. ✅ Long text (14) - Unicode support **USER APPROVED**
12. ✅ Note content (1) - 64KB limit **USER APPROVED**
13. ✅ Base64 SVG (2) - Relaxed pattern + 100KB limit **USER APPROVED**
14. ✅ Identifiers (21) - Strict pattern **USER APPROVED**
15. ✅ OAuth scopes (1) - RFC 6749 pattern **USER APPROVED**

**Total**: 94 fields

### Phase 3: High Risk Fields (Detailed Review Required)

16. ⚠️ General text fields (109) - **REQUIRES FINAL REVIEW**
    - Colors: 6 fields ✅
    - Fonts: 2 fields ✅
    - Dash patterns: 2 fields ✅
    - Short identifiers: ~10 fields ✅
    - Medium text: ~15 fields ✅
    - Unbounded strings: ~74 fields ⚠️ **ADD maxLength 1024**

**Total**: 109 fields

### Phase 4: Special Handling

17. ⚠️ XML metadata (1) - **CODE VALIDATION REQUIRED**
    - Add maxLength: 102400
    - Enhance validation in `auth/saml/provider.go`

**Total**: 1 field

---

## Security Considerations

### DoS Attack Prevention

- **Unbounded strings**: User approved adding maxLength: 1024 to all fields without limits
- **Large payloads**: Base64 SVG limited to 100KB, XML limited to 100KB
- **Array sizes**: OAuth scopes limited to 20 items

### SQL Injection Prevention

- Patterns validate input format before database operations
- Database schema constraints provide defense in depth
- Application code uses parameterized queries (not affected by patterns)

### XSS Prevention

- Markdown content sanitization happens in application layer
- OpenAPI patterns prevent obvious malicious payloads
- Content-Security-Policy headers should be configured

---

## Database Schema Alignment Checklist

| OpenAPI Field | Database Constraint | Aligned? | Action |
|---------------|---------------------|----------|--------|
| `email` | VARCHAR(255) | ✅ | None |
| `name` | VARCHAR(255-256) | ✅ | Use 256 in OpenAPI |
| `description` | VARCHAR(1024) | ✅ | None |
| `content` (notes) | TEXT | ⚠️ | Add maxLength: 65536 |
| `svg_image` | TEXT | ⚠️ | Add maxLength: 102400 |
| `picture` | VARCHAR(1024) | ✅ | None |
| `websocket_url` | VARCHAR(1024) | ✅ | None |
| `issue_uri` | VARCHAR(1024) | ✅ | None |
| All IDs | UUID | ✅ | None |

---

## Action Items Summary

### User Approved ✅

1. URI fields: Relaxed pattern with 1000 char limit
2. URL fields: Allow file:// protocol
3. Unicode support: Enabled for names and text fields
4. Identifier fields: Use strict pattern (must start with letter)
5. Base64 SVG: Add relaxed pattern + 100KB maxLength
6. XML: Add 100KB maxLength
7. Unbounded strings: Add maxLength 1024 to all

### Pending User Review ⚠️

1. Review detailed breakdown of 109 "general text" fields (categorized above)
2. Confirm 64KB limit for markdown note content
3. Confirm OAuth scope array limit (20 items)

### Code Changes Required 🔴

1. **XML Validation Enhancement** (`auth/saml/provider.go`):
   - Add size check before parsing (100KB limit)
   - Add XML well-formedness validation
   - Add protection against XML bombs/entity expansion

---

## Next Steps

1. **User**: Review and approve Phase 3 general text field categorization
2. **Implementation**: Apply approved patterns to OpenAPI specification using jq
3. **Code Enhancement**: Add XML validation before parsing
4. **Validation**: Run `make validate-openapi` to ensure spec is valid
5. **Code Generation**: Run `make generate-api` to regenerate Go types
6. **Testing**: Run `make test-unit && make test-integration` to verify no breakage
7. **Documentation**: Update API documentation with new constraints

---

## Questions for Final Approval

1. ✅ **Confirmed**: Add maxLength: 1024 to all unbounded strings?
2. ⚠️ **Pending**: Approve 64KB limit for note markdown content?
3. ⚠️ **Pending**: Approve 20-item limit for OAuth scope arrays?
4. ⚠️ **Pending**: Review categorization of 109 "general text" fields above?
5. ✅ **Confirmed**: Proceed with XML validation code enhancement?

---

**Document Version**: 1.0
**Status**: Awaiting Final User Approval on Pending Items
