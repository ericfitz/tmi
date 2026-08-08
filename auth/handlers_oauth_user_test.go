package auth

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
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
	byLinkedIdentity   *models.LinkedIdentity // new: Tier 1b linked identity lookup
	byInternalUUID     *User                  // new: owner lookup for linked identity

	// providerIDErr, if set, is what GetUserByProviderID returns instead of
	// errFakeUserNotFound when byProviderID is nil — used to simulate a real
	// lookup failure (e.g. a transient DB error) rather than a not-found.
	providerIDErr error
	// linkedIdentityErr, if set, is what GetLinkedIdentityByProviderSub
	// returns instead of ErrLinkedIdentityNotFound when byLinkedIdentity is
	// nil — used to simulate a real tier 1b lookup failure (#718/#720).
	linkedIdentityErr error

	createdUser User
	createErr   error
	// afterCreateConflict, if set, is what GetUserByProviderID returns on any
	// call made after CreateUser has already been invoked at least once —
	// simulates the "lost the race, re-fetch the winner" path (#718).
	afterCreateConflict *User

	createCalls     int
	providerIDCalls int
}

var errFakeUserNotFound = errors.New("user not found")

func (f *fakeUserResolver) GetUserByProviderID(_ context.Context, _, _ string) (User, error) {
	f.providerIDCalls++
	if f.createCalls > 0 && f.afterCreateConflict != nil {
		return *f.afterCreateConflict, nil
	}
	if f.byProviderID == nil {
		if f.providerIDErr != nil {
			return User{}, f.providerIDErr
		}
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

func (f *fakeUserResolver) GetLinkedIdentityByProviderSub(_ context.Context, _, _ string) (models.LinkedIdentity, error) {
	if f.byLinkedIdentity == nil {
		if f.linkedIdentityErr != nil {
			return models.LinkedIdentity{}, f.linkedIdentityErr
		}
		return models.LinkedIdentity{}, ErrLinkedIdentityNotFound
	}
	return *f.byLinkedIdentity, nil
}

func (f *fakeUserResolver) GetUserByInternalUUID(_ context.Context, _ string) (User, error) {
	if f.byInternalUUID == nil {
		return User{}, errFakeUserNotFound
	}
	return *f.byInternalUUID, nil
}

func (f *fakeUserResolver) TouchLinkedIdentityLastUsed(_ context.Context, _ string) error {
	return nil
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

// Tier 1b — linked identity: resolves to owning user without creating a new record.
func TestFindOrCreateUser_ResolvesLinkedIdentity(t *testing.T) {
	owner := User{
		InternalUUID:   "uuid-alice-primary",
		Provider:       "google",
		ProviderUserID: "google-alice",
		Email:          "alice@example.com",
		EmailVerified:  true,
	}
	li := models.LinkedIdentity{
		ID:               models.DBVarchar("li-id-1"),
		UserInternalUUID: models.DBVarchar("uuid-alice-primary"),
		Provider:         models.DBVarchar("github"),
		ProviderUserID:   models.DBVarchar("gh-alice"),
	}
	// Primary lookup misses, linked identity hits
	resolver := &fakeUserResolver{
		byLinkedIdentity: &li,
		byInternalUUID:   &owner,
	}
	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"github", "gh-alice", "alice@github.com", "Alice", true)
	require.NoError(t, err)
	assert.Equal(t, userMatchLinkedIdentity, match)
	assert.Equal(t, owner.InternalUUID, user.InternalUUID)
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

// #718 — a tier-1 lookup failure that is NOT a not-found (e.g. a transient
// Oracle connection error) MUST abort the login attempt instead of being
// treated as "no match" and falling through to CreateUser. Before this fix,
// any error was conflated with not-found, which could silently duplicate a
// USERS row (pre-#701) or 500 on the new unique index (post-#701).
func TestFindOrCreateUser_OtherLookupErrorDoesNotCreate(t *testing.T) {
	lookupErr := errors.New("driver: bad connection")
	resolver := &fakeUserResolver{providerIDErr: lookupErr}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "alice@example.com", "Alice", true)

	require.Error(t, err)
	assert.True(t, errors.Is(err, lookupErr), "expected wrapped lookup error, got %v", err)
	assert.Equal(t, userMatchNone, match)
	assert.Empty(t, user.InternalUUID)
	assert.Equal(t, 0, resolver.createCalls,
		"must not attempt to create a user when a lookup fails for a reason other than not-found")
}

// #718 — CreateUser losing the race on the (provider, provider_user_id)
// unique index (idx_users_provider_lookup, #701) must re-fetch and return
// the winning row instead of surfacing a 500. Simulates two concurrent OAuth
// callbacks for the same brand-new identity (SPA double-submit / retry).
func TestFindOrCreateUser_DuplicateOnCreateRefetchesWinner(t *testing.T) {
	winner := User{
		InternalUUID:   "uuid-winner",
		Provider:       "google",
		ProviderUserID: "google-123",
		Email:          "alice@example.com",
		EmailVerified:  true,
	}
	dupErr := dberrors.Wrap(errors.New("ORA-00001: unique constraint violated"), dberrors.ErrDuplicate)
	resolver := &fakeUserResolver{
		createErr:           dupErr,
		afterCreateConflict: &winner,
	}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"google", "google-123", "alice@example.com", "Alice", true)

	require.NoError(t, err)
	assert.Equal(t, userMatchProviderID, match)
	assert.Equal(t, winner.InternalUUID, user.InternalUUID)
	assert.Equal(t, 1, resolver.createCalls)
}

// #718/#720 — same bug class as tiers 1-3, in tier 1b: a genuine
// GetLinkedIdentityByProviderSub failure (not ErrLinkedIdentityNotFound, e.g.
// an ADB idle-session kill) must abort the login attempt instead of being
// conflated with "no linked identity" and falling through to tiers 2/3/create.
func TestFindOrCreateUser_LinkedIdentityLookupErrorDoesNotCreate(t *testing.T) {
	lookupErr := errors.New("driver: bad connection")
	resolver := &fakeUserResolver{linkedIdentityErr: lookupErr}

	user, match, err := findOrCreateUserWithResolver(context.Background(), newTestGinContext(), resolver,
		"github", "gh-alice", "alice@example.com", "Alice", true)

	require.Error(t, err)
	assert.True(t, errors.Is(err, lookupErr), "expected wrapped lookup error, got %v", err)
	assert.Equal(t, userMatchNone, match)
	assert.Empty(t, user.InternalUUID)
	assert.Equal(t, 0, resolver.createCalls,
		"must not attempt to create a user when the tier 1b linked-identity lookup fails for a reason other than not-found")
}
