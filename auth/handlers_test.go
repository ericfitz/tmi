package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProvidersHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create test config with OAuth providers
	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/auth/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:       "google",
					Name:     "Google",
					Enabled:  true,
					Icon:     "google",
					ClientID: "test-google-client-id",
				},
				"github": {
					ID:       "github",
					Name:     "GitHub",
					Enabled:  true,
					Icon:     "github",
					ClientID: "test-github-client-id",
				},
			},
		},
	}

	// Create handlers with test config
	handlers := &Handlers{
		config: config,
	}

	// Register just the providers endpoint
	router.GET("/auth/providers", handlers.GetProviders)

	// Test the endpoint
	req := httptest.NewRequest("GET", "/auth/providers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	providers := response["providers"]
	assert.Len(t, providers, 2) // google and github

	// Check provider details
	googleProvider := findProviderByID(providers, "google")
	require.NotNil(t, googleProvider)
	assert.Equal(t, "Google", googleProvider["name"])
	assert.Equal(t, "fa-brands fa-google", googleProvider["icon"])
	assert.Equal(t, "test-google-client-id", googleProvider["client_id"])
	assert.Equal(t, "http://localhost:8080/auth/callback", googleProvider["redirect_uri"])
	assert.Contains(t, googleProvider["auth_url"], "/auth/login/google")

	githubProvider := findProviderByID(providers, "github")
	require.NotNil(t, githubProvider)
	assert.Equal(t, "GitHub", githubProvider["name"])
	assert.Equal(t, "fa-brands fa-github", githubProvider["icon"])
	assert.Equal(t, "test-github-client-id", githubProvider["client_id"])
	assert.Equal(t, "http://localhost:8080/auth/callback", githubProvider["redirect_uri"])
	assert.Contains(t, githubProvider["auth_url"], "/auth/login/github")
}

func TestGetProvidersEmptyConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create empty config
	config := Config{
		OAuth: OAuthConfig{
			Providers: map[string]OAuthProviderConfig{},
		},
	}

	handlers := &Handlers{
		config: config,
	}

	router.GET("/auth/providers", handlers.GetProviders)

	req := httptest.NewRequest("GET", "/auth/providers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	providers := response["providers"]
	assert.Len(t, providers, 0) // no providers configured
}

func TestGetProvidersDisabledProviders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create config with disabled providers
	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/auth/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:       "google",
					Name:     "Google",
					Enabled:  true,
					Icon:     "google",
					ClientID: "test-google-client-id",
				},
				"github": {
					ID:       "github",
					Name:     "GitHub",
					Enabled:  false, // disabled
					Icon:     "github",
					ClientID: "test-github-client-id",
				},
			},
		},
	}

	handlers := &Handlers{
		config: config,
	}

	router.GET("/auth/providers", handlers.GetProviders)

	req := httptest.NewRequest("GET", "/auth/providers", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	providers := response["providers"]
	assert.Len(t, providers, 1) // only enabled providers returned

	// Should only have Google provider
	googleProvider := findProviderByID(providers, "google")
	require.NotNil(t, googleProvider)
	assert.Equal(t, "Google", googleProvider["name"])
	assert.Equal(t, "fa-brands fa-google", googleProvider["icon"])

	// Should not have disabled GitHub provider
	githubProvider := findProviderByID(providers, "github")
	assert.Nil(t, githubProvider)
}

func TestExchangeHandlerValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		OAuth: OAuthConfig{
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "test-client-id",
					ClientSecret:     "test-secret",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
					TokenURL:         "https://oauth2.googleapis.com/token",
					UserInfoURL:      "https://www.googleapis.com/oauth2/v3/userinfo",
				},
			},
		},
	}

	handlers := &Handlers{
		config: config,
	}

	router.POST("/auth/exchange/:provider", handlers.Exchange)

	tests := []struct {
		name           string
		provider       string
		requestBody    map[string]interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:     "Missing code parameter",
			provider: "google",
			requestBody: map[string]interface{}{
				"redirect_uri": "http://localhost:3000/callback",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:     "Missing redirect_uri parameter",
			provider: "google",
			requestBody: map[string]interface{}{
				"code": "test-auth-code",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:     "Invalid provider",
			provider: "invalid",
			requestBody: map[string]interface{}{
				"code":         "test-auth-code",
				"redirect_uri": "http://localhost:3000/callback",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid provider: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/auth/exchange/"+tt.provider, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
				require.NoError(t, err)
				assert.Contains(t, errorResponse["error"], tt.expectedError)
			}
		})
	}
}

func TestExchangeHandlerSuccess(t *testing.T) {
	// This test would require setting up mock OAuth provider responses
	// For now, we'll test validation logic only
	t.Skip("Full OAuth exchange requires mock OAuth provider - test validation only")
}

func TestRefreshTokenHandler(t *testing.T) {
	// Skip this test as it requires service for refresh token validation
	t.Skip("Refresh handler requires service for token operations - test structure only")

	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create test handlers with minimal config
	handlers := &Handlers{
		config: Config{},
		// Note: service is nil - this will cause errors but tests validation logic
	}

	router.POST("/auth/refresh", handlers.Refresh)

	tests := []struct {
		name           string
		requestBody    map[string]interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing refresh token",
			requestBody:    map[string]interface{}{},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name: "Empty refresh token",
			requestBody: map[string]interface{}{
				"refresh_token": "",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.requestBody)
			req := httptest.NewRequest("POST", "/auth/refresh", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
				require.NoError(t, err)
				assert.Contains(t, errorResponse["error"], tt.expectedError)
			}
		})
	}
}

