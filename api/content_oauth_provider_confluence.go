package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// ConfluenceContentOAuthProvider wraps BaseContentOAuthProvider to upgrade
// the account label using Atlassian's /oauth/token/accessible-resources
// endpoint. The base provider's /me call already populates account_id and a
// reasonable fallback label (email or display name); this wrapper additionally
// sets the label to the first matched site URL when accessible-resources
// returns one or more entries, which is more useful for users with multiple
// Atlassian instances.
//
// Authorization URL, token exchange, refresh, revoke, and required-scopes
// behavior are all delegated to the base provider unchanged.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: Confluence OAuth provider that enriches account info with Atlassian accessible-resources site URL
type ConfluenceContentOAuthProvider struct {
	base    *BaseContentOAuthProvider
	client  *SafeHTTPClient
	apiBase string
}

// NewConfluenceContentOAuthProvider wraps base with Confluence-specific
// account-info enrichment. validator MUST be non-nil and is used to validate
// outbound calls to api.atlassian.com (or operator-overridden test stubs).
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: build a ConfluenceContentOAuthProvider wrapping a base provider with SSRF-validated HTTP client (pure)
func NewConfluenceContentOAuthProvider(base *BaseContentOAuthProvider, validator *URIValidator) *ConfluenceContentOAuthProvider {
	return &ConfluenceContentOAuthProvider{
		base: base,
		client: NewSafeHTTPClient(
			validator,
			WithDefaultTimeouts(30*time.Second, 10*time.Second, confluenceMaxBodySize),
		),
		apiBase: confluenceAPIBase,
	}
}

// ID returns the wrapped provider's id (always "confluence" in practice; the
// registry chooses this implementation by id).
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: return the provider identifier from the wrapped base provider (pure)
func (p *ConfluenceContentOAuthProvider) ID() string { return p.base.ID() }

// AuthorizationURL delegates to the base provider, which appends any
// configured ExtraAuthorizeParams (e.g. audience=api.atlassian.com).
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: build the Confluence OAuth authorization URL, delegating to the base provider (pure)
func (p *ConfluenceContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	return p.base.AuthorizationURL(state, pkceChallenge, redirectURI)
}

// ExchangeCode delegates to the base provider.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: exchange an authorization code for Confluence OAuth tokens via the base provider
func (p *ConfluenceContentOAuthProvider) ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error) {
	return p.base.ExchangeCode(ctx, code, pkceVerifier, redirectURI)
}

// Refresh delegates to the base provider.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: refresh a Confluence OAuth access token via the base provider
func (p *ConfluenceContentOAuthProvider) Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error) {
	return p.base.Refresh(ctx, refreshToken)
}

// Revoke delegates to the base provider. Atlassian 3LO has no public RFC 7009
// revocation endpoint, so operator config typically leaves revocation_url
// empty; the base provider treats that as a no-op.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: revoke a Confluence OAuth token via the base provider (no-op if revocation URL is unconfigured)
func (p *ConfluenceContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	return p.base.Revoke(ctx, token)
}

// RequiredScopes delegates to the base provider. Operators must include
// "offline_access" in required_scopes for refresh tokens to be issued.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: return the OAuth scopes required for Confluence access, delegating to the base provider (pure)
func (p *ConfluenceContentOAuthProvider) RequiredScopes() []string {
	return p.base.RequiredScopes()
}

// FetchAccountInfo first calls the base /me endpoint for the canonical
// account_id, then calls accessible-resources to upgrade the label to the
// first site URL when available. Errors during the accessible-resources
// call are logged and ignored — the base label is still returned, so the
// row can still be linked.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: fetch Atlassian account ID and upgrade the label to the first accessible site URL
func (p *ConfluenceContentOAuthProvider) FetchAccountInfo(ctx context.Context, accessToken string) (string, string, error) {
	id, label, err := p.base.FetchAccountInfo(ctx, accessToken)
	if err != nil {
		return id, label, err
	}
	siteURL, lookupErr := p.firstAccessibleResourceURL(ctx, accessToken)
	if lookupErr != nil {
		slogging.Get().Warn("confluence: accessible-resources lookup failed during account link: %v", lookupErr)
		return id, label, nil
	}
	if siteURL != "" {
		return id, siteURL, nil
	}
	return id, label, nil
}

// firstAccessibleResourceURL returns the URL of the first accessible Atlassian
// resource for the bearer token, or "" if none are returned. Errors propagate
// to the caller for logging.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: fetch the first Atlassian accessible-resource site URL for the bearer token
func (p *ConfluenceContentOAuthProvider) firstAccessibleResourceURL(ctx context.Context, accessToken string) (string, error) {
	endpoint := p.apiBase + "/oauth/token/accessible-resources"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)
	headers.Set("Accept", "application/json")
	result, err := p.client.Fetch(ctx, endpoint, SafeFetchOptions{
		Method:       http.MethodGet,
		Headers:      headers,
		MaxBodyBytes: confluenceMaxBodySize,
	})
	if err != nil {
		return "", err
	}
	if result.StatusCode != http.StatusOK {
		// Truncate body to a small slice for error display.
		bodyPreview := result.Body
		if len(bodyPreview) > 1024 {
			bodyPreview = bodyPreview[:1024]
		}
		return "", fmt.Errorf("accessible-resources status=%d body=%s", result.StatusCode, string(bodyPreview))
	}
	var resources []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(result.Body, &resources); err != nil {
		return "", err
	}
	for _, r := range resources {
		if r.URL != "" {
			return r.URL, nil
		}
	}
	return "", nil
}
