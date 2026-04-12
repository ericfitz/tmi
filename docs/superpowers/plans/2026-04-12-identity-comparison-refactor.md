# Identity Comparison Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Centralize identity handling into a foundational `ResolvedUser` type with strict `SamePrincipal` comparison, eliminating all ad-hoc string-based identity comparisons that cause auth failures when email != provider_id.

**Architecture:** New `api/identity.go` file provides `ResolvedUser` (canonical identity struct), `SamePrincipal` (in-memory comparison), `ResolveUser` (fuzzy DB lookup), and `GetAuthenticatedUser`/`GetResourceRole` (Gin context extractors). Replaces `ValidateAuthenticatedUser` and the `matchesProviderID`/`matchesUserIdentifier` family. Existing `UserContext` struct in `user_context_utils.go` is replaced by `ResolvedUser`. WebSocket internals migrate from bare strings to `ResolvedUser`.

**Tech Stack:** Go, Gin, GORM, PostgreSQL

**Spec:** `docs/superpowers/specs/2026-04-12-identity-comparison-refactor-design.md`

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `api/identity.go` | Create | `ResolvedUser` type, `SamePrincipal`, `ResolveUser`, `GetAuthenticatedUser`, `GetResourceRole`, conversion helpers |
| `api/identity_test.go` | Create | Tests for all identity functions |
| `api/user_context_utils.go` | Modify | Remove `UserContext` struct and `GetUserContext`/`ValidateUserAuthentication` (replaced by `ResolvedUser`/`GetAuthenticatedUser`). Keep individual getters (`GetUserEmail`, etc.) as they're used by middleware. |
| `api/request_utils.go` | Modify | Delete `ValidateAuthenticatedUser` |
| `api/auth_utils.go` | Modify | Refactor `AccessCheckWithGroups`/`AccessCheckWithGroupsAndIdPLookup` to take `ResolvedUser`; delete `matchesUserIdentifier`, `matchesProviderID`, `checkUserMatch` |
| `api/middleware.go` | Modify | Update `AccessCheckWithGroups` call sites to pass `ResolvedUser` |
| `api/threat_model_handlers.go` | Modify | Replace `ValidateAuthenticatedUser` calls with `GetAuthenticatedUser`; fix owner comparison |
| `api/threat_model_diagram_handlers.go` | Modify | Same handler migration; fix session host check |
| `api/threat_sub_resource_handlers.go` | Modify | Handler migration (11 call sites) |
| `api/cell_handlers.go` | Modify | Handler migration (7 call sites) |
| `api/metadata_handlers.go` | Modify | Handler migration (8 call sites) |
| `api/document_sub_resource_handlers.go` | Modify | Handler migration (8 call sites) |
| `api/asset_sub_resource_handlers.go` | Modify | Handler migration (8 call sites) |
| `api/repository_sub_resource_handlers.go` | Modify | Handler migration (8 call sites) |
| `api/note_sub_resource_handlers.go` | Modify | Handler migration (6 call sites) |
| `api/audit_handlers.go` | Modify | Handler migration (6 call sites) |
| `api/triage_note_handlers.go` | Modify | Handler migration (3 call sites) |
| `api/survey_handlers.go` | Modify | Handler migration (1 call site) |
| `api/webhook_delivery_handlers.go` | Modify | Handler migration (1 call site) |
| `api/user_deletion_handlers.go` | Modify | Handler migration (1 call site) |
| `api/saml_user_handlers.go` | Modify | Handler migration (1 call site) |
| `api/addon_invocation_handlers.go` | Modify | Handler migration (1 call site) |
| `api/server_websocket.go` | Modify | Handler migration (1 call site) |
| `api/ws_ticket_handler.go` | Modify | Add InternalUUID to ticket flow |
| `api/ticket_store.go` | Modify | Add InternalUUID to `IssueTicket`/`ValidateTicket` interface |
| `api/ticket_store_redis.go` | Modify | Add InternalUUID to Redis ticket implementation |
| `api/websocket_validation.go` | Modify | Add InternalUUID to `UserInfo` struct and extraction |
| `api/websocket.go` | Modify | Migrate `Host`/`CurrentPresenter` to `ResolvedUser`, `DeniedUsers` to UUID-keyed, add `InternalUUID` to `WebSocketClient`, rename `findClientByUserEmail` to `findClientByIdentity` |
| `api/middleware_test_helpers.go` | Modify | Update `SetUserContext`/`SetFullUserContext` to also accept/set `userProvider` key consistently |
| `api/auth_test_helpers.go` | Modify | Fix identity comparison patterns |
| `api/auth_utils_test.go` | Modify | Update `AccessCheckWithGroups` test calls |
| Various `*_test.go` files | Modify | Update test setup to use consistent identity fields |

---

## Task 1: Create `ResolvedUser` Type and Conversion Helpers

**Files:**
- Create: `api/identity.go`
- Create: `api/identity_test.go`

- [ ] **Step 1: Write tests for ResolvedUser and conversion helpers**

Create `api/identity_test.go`:

```go
package api

import (
	"testing"

	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
)

func TestResolvedUserToUser(t *testing.T) {
	ru := ResolvedUser{
		InternalUUID: "uuid-123",
		Provider:     "tmi",
		ProviderID:   "alice",
		Email:        "alice@tmi.local",
		DisplayName:  "Alice",
	}

	u := ru.ToUser()
	assert.Equal(t, "alice", u.ProviderId)
	assert.Equal(t, "tmi", u.Provider)
	assert.Equal(t, openapi_types.Email("alice@tmi.local"), u.Email)
	assert.Equal(t, "Alice", u.DisplayName)
	assert.Equal(t, UserPrincipalTypeUser, u.PrincipalType)
}

func TestResolvedUserToPrincipal(t *testing.T) {
	ru := ResolvedUser{
		InternalUUID: "uuid-123",
		Provider:     "google",
		ProviderID:   "google-uid-alice",
		Email:        "alice@gmail.com",
		DisplayName:  "Alice",
	}

	p := ru.ToPrincipal()
	assert.Equal(t, "google-uid-alice", p.ProviderId)
	assert.Equal(t, "google", p.Provider)
	assert.NotNil(t, p.Email)
	assert.Equal(t, openapi_types.Email("alice@gmail.com"), *p.Email)
	assert.NotNil(t, p.DisplayName)
	assert.Equal(t, "Alice", *p.DisplayName)
	assert.Equal(t, PrincipalPrincipalTypeUser, p.PrincipalType)
}

func TestResolvedUserFromUser(t *testing.T) {
	u := User{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         openapi_types.Email("alice@tmi.local"),
		DisplayName:   "Alice",
		PrincipalType: UserPrincipalTypeUser,
	}

	ru := ResolvedUserFromUser(u)
	assert.Equal(t, "", ru.InternalUUID) // Not available from API User
	assert.Equal(t, "tmi", ru.Provider)
	assert.Equal(t, "alice", ru.ProviderID)
	assert.Equal(t, "alice@tmi.local", ru.Email)
	assert.Equal(t, "Alice", ru.DisplayName)
}

func TestResolvedUserFromPrincipal(t *testing.T) {
	email := openapi_types.Email("alice@tmi.local")
	displayName := "Alice"
	p := Principal{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         &email,
		DisplayName:   &displayName,
		PrincipalType: PrincipalPrincipalTypeUser,
	}

	ru := ResolvedUserFromPrincipal(p)
	assert.Equal(t, "", ru.InternalUUID)
	assert.Equal(t, "tmi", ru.Provider)
	assert.Equal(t, "alice", ru.ProviderID)
	assert.Equal(t, "alice@tmi.local", ru.Email)
	assert.Equal(t, "Alice", ru.DisplayName)
}

func TestResolvedUserFromPrincipalNilOptionals(t *testing.T) {
	p := Principal{
		Provider:      "tmi",
		ProviderId:    "alice",
		Email:         nil,
		DisplayName:   nil,
		PrincipalType: PrincipalPrincipalTypeUser,
	}

	ru := ResolvedUserFromPrincipal(p)
	assert.Equal(t, "", ru.Email)
	assert.Equal(t, "", ru.DisplayName)
}

func TestResolvedUserIsEmpty(t *testing.T) {
	assert.True(t, (ResolvedUser{}).IsEmpty())
	assert.False(t, (ResolvedUser{Email: "a@b.com"}).IsEmpty())
	assert.False(t, (ResolvedUser{ProviderID: "x"}).IsEmpty())
	assert.False(t, (ResolvedUser{InternalUUID: "uuid"}).IsEmpty())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestResolvedUser`
