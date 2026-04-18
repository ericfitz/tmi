//go:build dev || test

package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Helper — minimal stub ContentOAuthProvider for mock delegated source tests
// =============================================================================

// mockDelegatedTestProvider is a minimal ContentOAuthProvider that always
// succeeds on Refresh, returning a new access token. It is used to keep the
// MockDelegatedSource tests self-contained.
type mockDelegatedTestProvider struct{}

func (mockDelegatedTestProvider) ID() string                                       { return "mock" }
func (mockDelegatedTestProvider) AuthorizationURL(_, _, _ string) string           { return "" }
func (mockDelegatedTestProvider) RequiredScopes() []string                         { return nil }
func (mockDelegatedTestProvider) Revoke(_ context.Context, _ string) error         { return nil }
func (mockDelegatedTestProvider) FetchAccountInfo(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}
func (mockDelegatedTestProvider) ExchangeCode(_ context.Context, _, _, _ string) (*ContentOAuthTokenResponse, error) {
	return &ContentOAuthTokenResponse{AccessToken: "exchange-at", RefreshToken: "exchange-rt", ExpiresIn: 3600}, nil
}
func (mockDelegatedTestProvider) Refresh(_ context.Context, _ string) (*ContentOAuthTokenResponse, error) {
	exp := int(time.Hour / time.Second)
	return &ContentOAuthTokenResponse{
		AccessToken:  "refreshed-access-token",
		RefreshToken: "refreshed-refresh-token",
		ExpiresIn:    exp,
	}, nil
}

// newTestMockDelegated creates a fully wired MockDelegatedSource backed by an
// in-memory SQLite token repository and a registry containing the mock provider.
func newTestMockDelegated(t *testing.T) (*MockDelegatedSource, ContentTokenRepository) {
	t.Helper()
	repo, _, _ := newTestContentTokenRepo(t)
	registry := NewContentOAuthProviderRegistry()
	registry.Register(mockDelegatedTestProvider{})
	mock := NewMockDelegatedSource(repo, registry)
	return mock, repo
}

// =============================================================================
// Tests
// =============================================================================

func TestMockDelegatedSource_CanHandle_MockURI(t *testing.T) {
	mock, _ := newTestMockDelegated(t)

	assert.True(t, mock.CanHandle(context.Background(), "mock://doc/123"))
	assert.True(t, mock.CanHandle(context.Background(), "mock://doc/"))
	assert.False(t, mock.CanHandle(context.Background(), "https://example.com/doc"))
	assert.False(t, mock.CanHandle(context.Background(), "confluence://page/1"))
	assert.False(t, mock.CanHandle(context.Background(), ""))
}

func TestMockDelegatedSource_Name(t *testing.T) {
	mock, _ := newTestMockDelegated(t)
	assert.Equal(t, "mock", mock.Name())
}

func TestMockDelegatedSource_Fetch_HappyPath(t *testing.T) {
	mock, repo := newTestMockDelegated(t)
	ctx := context.Background()

	// Pre-populate a valid, non-expired token for user "u1".
	exp := time.Now().Add(time.Hour)
	tok := &ContentToken{
		UserID:       "u1",
		ProviderID:   "mock",
		AccessToken:  "valid-access-token",
		RefreshToken: "valid-refresh-token",
		Status:       ContentTokenStatusActive,
		ExpiresAt:    &exp,
	}
	require.NoError(t, repo.Upsert(ctx, tok))

	// Register a document.
	mock.Contents["doc1"] = []byte("hello from mock")

	// Fetch via the context-aware Fetch method.
	userCtx := WithUserID(ctx, "u1")
	data, contentType, err := mock.Fetch(userCtx, "mock://doc/doc1")

	require.NoError(t, err)
	assert.Equal(t, []byte("hello from mock"), data)
	assert.Equal(t, "text/plain", contentType)
}

func TestMockDelegatedSource_Fetch_NoUserInContext(t *testing.T) {
	mock, _ := newTestMockDelegated(t)
	mock.Contents["doc1"] = []byte("data")

	// Context with no user ID.
	_, _, err := mock.Fetch(context.Background(), "mock://doc/doc1")

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "no user in context"),
		"expected 'no user in context' in error, got: %s", err.Error())
}

func TestMockDelegatedSource_Fetch_MissingContent_ReturnsError(t *testing.T) {
	mock, repo := newTestMockDelegated(t)
	ctx := context.Background()

	exp := time.Now().Add(time.Hour)
	require.NoError(t, repo.Upsert(ctx, &ContentToken{
		UserID:       "u2",
		ProviderID:   "mock",
		AccessToken:  "at",
		RefreshToken: "rt",
		Status:       ContentTokenStatusActive,
		ExpiresAt:    &exp,
	}))
	// No document in Contents — doFetch should return not-found error.
	userCtx := WithUserID(ctx, "u2")
	_, _, err := mock.Fetch(userCtx, "mock://doc/nonexistent")

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "not found"),
		"expected 'not found' in error, got: %s", err.Error())
}

func TestMockDelegatedSource_Fetch_NoToken_ReturnsAuthRequired(t *testing.T) {
	mock, _ := newTestMockDelegated(t)
	mock.Contents["doc1"] = []byte("data")

	// User "u3" has no token in the repository.
	userCtx := WithUserID(context.Background(), "u3")
	_, _, err := mock.Fetch(userCtx, "mock://doc/doc1")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrAuthRequired,
		"expected ErrAuthRequired when no token exists for user")
}

func TestMockDelegatedSource_FetchForUser_RefreshesExpiredToken(t *testing.T) {
	mock, repo := newTestMockDelegated(t)
	ctx := context.Background()

	// Insert an expired token.
	expired := time.Now().Add(-time.Hour)
	require.NoError(t, repo.Upsert(ctx, &ContentToken{
		UserID:       "u4",
		ProviderID:   "mock",
		AccessToken:  "old-at",
		RefreshToken: "old-rt",
		Status:       ContentTokenStatusActive,
		ExpiresAt:    &expired,
	}))
	mock.Contents["docX"] = []byte("refreshed content")

	// FetchForUser should trigger a lazy refresh via the mock provider.
	data, ct, err := mock.FetchForUser(ctx, "u4", "mock://doc/docX")

	require.NoError(t, err)
	assert.Equal(t, []byte("refreshed content"), data)
	assert.Equal(t, "text/plain", ct)

	// Verify the token was updated in the repository.
	updated, err := repo.GetByUserAndProvider(ctx, "u4", "mock")
	require.NoError(t, err)
	assert.Equal(t, "refreshed-access-token", updated.AccessToken,
		"repository should hold the refreshed access token")
}
