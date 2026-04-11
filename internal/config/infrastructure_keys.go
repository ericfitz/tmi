package config

import "strings"

// infrastructureKeyPrefixes lists setting key prefixes that must always be read
// from config file or environment variables, never from the database.
//
// These settings are consumed during server startup before the settings service
// is initialized, or represent circular dependencies (e.g., database connection
// settings cannot be read from the database).
//
// See docs/superpowers/specs/2026-04-10-unified-seeder-design.md for the full
// startup phase analysis that derived this list.
var infrastructureKeyPrefixes = []string{
	"logging.",
	"observability.",
	"database.",
	"server.port",
	"server.interface",
	"server.tls_",
	"server.cors.",
	"server.trusted_proxies",
	"server.http_to_https_redirect",
	"server.read_timeout",
	"server.write_timeout",
	"server.idle_timeout",
	"secrets.",
	"auth.build_mode",
	"auth.jwt.secret",
	"auth.jwt.signing_method",
}

// infrastructureKeyExact lists setting keys that are exact matches
// (not prefix-based) for infrastructure keys.
var infrastructureKeyExact = []string{
	"administrators",
}

// IsInfrastructureKey returns true if the given setting key is an infrastructure
// key that must always be read from config file or environment variables.
func IsInfrastructureKey(key string) bool {
	for _, exact := range infrastructureKeyExact {
		if key == exact {
			return true
		}
	}
	for _, prefix := range infrastructureKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
