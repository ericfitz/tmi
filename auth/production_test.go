//go:build !dev && !test

package auth

import (
	"testing"
)

func TestTestProviderNotAvailableInProduction(t *testing.T) {
	config := OAuthProviderConfig{
		ID:       "test",
		Name:     "Test Provider",
		Enabled:  true,
		ClientID: "test-client-id",
	}

	// This should panic in production builds
	defer func() {
		if r := recover(); r == nil {
			t.Error("Expected panic when using test provider in production, but no panic occurred")
		}
	}()

	NewProvider(config, "http://localhost:8080/auth/callback")
}