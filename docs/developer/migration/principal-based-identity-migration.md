# Principal-Based Identity Migration Guide

**Date**: 2025-01-23
**Impact**: Breaking Changes
**Status**: In Progress

## Overview

TMI is migrating from internal UUID-based identity representation to a portable, Principal-based architecture. This change ensures that user and group identities are globally unique and portable across TMI instances, using the combination of `provider` + `provider_id` as the identity key instead of internal UUIDs.

## What Changed

### 1. New Schema Types

#### Principal (Base Type)
```json
{
  "principal_type": "user" | "group",
  "provider": "google",
  "provider_id": "alice@example.com",
  "display_name": "Alice Anderson",
  "email": "alice@example.com"
}
```

#### User (Extends Principal)
```json
{
  "principal_type": "user",
  "provider": "google",
  "provider_id": "alice@example.com",
  "display_name": "Alice Anderson",
  "email": "alice@example.com"
}
```

**Required fields**: `principal_type`, `provider`, `provider_id`, `display_name`, `email`

#### Group (Extends Principal)
```json
{
  "principal_type": "group",
  "provider": "*",
  "provider_id": "security-team",
  "display_name": "Security Team",
  "description": "Corporate security team members",
  "email": "security-team@example.com"
}
```

**Required fields**: `principal_type`, `provider`, `provider_id`, `display_name`
**Optional fields**: `email`, `description`, `first_used`, `last_used`, `usage_count` (read-only)

### 2. Authorization Schema Changes

**Before** (Old Format):
```json
{
  "subject": "550e8400-e29b-41d4-a716-446655440000",
  "subject_type": "user",
  "idp": "google",
  "role": "writer"
}
```

**After** (New Format):
```json
{
  "principal_type": "user",
  "provider": "google",
  "provider_id": "alice@example.com",
  "display_name": "Alice Anderson",
  "email": "alice@example.com",
  "role": "writer"
}
```

**Changes**:
- ❌ Removed: `subject` (was internal UUID)
- ❌ Removed: `subject_type`
- ❌ Removed: `idp`
- ✅ Added: `principal_type` (replaces `subject_type`)
- ✅ Added: `provider` (replaces `idp`, now required)
- ✅ Added: `provider_id` (globally unique identifier)
- ✅ Added: `display_name` (for UI rendering)
- ✅ Added: `email` (for users, optional for groups)
- ✅ Kept: `role` (unchanged)

### 3. ThreatModel Schema Changes

**Before**:
```json
{
  "id": "F898D276-DE86-4837-9B5E-C46E1A08EAB6",
  "owner": "user@example.com",
  "created_by": "user@example.com",
  "authorization": [
    {
      "subject": "uuid-here",
      "subject_type": "user",
      "role": "owner"
    }
  ]
}
```

**After**:
```json
{
  "id": "F898D276-DE86-4837-9B5E-C46E1A08EAB6",
  "owner": {
    "principal_type": "user",
    "provider": "google",
    "provider_id": "user@example.com",
    "display_name": "User Example",
    "email": "user@example.com"
  },
  "created_by": {
    "principal_type": "user",
    "provider": "google",
    "provider_id": "user@example.com",
    "display_name": "User Example",
    "email": "user@example.com"
  },
  "authorization": [
    {
      "principal_type": "user",
      "provider": "google",
      "provider_id": "user@example.com",
      "display_name": "User Example",
      "email": "user@example.com",
      "role": "owner"
    }
  ]
}
```

**Changes**:
- `owner`: String → User object
- `created_by`: String → User object
- `authorization`: Updated to use Principal-based format

### 4. JWT Token Changes

**Removed Claims**:
- `given_name` - No longer included
- `family_name` - No longer included
- `picture` - No longer included
- `locale` - No longer included

