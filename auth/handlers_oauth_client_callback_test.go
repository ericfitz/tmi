package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuthorize_RejectsClientCallbackOutsideAllowlist pins the regression
// for T16. If a future refactor accepts an arbitrary client_callback (the
// pre-fix behavior), this test fails because the attacker URL would
// progress past the allowlist check.
func TestAuthorize_RejectsClientCallbackOutsideAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
			ClientCallbackAllowList: []string{
				"http://localhost:8079/",
				"http://localhost:8079/*",
			},
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "test-client-id",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
				},
			},
		},
	}

	h := &Handlers{config: cfg}
	router.GET("/oauth2/authorize", h.Authorize)

	cases := []struct {
		name           string
		clientCallback string
		expectStatus   int
		expectErrCode  string
	}{
		{
			name:           "attacker host rejected",
			clientCallback: "http://evil.example.com/grab",
			expectStatus:   http.StatusBadRequest,
			expectErrCode:  "invalid_request",
		},
		{
			name:           "host suffix smuggling rejected",
			clientCallback: "http://localhost:8079.evil.com/",
			expectStatus:   http.StatusBadRequest,
			expectErrCode:  "invalid_request",
		},
		{
			name:           "scheme mismatch rejected",
			clientCallback: "https://localhost:8079/",
			expectStatus:   http.StatusBadRequest,
			expectErrCode:  "invalid_request",
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			q := url.Values{}
			q.Set("idp", "google")
			q.Set("scope", "openid")
			q.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
			q.Set("code_challenge_method", "S256")
			q.Set("client_callback", tt.clientCallback)

			req := httptest.NewRequest("GET", "/oauth2/authorize?"+q.Encode(), nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectStatus, w.Code, "body=%s", w.Body.String())

			var resp map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tt.expectErrCode, resp["error"])
		})
	}
}

// TestAuthorize_AllowedClientCallbackPassesAllowlist confirms that a
// configured allowlist entry continues past the check (and only fails
// later for unrelated reasons like the missing test service). If the
// allowlist becomes too strict, this test catches that.
func TestAuthorize_AllowedClientCallbackPassesAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
			ClientCallbackAllowList: []string{
				"http://localhost:8079/*",
			},
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "test-client-id",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
				},
			},
		},
	}

	h := &Handlers{config: cfg}
	router.GET("/oauth2/authorize", h.Authorize)

	q := url.Values{}
	q.Set("idp", "google")
	q.Set("scope", "openid")
	q.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
	q.Set("code_challenge_method", "S256")
	q.Set("client_callback", "http://localhost:8079/cb?run=42")

	req := httptest.NewRequest("GET", "/oauth2/authorize?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The allowlist accepts; the handler then reaches a downstream stage
	// that requires the service (nil in this test), so we expect 503.
	assert.NotEqual(t, http.StatusBadRequest, w.Code,
		"allowed callback must NOT be rejected with 400 (got body=%s)", w.Body.String())
}

// TestAuthorize_EmptyAllowlistRejectsAnyCallback confirms the fail-closed
// default — an operator who forgets to configure the allowlist will get
// every client_callback rejected, not silently allowed.
func TestAuthorize_EmptyAllowlistRejectsAnyCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	cfg := Config{
		OAuth: OAuthConfig{
			CallbackURL:             "http://localhost:8080/oauth2/callback",
			ClientCallbackAllowList: nil,
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "test-client-id",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
				},
			},
		},
	}

	h := &Handlers{config: cfg}
	router.GET("/oauth2/authorize", h.Authorize)

	q := url.Values{}
	q.Set("idp", "google")
	q.Set("scope", "openid")
	q.Set("code_challenge", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM")
	q.Set("code_challenge_method", "S256")
	q.Set("client_callback", "http://localhost:8079/cb")

	req := httptest.NewRequest("GET", "/oauth2/authorize?"+q.Encode(), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "invalid_request", resp["error"])
}
