package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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
type ConfluenceContentOAuthProvider struct {
	base       *BaseContentOAuthProvider
	httpClient *http.Client
	apiBase    string
}

// NewConfluenceContentOAuthProvider wraps base with Confluence-specific
// account-info enrichment.
func NewConfluenceContentOAuthProvider(base *BaseContentOAuthProvider) *ConfluenceContentOAuthProvider {
	return &ConfluenceContentOAuthProvider{
		base:       base,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		apiBase:    confluenceAPIBase,
	}
}

// ID returns the wrapped provider's id (always "confluence" in practice; the
// registry chooses this implementation by id).
func (p *ConfluenceContentOAuthProvider) ID() string { return p.base.ID() }

// AuthorizationURL delegates to the base provider, which appends any
// configured ExtraAuthorizeParams (e.g. audience=api.atlassian.com).
func (p *ConfluenceContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	return p.base.AuthorizationURL(state, pkceChallenge, redirectURI)
}

// ExchangeCode delegates to the base provider.
func (p *ConfluenceContentOAuthProvider) ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error) {
	return p.base.ExchangeCode(ctx, code, pkceVerifier, redirectURI)
}

// Refresh delegates to the base provider.
func (p *ConfluenceContentOAuthProvider) Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error) {
	return p.base.Refresh(ctx, refreshToken)
}

// Revoke delegates to the base provider. Atlassian 3LO has no public RFC 7009
// revocation endpoint, so operator config typically leaves revocation_url
// empty; the base provider treats that as a no-op.
func (p *ConfluenceContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	return p.base.Revoke(ctx, token)
}

// RequiredScopes delegates to the base provider. Operators must include
// "offline_access" in required_scopes for refresh tokens to be issued.
func (p *ConfluenceContentOAuthProvider) RequiredScopes() []string {
	return p.base.RequiredScopes()
}

// FetchAccountInfo first calls the base /me endpoint for the canonical
// account_id, then calls accessible-resources to upgrade the label to the
// first site URL when available. Errors during the accessible-resources
// call are logged and ignored — the base label is still returned, so the
// row can still be linked.
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
func (p *ConfluenceContentOAuthProvider) firstAccessibleResourceURL(ctx context.Context, accessToken string) (string, error) {
	endpoint := p.apiBase + "/oauth/token/accessible-resources"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req) //nolint:gosec // G704 - URL is api.atlassian.com/oauth/token/accessible-resources (apiBase is operator-configured)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("accessible-resources status=%d body=%s", resp.StatusCode, string(body))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, confluenceMaxBodySize))
	if err != nil {
		return "", err
	}
	var resources []struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &resources); err != nil {
		return "", err
	}
	for _, r := range resources {
		if r.URL != "" {
			return r.URL, nil
		}
	}
	return "", nil
}
