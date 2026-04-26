package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Stub refreshable provider (supports Refresh unlike stubContentOAuthProvider)
// =============================================================================

type stubRefreshableProvider struct {
	id          string
	refreshResp *ContentOAuthTokenResponse
	refreshErr  error
}

func (s *stubRefreshableProvider) ID() string                             { return s.id }
func (s *stubRefreshableProvider) AuthorizationURL(_, _, _ string) string { return "" }
func (s *stubRefreshableProvider) ExchangeCode(_ context.Context, _, _, _ string) (*ContentOAuthTokenResponse, error) {
	return nil, errors.New("not used")
}
func (s *stubRefreshableProvider) Refresh(_ context.Context, _ string) (*ContentOAuthTokenResponse, error) {
	return s.refreshResp, s.refreshErr
}
func (s *stubRefreshableProvider) Revoke(_ context.Context, _ string) error { return nil }
func (s *stubRefreshableProvider) RequiredScopes() []string                 { return nil }
func (s *stubRefreshableProvider) FetchAccountInfo(_ context.Context, _ string) (string, string, error) {
	return "", "", nil
}

// =============================================================================
// Test helper
// =============================================================================

const pickerProviderID = "google_workspace"

// newPickerTestHandler builds a PickerTokenHandler wired to the given repo,
// registry, and picker config. The UserLookup reads "userInternalUUID" from
// the gin context (same pattern as ContentOAuthHandlers tests).
func newPickerTestHandler(
	repo ContentTokenRepository,
	registry *ContentOAuthProviderRegistry,
	configs map[string]PickerTokenConfig,
) *PickerTokenHandler {
	return NewPickerTokenHandler(
		repo,
		registry,
		configs,
		func(c *gin.Context) (string, bool) {
			val, exists := c.Get("userInternalUUID")
			if !exists {
				return "", false
			}
			uid, ok := val.(string)
			return uid, ok
		},
	)
}

// pickerTestRouter returns a gin engine with a route for the picker handler.
func pickerTestRouter(h *PickerTokenHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", testUserID); c.Next() })
	r.POST("/me/picker_tokens/:provider_id", h.Handle)
	return r
}

// defaultPickerConfigs returns a valid config map for pickerProviderID.
func defaultPickerConfigs() map[string]PickerTokenConfig {
	return map[string]PickerTokenConfig{
		pickerProviderID: {DeveloperKey: "dev-key-123", AppID: "app-id-456"},
	}
}

// futureExpiry returns an expiry time well in the future (not expired).
func futureExpiry() *time.Time {
	t := time.Now().Add(1 * time.Hour)
	return &t
}

// pastExpiry returns an expiry time well in the past (expired).
func pastExpiry() *time.Time {
	t := time.Now().Add(-1 * time.Hour)
	return &t
}

// =============================================================================
// TestMintPickerToken — 8 test cases
// =============================================================================

// Case 1: Happy path — linked token with fresh expiry → 200.
func TestMintPickerToken_HappyPath(t *testing.T) {
	expiry := futureExpiry()
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{
				ID:           "tok-1",
				UserID:       testUserID,
				ProviderID:   pickerProviderID,
				AccessToken:  "fresh-access-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    expiry,
				Status:       ContentTokenStatusActive,
			}, nil
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body pickerTokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "fresh-access-token", body.AccessToken)
	assert.Equal(t, "dev-key-123", body.DeveloperKey)
	assert.Equal(t, "app-id-456", body.AppID)
	assert.False(t, body.ExpiresAt.IsZero())
}

// Case 2: No linked token → 404 with code "not_linked".
func TestMintPickerToken_NoLinkedToken(t *testing.T) {
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "not_linked", body["code"])
}

// Case 3: Token in failed_refresh status → 401 with code "token_refresh_failed".
func TestMintPickerToken_FailedRefreshStatus(t *testing.T) {
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{
				ID:         "tok-2",
				UserID:     testUserID,
				ProviderID: pickerProviderID,
				Status:     ContentTokenStatusFailedRefresh,
				ExpiresAt:  futureExpiry(),
			}, nil
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "token_refresh_failed", body["code"])
}

// Case 4: Provider not registered in registry → 422 with code "provider_not_registered".
func TestMintPickerToken_ProviderNotRegistered(t *testing.T) {
	repo := &mockContentTokenRepo{}
	registry := NewContentOAuthProviderRegistry()
	// Do not register the provider.

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "provider_not_registered", body["code"])
}

// Case 5: Picker inputs missing from config → 422 with code "picker_not_configured".
func TestMintPickerToken_PickerNotConfigured(t *testing.T) {
	repo := &mockContentTokenRepo{}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})

	// Config entry exists but all fields are empty (no ProviderConfig, no DeveloperKey, no AppID).
	configs := map[string]PickerTokenConfig{
		pickerProviderID: {DeveloperKey: "", AppID: ""},
	}

	h := newPickerTestHandler(repo, registry, configs)
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "picker_not_configured", body["code"])
}

// Case 6: No user in context → 401 with code "unauthenticated".
func TestMintPickerToken_Unauthenticated(t *testing.T) {
	repo := &mockContentTokenRepo{}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())

	// Build router WITHOUT the user context middleware.
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/me/picker_tokens/:provider_id", h.Handle)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "unauthenticated", body["code"])
}

