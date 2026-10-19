package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Backend-unavailable paths for the content OAuth and picker handlers (#1119):
// a Redis or transient DB fault is a 503 with a JSON Error body and Retry-After,
// never a bare 500.

// errTransientDB mimics a classified transient DB fault from the token repository.
var errTransientDB = fmt.Errorf("content token: %w", dberrors.ErrTransient)

// assertServiceUnavailable checks for a documented 503: JSON Error body plus Retry-After.
func assertServiceUnavailable(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	var body Error
	require.NoError(t, dec.Decode(&body), "body must match the Error schema: %s", w.Body.String())
	assert.Equal(t, "service_unavailable", string(body.Error))
	assert.NotEmpty(t, body.ErrorDescription)
}

func TestContentOAuthUnavailable_ContentOAuthStateStore_RedisDown_ReturnsUnavailable(t *testing.T) {
	mr, store := newTestStateStoreWithServer(t)
	mr.Close()

	_, err := store.Put(context.Background(), ContentOAuthStatePayload{UserID: "u"}, time.Minute)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContentOAuthStateUnavailable)
	assert.ErrorIs(t, err, dberrors.ErrTransient)

	_, err = store.Consume(context.Background(), "some-nonce")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrContentOAuthStateUnavailable)
	assert.NotErrorIs(t, err, ErrContentOAuthStateNotFound)
}

func TestContentOAuthUnavailable_ContentOAuthStateStore_MissingNonce_IsNotUnavailable(t *testing.T) {
	store := newTestStateStore(t)
	_, err := store.Consume(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrContentOAuthStateNotFound)
	assert.NotErrorIs(t, err, ErrContentOAuthStateUnavailable)
}

