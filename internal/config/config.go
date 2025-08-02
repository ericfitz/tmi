package config

import (
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
	"gopkg.in/yaml.v3"
)

// Config holds all application configuration
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Database  DatabaseConfig  `yaml:"database"`
	Auth      AuthConfig      `yaml:"auth"`
	Logging   LoggingConfig   `yaml:"logging"`
	Telemetry TelemetryConfig `yaml:"telemetry"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port                string        `yaml:"port" env:"TMI_SERVER_PORT"`
	Interface           string        `yaml:"interface" env:"TMI_SERVER_INTERFACE"`
	ReadTimeout         time.Duration `yaml:"read_timeout" env:"TMI_SERVER_READ_TIMEOUT"`
	WriteTimeout        time.Duration `yaml:"write_timeout" env:"TMI_SERVER_WRITE_TIMEOUT"`
	IdleTimeout         time.Duration `yaml:"idle_timeout" env:"TMI_SERVER_IDLE_TIMEOUT"`
	TLSEnabled          bool          `yaml:"tls_enabled" env:"TMI_TLS_ENABLED"`
	TLSCertFile         string        `yaml:"tls_cert_file" env:"TMI_TLS_CERT_FILE"`
	TLSKeyFile          string        `yaml:"tls_key_file" env:"TMI_TLS_KEY_FILE"`
	TLSSubjectName      string        `yaml:"tls_subject_name" env:"TMI_TLS_SUBJECT_NAME"`
	HTTPToHTTPSRedirect bool          `yaml:"http_to_https_redirect" env:"TMI_TLS_HTTP_REDIRECT"`
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Postgres PostgresConfig `yaml:"postgres"`
	Redis    RedisConfig    `yaml:"redis"`
}

// PostgresConfig holds PostgreSQL configuration
type PostgresConfig struct {
	Host     string `yaml:"host" env:"TMI_DATABASE_POSTGRES_HOST"`
	Port     string `yaml:"port" env:"TMI_DATABASE_POSTGRES_PORT"`
	User     string `yaml:"user" env:"TMI_DATABASE_POSTGRES_USER"`
	Password string `yaml:"password" env:"TMI_DATABASE_POSTGRES_PASSWORD"`
	Database string `yaml:"database" env:"TMI_DATABASE_POSTGRES_DATABASE"`
	SSLMode  string `yaml:"sslmode" env:"TMI_DATABASE_POSTGRES_SSLMODE"`
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string `yaml:"host" env:"TMI_DATABASE_REDIS_HOST"`
	Port     string `yaml:"port" env:"TMI_DATABASE_REDIS_PORT"`
	Password string `yaml:"password" env:"TMI_DATABASE_REDIS_PASSWORD"`
	DB       int    `yaml:"db" env:"TMI_DATABASE_REDIS_DB"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWT   JWTConfig   `yaml:"jwt"`
	OAuth OAuthConfig `yaml:"oauth"`
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret            string `yaml:"secret" env:"TMI_AUTH_JWT_SECRET"`
	ExpirationSeconds int    `yaml:"expiration_seconds" env:"TMI_AUTH_JWT_EXPIRATION_SECONDS"`
	SigningMethod     string `yaml:"signing_method" env:"TMI_AUTH_JWT_SIGNING_METHOD"`
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	CallbackURL string                         `yaml:"callback_url" env:"TMI_AUTH_OAUTH_CALLBACK_URL"`
	Providers   map[string]OAuthProviderConfig `yaml:"providers"`
}

