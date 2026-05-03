package config

import (
	"encoding/json"
	"os"
	"strconv"
)

// MigratableSetting represents a setting that can be migrated from config to database
type MigratableSetting struct {
	Key         string
	Value       string
	Type        string
	Description string
	Secret      bool   // true = mask value in API responses
	Source      string // "config" or "environment"
}

// settingSource returns "environment" if the given env var is set, otherwise "config".
func settingSource(envVar string) string {
	if os.Getenv(envVar) != "" {
		return "environment"
	}
	return "config"
}

// GetMigratableSettings returns all settings from the config formatted for database storage.
// Secret fields are included with Secret=true so the API layer can mask their values.
func (c *Config) GetMigratableSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	settings = append(settings, c.getMigratableServerSettings()...)
	settings = append(settings, c.getMigratableDatabaseSettings()...)
	settings = append(settings, c.getMigratableAuthSettings()...)
	settings = append(settings, c.getMigratableFeatureFlags()...)
	settings = append(settings, c.getMigratableOAuthSettings()...)
	settings = append(settings, c.getMigratableSAMLSettings()...)
	settings = append(settings, c.getMigratableRuntimeSettings()...)
	settings = append(settings, c.getMigratableLoggingSettings()...)
	settings = append(settings, c.getMigratableSecretsSettings()...)
	settings = append(settings, c.getMigratableAdministratorsSettings()...)

	return settings
}

// getMigratableServerSettings returns server configuration settings
func (c *Config) getMigratableServerSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "server.port", Value: c.Server.Port, Type: "string", Description: "HTTP server port", Source: settingSource("TMI_SERVER_PORT")},
		{Key: "server.interface", Value: c.Server.Interface, Type: "string", Description: "Network interface to bind to", Source: settingSource("TMI_SERVER_INTERFACE")},
		{Key: "server.tls_enabled", Value: strconv.FormatBool(c.Server.TLSEnabled), Type: "bool", Description: "TLS enabled", Source: settingSource("TMI_SERVER_TLS_ENABLED")},
		{Key: "server.tls_subject_name", Value: c.Server.TLSSubjectName, Type: "string", Description: "TLS certificate subject name", Source: settingSource("TMI_SERVER_TLS_SUBJECT_NAME")},
		{Key: "server.http_to_https_redirect", Value: strconv.FormatBool(c.Server.HTTPToHTTPSRedirect), Type: "bool", Description: "HTTP to HTTPS redirect", Source: settingSource("TMI_SERVER_HTTP_TO_HTTPS_REDIRECT")},
		{Key: "server.read_timeout", Value: c.Server.ReadTimeout.String(), Type: "string", Description: "HTTP read timeout", Source: settingSource("TMI_SERVER_READ_TIMEOUT")},
		{Key: "server.write_timeout", Value: c.Server.WriteTimeout.String(), Type: "string", Description: "HTTP write timeout", Source: settingSource("TMI_SERVER_WRITE_TIMEOUT")},
		{Key: "server.idle_timeout", Value: c.Server.IdleTimeout.String(), Type: "string", Description: "HTTP idle timeout", Source: settingSource("TMI_SERVER_IDLE_TIMEOUT")},
	}
	if c.Server.BaseURL != "" {
		settings = append(settings, MigratableSetting{Key: "server.base_url", Value: c.Server.BaseURL, Type: "string", Description: "Public base URL for callbacks", Source: settingSource("TMI_SERVER_BASE_URL")})
	}
	if c.Server.TLSEnabled {
		settings = append(settings,
			MigratableSetting{Key: "server.tls_cert_file", Value: c.Server.TLSCertFile, Type: "string", Description: "TLS certificate file path", Source: settingSource("TMI_SERVER_TLS_CERT_FILE")},
			MigratableSetting{Key: "server.tls_key_file", Value: c.Server.TLSKeyFile, Type: "string", Description: "TLS key file path", Source: settingSource("TMI_SERVER_TLS_KEY_FILE")},
		)
	}
	if len(c.Server.CORS.AllowedOrigins) > 0 {
		originsJSON, _ := json.Marshal(c.Server.CORS.AllowedOrigins)
		settings = append(settings, MigratableSetting{Key: "server.cors.allowed_origins", Value: string(originsJSON), Type: "json", Description: "CORS allowed origins", Source: settingSource("TMI_CORS_ALLOWED_ORIGINS")})
	}
	return settings
}

