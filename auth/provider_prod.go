//go:build !dev && !test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// newTestProvider creates the TMI provider in production builds
// In production: Only Client Credentials Grant is supported (Authorization Code flow disabled)
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	logger := slogging.Get()
	logger.Info("Creating TMI provider for production build (Client Credentials Grant only) provider_id=%v", config.ID)
	return NewTestProvider(config, callbackURL)
}
