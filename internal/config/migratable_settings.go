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
	Secret      bool   // true = mask value in API responses (kept for back-compat; mirrors Class.Secret)
	Source      string // "config" or "environment"
	EnvVar      string // TMI_* environment variable that overrides this setting ("" if none)
	Class       ConfigClass
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
	settings = append(settings, c.getMigratableTimmySettings()...)
	settings = append(settings, c.getMigratableObservabilitySettings()...)
	settings = append(settings, c.getMigratableSSRFSettings()...)
	settings = append(settings, c.getMigratableWebhooksSettings()...)
	settings = append(settings, c.getMigratableContentExtractorsSettings()...)
	settings = append(settings, c.getMigratableContentOAuthSettings()...)
	settings = append(settings, c.getMigratableContentSourcesSettings()...)

	for i := range settings {
		settings[i].Class = classificationFor(settings[i].Key)
		// Keep the legacy per-setting Secret flag and Class.Secret consistent.
		if settings[i].Class.Secret {
			settings[i].Secret = true
		}
	}

	return settings
}

// getMigratableServerSettings returns server configuration settings
func (c *Config) getMigratableServerSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "server.port", Value: c.Server.Port, Type: "string", Description: "HTTP server port", Source: settingSource("TMI_SERVER_PORT"), EnvVar: "TMI_SERVER_PORT"},
		{Key: "server.interface", Value: c.Server.Interface, Type: "string", Description: "Network interface to bind to", Source: settingSource("TMI_SERVER_INTERFACE"), EnvVar: "TMI_SERVER_INTERFACE"},
		{Key: "server.tls_enabled", Value: strconv.FormatBool(c.Server.TLSEnabled), Type: "bool", Description: "TLS enabled", Source: settingSource("TMI_SERVER_TLS_ENABLED"), EnvVar: "TMI_SERVER_TLS_ENABLED"},
		{Key: "server.tls_subject_name", Value: c.Server.TLSSubjectName, Type: "string", Description: "TLS certificate subject name", Source: settingSource("TMI_SERVER_TLS_SUBJECT_NAME"), EnvVar: "TMI_SERVER_TLS_SUBJECT_NAME"},
		{Key: "server.http_to_https_redirect", Value: strconv.FormatBool(c.Server.HTTPToHTTPSRedirect), Type: "bool", Description: "HTTP to HTTPS redirect", Source: settingSource("TMI_SERVER_HTTP_TO_HTTPS_REDIRECT"), EnvVar: "TMI_SERVER_HTTP_TO_HTTPS_REDIRECT"},
		{Key: "server.read_timeout", Value: c.Server.ReadTimeout.String(), Type: "string", Description: "HTTP read timeout", Source: settingSource("TMI_SERVER_READ_TIMEOUT"), EnvVar: "TMI_SERVER_READ_TIMEOUT"},
		{Key: "server.write_timeout", Value: c.Server.WriteTimeout.String(), Type: "string", Description: "HTTP write timeout", Source: settingSource("TMI_SERVER_WRITE_TIMEOUT"), EnvVar: "TMI_SERVER_WRITE_TIMEOUT"},
		{Key: "server.idle_timeout", Value: c.Server.IdleTimeout.String(), Type: "string", Description: "HTTP idle timeout", Source: settingSource("TMI_SERVER_IDLE_TIMEOUT"), EnvVar: "TMI_SERVER_IDLE_TIMEOUT"},
		// Rate-limiting and optimistic-locking knobs (#426)
		{Key: "server.disable_rate_limiting", Value: strconv.FormatBool(c.Server.DisableRateLimiting), Type: "bool", Description: "Disable all rate limiting (dev/test only)", Source: settingSource("TMI_DISABLE_RATE_LIMITING"), EnvVar: "TMI_DISABLE_RATE_LIMITING"},
		{Key: "server.ratelimit_public_rpm", Value: strconv.Itoa(c.Server.RateLimitPublicRPM), Type: "int", Description: "Requests per minute per IP for public endpoints", Source: settingSource("TMI_RATELIMIT_PUBLIC_RPM"), EnvVar: "TMI_RATELIMIT_PUBLIC_RPM"},
		{Key: "server.require_if_match", Value: strconv.FormatBool(c.Server.RequireIfMatch), Type: "bool", Description: "Return 428 when If-Match header is missing on PUT/PATCH", Source: settingSource("TMI_REQUIRE_IF_MATCH"), EnvVar: "TMI_REQUIRE_IF_MATCH"},
	}
	if c.Server.BaseURL != "" {
		settings = append(settings, MigratableSetting{Key: "server.base_url", Value: c.Server.BaseURL, Type: "string", Description: "Public base URL for callbacks", Source: settingSource("TMI_SERVER_BASE_URL"), EnvVar: "TMI_SERVER_BASE_URL"})
	}
	if c.Server.TLSEnabled {
		settings = append(settings,
			MigratableSetting{Key: "server.tls_cert_file", Value: c.Server.TLSCertFile, Type: "string", Description: "TLS certificate file path", Source: settingSource("TMI_SERVER_TLS_CERT_FILE"), EnvVar: "TMI_SERVER_TLS_CERT_FILE"},
			MigratableSetting{Key: "server.tls_key_file", Value: c.Server.TLSKeyFile, Type: "string", Description: "TLS key file path", Source: settingSource("TMI_SERVER_TLS_KEY_FILE"), EnvVar: "TMI_SERVER_TLS_KEY_FILE"},
		)
	}
	if len(c.Server.CORS.AllowedOrigins) > 0 {
		originsJSON, _ := json.Marshal(c.Server.CORS.AllowedOrigins)
		settings = append(settings, MigratableSetting{Key: "server.cors.allowed_origins", Value: string(originsJSON), Type: "json", Description: "CORS allowed origins", Source: settingSource("TMI_CORS_ALLOWED_ORIGINS"), EnvVar: "TMI_CORS_ALLOWED_ORIGINS"})
	}
	return settings
}

