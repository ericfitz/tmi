//go:build dev || test

package auth

import (
	"context"
	"testing"
)

func TestTestProvider(t *testing.T) {
	config := OAuthProviderConfig{
		ID:               "test",
		Name:             "Test Provider",
		Enabled:          true,
		Icon:             "flask-vial",
		ClientID:         "test-client-id",
		ClientSecret:     "test-oauth-secret-12345",
		AuthorizationURL: "http://localhost:8080/oauth2/authorize?idp=test",
		TokenURL:         "http://localhost:8080/oauth2/token?idp=test",
		Scopes:           []string{"profile", "email"},
		EmailClaim:       "email",
		NameClaim:        "name",
		SubjectClaim:     "sub",
	}

	callbackURL := "http://localhost:8080/oauth2/callback"
	provider := NewTestProvider(config, callbackURL)

	// Test GetAuthorizationURL
	authURL := provider.GetAuthorizationURL("test-state-123")
	if authURL == "" {
		t.Error("Expected authorization URL, got empty string")
	}
	t.Logf("Authorization URL: %s", authURL)

	// Test ExchangeCode
	ctx := context.Background()
	tokenResponse, err := provider.ExchangeCode(ctx, "test-code")
	if err != nil {
		t.Errorf("ExchangeCode failed: %v", err)
	}
	if tokenResponse.AccessToken == "" {
		t.Error("Expected access token, got empty string")
	}
	t.Logf("Token Response: %+v", tokenResponse)

	// Test GetUserInfo
	userInfo, err := provider.GetUserInfo(ctx, tokenResponse.AccessToken)
	if err != nil {
		t.Errorf("GetUserInfo failed: %v", err)
	}
	if userInfo.Email == "" || userInfo.ID == "" {
		t.Error("Expected user info with email and ID")
	}
	if userInfo.Email != userInfo.ID+"@test.tmi" {
		t.Errorf("Expected email format %s@test.tmi, got %s", userInfo.ID, userInfo.Email)
	}
	t.Logf("User Info: %+v", userInfo)

	// Test ValidateIDToken
	claims, err := provider.ValidateIDToken(ctx, tokenResponse.IDToken)
	if err != nil {
		t.Errorf("ValidateIDToken failed: %v", err)
	}
	if claims.Subject == "" || claims.Email == "" {
		t.Error("Expected ID token claims with subject and email")
	}
	t.Logf("ID Token Claims: %+v", claims)
}

func TestNewProvider_Test(t *testing.T) {
	config := OAuthProviderConfig{
		ID:       "test",
		Name:     "Test Provider",
		Enabled:  true,
		ClientID: "test-client-id",
	}

	provider, err := NewProvider(config, "http://localhost:8080/oauth2/callback")
	if err != nil {
		t.Errorf("NewProvider failed: %v", err)
	}

	if provider == nil {
		t.Error("Expected provider instance, got nil")
	}

	// Check that it's a TestProvider
	if _, ok := provider.(*TestProvider); !ok {
		t.Errorf("Expected TestProvider, got %T", provider)
	}
}