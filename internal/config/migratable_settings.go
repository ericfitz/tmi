package config

import (
	"strconv"
)

// MigratableSetting represents a setting that can be migrated from config to database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
}

// GetMigratableSettings returns settings from the config that can be migrated to the database.
// This extracts runtime-configurable settings from the config file/environment
// and formats them for database storage.
//
// Note: This only includes settings that are useful to have in the database for runtime
// configuration or for display in admin UIs. It excludes:
// - Sensitive credentials (OAuth client secrets, SAML private keys/certificates)
// - Startup-time settings (connection pool config - used before DB is available)
func (c *Config) GetMigratableSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	// Feature flags
	settings = append(settings, c.getMigratableFeatureFlags()...)

	// OAuth settings
	settings = append(settings, c.getMigratableOAuthSettings()...)

	// SAML settings
	settings = append(settings, c.getMigratableSAMLSettings()...)

	// Runtime settings
	settings = append(settings, c.getMigratableRuntimeSettings()...)

	return settings
}

// getMigratableFeatureFlags returns feature flag settings
func (c *Config) getMigratableFeatureFlags() []MigratableSetting {
	return []MigratableSetting{
		{
			Key:         "features.saml_enabled",
			Value:       strconv.FormatBool(c.Auth.SAML.Enabled),
			Type:        "bool",
			Description: "Enable SAML authentication (from config)",
		},
	}
}

// getMigratableOAuthSettings returns OAuth provider settings (non-secret fields only)
func (c *Config) getMigratableOAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	// OAuth callback URL (non-sensitive, useful for diagnostics)
	if c.Auth.OAuth.CallbackURL != "" {
		settings = append(settings, MigratableSetting{
			Key:         "auth.oauth_callback_url",
			Value:       c.Auth.OAuth.CallbackURL,
			Type:        "string",
			Description: "OAuth callback URL (from config)",
		})
	}

	// OAuth provider settings (non-secret fields only)
	for providerKey, p := range c.Auth.OAuth.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, c.getMigratableOAuthProviderSettings(providerKey, p)...)
	}

	return settings
}

// getMigratableOAuthProviderSettings returns settings for a single OAuth provider
func (c *Config) getMigratableOAuthProviderSettings(providerKey string, p OAuthProviderConfig) []MigratableSetting {
	prefix := "auth.oauth.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "OAuth provider enabled (from config)"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "OAuth provider ID (from config)"},
		{".name", p.Name, "OAuth provider display name (from config)"},
		{".icon", p.Icon, "OAuth provider icon (from config)"},
		{".authorization_url", p.AuthorizationURL, "OAuth authorization URL (from config)"},
		{".token_url", p.TokenURL, "OAuth token URL (from config)"},
		{".issuer", p.Issuer, "OAuth issuer (from config)"},
		{".jwks_url", p.JWKSURL, "OAuth JWKS URL (from config)"},
		{".client_id", p.ClientID, "OAuth client ID (from config)"}, // semi-public, visible in browser
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc,
			})
		}
	}

	// NOTE: ClientSecret is NOT migrated - it's sensitive
	return settings
}

// getMigratableSAMLSettings returns SAML provider settings (non-secret fields only)
func (c *Config) getMigratableSAMLSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	if !c.Auth.SAML.Enabled {
		return settings
	}

	for providerKey, p := range c.Auth.SAML.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, c.getMigratableSAMLProviderSettings(providerKey, p)...)
	}

	return settings
}

// getMigratableSAMLProviderSettings returns settings for a single SAML provider
func (c *Config) getMigratableSAMLProviderSettings(providerKey string, p SAMLProviderConfig) []MigratableSetting {
	prefix := "auth.saml.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "SAML provider enabled (from config)"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "SAML provider ID (from config)"},
		{".name", p.Name, "SAML provider display name (from config)"},
		{".icon", p.Icon, "SAML provider icon (from config)"},
		{".entity_id", p.EntityID, "SAML SP entity ID (from config)"},
		{".metadata_url", p.MetadataURL, "SAML metadata URL (from config)"},
		{".acs_url", p.ACSURL, "SAML ACS URL (from config)"},
		{".slo_url", p.SLOURL, "SAML SLO URL (from config)"},
		{".idp_metadata_url", p.IDPMetadataURL, "SAML IdP metadata URL (from config)"},
		{".name_id_attribute", p.NameIDAttribute, "SAML NameID attribute (from config)"},
		{".email_attribute", p.EmailAttribute, "SAML email attribute (from config)"},
		{".name_attribute", p.NameAttribute, "SAML name attribute (from config)"},
		{".groups_attribute", p.GroupsAttribute, "SAML groups attribute (from config)"},
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc,
			})
		}
	}

	// SAML behavior flags (always include these)
	boolFields := []struct {
		suffix string
		value  bool
		desc   string
	}{
		{".allow_idp_initiated", p.AllowIDPInitiated, "Allow IdP-initiated SAML login (from config)"},
		{".force_authn", p.ForceAuthn, "Force re-authentication (from config)"},
		{".sign_requests", p.SignRequests, "Sign SAML requests (from config)"},
	}

	for _, f := range boolFields {
		settings = append(settings, MigratableSetting{
			Key: prefix + f.suffix, Value: strconv.FormatBool(f.value), Type: "bool", Description: f.desc,
		})
	}

	// NOTE: SPPrivateKey, SPCertificate, MetadataXML, IDPMetadataB64XML are NOT migrated - sensitive
	return settings
}

// getMigratableRuntimeSettings returns runtime-configurable settings
func (c *Config) getMigratableRuntimeSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	// WebSocket settings
	if c.WebSocket.InactivityTimeoutSeconds > 0 {
		settings = append(settings, MigratableSetting{
			Key:         "websocket.inactivity_timeout_seconds",
			Value:       strconv.Itoa(c.WebSocket.InactivityTimeoutSeconds),
			Type:        "int",
			Description: "WebSocket inactivity timeout in seconds (from config)",
		})
	}

	// JWT settings
	if c.Auth.JWT.ExpirationSeconds > 0 {
		settings = append(settings, MigratableSetting{
			Key:         "session.timeout_minutes",
			Value:       strconv.Itoa(c.Auth.JWT.ExpirationSeconds / 60),
			Type:        "int",
			Description: "JWT token expiration in minutes (from config)",
		})
	}

	// Operator settings
	if c.Operator.Name != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.name",
			Value:       c.Operator.Name,
			Type:        "string",
			Description: "Operator/maintainer name (from config)",
		})
	}
	if c.Operator.Contact != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.contact",
			Value:       c.Operator.Contact,
			Type:        "string",
			Description: "Operator contact information (from config)",
		})
	}

	// Logging level
	if c.Logging.Level != "" {
		settings = append(settings, MigratableSetting{
			Key:         "logging.level",
			Value:       c.Logging.Level,
			Type:        "string",
			Description: "Logging level at startup (from config, read-only)",
		})
	}

	return settings
}
