package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock ContentTokenRepository
// =============================================================================

type mockContentTokenRepo struct {
	getByUserAndProvider func(ctx context.Context, userID, providerID string) (*ContentToken, error)
	listByUser           func(ctx context.Context, userID string) ([]ContentToken, error)
	upsert               func(ctx context.Context, t *ContentToken) error
	updateStatus         func(ctx context.Context, id, status, lastError string) error
	deleteByID           func(ctx context.Context, id string) error
	deleteByUserAndProv  func(ctx context.Context, userID, providerID string) (*ContentToken, error)
	refreshWithLock      func(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error)
}

func (m *mockContentTokenRepo) GetByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	if m.getByUserAndProvider != nil {
		return m.getByUserAndProvider(ctx, userID, providerID)
	}
	return nil, ErrContentTokenNotFound
}

func (m *mockContentTokenRepo) ListByUser(ctx context.Context, userID string) ([]ContentToken, error) {
	if m.listByUser != nil {
		return m.listByUser(ctx, userID)
	}
	return []ContentToken{}, nil
}

func (m *mockContentTokenRepo) Upsert(ctx context.Context, t *ContentToken) error {
	if m.upsert != nil {
		return m.upsert(ctx, t)
	}
	return nil
}

func (m *mockContentTokenRepo) UpdateStatus(ctx context.Context, id, status, lastError string) error {
	if m.updateStatus != nil {
		return m.updateStatus(ctx, id, status, lastError)
	}
	return nil
}

func (m *mockContentTokenRepo) Delete(ctx context.Context, id string) error {
	if m.deleteByID != nil {
		return m.deleteByID(ctx, id)
	}
	return nil
}

func (m *mockContentTokenRepo) DeleteByUserAndProvider(ctx context.Context, userID, providerID string) (*ContentToken, error) {
	if m.deleteByUserAndProv != nil {
		return m.deleteByUserAndProv(ctx, userID, providerID)
	}
	return nil, ErrContentTokenNotFound
}

func (m *mockContentTokenRepo) RefreshWithLock(ctx context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
	if m.refreshWithLock != nil {
		return m.refreshWithLock(ctx, id, fn)
	}
	return nil, ErrContentTokenNotFound
}

// =============================================================================
// Stub ContentOAuthProvider
// =============================================================================

type stubContentOAuthProvider struct {
	id            string
	authURL       string
	exchangeResp  *ContentOAuthTokenResponse
	exchangeErr   error
	revokeErr     error
	revokeCalls   int
	userinfoID    string
	userinfoLabel string
	userinfoErr   error
}

func (s *stubContentOAuthProvider) ID() string { return s.id }

func (s *stubContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	q := url.Values{}
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge)
	q.Set("redirect_uri", redirectURI)
	sep := "?"
	if strings.Contains(s.authURL, "?") {
		sep = "&"
	}
	return s.authURL + sep + q.Encode()
}

func (s *stubContentOAuthProvider) ExchangeCode(_ context.Context, _, _, _ string) (*ContentOAuthTokenResponse, error) {
	return s.exchangeResp, s.exchangeErr
}

func (s *stubContentOAuthProvider) Refresh(_ context.Context, _ string) (*ContentOAuthTokenResponse, error) {
	return nil, errors.New("not implemented in stub")
}

func (s *stubContentOAuthProvider) Revoke(_ context.Context, _ string) error {
	s.revokeCalls++
	return s.revokeErr
}

func (s *stubContentOAuthProvider) RequiredScopes() []string { return []string{"read"} }

func (s *stubContentOAuthProvider) FetchAccountInfo(_ context.Context, _ string) (string, string, error) {
	return s.userinfoID, s.userinfoLabel, s.userinfoErr
}

// =============================================================================
// Test helpers
// =============================================================================

const testUserID = "user-uuid-1234"

