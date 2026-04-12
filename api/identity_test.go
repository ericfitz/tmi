package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	openapi_types "github.com/oapi-codegen/runtime/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/auth"
)

// mockUserResolver implements UserResolver for testing.
type mockUserResolver struct {
	users []auth.User
}

func (m *mockUserResolver) GetUserByID(_ context.Context, id string) (auth.User, error) {
	for _, u := range m.users {
		if u.InternalUUID == id {
			return u, nil
		}
	}
	return auth.User{}, errors.New("user not found")
}

func (m *mockUserResolver) GetUserByProviderID(_ context.Context, provider, providerUserID string) (auth.User, error) {
	for _, u := range m.users {
		if u.Provider == provider && u.ProviderUserID == providerUserID {
			return u, nil
		}
	}
	return auth.User{}, errors.New("user not found")
}

func (m *mockUserResolver) GetUserByProviderAndEmail(_ context.Context, provider, email string) (auth.User, error) {
	for _, u := range m.users {
		if u.Provider == provider && u.Email == email {
			return u, nil
		}
	}
	return auth.User{}, errors.New("user not found")
}

func (m *mockUserResolver) GetUserByEmail(_ context.Context, email string) (auth.User, error) {
	for _, u := range m.users {
		if u.Email == email {
			return u, nil
		}
	}
	return auth.User{}, errors.New("user not found")
}

func (m *mockUserResolver) GetUserByAnyProviderID(_ context.Context, providerUserID string) (auth.User, error) {
	var matches []auth.User
	for _, u := range m.users {
		if u.ProviderUserID == providerUserID {
			matches = append(matches, u)
		}
	}
	if len(matches) == 0 {
		return auth.User{}, errors.New("user not found")
	}
	if len(matches) > 1 {
		return auth.User{}, fmt.Errorf("ambiguous: multiple users with provider ID %q", providerUserID)
	}
	return matches[0], nil
}

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
	assert.Equal(t, "", ru.InternalUUID)
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
	var reqErr *RequestError
	assert.True(t, errors.As(err, &reqErr))
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
	var reqErr *RequestError
	assert.True(t, errors.As(err, &reqErr))
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

// --- ResolveUser tests ---

func newTestResolver() *mockUserResolver {
	return &mockUserResolver{
		users: []auth.User{
			{
				InternalUUID:   "uuid-alice",
				Provider:       "tmi",
				ProviderUserID: "alice",
				Email:          "alice@tmi.local",
				Name:           "Alice",
			},
			{
				InternalUUID:   "uuid-bob",
				Provider:       "google",
				ProviderUserID: "google-bob-123",
				Email:          "bob@gmail.com",
				Name:           "Bob",
			},
			{
				InternalUUID:   "uuid-charlie",
				Provider:       "github",
				ProviderUserID: "gh-charlie",
				Email:          "charlie@example.com",
				Name:           "Charlie",
			},
		},
	}
}

func TestResolveUserByUUIDFound(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{InternalUUID: "uuid-alice"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "tmi", result.Provider)
	assert.Equal(t, "alice", result.ProviderID)
	assert.Equal(t, "alice@tmi.local", result.Email)
	assert.Equal(t, "Alice", result.DisplayName)
}

