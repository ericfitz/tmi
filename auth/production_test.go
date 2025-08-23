//go:build !dev && !test

package auth

import (
	"strings"
	"testing"
)

func TestTestProviderNotAvailableInProduction(t *testing.T) {
	config := OAuthProviderConfig{
		ID:       "test",
		Name:     "Test Provider",
		Enabled:  true,
		ClientID: "test-client-id",
	}

	// This should return an error in production builds (not panic)
	provider, err := NewProvider(config, "http://localhost:8080/oauth2/callback")

	if err == nil {
		t.Error("Expected error when using test provider in production, but no error occurred")
	}
	if provider != nil {
		t.Error("Expected nil provider when test provider is not available in production")
	}
	if !strings.Contains(err.Error(), "not available in production") {
		t.Errorf("Expected error to contain 'not available in production', got: %s", err.Error())
	}
}
