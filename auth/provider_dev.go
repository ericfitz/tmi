//go:build dev || test

package auth

// newTestProvider creates a test provider (only available in dev/test builds)
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	return NewTestProvider(config, callbackURL)
}