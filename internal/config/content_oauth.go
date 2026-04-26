package config

import (
	"fmt"
)

// ContentOAuthConfig holds configuration for delegated content OAuth providers.
type ContentOAuthConfig struct {
	CallbackURL            string                                `yaml:"callback_url" env:"TMI_CONTENT_OAUTH_CALLBACK_URL"`
	AllowedClientCallbacks []string                              `yaml:"allowed_client_callbacks"`
	Providers              map[string]ContentOAuthProviderConfig `yaml:"providers"`
}

// ContentOAuthProviderConfig is one entry under content_oauth.providers.*
//
// ExtraAuthorizeParams are appended to the authorize URL query string. They are
// useful for providers that require non-standard parameters beyond the standard
// OAuth 2.0 + PKCE set (e.g. Atlassian's audience=api.atlassian.com). yaml-only
// for now; if env-var support is needed later it can be added without breaking
// existing configs.
type ContentOAuthProviderConfig struct {
	Enabled              bool              `yaml:"enabled"`
	ClientID             string            `yaml:"client_id"`
	ClientSecret         string            `yaml:"client_secret"` //nolint:gosec // G117 - OAuth provider client secret
	AuthURL              string            `yaml:"auth_url"`
	TokenURL             string            `yaml:"token_url"`
	UserinfoURL          string            `yaml:"userinfo_url"`
	RevocationURL        string            `yaml:"revocation_url"`
	RequiredScopes       []string          `yaml:"required_scopes"`
	ExtraAuthorizeParams map[string]string `yaml:"extra_authorize_params"`
}

// Validate returns an error if any enabled provider is missing required fields,
// or if at least one provider is enabled but the encryption key is empty/invalid.
func (c *ContentOAuthConfig) Validate(encryptionKey string) error {
	anyEnabled := false
	for id, p := range c.Providers {
		if !p.Enabled {
			continue
		}
		anyEnabled = true
		if p.ClientID == "" {
			return fmt.Errorf("content_oauth.providers.%s: client_id is required when enabled", id)
		}
		if p.AuthURL == "" {
			return fmt.Errorf("content_oauth.providers.%s: auth_url is required when enabled", id)
		}
		if p.TokenURL == "" {
			return fmt.Errorf("content_oauth.providers.%s: token_url is required when enabled", id)
		}
	}
	if anyEnabled && encryptionKey == "" {
		return fmt.Errorf("at least one content OAuth provider is enabled but TMI_CONTENT_TOKEN_ENCRYPTION_KEY is not set")
	}
	return nil
}
