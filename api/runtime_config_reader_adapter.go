package api

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/slogging"
)

// RuntimeConfigReaderAdapter adapts SettingsServiceInterface to
// auth.RuntimeConfigReader so the auth package can read DB-backed
// operational config at request time without importing api or
// internal/config (#419).
type RuntimeConfigReaderAdapter struct {
	settings SettingsServiceInterface
}

// NewRuntimeConfigReaderAdapter constructs an adapter over the given
// SettingsService.
func NewRuntimeConfigReaderAdapter(settings SettingsServiceInterface) *RuntimeConfigReaderAdapter {
	return &RuntimeConfigReaderAdapter{settings: settings}
}

// GetClientCallbackAllowList reads the operator-configured client_callback
// allowlist for /oauth2/authorize and /oauth2/step_up. The DB row holds a
// JSON-encoded []string. Returns (list, exists, err) per the interface
// contract:
//   - exists=false: no DB row → caller falls back to YAML.
//   - exists=true, err==nil: parsed allowlist returned.
//   - exists=true, err!=nil: DB row present but unusable → caller MUST
//     fail-closed to prevent open-redirect against a corrupt row.
func (a *RuntimeConfigReaderAdapter) GetClientCallbackAllowList(ctx context.Context) ([]string, bool, error) {
	raw, err := a.settings.GetString(ctx, "auth.oauth.client_callback_allowlist")
	if err != nil {
		slogging.Get().Warn("RuntimeConfigReader: failed to read auth.oauth.client_callback_allowlist: %v", err)
		// A read error is not the same as a missing row; treat as
		// "exists, but unusable" so the caller fails-closed.
		return nil, true, err
	}
	if raw == "" {
		return nil, false, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		slogging.Get().Warn("RuntimeConfigReader: auth.oauth.client_callback_allowlist is not valid JSON: %v", err)
		return nil, true, err
	}
	return list, true, nil
}

// IsSAMLEnabled reads features.saml_enabled. A read error or missing row
// returns false (fail-closed).
func (a *RuntimeConfigReaderAdapter) IsSAMLEnabled(ctx context.Context) bool {
	raw, err := a.settings.GetString(ctx, "features.saml_enabled")
	if err != nil || raw == "" {
		return false
	}
	v, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		slogging.Get().Warn("RuntimeConfigReader: features.saml_enabled is not a valid bool (%q): %v", raw, parseErr)
		return false
	}
	return v
}

// GetOAuthCallbackURL reads auth.oauth_callback_url. An empty string is
// returned on error/missing row; the caller falls back to the YAML
// snapshot in h.config.OAuth.CallbackURL.
func (a *RuntimeConfigReaderAdapter) GetOAuthCallbackURL(ctx context.Context) string {
	raw, err := a.settings.GetString(ctx, "auth.oauth_callback_url")
	if err != nil {
		slogging.Get().Warn("RuntimeConfigReader: failed to read auth.oauth_callback_url: %v", err)
		return ""
	}
	return raw
}

// Compile-time check that the adapter satisfies the auth interface.
var _ auth.RuntimeConfigReader = (*RuntimeConfigReaderAdapter)(nil)
