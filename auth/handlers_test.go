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
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:       "google",
					Name:     "Google",
					Enabled:  true,
					Icon:     "fa-brands fa-google",
					ClientID: "test-google-client-id",
				},
				"github": {
					ID:       "github",
					Name:     "GitHub",
					Enabled:  true,
					Icon:     "fa-brands fa-github",
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
	router.GET("/oauth2/providers", handlers.GetProviders)

	// Test the endpoint
	req := httptest.NewRequest("GET", "/oauth2/providers", nil)
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
	assert.Equal(t, "http://localhost:8080/oauth2/callback", googleProvider["redirect_uri"])
	assert.Contains(t, googleProvider["auth_url"], "/oauth2/authorize?idp=google")

	githubProvider := findProviderByID(providers, "github")
	require.NotNil(t, githubProvider)
	assert.Equal(t, "GitHub", githubProvider["name"])
	assert.Equal(t, "fa-brands fa-github", githubProvider["icon"])
	assert.Equal(t, "test-github-client-id", githubProvider["client_id"])
	assert.Equal(t, "http://localhost:8080/oauth2/callback", githubProvider["redirect_uri"])
	assert.Contains(t, githubProvider["auth_url"], "/oauth2/authorize?idp=github")
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

	router.GET("/oauth2/providers", handlers.GetProviders)

	req := httptest.NewRequest("GET", "/oauth2/providers", nil)
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
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:       "google",
					Name:     "Google",
					Enabled:  true,
					Icon:     "fa-brands fa-google",
					ClientID: "test-google-client-id",
				},
				"github": {
					ID:       "github",
					Name:     "GitHub",
					Enabled:  false, // disabled
					Icon:     "fa-brands fa-github",
					ClientID: "test-github-client-id",
				},
			},
		},
	}

	handlers := &Handlers{
		config: config,
	}

	router.GET("/oauth2/providers", handlers.GetProviders)

	req := httptest.NewRequest("GET", "/oauth2/providers", nil)
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

func TestGetSAMLProvidersHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		SAML: SAMLConfig{
			Enabled: true,
			Providers: map[string]SAMLProviderConfig{
				"okta": {
					ID:       "okta",
					Name:     "Okta",
					Enabled:  true,
					Icon:     "fa-brands fa-okta",
					EntityID: "https://tmi.example.com",
					ACSURL:   "https://tmi.example.com/saml/acs",
					SLOURL:   "https://tmi.example.com/saml/slo",
				},
				"azure": {
					ID:       "azure",
					Name:     "Azure AD",
					Enabled:  true,
					Icon:     "fa-brands fa-microsoft",
					EntityID: "https://tmi.example.com",
					ACSURL:   "https://tmi.example.com/saml/acs",
				},
			},
		},
	}

	handlers := &Handlers{config: config}
	router.GET("/saml/providers", handlers.GetSAMLProviders)

	req := httptest.NewRequest("GET", "/saml/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	providers := response["providers"]
	assert.Len(t, providers, 2)

	// Verify okta provider
	oktaProvider := findProviderByID(providers, "okta")
	require.NotNil(t, oktaProvider)
	assert.Equal(t, "Okta", oktaProvider["name"])
	assert.Equal(t, "fa-brands fa-okta", oktaProvider["icon"])
	assert.Contains(t, oktaProvider["auth_url"], "/saml/okta/login")
	assert.Contains(t, oktaProvider["metadata_url"], "/saml/okta/metadata")
	assert.Equal(t, "https://tmi.example.com", oktaProvider["entity_id"])
	assert.Equal(t, "https://tmi.example.com/saml/acs", oktaProvider["acs_url"])

	// Verify cache header
	assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))
}

func TestGetSAMLProvidersDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		SAML: SAMLConfig{
			Enabled: true,
			Providers: map[string]SAMLProviderConfig{
				"okta": {
					ID:      "okta",
					Enabled: false, // Disabled
				},
			},
		},
	}

	handlers := &Handlers{config: config}
	router.GET("/saml/providers", handlers.GetSAMLProviders)

	req := httptest.NewRequest("GET", "/saml/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Disabled providers should be filtered out
	assert.Empty(t, response["providers"])
}

func TestGetSAMLProvidersSAMLDisabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		SAML: SAMLConfig{
			Enabled: false, // SAML completely disabled
		},
	}

	handlers := &Handlers{config: config}
	router.GET("/saml/providers", handlers.GetSAMLProviders)

	req := httptest.NewRequest("GET", "/saml/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string][]map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Should return empty array, not error
	assert.Empty(t, response["providers"])
}