// OAuthProviderConfig holds configuration for an OAuth provider
type OAuthProviderConfig struct {
	ID               string            `yaml:"id"`
	Name             string            `yaml:"name"`
	Enabled          bool              `yaml:"enabled"`
	Icon             string            `yaml:"icon"`
	ClientID         string            `yaml:"client_id"`
	ClientSecret     string            `yaml:"client_secret"`
	AuthorizationURL string            `yaml:"authorization_url"`
	TokenURL         string            `yaml:"token_url"`
	UserInfoURL      string            `yaml:"userinfo_url"`
	Issuer           string            `yaml:"issuer"`
	JWKSURL          string            `yaml:"jwks_url"`
	Scopes           []string          `yaml:"scopes"`
	AdditionalParams map[string]string `yaml:"additional_params"`
	EmailClaim       string            `yaml:"email_claim"`
	NameClaim        string            `yaml:"name_claim"`
	SubjectClaim     string            `yaml:"subject_claim"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level            string `yaml:"level" env:"TMI_LOGGING_LEVEL"`
	IsDev            bool   `yaml:"is_dev" env:"TMI_LOGGING_IS_DEV"`
	LogDir           string `yaml:"log_dir" env:"TMI_LOGGING_LOG_DIR"`
	MaxAgeDays       int    `yaml:"max_age_days" env:"TMI_LOGGING_MAX_AGE_DAYS"`
	MaxSizeMB        int    `yaml:"max_size_mb" env:"TMI_LOGGING_MAX_SIZE_MB"`
	MaxBackups       int    `yaml:"max_backups" env:"TMI_LOGGING_MAX_BACKUPS"`
	AlsoLogToConsole bool   `yaml:"also_log_to_console" env:"TMI_LOGGING_ALSO_LOG_TO_CONSOLE"`
}

// TelemetryConfig holds OpenTelemetry configuration
type TelemetryConfig struct {
	Enabled            bool                   `yaml:"enabled" env:"OTEL_ENABLED"`
	ServiceName        string                 `yaml:"service_name" env:"OTEL_SERVICE_NAME"`
	ServiceVersion     string                 `yaml:"service_version" env:"OTEL_SERVICE_VERSION"`
	Environment        string                 `yaml:"environment" env:"OTEL_ENVIRONMENT"`
	Tracing            TelemetryTracingConfig `yaml:"tracing"`
	Metrics            TelemetryMetricsConfig `yaml:"metrics"`
	Logging            TelemetryLoggingConfig `yaml:"logging"`
	ResourceAttributes map[string]string      `yaml:"resource_attributes"`
}

// TelemetryTracingConfig holds tracing-specific configuration
type TelemetryTracingConfig struct {
	Enabled    bool              `yaml:"enabled" env:"OTEL_TRACING_ENABLED"`
	SampleRate float64           `yaml:"sample_rate" env:"OTEL_TRACING_SAMPLE_RATE"`
	Endpoint   string            `yaml:"endpoint" env:"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"`
	Headers    map[string]string `yaml:"headers"`
}

// TelemetryMetricsConfig holds metrics-specific configuration
type TelemetryMetricsConfig struct {
	Enabled  bool              `yaml:"enabled" env:"OTEL_METRICS_ENABLED"`
	Interval time.Duration     `yaml:"interval" env:"OTEL_METRICS_INTERVAL"`
	Endpoint string            `yaml:"endpoint" env:"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"`
	Headers  map[string]string `yaml:"headers"`
}

// TelemetryLoggingConfig holds logging-specific configuration
type TelemetryLoggingConfig struct {
	Enabled            bool              `yaml:"enabled" env:"OTEL_LOGGING_ENABLED"`
	Endpoint           string            `yaml:"endpoint" env:"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT"`
	CorrelationEnabled bool              `yaml:"correlation_enabled" env:"OTEL_LOG_CORRELATION_ENABLED"`
	Headers            map[string]string `yaml:"headers"`
}

// Load loads configuration from YAML file with environment variable overrides
func Load(configFile string) (*Config, error) {
	config := getDefaultConfig()

	// Load from YAML file if provided
	if configFile != "" {
		if err := loadFromYAML(config, configFile); err != nil {
			return nil, fmt.Errorf("failed to load config from YAML: %w", err)
		}
	}

	// Override with environment variables
	if err := overrideWithEnv(config); err != nil {
		return nil, fmt.Errorf("failed to override with environment variables: %w", err)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// getDefaultConfig returns a configuration with default values
func getDefaultConfig() *Config {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}

	return &Config{
		Server: ServerConfig{
			Port:                "8080",
			Interface:           "0.0.0.0",
			ReadTimeout:         5 * time.Second,
			WriteTimeout:        10 * time.Second,
			IdleTimeout:         60 * time.Second,
			TLSEnabled:          false,
			TLSCertFile:         "",
			TLSKeyFile:          "",
			TLSSubjectName:      hostname,
			HTTPToHTTPSRedirect: true,
		},
		Database: DatabaseConfig{
			Postgres: PostgresConfig{
				Host:     "localhost",
				Port:     "5432",
				User:     "postgres",
				Password: "",
				Database: "tmi",
				SSLMode:  "disable",
			},
			Redis: RedisConfig{
				Host:     "localhost",
				Port:     "6379",
				Password: "",
				DB:       0,
			},
		},
		Auth: AuthConfig{
			JWT: JWTConfig{
				Secret:            "",
				ExpirationSeconds: 3600,
				SigningMethod:     "HS256",
			},
			OAuth: OAuthConfig{
				CallbackURL: "http://localhost:8080/auth/callback",
				Providers:   getDefaultOAuthProviders(),
			},
		},
		Logging: LoggingConfig{
			Level:            "info",
			IsDev:            true,
			LogDir:           "logs",
			MaxAgeDays:       7,
			MaxSizeMB:        100,
			MaxBackups:       10,
			AlsoLogToConsole: true,
		},
		Telemetry: TelemetryConfig{
			Enabled:        false,
			ServiceName:    "tmi-api",
			ServiceVersion: "1.0.0",
			Environment:    "development",
			Tracing: TelemetryTracingConfig{
				Enabled:    false,
				SampleRate: 1.0,
				Endpoint:   "http://localhost:4318",
				Headers:    make(map[string]string),
			},
			Metrics: TelemetryMetricsConfig{
				Enabled:  false,
				Interval: 30 * time.Second,
				Endpoint: "http://localhost:4318",
				Headers:  make(map[string]string),
			},
			Logging: TelemetryLoggingConfig{
				Enabled:            false,
				Endpoint:           "http://localhost:4318",
				CorrelationEnabled: true,
				Headers:            make(map[string]string),
			},
			ResourceAttributes: make(map[string]string),
		},
	}
}

// getDefaultOAuthProviders returns default OAuth provider configurations
func getDefaultOAuthProviders() map[string]OAuthProviderConfig {
	return map[string]OAuthProviderConfig{
		"google": {
			ID:               "google",
			Name:             "Google",
			Enabled:          true,
			Icon:             "google",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:         "https://oauth2.googleapis.com/token",
			UserInfoURL:      "https://www.googleapis.com/oauth2/v3/userinfo",
			Issuer:           "https://accounts.google.com",
			JWKSURL:          "https://www.googleapis.com/oauth2/v3/certs",
			Scopes:           []string{"openid", "profile", "email"},
			AdditionalParams: map[string]string{},
			EmailClaim:       "email",
			NameClaim:        "name",
			SubjectClaim:     "sub",
		},
		"github": {
			ID:               "github",
			Name:             "GitHub",
			Enabled:          true,
			Icon:             "github",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://github.com/login/oauth/authorize",
			TokenURL:         "https://github.com/login/oauth/access_token",
			UserInfoURL:      "https://api.github.com/user",
			Scopes:           []string{"user:email"},
			AdditionalParams: map[string]string{},
			EmailClaim:       "email",
			NameClaim:        "name",
			SubjectClaim:     "id",
		},
		"microsoft": {
			ID:               "microsoft",
			Name:             "Microsoft",
			Enabled:          true,
			Icon:             "microsoft",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:         "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfoURL:      "https://graph.microsoft.com/v1.0/me",
			Issuer:           "https://login.microsoftonline.com/common/v2.0",
			JWKSURL:          "https://login.microsoftonline.com/common/discovery/v2.0/keys",
			Scopes:           []string{"openid", "profile", "email", "User.Read"},
			AdditionalParams: map[string]string{},
			EmailClaim:       "email",
			NameClaim:        "name",
			SubjectClaim:     "sub",
		},
	}
}

// loadFromYAML loads configuration from a YAML file
func loadFromYAML(config *Config, filename string) error {
	data, err := os.ReadFile(filename) // #nosec G304
	if err != nil {
		return fmt.Errorf("failed to read config file %s: %w", filename, err)
	}

	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse YAML config: %w", err)
	}

	return nil
}

// overrideWithEnv overrides configuration values with environment variables
func overrideWithEnv(config *Config) error {
	return overrideStructWithEnv(reflect.ValueOf(config).Elem())
}

// overrideStructWithEnv recursively overrides struct fields with environment variables
func overrideStructWithEnv(v reflect.Value) error {
	t := v.Type()

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		// Skip unexported fields
		if !field.CanSet() {
			continue
		}

		// Handle nested structs
		if field.Kind() == reflect.Struct {
			if err := overrideStructWithEnv(field); err != nil {
				return err
			}
			continue
		}

		// Handle maps (for OAuth providers)
		if field.Kind() == reflect.Map && fieldType.Name == "Providers" {
			if err := overrideOAuthProviders(field); err != nil {
				return err
			}
			continue
		}

		// Get environment variable name from tag
		envTag := fieldType.Tag.Get("env")
		if envTag == "" {
			continue
		}

		// Get environment variable value
		envValue := os.Getenv(envTag)
		if envValue == "" {
			continue
		}

		// Set field value based on type
		if err := setFieldFromString(field, envValue); err != nil {
			return fmt.Errorf("failed to set field %s from env %s: %w", fieldType.Name, envTag, err)
		}
	}

	return nil
}

// overrideOAuthProviders handles environment variable overrides for OAuth providers
func overrideOAuthProviders(mapField reflect.Value) error {
	if mapField.IsNil() {
		return nil
	}

	providers := []string{"google", "github", "microsoft"}

	for _, providerID := range providers {
		providerValue := mapField.MapIndex(reflect.ValueOf(providerID))
		if !providerValue.IsValid() {
			continue
		}

		// Create a copy of the provider config to modify
		provider := providerValue.Interface().(OAuthProviderConfig)

		// Override provider-specific environment variables
		envPrefix := fmt.Sprintf("TMI_AUTH_OAUTH_PROVIDERS_%s_", strings.ToUpper(providerID))

		if val := os.Getenv(envPrefix + "ENABLED"); val != "" {
			provider.Enabled = val == "true"
		}
		if val := os.Getenv(envPrefix + "CLIENT_ID"); val != "" {
			provider.ClientID = val
		}
		if val := os.Getenv(envPrefix + "CLIENT_SECRET"); val != "" {
			provider.ClientSecret = val
		}

		// Set the modified provider back to the map
		mapField.SetMapIndex(reflect.ValueOf(providerID), reflect.ValueOf(provider))
	}

	return nil
}

// setFieldFromString sets a struct field value from a string based on the field type
func setFieldFromString(field reflect.Value, value string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		boolVal, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool value: %s", value)
		}
		field.SetBool(boolVal)
	case reflect.Int:
		intVal, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int value: %s", value)
		}
		field.SetInt(int64(intVal))
	case reflect.Int64:
		// Handle time.Duration specially
		if field.Type() == reflect.TypeOf(time.Duration(0)) {
			duration, err := time.ParseDuration(value)
			if err != nil {
				return fmt.Errorf("invalid duration value: %s", value)
			}
			field.SetInt(int64(duration))
		} else {
			intVal, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return fmt.Errorf("invalid int64 value: %s", value)
			}
			field.SetInt(intVal)
		}
	default:
		return fmt.Errorf("unsupported field type: %s", field.Kind())
	}
	return nil
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate server configuration
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}

	// Validate database configuration
	if c.Database.Postgres.Host == "" {
		return fmt.Errorf("postgres host is required")
	}
	if c.Database.Postgres.Port == "" {
		return fmt.Errorf("postgres port is required")
	}
	if c.Database.Postgres.User == "" {
		return fmt.Errorf("postgres user is required")
	}
	if c.Database.Postgres.Database == "" {
		return fmt.Errorf("postgres database is required")
	}

	if c.Database.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}
	if c.Database.Redis.Port == "" {
		return fmt.Errorf("redis port is required")
	}

	// Validate JWT configuration
	if c.Auth.JWT.Secret == "" {
		return fmt.Errorf("jwt secret is required")
	}
	if c.Auth.JWT.ExpirationSeconds <= 0 {
		return fmt.Errorf("jwt expiration must be greater than 0")
	}

	// Validate OAuth configuration
	if c.Auth.OAuth.CallbackURL == "" {
		return fmt.Errorf("oauth callback url is required")
	}

	// Check that at least one OAuth provider is enabled and configured
	hasEnabledProvider := false
	for _, provider := range c.Auth.OAuth.Providers {
		if provider.Enabled && provider.ClientID != "" && provider.ClientSecret != "" {
			hasEnabledProvider = true
			break
		}
	}
	if !hasEnabledProvider {
		return fmt.Errorf("at least one oauth provider must be enabled and configured")
	}

	return nil
}

// GetJWTDuration returns the JWT expiration duration
func (c *Config) GetJWTDuration() time.Duration {
	return time.Duration(c.Auth.JWT.ExpirationSeconds) * time.Second
}

// GetLogLevel returns the parsed log level
func (c *Config) GetLogLevel() logging.LogLevel {
	return logging.ParseLogLevel(c.Logging.Level)
}

// GetEnabledOAuthProviders returns a slice of enabled OAuth providers
func (c *Config) GetEnabledOAuthProviders() []OAuthProviderConfig {
	var enabled []OAuthProviderConfig
	for _, provider := range c.Auth.OAuth.Providers {
		if provider.Enabled {
			enabled = append(enabled, provider)
		}
	}
	return enabled
}

// GetOAuthProvider returns a specific OAuth provider configuration
func (c *Config) GetOAuthProvider(providerID string) (OAuthProviderConfig, bool) {
	provider, exists := c.Auth.OAuth.Providers[providerID]
	if !exists || !provider.Enabled {
		return OAuthProviderConfig{}, false
	}
	return provider, true
}

// GetTelemetryConfig converts the runtime TelemetryConfig to the format expected by the telemetry package
func (c *Config) GetTelemetryConfig() *TelemetryRuntimeConfig {
	return &TelemetryRuntimeConfig{
		ServiceName:    c.Telemetry.ServiceName,
		ServiceVersion: c.Telemetry.ServiceVersion,
		Environment:    c.Telemetry.Environment,

		TracingEnabled:    c.Telemetry.Enabled && c.Telemetry.Tracing.Enabled,
		TracingSampleRate: c.Telemetry.Tracing.SampleRate,
		TracingEndpoint:   c.Telemetry.Tracing.Endpoint,
		TracingHeaders:    c.Telemetry.Tracing.Headers,

		MetricsEnabled:  c.Telemetry.Enabled && c.Telemetry.Metrics.Enabled,
		MetricsInterval: c.Telemetry.Metrics.Interval,
		MetricsEndpoint: c.Telemetry.Metrics.Endpoint,
		MetricsHeaders:  c.Telemetry.Metrics.Headers,

		LoggingEnabled:        c.Telemetry.Enabled && c.Telemetry.Logging.Enabled,
		LoggingEndpoint:       c.Telemetry.Logging.Endpoint,
		LogCorrelationEnabled: c.Telemetry.Logging.CorrelationEnabled,
		LoggingHeaders:        c.Telemetry.Logging.Headers,

		ResourceAttributes: c.Telemetry.ResourceAttributes,

		IsDevelopment:   strings.ToLower(c.Telemetry.Environment) == "development",
		ConsoleExporter: false, // Can be added to config if needed
		DebugMode:       false, // Can be added to config if needed
	}
}

// TelemetryRuntimeConfig matches the structure expected by the telemetry package
type TelemetryRuntimeConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string

	TracingEnabled    bool
	TracingSampleRate float64
	TracingEndpoint   string
	TracingHeaders    map[string]string

	MetricsEnabled  bool
	MetricsInterval time.Duration
	MetricsEndpoint string
	MetricsHeaders  map[string]string

	LoggingEnabled        bool
	LoggingEndpoint       string
	LogCorrelationEnabled bool
	LoggingHeaders        map[string]string

	ResourceAttributes map[string]string

	IsDevelopment   bool
	ConsoleExporter bool
	DebugMode       bool
}
