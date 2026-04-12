# Identity Comparison Refactor Design

**Issue:** [#253](https://github.com/ericfitz/tmi/issues/253) - fix(auth): user identity matching uses email instead of (provider, provider_id)
**Date:** 2026-04-12
**Branch:** dev/1.4.0

## Summary

User identity matching throughout the server incorrectly compares `userEmail` (JWT `email` claim) against `ProviderId` fields (OAuth `sub` claim). These will never match for providers where email != provider_id (e.g., the TMI provider), breaking ownership transfer, WebSocket host checks, and other authorization flows.

The fix centralizes identity handling into a foundational layer: a canonical `ResolvedUser` type, a strict `SamePrincipal` comparison function, and a `ResolveUser` fuzzy lookup for partially-known identities. All ad-hoc string comparisons and fragmentary identity passing are eliminated.

## Problem

15 instances across 4 files compare `userEmail` against `ProviderId` fields using bare string equality. The correct matching functions (`matchesUserIdentifier`, `matchesProviderID`) exist in `auth_utils.go` but are not used consistently. Additionally, identity is passed around as loose strings (`userEmail`, `providerID`, `internalUUID`) rather than complete structures, making it easy for callers to use the wrong field.

The WebSocket layer has a confirmed type inconsistency: `DiagramSession.Host` is set from email on session creation, but `CurrentPresenter` can be set from `ProviderId` via `processChangePresenter`. Comparisons are a mix of `client.UserEmail == s.Host` and `client.UserID == s.Host`.

## Design Principles

1. **Pass complete identity structures, never fragmentary strings.** Functions receive and return `ResolvedUser`, not ad-hoc combinations of email/providerID/UUID.
2. **Single comparison function for all identity checks.** `SamePrincipal` is the only way to determine if two identities refer to the same person.
3. **Email is a contact attribute, not an identifier.** It is never used for identity comparison in `SamePrincipal`.
4. **Build on foundational security functions.** All identity logic lives in one file (`identity.go`), used everywhere.

## New File: `api/identity.go`

### `ResolvedUser` Type

```go
type ResolvedUser struct {
    InternalUUID string // System-assigned UUID from users table (may be empty if unresolved)
    Provider     string // Identity provider name (e.g., "google", "github", "tmi")
    ProviderID   string // Provider-assigned unique identifier (OAuth sub / SAML NameID)
    Email        string // User's email address (mutable contact attribute, not identity)
    DisplayName  string // Human-readable display name
}
```

Key properties:
- No `Role` field — role is resource-scoped, obtained separately via `GetResourceRole`
- No `PrincipalType` field — this struct is always a user
- `InternalUUID` may be empty for partially-constructed identities before resolution
- Email is carried for display/contact purposes but never used for identity comparison

### Conversion Helpers

```go
func (u ResolvedUser) ToUser() User                      // To API User for wire format
func (u ResolvedUser) ToPrincipal() Principal             // To API Principal for wire format
func ResolvedUserFromUser(u User) ResolvedUser            // From API User (InternalUUID empty)
func ResolvedUserFromPrincipal(p Principal) ResolvedUser  // From API Principal (InternalUUID empty)
```

### `SamePrincipal` — Identity Comparison

```go
func SamePrincipal(a, b ResolvedUser) bool
```

Pure in-memory comparison, no DB access. Algorithm:

1. If `a.InternalUUID` is non-empty AND `b.InternalUUID` is non-empty:
   - If `a.InternalUUID == b.InternalUUID`: log a warning if `(provider, provider_id)` are both populated on both sides and don't match, then return `true`
   - Otherwise return `false`
2. If `a.Provider` is non-empty AND `a.ProviderID` is non-empty AND `b.Provider` is non-empty AND `b.ProviderID` is non-empty:
   - Return `a.Provider == b.Provider && a.ProviderID == b.ProviderID`
3. Otherwise return `false` (insufficient information)

What this does NOT do:
- Never falls back to email comparison
- Returns `false` rather than guessing when fields are missing
- Does not attempt partial matching (e.g., providerID without provider)

### `ResolveUser` — Fuzzy Database Lookup

```go
func ResolveUser(partial ResolvedUser, db *gorm.DB) (ResolvedUser, error)
```

Takes a partially-populated `ResolvedUser` and finds the matching user in the database.

**Input validation:** At least one of `InternalUUID`, `ProviderID`, or `Email` must be non-empty.

**Lookup strategy:**

If `InternalUUID` is non-empty:
- Look up by UUID directly
- If not found: return error (no match, no fallthrough to weaker strategies)
- If found: verify no provided fields conflict:
  - `Provider` mismatch (if provided) -> error
  - `ProviderID` mismatch (if provided) -> error
  - `Email` mismatch -> tolerated (email is mutable)

If `InternalUUID` is empty, try in priority order (first match wins):
1. `Provider` non-empty AND `ProviderID` non-empty -> match on `(provider, provider_id)`, ignore email
2. `Provider` empty, `ProviderID` non-empty, `Email` non-empty -> match on `(provider_id, email)`
3. `Provider` non-empty, `ProviderID` empty, `Email` non-empty -> match on `(provider, email)`
4. Only `Email` non-empty -> match on `email`

**Result validation:**
- Zero matches: return not-found error
- Multiple matches: log explicit error, return ambiguity error
- Exactly one match: verify no provided non-empty fields conflict (same rules as UUID path)

**Return value:** The full user record from the database, with the email field replaced by the provided email if it was non-empty (reflecting the most current email without persisting it). Email updates to the database only happen during authentication, not during resolution.

### `GetAuthenticatedUser` — Gin Context Extractor

```go
func GetAuthenticatedUser(c *gin.Context) (ResolvedUser, error)
```

Replaces `ValidateAuthenticatedUser`. Extracts identity from Gin context keys set by JWT middleware:
- `userInternalUUID` -> InternalUUID
- `userID` -> ProviderID
- `userProvider` -> Provider
- `userEmail` -> Email
- `userDisplayName` -> DisplayName

Requires `userID` and `userEmail` to be present (returns 401 if missing). `userProvider` is populated if available (set by JWT middleware for all authenticated requests, needed for `SamePrincipal` comparisons). `InternalUUID` may be empty if middleware hasn't done a DB lookup.

### `GetResourceRole` — Role Extractor

```go
func GetResourceRole(c *gin.Context) (Role, error)
```

Extracts the resource-scoped role from `userRole` context key. Returns empty role if not set (some endpoints don't have resource middleware). Errors only on type assertion failure.

### `ResolvedUserFromWebSocketClient` — WebSocket Extractor

```go
func ResolvedUserFromWebSocketClient(client *WebSocketClient) ResolvedUser
```

Maps WebSocketClient fields to ResolvedUser:
- `client.InternalUUID` -> InternalUUID (new field on WebSocketClient)
- `client.UserID` -> ProviderID
- `client.UserProvider` -> Provider
- `client.UserEmail` -> Email
- `client.UserName` -> DisplayName

## WebSocket Internal State Changes

### DiagramSession Fields

| Field | Before | After |
|---|---|---|
| `Host` | `string` | `ResolvedUser` |
| `HostUserInfo` | `*User` | Removed (use `Host.ToUser()`) |
| `CurrentPresenter` | `string` | `*ResolvedUser` (nil = no presenter) |
| `CurrentPresenterUserInfo` | `*User` | Removed (use `CurrentPresenter.ToUser()`) |
| `DeniedUsers` | `map[string]bool` | `map[string]bool` keyed by InternalUUID |

### WebSocketClient Fields

| Field | Change |
|---|---|
| `InternalUUID` | New field, populated from ticket at connect time |
| `UserID`, `UserEmail`, `UserProvider`, `UserName` | Unchanged (individual fields kept; `ResolvedUserFromWebSocketClient` bridges to identity layer) |

### Behavioral Changes

- All host/presenter comparisons become `SamePrincipal(ResolvedUserFromWebSocketClient(client), session.Host)`
- `DeniedUsers` denial check: resolve connecting client's InternalUUID, then check set membership
- `broadcastParticipantsUpdate()` uses `session.Host.ToUser()` instead of cached `HostUserInfo`
- `findClientByUserEmail` renamed to `findClientByIdentity`, takes `ResolvedUser`, uses `SamePrincipal`
- WebSocket ticket handler captures `InternalUUID` from Gin context and carries it into the client

### Wire Messages

No changes. All WebSocket messages already carry full `User` objects. The `toUser()` method on `WebSocketClient` continues to produce these for serialization.

## Functions Deleted

| Function | File | Replacement |
|---|---|---|
| `ValidateAuthenticatedUser` | `request_utils.go` | `GetAuthenticatedUser` |
| `matchesUserIdentifier` | `auth_utils.go` | `SamePrincipal` |
| `matchesProviderID` | `auth_utils.go` | `SamePrincipal` |
| `checkUserMatch` | `auth_utils.go` | `SamePrincipal` |

## Functions Refactored

| Function | Change |
|---|---|
| `AccessCheckWithGroups` | Signature changes from `(principal, principalProviderID, principalInternalUUID, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData)` to `(user ResolvedUser, groups []string, requiredRole Role, authData AuthorizationData)` |
| `AccessCheckWithGroupsAndIdPLookup` | Same signature collapse |
| `checkGroupMatch` | Takes `ResolvedUser` instead of loose strings |

## Commit Strategy

Five ordered commits, each building and passing `make test-unit` independently:

| # | Type | Description | Behavioral Change |
|---|---|---|---|
| 1 | `refactor(api)` | Add `identity.go` with `ResolvedUser`, `SamePrincipal`, `ResolveUser`, `GetAuthenticatedUser`, `GetResourceRole`, conversion helpers. New file only. | No |
| 2 | `refactor(api)` | Migrate all handler call sites from `ValidateAuthenticatedUser` to `GetAuthenticatedUser`. Delete `ValidateAuthenticatedUser`. Callers receive `ResolvedUser` but still use old comparison patterns. | No |
| 3 | `fix(auth)` | Replace all identity comparisons with `SamePrincipal`. Refactor `AccessCheckWithGroups` to take `ResolvedUser`. Delete old matching functions. **Fixes #253.** | Yes |
| 4 | `refactor(websocket)` | Migrate WebSocket internals: `Host`/`CurrentPresenter` to `ResolvedUser`, `DeniedUsers` to UUID-keyed, add `InternalUUID` to ticket/client flow, rename `findClientByUserEmail` to `findClientByIdentity`. | Yes |
| 5 | `test(auth)` | Update all test helpers and test cases to use `ResolvedUser` and `SamePrincipal`. | No |
