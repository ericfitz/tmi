//go:build !dev && !test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// newTestProvider creates the TMI provider in production builds
// In production: Only Client Credentials Grant is supported (Authorization Code flow disabled)
// SEM@2e1e229947d57021bf27a7c51c052e3e2a18c98e: build the TMI OAuth provider restricted to client credentials grant in production builds (pure)
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	logger := slogging.Get()
	logger.Info("Creating TMI provider for production build (Client Credentials Grant only) provider_id=%v", config.ID)
	return NewTestProvider(config, callbackURL)
}
