package envutil

import (
	"os"
	"strings"
)

// Get retrieves an environment variable with a fallback value.
// Returns the environment variable value if set and non-empty, otherwise returns the fallback.
// SEM@f7c112539bdb78e960d4a182be763184e41c531c: fetch an environment variable value with a fallback default (pure)
func Get(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists && value != "" {
		return value
	}
	return fallback
}

// ScanPrefixedMap returns a map of every environment variable whose name starts
// with prefix, keyed by the lowercased remainder of the name and valued by the
// variable's value. For example, with prefix
// "OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_":
//
//	OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_SUBJECT_CLAIM=sub  -> {"subject_claim": "sub"}
//	OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_EMAIL_CLAIM=email  -> {"email_claim": "email"}
//
// Returns a non-nil empty map when nothing matches. Used for OAuth userinfo
// claim mappings and additional OAuth parameters, which are dynamic-cardinality
// (the concrete keys are only known at runtime).
// SEM@33c446dc529c7bbdd5753f7eb5d6fb76e8f6ae6c: scan environment variables under a prefix into a lowercase suffix-to-value map (reads env)
func ScanPrefixedMap(prefix string) map[string]string {
	out := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if suffix, ok := strings.CutPrefix(parts[0], prefix); ok && suffix != "" {
			out[strings.ToLower(suffix)] = parts[1]
		}
	}
	return out
}

// DiscoverProviders scans environment variables to find configured providers.
// It looks for environment variables matching the pattern: <prefix><PROVIDER_ID><suffix>
// For example, with prefix="SAML_PROVIDERS_" and suffix="_ENABLED",
// it will find "ENTRA_TMIDEV_SAML" from "SAML_PROVIDERS_ENTRA_TMIDEV_SAML_ENABLED=true"
// SEM@33c446dc529c7bbdd5753f7eb5d6fb76e8f6ae6c: scan environment variables to find provider IDs matching a prefix/suffix pattern (pure)
func DiscoverProviders(prefix, suffix string) []string {
	providerIDs := make([]string, 0)
	seen := make(map[string]bool)

	// Scan all environment variables
	for _, env := range os.Environ() {
		// Split into key=value
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]

		// Check if this key matches our pattern
		if strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) {
			// Extract provider ID by removing prefix and suffix
			providerID := strings.TrimPrefix(key, prefix)
			providerID = strings.TrimSuffix(providerID, suffix)

			// Add to list if not already seen
			if providerID != "" && !seen[providerID] {
				providerIDs = append(providerIDs, providerID)
				seen[providerID] = true
			}
		}
	}

	return providerIDs
}

// ProviderIDToKey converts an environment variable provider ID to a provider key.
// It converts to lowercase and replaces underscores with hyphens.
// For example: "ENTRA_TMIDEV_SAML" -> "entra-tmidev-saml"
// SEM@33c446dc529c7bbdd5753f7eb5d6fb76e8f6ae6c: convert an uppercase env-var provider ID to a lowercase hyphenated key (pure)
func ProviderIDToKey(providerID string) string {
	// Convert to lowercase
	key := strings.ToLower(providerID)
	// Replace underscores with hyphens
	key = strings.ReplaceAll(key, "_", "-")
	return key
}