// getMigratableAuthSettings returns authentication configuration settings
func (c *Config) getMigratableAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "auth.build_mode", Value: c.Auth.BuildMode, Type: "string", Description: "Build mode (dev, test, production)", Source: settingSource("TMI_BUILD_MODE")},
		{Key: "auth.auto_promote_first_user", Value: strconv.FormatBool(c.Auth.AutoPromoteFirstUser), Type: "bool", Description: "Auto-promote first user to admin", Source: settingSource("TMI_AUTH_AUTO_PROMOTE_FIRST_USER")},
		{Key: "auth.everyone_is_a_reviewer", Value: strconv.FormatBool(c.Auth.EveryoneIsAReviewer), Type: "bool", Description: "Auto-add all users to Security Reviewers group", Source: settingSource("TMI_AUTH_EVERYONE_IS_A_REVIEWER")},
	}
	// JWT settings
	settings = append(settings,
		MigratableSetting{Key: "auth.jwt.secret", Value: c.Auth.JWT.Secret, Type: "string", Description: "JWT signing secret", Source: settingSource("TMI_JWT_SECRET"), Secret: true},
		MigratableSetting{Key: "auth.jwt.expiration_seconds", Value: strconv.Itoa(c.Auth.JWT.ExpirationSeconds), Type: "int", Description: "JWT token expiration in seconds", Source: settingSource("TMI_JWT_EXPIRATION_SECONDS")},
		MigratableSetting{Key: "auth.jwt.signing_method", Value: c.Auth.JWT.SigningMethod, Type: "string", Description: "JWT signing method", Source: settingSource("TMI_JWT_SIGNING_METHOD")},
		MigratableSetting{Key: "auth.jwt.refresh_token_days", Value: strconv.Itoa(c.Auth.JWT.RefreshTokenDays), Type: "int", Description: "Refresh token TTL in days", Source: settingSource("TMI_REFRESH_TOKEN_DAYS")},
		MigratableSetting{Key: "auth.jwt.session_lifetime_days", Value: strconv.Itoa(c.Auth.JWT.SessionLifetimeDays), Type: "int", Description: "Absolute session lifetime in days", Source: settingSource("TMI_SESSION_LIFETIME_DAYS")},
	)
	// Cookie settings
	settings = append(settings,
		MigratableSetting{Key: "auth.cookie.enabled", Value: strconv.FormatBool(c.Auth.Cookie.Enabled), Type: "bool", Description: "HttpOnly cookie-based auth enabled", Source: settingSource("TMI_COOKIE_ENABLED")},
		MigratableSetting{Key: "auth.cookie.domain", Value: c.Auth.Cookie.Domain, Type: "string", Description: "Cookie domain", Source: settingSource("TMI_COOKIE_DOMAIN")},
		MigratableSetting{Key: "auth.cookie.secure", Value: strconv.FormatBool(c.Auth.Cookie.Secure), Type: "bool", Description: "Require HTTPS for cookies", Source: settingSource("TMI_COOKIE_SECURE")},
	)
	return settings
}

// getMigratableDatabaseSettings returns database configuration settings
func (c *Config) getMigratableDatabaseSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "database.url", Value: sanitizeURL(c.Database.URL), Type: "string", Description: "Database connection URL (password redacted)", Source: settingSource("TMI_DATABASE_URL")},
	}
	// Connection pool
	settings = append(settings,
		MigratableSetting{Key: "database.connection_pool.max_open_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxOpenConns), Type: "int", Description: "Maximum open database connections", Source: settingSource("TMI_DB_MAX_OPEN_CONNS")},
		MigratableSetting{Key: "database.connection_pool.max_idle_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxIdleConns), Type: "int", Description: "Maximum idle database connections", Source: settingSource("TMI_DB_MAX_IDLE_CONNS")},
		MigratableSetting{Key: "database.connection_pool.conn_max_lifetime", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxLifetime), Type: "int", Description: "Max connection lifetime in seconds", Source: settingSource("TMI_DB_CONN_MAX_LIFETIME")},
		MigratableSetting{Key: "database.connection_pool.conn_max_idle_time", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxIdleTime), Type: "int", Description: "Max connection idle time in seconds", Source: settingSource("TMI_DB_CONN_MAX_IDLE_TIME")},
	)
	// Redis
	if c.Database.Redis.URL != "" {
		settings = append(settings, MigratableSetting{Key: "database.redis.url", Value: sanitizeURL(c.Database.Redis.URL), Type: "string", Description: "Redis connection URL (password redacted)", Source: settingSource("TMI_REDIS_URL")})
	}
	settings = append(settings,
		MigratableSetting{Key: "database.redis.host", Value: c.Database.Redis.Host, Type: "string", Description: "Redis host", Source: settingSource("TMI_REDIS_HOST")},
		MigratableSetting{Key: "database.redis.port", Value: c.Database.Redis.Port, Type: "string", Description: "Redis port", Source: settingSource("TMI_REDIS_PORT")},
		MigratableSetting{Key: "database.redis.password", Value: c.Database.Redis.Password, Type: "string", Description: "Redis password", Source: settingSource("TMI_REDIS_PASSWORD"), Secret: true},
		MigratableSetting{Key: "database.redis.db", Value: strconv.Itoa(c.Database.Redis.DB), Type: "int", Description: "Redis database number", Source: settingSource("TMI_REDIS_DB")},
	)
	return settings
}