// newTestHandlers creates a ContentOAuthHandlers with a miniredis-backed state store,
// a mock repo, a fresh registry with the given provider registered, and an allow-list
// that permits "http://localhost:8079/".
func newTestHandlers(t *testing.T, repo ContentTokenRepository, provider ContentOAuthProvider) (*ContentOAuthHandlers, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	stateStore := NewContentOAuthStateStore(client)

	registry := NewContentOAuthProviderRegistry()
	if provider != nil {
		registry.Register(provider)
	}

	allowList := NewClientCallbackAllowList([]string{"http://localhost:8079/", "http://localhost:4200/*"})

	cfg := config.ContentOAuthConfig{
		CallbackURL:            "http://tmi.local/oauth2/content_callback",
		AllowedClientCallbacks: []string{"http://localhost:8079/", "http://localhost:4200/*"},
	}

	h := &ContentOAuthHandlers{
		Cfg:           cfg,
		Registry:      registry,
		StateStore:    stateStore,
		Tokens:        repo,
		CallbackAllow: allowList,
		UserLookup: func(c *gin.Context) (string, bool) {
			val, exists := c.Get("userInternalUUID")
			if !exists {
				return "", false
			}
			uid, ok := val.(string)
			return uid, ok
		},
	}
	return h, mr
}

// setUserContext injects the test userID into the gin context.
func setUserContext(c *gin.Context, userID string) {
	c.Set("userInternalUUID", userID)
}

// ginTestRouter builds a gin engine in test mode.
func ginTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// =============================================================================
// Task 3.4 — List tests
// =============================================================================

func TestContentOAuthHandlers_List_Empty(t *testing.T) {
	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return []ContentToken{}, nil
		},
	}
	h, _ := newTestHandlers(t, repo, nil)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.GET("/me/content_tokens", h.List)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me/content_tokens", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		ContentTokens []contentTokenInfo `json:"content_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Empty(t, body.ContentTokens)
}