func TestResolveUserByUUIDNotFound(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{InternalUUID: "uuid-nonexistent"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveUserByUUIDProviderConflict(t *testing.T) {
	resolver := newTestResolver()
	// Alice is "tmi" provider, but we claim "google"
	partial := ResolvedUser{InternalUUID: "uuid-alice", Provider: "google"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider conflict")
}

func TestResolveUserByUUIDProviderIDConflict(t *testing.T) {
	resolver := newTestResolver()
	// Alice's provider ID is "alice", but we claim "wrong-id"
	partial := ResolvedUser{InternalUUID: "uuid-alice", Provider: "tmi", ProviderID: "wrong-id"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider ID conflict")
}

func TestResolveUserByUUIDEmailMismatchTolerated(t *testing.T) {
	resolver := newTestResolver()
	// Alice's email in DB is "alice@tmi.local", but we provide "alice-new@tmi.local"
	partial := ResolvedUser{InternalUUID: "uuid-alice", Email: "alice-new@tmi.local"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	// The provided email should be reflected in the result
	assert.Equal(t, "alice-new@tmi.local", result.Email)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "tmi", result.Provider)
}

func TestResolveUserByProviderAndProviderID(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{Provider: "google", ProviderID: "google-bob-123"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-bob", result.InternalUUID)
	assert.Equal(t, "google", result.Provider)
	assert.Equal(t, "google-bob-123", result.ProviderID)
	assert.Equal(t, "bob@gmail.com", result.Email)
}

func TestResolveUserByProviderAndProviderIDWithEmailReflection(t *testing.T) {
	resolver := newTestResolver()
	// Provide a different email — it should be reflected in the result
	partial := ResolvedUser{Provider: "google", ProviderID: "google-bob-123", Email: "bob-new@gmail.com"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-bob", result.InternalUUID)
	assert.Equal(t, "bob-new@gmail.com", result.Email)
}

func TestResolveUserByProviderIDAndEmail(t *testing.T) {
	resolver := newTestResolver()
	// No provider, but have providerID and email
	partial := ResolvedUser{ProviderID: "gh-charlie", Email: "charlie@example.com"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-charlie", result.InternalUUID)
	assert.Equal(t, "github", result.Provider)
	assert.Equal(t, "gh-charlie", result.ProviderID)
	// Email is reflected from partial
	assert.Equal(t, "charlie@example.com", result.Email)
}

func TestResolveUserByProviderAndEmail(t *testing.T) {
	resolver := newTestResolver()
	// Have provider and email, but no providerID
	partial := ResolvedUser{Provider: "github", Email: "charlie@example.com"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-charlie", result.InternalUUID)
	assert.Equal(t, "github", result.Provider)
	assert.Equal(t, "charlie@example.com", result.Email)
}

func TestResolveUserByEmailOnly(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{Email: "bob@gmail.com"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-bob", result.InternalUUID)
	assert.Equal(t, "google", result.Provider)
	assert.Equal(t, "bob@gmail.com", result.Email)
}

func TestResolveUserNoFieldsProvided(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestResolveUserNotFoundByAnyStrategy(t *testing.T) {
	resolver := newTestResolver()
	partial := ResolvedUser{Email: "nobody@nowhere.com"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveUserEmailReflectionOnMatch(t *testing.T) {
	resolver := newTestResolver()
	// Lookup by UUID, but provide a newer email
	partial := ResolvedUser{InternalUUID: "uuid-bob", Email: "bob-updated@gmail.com"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-bob", result.InternalUUID)
	// The provided email should be reflected, not the DB email
	assert.Equal(t, "bob-updated@gmail.com", result.Email)
}

func TestResolveUserByProviderIDAndEmailFallsToEmailOnly(t *testing.T) {
	resolver := newTestResolver()
	// providerID doesn't match anyone, but email does
	partial := ResolvedUser{ProviderID: "nonexistent-pid", Email: "alice@tmi.local"}

	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "alice@tmi.local", result.Email)
}

func TestResolveUserByProviderAndProviderIDNotFound(t *testing.T) {
	resolver := newTestResolver()
	// Provider+ProviderID is specific enough that we don't fall through
	partial := ResolvedUser{Provider: "tmi", ProviderID: "nonexistent"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestResolveUserAmbiguousProviderID(t *testing.T) {
	// Two users with the same providerID across different providers
	resolver := &mockUserResolver{
		users: []auth.User{
			{InternalUUID: "uuid-1", Provider: "github", ProviderUserID: "shared-id", Email: "user1@example.com", Name: "User1"},
			{InternalUUID: "uuid-2", Provider: "gitlab", ProviderUserID: "shared-id", Email: "user2@example.com", Name: "User2"},
		},
	}
	partial := ResolvedUser{ProviderID: "shared-id", Email: "user1@example.com"}

	_, err := ResolveUser(context.Background(), partial, resolver)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestResolveUserProviderEmailFallsThroughToEmailOnly(t *testing.T) {
	resolver := newTestResolver()
	// Provider + email where provider doesn't match any user with that email
	// Alice is "tmi" provider, not "google"
	partial := ResolvedUser{Provider: "google", Email: "alice@tmi.local"}

	// GetUserByProviderAndEmail won't find (google, alice@tmi.local),
	// but GetUserByEmail will find alice@tmi.local
	result, err := ResolveUser(context.Background(), partial, resolver)
	require.NoError(t, err)
	assert.Equal(t, "uuid-alice", result.InternalUUID)
	assert.Equal(t, "alice@tmi.local", result.Email)
}

func TestResolvedUserFromAuthUserConversion(t *testing.T) {
	u := auth.User{
		InternalUUID:   "uuid-test",
		Provider:       "tmi",
		ProviderUserID: "test-user",
		Email:          "test@example.com",
		Name:           "Test User",
	}

	result := resolvedUserFromAuthUser(u)
	assert.Equal(t, "uuid-test", result.InternalUUID)
	assert.Equal(t, "tmi", result.Provider)
	assert.Equal(t, "test-user", result.ProviderID)
	assert.Equal(t, "test@example.com", result.Email)
	assert.Equal(t, "Test User", result.DisplayName)
}
