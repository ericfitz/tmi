package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ErrAuthRequired indicates the caller has no valid token for this provider.
var ErrAuthRequired = errors.New("delegated source: authentication required")

// ErrTransient indicates a temporary failure (5xx / network) during refresh.
var ErrTransient = errors.New("delegated source: transient refresh failure")

// errNoRefreshToken is the LastError message stored when a token refresh is
// attempted but no refresh token is available. Shared across refresh helpers
// to satisfy goconst.
const errNoRefreshToken = "no refresh token available"

// DelegatedSourceDoFetch is the callback concrete delegated sources implement.
// It receives the plaintext access token and the URI to fetch, and returns the
// raw bytes and content-type of the fetched resource.
type DelegatedSourceDoFetch func(ctx context.Context, accessToken, uri string) (data []byte, contentType string, err error)

// DelegatedSource is a reusable helper that concrete delegated content sources
// (e.g. Confluence, Google Workspace) embed to handle token lookup, skew-aware
// expiry detection, lazy refresh with SELECT … FOR UPDATE serialization, status
// transitions, and error propagation (ErrAuthRequired, ErrTransient).
type DelegatedSource struct {
	// ProviderID is the OAuth provider identifier (e.g. "confluence").
	ProviderID string
	// Tokens is the repository used to look up and refresh tokens.
	Tokens ContentTokenRepository
	// Registry is used to look up the OAuth provider by ProviderID.
	Registry *ContentOAuthProviderRegistry
	// DoFetch is the provider-specific fetch callback. It receives the
	// plaintext access token and URI and returns raw bytes + content-type.
	DoFetch DelegatedSourceDoFetch
	// Skew is the time added to now() when comparing against ExpiresAt to
	// proactively refresh tokens about to expire. Defaults to 30s when zero.
	Skew time.Duration
}

// FetchForUser fetches the resource at uri on behalf of userID.
//
// Error semantics:
//   - ErrAuthRequired: no token, or token status is failed_refresh. The caller
//     should redirect the user to re-authorize.
//   - ErrTransient: refresh failed with a transient (5xx/network) error. The
//     caller may retry.
//   - Any other error: propagated from DoFetch.
func (d *DelegatedSource) FetchForUser(ctx context.Context, userID, uri string) ([]byte, string, error) {
	log := slogging.Get()

	tok, err := d.Tokens.GetByUserAndProvider(ctx, userID, d.ProviderID)
	if err != nil {
		if errors.Is(err, ErrContentTokenNotFound) {
			log.Debug("delegated_source: no token for user=%s provider=%s", userID, d.ProviderID)
			return nil, "", ErrAuthRequired
		}
		return nil, "", err
	}

	// Short-circuit: token is already in a permanently failed state.
	if tok.Status == ContentTokenStatusFailedRefresh {
		log.Debug("delegated_source: token status=failed_refresh user=%s provider=%s", userID, d.ProviderID)
		return nil, "", ErrAuthRequired
	}

	// Lazy refresh if token is expired (or about to expire within skew window).
	if d.expired(tok) {
		log.Debug("delegated_source: token expired, refreshing user=%s provider=%s", userID, d.ProviderID)
		tok, err = d.refresh(ctx, tok.ID)
		if err != nil {
			return nil, "", err
		}
	}

	data, contentType, err := d.DoFetch(ctx, tok.AccessToken, uri)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

// expired returns true when the token has an expiry time that is within the
// skew window of the current time. If ExpiresAt is nil (no expiry supplied by
// the provider), the token is treated as valid indefinitely.
func (d *DelegatedSource) expired(t *ContentToken) bool {
	if t.ExpiresAt == nil {
		return false
	}
	skew := d.Skew
	if skew == 0 {
		skew = 30 * time.Second
	}
	return time.Now().Add(skew).After(*t.ExpiresAt)
}

// refresh uses RefreshWithLock to serialize concurrent refresh attempts for
// the same token. It returns the updated ContentToken or one of ErrAuthRequired
// / ErrTransient.
//
// Transaction semantics: RefreshWithLock rolls back the transaction if the fn
// callback returns a non-nil error. To persist a failed_refresh status change
// atomically while still signalling ErrAuthRequired to the caller, we use the
// "return nil-error + mutated token" pattern inside fn: when the refresh is
// permanently invalid we flip tok.Status to failed_refresh and return (tok,
// nil) so the row is committed, then return ErrAuthRequired from this function.
func (d *DelegatedSource) refresh(ctx context.Context, tokenID string) (*ContentToken, error) {
	log := slogging.Get()

	provider, ok := d.Registry.Get(d.ProviderID)
	if !ok {
		log.Warn("delegated_source: provider not registered provider=%s", d.ProviderID)
		return nil, ErrAuthRequired
	}

	var permanentFailure bool
	var transientFailure bool

	updated, err := d.Tokens.RefreshWithLock(ctx, tokenID, func(current *ContentToken) (*ContentToken, error) {
		// Re-check expiry inside the lock: another goroutine may have already
		// refreshed the token between our initial check and acquiring the lock.
		if !d.expired(current) {
			log.Debug("delegated_source: token already refreshed by peer, skipping provider call provider=%s", d.ProviderID)
			return current, nil
		}

		// No refresh token — cannot refresh; flip to failed_refresh and commit.
		if current.RefreshToken == "" {
			log.Warn("delegated_source: no refresh token available, marking failed provider=%s", d.ProviderID)
			current.Status = ContentTokenStatusFailedRefresh
			current.LastError = errNoRefreshToken
			permanentFailure = true
			// Return (current, nil) so the transaction commits the status change.
			return current, nil
		}

		resp, refreshErr := provider.Refresh(ctx, current.RefreshToken)
		if refreshErr != nil {
			if IsContentOAuthPermanentFailure(refreshErr) {
				log.Warn("delegated_source: permanent refresh failure, marking failed provider=%s err=%s", d.ProviderID, refreshErr)
				current.Status = ContentTokenStatusFailedRefresh
				current.LastError = refreshErr.Error()
				permanentFailure = true
				// Commit the status change, then signal failure to the caller.
				return current, nil
			}
			// Transient: do NOT persist any change; let the tx roll back.
			log.Warn("delegated_source: transient refresh failure provider=%s err=%s", d.ProviderID, refreshErr)
			transientFailure = true
			return nil, fmt.Errorf("delegated_source refresh transient: %w", refreshErr)
		}

		// Success: update all token fields.
		now := time.Now()
		current.AccessToken = resp.AccessToken
		if resp.RefreshToken != "" {
			current.RefreshToken = resp.RefreshToken
		}
		if resp.Scope != "" {
			current.Scopes = resp.Scope
		}
		current.ExpiresAt = resp.ExpiresAt()
		current.LastRefreshAt = &now
		current.LastError = ""
		current.Status = ContentTokenStatusActive
		return current, nil
	})

	if permanentFailure {
		return nil, ErrAuthRequired
	}
	if transientFailure {
		return nil, ErrTransient
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}