Expected: FAIL — `ResolvedUser` type not defined

- [ ] **Step 3: Write `ResolvedUser` type and conversion helpers**

Create `api/identity.go`:

```go
package api

import (
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ResolvedUser is the internal canonical representation of an authenticated user identity.
// It is the ONLY type that should be passed between functions for identity operations.
// It is never serialized to wire format directly — convert to/from API types (User, Principal)
// at system boundaries.
type ResolvedUser struct {
	InternalUUID string // System-assigned UUID from users table (may be empty if unresolved)
	Provider     string // Identity provider name (e.g., "google", "github", "tmi")
	ProviderID   string // Provider-assigned unique identifier (OAuth sub / SAML NameID)
	Email        string // User's email address (mutable contact attribute, not identity)
	DisplayName  string // Human-readable display name
}

// IsEmpty returns true if the ResolvedUser has no identity fields set.
func (u ResolvedUser) IsEmpty() bool {
	return u.InternalUUID == "" && u.Provider == "" && u.ProviderID == "" && u.Email == "" && u.DisplayName == ""
}

// ToUser converts a ResolvedUser to the API User type for wire format serialization.
func (u ResolvedUser) ToUser() User {
	return User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      u.Provider,
		ProviderId:    u.ProviderID,
		Email:         openapi_types.Email(u.Email),
		DisplayName:   u.DisplayName,
	}
}

// ToPrincipal converts a ResolvedUser to the API Principal type for wire format serialization.
func (u ResolvedUser) ToPrincipal() Principal {
	email := openapi_types.Email(u.Email)
	displayName := u.DisplayName
	return Principal{
		PrincipalType: PrincipalPrincipalTypeUser,
		Provider:      u.Provider,
		ProviderId:    u.ProviderID,
		Email:         &email,
		DisplayName:   &displayName,
	}
}

// ResolvedUserFromUser creates a ResolvedUser from an API User.
// InternalUUID will be empty since the API User type does not carry it.
func ResolvedUserFromUser(u User) ResolvedUser {
	return ResolvedUser{
		Provider:    u.Provider,
		ProviderID:  u.ProviderId,
		Email:       string(u.Email),
		DisplayName: u.DisplayName,
	}
}

// ResolvedUserFromPrincipal creates a ResolvedUser from an API Principal.
// InternalUUID will be empty since the API Principal type does not carry it.
func ResolvedUserFromPrincipal(p Principal) ResolvedUser {
	ru := ResolvedUser{
		Provider:   p.Provider,
		ProviderID: p.ProviderId,
	}
	if p.Email != nil {
		ru.Email = string(*p.Email)
	}
	if p.DisplayName != nil {
		ru.DisplayName = *p.DisplayName
	}
	return ru
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestResolvedUser`
Expected: PASS

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: PASS (no new warnings)

- [ ] **Step 6: Commit**

```bash
git add api/identity.go api/identity_test.go
git commit -m "refactor(api): add ResolvedUser type and conversion helpers

Foundation for centralized identity handling. ResolvedUser is the
canonical internal representation of an authenticated user identity.
Conversion helpers bridge to/from API wire types (User, Principal).

Part of #253"
```

---

## Task 2: Add `SamePrincipal` Comparison Function

**Files:**
- Modify: `api/identity.go`
- Modify: `api/identity_test.go`

- [ ] **Step 1: Write tests for SamePrincipal**

Append to `api/identity_test.go`:

```go
func TestSamePrincipalByUUID(t *testing.T) {
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByUUIDDifferent(t *testing.T) {
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-2", Provider: "tmi", ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalByUUIDWithProviderMismatchStillMatches(t *testing.T) {
	// UUID match takes precedence, but provider mismatch should log warning
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{InternalUUID: "uuid-1", Provider: "google", ProviderID: "google-uid"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderID(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderIDDifferentProvider(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "google", ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalByProviderAndProviderIDDifferentID(t *testing.T) {
	a := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "bob"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalInsufficientInfo(t *testing.T) {
	// Only email — not enough for identity comparison
	a := ResolvedUser{Email: "alice@tmi.local"}
	b := ResolvedUser{Email: "alice@tmi.local"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalOneHasUUIDOtherDoesNot(t *testing.T) {
	// Falls through to provider+providerID check
	a := ResolvedUser{InternalUUID: "uuid-1", Provider: "tmi", ProviderID: "alice"}
	b := ResolvedUser{Provider: "tmi", ProviderID: "alice"}
	assert.True(t, SamePrincipal(a, b))
}

func TestSamePrincipalProviderIDWithoutProvider(t *testing.T) {
	// ProviderID alone without provider is insufficient
	a := ResolvedUser{ProviderID: "alice"}
	b := ResolvedUser{ProviderID: "alice"}
	assert.False(t, SamePrincipal(a, b))
}

func TestSamePrincipalBothEmpty(t *testing.T) {
	assert.False(t, SamePrincipal(ResolvedUser{}, ResolvedUser{}))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSamePrincipal`
Expected: FAIL — `SamePrincipal` not defined

- [ ] **Step 3: Implement `SamePrincipal`**

Append to `api/identity.go` (add `"github.com/ericfitz/tmi/internal/slogging"` to imports):

```go
// SamePrincipal returns true if two ResolvedUser values represent the same person.
// Pure in-memory comparison, no DB access. Both arguments should ideally be fully
// resolved (via GetAuthenticatedUser or ResolveUser) before calling.
//
// Algorithm:
// 1. If both have InternalUUID: match on UUID (warn if provider fields conflict)
// 2. If both have Provider AND ProviderID: match on (provider, provider_id)
// 3. Otherwise: return false (insufficient information)
//
// Email is NEVER used for identity comparison.
func SamePrincipal(a, b ResolvedUser) bool {
	logger := slogging.Get()

	// Step 1: UUID comparison (highest priority)
	if a.InternalUUID != "" && b.InternalUUID != "" {
		if a.InternalUUID == b.InternalUUID {
			// UUID matches — warn if provider fields are populated and inconsistent
			if a.Provider != "" && b.Provider != "" && a.ProviderID != "" && b.ProviderID != "" {
				if a.Provider != b.Provider || a.ProviderID != b.ProviderID {
					logger.Warn("SamePrincipal: UUID match (%s) but provider fields differ: (%s, %s) vs (%s, %s)",
						a.InternalUUID, a.Provider, a.ProviderID, b.Provider, b.ProviderID)
				}
			}
			return true
		}
		return false
	}

	// Step 2: Provider + ProviderID comparison
	if a.Provider != "" && a.ProviderID != "" && b.Provider != "" && b.ProviderID != "" {
		return a.Provider == b.Provider && a.ProviderID == b.ProviderID
	}

	// Step 3: Insufficient information
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestSamePrincipal`
Expected: PASS

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/identity.go api/identity_test.go
git commit -m "refactor(api): add SamePrincipal identity comparison function

