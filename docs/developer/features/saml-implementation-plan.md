# SAML Enterprise SSO Implementation Plan

## Overview

This document outlines the implementation of SAML 2.0 authentication support for TMI, enabling enterprise Single Sign-On (SSO) with group-based authorization. The implementation introduces first-class identity provider (IdP) fields throughout the system to ensure clean provider isolation and prevent cross-provider authorization leakage.

## Key Features

1. **SAML 2.0 Authentication**: Full support for enterprise SAML identity providers (Okta, Azure AD, etc.)
2. **Group-Based Authorization**: Support for both user and group-based access control
3. **Provider Isolation**: Groups are scoped to specific IdPs to prevent cross-provider access
4. **Session-Based Group Caching**: Groups cached in Redis for session duration only
5. **First-Class IdP Fields**: Explicit IdP tracking in users, authorizations, and sessions

## Design Principles

- **IdP as Source of Truth**: Group memberships always come from the identity provider
- **No User-Group Persistence**: User-group relationships are never stored in PostgreSQL
- **Clean Schema Design**: Modify existing migrations (database rebuild required)
- **Explicit Semantics**: Use `subject_type` and `idp` fields instead of encoded strings
- **Provider Isolation**: Groups from different IdPs cannot grant access across providers

## API Changes

### User Information Endpoints

#### GET /me (Updated)
Returns current user information including groups and IdP:
```json
{
  "id": "uuid",
  "email": "alice@example.com",
  "name": "Alice Smith",
  "picture": "...",
  "idp": "saml_okta",
  "groups": ["security-team", "developers"],
  "last_login": "2024-01-15T10:30:00Z"
}
```

#### GET /oauth2/userinfo (New)
OAuth2/OIDC compliant endpoint:
```json
{
  "sub": "alice@example.com",
  "email": "alice@example.com",
  "name": "Alice Smith",
  "idp": "saml_okta",
  "groups": ["security-team", "developers"]
}
```

### Group Discovery

#### GET /oauth2/providers/{idp}/groups (New)
Lists groups from a specific provider for UI autocomplete:
```json
{
  "idp": "saml_okta",
  "groups": [
    {
      "name": "security-team",
      "display_name": "Security Team",
      "used_in_authorizations": true
    },
    {
      "name": "developers",
      "display_name": "Development Team",
      "used_in_authorizations": false
    }
  ]
}
```

## Database Schema Changes

### Users Table
```sql
ALTER TABLE users
ADD COLUMN identity_provider VARCHAR(100),
ADD COLUMN last_login TIMESTAMPTZ;
```

### Threat Model Access Table
```sql
CREATE TABLE threat_model_access (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    subject VARCHAR(500) NOT NULL,
    subject_type VARCHAR(20) NOT NULL CHECK (subject_type IN ('user', 'group')),
    idp VARCHAR(100),
    role VARCHAR(50) NOT NULL CHECK (role IN ('owner', 'writer', 'reader')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    granted_by VARCHAR(255),
    UNIQUE(threat_model_id, subject, subject_type, idp)
);
```

### Authorization Groups Table
```sql
CREATE TABLE authorization_groups (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    idp VARCHAR(100) NOT NULL,
    group_name VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    first_used TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_used TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    usage_count INTEGER DEFAULT 1,
    UNIQUE(idp, group_name)
);
```

## Data Model

### Authorization Object
```json
{
  "subject": "security-team",
  "subject_type": "group",
  "idp": "saml_okta",
  "role": "writer"
}
```

### JWT Claims
```go
type Claims struct {
    Email         string   `json:"email"`
    EmailVerified bool     `json:"email_verified"`
    Name          string   `json:"name"`
    IdP           string   `json:"idp"`
    Groups        []string `json:"groups,omitempty"`
    // ... other standard claims
}
```

### Redis Session Cache
```json
{
  "email": "alice@example.com",
  "idp": "saml_okta",
  "groups": ["security-team", "developers"],
  "cached_at": 1705315800
}
```

## Authorization Logic

Authorization checks must validate both group membership AND IdP match:

```go
// For user authorization
if auth.SubjectType == "user" && auth.Subject == userEmail {
    // Grant access based on role
}

// For group authorization
if auth.SubjectType == "group" && auth.IdP == userIdP {
    for _, userGroup := range userGroups {
        if auth.Subject == userGroup {
            // Grant access based on role
        }
    }
}
```

