//go:build dev || test

package auth

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"
)

// TestProvider implements a test-only OAuth provider that always succeeds
type TestProvider struct {
	*BaseProvider
	clientSecret string
}

// NewTestProvider creates a new test OAuth provider
func NewTestProvider(config OAuthProviderConfig, callbackURL string) *TestProvider {
	// Use a fixed well-known secret for testing
	testSecret := "test-oauth-secret-12345"
	
	baseConfig := &oauth2.Config{
		ClientID:     config.ClientID,
		ClientSecret: testSecret,
		RedirectURL:  callbackURL,
		Scopes:       []string{"profile", "email"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "/auth/test/authorize",
			TokenURL: "/auth/test/token",
		},
	}

	return &TestProvider{
		BaseProvider: &BaseProvider{
			config:       config,
			oauth2Config: baseConfig,
		},
		clientSecret: testSecret,
	}
}

// GetAuthorizationURL returns the test authorization URL
func (p *TestProvider) GetAuthorizationURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state)
}

// ExchangeCode simulates token exchange and always succeeds
func (p *TestProvider) ExchangeCode(ctx context.Context, code string) (*TokenResponse, error) {
	// Generate a fake access token
	accessToken := fmt.Sprintf("test_access_token_%d", time.Now().Unix())
	idToken := p.generateTestIDToken()

	return &TokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   3600,
		IDToken:     idToken,
	}, nil
}

// GetUserInfo returns fake user information
func (p *TestProvider) GetUserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	// Generate random 8-digit number for username
	randomNum, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return nil, fmt.Errorf("failed to generate random number: %w", err)
	}
	
	username := fmt.Sprintf("testuser-%08d", randomNum)
	email := fmt.Sprintf("%s@test.tmi", username)

	return &UserInfo{
		ID:    username,
		Email: email,
		Name:  fmt.Sprintf("Test User %08d", randomNum),
	}, nil
}

// ValidateIDToken validates the test ID token (always succeeds)
func (p *TestProvider) ValidateIDToken(ctx context.Context, idToken string) (*IDTokenClaims, error) {
	// Generate the same user info for consistency
	randomNum, err := rand.Int(rand.Reader, big.NewInt(100000000))
	if err != nil {
		return nil, fmt.Errorf("failed to generate random number: %w", err)
	}

	username := fmt.Sprintf("testuser-%08d", randomNum)
	email := fmt.Sprintf("%s@test.tmi", username)

	return &IDTokenClaims{
		Subject:   username,
		Email:     email,
		Name:      fmt.Sprintf("Test User %08d", randomNum),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
		IssuedAt:  time.Now().Unix(),
		Issuer:    "test-oauth-provider",
		Audience:  p.config.ClientID,
	}, nil
}

// generateTestIDToken creates a simple JWT-like token for testing
func (p *TestProvider) generateTestIDToken() string {
	header := map[string]interface{}{
		"alg": "HS256",
		"typ": "JWT",
	}

	payload := map[string]interface{}{
		"iss": "test-oauth-provider",
		"aud": p.config.ClientID,
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
		"sub": "test-user",
	}

	headerBytes, _ := json.Marshal(header)
	payloadBytes, _ := json.Marshal(payload)

	// Simple base64-like encoding for test purposes
	return fmt.Sprintf("%x.%x.test-signature", headerBytes, payloadBytes)
}

// HandleTestAuthorize handles the test authorization endpoint
func HandleTestAuthorize(w http.ResponseWriter, r *http.Request) {
	// Extract parameters
	redirectURI := r.URL.Query().Get("redirect_uri")
	state := r.URL.Query().Get("state")
	
	// Generate a fake authorization code
	authCode := fmt.Sprintf("test_auth_code_%d", time.Now().Unix())
	
	// Build callback URL
	callbackURL, err := url.Parse(redirectURI)
	if err != nil {
		http.Error(w, "Invalid redirect URI", http.StatusBadRequest)
		return
	}
	
	params := url.Values{}
	params.Add("code", authCode)
	params.Add("state", state)
	callbackURL.RawQuery = params.Encode()
	
	// Redirect back to callback
	http.Redirect(w, r, callbackURL.String(), http.StatusFound)
}

// HandleTestToken handles the test token endpoint
func HandleTestToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse form data
	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	// Validate required parameters
	grantType := r.Form.Get("grant_type")
	code := r.Form.Get("code")
	clientID := r.Form.Get("client_id")

	if grantType != "authorization_code" || code == "" || clientID == "" {
		http.Error(w, "Invalid request parameters", http.StatusBadRequest)
		return
	}

	// Generate fake tokens
	accessToken := fmt.Sprintf("test_access_token_%d", time.Now().Unix())
	refreshToken := fmt.Sprintf("test_refresh_token_%d", time.Now().Unix())

	// Create response
	response := map[string]interface{}{
		"access_token":  accessToken,
		"token_type":    "Bearer",
		"expires_in":    3600,
		"refresh_token": refreshToken,
		"scope":         "profile email",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}