// getMigratableFeatureFlags returns feature flag settings
func (c *Config) getMigratableFeatureFlags() []MigratableSetting {
	return []MigratableSetting{
		{
			Key:         "features.saml_enabled",
			Value:       strconv.FormatBool(c.Auth.SAML.Enabled),
			Type:        "bool",
			Description: "Enable SAML authentication",
			Source:      settingSource("TMI_SAML_ENABLED"),
		},
	}
}

// getMigratableOAuthSettings returns OAuth provider settings
func (c *Config) getMigratableOAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	// OAuth callback URL (non-sensitive, useful for diagnostics)
	if c.Auth.OAuth.CallbackURL != "" {
		settings = append(settings, MigratableSetting{
			Key:         "auth.oauth_callback_url",
			Value:       c.Auth.OAuth.CallbackURL,
			Type:        "string",
			Description: "OAuth callback URL",
			Source:      "config",
		})
	}

	// OAuth provider settings
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
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "OAuth provider enabled", Source: "config"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "OAuth provider ID"},
		{".name", p.Name, "OAuth provider display name"},
		{".icon", p.Icon, "OAuth provider icon"},
		{".authorization_url", p.AuthorizationURL, "OAuth authorization URL"},
		{".token_url", p.TokenURL, "OAuth token URL"},
		{".issuer", p.Issuer, "OAuth issuer"},
		{".jwks_url", p.JWKSURL, "OAuth JWKS URL"},
		{".client_id", p.ClientID, "OAuth client ID"}, // semi-public, visible in browser
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}

	// Client secret — masked in API responses
	settings = append(settings, MigratableSetting{
		Key: prefix + ".client_secret", Value: p.ClientSecret, Type: "string",
		Description: "OAuth client secret", Source: "config", Secret: true,
	})

	// Scopes as JSON array
	if len(p.Scopes) > 0 {
		scopesJSON, _ := json.Marshal(p.Scopes)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".scopes", Value: string(scopesJSON), Type: "json",
			Description: "OAuth scopes", Source: "config",
		})
	}

	// UserInfo endpoints as JSON
	if len(p.UserInfo) > 0 {
		userInfoJSON, _ := json.Marshal(p.UserInfo)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".userinfo", Value: string(userInfoJSON), Type: "json",
			Description: "OAuth userinfo endpoints", Source: "config",
		})
	}

	if p.AuthHeaderFormat != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".auth_header_format", Value: p.AuthHeaderFormat, Type: "string",
			Description: "OAuth auth header format", Source: "config",
		})
	}
	if p.AcceptHeader != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".accept_header", Value: p.AcceptHeader, Type: "string",
			Description: "OAuth accept header", Source: "config",
		})
	}

	return settings
}

