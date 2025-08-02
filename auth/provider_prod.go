//go:build !dev && !test

package auth

// newTestProvider returns an error in production builds
func newTestProvider(config OAuthProviderConfig, callbackURL string) Provider {
	// Test provider is not available in production builds
	panic("test provider not available in production builds")
}