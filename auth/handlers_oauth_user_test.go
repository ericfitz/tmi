package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeUserResolver is a test double for the userResolver interface. Each call
// returns the configured response for that method; not-found is signaled by
// returning errFakeUserNotFound. Callers may override individual fields per
// test case.
type fakeUserResolver struct {
	byProviderID       *User
	byProviderAndEmail *User
	byEmail            *User

	createdUser User
	createErr   error

	createCalls int
}

var errFakeUserNotFound = errors.New("user not found")

func (f *fakeUserResolver) GetUserByProviderID(_ context.Context, _, _ string) (User, error) {
	if f.byProviderID == nil {
		return User{}, errFakeUserNotFound
	}
	return *f.byProviderID, nil
}

func (f *fakeUserResolver) GetUserByProviderAndEmail(_ context.Context, _, _ string) (User, error) {
	if f.byProviderAndEmail == nil {
		return User{}, errFakeUserNotFound
	}
	return *f.byProviderAndEmail, nil
}

func (f *fakeUserResolver) GetUserByEmail(_ context.Context, _ string) (User, error) {
	if f.byEmail == nil {
		return User{}, errFakeUserNotFound
	}
	return *f.byEmail, nil
}

func (f *fakeUserResolver) CreateUser(_ context.Context, u User) (User, error) {
	f.createCalls++
	if f.createErr != nil {
		return User{}, f.createErr
	}
	created := f.createdUser
	if created.InternalUUID == "" {
		created = u
		created.InternalUUID = uuid.NewString()
	}
	return created, nil
}

func newTestGinContext() *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/oauth2/token", nil)
	return c
}

// Tier 1 — same-provider re-login: returns userMatchProviderID with no error.
func TestFindOrCreateUser_SameProviderRelogin(t *testing.T) {
	existing := User{
		InternalUUID:   "uuid-google-alice",
		Provider:       "google",
		ProviderUserID: "google-123",
		Email:          "alice@example.com",
		EmailVerified:  true,
	}
	resolver := &fakeUserResolver{byProviderID: &existing}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "alice@example.com", "Alice", true)

	require.NoError(t, err)
	assert.Equal(t, userMatchProviderID, match)
	assert.Equal(t, existing.InternalUUID, user.InternalUUID)
	assert.Equal(t, 0, resolver.createCalls)
}

// Tier 2 — provider+email match: returns userMatchProviderEmail with no error.
func TestFindOrCreateUser_SameProviderEmailMatch(t *testing.T) {
	existing := User{
		InternalUUID:   "uuid-google-alice",
		Provider:       "google",
		ProviderUserID: "", // sparse on provider_user_id
		Email:          "alice@example.com",
	}
	resolver := &fakeUserResolver{byProviderAndEmail: &existing}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "alice@example.com", "Alice", true)

	require.NoError(t, err)
	assert.Equal(t, userMatchProviderEmail, match)
	assert.Equal(t, existing.InternalUUID, user.InternalUUID)
	assert.Equal(t, 0, resolver.createCalls)
}

// Tier 3 — sparse-record completion (verified email): binds the user.
func TestFindOrCreateUser_SparseRecordVerifiedEmailBinds(t *testing.T) {
	sparse := User{
		InternalUUID:  "uuid-sparse-alice",
		Provider:      "", // sparse — no provider yet
		Email:         "alice@example.com",
		EmailVerified: true,
	}
	resolver := &fakeUserResolver{byEmail: &sparse}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "alice@example.com", "Alice", true /* email_verified */)

	require.NoError(t, err)
	assert.Equal(t, userMatchEmailOnly, match)
	assert.Equal(t, sparse.InternalUUID, user.InternalUUID)
	assert.Equal(t, 0, resolver.createCalls)
}

// Tier 3 — cross-provider conflict (#290): MUST reject with a sentinel error
// and MUST NOT return the existing user record.
func TestFindOrCreateUser_CrossProviderConflictRejected(t *testing.T) {
	existing := User{
		InternalUUID:   "uuid-google-alice",
		Provider:       "google", // already bound to google
		ProviderUserID: "google-123",
		Email:          "alice@example.com",
		EmailVerified:  true,
	}
	resolver := &fakeUserResolver{byEmail: &existing}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"github" /* attacker presents a different provider */, "github-456",
		"alice@example.com", "Alice", true)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errCrossProviderConflict),
		"expected errCrossProviderConflict, got %v", err)
	assert.Equal(t, userMatchCrossProviderConflict, match)
	// Critical: the existing user's UUID must NOT leak into the returned User.
	// Returning the bound record would let the caller mint a token for it.
	assert.Empty(t, user.InternalUUID, "must not return existing user on cross-provider conflict")
	assert.Equal(t, 0, resolver.createCalls)
}

// Tier 3 — unverified email match: MUST reject sparse-record bind when
// the upstream provider has not verified the email.
func TestFindOrCreateUser_UnverifiedEmailMatchRejected(t *testing.T) {
	sparse := User{
		InternalUUID: "uuid-sparse-alice",
		Provider:     "", // sparse
		Email:        "alice@example.com",
	}
	resolver := &fakeUserResolver{byEmail: &sparse}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"github", "github-456", "alice@example.com", "Alice", false /* email_verified=false */)

	require.Error(t, err)
	assert.True(t, errors.Is(err, errUnverifiedEmailMatch),
		"expected errUnverifiedEmailMatch, got %v", err)
	assert.Equal(t, userMatchNone, match)
	assert.Empty(t, user.InternalUUID, "must not return sparse record without verified email")
	assert.Equal(t, 0, resolver.createCalls)
}

// No-match path — when no tier matches, a fresh user is created.
func TestFindOrCreateUser_NoMatchCreatesUser(t *testing.T) {
	resolver := &fakeUserResolver{} // all lookups return not-found

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "newuser@example.com", "New User", true)

	require.NoError(t, err)
	assert.Equal(t, userMatchNone, match)
	assert.Equal(t, 1, resolver.createCalls, "expected exactly one CreateUser call")
	assert.Equal(t, "google", user.Provider)
	assert.Equal(t, "google-123", user.ProviderUserID)
	assert.Equal(t, "newuser@example.com", user.Email)
	assert.True(t, user.EmailVerified)
	assert.NotEmpty(t, user.InternalUUID)
}