// getMigratableSAMLSettings returns SAML provider settings
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
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "SAML provider enabled", Source: "config"},
	}

	// Add non-empty string fields
	stringFields := []struct {
		suffix, value, desc string
	}{
		{".id", p.ID, "SAML provider ID"},
		{".name", p.Name, "SAML provider display name"},
		{".icon", p.Icon, "SAML provider icon"},
		{".entity_id", p.EntityID, "SAML SP entity ID"},
		{".metadata_url", p.MetadataURL, "SAML metadata URL"},
		{".acs_url", p.ACSURL, "SAML ACS URL"},
		{".slo_url", p.SLOURL, "SAML SLO URL"},
		{".idp_metadata_url", p.IDPMetadataURL, "SAML IdP metadata URL"},
		{".name_id_attribute", p.NameIDAttribute, "SAML NameID attribute"},
		{".email_attribute", p.EmailAttribute, "SAML email attribute"},
		{".name_attribute", p.NameAttribute, "SAML name attribute"},
		{".groups_attribute", p.GroupsAttribute, "SAML groups attribute"},
	}

	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}

	// SAML behavior flags (always include these)
	boolFields := []struct {
		suffix string
		value  bool
		desc   string
	}{
		{".allow_idp_initiated", p.AllowIDPInitiated, "Allow IdP-initiated SAML login"},
		{".force_authn", p.ForceAuthn, "Force re-authentication"},
		{".sign_requests", p.SignRequests, "Sign SAML requests"},
	}

	for _, f := range boolFields {
		settings = append(settings, MigratableSetting{
			Key: prefix + f.suffix, Value: strconv.FormatBool(f.value), Type: "bool", Description: f.desc, Source: "config",
		})
	}

	// Secret fields
	settings = append(settings,
		MigratableSetting{Key: prefix + ".sp_private_key", Value: p.SPPrivateKey, Type: "string", Description: "SAML SP private key", Source: "config", Secret: true},
		MigratableSetting{Key: prefix + ".sp_certificate", Value: p.SPCertificate, Type: "string", Description: "SAML SP certificate", Source: "config", Secret: true},
		MigratableSetting{Key: prefix + ".idp_metadata_b64xml", Value: p.IDPMetadataB64XML, Type: "string", Description: "IdP metadata (base64 XML)", Source: "config", Secret: true},
	)

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
			Description: "WebSocket inactivity timeout in seconds",
			Source:      settingSource("TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS"),
		})
	}

	// JWT settings
	if c.Auth.JWT.ExpirationSeconds > 0 {
		settings = append(settings, MigratableSetting{
			Key:         "session.timeout_minutes",
			Value:       strconv.Itoa(c.Auth.JWT.ExpirationSeconds / 60),
			Type:        "int",
			Description: "JWT token expiration in minutes",
			Source:      settingSource("TMI_JWT_EXPIRATION_SECONDS"),
		})
	}

	// Operator settings
	if c.Operator.Name != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.name",
			Value:       c.Operator.Name,
			Type:        "string",
			Description: "Operator/maintainer name",
			Source:      settingSource("TMI_OPERATOR_NAME"),
		})
	}
	if c.Operator.Contact != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.contact",
			Value:       c.Operator.Contact,
			Type:        "string",
			Description: "Operator contact information",
			Source:      settingSource("TMI_OPERATOR_CONTACT"),
		})
	}

	return settings
}

