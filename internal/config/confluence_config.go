package config

// ConfluenceConfig holds settings for the delegated Confluence content source.
// OAuth-provider settings (client id/secret, endpoints, scopes,
// extra_authorize_params for audience=api.atlassian.com) live under
// content_oauth.providers.confluence.
//
// This struct intentionally has no picker fields — Confluence has no picker
// UX; users paste page URLs.
type ConfluenceConfig struct {
	Enabled bool `yaml:"enabled" env:"TMI_CONTENT_SOURCE_CONFLUENCE_ENABLED"`
}

// IsConfigured returns true when the source is enabled. The OAuth provider
// configuration is validated separately via the content_oauth registry.
func (c ConfluenceConfig) IsConfigured() bool {
	return c.Enabled
}
