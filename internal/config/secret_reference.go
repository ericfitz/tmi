package config

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Secret-reference scheme prefixes. A secret config value whose string begins
// with one of these is a LOCATOR, dereferenced at load time by
// ResolveSecretValue. A value with no recognized scheme prefix is an INLINE
// value and is returned unchanged — including values that contain "://" but
// use an unrelated scheme (e.g. a "postgres://" connection URL).
const (
	schemeVault = "vault://"
	schemeEnv   = "env://"
	schemeFile  = "file://"
)

// SecretResolver dereferences a vault:// secret locator. It is implemented in a
// package that can import internal/secrets; internal/config cannot import
// internal/secrets directly (that would create an import cycle, because
// internal/secrets imports internal/config for config.SecretsConfig).
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: interface for dereferencing a vault:// secret path to its plaintext value (pure)
type SecretResolver interface {
	// ResolveVault returns the secret value stored at the given vault path.
	ResolveVault(ctx context.Context, path string) (string, error)
}

// IsSecretReference reports whether value is a secret-reference locator
// (vault://, env://, or file://) rather than an inline secret literal.
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: report whether a config value is a secret-reference locator rather than an inline literal (pure)
func IsSecretReference(value string) bool {
	return strings.HasPrefix(value, schemeVault) ||
		strings.HasPrefix(value, schemeEnv) ||
		strings.HasPrefix(value, schemeFile)
}

// ResolveSecretValue dereferences a secret config value.
//
//   - An inline value (no recognized scheme:// prefix) is returned unchanged.
//     A value such as "postgres://..." is inline — postgres is not a
//     reference scheme.
//   - env://NAME resolves to the value of environment variable NAME; an unset
//     or empty variable is an error.
//   - file://PATH reads PATH and returns its whitespace-trimmed contents.
//   - vault://PATH dereferences PATH through the supplied SecretResolver. A nil
//     resolver is an error (a vault reference requires a configured secrets
//     provider).
//
// Resolution failures are returned as typed errors; this function never panics.
// SEM@b583a71af02ca00e2c408d9d52e1e41f514df3ff: dereference a vault://, env://, or file:// secret locator to its plaintext value (reads env/file/vault)
func ResolveSecretValue(ctx context.Context, value string, vault SecretResolver) (string, error) {
	switch {
	case strings.HasPrefix(value, schemeVault):
		path := strings.TrimPrefix(value, schemeVault)
		if vault == nil {
			return "", fmt.Errorf("vault reference %q requires a configured secrets provider", value)
		}
		resolved, err := vault.ResolveVault(ctx, path)
		if err != nil {
			return "", fmt.Errorf("resolving vault reference %q: %w", value, err)
		}
		return resolved, nil

	case strings.HasPrefix(value, schemeEnv):
		name := strings.TrimPrefix(value, schemeEnv)
		v := os.Getenv(name)
		if v == "" {
			return "", fmt.Errorf("env reference %s resolved to empty", name)
		}
		return v, nil

	case strings.HasPrefix(value, schemeFile):
		path := strings.TrimPrefix(value, schemeFile)
		data, err := os.ReadFile(path) // #nosec G304 -- path comes from operator-controlled bootstrap config
		if err != nil {
			return "", fmt.Errorf("reading file reference %q: %w", value, err)
		}
		return strings.TrimSpace(string(data)), nil

	default:
		// No recognized scheme prefix: an inline secret literal. A value may
		// still contain "://" (e.g. a "postgres://" connection URL) — that is
		// inline, not a malformed reference.
		return value, nil
	}
}
