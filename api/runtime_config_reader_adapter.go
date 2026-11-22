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
//
// The read goes through GetResolvedString, which applies the converged
// precedence rule (#794). It used to be database-only, because for #419
// runtime keys the DB row must win over the YAML — and reading through
// GetString would have inverted that, since its env/config-file priority
// shadows the DB row whenever the YAML also carries the key (#767).
// GetResolvedString keeps that property for any row an operator actually
// set, while letting an explicit config value beat a merely-seeded default.
func (a *RuntimeConfigReaderAdapter) GetClientCallbackAllowList(ctx context.Context) ([]string, bool, error) {
	raw, exists, err := a.settings.GetResolvedString(ctx, "auth.oauth.client_callback_allowlist")
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

// IsEveryoneAReviewer reads auth.everyone_is_a_reviewer by the converged
// precedence rule (#794). Fail-closed: any read error, missing value, or
// unparseable value returns false.
//
// This key previously had no runtime reader at all — its only consumer read
// the config struct field directly, so the database row was display-only
// drift and editing it did nothing. It is the fifth and last of the keys
// #794 catalogued as following inconsistent precedence.
func (a *RuntimeConfigReaderAdapter) IsEveryoneAReviewer(ctx context.Context) bool {
	raw, exists, err := a.settings.GetResolvedString(ctx, "auth.everyone_is_a_reviewer")
	if err != nil {
		slogging.Get().Warn("RuntimeConfigReader: failed to read auth.everyone_is_a_reviewer: %v", err)
		return false
	}
	if !exists || raw == "" {
		return false
	}
	v, parseErr := strconv.ParseBool(raw)
	if parseErr != nil {
		slogging.Get().Warn("RuntimeConfigReader: auth.everyone_is_a_reviewer is not a valid bool (%q): %v", raw, parseErr)
		return false
	}
	return v
}

// Compile-time check that the adapter satisfies the auth interface.
var _ auth.RuntimeConfigReader = (*RuntimeConfigReaderAdapter)(nil)