// getMigratableAuthSettings returns authentication configuration settings
func (c *Config) getMigratableAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "auth.build_mode", Value: c.Auth.BuildMode, Type: "string", Description: "Build mode (dev, test, production)", Source: settingSource("TMI_BUILD_MODE"), EnvVar: "TMI_BUILD_MODE"},
		{Key: "auth.auto_promote_first_user", Value: strconv.FormatBool(c.Auth.AutoPromoteFirstUser), Type: "bool", Description: "Auto-promote first user to admin", Source: settingSource("TMI_AUTH_AUTO_PROMOTE_FIRST_USER"), EnvVar: "TMI_AUTH_AUTO_PROMOTE_FIRST_USER"},
		{Key: "auth.everyone_is_a_reviewer", Value: strconv.FormatBool(c.Auth.EveryoneIsAReviewer), Type: "bool", Description: "Auto-add all users to Security Reviewers group", Source: settingSource("TMI_AUTH_EVERYONE_IS_A_REVIEWER"), EnvVar: "TMI_AUTH_EVERYONE_IS_A_REVIEWER"},
	}
	// JWT settings
	settings = append(settings,
		MigratableSetting{Key: "auth.jwt.secret", Value: c.Auth.JWT.Secret, Type: "string", Description: "JWT signing secret", Source: settingSource("TMI_JWT_SECRET"), Secret: true, EnvVar: "TMI_JWT_SECRET"},
		MigratableSetting{Key: "auth.jwt.expiration_seconds", Value: strconv.Itoa(c.Auth.JWT.ExpirationSeconds), Type: "int", Description: "JWT token expiration in seconds", Source: settingSource("TMI_JWT_EXPIRATION_SECONDS"), EnvVar: "TMI_JWT_EXPIRATION_SECONDS"},
		MigratableSetting{Key: "auth.jwt.signing_method", Value: c.Auth.JWT.SigningMethod, Type: "string", Description: "JWT signing method", Source: settingSource("TMI_JWT_SIGNING_METHOD"), EnvVar: "TMI_JWT_SIGNING_METHOD"},
		MigratableSetting{Key: "auth.jwt.refresh_token_days", Value: strconv.Itoa(c.Auth.JWT.RefreshTokenDays), Type: "int", Description: "Refresh token TTL in days", Source: settingSource("TMI_REFRESH_TOKEN_DAYS"), EnvVar: "TMI_REFRESH_TOKEN_DAYS"},
		MigratableSetting{Key: "auth.jwt.session_lifetime_days", Value: strconv.Itoa(c.Auth.JWT.SessionLifetimeDays), Type: "int", Description: "Absolute session lifetime in days", Source: settingSource("TMI_SESSION_LIFETIME_DAYS"), EnvVar: "TMI_SESSION_LIFETIME_DAYS"},
		MigratableSetting{Key: "auth.step_up_window_seconds", Value: strconv.Itoa(c.Auth.StepUpWindowSeconds), Type: "int", Description: "Step-up auth_time freshness window in seconds for /admin/* writes (#355); minimum 60", Source: settingSource("TMI_AUTH_STEP_UP_WINDOW_SECONDS"), EnvVar: "TMI_AUTH_STEP_UP_WINDOW_SECONDS"},
	)
	// Cookie settings
	settings = append(settings,
		MigratableSetting{Key: "auth.cookie.enabled", Value: strconv.FormatBool(c.Auth.Cookie.Enabled), Type: "bool", Description: "HttpOnly cookie-based auth enabled", Source: settingSource("TMI_COOKIE_ENABLED"), EnvVar: "TMI_COOKIE_ENABLED"},
		MigratableSetting{Key: "auth.cookie.domain", Value: c.Auth.Cookie.Domain, Type: "string", Description: "Cookie domain", Source: settingSource("TMI_COOKIE_DOMAIN"), EnvVar: "TMI_COOKIE_DOMAIN"},
		MigratableSetting{Key: "auth.cookie.secure", Value: strconv.FormatBool(c.Auth.Cookie.Secure), Type: "bool", Description: "Require HTTPS for cookies", Source: settingSource("TMI_COOKIE_SECURE"), EnvVar: "TMI_COOKIE_SECURE"},
	)
	return settings
}

