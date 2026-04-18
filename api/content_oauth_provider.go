package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentOAuthProvider is the interface each delegated content provider implements.
type ContentOAuthProvider interface {
	ID() string
	AuthorizationURL(state, pkceChallenge, redirectURI string) string
	ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error)
	Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error)
	Revoke(ctx context.Context, token string) error
	RequiredScopes() []string
	// FetchAccountInfo is provider-specific; if UserinfoURL is configured, it
	// returns the external account id + label. Returns empty values if unavailable.
	FetchAccountInfo(ctx context.Context, accessToken string) (accountID, label string, err error)
}

// ContentOAuthTokenResponse is the token payload returned by exchange/refresh.
type ContentOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`  //nolint:gosec // G117 - OAuth token response field
	RefreshToken string `json:"refresh_token"` //nolint:gosec // G117 - OAuth token response field
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// ExpiresAt returns the computed absolute expiry time, or nil if ExpiresIn is zero.
func (r *ContentOAuthTokenResponse) ExpiresAt() *time.Time {
	if r.ExpiresIn <= 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	return &t
}

// errContentOAuthPermanent marks a failure as non-retryable (e.g., invalid_grant).
type errContentOAuthPermanent struct{ msg string }

func (e *errContentOAuthPermanent) Error() string { return e.msg }

// IsContentOAuthPermanentFailure returns true when err wraps a permanent OAuth
// failure (e.g. 4xx on refresh meaning the token is revoked or invalid).
// Permanent failures should not be retried; callers should mark the token as
// failed and ask the user to re-authorize.
func IsContentOAuthPermanentFailure(err error) bool {
	var e *errContentOAuthPermanent
	return errors.As(err, &e)
}

// BaseContentOAuthProvider is the default implementation; providers with
// provider-specific userinfo / scope semantics can wrap it.
type BaseContentOAuthProvider struct {
	id         string
	cfg        config.ContentOAuthProviderConfig
	httpClient *http.Client
}

// NewBaseContentOAuthProvider creates a new BaseContentOAuthProvider with a 30-second HTTP timeout.
func NewBaseContentOAuthProvider(id string, cfg config.ContentOAuthProviderConfig) *BaseContentOAuthProvider {
	return &BaseContentOAuthProvider{
		id:         id,
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// ID returns the provider identifier.
func (p *BaseContentOAuthProvider) ID() string { return p.id }

// RequiredScopes returns the scopes required by this provider.
func (p *BaseContentOAuthProvider) RequiredScopes() []string { return p.cfg.RequiredScopes }

// AuthorizationURL builds the authorization URL with PKCE and state parameters.
// It respects any existing query string in cfg.AuthURL.
func (p *BaseContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", pkceChallenge)
	q.Set("code_challenge_method", "S256")
	if len(p.cfg.RequiredScopes) > 0 {
		q.Set("scope", strings.Join(p.cfg.RequiredScopes, " "))
	}
	sep := "?"
	if strings.Contains(p.cfg.AuthURL, "?") {
		sep = "&"
	}
	return p.cfg.AuthURL + sep + q.Encode()
}

// ExchangeCode exchanges an authorization code for tokens using the authorization_code grant.
func (p *BaseContentOAuthProvider) ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code_verifier", pkceVerifier)
	return p.postToken(ctx, form, false)
}

// Refresh exchanges a refresh token for new tokens using the refresh_token grant.
// On 4xx responses (e.g., invalid_grant), an errContentOAuthPermanent is returned.
// On 5xx responses, a plain transient error is returned.
func (p *BaseContentOAuthProvider) Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	return p.postToken(ctx, form, true)
}

// Revoke revokes the given token via RFC 7009.
// If no RevocationURL is configured, this is a no-op and returns nil.
func (p *BaseContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	if p.cfg.RevocationURL == "" {
		slogging.Get().Info("content_oauth provider %q has no revocation_url; skipping provider-side revoke", p.id)
		return nil
	}
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.RevocationURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req) //nolint:gosec // G107 - URL is from admin-configured OAuth provider
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("content_oauth revoke failed: status=%d body=%s", resp.StatusCode, string(body))
}

// FetchAccountInfo fetches the account id and label from the provider's userinfo endpoint.
// Returns empty strings (not an error) if UserinfoURL is not configured.
// The account id is taken from the first non-empty of: sub, id, account_id.
// The label is taken from the first non-empty of: email, username, name.
func (p *BaseContentOAuthProvider) FetchAccountInfo(ctx context.Context, accessToken string) (string, string, error) {
	if p.cfg.UserinfoURL == "" {
		return "", "", nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.UserinfoURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := p.httpClient.Do(req) //nolint:gosec // G107 - URL is from admin-configured OAuth provider
	if err != nil {
		return "", "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo returned status %d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", "", err
	}
	return stringField(payload, "sub", "id", "account_id"),
		stringField(payload, "email", "username", "name"),
		nil
}

// stringField returns the first non-empty string value from the given keys in m.
func stringField(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// postToken posts a form to the token endpoint and decodes the response.
// When isRefresh is true, 4xx responses are wrapped in errContentOAuthPermanent.
func (p *BaseContentOAuthProvider) postToken(ctx context.Context, form url.Values, isRefresh bool) (*ContentOAuthTokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := p.httpClient.Do(req) //nolint:gosec // G107 - URL is from admin-configured OAuth provider
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		msg := fmt.Sprintf("content_oauth token call failed: status=%d body=%s", resp.StatusCode, string(body))
		if isRefresh {
			// Refresh 4xx errors are treated as permanent (token revoked or invalid).
			return nil, &errContentOAuthPermanent{msg: msg}
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("content_oauth token call returned 5xx: status=%d body=%s", resp.StatusCode, string(body))
	}
	var out ContentOAuthTokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("content_oauth token response decode: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("content_oauth token response missing access_token")
	}
	return &out, nil
}