func TestContentOAuthHandlers_List_ReturnsTokens(t *testing.T) {
	now := time.Now()
	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return []ContentToken{
				{
					ProviderID:           "confluence",
					ProviderAccountID:    "acc-1",
					ProviderAccountLabel: "alice@example.com",
					Scopes:               "read write",
					Status:               ContentTokenStatusActive,
					AccessToken:          "SECRET_SHOULD_NOT_APPEAR",
					RefreshToken:         "REFRESH_SHOULD_NOT_APPEAR",
					ExpiresAt:            &now,
					CreatedAt:            now,
				},
			}, nil
		},
	}
	h, _ := newTestHandlers(t, repo, nil)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.GET("/me/content_tokens", h.List)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me/content_tokens", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	bodyStr := w.Body.String()
	// No secrets in response body
	assert.NotContains(t, bodyStr, "SECRET_SHOULD_NOT_APPEAR")
	assert.NotContains(t, bodyStr, "REFRESH_SHOULD_NOT_APPEAR")

	var body struct {
		ContentTokens []contentTokenInfo `json:"content_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.ContentTokens, 1)
	assert.Equal(t, "confluence", body.ContentTokens[0].ProviderID)
	assert.Equal(t, "alice@example.com", body.ContentTokens[0].ProviderAccountLabel)
	assert.Equal(t, []string{"read", "write"}, body.ContentTokens[0].Scopes)
	assert.Equal(t, ContentTokenStatusActive, body.ContentTokens[0].Status)
}

func TestContentOAuthHandlers_List_Unauthorized(t *testing.T) {
	h, _ := newTestHandlers(t, &mockContentTokenRepo{}, nil)
	r := ginTestRouter()
	// No user context set
	r.GET("/me/content_tokens", h.List)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/me/content_tokens", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// =============================================================================
// Task 3.4 — Authorize tests
// =============================================================================

func TestContentOAuthHandlers_Authorize_HappyPath(t *testing.T) {
	provider := &stubContentOAuthProvider{
		id:      "mock",
		authURL: "https://auth.example.com/authorize",
	}
	h, mr := newTestHandlers(t, &mockContentTokenRepo{}, provider)
	_ = mr
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.POST("/me/content_tokens/:provider_id/authorize", h.Authorize)

	body := `{"client_callback":"http://localhost:8079/"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/content_tokens/mock/authorize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var resp authorizeResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.NotEmpty(t, resp.AuthorizationURL)
	assert.Contains(t, resp.AuthorizationURL, "https://auth.example.com/authorize")
	assert.Contains(t, resp.AuthorizationURL, "state=")
	assert.Contains(t, resp.AuthorizationURL, "code_challenge=")
	assert.Contains(t, resp.AuthorizationURL, "redirect_uri=")

	// expires_at should be approximately 10 minutes from now
	delta := time.Until(resp.ExpiresAt)
	assert.Greater(t, delta, 9*time.Minute)
	assert.Less(t, delta, 11*time.Minute)
}

func TestContentOAuthHandlers_Authorize_ProviderNotRegistered(t *testing.T) {
	h, _ := newTestHandlers(t, &mockContentTokenRepo{}, nil) // no provider registered
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.POST("/me/content_tokens/:provider_id/authorize", h.Authorize)

	body := `{"client_callback":"http://localhost:8079/"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/content_tokens/unknown/authorize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "content_token_provider_not_configured", resp["error"])
}

func TestContentOAuthHandlers_Authorize_ClientCallbackRejected(t *testing.T) {
	provider := &stubContentOAuthProvider{id: "mock", authURL: "https://auth.example.com/authorize"}
	h, _ := newTestHandlers(t, &mockContentTokenRepo{}, provider)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.POST("/me/content_tokens/:provider_id/authorize", h.Authorize)

	body := `{"client_callback":"http://evil.com/callback"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/content_tokens/mock/authorize", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "client_callback_not_allowed", resp["error"])
}

// =============================================================================
// Task 3.4 — Delete tests
// =============================================================================

func TestContentOAuthHandlers_Delete_HappyPath(t *testing.T) {
	accessToken := "at-to-revoke"
	provider := &stubContentOAuthProvider{id: "mock", authURL: "https://auth.example.com/authorize"}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			return &ContentToken{
				UserID:      userID,
				ProviderID:  providerID,
				AccessToken: accessToken,
				Status:      ContentTokenStatusActive,
			}, nil
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/mock", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, provider.revokeCalls, "provider.Revoke should have been called once")
}

func TestContentOAuthHandlers_Delete_NoRow_Idempotent(t *testing.T) {
	provider := &stubContentOAuthProvider{id: "mock", authURL: "https://auth.example.com/authorize"}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/mock", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 0, provider.revokeCalls, "provider.Revoke should NOT have been called")
}

func TestContentOAuthHandlers_Delete_RevokeFailure_Still204(t *testing.T) {
	provider := &stubContentOAuthProvider{
		id:        "mock",
		authURL:   "https://auth.example.com/authorize",
		revokeErr: errors.New("provider unavailable"),
	}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, userID, providerID string) (*ContentToken, error) {
			return &ContentToken{
				UserID:      userID,
				ProviderID:  providerID,
				AccessToken: "at-1",
				Status:      ContentTokenStatusActive,
			}, nil
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.DELETE("/me/content_tokens/:provider_id", h.Delete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/me/content_tokens/mock", nil)
	r.ServeHTTP(w, req)

	// Should still be 204 even though revocation failed
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, provider.revokeCalls, "provider.Revoke should have been attempted")
}

// =============================================================================
// Task 3.5 — Callback tests
// =============================================================================

// buildCallbackURL builds a URL for the callback handler with the given query params.
func buildCallbackURL(params url.Values) string {
	return "/oauth2/content_callback?" + params.Encode()
}

// seedState stores an OAuth state in the state store and returns the nonce.
func seedState(t *testing.T, h *ContentOAuthHandlers, userID, providerID, clientCallback string) string {
	t.Helper()
	verifier, err := NewPKCEVerifier()
	require.NoError(t, err)
	payload := ContentOAuthStatePayload{
		UserID:           userID,
		ProviderID:       providerID,
		ClientCallback:   clientCallback,
		PKCECodeVerifier: verifier,
		CreatedAt:        time.Now(),
	}
	nonce, err := h.StateStore.Put(context.Background(), payload, 10*time.Minute)
	require.NoError(t, err)
	return nonce
}

func TestContentOAuthHandlers_Callback_HappyPath_PersistsToken(t *testing.T) {
	var storedToken *ContentToken
	provider := &stubContentOAuthProvider{
		id:      "mock",
		authURL: "https://auth.example.com/authorize",
		exchangeResp: &ContentOAuthTokenResponse{
			AccessToken:  "at-from-exchange",
			RefreshToken: "rt-from-exchange",
			ExpiresIn:    3600,
			Scope:        "read write",
		},
		userinfoID:    "account-abc",
		userinfoLabel: "alice@example.com",
	}
	repo := &mockContentTokenRepo{
		upsert: func(_ context.Context, t *ContentToken) error {
			storedToken = t
			return nil
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	params.Set("code", "auth-code-1")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusFound, w.Code)

	location := w.Header().Get("Location")
	assert.Contains(t, location, "status=success")
	assert.Contains(t, location, "provider_id=mock")

	// Token was stored
	require.NotNil(t, storedToken)
	assert.Equal(t, testUserID, storedToken.UserID)
	assert.Equal(t, "mock", storedToken.ProviderID)
	assert.Equal(t, "at-from-exchange", storedToken.AccessToken)
	assert.Equal(t, "rt-from-exchange", storedToken.RefreshToken)
	assert.Equal(t, ContentTokenStatusActive, storedToken.Status)
	assert.Equal(t, "account-abc", storedToken.ProviderAccountID)
	assert.Equal(t, "alice@example.com", storedToken.ProviderAccountLabel)
}

func TestContentOAuthHandlers_Callback_InvalidState_RendersError(t *testing.T) {
	h, _ := newTestHandlers(t, &mockContentTokenRepo{}, nil)
	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	// No state param
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/oauth2/content_callback", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "missing_state")
}

func TestContentOAuthHandlers_Callback_ExpiredState_RendersError(t *testing.T) {
	h, mr := newTestHandlers(t, &mockContentTokenRepo{}, nil)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")
	// Fast-forward past TTL
	mr.FastForward(11 * time.Minute)

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_state")
}

func TestContentOAuthHandlers_Callback_ProviderDenial_RedirectsError(t *testing.T) {
	provider := &stubContentOAuthProvider{id: "mock", authURL: "https://auth.example.com/authorize"}
	h, _ := newTestHandlers(t, &mockContentTokenRepo{}, provider)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	params.Set("error", "access_denied")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	assert.Contains(t, location, "status=error")
	assert.Contains(t, location, "error=access_denied")
	assert.Contains(t, location, "provider_id=mock")
}

func TestContentOAuthHandlers_Callback_TokenExchangeFails_Redirects(t *testing.T) {
	var upsertCalled bool
	provider := &stubContentOAuthProvider{
		id:          "mock",
		authURL:     "https://auth.example.com/authorize",
		exchangeErr: errors.New("token endpoint error"),
	}
	repo := &mockContentTokenRepo{
		upsert: func(_ context.Context, _ *ContentToken) error {
			upsertCalled = true
			return nil
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	params.Set("code", "bad-code")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	assert.Contains(t, location, "status=error")
	assert.Contains(t, location, "error=token_exchange_failed")
	assert.False(t, upsertCalled, "no token should be stored on exchange failure")
}

func TestContentOAuthHandlers_Callback_PersistFailure_Redirects(t *testing.T) {
	provider := &stubContentOAuthProvider{
		id:      "mock",
		authURL: "https://auth.example.com/authorize",
		exchangeResp: &ContentOAuthTokenResponse{
			AccessToken: "at-1",
			ExpiresIn:   3600,
			Scope:       "read",
		},
	}
	repo := &mockContentTokenRepo{
		upsert: func(_ context.Context, _ *ContentToken) error {
			return errors.New("database down")
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	params.Set("code", "ok-code")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	assert.Contains(t, location, "status=error")
	assert.Contains(t, location, "error=persist_failed")
}

func TestContentOAuthHandlers_Callback_FetchAccountInfoFailure_DoesNotBlock(t *testing.T) {
	var storedToken *ContentToken
	provider := &stubContentOAuthProvider{
		id:      "mock",
		authURL: "https://auth.example.com/authorize",
		exchangeResp: &ContentOAuthTokenResponse{
			AccessToken: "at-1",
			ExpiresIn:   3600,
			Scope:       "read",
		},
		userinfoErr: errors.New("userinfo endpoint unavailable"),
	}
	repo := &mockContentTokenRepo{
		upsert: func(_ context.Context, t *ContentToken) error {
			storedToken = t
			return nil
		},
	}
	h, _ := newTestHandlers(t, repo, provider)
	nonce := seedState(t, h, testUserID, "mock", "http://localhost:8079/")

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", nonce)
	params.Set("code", "ok-code")
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil)
	r.ServeHTTP(w, req)

	// Should succeed even though FetchAccountInfo failed
	assert.Equal(t, http.StatusFound, w.Code)
	location := w.Header().Get("Location")
	assert.Contains(t, location, "status=success")

	require.NotNil(t, storedToken)
	// account info should be empty (fetch failed)
	assert.Empty(t, storedToken.ProviderAccountID)
	assert.Empty(t, storedToken.ProviderAccountLabel)
}

// =============================================================================
// Helpers for test HTTP bodies
// =============================================================================

func mustMarshal(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return bytes.NewBuffer(b)
}