## SAML Provider Implementation

### Directory Structure
```
auth/
├── saml/
│   ├── provider.go        # SAMLProvider implementing Provider interface
│   ├── config.go          # SAML-specific configuration
│   ├── metadata.go        # SP metadata generation and IdP parsing
│   ├── attributes.go      # Attribute extraction and mapping
│   └── handlers.go        # SAML HTTP endpoints (ACS, metadata, SLO)
```

### Key Components

1. **SAMLProvider**: Implements the existing Provider interface
2. **Group Extraction**: Maps SAML assertions to groups
3. **Metadata Support**: Generates SP metadata, consumes IdP metadata
4. **Session Management**: Caches groups in Redis with session TTL
5. **JWT Integration**: Includes groups in JWT claims

## Configuration

### SAML Provider Configuration
```yaml
oauth:
  providers:
    saml_okta:
      id: "saml_okta"
      name: "Okta SSO"
      type: "saml"
      icon: "fa-solid fa-key"
      enabled: true
      saml:
        entity_id: "https://tmi.example.com"
        acs_url: "https://tmi.example.com/saml/acs"
        slo_url: "https://tmi.example.com/saml/slo"
        idp_metadata_url: "https://okta.example.com/app/metadata"
        sp_private_key_path: "/path/to/sp.key"
        sp_certificate_path: "/path/to/sp.crt"
        attribute_mapping:
          email: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"
          name: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/name"
          groups: "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups"
```

## Implementation Phases

### Phase 1: Foundation
1. Create feature branch
2. Document implementation plan
3. Add SAML dependencies (crewjam/saml)

### Phase 2: Database & API
1. Modify database migrations
2. Update OpenAPI specification
3. Regenerate API code

### Phase 3: SAML Provider
1. Implement SAML provider structure
2. Add group extraction logic
3. Create SAML endpoints

### Phase 4: User & Groups
1. Update /me endpoint
2. Add /oauth2/userinfo endpoint
3. Implement provider-specific group endpoint

### Phase 5: Authorization
1. Update authorization logic for groups
2. Modify middleware for group support
3. Add Redis caching

### Phase 6: Testing
1. Multi-provider isolation tests
2. Group authorization scenarios
3. Session management tests

## Security Considerations

1. **SAML Signature Validation**: All SAML responses must be cryptographically validated
2. **Replay Attack Prevention**: Track processed assertion IDs in Redis
3. **Session Management**: Groups cleared on logout, refreshed on login
4. **Provider Isolation**: Groups from one IdP cannot grant access for another
5. **Certificate Management**: Regular rotation of SP certificates

## Example Authorization Scenario

```json
{
  "threat_model": {
    "name": "Payment System",
    "owner": "alice@example.com",
    "authorization": [
      {
        "subject": "bob@example.com",
        "subject_type": "user",
        "idp": null,
        "role": "writer"
      },
      {
        "subject": "security-team",
        "subject_type": "group",
        "idp": "saml_okta",
        "role": "writer"
      },
      {
        "subject": "security-team",
        "subject_type": "group",
        "idp": "saml_azure",
        "role": "reader"
      }
    ]
  }
}
```

Access Results:
- Alice (owner): Full access
- Bob: Writer access (regardless of IdP)
- Okta "security-team" members: Writer access
- Azure "security-team" members: Reader access (different provider)

## Testing Strategy

1. **Unit Tests**
   - SAML attribute extraction
   - Group authorization logic
   - Provider isolation

2. **Integration Tests**
   - Full SAML authentication flow
   - Multi-provider scenarios
   - Session management

3. **Edge Cases**
   - User with no groups
   - Cross-provider authorization attempts
   - Session expiration

## Success Metrics

- SAML authentication works with major IdPs
- Groups properly scoped to providers
- No cross-provider authorization leakage
- Clean API design maintained
- Performance acceptable with group checks
- Comprehensive test coverage

## References

- [SAML 2.0 Specification](http://docs.oasis-open.org/security/saml/v2.0/)
- [crewjam/saml Documentation](https://github.com/crewjam/saml)
- [TMI OAuth Implementation](../setup/oauth-integration.md)