func TestGetAuthorizeURL(t *testing.T) {
	// Skip this test as it requires service and Redis for state storage
	t.Skip("Authorize handler requires service and Redis - test structure only")

	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/auth/callback",
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

	handlers := &Handlers{
		config: config,
	}

	router.GET("/auth/login/:provider", handlers.Authorize)

	tests := []struct {
		name           string
		provider       string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Valid provider",
			provider:       "google",
			expectedStatus: http.StatusFound, // 302 redirect
		},
		{
			name:           "Invalid provider",
			provider:       "invalid",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid provider: invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/auth/login/"+tt.provider, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusFound {
				// Check that Location header contains expected OAuth URL
				location := w.Header().Get("Location")
				assert.Contains(t, location, "https://accounts.google.com/o/oauth2/auth")
				assert.Contains(t, location, "client_id=test-client-id")
				assert.Contains(t, location, "state=") // Should contain state parameter
			} else if tt.expectedError != "" {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
				require.NoError(t, err)
				assert.Contains(t, errorResponse["error"], tt.expectedError)
			}
		})
	}
}

func TestStateParameterGeneration(t *testing.T) {
	// Test that state parameters are properly generated and stored
	handlers := &Handlers{
		config: Config{},
	}

	// Generate multiple states to ensure uniqueness
	states := make(map[string]bool)
	for i := 0; i < 100; i++ {
		state := handlers.generateState()
		assert.NotEmpty(t, state)
		assert.False(t, states[state], "State should be unique")
		states[state] = true
		assert.True(t, len(state) > 10, "State should be sufficiently random")
	}
}

func TestClientCallbackURLBuilder(t *testing.T) {
	// Test the buildClientRedirectURL helper function
	tokenPair := TokenPair{
		AccessToken:  "access_token_123",
		RefreshToken: "refresh_token_456",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
	}
	state := "test_state_789"

	tests := []struct {
		name           string
		clientCallback string
		expectedURL    string
		expectError    bool
	}{
		{
			name:           "Simple callback URL",
			clientCallback: "http://localhost:4200/auth/callback",
			expectedURL:    "http://localhost:4200/auth/callback?access_token=access_token_123&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
			expectError:    false,
		},
		{
			name:           "Callback URL with existing query params",
			clientCallback: "http://localhost:4200/auth/callback?existing=param",
			expectedURL:    "http://localhost:4200/auth/callback?access_token=access_token_123&existing=param&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
			expectError:    false,
		},
		{
			name:           "HTTPS callback URL",
			clientCallback: "https://app.example.com/oauth/callback",
			expectedURL:    "https://app.example.com/oauth/callback?access_token=access_token_123&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
			expectError:    false,
		},
		{
			name:           "Invalid URL",
			clientCallback: "not-a-valid-url",
			expectedURL:    "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := buildClientRedirectURL(tt.clientCallback, tokenPair, state)

			if tt.expectError {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)

				// Parse the result URL to verify all parameters are present
				parsedURL, err := url.Parse(result)
				require.NoError(t, err)

				params := parsedURL.Query()
				assert.Equal(t, "access_token_123", params.Get("access_token"))
				assert.Equal(t, "refresh_token_456", params.Get("refresh_token"))
				assert.Equal(t, "Bearer", params.Get("token_type"))
				assert.Equal(t, "3600", params.Get("expires_in"))
				assert.Equal(t, "test_state_789", params.Get("state"))
			}
		})
	}
}

func TestAuthorizeWithClientCallback(t *testing.T) {
	// Skip this test as it requires service and Redis for state storage
	t.Skip("Authorize handler with client callback requires service and Redis - test structure only")

	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/auth/callback",
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

	handlers := &Handlers{
		config: config,
		// Note: service would be required for Redis operations
	}

	router.GET("/auth/login/:provider", handlers.Authorize)

	tests := []struct {
		name           string
		provider       string
		clientCallback string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Valid provider with client callback",
			provider:       "google",
			clientCallback: "http://localhost:4200/auth/callback",
			expectedStatus: http.StatusFound, // 302 redirect
		},
		{
			name:           "Valid provider without client callback",
			provider:       "google",
			clientCallback: "",
			expectedStatus: http.StatusFound, // 302 redirect
		},
		{
			name:           "Invalid provider with client callback",
			provider:       "invalid",
			clientCallback: "http://localhost:4200/auth/callback",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "provider invalid not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqURL := "/auth/login/" + tt.provider
			if tt.clientCallback != "" {
				reqURL += "?client_callback=" + url.QueryEscape(tt.clientCallback)
			}

			req := httptest.NewRequest("GET", reqURL, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusFound {
				// Check that Location header contains expected OAuth URL
				location := w.Header().Get("Location")
				assert.Contains(t, location, "https://accounts.google.com/o/oauth2/auth")
				assert.Contains(t, location, "client_id=test-client-id")
				assert.Contains(t, location, "state=") // Should contain state parameter
			} else if tt.expectedError != "" {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
				require.NoError(t, err)
				assert.Contains(t, errorResponse["error"], tt.expectedError)
			}
		})
	}
}

func TestCallbackWithClientRedirect(t *testing.T) {
	// Skip this test as it requires full service setup
	t.Skip("Callback handler with client redirect requires service, Redis, and OAuth provider - test structure only")

	// This test would verify:
	// 1. State parameter validation with JSON format
	// 2. Client callback URL extraction from state
	// 3. Redirect to client callback with tokens
	// 4. Fallback to JSON response when no client callback

	t.Logf("Would test OAuth callback with client_redirect functionality")
	t.Logf("Including state validation, token generation, and client redirect")
}

func findProviderByID(providers []map[string]interface{}, id string) map[string]interface{} {
	for _, provider := range providers {
		if provider["id"] == id {
			return provider
		}
	}
	return nil
}