// getMigratableDatabaseSettings returns database configuration settings
func (c *Config) getMigratableDatabaseSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "database.url", Value: sanitizeURL(c.Database.URL), Type: "string", Description: "Database connection URL (password redacted)", Source: settingSource("TMI_DATABASE_URL"), EnvVar: "TMI_DATABASE_URL"},
	}
	// Connection pool
	settings = append(settings,
		MigratableSetting{Key: "database.connection_pool.max_open_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxOpenConns), Type: "int", Description: "Maximum open database connections", Source: settingSource("TMI_DB_MAX_OPEN_CONNS"), EnvVar: "TMI_DB_MAX_OPEN_CONNS"},
		MigratableSetting{Key: "database.connection_pool.max_idle_conns", Value: strconv.Itoa(c.Database.ConnectionPool.MaxIdleConns), Type: "int", Description: "Maximum idle database connections", Source: settingSource("TMI_DB_MAX_IDLE_CONNS"), EnvVar: "TMI_DB_MAX_IDLE_CONNS"},
		MigratableSetting{Key: "database.connection_pool.conn_max_lifetime", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxLifetime), Type: "int", Description: "Max connection lifetime in seconds", Source: settingSource("TMI_DB_CONN_MAX_LIFETIME"), EnvVar: "TMI_DB_CONN_MAX_LIFETIME"},
		MigratableSetting{Key: "database.connection_pool.conn_max_idle_time", Value: strconv.Itoa(c.Database.ConnectionPool.ConnMaxIdleTime), Type: "int", Description: "Max connection idle time in seconds", Source: settingSource("TMI_DB_CONN_MAX_IDLE_TIME"), EnvVar: "TMI_DB_CONN_MAX_IDLE_TIME"},
	)
	// Redis
	if c.Database.Redis.URL != "" {
		settings = append(settings, MigratableSetting{Key: "database.redis.url", Value: sanitizeURL(c.Database.Redis.URL), Type: "string", Description: "Redis connection URL (password redacted)", Source: settingSource("TMI_REDIS_URL"), EnvVar: "TMI_REDIS_URL"})
	}
	settings = append(settings,
		MigratableSetting{Key: "database.redis.host", Value: c.Database.Redis.Host, Type: "string", Description: "Redis host", Source: settingSource("TMI_REDIS_HOST"), EnvVar: "TMI_REDIS_HOST"},
		MigratableSetting{Key: "database.redis.port", Value: c.Database.Redis.Port, Type: "string", Description: "Redis port", Source: settingSource("TMI_REDIS_PORT"), EnvVar: "TMI_REDIS_PORT"},
		MigratableSetting{Key: "database.redis.password", Value: c.Database.Redis.Password, Type: "string", Description: "Redis password", Source: settingSource("TMI_REDIS_PASSWORD"), Secret: true, EnvVar: "TMI_REDIS_PASSWORD"},
		MigratableSetting{Key: "database.redis.db", Value: strconv.Itoa(c.Database.Redis.DB), Type: "int", Description: "Redis database number", Source: settingSource("TMI_REDIS_DB"), EnvVar: "TMI_REDIS_DB"},
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
			EnvVar:      "TMI_SAML_ENABLED",
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

	// OAuth client_callback allowlist (operational; read at request time by
	// the auth handler to validate the client_callback query parameter).
	if len(c.Auth.OAuth.ClientCallbackAllowList) > 0 {
		allowJSON, _ := json.Marshal(c.Auth.OAuth.ClientCallbackAllowList)
		settings = append(settings, MigratableSetting{
			Key:         "auth.oauth.client_callback_allowlist",
			Value:       string(allowJSON),
			Type:        "json",
			Description: "Allowlist of client_callback URLs for /oauth2/authorize and /oauth2/step_up (exact URL or wildcard pattern ending in '*')",
			Source:      settingSource("TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST"),
			EnvVar:      "TMI_OAUTH_CLIENT_CALLBACK_ALLOWLIST",
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
			EnvVar:      "TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS",
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
			EnvVar:      "TMI_JWT_EXPIRATION_SECONDS",
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
			EnvVar:      "TMI_OPERATOR_NAME",
		})
	}
	if c.Operator.Contact != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.contact",
			Value:       c.Operator.Contact,
			Type:        "string",
			Description: "Operator contact information",
			Source:      settingSource("TMI_OPERATOR_CONTACT"),
			EnvVar:      "TMI_OPERATOR_CONTACT",
		})
	}
	if c.Operator.Jurisdiction != "" {
		settings = append(settings, MigratableSetting{
			Key:         "operator.jurisdiction",
			Value:       c.Operator.Jurisdiction,
			Type:        "string",
			Description: "Legal jurisdiction under which the service operates",
			Source:      settingSource("TMI_OPERATOR_JURISDICTION"),
			EnvVar:      "TMI_OPERATOR_JURISDICTION",
		})
	}

	return settings
}

// getMigratableLoggingSettings returns logging configuration settings
func (c *Config) getMigratableLoggingSettings() []MigratableSetting {
	return []MigratableSetting{
		{Key: "logging.level", Value: c.Logging.Level, Type: "string", Description: "Log level", Source: settingSource("TMI_LOG_LEVEL"), EnvVar: "TMI_LOG_LEVEL"},
		{Key: "logging.is_dev", Value: strconv.FormatBool(c.Logging.IsDev), Type: "bool", Description: "Development mode logging", Source: settingSource("TMI_LOG_IS_DEV"), EnvVar: "TMI_LOG_IS_DEV"},
		{Key: "logging.is_test", Value: strconv.FormatBool(c.Logging.IsTest), Type: "bool", Description: "Test mode logging", Source: settingSource("TMI_LOG_IS_TEST"), EnvVar: "TMI_LOG_IS_TEST"},
		{Key: "logging.log_dir", Value: c.Logging.LogDir, Type: "string", Description: "Log directory", Source: settingSource("TMI_LOG_DIR"), EnvVar: "TMI_LOG_DIR"},
		{Key: "logging.max_age_days", Value: strconv.Itoa(c.Logging.MaxAgeDays), Type: "int", Description: "Log max age in days", Source: settingSource("TMI_LOG_MAX_AGE_DAYS"), EnvVar: "TMI_LOG_MAX_AGE_DAYS"},
		{Key: "logging.max_size_mb", Value: strconv.Itoa(c.Logging.MaxSizeMB), Type: "int", Description: "Log max size in MB", Source: settingSource("TMI_LOG_MAX_SIZE_MB"), EnvVar: "TMI_LOG_MAX_SIZE_MB"},
		{Key: "logging.max_backups", Value: strconv.Itoa(c.Logging.MaxBackups), Type: "int", Description: "Log max backup count", Source: settingSource("TMI_LOG_MAX_BACKUPS"), EnvVar: "TMI_LOG_MAX_BACKUPS"},
		{Key: "logging.also_log_to_console", Value: strconv.FormatBool(c.Logging.AlsoLogToConsole), Type: "bool", Description: "Also log to console", Source: settingSource("TMI_LOG_ALSO_LOG_TO_CONSOLE"), EnvVar: "TMI_LOG_ALSO_LOG_TO_CONSOLE"},
		{Key: "logging.cloud_error_threshold", Value: strconv.Itoa(c.Logging.CloudErrorThreshold), Type: "int", Description: "Cloud sink consecutive-failure threshold for one-shot Warn alarm (0 disables)", Source: settingSource("TMI_LOG_CLOUD_ERROR_THRESHOLD"), EnvVar: "TMI_LOG_CLOUD_ERROR_THRESHOLD"},
		{Key: "logging.log_api_requests", Value: strconv.FormatBool(c.Logging.LogAPIRequests), Type: "bool", Description: "Log API requests", Source: settingSource("TMI_LOG_API_REQUESTS"), EnvVar: "TMI_LOG_API_REQUESTS"},
		{Key: "logging.log_api_responses", Value: strconv.FormatBool(c.Logging.LogAPIResponses), Type: "bool", Description: "Log API responses", Source: settingSource("TMI_LOG_API_RESPONSES"), EnvVar: "TMI_LOG_API_RESPONSES"},
		{Key: "logging.log_websocket_messages", Value: strconv.FormatBool(c.Logging.LogWebSocketMsg), Type: "bool", Description: "Log WebSocket messages", Source: settingSource("TMI_LOG_WEBSOCKET_MESSAGES"), EnvVar: "TMI_LOG_WEBSOCKET_MESSAGES"},
		{Key: "logging.redact_auth_tokens", Value: strconv.FormatBool(c.Logging.RedactAuthTokens), Type: "bool", Description: "Redact auth tokens in logs", Source: settingSource("TMI_LOG_REDACT_AUTH_TOKENS"), EnvVar: "TMI_LOG_REDACT_AUTH_TOKENS"},
		{Key: "logging.suppress_unauthenticated_logs", Value: strconv.FormatBool(c.Logging.SuppressUnauthenticatedLogs), Type: "bool", Description: "Suppress unauthenticated request logs", Source: settingSource("TMI_LOG_SUPPRESS_UNAUTH_LOGS"), EnvVar: "TMI_LOG_SUPPRESS_UNAUTH_LOGS"},
	}
}

