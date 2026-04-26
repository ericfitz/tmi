package api

import "context"

// MicrosoftContentOAuthProvider wraps BaseContentOAuthProvider for Microsoft
// Entra ID. The base provider already understands Microsoft Graph's /me
// userinfo response shape (id + mail + displayName), so this wrapper is
// currently a thin pass-through. It exists as a stable extension point for
// future Graph-specific behavior (e.g., resolving tenant id from access
// tokens, decoding Microsoft-specific error payloads on refresh failures).
//
// The TMI Entra app must be registered in the operator's tenant with the
// scopes listed in MicrosoftContentOAuthProvider.RequiredScopes() and a
// redirect URI matching ContentOAuthConfig.CallbackURL.
type MicrosoftContentOAuthProvider struct {
	base *BaseContentOAuthProvider
}

// NewMicrosoftContentOAuthProvider wraps base for the Microsoft provider id.
func NewMicrosoftContentOAuthProvider(base *BaseContentOAuthProvider) *MicrosoftContentOAuthProvider {
	return &MicrosoftContentOAuthProvider{base: base}
}

// ID returns "microsoft".
func (p *MicrosoftContentOAuthProvider) ID() string { return p.base.ID() }

// AuthorizationURL delegates to the base provider.
func (p *MicrosoftContentOAuthProvider) AuthorizationURL(state, pkceChallenge, redirectURI string) string {
	return p.base.AuthorizationURL(state, pkceChallenge, redirectURI)
}

// ExchangeCode delegates to the base provider.
func (p *MicrosoftContentOAuthProvider) ExchangeCode(ctx context.Context, code, pkceVerifier, redirectURI string) (*ContentOAuthTokenResponse, error) {
	return p.base.ExchangeCode(ctx, code, pkceVerifier, redirectURI)
}

// Refresh delegates to the base provider.
func (p *MicrosoftContentOAuthProvider) Refresh(ctx context.Context, refreshToken string) (*ContentOAuthTokenResponse, error) {
	return p.base.Refresh(ctx, refreshToken)
}

// Revoke delegates to the base provider. Microsoft Graph has no public RFC
// 7009 revocation endpoint as of 2026; operator config typically leaves
// revocation_url empty and the base provider treats this as a no-op.
func (p *MicrosoftContentOAuthProvider) Revoke(ctx context.Context, token string) error {
	return p.base.Revoke(ctx, token)
}

// RequiredScopes delegates to the base provider. Operators MUST include
// "offline_access" for refresh tokens to be issued, "Files.SelectedOperations.Selected"
// for read access to per-file-permissioned items, "Files.ReadWrite" for the
// picker-grant call, and "User.Read" for account labelling.
func (p *MicrosoftContentOAuthProvider) RequiredScopes() []string {
	return p.base.RequiredScopes()
}

// FetchAccountInfo delegates to the base provider. Microsoft Graph /me returns
// "id" (account_id) and "mail" or "userPrincipalName" (label), both handled by
// BaseContentOAuthProvider's stringField lookup over standard keys.
func (p *MicrosoftContentOAuthProvider) FetchAccountInfo(ctx context.Context, accessToken string) (string, string, error) {
	return p.base.FetchAccountInfo(ctx, accessToken)
}
