//go:build dev || test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// newTestProvider creates a test provider (only available in dev/test builds)
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	logger := slogging.Get()
	logger.Info("Creating test provider for dev/test build provider_id=%v", config.ID)
	return NewTestProvider(config, callbackURL)
}
