package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContentOAuthProvider_AuthorizationURL(t *testing.T) {
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", AuthURL: "https://auth.example/authorize",
		TokenURL:       "https://auth.example/token",
		RequiredScopes: []string{"read:a", "read:b"},
	})
	u := p.AuthorizationURL("state-123", "challenge-xyz", "https://tmi/cb")
	assert.Contains(t, u, "client_id=cid")
	assert.Contains(t, u, "state=state-123")
	assert.Contains(t, u, "code_challenge=challenge-xyz")
	assert.Contains(t, u, "code_challenge_method=S256")
	assert.Contains(t, u, "scope=read%3Aa+read%3Ab")
	assert.Contains(t, u, "redirect_uri=https%3A%2F%2Ftmi%2Fcb")
	assert.Contains(t, u, "response_type=code")
}

// TestContentOAuthProvider_AuthorizationURL_ExtraParams verifies that custom
// authorize-URL parameters configured via ExtraAuthorizeParams (used by
// Atlassian/Confluence to pass audience=api.atlassian.com) are appended to
// the URL alongside the standard parameters, and that standard parameters
// are not displaced when an extra collides on key.
func TestContentOAuthProvider_AuthorizationURL_ExtraParams(t *testing.T) {
	p := NewBaseContentOAuthProvider("confluence", config.ContentOAuthProviderConfig{
		ClientID:       "cid",
		AuthURL:        "https://auth.atlassian.com/authorize",
		TokenURL:       "https://auth.atlassian.com/oauth/token",
		RequiredScopes: []string{"read:confluence-content.all", "offline_access"},
		ExtraAuthorizeParams: map[string]string{
			"audience": "api.atlassian.com",
			"prompt":   "consent",
			// Collides with a standard param; should be overwritten.
			"client_id": "should-be-ignored",
		},
	})
	u := p.AuthorizationURL("state-1", "ch-1", "https://tmi/cb")
	assert.Contains(t, u, "audience=api.atlassian.com")
	assert.Contains(t, u, "prompt=consent")
	assert.Contains(t, u, "client_id=cid")
	assert.NotContains(t, u, "client_id=should-be-ignored")
	// Sanity: standard PKCE + state still present.
	assert.Contains(t, u, "code_challenge=ch-1")
	assert.Contains(t, u, "code_challenge_method=S256")
	assert.Contains(t, u, "state=state-1")
}

func TestContentOAuthProvider_ExchangeCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "authorization_code", r.FormValue("grant_type"))
		assert.Equal(t, "code-1", r.FormValue("code"))
		assert.Equal(t, "pkce-v", r.FormValue("code_verifier"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-1",
			"refresh_token": "rt-1",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"scope":         "read:a",
		})
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec",
		AuthURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token",
	})
	tok, err := p.ExchangeCode(context.Background(), "code-1", "pkce-v", srv.URL+"/cb")
	require.NoError(t, err)
	assert.Equal(t, "at-1", tok.AccessToken)
	assert.Equal(t, "rt-1", tok.RefreshToken)
	assert.Equal(t, 3600, tok.ExpiresIn)
}

func TestContentOAuthProvider_Refresh(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "refresh_token", r.FormValue("grant_type"))
		assert.Equal(t, "rt-old", r.FormValue("refresh_token"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "at-new",
			"refresh_token": "rt-new",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", TokenURL: srv.URL,
	})
	tok, err := p.Refresh(context.Background(), "rt-old")
	require.NoError(t, err)
	assert.Equal(t, "at-new", tok.AccessToken)
	assert.Equal(t, "rt-new", tok.RefreshToken)
}

func TestContentOAuthProvider_Refresh_InvalidGrantIsPermanent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", TokenURL: srv.URL,
	})
	_, err := p.Refresh(context.Background(), "bad")
	require.Error(t, err)
	assert.True(t, IsContentOAuthPermanentFailure(err))
}

func TestContentOAuthProvider_Revoke_Succeeds(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		require.NoError(t, r.ParseForm())
		assert.Equal(t, "tok-1", r.FormValue("token"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{
		ClientID: "cid", ClientSecret: "sec", RevocationURL: srv.URL,
	})
	err := p.Revoke(context.Background(), "tok-1")
	require.NoError(t, err)
	assert.True(t, called)
}

func TestContentOAuthProvider_Revoke_NoURLIsNoop(t *testing.T) {
	p := NewBaseContentOAuthProvider("mock", config.ContentOAuthProviderConfig{})
	assert.NoError(t, p.Revoke(context.Background(), "tok-1"))
}
