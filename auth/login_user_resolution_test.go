package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/ericfitz/tmi/auth/saml"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// creatingUserRepo is an empty user table that records creates: every lookup
// misses, and Create assigns a fresh internal UUID.
type creatingUserRepo struct {
	stubUserRepo
	created []repository.User
}

func (r *creatingUserRepo) Create(_ context.Context, u *repository.User) (*repository.User, error) {
	out := *u
	out.InternalUUID = uuid.NewString()
	r.created = append(r.created, out)
	return &out, nil
}

// staleCachedUser seeds the Redis user cache (by ID, email and provider) with
// a user whose row does not exist in the database: the state after a database
// restore or reset while Redis keeps its keys (#1162).
func staleCachedUser(t *testing.T, svc *Service, provider, providerUserID, email string) User {
	t.Helper()
	stale := User{
		InternalUUID:   uuid.NewString(),
		Provider:       provider,
		ProviderUserID: providerUserID,
		Email:          email,
		Name:           "Stale",
		EmailVerified:  true,
	}
	require.NoError(t, svc.CacheUser(context.Background(), stale))
	return stale
}

// Login decides whether to create a user, so a cached user whose row is gone
// must not count as a match: the login must create the row (#1162).
func TestFindOrCreateUser_StaleCacheEntryDoesNotSkipCreate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &creatingUserRepo{}
	svc, cleanup := setupTestServiceWithRepos(t, repo, nil)
	defer cleanup()
	stale := staleCachedUser(t, svc, "tmi", "charlie", "charlie@tmi.local")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/oauth2/callback", nil)
	h := &Handlers{service: svc}
	user, match, err := h.findOrCreateUser(context.Background(), c, "tmi", "charlie", "charlie@tmi.local", "Charlie", true)
	require.NoError(t, err)

	assert.Equal(t, userMatchNone, match, "a cache-only user must not be reported as an existing match")
	require.Len(t, repo.created, 1, "the login must create the missing user row")
	assert.NotEqual(t, stale.InternalUUID, user.InternalUUID)
	assert.Equal(t, repo.created[0].InternalUUID, user.InternalUUID)
}

// The SAML login path shares the tiered matching and the same hazard (#1162).
func TestProcessSAMLUser_StaleCacheEntryDoesNotSkipCreate(t *testing.T) {
	repo := &creatingUserRepo{}
	svc, cleanup := setupTestServiceWithRepos(t, repo, nil)
	defer cleanup()
	stale := staleCachedUser(t, svc, "saml_test", "saml-sub-1", "dana@example.com")

	m := &SAMLManager{service: svc}
	user, err := m.processUser(context.Background(), &saml.UserInfo{
		ID:            "saml-sub-1",
		Email:         "dana@example.com",
		EmailVerified: true,
		Name:          "Dana",
	}, "saml_test")
	require.NoError(t, err)
	require.NotNil(t, user)

	require.Len(t, repo.created, 1, "the SAML login must create the missing user row")
	assert.NotEqual(t, stale.InternalUUID, user.InternalUUID)
}

// Each lookup the login resolver serves from the cache in the general path
// must read the database instead (#1162).
func TestLoginResolver_IgnoresStaleCache(t *testing.T) {
	repo := &creatingUserRepo{}
	svc, cleanup := setupTestServiceWithRepos(t, repo, nil)
	defer cleanup()
	stale := staleCachedUser(t, svc, "tmi", "erin", "erin@tmi.local")
	ctx := context.Background()
	r := svc.loginResolver()

	_, err := r.GetUserByProviderID(ctx, "tmi", "erin")
	assert.ErrorIs(t, err, ErrUserNotFound, "provider lookup")
	_, err = r.GetUserByEmail(ctx, "erin@tmi.local")
	assert.ErrorIs(t, err, ErrUserNotFound, "email lookup")
	_, err = r.GetUserByInternalUUID(ctx, stale.InternalUUID)
	assert.ErrorIs(t, err, ErrUserNotFound, "internal UUID lookup")

	// The general, cached path is unchanged.
	cached, err := svc.GetUserByProviderID(ctx, "tmi", "erin")
	require.NoError(t, err)
	assert.Equal(t, stale.InternalUUID, cached.InternalUUID)
}

// The identity-link "already bound" guard reads the users table; a stale
// cache entry must not make an unbound identity look bound (#1162).
func TestIsProviderSubAlreadyBound_IgnoresStaleCache(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, &creatingUserRepo{}, nil)
	defer cleanup()
	staleCachedUser(t, svc, "github", "gh-42", "frank@example.com")

	h := &Handlers{service: svc}
	assert.False(t, h.isProviderSubAlreadyBound(context.Background(), "github", "gh-42"))
}
