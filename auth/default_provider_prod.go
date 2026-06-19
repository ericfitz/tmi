//go:build !dev && !test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// getDefaultProviderID returns the default OAuth provider ID for production builds
// In production builds, we require explicit idp parameter - no defaults
// SEM@70ff47b7829f38ef04399520210ae8765d39495d: return empty string in production builds, requiring an explicit OAuth provider parameter (pure)
func getDefaultProviderID() string {
	logger := slogging.Get()
	logger.Debug("No default provider ID in production builds - explicit idp parameter required")
	return ""
}
