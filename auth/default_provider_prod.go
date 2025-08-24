//go:build !dev && !test

package auth

// getDefaultProviderID returns the default OAuth provider ID for production builds
// In production builds, we require explicit idp parameter - no defaults
func getDefaultProviderID() string {
	return ""
}
