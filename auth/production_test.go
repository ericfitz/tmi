//go:build !dev && !test

package auth

import (
	"context"
	"os"
	"testing"
)

func TestTMIProviderAvailableInProduction(t *testing.T) {
	// TMI provider should be available in all builds (including production)
	// Both "test" and "tmi" provider IDs are accepted as aliases

	testCases := []struct {
		name       string
		providerID string
	}{
		{"test alias", "test"},
		{"tmi provider", "tmi"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := OAuthProviderConfig{
				ID:       tc.providerID,
				Name:     "TMI Provider",
				Enabled:  true,
				ClientID: "test-client-id",
				TokenURL: "http://localhost:8080/oauth2/token",
			}

			provider, err := NewProvider(config, "http://localhost:8080/oauth2/callback")

			if err != nil {
				t.Errorf("Expected TMI provider to be available in production, but got error: %v", err)
			}
			if provider == nil {
				t.Error("Expected non-nil provider for TMI provider in production")
			}
		})
	}
}

func TestAuthorizationCodeFlowRestrictedInProduction(t *testing.T) {
	// Authorization Code flow should be restricted in production builds
	// This is controlled by TMI_BUILD_MODE environment variable

	// Ensure we're simulating production mode
	originalMode := os.Getenv("TMI_BUILD_MODE")
	_ = os.Setenv("TMI_BUILD_MODE", "production")
	defer func() { _ = os.Setenv("TMI_BUILD_MODE", originalMode) }()

	config := OAuthProviderConfig{
		ID:       "tmi",
		Name:     "TMI Provider",
		Enabled:  true,
		ClientID: "test-client-id",
		TokenURL: "http://localhost:8080/oauth2/token",
	}

	provider, err := NewProvider(config, "http://localhost:8080/oauth2/callback")
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Attempt to use Authorization Code flow (should fail in production)
	testProvider, ok := provider.(*TestProvider)
	if !ok {
		t.Fatal("Expected TestProvider type")
	}

	ctx := context.Background()
	_, err = testProvider.ExchangeCode(ctx, "test-code")

	if err == nil {
		t.Error("Expected error when using Authorization Code flow in production, but no error occurred")
	}
	// Note: The actual error check depends on the implementation in test_provider.go
}
