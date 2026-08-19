package api

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ericfitz/tmi/auth"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
)

// RuntimeConfigReaderAdapter adapts SettingsServiceInterface to
// auth.RuntimeConfigReader so the auth package can read DB-backed
// operational config at request time without importing api or
// internal/config (#419).
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: adapter bridging SettingsService to the auth package's RuntimeConfigReader interface (pure)
type RuntimeConfigReaderAdapter struct {
	settings SettingsServiceInterface
}

// NewRuntimeConfigReaderAdapter constructs an adapter over the given
// SettingsService.
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: build a RuntimeConfigReaderAdapter over the given settings service (pure)
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
//
// The read is database-only (GetDatabaseString): for #419 runtime keys the
// DB row must win over the YAML config, which acts purely as a first-run
// fallback. Reading through GetString would invert that — its
// env/config-file priority silently shadows the DB row whenever the YAML
// also carries the key (#767).
//
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: fetch the OAuth client callback allowlist from DB settings only; fail-closed on corrupt row (reads DB)
func (a *RuntimeConfigReaderAdapter) GetClientCallbackAllowList(ctx context.Context) ([]string, bool, error) {
	raw, exists, err := a.settings.GetDatabaseString(ctx, "auth.oauth.client_callback_allowlist")
	if err != nil {
		// A TRANSIENT DB fault (ADB idle-session kill, pool exhaustion,
		// listener blip — ORA-02396/03113/12537 class) must NOT fail closed:
		// before #767 an env/YAML-configured allowlist never touched the DB,
		// and turning every ADB hiccup into "reject all logins" would be an
		// Oracle-only outage PG testing cannot reproduce. The YAML snapshot
		// is operator-supplied, not attacker-controlled, so falling back to
		// it while the DB is unreachable is strictly safer than rejecting
		// every client_callback (oracle-db-admin review of #767, note 1).
		if errors.Is(dberrors.Classify(err), dberrors.ErrTransient) {
			slogging.Get().Warn("RuntimeConfigReader: transient DB error reading auth.oauth.client_callback_allowlist; falling back to the YAML snapshot: %v", err)
			return nil, false, nil
		}
		slogging.Get().Warn("RuntimeConfigReader: failed to read auth.oauth.client_callback_allowlist: %v", err)
		// A non-transient read error is not the same as a missing row; treat
		// as "exists, but unusable" so the caller fails-closed.
		return nil, true, err
	}
	if !exists || raw == "" {
		return nil, false, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		slogging.Get().Warn("RuntimeConfigReader: auth.oauth.client_callback_allowlist is not valid JSON: %v", err)
		return nil, true, err
	}
	return list, true, nil
}

// IsSAMLEnabled reads features.saml_enabled, DB row first (#419/#767).
// When no DB row exists it falls back to GetString (env/config file) —
// unlike the other two readers, the auth handler has no YAML fallback of
// its own once a RuntimeConfigReader is wired. A read error or garbage
// value returns false (fail-closed).
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: check whether SAML login is enabled, DB-first with config fallback; fail-closed on error (reads DB)
func (a *RuntimeConfigReaderAdapter) IsSAMLEnabled(ctx context.Context) bool {
	raw, exists, err := a.settings.GetDatabaseString(ctx, "features.saml_enabled")
	if err != nil {
		// Transient DB faults fall through to the config-layer fallback below
		// (same reasoning as GetClientCallbackAllowList): an ADB blip must not
		// report SAML disabled where the YAML enables it (oracle-db-admin
		// review of #767, note 1). Non-transient errors stay fail-closed.
		if !errors.Is(dberrors.Classify(err), dberrors.ErrTransient) {
			return false
		}
		slogging.Get().Warn("RuntimeConfigReader: transient DB error reading features.saml_enabled; falling back to the config layer: %v", err)
		exists = false
		raw = ""
	}
	if !exists || raw == "" {
		// No DB row — fall back to the config layer (env > YAML).
		var cfgErr error
		raw, cfgErr = a.settings.GetString(ctx, "features.saml_enabled")
		if cfgErr != nil || raw == "" {
			return false
		}
	}
	v, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		slogging.Get().Warn("RuntimeConfigReader: features.saml_enabled is not a valid bool (%q): %v", raw, parseErr)
		return false
	}
	return v
}

// GetOAuthCallbackURL reads auth.oauth_callback_url from the database only
// (#419/#767). An empty string is returned on error/missing row; the
// caller falls back to the YAML snapshot in h.config.OAuth.CallbackURL.
// SEM@08e19a77d4d2c499f116e1a1ee3c875c06407335: fetch the OAuth callback URL from DB settings only; return empty string on error (reads DB)
func (a *RuntimeConfigReaderAdapter) GetOAuthCallbackURL(ctx context.Context) string {
	raw, exists, err := a.settings.GetDatabaseString(ctx, "auth.oauth_callback_url")
	if err != nil {
		slogging.Get().Warn("RuntimeConfigReader: failed to read auth.oauth_callback_url: %v", err)
		return ""
	}
	// raw == "" guarded here, not just at the caller: on Oracle '' binds as
	// NULL, so an empty-but-present row is the ''-is-NULL asymmetry surfacing
	// — keep the collapse in the adapter alongside the other two readers so
	// the invariant does not depend on a caller two packages away
	// (oracle-db-admin review of #767, note 3).
	if !exists || raw == "" {
		return ""
	}
	return raw
}

// Compile-time check that the adapter satisfies the auth interface.
var _ auth.RuntimeConfigReader = (*RuntimeConfigReaderAdapter)(nil)
