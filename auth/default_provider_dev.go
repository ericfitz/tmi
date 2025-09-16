//go:build dev || test

package auth

import "github.com/ericfitz/tmi/internal/slogging"

// getDefaultProviderID returns the default OAuth provider ID for non-production builds
// In dev/test builds, we default to "test" provider when no idp parameter is provided
func getDefaultProviderID() string {
	logger := slogging.Get()
	logger.Debug("Using default test provider for dev/test builds default_provider_id=%v", "test")
	return "test"
}
