//go:build !dev && !test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// newTestProvider returns an error in production builds
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	logger := slogging.Get()
	logger.Debug("Test provider requested in production build - not available provider_id=%v", config.ID)
	// Test provider is not available in production builds
	return nil
}