Strict in-memory comparison: matches on InternalUUID first, then
(provider, provider_id). Never falls back to email. Logs a warning
if UUID matches but provider fields are inconsistent.

Part of #253"
```

---

## Task 3: Add `GetAuthenticatedUser` and `GetResourceRole`

**Files:**
- Modify: `api/identity.go`
- Modify: `api/identity_test.go`

- [ ] **Step 1: Write tests for GetAuthenticatedUser**

Append to `api/identity_test.go`:

```go
import (
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

func TestGetAuthenticatedUserFullContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set("userEmail", "alice@tmi.local")
	c.Set("userID", "alice")
	c.Set("userProvider", "tmi")
	c.Set("userInternalUUID", "uuid-123")
	c.Set("userDisplayName", "Alice")

	user, err := GetAuthenticatedUser(c)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-123", user.InternalUUID)
	assert.Equal(t, "tmi", user.Provider)
	assert.Equal(t, "alice", user.ProviderID)
	assert.Equal(t, "alice@tmi.local", user.Email)
	assert.Equal(t, "Alice", user.DisplayName)
}

func TestGetAuthenticatedUserMinimalContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set("userEmail", "alice@tmi.local")
	c.Set("userID", "alice")

	user, err := GetAuthenticatedUser(c)
	assert.NoError(t, err)
	assert.Equal(t, "", user.InternalUUID)
	assert.Equal(t, "", user.Provider)
	assert.Equal(t, "alice", user.ProviderID)
	assert.Equal(t, "alice@tmi.local", user.Email)
}

func TestGetAuthenticatedUserMissingEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set("userID", "alice")

	_, err := GetAuthenticatedUser(c)
	assert.Error(t, err)
	reqErr, ok := err.(*RequestError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, reqErr.Status)
}

func TestGetAuthenticatedUserMissingProviderID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set("userEmail", "alice@tmi.local")

	_, err := GetAuthenticatedUser(c)
	assert.Error(t, err)
	reqErr, ok := err.(*RequestError)
	assert.True(t, ok)
	assert.Equal(t, http.StatusUnauthorized, reqErr.Status)
}

func TestGetResourceRolePresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	c.Set("userRole", RoleOwner)

	role, err := GetResourceRole(c)
	assert.NoError(t, err)
	assert.Equal(t, RoleOwner, role)
}

func TestGetResourceRoleAbsent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	role, err := GetResourceRole(c)
	assert.NoError(t, err)
	assert.Equal(t, Role(""), role)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestGetAuthenticatedUser`
Expected: FAIL — `GetAuthenticatedUser` not defined

- [ ] **Step 3: Implement `GetAuthenticatedUser` and `GetResourceRole`**

Append to `api/identity.go` (add `"net/http"` and `"github.com/gin-gonic/gin"` to imports):

```go
// GetAuthenticatedUser extracts the authenticated user identity from the Gin context.
// Returns a ResolvedUser populated from JWT claims set by auth middleware.
// Requires userID (provider ID) and userEmail to be present; returns 401 if missing.
// Provider, InternalUUID, and DisplayName are populated if available.
//
// This replaces ValidateAuthenticatedUser. Role is NOT included — use GetResourceRole separately.
func GetAuthenticatedUser(c *gin.Context) (ResolvedUser, error) {
	// Get user email from JWT claim (required)
	userEmailInterface, _ := c.Get("userEmail")
	userEmail, ok := userEmailInterface.(string)
	if !ok || userEmail == "" {
		return ResolvedUser{}, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required",
		}
	}

	// Get provider user ID from JWT "sub" claim (required)
	providerIDInterface, _ := c.Get("userID")
	providerID, ok := providerIDInterface.(string)
	if !ok || providerID == "" {
		return ResolvedUser{}, &RequestError{
			Status:  http.StatusUnauthorized,
			Code:    "unauthorized",
			Message: "Authentication required - missing provider ID",
		}
	}

	// Get provider name (optional — set by JWT middleware)
	provider := ""
	if p, exists := c.Get("userProvider"); exists {
		if pStr, ok := p.(string); ok {
			provider = pStr
		}
	}

	// Get internal UUID (optional — may not be set if middleware hasn't done DB lookup)
	internalUUID := ""
	if uuid, exists := c.Get("userInternalUUID"); exists {
		if uStr, ok := uuid.(string); ok {
			internalUUID = uStr
		}
	}

	// Get display name (optional)
	displayName := ""
	if name, exists := c.Get("userDisplayName"); exists {
		if nStr, ok := name.(string); ok {
			displayName = nStr
		}
	}

	return ResolvedUser{
		InternalUUID: internalUUID,
		Provider:     provider,
		ProviderID:   providerID,
		Email:        userEmail,
		DisplayName:  displayName,
	}, nil
}

