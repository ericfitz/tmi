package config

import (
	"encoding/json"
	"strconv"
)

// serverSettingDefs declares the infrastructure bootstrap settings: server,
// database (including its nested connection pool and Redis coordinates),
// logging, and observability (OpenTelemetry/Prometheus). See setting_def.go
// for the SettingDef contract and classification_registry.go for the source
// of truth on each key's ConfigClass.
var serverSettingDefs = []SettingDef{
	// --- server.* ---
	{
		Key:         "server.port",
		Class:       bootstrapClass(true, VisibilityInternal, false),
		Type:        "string",
		Description: "HTTP server port",
		YAMLPath:    "server.port",
		EnvVar:      "TMI_SERVER_PORT",
		Get:         func(c *Config) string { return c.Server.Port },
	},
	{
		Key:         "server.interface",
		Class:       bootstrapClass(true, VisibilityInternal, false),
		Type:        "string",
		Description: "Network interface to bind to",
		YAMLPath:    "server.interface",
		EnvVar:      "TMI_SERVER_INTERFACE",
		Get:         func(c *Config) string { return c.Server.Interface },
	},
	{
		Key:         "server.base_url",
		Class:       bootstrapClass(false, VisibilityPublic, false),
		Type:        "string",
		Description: "Public base URL for callbacks",
		YAMLPath:    "server.base_url",
		EnvVar:      "TMI_SERVER_BASE_URL",
		Get:         func(c *Config) string { return c.Server.BaseURL },
	},
	{
		Key:         "server.read_timeout",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "HTTP read timeout",
		YAMLPath:    "server.read_timeout",
		EnvVar:      "TMI_SERVER_READ_TIMEOUT",
		Get:         func(c *Config) string { return c.Server.ReadTimeout.String() },
	},
	{
		Key:         "server.write_timeout",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "HTTP write timeout",
		YAMLPath:    "server.write_timeout",
		EnvVar:      "TMI_SERVER_WRITE_TIMEOUT",
		Get:         func(c *Config) string { return c.Server.WriteTimeout.String() },
	},
	{
		Key:         "server.idle_timeout",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "HTTP idle timeout",
		YAMLPath:    "server.idle_timeout",
		EnvVar:      "TMI_SERVER_IDLE_TIMEOUT",
		Get:         func(c *Config) string { return c.Server.IdleTimeout.String() },
	},
	{
		Key:         "server.tls_enabled",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "TLS enabled",
		YAMLPath:    "server.tls_enabled",
		EnvVar:      "TMI_SERVER_TLS_ENABLED",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Server.TLSEnabled) },
	},
	{
		Key:         "server.tls_cert_file",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "TLS certificate file path",
		YAMLPath:    "server.tls_cert_file",
		EnvVar:      "TMI_SERVER_TLS_CERT_FILE",
		Get:         func(c *Config) string { return c.Server.TLSCertFile },
	},
	{
		Key:         "server.tls_key_file",
		Class:       bootstrapClass(false, VisibilityInternal, true),
		Type:        "string",
		Description: "TLS key file path",
		YAMLPath:    "server.tls_key_file",
		EnvVar:      "TMI_SERVER_TLS_KEY_FILE",
		Get:         func(c *Config) string { return c.Server.TLSKeyFile },
	},
	{
		Key:         "server.tls_subject_name",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "TLS certificate subject name",
		YAMLPath:    "server.tls_subject_name",
		EnvVar:      "TMI_SERVER_TLS_SUBJECT_NAME",
		Get:         func(c *Config) string { return c.Server.TLSSubjectName },
	},
	{
		Key:         "server.http_to_https_redirect",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "HTTP to HTTPS redirect",
		YAMLPath:    "server.http_to_https_redirect",
		EnvVar:      "TMI_SERVER_HTTP_TO_HTTPS_REDIRECT",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Server.HTTPToHTTPSRedirect) },
	},
	// server.trusted_proxies has no entry in exactClassifications (see
	// classification_registry.go) and no entry in migratable_settings.go's
	// getMigratableServerSettings — it is unclassified today. Falling back to
	// the same bootstrapClass(false, VisibilityInternal, false) shape as its
	// ServerConfig siblings; flagged for the wiring/validation pass.
	{
		Key:         "server.trusted_proxies",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "json",
		Description: "Comma-separated CIDRs/IPs for X-Forwarded-For trusted-proxy validation",
		YAMLPath:    "server.trusted_proxies",
		EnvVar:      "TMI_TRUSTED_PROXIES",
		Get: func(c *Config) string {
			b, err := json.Marshal(c.Server.TrustedProxies)
			if err != nil {
				return "[]"
			}
			return string(b)
		},
	},
	{
		Key:          "server.disable_rate_limiting",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "bool",
		Description:  "Disable all rate limiting (dev/test only)",
		Default:      "false",
		YAMLPath:     "server.disable_rate_limiting",
		EnvVar:       "TMI_DISABLE_RATE_LIMITING",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Server.DisableRateLimiting) },
		Transitional: true,
	},
	// server.ratelimit_public_rpm's compiled-in Config default is the Go zero
	// value 0 (getDefaultConfig does not set ServerConfig.RateLimitPublicRPM);
	// applyRateLimitConfig in cmd/server/main.go treats 0 as "no override,
	// keep the rate limiter's own built-in default" rather than literally
	// zero requests/min. The struct-tag comment's "(default: 10)" describes
	// that runtime fallback, not this Default field, which must mirror the
	// actual compiled-in Config value.
	{
		Key:          "server.ratelimit_public_rpm",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "int",
		Description:  "Requests per minute per IP for public endpoints",
		Default:      "0",
		YAMLPath:     "server.ratelimit_public_rpm",
		EnvVar:       "TMI_RATELIMIT_PUBLIC_RPM",
		Get:          func(c *Config) string { return strconv.Itoa(c.Server.RateLimitPublicRPM) },
		Transitional: true,
	},
	{
		Key:          "server.require_if_match",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "bool",
		Description:  "Return 428 when If-Match header is missing on PUT/PATCH",
		Default:      "false",
		YAMLPath:     "server.require_if_match",
		EnvVar:       "TMI_REQUIRE_IF_MATCH",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Server.RequireIfMatch) },
		Transitional: true,
	},
	{
		Key:         "server.cors.allowed_origins",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "json",
		Description: "CORS allowed origins",
		YAMLPath:    "server.cors.allowed_origins",
		EnvVar:      "TMI_CORS_ALLOWED_ORIGINS",
		Get: func(c *Config) string {
			b, err := json.Marshal(c.Server.CORS.AllowedOrigins)
			if err != nil {
				return "[]"
			}
			return string(b)
		},
	},

	// --- database.* ---
	{
		Key:         "database.url",
		Class:       bootstrapClass(true, VisibilityInternal, true),
		Type:        "string",
		Description: "Database connection URL (password redacted)",
		YAMLPath:    "database.url",
		EnvVar:      "TMI_DATABASE_URL",
		Get:         func(c *Config) string { return sanitizeURL(c.Database.URL) },
	},
	// database.oracle_wallet_location has no entry in exactClassifications
	// and is deliberately excluded from GetMigratableSettings
	// (ExpectedMigratableKeysSkipped in migratable_discovery.go: "filesystem
	// path used at DB connect time; cannot be DB-backed by construction").
	// It is still a real bootstrap config/env-delivered field, so it gets a
	// SettingDef with the same fallback shape as other unclassified bootstrap
	// keys; flagged for the wiring/validation pass.
	{
		Key:         "database.oracle_wallet_location",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "Path to Oracle wallet directory (Oracle ADB only)",
		YAMLPath:    "database.oracle_wallet_location",
		EnvVar:      "TMI_ORACLE_WALLET_LOCATION",
		Get:         func(c *Config) string { return c.Database.OracleWalletLocation },
	},
	{
		Key:         "database.connection_pool.max_open_conns",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Maximum open database connections",
		YAMLPath:    "database.connection_pool.max_open_conns",
		EnvVar:      "TMI_DB_MAX_OPEN_CONNS",
		Get:         func(c *Config) string { return strconv.Itoa(c.Database.ConnectionPool.MaxOpenConns) },
	},
	{
		Key:         "database.connection_pool.max_idle_conns",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Maximum idle database connections",
		YAMLPath:    "database.connection_pool.max_idle_conns",
		EnvVar:      "TMI_DB_MAX_IDLE_CONNS",
		Get:         func(c *Config) string { return strconv.Itoa(c.Database.ConnectionPool.MaxIdleConns) },
	},
	{
		Key:         "database.connection_pool.conn_max_lifetime",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Max connection lifetime in seconds",
		YAMLPath:    "database.connection_pool.conn_max_lifetime",
		EnvVar:      "TMI_DB_CONN_MAX_LIFETIME",
		Get:         func(c *Config) string { return strconv.Itoa(c.Database.ConnectionPool.ConnMaxLifetime) },
	},
	{
		Key:         "database.connection_pool.conn_max_idle_time",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Max connection idle time in seconds",
		YAMLPath:    "database.connection_pool.conn_max_idle_time",
		EnvVar:      "TMI_DB_CONN_MAX_IDLE_TIME",
		Get:         func(c *Config) string { return strconv.Itoa(c.Database.ConnectionPool.ConnMaxIdleTime) },
	},
	{
		Key:         "database.redis.url",
		Class:       bootstrapClass(false, VisibilityInternal, true),
		Type:        "string",
		Description: "Redis connection URL (password redacted)",
		YAMLPath:    "database.redis.url",
		EnvVar:      "TMI_REDIS_URL",
		Get:         func(c *Config) string { return sanitizeURL(c.Database.Redis.URL) },
	},
	{
		Key:         "database.redis.host",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "Redis host",
		YAMLPath:    "database.redis.host",
		EnvVar:      "TMI_REDIS_HOST",
		Get:         func(c *Config) string { return c.Database.Redis.Host },
	},
	{
		Key:         "database.redis.port",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "Redis port",
		YAMLPath:    "database.redis.port",
		EnvVar:      "TMI_REDIS_PORT",
		Get:         func(c *Config) string { return c.Database.Redis.Port },
	},
	{
		Key:         "database.redis.password",
		Class:       bootstrapClass(false, VisibilityInternal, true),
		Type:        "string",
		Description: "Redis password",
		YAMLPath:    "database.redis.password",
		EnvVar:      "TMI_REDIS_PASSWORD",
		Get:         func(c *Config) string { return c.Database.Redis.Password },
	},
	{
		Key:         "database.redis.db",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Redis database number",
		YAMLPath:    "database.redis.db",
		EnvVar:      "TMI_REDIS_DB",
		Get:         func(c *Config) string { return strconv.Itoa(c.Database.Redis.DB) },
	},

	// --- logging.* ---
	{
		Key:         "logging.level",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "Log level",
		YAMLPath:    "logging.level",
		EnvVar:      "TMI_LOG_LEVEL",
		Get:         func(c *Config) string { return c.Logging.Level },
	},
	{
		Key:         "logging.is_dev",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Development mode logging",
		YAMLPath:    "logging.is_dev",
		EnvVar:      "TMI_LOG_IS_DEV",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.IsDev) },
	},
	{
		Key:         "logging.is_test",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Test mode logging",
		YAMLPath:    "logging.is_test",
		EnvVar:      "TMI_LOG_IS_TEST",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.IsTest) },
	},
	{
		Key:         "logging.log_dir",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "string",
		Description: "Log directory",
		YAMLPath:    "logging.log_dir",
		EnvVar:      "TMI_LOG_DIR",
		Get:         func(c *Config) string { return c.Logging.LogDir },
	},
	{
		Key:         "logging.max_age_days",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Log max age in days",
		YAMLPath:    "logging.max_age_days",
		EnvVar:      "TMI_LOG_MAX_AGE_DAYS",
		Get:         func(c *Config) string { return strconv.Itoa(c.Logging.MaxAgeDays) },
	},
	{
		Key:         "logging.max_size_mb",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Log max size in MB",
		YAMLPath:    "logging.max_size_mb",
		EnvVar:      "TMI_LOG_MAX_SIZE_MB",
		Get:         func(c *Config) string { return strconv.Itoa(c.Logging.MaxSizeMB) },
	},
	{
		Key:         "logging.max_backups",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Log max backup count",
		YAMLPath:    "logging.max_backups",
		EnvVar:      "TMI_LOG_MAX_BACKUPS",
		Get:         func(c *Config) string { return strconv.Itoa(c.Logging.MaxBackups) },
	},
	{
		Key:         "logging.also_log_to_console",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Also log to console",
		YAMLPath:    "logging.also_log_to_console",
		EnvVar:      "TMI_LOG_ALSO_LOG_TO_CONSOLE",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.AlsoLogToConsole) },
	},
	{
		Key:         "logging.cloud_error_threshold",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "int",
		Description: "Cloud sink consecutive-failure threshold for one-shot Warn alarm (0 disables)",
		YAMLPath:    "logging.cloud_error_threshold",
		EnvVar:      "TMI_LOG_CLOUD_ERROR_THRESHOLD",
		Get:         func(c *Config) string { return strconv.Itoa(c.Logging.CloudErrorThreshold) },
	},
	{
		Key:         "logging.log_api_requests",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Log API requests",
		YAMLPath:    "logging.log_api_requests",
		EnvVar:      "TMI_LOG_API_REQUESTS",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.LogAPIRequests) },
	},
	{
		Key:         "logging.log_api_responses",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Log API responses",
		YAMLPath:    "logging.log_api_responses",
		EnvVar:      "TMI_LOG_API_RESPONSES",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.LogAPIResponses) },
	},
	{
		Key:         "logging.log_websocket_messages",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Log WebSocket messages",
		YAMLPath:    "logging.log_websocket_messages",
		EnvVar:      "TMI_LOG_WEBSOCKET_MESSAGES",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.LogWebSocketMsg) },
	},
	{
		Key:         "logging.redact_auth_tokens",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Redact auth tokens in logs",
		YAMLPath:    "logging.redact_auth_tokens",
		EnvVar:      "TMI_LOG_REDACT_AUTH_TOKENS",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.RedactAuthTokens) },
	},
	{
		Key:         "logging.suppress_unauthenticated_logs",
		Class:       bootstrapClass(false, VisibilityInternal, false),
		Type:        "bool",
		Description: "Suppress unauthenticated request logs",
		YAMLPath:    "logging.suppress_unauthenticated_logs",
		EnvVar:      "TMI_LOG_SUPPRESS_UNAUTH_LOGS",
		Get:         func(c *Config) string { return strconv.FormatBool(c.Logging.SuppressUnauthenticatedLogs) },
	},

	// --- observability.* ---
	{
		Key:          "observability.enabled",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "bool",
		Description:  "OpenTelemetry tracing enabled",
		Default:      "false",
		YAMLPath:     "observability.enabled",
		EnvVar:       "TMI_OTEL_ENABLED",
		Get:          func(c *Config) string { return strconv.FormatBool(c.Observability.Enabled) },
		Transitional: true,
	},
	{
		Key:          "observability.prometheus_port",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "int",
		Description:  "Prometheus metrics port (0 = disabled)",
		Default:      "0",
		YAMLPath:     "observability.prometheus_port",
		EnvVar:       "TMI_OTEL_PROMETHEUS_PORT",
		Get:          func(c *Config) string { return strconv.Itoa(c.Observability.PrometheusPort) },
		Transitional: true,
	},
	// Type is "float", not "string": Observability.SamplingRate is a
	// float64, and mislabelling it made --export-config emit a quoted "1"
	// that --import-config could not unmarshal back into the field (#791).
	{
		Key:          "observability.sampling_rate",
		Class:        operationalClass(VisibilityAdminOnly, false),
		Type:         "float",
		Description:  "OpenTelemetry trace sampling rate (0.0-1.0)",
		Default:      "1",
		YAMLPath:     "observability.sampling_rate",
		EnvVar:       "TMI_OTEL_SAMPLING_RATE",
		Get:          func(c *Config) string { return strconv.FormatFloat(c.Observability.SamplingRate, 'f', -1, 64) },
		Transitional: true,
	},
}
