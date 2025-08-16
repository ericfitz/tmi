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
		Scopes:       config.Scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:  config.AuthorizationURL,
			TokenURL: config.TokenURL,
		},
	}

	httpClient := &http.Client{Timeout: 10 * time.Second}
	
	return &TestProvider{
		BaseProvider: &BaseProvider{
			config:       config,
			oauth2Config: baseConfig,
			httpClient:   httpClient,
		},
		clientSecret: testSecret,
	}
}

// GetAuthorizationURL returns the test authorization URL
// For the test provider, we'll create a direct callback URL instead of an external redirect
func (p *TestProvider) GetAuthorizationURL(state string) string {
	// For test provider, generate a fake auth code and redirect directly to callback
	authCode := fmt.Sprintf("test_auth_code_%d", time.Now().Unix())
	
	callbackURL := p.oauth2Config.RedirectURL
	if callbackURL == "" {
		// Fallback to default callback
		callbackURL = "http://localhost:8080/auth/callback"
	}
	
	// Parse the callback URL and add query parameters
	if parsedURL, err := url.Parse(callbackURL); err == nil {
		params := url.Values{}
		params.Add("code", authCode)
		params.Add("state", state)
		parsedURL.RawQuery = params.Encode()
		return parsedURL.String()
	}
	
	// Fallback if URL parsing fails
	return fmt.Sprintf("%s?code=%s&state=%s", callbackURL, authCode, state)
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