func TestContentOAuthUnavailable_ContentOAuthHandlers_Authorize_RedisDown_Returns503(t *testing.T) {
	provider := &stubContentOAuthProvider{id: "mock", authURL: "https://auth.example.com/authorize"}
	h, mr := newTestHandlers(t, &mockContentTokenRepo{}, provider)
	mr.Close()

	r := ginTestRouter()
	r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
	r.POST("/me/content_tokens/:provider_id/authorize", h.Authorize)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/me/content_tokens/mock/authorize",
		strings.NewReader(`{"client_callback":"http://localhost:8079/"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assertServiceUnavailable(t, w)
}

func TestContentOAuthUnavailable_ContentOAuthHandlers_Callback_RedisDown_Returns503(t *testing.T) {
	h, mr := newTestHandlers(t, &mockContentTokenRepo{}, nil)
	mr.Close()

	r := ginTestRouter()
	r.GET("/oauth2/content_callback", h.Callback)

	params := url.Values{}
	params.Set("state", "some-nonce")
	params.Set("code", "c")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, buildCallbackURL(params), nil))

	assertServiceUnavailable(t, w)
}

func TestContentOAuthUnavailable_ContentOAuthHandlers_TokenStoreErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"transient DB fault is 503", errTransientDB, http.StatusServiceUnavailable},
		{"unclassified error is 500 with body", errors.New("boom"), http.StatusInternalServerError},
	}
	routes := []struct {
		name   string
		method string
		path   string
		route  string
		pick   func(h *ContentOAuthHandlers) gin.HandlerFunc
	}{
		{"List", http.MethodGet, "/me/content_tokens", "/me/content_tokens",
			func(h *ContentOAuthHandlers) gin.HandlerFunc { return h.List }},
		{"Delete", http.MethodDelete, "/me/content_tokens/mock", "/me/content_tokens/:provider_id",
			func(h *ContentOAuthHandlers) gin.HandlerFunc { return h.Delete }},
		{"AdminList", http.MethodGet, "/admin/users/u2/content_tokens", "/admin/users/:user_id/content_tokens",
			func(h *ContentOAuthHandlers) gin.HandlerFunc { return h.AdminList }},
		{"AdminDelete", http.MethodDelete, "/admin/users/u2/content_tokens/mock", "/admin/users/:user_id/content_tokens/:provider_id",
			func(h *ContentOAuthHandlers) gin.HandlerFunc { return h.AdminDelete }},
	}
	for _, rt := range routes {
		for _, tc := range tests {
			t.Run(rt.name+"/"+tc.name, func(t *testing.T) {
				repo := &mockContentTokenRepo{
					listByUser: func(_ context.Context, _ string) ([]ContentToken, error) { return nil, tc.err },
					deleteByUserAndProv: func(_ context.Context, _, _ string) (*ContentToken, error) {
						return nil, tc.err
					},
				}
				h, _ := newTestHandlers(t, repo, nil)
				r := ginTestRouter()
				r.Use(func(c *gin.Context) { setUserContext(c, testUserID); c.Next() })
				r.Handle(rt.method, rt.route, rt.pick(h))

				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(rt.method, rt.path, nil))

				if tc.wantStatus == http.StatusServiceUnavailable {
					assertServiceUnavailable(t, w)
					return
				}
				require.Equal(t, tc.wantStatus, w.Code)
				var body Error
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, "server_error", string(body.Error))
			})
		}
	}
}

func TestContentOAuthUnavailable_MintPickerToken_TransientLookupFailure_Returns503(t *testing.T) {
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, errTransientDB
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: pickerProviderID})
	r := pickerTestRouter(newPickerTestHandler(repo, registry, defaultPickerConfigs()))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil))

	assertServiceUnavailable(t, w)
}

func TestContentOAuthUnavailable_MicrosoftPickerGrantHandler_Handle_TransientLookupFailure_Returns503(t *testing.T) {
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) {
			return nil, errTransientDB
		},
	}
	h := NewMicrosoftPickerGrantHandler(repo, NewContentOAuthProviderRegistry(), "app-id", "",
		func(_ *gin.Context) (string, bool) { return "u1", true }, permissiveLoopbackValidator())

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assertServiceUnavailable(t, rec)
}

// assertPickerErrorBody decodes body strictly as the spec's Error schema (which
// forbids additional properties) and checks the error and details.code values.
// An empty wantDetail asserts that no details.code is present.
func assertPickerErrorBody(t *testing.T, body []byte, wantError, wantDetail string) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var e Error
	require.NoError(t, dec.Decode(&e), "body must match the Error schema: %s", body)
	assert.Equal(t, wantError, string(e.Error))
	assert.NotEmpty(t, e.ErrorDescription)
	gotDetail := ""
	if e.Details != nil && e.Details.Code != nil {
		gotDetail = *e.Details.Code
	}
	assert.Equal(t, wantDetail, gotDetail)
}

// expiredPickerToken returns a linked token that needs a refresh before use.
func expiredPickerToken(providerID string) *ContentToken {
	past := time.Now().Add(-1 * time.Hour)
	return &ContentToken{
		ID:           "tok-expired",
		UserID:       testUserID,
		ProviderID:   providerID,
		AccessToken:  "old",
		RefreshToken: "refresh",
		Status:       ContentTokenStatusActive,
		ExpiresAt:    &past,
	}
}

func TestContentOAuthUnavailable_MintPickerToken_RefreshErrors(t *testing.T) {
	tests := []struct {
		name        string
		refreshErr  error // provider Refresh error
		lockErr     error // RefreshWithLock store error (skips the provider call)
		wantStatus  int
		wantError   string
		wantDetail  string
		wantRetryAf bool
	}{
		{"transient provider failure", errors.New("provider timeout"), nil, http.StatusServiceUnavailable, "service_unavailable", "transient_failure", true},
		{"transient DB fault in refresh lock", nil, errTransientDB, http.StatusServiceUnavailable, "service_unavailable", "", true},
		{"unclassified refresh lock error", nil, errors.New("boom"), http.StatusInternalServerError, "server_error", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tok := expiredPickerToken(pickerProviderID)
			repo := &mockContentTokenRepo{
				getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) { return tok, nil },
				refreshWithLock: func(_ context.Context, _ string, fn func(*ContentToken) (*ContentToken, error)) (*ContentToken, error) {
					if tc.lockErr != nil {
						return nil, tc.lockErr
					}
					cur := *tok
					return fn(&cur)
				},
			}
			registry := NewContentOAuthProviderRegistry()
			registry.Register(&stubRefreshableProvider{id: pickerProviderID, refreshErr: tc.refreshErr})
			r := pickerTestRouter(newPickerTestHandler(repo, registry, defaultPickerConfigs()))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/me/picker_tokens/"+pickerProviderID, nil))

			require.Equal(t, tc.wantStatus, w.Code)
			assert.Equal(t, tc.wantRetryAf, w.Header().Get("Retry-After") != "")
			assertPickerErrorBody(t, w.Body.Bytes(), tc.wantError, tc.wantDetail)
		})
	}
}

func TestContentOAuthUnavailable_MicrosoftPickerGrantHandler_RefreshLockTransient_Returns503(t *testing.T) {
	tok := expiredPickerToken(ProviderMicrosoft)
	repo := &mockContentTokenRepo{
		getByUserAndProvider: func(_ context.Context, _, _ string) (*ContentToken, error) { return tok, nil },
		refreshWithLock: func(_ context.Context, _ string, _ func(*ContentToken) (*ContentToken, error)) (*ContentToken, error) {
			return nil, errTransientDB
		},
	}
	registry := NewContentOAuthProviderRegistry()
	registry.Register(&stubRefreshableProvider{id: ProviderMicrosoft})
	h := NewMicrosoftPickerGrantHandler(repo, registry, "app-id", "",
		func(_ *gin.Context) (string, bool) { return testUserID, true }, permissiveLoopbackValidator())

	body, _ := json.Marshal(MicrosoftPickerGrantRequest{DriveId: "b!abc", ItemId: "01XYZ"})
	c, rec := newPickerGrantGinContext(t, "POST", "/me/microsoft/picker_grants", bytes.NewReader(body))
	h.Handle(c)

	assertServiceUnavailable(t, rec)
}
