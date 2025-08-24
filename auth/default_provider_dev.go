//go:build dev || test

package auth

// getDefaultProviderID returns the default OAuth provider ID for non-production builds
// In dev/test builds, we default to "test" provider when no idp parameter is provided
func getDefaultProviderID() string {
	return "test"
}