// getMigratableSecretsSettings returns secrets provider configuration settings
func (c *Config) getMigratableSecretsSettings() []MigratableSetting {
	settings := []MigratableSetting{
		{Key: "secrets.provider", Value: c.Secrets.Provider, Type: "string", Description: "Secret provider type", Source: settingSource("TMI_SECRETS_PROVIDER"), EnvVar: "TMI_SECRETS_PROVIDER"},
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
			settings = append(settings, MigratableSetting{Key: f.key, Value: f.value, Type: "string", Description: f.desc, Source: settingSource(f.env), EnvVar: f.env})
		}
	}
	settings = append(settings, MigratableSetting{Key: "secrets.vault_token", Value: c.Secrets.VaultToken, Type: "string", Description: "HashiCorp Vault token", Source: settingSource("TMI_VAULT_TOKEN"), Secret: true, EnvVar: "TMI_VAULT_TOKEN"})
	return settings
}

// getMigratableTimmySettings returns Timmy AI assistant settings, including
// the shared embedding profile keys.
func (c *Config) getMigratableTimmySettings() []MigratableSetting {
	t := c.Timmy
	settings := []MigratableSetting{
		{Key: "timmy.enabled", Value: strconv.FormatBool(t.Enabled), Type: "bool", Description: "Timmy AI assistant enabled", Source: settingSource("TMI_TIMMY_ENABLED"), EnvVar: "TMI_TIMMY_ENABLED"},
		{Key: "timmy.llm_provider", Value: t.LLMProvider, Type: "string", Description: "LLM provider", Source: settingSource("TMI_TIMMY_LLM_PROVIDER"), EnvVar: "TMI_TIMMY_LLM_PROVIDER"},
		{Key: "timmy.llm_model", Value: t.LLMModel, Type: "string", Description: "LLM model", Source: settingSource("TMI_TIMMY_LLM_MODEL"), EnvVar: "TMI_TIMMY_LLM_MODEL"},
		{Key: "timmy.llm_api_key", Value: t.LLMAPIKey, Type: "string", Description: "LLM API key", Source: settingSource("TMI_TIMMY_LLM_API_KEY"), Secret: true, EnvVar: "TMI_TIMMY_LLM_API_KEY"},
		{Key: "timmy.llm_base_url", Value: t.LLMBaseURL, Type: "string", Description: "LLM API base URL", Source: settingSource("TMI_TIMMY_LLM_BASE_URL"), EnvVar: "TMI_TIMMY_LLM_BASE_URL"},
		{Key: "timmy.text_embedding_provider", Value: t.TextEmbeddingProvider, Type: "string", Description: "Text embedding provider", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_PROVIDER"), EnvVar: "TMI_TIMMY_TEXT_EMBEDDING_PROVIDER"},
		{Key: "timmy.text_embedding_model", Value: t.TextEmbeddingModel, Type: "string", Description: "Text embedding model — shared invariant between ingest and query", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_MODEL"), EnvVar: "TMI_TIMMY_TEXT_EMBEDDING_MODEL"},
		{Key: "timmy.text_embedding_base_url", Value: t.TextEmbeddingBaseURL, Type: "string", Description: "Text embedding API base URL — shared invariant", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_BASE_URL"), EnvVar: "TMI_TIMMY_TEXT_EMBEDDING_BASE_URL"},
		{Key: "timmy.embedding_dimension", Value: strconv.Itoa(t.EmbeddingDimension), Type: "int", Description: "Text embedding vector dimension — shared invariant", Source: settingSource("TMI_TIMMY_EMBEDDING_DIMENSION"), EnvVar: "TMI_TIMMY_EMBEDDING_DIMENSION"},
		{Key: "timmy.text_embedding_api_key", Value: t.TextEmbeddingAPIKey, Type: "string", Description: "Text embedding API key", Source: settingSource("TMI_TIMMY_TEXT_EMBEDDING_API_KEY"), Secret: true, EnvVar: "TMI_TIMMY_TEXT_EMBEDDING_API_KEY"},
		{Key: "timmy.text_retrieval_top_k", Value: strconv.Itoa(t.TextRetrievalTopK), Type: "int", Description: "Text retrieval top-k results", Source: settingSource("TMI_TIMMY_TEXT_RETRIEVAL_TOP_K"), EnvVar: "TMI_TIMMY_TEXT_RETRIEVAL_TOP_K"},
		{Key: "timmy.code_embedding_provider", Value: t.CodeEmbeddingProvider, Type: "string", Description: "Code embedding provider", Source: settingSource("TMI_TIMMY_CODE_EMBEDDING_PROVIDER"), EnvVar: "TMI_TIMMY_CODE_EMBEDDING_PROVIDER"},
		{Key: "timmy.code_embedding_model", Value: t.CodeEmbeddingModel, Type: "string", Description: "Code embedding model", Source: settingSource("TMI_TIMMY_CODE_EMBEDDING_MODEL"), EnvVar: "TMI_TIMMY_CODE_EMBEDDING_MODEL"},
		{Key: "timmy.code_embedding_api_key", Value: t.CodeEmbeddingAPIKey, Type: "string", Description: "Code embedding API key", Source: settingSource("TMI_TIMMY_CODE_EMBEDDING_API_KEY"), Secret: true, EnvVar: "TMI_TIMMY_CODE_EMBEDDING_API_KEY"},
		{Key: "timmy.code_embedding_base_url", Value: t.CodeEmbeddingBaseURL, Type: "string", Description: "Code embedding API base URL", Source: settingSource("TMI_TIMMY_CODE_EMBEDDING_BASE_URL"), EnvVar: "TMI_TIMMY_CODE_EMBEDDING_BASE_URL"},
		{Key: "timmy.code_retrieval_top_k", Value: strconv.Itoa(t.CodeRetrievalTopK), Type: "int", Description: "Code retrieval top-k results", Source: settingSource("TMI_TIMMY_CODE_RETRIEVAL_TOP_K"), EnvVar: "TMI_TIMMY_CODE_RETRIEVAL_TOP_K"},
		{Key: "timmy.query_decomposition_enabled", Value: strconv.FormatBool(t.QueryDecompositionEnabled), Type: "bool", Description: "Query decomposition enabled", Source: settingSource("TMI_TIMMY_QUERY_DECOMPOSITION_ENABLED"), EnvVar: "TMI_TIMMY_QUERY_DECOMPOSITION_ENABLED"},
		{Key: "timmy.rerank_provider", Value: t.RerankProvider, Type: "string", Description: "Reranker provider", Source: settingSource("TMI_TIMMY_RERANK_PROVIDER"), EnvVar: "TMI_TIMMY_RERANK_PROVIDER"},
		{Key: "timmy.rerank_model", Value: t.RerankModel, Type: "string", Description: "Reranker model", Source: settingSource("TMI_TIMMY_RERANK_MODEL"), EnvVar: "TMI_TIMMY_RERANK_MODEL"},
		{Key: "timmy.rerank_api_key", Value: t.RerankAPIKey, Type: "string", Description: "Reranker API key", Source: settingSource("TMI_TIMMY_RERANK_API_KEY"), Secret: true, EnvVar: "TMI_TIMMY_RERANK_API_KEY"},
		{Key: "timmy.rerank_base_url", Value: t.RerankBaseURL, Type: "string", Description: "Reranker API base URL", Source: settingSource("TMI_TIMMY_RERANK_BASE_URL"), EnvVar: "TMI_TIMMY_RERANK_BASE_URL"},
		{Key: "timmy.rerank_top_k", Value: strconv.Itoa(t.RerankTopK), Type: "int", Description: "Reranker top-k results", Source: settingSource("TMI_TIMMY_RERANK_TOP_K"), EnvVar: "TMI_TIMMY_RERANK_TOP_K"},
		{Key: "timmy.max_conversation_history", Value: strconv.Itoa(t.MaxConversationHistory), Type: "int", Description: "Max conversation history entries", Source: settingSource("TMI_TIMMY_MAX_CONVERSATION_HISTORY"), EnvVar: "TMI_TIMMY_MAX_CONVERSATION_HISTORY"},
		{Key: "timmy.operator_system_prompt", Value: t.OperatorSystemPrompt, Type: "string", Description: "Operator system prompt override", Source: settingSource("TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"), EnvVar: "TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"},
		{Key: "timmy.max_memory_mb", Value: strconv.Itoa(t.MaxMemoryMB), Type: "int", Description: "Max memory in MB", Source: settingSource("TMI_TIMMY_MAX_MEMORY_MB"), EnvVar: "TMI_TIMMY_MAX_MEMORY_MB"},
		{Key: "timmy.inactivity_timeout_seconds", Value: strconv.Itoa(t.InactivityTimeoutSeconds), Type: "int", Description: "Session inactivity timeout in seconds", Source: settingSource("TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"), EnvVar: "TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"},
		{Key: "timmy.max_messages_per_user_per_hour", Value: strconv.Itoa(t.MaxMessagesPerUserPerHour), Type: "int", Description: "Max messages per user per hour", Source: settingSource("TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"), EnvVar: "TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"},
		{Key: "timmy.max_sessions_per_threat_model", Value: strconv.Itoa(t.MaxSessionsPerThreatModel), Type: "int", Description: "Max Timmy sessions per threat model", Source: settingSource("TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"), EnvVar: "TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"},
		{Key: "timmy.max_concurrent_llm_requests", Value: strconv.Itoa(t.MaxConcurrentLLMRequests), Type: "int", Description: "Max concurrent LLM requests", Source: settingSource("TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"), EnvVar: "TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"},
		{Key: "timmy.chunk_size", Value: strconv.Itoa(t.ChunkSize), Type: "int", Description: "Embedding chunk size", Source: settingSource("TMI_TIMMY_CHUNK_SIZE"), EnvVar: "TMI_TIMMY_CHUNK_SIZE"},
		{Key: "timmy.chunk_overlap", Value: strconv.Itoa(t.ChunkOverlap), Type: "int", Description: "Embedding chunk overlap", Source: settingSource("TMI_TIMMY_CHUNK_OVERLAP"), EnvVar: "TMI_TIMMY_CHUNK_OVERLAP"},
		{Key: "timmy.llm_timeout_seconds", Value: strconv.Itoa(t.LLMTimeoutSeconds), Type: "int", Description: "LLM request timeout in seconds", Source: settingSource("TMI_TIMMY_LLM_TIMEOUT_SECONDS"), EnvVar: "TMI_TIMMY_LLM_TIMEOUT_SECONDS"},
		{Key: "timmy.embedding_cleanup_interval_minutes", Value: strconv.Itoa(t.EmbeddingCleanupIntervalMinutes), Type: "int", Description: "Embedding cleanup interval in minutes", Source: settingSource("TMI_TIMMY_EMBEDDING_CLEANUP_INTERVAL_MINUTES"), EnvVar: "TMI_TIMMY_EMBEDDING_CLEANUP_INTERVAL_MINUTES"},
		{Key: "timmy.embedding_idle_days_active", Value: strconv.Itoa(t.EmbeddingIdleDaysActive), Type: "int", Description: "Days before idle active-TM embeddings are cleaned up", Source: settingSource("TMI_TIMMY_EMBEDDING_IDLE_DAYS_ACTIVE"), EnvVar: "TMI_TIMMY_EMBEDDING_IDLE_DAYS_ACTIVE"},
		{Key: "timmy.embedding_idle_days_closed", Value: strconv.Itoa(t.EmbeddingIdleDaysClosed), Type: "int", Description: "Days before idle closed-TM embeddings are cleaned up", Source: settingSource("TMI_TIMMY_EMBEDDING_IDLE_DAYS_CLOSED"), EnvVar: "TMI_TIMMY_EMBEDDING_IDLE_DAYS_CLOSED"},
		{Key: "timmy.dump_extracted_text_to_note", Value: strconv.FormatBool(t.DumpExtractedTextToNote), Type: "bool", Description: "Dump extracted text to note (dev/test only)", Source: settingSource("TMI_TIMMY_DUMP_EXTRACTED_TEXT_TO_NOTE"), EnvVar: "TMI_TIMMY_DUMP_EXTRACTED_TEXT_TO_NOTE"},
	}
	return settings
}

// DefaultOperationalSettings returns the operational-category settings from a
// default Config. It is the seed source for the DB-backed settings service.
// Bootstrap-category settings are intentionally excluded — they are file/env
// only and must never be written to the database.
func DefaultOperationalSettings() []MigratableSetting {
	all := getDefaultConfig().GetMigratableSettings()
	out := make([]MigratableSetting, 0, len(all))
	for _, s := range all {
		if s.Class.Category == CategoryOperational {
			out = append(out, s)
		}
	}
	return out
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

// getMigratableObservabilitySettings returns OpenTelemetry / Prometheus settings.
func (c *Config) getMigratableObservabilitySettings() []MigratableSetting {
	return []MigratableSetting{
		{Key: "observability.enabled", Value: strconv.FormatBool(c.Observability.Enabled), Type: "bool", Description: "OpenTelemetry tracing enabled", Source: settingSource("TMI_OTEL_ENABLED"), EnvVar: "TMI_OTEL_ENABLED"},
		{Key: "observability.prometheus_port", Value: strconv.Itoa(c.Observability.PrometheusPort), Type: "int", Description: "Prometheus metrics port (0 = disabled)", Source: settingSource("TMI_OTEL_PROMETHEUS_PORT"), EnvVar: "TMI_OTEL_PROMETHEUS_PORT"},
		{Key: "observability.sampling_rate", Value: strconv.FormatFloat(c.Observability.SamplingRate, 'f', -1, 64), Type: "string", Description: "OpenTelemetry trace sampling rate (0.0–1.0)", Source: settingSource("TMI_OTEL_SAMPLING_RATE"), EnvVar: "TMI_OTEL_SAMPLING_RATE"},
	}
}

// getMigratableSSRFSettings returns SSRF protection allowlist settings.
//
// Allowlist and schemes fields are security-sensitive: they gate outbound HTTP
// calls to external systems. An empty allowlist is fail-closed (no hosts
// permitted), so we deliberately emit only when non-empty to avoid seeding an
// empty-string CLOB row on Oracle (which is indistinguishable from NULL and
// could silently widen the allowlist to all hosts on misconfigured installs).
// The runtime SSRF validator already defaults to fail-closed when the setting
// is absent from the DB.
func (c *Config) getMigratableSSRFSettings() []MigratableSetting {
	type ssrfEntry struct {
		prefix string
		cfg    SSRFURIConfig
	}
	entries := []ssrfEntry{
		{"ssrf.issue_uri", c.SSRF.IssueURI},
		{"ssrf.document_uri", c.SSRF.DocumentURI},
		{"ssrf.repository_uri", c.SSRF.RepositoryURI},
		{"ssrf.timmy", c.SSRF.Timmy},
		{"ssrf.webhook", c.SSRF.Webhook},
	}
	settings := []MigratableSetting{}
	for _, e := range entries {
		// Only emit when the operator has explicitly configured the field;
		// empty string is fail-closed (no hosts allowed) and must not be
		// seeded as an empty CLOB row on Oracle.
		if e.cfg.Allowlist != "" {
			settings = append(settings, MigratableSetting{
				Key:         e.prefix + ".allowlist",
				Value:       e.cfg.Allowlist,
				Type:        "string",
				Description: "SSRF allowlist for " + e.prefix + " (comma-separated host patterns)",
				Source:      "config",
			})
		}
		if e.cfg.Schemes != "" {
			settings = append(settings, MigratableSetting{
				Key:         e.prefix + ".schemes",
				Value:       e.cfg.Schemes,
				Type:        "string",
				Description: "Permitted URI schemes for " + e.prefix + " (comma-separated, e.g. https)",
				Source:      "config",
			})
		}
	}
	return settings
}

// getMigratableWebhooksSettings returns webhook configuration settings.
func (c *Config) getMigratableWebhooksSettings() []MigratableSetting {
	return []MigratableSetting{
		{Key: "webhooks.allow_http_targets", Value: strconv.FormatBool(c.Webhooks.AllowHTTPTargets), Type: "bool", Description: "Allow non-HTTPS webhook target URLs (intra-cluster use only)", Source: settingSource("TMI_WEBHOOK_ALLOW_HTTP_TARGETS"), EnvVar: "TMI_WEBHOOK_ALLOW_HTTP_TARGETS"},
	}
}

// getMigratableContentExtractorsSettings returns OOXML extractor pipeline limits.
func (c *Config) getMigratableContentExtractorsSettings() []MigratableSetting {
	e := c.ContentExtractors
	return []MigratableSetting{
		{Key: "content_extractors.compressed_size_bytes", Value: strconv.FormatInt(e.CompressedSizeBytes, 10), Type: "int", Description: "Max compressed upload size in bytes", Source: settingSource("TMI_CONTENT_EXTRACTORS_COMPRESSED_SIZE_BYTES"), EnvVar: "TMI_CONTENT_EXTRACTORS_COMPRESSED_SIZE_BYTES"},
		{Key: "content_extractors.decompressed_size_bytes", Value: strconv.FormatInt(e.DecompressedSizeBytes, 10), Type: "int", Description: "Max decompressed content size in bytes", Source: settingSource("TMI_CONTENT_EXTRACTORS_DECOMPRESSED_SIZE_BYTES"), EnvVar: "TMI_CONTENT_EXTRACTORS_DECOMPRESSED_SIZE_BYTES"},
		{Key: "content_extractors.part_size_bytes", Value: strconv.FormatInt(e.PartSizeBytes, 10), Type: "int", Description: "Max size of a single archive part in bytes", Source: settingSource("TMI_CONTENT_EXTRACTORS_PART_SIZE_BYTES"), EnvVar: "TMI_CONTENT_EXTRACTORS_PART_SIZE_BYTES"},
		{Key: "content_extractors.pptx_slides", Value: strconv.Itoa(e.PPTXSlides), Type: "int", Description: "Max number of PowerPoint slides to extract", Source: settingSource("TMI_CONTENT_EXTRACTORS_PPTX_SLIDES"), EnvVar: "TMI_CONTENT_EXTRACTORS_PPTX_SLIDES"},
		{Key: "content_extractors.xlsx_cells", Value: strconv.Itoa(e.XLSXCells), Type: "int", Description: "Max number of Excel cells to extract", Source: settingSource("TMI_CONTENT_EXTRACTORS_XLSX_CELLS"), EnvVar: "TMI_CONTENT_EXTRACTORS_XLSX_CELLS"},
		{Key: "content_extractors.markdown_size_bytes", Value: strconv.FormatInt(e.MarkdownSizeBytes, 10), Type: "int", Description: "Max markdown output size in bytes", Source: settingSource("TMI_CONTENT_EXTRACTORS_MARKDOWN_SIZE_BYTES"), EnvVar: "TMI_CONTENT_EXTRACTORS_MARKDOWN_SIZE_BYTES"},
		{Key: "content_extractors.wall_clock_budget", Value: e.WallClockBudget.String(), Type: "string", Description: "Max wall-clock time for a single extraction", Source: settingSource("TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET"), EnvVar: "TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET"},
		{Key: "content_extractors.per_user_concurrency_default", Value: strconv.Itoa(e.PerUserConcurrencyDefault), Type: "int", Description: "Default max concurrent extractions per user", Source: settingSource("TMI_CONTENT_EXTRACTORS_PER_USER_CONCURRENCY_DEFAULT"), EnvVar: "TMI_CONTENT_EXTRACTORS_PER_USER_CONCURRENCY_DEFAULT"},
		{Key: "extraction.async_enabled", Value: strconv.FormatBool(e.AsyncEnabled), Type: "bool", Description: "Route document extraction through the async worker pipeline instead of inline (default false; requires NATS)", Source: settingSource("TMI_EXTRACTION_ASYNC_ENABLED"), EnvVar: "TMI_EXTRACTION_ASYNC_ENABLED"},
	}
}

// getMigratableContentOAuthSettings returns delegated content OAuth settings.
//
// callback_url is omitted when empty to avoid seeding an empty-string CLOB row
// on Oracle. content_oauth.providers.* are dynamic-cardinality and handled by
// the prefix classification; individual provider secrets already carry
// Secret:true in the per-provider helper below.
func (c *Config) getMigratableContentOAuthSettings() []MigratableSetting {
	settings := []MigratableSetting{}
	if c.ContentOAuth.CallbackURL != "" {
		settings = append(settings, MigratableSetting{
			Key:         "content_oauth.callback_url",
			Value:       c.ContentOAuth.CallbackURL,
			Type:        "string",
			Description: "Content OAuth callback URL",
			Source:      settingSource("TMI_CONTENT_OAUTH_CALLBACK_URL"),
			EnvVar:      "TMI_CONTENT_OAUTH_CALLBACK_URL",
		})
	}
	// content_oauth.providers.* — per-provider helper
	for providerKey, p := range c.ContentOAuth.Providers {
		if !p.Enabled {
			continue
		}
		settings = append(settings, getMigratableContentOAuthProviderSettings(providerKey, p)...)
	}
	return settings
}

// getMigratableContentOAuthProviderSettings returns settings for a single content OAuth provider.
func getMigratableContentOAuthProviderSettings(providerKey string, p ContentOAuthProviderConfig) []MigratableSetting {
	prefix := "content_oauth.providers." + providerKey
	settings := []MigratableSetting{
		{Key: prefix + ".enabled", Value: "true", Type: "bool", Description: "Content OAuth provider enabled", Source: "config"},
	}
	stringFields := []struct{ suffix, value, desc string }{
		{".name", p.Name, "Content OAuth provider display name"},
		{".icon", p.Icon, "Content OAuth provider icon"},
		{".client_id", p.ClientID, "Content OAuth provider client ID"},
		{".auth_url", p.AuthURL, "Content OAuth authorization URL"},
		{".token_url", p.TokenURL, "Content OAuth token URL"},
		{".userinfo_url", p.UserinfoURL, "Content OAuth userinfo URL"},
		{".revocation_url", p.RevocationURL, "Content OAuth token revocation URL"},
	}
	for _, f := range stringFields {
		if f.value != "" {
			settings = append(settings, MigratableSetting{
				Key: prefix + f.suffix, Value: f.value, Type: "string", Description: f.desc, Source: "config",
			})
		}
	}
	// Client secret — masked in API responses. Guarded with a non-empty check
	// like its sibling string fields so an enabled provider with an empty
	// ClientSecret never emits an empty-valued setting (Oracle empty-CLOB safe
	// at the source, rather than relying solely on downstream skip guards).
	if p.ClientSecret != "" {
		settings = append(settings, MigratableSetting{
			Key: prefix + ".client_secret", Value: p.ClientSecret, Type: "string",
			Description: "Content OAuth client secret", Source: "config", Secret: true,
		})
	}
	if len(p.RequiredScopes) > 0 {
		scopesJSON, _ := json.Marshal(p.RequiredScopes)
		settings = append(settings, MigratableSetting{
			Key: prefix + ".required_scopes", Value: string(scopesJSON), Type: "json",
			Description: "Content OAuth required scopes", Source: "config",
		})
	}
	return settings
}

// getMigratableContentSourcesSettings returns content source provider settings.
//
// String fields that are filesystem paths or email addresses are omitted when
// empty (Oracle empty-CLOB safe). Boolean enabled flags and picker public IDs
// always emit since they have meaningful zero/sentinel values.
func (c *Config) getMigratableContentSourcesSettings() []MigratableSetting {
	settings := []MigratableSetting{}

	// Google Drive
	gd := c.ContentSources.GoogleDrive
	settings = append(settings,
		MigratableSetting{Key: "content_sources.google_drive.enabled", Value: strconv.FormatBool(gd.Enabled), Type: "bool", Description: "Google Drive content source enabled", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED"},
	)
	if gd.ServiceAccountEmail != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_drive.service_account_email", Value: gd.ServiceAccountEmail, Type: "string", Description: "Google Drive service account email", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL"})
	}
	if gd.CredentialsFile != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_drive.credentials_file", Value: gd.CredentialsFile, Type: "string", Description: "Google Drive service account credentials file path", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE"})
	}
	if gd.BrowserOAuthClientID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_drive.browser_oauth_client_id", Value: gd.BrowserOAuthClientID, Type: "string", Description: "Google Drive browser OAuth client ID (public)", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_BROWSER_OAUTH_CLIENT_ID"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_BROWSER_OAUTH_CLIENT_ID"})
	}
	if gd.PickerDeveloperKey != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_drive.picker_developer_key", Value: gd.PickerDeveloperKey, Type: "string", Description: "Google Drive Picker developer key (public)", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_DEVELOPER_KEY"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_DEVELOPER_KEY"})
	}
	if gd.PickerAppID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_drive.picker_app_id", Value: gd.PickerAppID, Type: "string", Description: "Google Drive Picker app ID (public)", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_APP_ID"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_DRIVE_PICKER_APP_ID"})
	}

	// Google Workspace
	gw := c.ContentSources.GoogleWorkspace
	settings = append(settings,
		MigratableSetting{Key: "content_sources.google_workspace.enabled", Value: strconv.FormatBool(gw.Enabled), Type: "bool", Description: "Google Workspace content source enabled", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_ENABLED"},
	)
	if gw.PickerDeveloperKey != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_workspace.picker_developer_key", Value: gw.PickerDeveloperKey, Type: "string", Description: "Google Workspace Picker developer key (public)", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_DEVELOPER_KEY"})
	}
	if gw.PickerAppID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.google_workspace.picker_app_id", Value: gw.PickerAppID, Type: "string", Description: "Google Workspace Picker app ID (public)", Source: settingSource("TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID"), EnvVar: "TMI_CONTENT_SOURCE_GOOGLE_WORKSPACE_PICKER_APP_ID"})
	}

	// Confluence
	cf := c.ContentSources.Confluence
	settings = append(settings,
		MigratableSetting{Key: "content_sources.confluence.enabled", Value: strconv.FormatBool(cf.Enabled), Type: "bool", Description: "Confluence content source enabled", Source: settingSource("TMI_CONTENT_SOURCE_CONFLUENCE_ENABLED"), EnvVar: "TMI_CONTENT_SOURCE_CONFLUENCE_ENABLED"},
	)

	// Microsoft
	ms := c.ContentSources.Microsoft
	settings = append(settings,
		MigratableSetting{Key: "content_sources.microsoft.enabled", Value: strconv.FormatBool(ms.Enabled), Type: "bool", Description: "Microsoft content source enabled", Source: settingSource("TMI_CONTENT_SOURCE_MICROSOFT_ENABLED"), EnvVar: "TMI_CONTENT_SOURCE_MICROSOFT_ENABLED"},
	)
	if ms.TenantID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.microsoft.tenant_id", Value: ms.TenantID, Type: "string", Description: "Microsoft Entra tenant ID", Source: settingSource("TMI_CONTENT_SOURCE_MICROSOFT_TENANT_ID"), EnvVar: "TMI_CONTENT_SOURCE_MICROSOFT_TENANT_ID"})
	}
	if ms.ClientID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.microsoft.client_id", Value: ms.ClientID, Type: "string", Description: "Microsoft Entra app client ID (public)", Source: settingSource("TMI_CONTENT_SOURCE_MICROSOFT_CLIENT_ID"), EnvVar: "TMI_CONTENT_SOURCE_MICROSOFT_CLIENT_ID"})
	}
	if ms.ApplicationObjectID != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.microsoft.application_object_id", Value: ms.ApplicationObjectID, Type: "string", Description: "Microsoft Entra application object ID", Source: settingSource("TMI_CONTENT_SOURCE_MICROSOFT_APPLICATION_OBJECT_ID"), EnvVar: "TMI_CONTENT_SOURCE_MICROSOFT_APPLICATION_OBJECT_ID"})
	}
	if ms.PickerOrigin != "" {
		settings = append(settings, MigratableSetting{Key: "content_sources.microsoft.picker_origin", Value: ms.PickerOrigin, Type: "string", Description: "Microsoft Picker allowed origin URL", Source: settingSource("TMI_CONTENT_SOURCE_MICROSOFT_PICKER_ORIGIN"), EnvVar: "TMI_CONTENT_SOURCE_MICROSOFT_PICKER_ORIGIN"})
	}

	return settings
}
