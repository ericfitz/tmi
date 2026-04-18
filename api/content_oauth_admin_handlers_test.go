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

// newAdminTestHandlers creates a ContentOAuthHandlers suitable for admin handler tests.
// The admin-authorization check is enforced by middleware at route registration time;
// these tests exercise the handler logic directly (no admin middleware).
func newAdminTestHandlers(t *testing.T, repo ContentTokenRepository, provider ContentOAuthProvider) *ContentOAuthHandlers {
	t.Helper()
	h, _ := newTestHandlers(t, repo, provider)
	return h
}

// =============================================================================
// Task 5.1 — AdminList tests
// =============================================================================

func TestContentOAuthAdminHandlers_List_HappyPath(t *testing.T) {
	// The target user whose tokens we are listing.
	const targetUserID = "target-user-uuid-9999"
	const callerUserID = "caller-admin-uuid-1111"

	now := time.Now()
	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, userID string) ([]ContentToken, error) {
			// The handler must pass the path user_id, not the caller's ID.
			assert.Equal(t, targetUserID, userID)
			return []ContentToken{
				{
					ProviderID:           "confluence",
					ProviderAccountID:    "acc-target",
					ProviderAccountLabel: "target@example.com",
					Scopes:               "read write",
					Status:               ContentTokenStatusActive,
					AccessToken:          "SECRET_TOKEN",
					RefreshToken:         "REFRESH_TOKEN",
					ExpiresAt:            &now,
					CreatedAt:            now,
				},
			}, nil
		},
	}

	h := newAdminTestHandlers(t, repo, nil)
	r := ginTestRouter()
	// Inject the caller (admin) into context — the handler reads user_id from path, not context.
	r.Use(func(c *gin.Context) {
		setUserContext(c, callerUserID)
		c.Next()
	})
	r.GET("/admin/users/:user_id/content_tokens", h.AdminList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users/"+targetUserID+"/content_tokens", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	// Secrets must not appear in response.
	body := w.Body.String()
	assert.NotContains(t, body, "SECRET_TOKEN")
	assert.NotContains(t, body, "REFRESH_TOKEN")

	var resp struct {
		ContentTokens []contentTokenInfo `json:"content_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.ContentTokens, 1)
	assert.Equal(t, "confluence", resp.ContentTokens[0].ProviderID)
	assert.Equal(t, "target@example.com", resp.ContentTokens[0].ProviderAccountLabel)
}

func TestContentOAuthAdminHandlers_List_EmptyUser(t *testing.T) {
	const targetUserID = "user-with-no-tokens"

	repo := &mockContentTokenRepo{
		listByUser: func(_ context.Context, _ string) ([]ContentToken, error) {
			return []ContentToken{}, nil
		},
	}

	h := newAdminTestHandlers(t, repo, nil)
	r := ginTestRouter()
	r.GET("/admin/users/:user_id/content_tokens", h.AdminList)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users/"+targetUserID+"/content_tokens", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		ContentTokens []contentTokenInfo `json:"content_tokens"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Empty(t, resp.ContentTokens)
}

// =============================================================================
// Task 5.1 — AdminDelete tests
// =============================================================================

func TestContentOAuthAdminHandlers_Delete_HappyPath(t *testing.T) {
	const targetUserID = "target-user-uuid-9999"
	const providerID = "mock"
	const accessToken = "at-to-revoke-admin"

	provider := &stubContentOAuthProvider{id: providerID, authURL: "https://auth.example.com/authorize"}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, userID, pID string) (*ContentToken, error) {
			assert.Equal(t, targetUserID, userID)
			assert.Equal(t, providerID, pID)
			return &ContentToken{
				UserID:      userID,
				ProviderID:  pID,
				AccessToken: accessToken,
				Status:      ContentTokenStatusActive,
			}, nil
		},
	}

	h := newAdminTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.DELETE("/admin/users/:user_id/content_tokens/:provider_id", h.AdminDelete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+targetUserID+"/content_tokens/"+providerID, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, provider.revokeCalls, "provider.Revoke should have been called once")
}

func TestContentOAuthAdminHandlers_Delete_NotFound_Idempotent(t *testing.T) {
	const targetUserID = "target-user-uuid-9999"
	const providerID = "mock"

	provider := &stubContentOAuthProvider{id: providerID, authURL: "https://auth.example.com/authorize"}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, ErrContentTokenNotFound
		},
	}

	h := newAdminTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.DELETE("/admin/users/:user_id/content_tokens/:provider_id", h.AdminDelete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+targetUserID+"/content_tokens/"+providerID, nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 0, provider.revokeCalls, "provider.Revoke must NOT be called when token not found")
}

func TestContentOAuthAdminHandlers_Delete_RevokeFailure_Still204(t *testing.T) {
	const targetUserID = "target-user-uuid-9999"
	const providerID = "mock"

	provider := &stubContentOAuthProvider{
		id:        providerID,
		authURL:   "https://auth.example.com/authorize",
		revokeErr: errors.New("provider unavailable"),
	}
	repo := &mockContentTokenRepo{
		deleteByUserAndProv: func(_ context.Context, userID, pID string) (*ContentToken, error) {
			return &ContentToken{
				UserID:      userID,
				ProviderID:  pID,
				AccessToken: "at-admin-revoke",
				Status:      ContentTokenStatusActive,
			}, nil
		},
	}

	h := newAdminTestHandlers(t, repo, provider)
	r := ginTestRouter()
	r.DELETE("/admin/users/:user_id/content_tokens/:provider_id", h.AdminDelete)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/admin/users/"+targetUserID+"/content_tokens/"+providerID, nil)
	r.ServeHTTP(w, req)

	// 204 even when provider.Revoke errors.
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, provider.revokeCalls, "provider.Revoke should have been attempted")
}