// GetResourceRole extracts the resource-scoped role from the Gin context.
// Returns empty role if not set (some endpoints don't have resource middleware).
// Errors only on type assertion failure, not on absence.
func GetResourceRole(c *gin.Context) (Role, error) {
	roleValue, exists := c.Get("userRole")
	if !exists {
		return "", nil
	}

	userRole, ok := roleValue.(Role)
	if !ok {
		return "", &RequestError{
			Status:  http.StatusInternalServerError,
			Code:    "server_error",
			Message: "Failed to determine user role",
		}
	}

	return userRole, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestGetAuthenticatedUser`
Run: `make test-unit name=TestGetResourceRole`
Expected: PASS

- [ ] **Step 5: Add `ResolvedUserFromWebSocketClient`**

Append to `api/identity.go`:

```go
// ResolvedUserFromWebSocketClient creates a ResolvedUser from a WebSocketClient's identity fields.
func ResolvedUserFromWebSocketClient(client *WebSocketClient) ResolvedUser {
	return ResolvedUser{
		InternalUUID: client.InternalUUID,
		Provider:     client.UserProvider,
		ProviderID:   client.UserID,
		Email:        client.UserEmail,
		DisplayName:  client.UserName,
	}
}
```

Note: This will compile after Task 7 adds the `InternalUUID` field to `WebSocketClient`. For now, reference `client.InternalUUID` — it will cause a compile error that Task 7 resolves. If you need the build to pass at this commit, temporarily omit this function and add it in Task 7.

- [ ] **Step 6: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/identity.go api/identity_test.go
git commit -m "refactor(api): add GetAuthenticatedUser and GetResourceRole extractors

GetAuthenticatedUser replaces ValidateAuthenticatedUser, returning a
ResolvedUser instead of loose strings. GetResourceRole extracts the
resource-scoped role separately, keeping identity and authorization
concerns decoupled.

Part of #253"
```

---

## Task 4: Add `ResolveUser` Fuzzy Database Lookup

**Files:**
- Modify: `api/identity.go`
- Modify: `api/identity_test.go`

- [ ] **Step 1: Write tests for ResolveUser**

Append to `api/identity_test.go`. These tests will use a mock/test database. Since TMI uses `database/sql` for some queries and GORM for others, check how existing tests set up DB fixtures. For unit tests, we can test the logic with a test helper that creates an in-memory database.

```go
import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open test DB: %v", err)
	}

	_, err = db.Exec(`CREATE TABLE users (
		internal_uuid TEXT PRIMARY KEY,
		provider TEXT NOT NULL,
		provider_user_id TEXT NOT NULL,
		email TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		email_verified INTEGER NOT NULL DEFAULT 0,
		is_admin INTEGER NOT NULL DEFAULT 0,
		is_security_reviewer INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	// Seed test users
	_, err = db.Exec(`INSERT INTO users (internal_uuid, provider, provider_user_id, email, name) VALUES
		('uuid-alice', 'tmi', 'alice', 'alice@tmi.local', 'Alice'),
		('uuid-bob', 'google', 'google-uid-bob', 'bob@gmail.com', 'Bob'),
		('uuid-charlie', 'tmi', 'charlie', 'charlie@tmi.local', 'Charlie')`)
	if err != nil {
		t.Fatalf("Failed to seed users: %v", err)
	}

	return db
}

func TestResolveUserByUUID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	result, err := ResolveUser(ResolvedUser{InternalUUID: "uuid-alice"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "tmi", result.Provider)
	assert.Equal(t, "alice", result.ProviderID)
	assert.Equal(t, "alice@tmi.local", result.Email)
	assert.Equal(t, "Alice", result.DisplayName)
}

func TestResolveUserByUUIDNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, err := ResolveUser(ResolvedUser{InternalUUID: "uuid-nonexistent"}, db)
	assert.Error(t, err)
}

func TestResolveUserByUUIDWithProviderConflict(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, err := ResolveUser(ResolvedUser{InternalUUID: "uuid-alice", Provider: "google"}, db)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider mismatch")
}

func TestResolveUserByUUIDWithProviderIDConflict(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, err := ResolveUser(ResolvedUser{InternalUUID: "uuid-alice", ProviderID: "wrong-id"}, db)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "provider_id mismatch")
}