// getMigratableLoggingSettings returns logging configuration settings
func (c *Config) getMigratableLoggingSettings() []MigratableSetting {
	return []MigratableSetting{
		{Key: "logging.level", Value: c.Logging.Level, Type: "string", Description: "Log level", Source: settingSource("TMI_LOG_LEVEL")},
		{Key: "logging.is_dev", Value: strconv.FormatBool(c.Logging.IsDev), Type: "bool", Description: "Development mode logging", Source: settingSource("TMI_LOG_IS_DEV")},
		{Key: "logging.is_test", Value: strconv.FormatBool(c.Logging.IsTest), Type: "bool", Description: "Test mode logging", Source: settingSource("TMI_LOG_IS_TEST")},
		{Key: "logging.log_dir", Value: c.Logging.LogDir, Type: "string", Description: "Log directory", Source: settingSource("TMI_LOG_DIR")},
		{Key: "logging.max_age_days", Value: strconv.Itoa(c.Logging.MaxAgeDays), Type: "int", Description: "Log max age in days", Source: settingSource("TMI_LOG_MAX_AGE_DAYS")},
		{Key: "logging.max_size_mb", Value: strconv.Itoa(c.Logging.MaxSizeMB), Type: "int", Description: "Log max size in MB", Source: settingSource("TMI_LOG_MAX_SIZE_MB")},
		{Key: "logging.max_backups", Value: strconv.Itoa(c.Logging.MaxBackups), Type: "int", Description: "Log max backup count", Source: settingSource("TMI_LOG_MAX_BACKUPS")},
		{Key: "logging.also_log_to_console", Value: strconv.FormatBool(c.Logging.AlsoLogToConsole), Type: "bool", Description: "Also log to console", Source: settingSource("TMI_LOG_ALSO_LOG_TO_CONSOLE")},
		{Key: "logging.cloud_error_threshold", Value: strconv.Itoa(c.Logging.CloudErrorThreshold), Type: "int", Description: "Cloud sink consecutive-failure threshold for one-shot Warn alarm (0 disables)", Source: settingSource("TMI_LOG_CLOUD_ERROR_THRESHOLD")},
		{Key: "logging.log_api_requests", Value: strconv.FormatBool(c.Logging.LogAPIRequests), Type: "bool", Description: "Log API requests", Source: settingSource("TMI_LOG_API_REQUESTS")},
		{Key: "logging.log_api_responses", Value: strconv.FormatBool(c.Logging.LogAPIResponses), Type: "bool", Description: "Log API responses", Source: settingSource("TMI_LOG_API_RESPONSES")},
		{Key: "logging.log_websocket_messages", Value: strconv.FormatBool(c.Logging.LogWebSocketMsg), Type: "bool", Description: "Log WebSocket messages", Source: settingSource("TMI_LOG_WEBSOCKET_MESSAGES")},
		{Key: "logging.redact_auth_tokens", Value: strconv.FormatBool(c.Logging.RedactAuthTokens), Type: "bool", Description: "Redact auth tokens in logs", Source: settingSource("TMI_LOG_REDACT_AUTH_TOKENS")},
		{Key: "logging.suppress_unauthenticated_logs", Value: strconv.FormatBool(c.Logging.SuppressUnauthenticatedLogs), Type: "bool", Description: "Suppress unauthenticated request logs", Source: settingSource("TMI_LOG_SUPPRESS_UNAUTH_LOGS")},
	}
}

// getMigratableSecretsSettings returns secrets provider configuration settings
func (c *Config) getMigratableSecretsSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "secrets.provider", Value: c.Secrets.Provider, Type: "string", Description: "Secret provider type", Source: settingSource("TMI_SECRETS_PROVIDER")},
	}
	stringFields := []struct{ key, value, env, desc string }{
		{"secrets.vault_address", c.Secrets.VaultAddress, "TMI_VAULT_ADDRESS", "HashiCorp Vault address"},
		{"secrets.vault_path", c.Secrets.VaultPath, "TMI_VAULT_PATH", "HashiCorp Vault path"},
		{"secrets.aws_region", c.Secrets.AWSRegion, "TMI_AWS_REGION", "AWS region"},
		{"secrets.aws_secret_name", c.Secrets.AWSSecretName, "TMI_AWS_SECRET_NAME", "AWS secret name"},
		{"secrets.azure_vault_url", c.Secrets.AzureVaultURL, "TMI_AZURE_VAULT_URL", "Azure Key Vault URL"},
		{"secrets.gcp_project_id", c.Secrets.GCPProjectID, "TMI_GCP_PROJECT_ID", "GCP project ID"},
		{"secrets.gcp_secret_name", c.Secrets.GCPSecretName, "TMI_GCP_SECRET_NAME", "GCP secret name"},
		{"secrets.oci_compartment_id", c.Secrets.OCICompartmentID, "TMI_OCI_COMPARTMENT_ID", "OCI compartment ID"},
		{"secrets.oci_vault_id", c.Secrets.OCIVaultID, "TMI_OCI_VAULT_ID", "OCI vault ID"},
		{"secrets.oci_secret_name", c.Secrets.OCISecretName, "TMI_OCI_SECRET_NAME", "OCI secret name"},
	}
	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{Key: f.key, Value: f.value, Type: "string", Description: f.desc, Source: settingSource(f.env)})
		}
	}
	settings = append(settings, MigratableSetting{Key: "secrets.vault_token", Value: c.Secrets.VaultToken, Type: "string", Description: "HashiCorp Vault token", Source: settingSource("TMI_VAULT_TOKEN"), Secret: true})
	return settings
}

// getMigratableAdministratorsSettings returns administrator configuration settings
func (c *Config) getMigratableAdministratorsSettings() []MigratableSetting {
	if len(c.Administrators) == 0 {
		return nil
	}
	adminsJSON, err := json.Marshal(c.Administrators)
	if err != nil {
		return nil
	}
	return []MigratableSetting{
		{Key: "administrators", Value: string(adminsJSON), Type: "json", Description: "Configured administrators", Source: "config"},
	}
}