**Remaining Claims**:
- `email` - Still included
- `email_verified` - Still included
- `name` - Still included (display name)
- `idp` - Still included (identity provider)
- `groups` - Still included (user's groups)
- Standard claims (`sub`, `iss`, `exp`, etc.)

## Migration Steps for Clients

### Step 1: Update Authorization Handling

**Old Code**:
```typescript
interface Authorization {
  subject: string;  // UUID
  subject_type: "user" | "group";
  idp?: string;
  role: "reader" | "writer" | "owner";
}

// Display code
function displayAuthorization(auth: Authorization) {
  // Had to look up display name separately or just show UUID
  return `${auth.subject} (${auth.role})`;
}
```

**New Code**:
```typescript
interface Authorization {
  principal_type: "user" | "group";
  provider: string;
  provider_id: string;
  display_name?: string;
  email?: string;
  role: "reader" | "writer" | "owner";
}

// Display code - now has all needed information
function displayAuthorization(auth: Authorization) {
  const name = auth.display_name || auth.provider_id;
  const typeLabel = auth.principal_type === "user" ? "User" : "Group";
  return `${name} (${typeLabel}, ${auth.role})`;
}
```

### Step 2: Update Owner/Created By Display

**Old Code**:
```typescript
interface ThreatModel {
  id: string;
  owner: string;  // Just email
  created_by: string;  // Just email
  // ...
}

// Display
<div>Owner: {threatModel.owner}</div>
```

**New Code**:
```typescript
interface User {
  principal_type: "user";
  provider: string;
  provider_id: string;
  display_name: string;
  email: string;
}

interface ThreatModel {
  id: string;
  owner: User;
  created_by: User;
  // ...
}

// Display
<div>Owner: {threatModel.owner.display_name} ({threatModel.owner.email})</div>
```

### Step 3: Update Authorization Creation

**Old Code**:
```typescript
// Creating authorization entry
const newAuth = {
  subject: "user@example.com",  // Could be email or UUID
  subject_type: "user",
  idp: "google",
  role: "writer"
};

POST /threat_models/{id}/authorization
Body: newAuth
```

**New Code**:
```typescript
// Option 1: Provide full Principal information
const newAuth = {
  principal_type: "user",
  provider: "google",
  provider_id: "user@example.com",
  display_name: "User Name",
  email: "user@example.com",
  role: "writer"
};

// Option 2: Minimal (server will resolve and enrich)
const newAuth = {
  principal_type: "user",
  provider: "google",
  provider_id: "user@example.com",  // Server will look up display_name and email
  role: "writer"
};

POST /threat_models/{id}/authorization
Body: newAuth
```

### Step 4: Update JWT Token Parsing

**Old Code**:
```typescript
interface JWTClaims {
  email: string;
  name: string;
  given_name?: string;  // ❌ No longer available
  family_name?: string;  // ❌ No longer available
  picture?: string;  // ❌ No longer available
  locale?: string;  // ❌ No longer available
  idp: string;
  groups?: string[];
}

// Using removed fields
function getUserProfilePicture(claims: JWTClaims) {
  return claims.picture || "/default-avatar.png";  // ❌ Won't work
}
```

**New Code**:
```typescript
interface JWTClaims {
  email: string;
  name: string;  // Full display name
  idp: string;
  groups?: string[];
}

// Alternative: Fetch from user profile API if needed
async function getUserProfile(userId: string) {
  const response = await fetch(`/users/me`);
  return response.json();  // Returns User object with display_name, email
}
```

## Identity Portability

### Why Provider + Provider ID?

The combination of `provider` + `provider_id` is globally unique and portable:

1. **Globally Unique**: `google:alice@example.com` vs `github:alice` are different users
2. **Portable**: Can be exported and imported between TMI instances
3. **No Internal Dependencies**: Doesn't rely on instance-specific UUIDs
4. **Provider-Independent Groups**: Use `provider: "*"` for groups that span providers

### Special Case: "Everyone" Group

The "everyone" pseudo-group is represented as:
```json
{
  "principal_type": "group",
  "provider": "*",
  "provider_id": "everyone",
  "display_name": "Everyone"
}
```

## Breaking Changes Summary

### API Responses

| Field | Old Type | New Type | Notes |
|-------|----------|----------|-------|
| `authorization[].subject` | `string` (UUID) | ❌ Removed | Use `provider_id` instead |
| `authorization[].subject_type` | `"user" \| "group"` | ❌ Removed | Use `principal_type` instead |
| `authorization[].idp` | `string?` | ❌ Removed | Use `provider` instead |
| `authorization[].principal_type` | - | ✅ `"user" \| "group"` | New required field |
| `authorization[].provider` | - | ✅ `string` | New required field |
| `authorization[].provider_id` | - | ✅ `string` | New required field |
| `authorization[].display_name` | - | ✅ `string?` | New optional field |
| `authorization[].email` | - | ✅ `string?` | New optional field |
| `owner` | `string` | ✅ `User` object | Now full User object |
| `created_by` | `string` | ✅ `User` object | Now full User object |

### JWT Claims

| Claim | Status | Notes |
|-------|--------|-------|
| `given_name` | ❌ Removed | No longer populated |
| `family_name` | ❌ Removed | No longer populated |
| `picture` | ❌ Removed | No longer populated |
| `locale` | ❌ Removed | No longer populated |
| `name` | ✅ Kept | Full display name |
| `email` | ✅ Kept | User email |
| `idp` | ✅ Kept | Identity provider |
| `groups` | ✅ Kept | User groups |

## Testing Your Migration

### 1. Test Authorization Display
```bash
# Fetch a threat model
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/threat_models/{id}

# Verify response has new format:
# - owner is an object with principal fields
# - created_by is an object with principal fields
# - authorization array has principal fields
```

### 2. Test Creating Authorization
```bash
# Add a user to a threat model
curl -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "principal_type": "user",
    "provider": "google",
    "provider_id": "bob@example.com",
    "role": "writer"
  }' \
  http://localhost:8080/threat_models/{id}/authorization
```

### 3. Test JWT Claims
```javascript
// Decode your JWT token
const payload = JSON.parse(atob(token.split('.')[1]));
console.log(payload);

// Verify:
// - No given_name, family_name, picture, locale
// - Still has: name, email, idp, groups
```

## Support

For questions or issues with migration:
- **Issues**: https://github.com/ericfitz/tmi/issues
- **Documentation**: See `docs/developer/` for development guides

## Timeline

- **Schema Changes**: Completed
- **Database Migration**: In progress
- **API Implementation**: In progress
- **Testing**: Pending
- **Deployment**: TBD

Database will be reset (no backward compatibility) since TMI has not launched yet.