// Case 7: Token expired but refresh succeeds → 200 with refreshed token.
// Verifies RefreshWithLock was called.
func TestMintPickerToken_ExpiredTokenRefreshSucceeds(t *testing.T) {
	expiredToken := &ContentToken{
		ID:           "tok-3",
		UserID:       testUserID,
		ProviderID:   pickerProviderID,
		AccessToken:  "old-access-token",
		RefreshToken: "valid-refresh-token",
		ExpiresAt:    pastExpiry(),
		Status:       ContentTokenStatusActive,
	}

	newExpiry := time.Now().Add(1 * time.Hour)
	refreshCalled := false

	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return expiredToken, nil
		},
		refreshWithLock: func(_ context.Context, id string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
			refreshCalled = true
			assert.Equal(t, expiredToken.ID, id)
			current := *expiredToken // copy
			updated, err := fn(&current)
			return updated, err
		},
	}

	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{
		id: pickerProviderID,
		refreshResp: &ContentOAuthTokenResponse{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
		},
	})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	assert.True(t, refreshCalled, "RefreshWithLock should have been called")

	var body pickerTokenResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "new-access-token", body.AccessToken)
	assert.False(t, body.ExpiresAt.IsZero())
	_ = newExpiry
}

// Case 9: Microsoft provider — only ProviderConfig set (no DeveloperKey/AppID)
// → 200 with provider_config populated and no top-level developer_key/app_id.
func TestPickerTokenHandler_Handle_ReturnsProviderConfig(t *testing.T) {
	const microsoftProviderID = "microsoft"
	expiry := futureExpiry()
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{
				ID:           "tok-ms-1",
				UserID:       testUserID,
				ProviderID:   microsoftProviderID,
				AccessToken:  "ms-access-token",
				RefreshToken: "ms-refresh-token",
				ExpiresAt:    expiry,
				Status:       ContentTokenStatusActive,
			}, nil
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: microsoftProviderID})

	configs := map[string]PickerTokenConfig{
		microsoftProviderID: {
			ProviderConfig: map[string]string{
				"client_id":     "mscid",
				"tenant_id":     "tnt",
				"picker_origin": "https://contoso.sharepoint.com",
			},
		},
	}

	h := newPickerTestHandler(repo, registry, configs)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", testUserID); c.Next() })
	r.POST("/me/picker_tokens/:provider_id", h.Handle)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+microsoftProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var resp struct {
		AccessToken    string            `json:"access_token"` //nolint:gosec // G117 - test struct decoding short-lived picker token
		ProviderConfig map[string]string `json:"provider_config"`
		DeveloperKey   string            `json:"developer_key"`
		AppID          string            `json:"app_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ms-access-token", resp.AccessToken)
	assert.Equal(t, "mscid", resp.ProviderConfig["client_id"])
	assert.Equal(t, "tnt", resp.ProviderConfig["tenant_id"])
	assert.Equal(t, "https://contoso.sharepoint.com", resp.ProviderConfig["picker_origin"])
	assert.Empty(t, resp.DeveloperKey)
	assert.Empty(t, resp.AppID)
}

// Case 10: Regression guard — Handle() must not mutate the registered ProviderConfig map.
// This guards against the aliasing bug where backfill wrote into cfg.ProviderConfig directly.
func TestPickerTokenHandler_Handle_DoesNotMutateRegisteredMap(t *testing.T) {
	const provID = "microsoft"
	expiry := futureExpiry()

	// sharedMap simulates the registered config that is owned by h.configs.
	sharedMap := map[string]string{"client_id": "msc"}
	// Snapshot what we expect sharedMap to contain after the call.
	wantMap := map[string]string{"client_id": "msc"}

	configs := map[string]PickerTokenConfig{
		provID: {
			ProviderConfig: sharedMap,
			DeveloperKey:   "devkey", // would be backfilled into sharedMap under the old code
			AppID:          "appid",  // would be backfilled into sharedMap under the old code
		},
	}

	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return &ContentToken{
				ID:           "tok-mut-1",
				UserID:       testUserID,
				ProviderID:   provID,
				AccessToken:  "access-token",
				RefreshToken: "refresh-token",
				ExpiresAt:    expiry,
				Status:       ContentTokenStatusActive,
			}, nil
		},
	}

	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: provID})

	h := newPickerTestHandler(repo, registry, configs)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userInternalUUID", testUserID); c.Next() })
	r.POST("/me/picker_tokens/:provider_id", h.Handle)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+provID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	// The registered map must be unchanged after the call.
	assert.Equal(t, wantMap, sharedMap, "Handle() must not mutate the registered ProviderConfig map")
}

// Case 8: Refresh permanently fails → 401 with code "token_refresh_failed".
func TestMintPickerToken_RefreshPermanentFailure(t *testing.T) {
	expiredToken := &ContentToken{
		ID:           "tok-4",
		UserID:       testUserID,
		ProviderID:   pickerProviderID,
		AccessToken:  "old-access-token",
		RefreshToken: "revoked-refresh-token",
		ExpiresAt:    pastExpiry(),
		Status:       ContentTokenStatusActive,
	}

	// Simulate permanent failure: provider returns a permanent error.
	permErr := &errContentOAuthPermanent{msg: "invalid_grant: token revoked"}

	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return expiredToken, nil
		},
		refreshWithLock: func(_ context.Context, _ string, fn func(current *ContentToken) (*ContentToken, error)) (*ContentToken, error) {
			current := *expiredToken
			return fn(&current)
		},
	}

	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{
		id:         pickerProviderID,
		refreshErr: permErr,
	})

	h := newPickerTestHandler(repo, registry, defaultPickerConfigs())
	r := pickerTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "token_refresh_failed", body["code"])
}