func TestResolveUserByUUIDWithEmailMismatchTolerated(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Email mismatch is tolerated — returned user gets the provided email
	result, err := ResolveUser(ResolvedUser{InternalUUID: "uuid-alice", Email: "newalice@tmi.local"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "newalice@tmi.local", result.Email) // Provided email reflected back
	assert.Equal(t, "alice", result.ProviderID)          // DB fields preserved
}

func TestResolveUserByProviderAndProviderID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	result, err := ResolveUser(ResolvedUser{Provider: "tmi", ProviderID: "alice"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "alice@tmi.local", result.Email)
}

func TestResolveUserByProviderIDAndEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	result, err := ResolveUser(ResolvedUser{ProviderID: "alice", Email: "alice@tmi.local"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
}

func TestResolveUserByProviderAndEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	result, err := ResolveUser(ResolvedUser{Provider: "tmi", Email: "alice@tmi.local"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
}

func TestResolveUserByEmailOnly(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	result, err := ResolveUser(ResolvedUser{Email: "alice@tmi.local"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
}

func TestResolveUserNoFields(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, err := ResolveUser(ResolvedUser{}, db)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one")
}

func TestResolveUserNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	_, err := ResolveUser(ResolvedUser{Provider: "tmi", ProviderID: "nonexistent"}, db)
	assert.Error(t, err)
}

func TestResolveUserEmailReflection(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	// Match on provider+providerID, but provide a different email — email gets reflected back
	result, err := ResolveUser(ResolvedUser{Provider: "tmi", ProviderID: "alice", Email: "updated@tmi.local"}, db)
	assert.NoError(t, err)
	assert.Equal(t, "updated@tmi.local", result.Email)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestResolveUser`
Expected: FAIL — `ResolveUser` not defined

- [ ] **Step 3: Implement `ResolveUser`**

Append to `api/identity.go` (add `"database/sql"`, `"fmt"` to imports):

```go
// ResolveUser takes a partially-populated ResolvedUser and finds the matching user
// in the database. Returns a fully-populated ResolvedUser or an error.
//
// At least one of InternalUUID, ProviderID, or Email must be non-empty.
//
// If InternalUUID is provided and not found, returns error (no fallthrough).
// If matched, verifies no provided fields conflict (provider/providerID mismatch = error,
// email mismatch = tolerated). Returns the DB user with the provided email substituted
// if one was given (email is mutable; DB updates only happen during authentication).
func ResolveUser(partial ResolvedUser, db *sql.DB) (ResolvedUser, error) {
	logger := slogging.Get()

	if partial.InternalUUID == "" && partial.ProviderID == "" && partial.Email == "" {
		return ResolvedUser{}, fmt.Errorf("ResolveUser: at least one of InternalUUID, ProviderID, or Email must be provided")
	}

	var result ResolvedUser
	var err error

	if partial.InternalUUID != "" {
		result, err = resolveByUUID(db, partial.InternalUUID)
		if err != nil {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: UUID lookup failed: %w", err)
		}
	} else {
		result, err = resolveByFields(db, partial)
		if err != nil {
			return ResolvedUser{}, err
		}
	}

	// Verify no provided fields conflict with the matched record
	if err := validateResolvedUser(partial, result, logger); err != nil {
		return ResolvedUser{}, err
	}

	// Reflect provided email back if non-empty (email is mutable)
	if partial.Email != "" {
		result.Email = partial.Email
	}

	return result, nil
}

func resolveByUUID(db *sql.DB, uuid string) (ResolvedUser, error) {
	var result ResolvedUser
	err := db.QueryRow(
		"SELECT internal_uuid, provider, provider_user_id, email, name FROM users WHERE internal_uuid = $1",
		uuid,
	).Scan(&result.InternalUUID, &result.Provider, &result.ProviderID, &result.Email, &result.DisplayName)
	if err == sql.ErrNoRows {
		return ResolvedUser{}, fmt.Errorf("user not found with UUID %s", uuid)
	}
	if err != nil {
		return ResolvedUser{}, fmt.Errorf("database error: %w", err)
	}
	return result, nil
}

func resolveByFields(db *sql.DB, partial ResolvedUser) (ResolvedUser, error) {
	type queryStrategy struct {
		query string
		args  []interface{}
		desc  string
	}

	var strategies []queryStrategy

	// Strategy 1: provider + provider_id
	if partial.Provider != "" && partial.ProviderID != "" {
		strategies = append(strategies, queryStrategy{
			query: "SELECT internal_uuid, provider, provider_user_id, email, name FROM users WHERE provider = $1 AND provider_user_id = $2",
			args:  []interface{}{partial.Provider, partial.ProviderID},
			desc:  fmt.Sprintf("provider=%s, provider_id=%s", partial.Provider, partial.ProviderID),
		})
	}

	// Strategy 2: provider_id + email (no provider)
	if partial.Provider == "" && partial.ProviderID != "" && partial.Email != "" {
		strategies = append(strategies, queryStrategy{
			query: "SELECT internal_uuid, provider, provider_user_id, email, name FROM users WHERE provider_user_id = $1 AND email = $2",
			args:  []interface{}{partial.ProviderID, partial.Email},
			desc:  fmt.Sprintf("provider_id=%s, email=%s", partial.ProviderID, partial.Email),
		})
	}

	// Strategy 3: provider + email (no provider_id)
	if partial.Provider != "" && partial.ProviderID == "" && partial.Email != "" {
		strategies = append(strategies, queryStrategy{
			query: "SELECT internal_uuid, provider, provider_user_id, email, name FROM users WHERE provider = $1 AND email = $2",
			args:  []interface{}{partial.Provider, partial.Email},
			desc:  fmt.Sprintf("provider=%s, email=%s", partial.Provider, partial.Email),
		})
	}

	// Strategy 4: email only
	if partial.Email != "" {
		strategies = append(strategies, queryStrategy{
			query: "SELECT internal_uuid, provider, provider_user_id, email, name FROM users WHERE email = $1",
			args:  []interface{}{partial.Email},
			desc:  fmt.Sprintf("email=%s", partial.Email),
		})
	}

	logger := slogging.Get()

	for _, s := range strategies {
		rows, err := db.Query(s.query, s.args...)
		if err != nil {
			return ResolvedUser{}, fmt.Errorf("ResolveUser: database error on %s: %w", s.desc, err)
		}

		var results []ResolvedUser
		for rows.Next() {
			var r ResolvedUser
			if err := rows.Scan(&r.InternalUUID, &r.Provider, &r.ProviderID, &r.Email, &r.DisplayName); err != nil {
				rows.Close()
				return ResolvedUser{}, fmt.Errorf("ResolveUser: scan error: %w", err)
			}
			results = append(results, r)
		}
		rows.Close()

		if len(results) == 1 {
			return results[0], nil
		}
		if len(results) > 1 {
			logger.Error("ResolveUser: ambiguous match on %s — %d users found", s.desc, len(results))
			return ResolvedUser{}, fmt.Errorf("ResolveUser: ambiguous match on %s — %d users found", s.desc, len(results))
		}
		// Zero results — try next strategy
	}

	return ResolvedUser{}, fmt.Errorf("ResolveUser: no user found matching provided fields")
}

func validateResolvedUser(partial, result ResolvedUser, logger *slogging.Logger) error {
	if partial.Provider != "" && result.Provider != partial.Provider {
		return fmt.Errorf("ResolveUser: provider mismatch — provided %q but found %q for user %s",
			partial.Provider, result.Provider, result.InternalUUID)
	}
	if partial.ProviderID != "" && result.ProviderID != partial.ProviderID {
		return fmt.Errorf("ResolveUser: provider_id mismatch — provided %q but found %q for user %s",
			partial.ProviderID, result.ProviderID, result.InternalUUID)
	}
	// Email mismatch is tolerated (email is mutable)
	if partial.Email != "" && result.Email != partial.Email {
		logger.Debug("ResolveUser: email differs — provided %q, DB has %q for user %s (tolerated)",
			partial.Email, result.Email, result.InternalUUID)
	}
	return nil
}
```

**Important note on SQL placeholders:** The above uses `$1`, `$2` style (PostgreSQL). SQLite uses `?` style. The tests use SQLite in-memory, so the test helper's queries must use `?`. The production code uses `$1` for PostgreSQL. To handle this, either:
- Use `?` placeholders (works with both SQLite and some PostgreSQL drivers)
- Or make the tests use the same test DB infrastructure as existing integration tests

Check how existing tests handle this — if they use SQLite, use `?` placeholders throughout. If they use PostgreSQL, the unit tests should use `?` (compatible with both) and the production queries should also use `?` since the `database/sql` driver handles placeholder translation.

**Revision:** Use `?` placeholders in all queries for cross-driver compatibility:

Replace all `$1`, `$2` with `?` in the queries above.

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestResolveUser`
Expected: PASS

- [ ] **Step 5: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/identity.go api/identity_test.go
git commit -m "refactor(api): add ResolveUser fuzzy database lookup

Resolves partially-known identities against the users table.
Priority order: UUID > (provider, provider_id) > (provider_id, email)
> (provider, email) > email. Exactly one match required; ambiguous
matches log an error and fail. Email mismatch tolerated (mutable).

Part of #253"
```

---

## Task 5: Migrate Handler Call Sites — Commit 2 (Mechanical)

This task changes all ~71 handler call sites from `ValidateAuthenticatedUser` to `GetAuthenticatedUser`. This is a mechanical change — callers receive `ResolvedUser` but temporarily access `.Email` and `.ProviderID` fields to preserve existing comparison behavior. The semantic fix comes in Task 6.

**Files:** All handler files listed in the File Map above, plus `api/request_utils.go`

- [ ] **Step 1: Identify the migration pattern**

Every call site follows one of these patterns:

Pattern A (most common — ignores providerID and role):
```go
// Before:
userEmail, _, _, err := ValidateAuthenticatedUser(c)
// After:
user, err := GetAuthenticatedUser(c)
// Then use user.Email where userEmail was used
```

Pattern B (captures providerID):
```go
// Before:
userEmail, providerID, _, err := ValidateAuthenticatedUser(c)
// After:
user, err := GetAuthenticatedUser(c)
// Then use user.Email where userEmail was used, user.ProviderID where providerID was used
```

Pattern C (captures role):
```go
// Before:
userEmail, _, userRole, err := ValidateAuthenticatedUser(c)
// After:
user, err := GetAuthenticatedUser(c)
userRole, err := GetResourceRole(c)
// Then use user.Email where userEmail was used
```

- [ ] **Step 2: Migrate threat_model_handlers.go (6 call sites)**

For each call site in `api/threat_model_handlers.go` (lines 51, 131, 174, 394, 618, 822):

Replace the `ValidateAuthenticatedUser` call and update all references to the returned variables. For example, at line 174:

```go
// Before:
userEmail, providerID, _, err := ValidateAuthenticatedUser(c)

// After:
user, err := GetAuthenticatedUser(c)
```

Then replace `userEmail` with `user.Email` and `providerID` with `user.ProviderID` throughout the function scope. Do this for all 6 call sites in the file.

- [ ] **Step 3: Migrate threat_model_diagram_handlers.go (10 call sites)**

Same pattern as Step 2 for all 10 call sites (lines 34, 139, 227, 290, 424, 555, 630, 704, 785, 892).

- [ ] **Step 4: Migrate threat_sub_resource_handlers.go (11 call sites)**

Lines 73, 117, 289, 330, 414, 490, 568, 620, 717, 798, 868.

For call sites that capture the role (e.g., line 490: `userEmail, _, userRole, err := ValidateAuthenticatedUser(c)`), add a separate `GetResourceRole` call.

- [ ] **Step 5: Migrate remaining handler files**

Apply the same pattern to all remaining files:
- `api/cell_handlers.go` (7 sites)
- `api/metadata_handlers.go` (8 sites)
- `api/document_sub_resource_handlers.go` (8 sites)
- `api/asset_sub_resource_handlers.go` (8 sites)
- `api/repository_sub_resource_handlers.go` (8 sites)
- `api/note_sub_resource_handlers.go` (6 sites)
- `api/audit_handlers.go` (6 sites)
- `api/triage_note_handlers.go` (3 sites)
- `api/survey_handlers.go` (1 site)
- `api/webhook_delivery_handlers.go` (1 site)
- `api/user_deletion_handlers.go` (1 site)
- `api/saml_user_handlers.go` (1 site)
- `api/addon_invocation_handlers.go` (1 site)
- `api/server_websocket.go` (1 site)
- `api/ws_ticket_handler.go` (1 site)
- `api/auth_utils.go` (1 site — the `CheckSubResourceAccessFromContext` function)

- [ ] **Step 6: Delete `ValidateAuthenticatedUser` from `request_utils.go`**

Remove the function definition at `api/request_utils.go:356-401`.

- [ ] **Step 7: Remove `UserContext` and related functions from `user_context_utils.go`**

Delete `UserContext` struct (line 235), `GetUserContext` (line 217), and `ValidateUserAuthentication` (line 191) — they're superseded by `ResolvedUser` and `GetAuthenticatedUser`. Keep the individual getters (`GetUserEmail`, `GetUserProvider`, etc.) since middleware still uses them.

Update any callers of the removed functions. Search for `GetUserContext` and `ValidateUserAuthentication` call sites and migrate them to `GetAuthenticatedUser`.

- [ ] **Step 8: Build and run unit tests**

Run: `make build-server`
Expected: PASS

Run: `make test-unit`
Expected: PASS (some test files may need updating — see Step 9)

- [ ] **Step 9: Fix any test compilation errors**

Tests that call `ValidateAuthenticatedUser` directly (e.g., `request_utils_test.go`) need updating to call `GetAuthenticatedUser`. Tests that use `UserContext` need updating to use `ResolvedUser`.

Update `api/middleware_test_helpers.go` `SetFullUserContext` to also set `"userProvider"` (currently it sets `"userIdP"` but `GetAuthenticatedUser` reads `"userProvider"`):

```go
func SetFullUserContext(c *gin.Context, email, userID, internalUUID, idp string, groups []string) {
	c.Set("userEmail", email)
	c.Set("userID", userID)
	if internalUUID != "" {
		c.Set("userInternalUUID", internalUUID)
	}
	if idp != "" {
		c.Set("userIdP", idp)
		c.Set("userProvider", idp)  // GetAuthenticatedUser reads this key
	}
	if groups != nil {
		c.Set("userGroups", groups)
	}
}
```

- [ ] **Step 10: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 11: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 12: Commit**

```bash
git add -A
git commit -m "refactor(api): migrate all handlers from ValidateAuthenticatedUser to GetAuthenticatedUser

Mechanical migration: all 71 handler call sites now use
GetAuthenticatedUser returning ResolvedUser. Callers temporarily
access .Email/.ProviderID fields to preserve existing behavior.
Deletes ValidateAuthenticatedUser and UserContext (superseded).

Part of #253"
```

---

## Task 6: Replace Identity Comparisons with `SamePrincipal` — Commit 3 (Semantic Fix)

This is the commit that fixes the bug. All direct string comparisons for identity are replaced with `SamePrincipal`.

**Files:**
- Modify: `api/auth_utils.go` — refactor `AccessCheckWithGroups` signature, delete old matching functions
- Modify: `api/middleware.go` — update callers of `AccessCheckWithGroups`
- Modify: `api/threat_model_handlers.go` — fix owner comparison (lines 1116-1129)
- Modify: `api/threat_model_diagram_handlers.go` — fix session host check (line 837)

- [ ] **Step 1: Refactor `AccessCheckWithGroups` signature**

In `api/auth_utils.go`, change:

```go
// Before:
func AccessCheckWithGroups(principal string, principalProviderID string, principalInternalUUID string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {
	return AccessCheckWithGroupsAndIdPLookup(principal, principalProviderID, principalInternalUUID, principalIdP, principalGroups, requiredRole, authData)
}

// After:
func AccessCheckWithGroups(user ResolvedUser, groups []string, requiredRole Role, authData AuthorizationData) bool {
	return AccessCheckWithGroupsAndIdPLookup(user, groups, requiredRole, authData)
}
```

Similarly refactor `AccessCheckWithGroupsAndIdPLookup`:

```go
// Before:
func AccessCheckWithGroupsAndIdPLookup(principal string, principalProviderID string, principalInternalUUID string, principalIdP string, principalGroups []string, requiredRole Role, authData AuthorizationData) bool {

// After:
func AccessCheckWithGroupsAndIdPLookup(user ResolvedUser, groups []string, requiredRole Role, authData AuthorizationData) bool {
```

Inside the function body, replace:
- `matchesUserIdentifier(authData.Owner, principal, principalProviderID, principalInternalUUID)` → `SamePrincipal(user, ResolvedUserFromUser(authData.Owner))`
- `checkUserMatch(auth, principal, principalProviderID, principalInternalUUID)` → `SamePrincipal(user, ResolvedUserFromAuthorization(auth))`
- `checkGroupMatch(auth, principal, principalIdP, principalGroups)` → `checkGroupMatch(auth, user, groups)`

Add a new conversion helper in `api/identity.go`:

```go
// ResolvedUserFromAuthorization creates a ResolvedUser from an Authorization entry.
func ResolvedUserFromAuthorization(auth Authorization) ResolvedUser {
	ru := ResolvedUser{
		Provider:   auth.Provider,
		ProviderID: auth.ProviderId,
	}
	if auth.Email != nil {
		ru.Email = string(*auth.Email)
	}
	if auth.DisplayName != nil {
		ru.DisplayName = *auth.DisplayName
	}
	return ru
}
```

- [ ] **Step 2: Refactor `checkGroupMatch` to take `ResolvedUser`**

```go
// Before:
func checkGroupMatch(auth Authorization, principal string, principalIdP string, principalGroups []string) bool {

// After:
func checkGroupMatch(auth Authorization, user ResolvedUser, groups []string) bool {
```

Update the body to use `user.Email` where `principal` was used (for logging) and `user.Provider` where `principalIdP` was used.

- [ ] **Step 3: Refactor `CheckSubResourceAccess` to take `ResolvedUser`**

```go
// Before:
func CheckSubResourceAccess(ctx context.Context, db *sql.DB, cache *CacheService, principal, principalProviderID, principalInternalUUID, principalIdP string, principalGroups []string, threatModelID string, requiredRole Role) (bool, error) {

// After:
func CheckSubResourceAccess(ctx context.Context, db *sql.DB, cache *CacheService, user ResolvedUser, groups []string, threatModelID string, requiredRole Role) (bool, error) {
```

Update the `AccessCheckWithGroups` call inside to pass `user, groups`.

- [ ] **Step 4: Delete old matching functions**

Remove from `api/auth_utils.go`:
- `matchesUserIdentifier` (line 606)
- `matchesProviderID` (line 613)
- `checkUserMatch` (line 627)

- [ ] **Step 5: Update middleware callers**

In `api/middleware.go`, every `AccessCheckWithGroups` call currently passes 7 args. Update each to pass `ResolvedUser` + groups.

The middleware functions extract identity via `GetUserAuthFieldsForAccessCheck`. Update these to build a `ResolvedUser`:

```go
// In middleware functions that call AccessCheckWithGroups:
providerUserID, internalUUID, provider, groups := GetUserAuthFieldsForAccessCheck(c)
userEmail, _ := c.Get("userEmail")
email, _ := userEmail.(string)

user := ResolvedUser{
	InternalUUID: internalUUID,
	Provider:     provider,
	ProviderID:   providerUserID,
	Email:        email,
}

// Then:
AccessCheckWithGroups(user, groups, RoleOwner, authData)
```

Apply this to all 8 call sites in middleware.go.

- [ ] **Step 6: Fix owner comparison in threat_model_handlers.go**

At line 1116-1129, replace:

```go
// Before:
hasOwnerRole := (original.Owner.ProviderId == userEmail)
if !hasOwnerRole {
	for _, auth := range derefAuthSlice(original.Authorization) {
		if auth.ProviderId == userEmail && auth.Role == RoleOwner {

// After:
hasOwnerRole := SamePrincipal(user, ResolvedUserFromUser(original.Owner))
if !hasOwnerRole {
	for _, auth := range derefAuthSlice(original.Authorization) {
		if SamePrincipal(user, ResolvedUserFromAuthorization(auth)) && auth.Role == RoleOwner {
```

- [ ] **Step 7: Fix session host check in threat_model_diagram_handlers.go**

At line 837, replace:

```go
// Before:
isHost := (session.Host == userEmail)

// After:
isHost := SamePrincipal(user, session.Host) // session.Host becomes ResolvedUser in Task 7
```

**Note:** This step depends on Task 7 (WebSocket migration). If implementing sequentially, this comparison can temporarily use `SamePrincipal(user, ResolvedUser{Email: session.Host})` as a bridge — it won't match on email (SamePrincipal never uses email), so this needs the WebSocket migration. Alternative: do this specific fix in Task 7 instead.

**Revised approach:** Leave the session host check for Task 7. In this task, fix only the non-WebSocket identity comparisons.

- [ ] **Step 8: Update callers of `CheckSubResourceAccess`**

Search for all callers and update to pass `ResolvedUser` + groups instead of loose strings. The main caller is in `auth_utils.go` itself (`CheckSubResourceAccessFromContext`) and in `auth_test_helpers.go`.

- [ ] **Step 9: Build and run tests**

Run: `make build-server`
Expected: PASS

Run: `make test-unit`
Expected: Some tests may fail due to `AccessCheckWithGroups` signature change — fix in next step.

- [ ] **Step 10: Fix test calls to `AccessCheckWithGroups`**

In `api/auth_utils_test.go`, update all calls like:

```go
// Before:
result := AccessCheckWithGroups(tt.principal, "", "", tt.principalIdP, tt.groups, tt.requiredRole, tt.authData)

// After:
user := ResolvedUser{Email: tt.principal, Provider: tt.principalIdP}
result := AccessCheckWithGroups(user, tt.groups, tt.requiredRole, tt.authData)
```

Note: Tests that relied on email matching will now fail because `SamePrincipal` doesn't match on email. These tests need to provide `ProviderID` and `Provider` fields that match the auth data entries. Update test fixtures accordingly.

- [ ] **Step 11: Run full test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 12: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 13: Commit**

```bash
git add -A
git commit -m "fix(auth): replace identity comparisons with SamePrincipal

All identity comparisons now use SamePrincipal which matches on
InternalUUID or (provider, provider_id) — never email. Refactors
AccessCheckWithGroups to take ResolvedUser instead of 4 string params.
Deletes matchesProviderID, matchesUserIdentifier, checkUserMatch.

Fixes #253"
```

---

## Task 7: Migrate WebSocket Internals — Commit 4

**Files:**
- Modify: `api/websocket.go`
- Modify: `api/websocket_validation.go`
- Modify: `api/ticket_store.go`
- Modify: `api/ticket_store_redis.go`
- Modify: `api/ws_ticket_handler.go`
- Modify: `api/threat_model_diagram_handlers.go` (session host check deferred from Task 6)

- [ ] **Step 1: Add InternalUUID to ticket store interface**

In `api/ticket_store.go`, update the interface:

```go
// Before:
IssueTicket(ctx context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error)
ValidateTicket(ctx context.Context, ticket string) (userID, provider, sessionID string, err error)

// After:
IssueTicket(ctx context.Context, userID, provider, internalUUID, sessionID string, ttl time.Duration) (string, error)
ValidateTicket(ctx context.Context, ticket string) (userID, provider, internalUUID, sessionID string, err error)
```

Update `InMemoryTicketStore` implementation to store and return `internalUUID`.

- [ ] **Step 2: Update Redis ticket store**

In `api/ticket_store_redis.go`, update `IssueTicket` and `ValidateTicket` to include `internalUUID` in the stored/retrieved data.

- [ ] **Step 3: Update ws_ticket_handler.go**

Extract `internalUUID` from context and pass to `IssueTicket`:

```go
// Add after existing identity extraction:
internalUUID := c.GetString("userInternalUUID")

// Update ticket creation:
ticket, err := s.ticketStore.IssueTicket(c.Request.Context(), userID, provider, internalUUID, sessionID, wsTicketTTL)
```

- [ ] **Step 4: Add InternalUUID to UserInfo and WebSocketClient**

In `api/websocket_validation.go`, add `InternalUUID` to `UserInfo`:

```go
type UserInfo struct {
	UserID       string
	UserName     string
	UserEmail    string
	UserProvider string
	InternalUUID string  // New
}
```

Update `ExtractUserInfo` to populate `InternalUUID` from context.

In `api/websocket.go`, add `InternalUUID` to `WebSocketClient`:

```go
type WebSocketClient struct {
	// ... existing fields ...
	// User's internal UUID from the users table
	InternalUUID string
	// ... rest of fields ...
}
```

Update client creation (line ~1689) to populate `InternalUUID: userInfo.InternalUUID`.

- [ ] **Step 5: Migrate DiagramSession.Host to ResolvedUser**

In `api/websocket.go`, change:

```go
// Before:
Host             string
HostUserInfo     *User
CurrentPresenter string
CurrentPresenterUserInfo *User

// After:
Host             ResolvedUser
CurrentPresenter *ResolvedUser
```

Update `GetOrCreateSession` to accept `ResolvedUser` for the host parameter:

```go
// Before:
func (h *WebSocketHub) GetOrCreateSession(diagramID, threatModelID, hostUserID string) *DiagramSession {

// After:
func (h *WebSocketHub) GetOrCreateSession(diagramID, threatModelID string, hostUser ResolvedUser) *DiagramSession {
```

Update session creation to use `ResolvedUser`:

```go
Host:             hostUser,
CurrentPresenter: &hostUser, // Host starts as presenter
```

- [ ] **Step 6: Update all host/presenter comparisons**

Replace every `client.UserEmail == s.Host` or `client.UserID == s.Host` with:

```go
SamePrincipal(ResolvedUserFromWebSocketClient(client), s.Host)
```

Replace every `client.UserEmail == s.CurrentPresenter` with:

```go
s.CurrentPresenter != nil && SamePrincipal(ResolvedUserFromWebSocketClient(client), *s.CurrentPresenter)
```

Update presenter assignment:

```go
// Before:
s.CurrentPresenter = client.UserEmail

// After:
presenterUser := ResolvedUserFromWebSocketClient(client)
s.CurrentPresenter = &presenterUser
```

And for change presenter from request:

```go
// Before:
s.CurrentPresenter = req.NewPresenter.ProviderId
newPresenterUser := targetClient.toUser()
s.CurrentPresenterUserInfo = &newPresenterUser

// After:
presenterUser := ResolvedUserFromWebSocketClient(targetClient)
s.CurrentPresenter = &presenterUser
```

- [ ] **Step 7: Migrate DeniedUsers to UUID-keyed**

```go
// Before:
DeniedUsers map[string]bool  // keyed by email or providerID

// After:
DeniedUsers map[string]bool  // keyed by InternalUUID
```

Update denial check (line ~1420):

```go
// Before:
if s.DeniedUsers[client.UserEmail] || s.DeniedUsers[client.UserID] {

// After:
if client.InternalUUID != "" && s.DeniedUsers[client.InternalUUID] {
```

Update denial write (line ~1932):

```go
// Before:
s.DeniedUsers[req.RemovedUser.ProviderId] = true

// After:
removedClient := findClientByIdentity(s, ResolvedUserFromUser(req.RemovedUser))
if removedClient != nil && removedClient.InternalUUID != "" {
	s.DeniedUsers[removedClient.InternalUUID] = true
}
```

- [ ] **Step 8: Rename findClientByUserEmail to findClientByIdentity**

```go
// Before:
func findClientByUserEmail(s *DiagramSession, userEmail string) *WebSocketClient {
	for client := range s.Clients {
		if client.UserEmail == userEmail {
			return client
		}
	}
	return nil
}

// After:
func findClientByIdentity(s *DiagramSession, target ResolvedUser) *WebSocketClient {
	for client := range s.Clients {
		if SamePrincipal(ResolvedUserFromWebSocketClient(client), target) {
			return client
		}
	}
	return nil
}
```

Update all callers of `findClientByUserEmail` to use `findClientByIdentity` with a `ResolvedUser`.

- [ ] **Step 9: Update broadcastParticipantsUpdate**

Replace `HostUserInfo` / `CurrentPresenterUserInfo` with `.ToUser()`:

```go
// Before:
hostUser := s.HostUserInfo

// After:
hostAPIUser := s.Host.ToUser()
```

```go
// Before:
presenterUser := s.CurrentPresenterUserInfo

// After:
var presenterAPIUser *User
if s.CurrentPresenter != nil {
	u := s.CurrentPresenter.ToUser()
	presenterAPIUser = &u
}
```

- [ ] **Step 10: Fix session host check in threat_model_diagram_handlers.go**

Now that `session.Host` is a `ResolvedUser`:

```go
// Before:
isHost := (session.Host == userEmail)

// After:
isHost := SamePrincipal(user, session.Host)
```

- [ ] **Step 11: Build and run tests**

Run: `make build-server`
Expected: PASS

Run: `make test-unit`
Expected: Some WebSocket tests may fail — fix in next step.

- [ ] **Step 12: Fix WebSocket tests**

Update test code that creates `DiagramSession` with string `Host` to use `ResolvedUser`. Update tests that check `DeniedUsers` to use UUID keys. Update tests that reference `HostUserInfo`/`CurrentPresenterUserInfo` to use `.ToUser()`.

- [ ] **Step 13: Run full test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 14: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 15: Commit**

```bash
git add -A
git commit -m "refactor(websocket): migrate identity to ResolvedUser

Host and CurrentPresenter are now ResolvedUser instead of bare strings.
DeniedUsers keyed by InternalUUID. InternalUUID added to ticket flow
and WebSocketClient. All identity comparisons use SamePrincipal.
Removes HostUserInfo/CurrentPresenterUserInfo caching (use .ToUser()).

Part of #253"
```

---

## Task 8: Update Test Helpers and Test Cases — Commit 5

**Files:**
- Modify: `api/auth_test_helpers.go`
- Modify: `api/middleware_test_helpers.go`
- Modify: Various `*_test.go` files

- [ ] **Step 1: Fix auth_test_helpers.go identity patterns**

At lines 287/293, replace direct `ProviderId == userEmail` comparisons:

```go
// Before:
if auth.ProviderId == userEmail && auth.Role == expectedRole {

// After:
if SamePrincipal(user, ResolvedUserFromAuthorization(auth)) && auth.Role == expectedRole {
```

Update `CheckSubResourceAccess` calls to pass `ResolvedUser`:

```go
// Before:
hasAccess, err := CheckSubResourceAccess(h.TestContext, h.DB, h.Cache, userEmail, "", "", "", []string{}, threatModelID, expectedRole)

// After:
user := ResolvedUser{Provider: "test", ProviderID: userEmail, Email: userEmail}
hasAccess, err := CheckSubResourceAccess(h.TestContext, h.DB, h.Cache, user, []string{}, threatModelID, expectedRole)
```

- [ ] **Step 2: Update middleware_test_helpers.go**

Ensure `SetFullUserContext` sets both `userIdP` and `userProvider`:

```go
func SetFullUserContext(c *gin.Context, email, userID, internalUUID, idp string, groups []string) {
	c.Set("userEmail", email)
	c.Set("userID", userID)
	if internalUUID != "" {
		c.Set("userInternalUUID", internalUUID)
	}
	if idp != "" {
		c.Set("userIdP", idp)
		c.Set("userProvider", idp)
	}
	if groups != nil {
		c.Set("userGroups", groups)
	}
}
```

Also update `SetUserContext` to set provider:

```go
func SetUserContext(c *gin.Context, email, userID string, role Role) {
	c.Set("userEmail", email)
	c.Set("userID", userID)
	if role != "" {
		c.Set("userRole", role)
	}
}
```

- [ ] **Step 3: Update ticket store tests**

In `api/ticket_store_test.go`, update `IssueTicket` and `ValidateTicket` calls to include `internalUUID` parameter.

- [ ] **Step 4: Update WebSocket test fixtures**

Update `api/websocket_session_authz_test.go` and related test files to use `ResolvedUser` for session Host/CurrentPresenter.

- [ ] **Step 5: Run full test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 6: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "test(auth): update all tests for ResolvedUser and SamePrincipal

Aligns test helpers and test cases with the new identity model.
Test identity comparisons use SamePrincipal instead of string equality.
SetFullUserContext sets both userIdP and userProvider keys.

Part of #253"
```

---

## Task 9: Final Verification and Cleanup

- [ ] **Step 1: Run full build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Run lint**

Run: `make lint`
Expected: PASS (no new warnings)

- [ ] **Step 4: Verify no remaining old patterns**

Search for any remaining direct identity string comparisons:

```bash
# Check for old function calls
grep -rn "matchesProviderID\|matchesUserIdentifier\|checkUserMatch\|ValidateAuthenticatedUser" api/ --include="*.go" | grep -v "_test.go" | grep -v "identity.go"

# Check for direct ProviderId == comparisons (excluding generated api.go)
grep -rn "ProviderId ==" api/ --include="*.go" | grep -v "api/api.go" | grep -v "_test.go"

# Check for UserContext usage
grep -rn "UserContext\|GetUserContext\|ValidateUserAuthentication" api/ --include="*.go" | grep -v "_test.go"
```

Expected: No matches (all patterns migrated)

- [ ] **Step 5: Verify old functions are deleted**

```bash
grep -rn "func matchesProviderID\|func matchesUserIdentifier\|func checkUserMatch\|func ValidateAuthenticatedUser" api/ --include="*.go"
```

Expected: No matches

- [ ] **Step 6: Review diff against issue requirements**

```bash
git log --oneline dev/1.4.0 ^main | head -20
```

Verify the 5 commits are present in the expected order with the expected types.
