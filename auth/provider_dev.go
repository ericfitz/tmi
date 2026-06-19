//go:build dev || test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// newTestProvider creates the TMI provider (only available in dev/test builds)
// SEM@f61409f5a075256147e289bd78059fcd6be5886e: build a test OAuth provider for dev/test builds using the given config and callback URL (pure)
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	logger := slogging.Get()
	logger.Info("Creating TMI provider for dev/test build provider_id=%v", config.ID)
	return NewTestProvider(config, callbackURL)
}
