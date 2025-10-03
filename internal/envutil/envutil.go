package envutil

import "os"

// Get retrieves an environment variable with automatic TMI_ prefix fallback.
// It checks for the environment variable in this order:
// 1. Exact key as provided
// 2. Key with TMI_ prefix
// 3. Returns fallback if neither exists
//
// This supports both Heroku-style (TMI_ prefixed) and local dev (unprefixed) configurations.
func Get(key, fallback string) string {
	// Try exact key first (supports both prefixed and unprefixed)
	if value, exists := os.LookupEnv(key); exists {
		return value
	}

	// Try with TMI_ prefix if not already prefixed
	if len(key) < 4 || key[:4] != "TMI_" {
		if value, exists := os.LookupEnv("TMI_" + key); exists {
			return value
		}
	}

	return fallback
}
