package auth

import (
	"context"
)

// RuntimeConfigReader supplies operational config values that the auth
// handlers need at request time. The values are DB-backed via the
// SettingsService introduced in #415; this interface lets the auth package
// read them without importing internal/config or api (both of which would
// create import cycles).
//
// Implementations should be cheap to call per-request (Get() backed by a
// short TTL cache in SettingsService is fine). A nil reader is interpreted
// by handlers as "no DB available" and they fall back to the YAML snapshot
// in h.config — that path exists for unit tests and for the brief window
// during startup before SetRuntimeConfigReader is wired.
//
// See #419 for the motivation. Once every cfg.* operational read in the
// auth package has been moved here, the YAML snapshot fallback can be
// removed and h.config.OAuth.* / h.config.SAML.* trimmed to bootstrap-only
// fields.
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: interface for reading live auth configuration from the DB at request time (reads DB)
type RuntimeConfigReader interface {
	// GetClientCallbackAllowList returns the configured allowlist for the
	// /oauth2/authorize and /oauth2/step_up client_callback parameter.
	//
	// Three outcomes:
	//   - exists == false: no DB row. Caller falls back to the YAML
	//     snapshot, which preserves first-run dev workflows.
	//   - exists == true,  err == nil: returns the parsed allowlist.
	//   - exists == true,  err != nil: DB row is present but unusable
	//     (corrupt JSON, decryption failure, etc.). Caller MUST treat
	//     this as fail-closed (reject every client_callback) — silently
	//     falling back to YAML would defeat the open-redirect mitigation.
	GetClientCallbackAllowList(ctx context.Context) (list []string, exists bool, err error)

	// IsSAMLEnabled reports whether SAML auth is enabled.
	IsSAMLEnabled(ctx context.Context) bool

	// GetOAuthCallbackURL returns the configured OAuth callback URL used
	// when redirecting back from an external provider.
	GetOAuthCallbackURL(ctx context.Context) string
}
