//go:build dev || test || integration

package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/testhelpers"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildStubProviderConfig returns a ContentOAuthProviderConfig wired to all
// endpoints of the given stub server. Used by multiple tests in this file.
func buildStubProviderConfig(stub *testhelpers.StubOAuthProvider) config.ContentOAuthProviderConfig {
	return config.ContentOAuthProviderConfig{
		ClientID:       "test-cid",
		ClientSecret:   "test-sec",
		AuthURL:        stub.AuthURL(),
		TokenURL:       stub.TokenURL(),
		RevocationURL:  stub.RevokeURL(),
		UserinfoURL:    stub.UserinfoURL(),
		RequiredScopes: []string{"read"},
	}
}

// TestRevokeUserTokens_RevokesAllTokensForUser verifies that RevokeUserTokens
// calls provider-side revocation for every token belonging to the user and
// never returns an error (best-effort contract).
func TestRevokeUserTokens_RevokesAllTokensForUser(t *testing.T) {
	stub := testhelpers.NewStubOAuthProvider(t)

	// Two tokens for the same user, two different providers.
	const (
		userID    = "user-delete-test-uuid"
		providerA = "mock-a"
		providerB = "mock-b"
		tokenA    = "access-token-a"
		tokenB    = "access-token-b"
	)

	tokens := []ContentToken{
		{ID: "id-a", UserID: userID, ProviderID: providerA, AccessToken: tokenA},
		{ID: "id-b", UserID: userID, ProviderID: providerB, AccessToken: tokenB},
	}

	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, id string) ([]ContentToken, error) {
			require.Equal(t, userID, id)
			return tokens, nil
		},
	}

	// Build a registry with both providers pointing at the same stub.
	providerACfg := buildStubProviderConfig(stub)
	providerBCfg := buildStubProviderConfig(stub)
	registry := NewContentOAuthProviderRegistry()
	registry.Register(NewBaseContentOAuthProvider(providerA, providerACfg))
	registry.Register(NewBaseContentOAuthProvider(providerB, providerBCfg))

	h := &ContentOAuthHandlers{
		Tokens:   repo,
		Registry: registry,
	}

	// RevokeUserTokens must not return an error.
	h.RevokeUserTokens(context.Background(), userID)

	// The stub should have received two revoke calls.
	assert.Equal(t, 2, stub.RevokeCalls(), "expected two provider-side revoke calls")

	// Both tokens should appear in the revoked list.
	revoked := stub.RevokedTokens()
	assert.Contains(t, revoked, tokenA, "tokenA should be revoked")
	assert.Contains(t, revoked, tokenB, "tokenB should be revoked")
}

// TestRevokeUserTokens_ProviderRevokeFails_DoesNotBlock verifies that
// RevokeUserTokens is truly best-effort: if a provider revocation fails,
// the method still processes all remaining tokens and does not return an error.
func TestRevokeUserTokens_ProviderRevokeFails_DoesNotBlock(t *testing.T) {
	stub := testhelpers.NewStubOAuthProvider(t)
	stub.RevokeSucceeds = false // provider side returns 500 for every revoke call

	const (
		userID    = "user-fail-test-uuid"
		providerA = "mock-fail"
		tokenA    = "access-token-fail"
	)

	tokens := []ContentToken{
		{ID: "id-fail", UserID: userID, ProviderID: providerA, AccessToken: tokenA},
	}

	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return tokens, nil
		},
	}

	providerCfg := buildStubProviderConfig(stub)
	registry := NewContentOAuthProviderRegistry()
	registry.Register(NewBaseContentOAuthProvider(providerA, providerCfg))

	h := &ContentOAuthHandlers{
		Tokens:   repo,
		Registry: registry,
	}

	// Must not panic or return error even though revocation fails.
	h.RevokeUserTokens(context.Background(), userID)
	// Stub still received the call; failure was logged internally.
	assert.Equal(t, 1, stub.RevokeCalls())
}

// TestRevokeUserTokens_NoTokens_IsNoop verifies that RevokeUserTokens handles
// users with no tokens gracefully.
func TestRevokeUserTokens_NoTokens_IsNoop(t *testing.T) {
	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return []ContentToken{}, nil
		},
	}

	h := &ContentOAuthHandlers{
		Tokens:   repo,
		Registry: NewContentOAuthProviderRegistry(),
	}

	// Should not panic.
	h.RevokeUserTokens(context.Background(), "no-tokens-user")
}

// TestRevokeUserTokens_ListError_IsNoop verifies that a ListByUser error is
// absorbed and does not cause a panic or propagate.
func TestRevokeUserTokens_ListError_IsNoop(t *testing.T) {
	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}

	h := &ContentOAuthHandlers{
		Tokens:   repo,
		Registry: NewContentOAuthProviderRegistry(),
	}

	// Should not panic even when list fails.
	h.RevokeUserTokens(context.Background(), "list-error-user")
}

// TestRevokeUserTokens_UnknownProvider_IsSkipped verifies that tokens for
// providers not present in the registry are silently skipped (provider not
// configured locally — nothing to revoke).
func TestRevokeUserTokens_UnknownProvider_IsSkipped(t *testing.T) {
	tokens := []ContentToken{
		{ID: "id-unknown", UserID: "user-xyz", ProviderID: "unknown-provider", AccessToken: "tok"},
	}

	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return tokens, nil
		},
	}

	// Empty registry — no providers registered.
	h := &ContentOAuthHandlers{
		Tokens:   repo,
		Registry: NewContentOAuthProviderRegistry(),
	}

	// Should not panic.
	h.RevokeUserTokens(context.Background(), "user-xyz")
}