func TestGetSAMLProvidersNoSensitiveData(t *testing.T) {
	// Security test: Verify NO sensitive data in response
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		SAML: SAMLConfig{
			Enabled: true,
			Providers: map[string]SAMLProviderConfig{
				"okta": {
					ID:               "okta",
					Name:             "Okta",
					Enabled:          true,
					SPPrivateKey:     "SENSITIVE_PRIVATE_KEY",
					SPPrivateKeyPath: "/etc/saml/private.key",
					IDPMetadataURL:   "https://internal-idp.example.com/metadata",
					EntityID:         "https://tmi.example.com",
					ACSURL:           "https://tmi.example.com/saml/acs",
				},
			},
		},
	}

	handlers := &Handlers{config: config}
	router.GET("/saml/providers", handlers.GetSAMLProviders)

	req := httptest.NewRequest("GET", "/saml/providers", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	responseBody := w.Body.String()

	// Verify sensitive data is NOT in response
	assert.NotContains(t, responseBody, "SENSITIVE_PRIVATE_KEY")
	assert.NotContains(t, responseBody, "/etc/saml/private.key")
	assert.NotContains(t, responseBody, "internal-idp.example.com")
	assert.NotContains(t, responseBody, "sp_private_key")
	assert.NotContains(t, responseBody, "idp_metadata_url")
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
					UserInfo: []UserInfoEndpoint{
						{
							URL:    "https://www.googleapis.com/oauth2/v3/userinfo",
							Claims: map[string]string{},
						},
					},
				},
			},
		},
	}

	handlers := &Handlers{
		config: config,
	}

	// OAuth token endpoint uses query parameters: /oauth2/token?idp=provider
	router.POST("/oauth2/token", handlers.Exchange)

	tests := []struct {
		name           string
		provider       string
		requestBody    map[string]interface{}
		expectedStatus int
		expectedError  string
	}{
		{
			name:     "Missing grant_type parameter",
			provider: "google",
			requestBody: map[string]interface{}{
				"code":         "test-auth-code",
				"redirect_uri": "http://localhost:3000/callback",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:     "Invalid grant_type",
			provider: "google",
			requestBody: map[string]interface{}{
				"grant_type":   "client_credentials",
				"code":         "test-auth-code",
				"redirect_uri": "http://localhost:3000/callback",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_grant",
		},
		{
			name:     "Missing code parameter",
			provider: "google",
			requestBody: map[string]interface{}{
				"grant_type":   "authorization_code",
				"redirect_uri": "http://localhost:3000/callback",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:     "Missing redirect_uri parameter",
			provider: "google",
			requestBody: map[string]interface{}{
				"grant_type": "authorization_code",
				"code":       "test-auth-code",
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid request",
		},
		{
			name:     "Invalid provider",
			provider: "invalid",
			requestBody: map[string]interface{}{
				"grant_type":   "authorization_code",
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
			req := httptest.NewRequest("POST", "/oauth2/token?idp="+tt.provider, bytes.NewBuffer(body))
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

	router.POST("/oauth2/refresh", handlers.Refresh)

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
			req := httptest.NewRequest("POST", "/oauth2/refresh", bytes.NewBuffer(body))
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
			CallbackURL: "http://localhost:8080/oauth2/callback",
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

	// OAuth endpoints use query parameters: /oauth2/authorize?idp=provider
	router.GET("/oauth2/authorize", handlers.Authorize)

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
			req := httptest.NewRequest("GET", "/oauth2/authorize?idp="+tt.provider, nil)
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
			expectedURL:    "http://localhost:4200/auth/callback#access_token=access_token_123&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
			expectError:    false,
		},
		{
			name:           "Callback URL with existing query params",
			clientCallback: "http://localhost:4200/auth/callback?existing=param",
			expectedURL:    "http://localhost:4200/auth/callback?existing=param#access_token=access_token_123&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
			expectError:    false,
		},
		{
			name:           "HTTPS callback URL",
			clientCallback: "https://app.example.com/oauth/callback",
			expectedURL:    "https://app.example.com/oauth/callback#access_token=access_token_123&expires_in=3600&refresh_token=refresh_token_456&state=test_state_789&token_type=Bearer",
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

				// Parse the result URL to verify all parameters are present in fragment
				parsedURL, err := url.Parse(result)
				require.NoError(t, err)

				// Tokens should be in fragment (after #), not query string
				fragment := parsedURL.Fragment
				params, err := url.ParseQuery(fragment)
				require.NoError(t, err)

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
			CallbackURL: "http://localhost:8080/oauth2/callback",
			Providers: map[string]OAuthProviderConfig{
				"google": {
					ID:               "google",
					Name:             "Google",
					Enabled:          true,
					ClientID:         "test-client-id",
					AuthorizationURL: "https://accounts.google.com/o/oauth2/oauth2",
				},
			},
		},
	}

	handlers := &Handlers{
		config: config,
		// Note: service would be required for Redis operations
	}

	// OAuth endpoints use query parameters: /oauth2/authorize?idp=provider
	router.GET("/oauth2/authorize", handlers.Authorize)

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
			reqURL := "/oauth2/authorize?idp=" + tt.provider
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

func TestValidateOAuthScope(t *testing.T) {
	handlers := &Handlers{}

	tests := []struct {
		name        string
		scope       string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "Valid scope with openid, profile, email",
			scope:       "openid profile email",
			expectError: false,
		},
		{
			name:        "Valid scope with only openid",
			scope:       "openid",
			expectError: false,
		},
		{
			name:        "Valid scope with openid and profile",
			scope:       "openid profile",
			expectError: false,
		},
		{
			name:        "Valid scope with openid and email",
			scope:       "openid email",
			expectError: false,
		},
		{
			name:        "Valid scope with openid and unsupported scopes (should ignore unsupported)",
			scope:       "openid profile email write read admin",
			expectError: false,
		},
		{
			name:        "Missing openid scope",
			scope:       "profile email",
			expectError: true,
			errorMsg:    "OpenID Connect requires 'openid' scope",
		},
		{
			name:        "Empty scope parameter",
			scope:       "",
			expectError: true,
			errorMsg:    "scope parameter is required",
		},
		{
			name:        "Whitespace only scope",
			scope:       "   ",
			expectError: true,
			errorMsg:    "scope parameter cannot be empty",
		},
		{
			name:        "Valid scope with extra whitespace",
			scope:       "  openid   profile   email  ",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handlers.validateOAuthScope(tt.scope)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAuthorizeWithScopeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
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
		// Note: service is nil - this will test validation before service calls
	}

	router.GET("/oauth2/authorize", handlers.Authorize)

	tests := []struct {
		name           string
		url            string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "Missing scope parameter",
			url:            "/oauth2/authorize?idp=google",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_scope",
		},
		{
			name:           "Invalid scope - missing openid",
			url:            "/oauth2/authorize?idp=google&scope=profile%20email",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_scope",
		},
		{
			name:           "Valid scope - openid profile email",
			url:            "/oauth2/authorize?idp=google&scope=openid%20profile%20email",
			expectedStatus: http.StatusInternalServerError, // Will fail later due to missing service, but scope validation passes
		},
		{
			name:           "Valid scope - only openid",
			url:            "/oauth2/authorize?idp=google&scope=openid",
			expectedStatus: http.StatusInternalServerError, // Will fail later due to missing service, but scope validation passes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.url, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedError != "" && w.Code == http.StatusBadRequest {
				var errorResponse map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &errorResponse)
				require.NoError(t, err)
				assert.Contains(t, errorResponse["error"], tt.expectedError)
			}
		})
	}
}

func TestGetOAuthProtectedResourceMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create test config
	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "http://localhost:8080/oauth2/callback",
		},
	}

	handlers := &Handlers{
		config: config,
	}

	// Register the RFC 9728 endpoint
	router.GET("/.well-known/oauth-protected-resource", handlers.GetOAuthProtectedResourceMetadata)

	// Create test request
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	req.Host = "localhost:8080"
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=3600", w.Header().Get("Cache-Control"))

	// Parse and validate response body
	var metadata OAuthProtectedResourceMetadata
	err := json.Unmarshal(w.Body.Bytes(), &metadata)
	require.NoError(t, err)

	// Validate required field
	assert.Equal(t, "http://localhost:8080", metadata.Resource)

	// Validate optional fields
	assert.Equal(t, []string{"openid", "profile", "email"}, metadata.ScopesSupported)
	assert.Equal(t, []string{"http://localhost:8080"}, metadata.AuthorizationServers)
	assert.Equal(t, "http://localhost:8080/.well-known/jwks.json", metadata.JWKSURI)
	assert.Equal(t, []string{"header"}, metadata.BearerMethodsSupported)
	assert.Equal(t, "TMI (Threat Modeling Improved) API", metadata.ResourceName)
	assert.Equal(t, "https://github.com/ericfitz/tmi", metadata.ResourceDocumentation)
	assert.False(t, metadata.TLSClientCertificateBoundAccessTokens)

	// Validate JSON structure matches RFC 9728
	var jsonResponse map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &jsonResponse)
	require.NoError(t, err)

	// Check that all expected fields are present
	assert.Contains(t, jsonResponse, "resource")
	assert.Contains(t, jsonResponse, "scopes_supported")
	assert.Contains(t, jsonResponse, "authorization_servers")
	assert.Contains(t, jsonResponse, "jwks_uri")
	assert.Contains(t, jsonResponse, "bearer_methods_supported")
	assert.Contains(t, jsonResponse, "resource_name")
	assert.Contains(t, jsonResponse, "resource_documentation")
	assert.Contains(t, jsonResponse, "tls_client_certificate_bound_access_tokens")
}

func TestGetOAuthProtectedResourceMetadata_WithHTTPS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create test config
	config := Config{
		OAuth: OAuthConfig{
			CallbackURL: "https://api.example.com/oauth2/callback",
		},
	}

	handlers := &Handlers{
		config: config,
	}

	// Register the RFC 9728 endpoint
	router.GET("/.well-known/oauth-protected-resource", handlers.GetOAuthProtectedResourceMetadata)

	// Create test request with HTTPS
	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource", nil)
	req.Host = "api.example.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)

	// Parse and validate response body
	var metadata OAuthProtectedResourceMetadata
	err := json.Unmarshal(w.Body.Bytes(), &metadata)
	require.NoError(t, err)

	// Validate that HTTPS URLs are generated correctly
	assert.Equal(t, "https://api.example.com", metadata.Resource)
	assert.Equal(t, []string{"https://api.example.com"}, metadata.AuthorizationServers)
	assert.Equal(t, "https://api.example.com/.well-known/jwks.json", metadata.JWKSURI)
}
