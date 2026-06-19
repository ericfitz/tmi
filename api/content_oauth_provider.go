package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
)

// ContentOAuthProvider is the interface each delegated content provider implements.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: interface for delegated content OAuth providers covering auth, token exchange, refresh, and revoke
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
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: token payload returned by a content OAuth exchange or refresh (pure)
type ContentOAuthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// ExpiresAt returns the computed absolute expiry time, or nil if ExpiresIn is zero.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: compute the absolute expiry time from ExpiresIn seconds; nil when no expiry (pure)
func (r *ContentOAuthTokenResponse) ExpiresAt() *time.Time {
	if r.ExpiresIn <= 0 {
		return nil
	}
	t := time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	return &t
}

// errContentOAuthPermanent marks a failure as non-retryable (e.g., invalid_grant).
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: sentinel error type marking a non-retryable OAuth failure (pure)
type errContentOAuthPermanent struct{ msg string }

// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: return the permanent OAuth error message string (pure)
func (e *errContentOAuthPermanent) Error() string { return e.msg }

// IsContentOAuthPermanentFailure returns true when err wraps a permanent OAuth
// failure (e.g. 4xx on refresh meaning the token is revoked or invalid).
// Permanent failures should not be retried; callers should mark the token as
// failed and ask the user to re-authorize.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: report whether an error signals a permanent, non-retryable OAuth failure (pure)
func IsContentOAuthPermanentFailure(err error) bool {
	var e *errContentOAuthPermanent
	return errors.As(err, &e)
}

// contentOAuthMaxBodySize caps token/userinfo/revoke response bodies. Token
// responses are small JSON payloads; the cap is defensive against hostile or
// misconfigured providers returning unbounded data.
const contentOAuthMaxBodySize = 1 * 1024 * 1024 // 1 MiB

// contentOAuthDefaultTimeout is the per-request overall timeout for OAuth
// token endpoints. 30s matches the pre-migration http.Client.Timeout.
const contentOAuthDefaultTimeout = 30 * time.Second

// BaseContentOAuthProvider is the default implementation; providers with
// provider-specific userinfo / scope semantics can wrap it.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: default OAuth provider implementation backed by SafeHTTPClient (struct)
type BaseContentOAuthProvider struct {
	id     string
	cfg    config.ContentOAuthProviderConfig
	client *SafeHTTPClient
}

// NewBaseContentOAuthProvider creates a new BaseContentOAuthProvider routing
// outbound OAuth calls through SafeHTTPClient with a 30s overall timeout and
// a 1 MiB body cap. validator MUST be non-nil; in production it is built from
// the operator's content_oauth allowlist (typically equal to the
// authorization/token URL hosts).
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: build a BaseContentOAuthProvider with a 30s timeout and 1 MiB body cap (pure)
func NewBaseContentOAuthProvider(id string, cfg config.ContentOAuthProviderConfig, validator *URIValidator) *BaseContentOAuthProvider {
	return &BaseContentOAuthProvider{
		id:  id,
		cfg: cfg,
		client: NewSafeHTTPClient(
			validator,
			WithDefaultTimeouts(contentOAuthDefaultTimeout, 10*time.Second, contentOAuthMaxBodySize),
		),
	}
}

// ID returns the provider identifier.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: return the provider's identifier string (pure)
func (p *BaseContentOAuthProvider) ID() string { return p.id }

// RequiredScopes returns the scopes required by this provider.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: return the OAuth scopes required by this provider (pure)
func (p *BaseContentOAuthProvider) RequiredScopes() []string { return p.cfg.RequiredScopes }

// AuthorizationURL builds the authorization URL with PKCE and state parameters.
// It respects any existing query string in cfg.AuthURL and appends any provider
// configured ExtraAuthorizeParams (e.g. Atlassian's audience=api.atlassian.com).
// Standard parameters always win if a provider misconfigures an extra with the
// same key.
// SEM@6199f1bebeb0a5e637b7c38588d721ac36b525f4: build the PKCE authorization URL with state, scopes, and provider-specific extra params (pure)
func (p *BaseContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	q := url.Values{}
	for k, v := range p.cfg.ExtraAuthorizeParams {
		q.Set(k, v)
	}
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
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: exchange an authorization code for an access and refresh token via authorization_code grant
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
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: exchange a refresh token for new tokens; permanent failures return a non-retryable error
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
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: revoke a token at the provider via RFC 7009; no-op when no revocation URL is configured
func (p *BaseContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	if p.cfg.RevocationURL == "" {
		slogging.Get().Info("content_oauth provider %q has no revocation_url; skipping provider-side revoke", p.id)
		return nil
	}
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)

	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")

	result, err := p.client.Fetch(ctx, p.cfg.RevocationURL, SafeFetchOptions{
		Method:  http.MethodPost,
		Body:    strings.NewReader(form.Encode()),
		Headers: headers,
	})
	if err != nil {
		return err
	}
	if result.StatusCode >= 200 && result.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("content_oauth revoke failed: status=%d body=%s", result.StatusCode, string(result.Body))
}

// FetchAccountInfo fetches the account id and label from the provider's userinfo endpoint.
// Returns empty strings (not an error) if UserinfoURL is not configured.
// The account id is taken from the first non-empty of: sub, id, account_id.
// The label is taken from the first non-empty of: email, username, name.
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: fetch the provider account ID and label from the userinfo endpoint (reads DB)
func (p *BaseContentOAuthProvider) FetchAccountInfo(ctx context.Context, accessToken string) (string, string, error) {
	if p.cfg.UserinfoURL == "" {
		return "", "", nil
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+accessToken)

	result, err := p.client.Fetch(ctx, p.cfg.UserinfoURL, SafeFetchOptions{
		Method:  http.MethodGet,
		Headers: headers,
	})
	if err != nil {
		return "", "", err
	}
	if result.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("userinfo returned status %d", result.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(result.Body, &payload); err != nil {
		return "", "", err
	}
	return stringField(payload, "sub", "id", "account_id"),
		stringField(payload, "email", "username", "name"),
		nil
}

// stringField returns the first non-empty string value from the given keys in m.
// SEM@c97401fa0a66697da085fd38b4dff4f6898f6831: return the first non-empty string value from named keys in a map (pure)
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
// SEM@06d5e5b913b744dc0132db2d119ef31db9c989ae: POST form data to the token endpoint and decode the JSON response; wraps 4xx refresh errors as permanent
func (p *BaseContentOAuthProvider) postToken(ctx context.Context, form url.Values, isRefresh bool) (*ContentOAuthTokenResponse, error) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	headers.Set("Accept", "application/json")

	result, err := p.client.Fetch(ctx, p.cfg.TokenURL, SafeFetchOptions{
		Method:  http.MethodPost,
		Body:    strings.NewReader(form.Encode()),
		Headers: headers,
	})
	if err != nil {
		return nil, err
	}

	if result.StatusCode >= 400 && result.StatusCode < 500 {
		msg := fmt.Sprintf("content_oauth token call failed: status=%d body=%s", result.StatusCode, string(result.Body))
		if isRefresh {
			// Refresh 4xx errors are treated as permanent (token revoked or invalid).
			return nil, &errContentOAuthPermanent{msg: msg}
		}
		return nil, fmt.Errorf("%s", msg)
	}
	if result.StatusCode >= 500 {
		return nil, fmt.Errorf("content_oauth token call returned 5xx: status=%d body=%s", result.StatusCode, string(result.Body))
	}
	var out ContentOAuthTokenResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("content_oauth token response decode: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("content_oauth token response missing access_token")
	}
	return &out, nil
}
