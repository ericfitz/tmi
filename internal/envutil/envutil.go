package envutil

import (
	"os"
)

// Get retrieves an environment variable with a fallback value.
// Returns the environment variable value if set and non-empty, otherwise returns the fallback.
func Get(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}
