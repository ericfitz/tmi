package config

import (
	"flag"
	"fmt"
	"net"
	"os"
	"reflect"
	"regexp"
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
	WebSocket WebSocketConfig `yaml:"websocket"`
	Logging   LoggingConfig   `yaml:"logging"`
	Admin     AdminConfig     `yaml:"admin"`
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

// UserInfoEndpoint represents a single userinfo endpoint and its claim mappings
type UserInfoEndpoint struct {
	URL    string            `yaml:"url"`
	Claims map[string]string `yaml:"claims"`
}

// OAuthProviderConfig holds configuration for an OAuth provider
type OAuthProviderConfig struct {
	ID               string             `yaml:"id"`
	Name             string             `yaml:"name"`
	Enabled          bool               `yaml:"enabled"`
	Icon             string             `yaml:"icon"`
	ClientID         string             `yaml:"client_id"`
	ClientSecret     string             `yaml:"client_secret"`
	AuthorizationURL string             `yaml:"authorization_url"`
	TokenURL         string             `yaml:"token_url"`
	UserInfo         []UserInfoEndpoint `yaml:"userinfo"`
	Issuer           string             `yaml:"issuer"`
	JWKSURL          string             `yaml:"jwks_url"`
	Scopes           []string           `yaml:"scopes"`
	AdditionalParams map[string]string  `yaml:"additional_params"`
	AuthHeaderFormat string             `yaml:"auth_header_format,omitempty"`
	AcceptHeader     string             `yaml:"accept_header,omitempty"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level            string `yaml:"level" env:"TMI_LOGGING_LEVEL"`
	IsDev            bool   `yaml:"is_dev" env:"TMI_LOGGING_IS_DEV"`
	IsTest           bool   `yaml:"is_test" env:"TMI_LOGGING_IS_TEST"`
	LogDir           string `yaml:"log_dir" env:"TMI_LOGGING_LOG_DIR"`
	MaxAgeDays       int    `yaml:"max_age_days" env:"TMI_LOGGING_MAX_AGE_DAYS"`
	MaxSizeMB        int    `yaml:"max_size_mb" env:"TMI_LOGGING_MAX_SIZE_MB"`
	MaxBackups       int    `yaml:"max_backups" env:"TMI_LOGGING_MAX_BACKUPS"`
	AlsoLogToConsole bool   `yaml:"also_log_to_console" env:"TMI_LOGGING_ALSO_LOG_TO_CONSOLE"`
	// Enhanced debug logging options
	LogAPIRequests              bool `yaml:"log_api_requests" env:"TMI_LOGGING_LOG_API_REQUESTS"`
	LogAPIResponses             bool `yaml:"log_api_responses" env:"TMI_LOGGING_LOG_API_RESPONSES"`
	LogWebSocketMsg             bool `yaml:"log_websocket_messages" env:"TMI_LOGGING_LOG_WEBSOCKET_MESSAGES"`
	RedactAuthTokens            bool `yaml:"redact_auth_tokens" env:"TMI_LOGGING_REDACT_AUTH_TOKENS"`
	SuppressUnauthenticatedLogs bool `yaml:"suppress_unauthenticated_logs" env:"TMI_LOGGING_SUPPRESS_UNAUTH_LOGS"`
}

// WebSocketConfig holds WebSocket timeout configuration
type WebSocketConfig struct {
	InactivityTimeoutSeconds int `yaml:"inactivity_timeout_seconds" env:"TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS"`
}

// AdminConfig holds admin interface configuration
type AdminConfig struct {
	Enabled    bool                `yaml:"enabled" env:"TMI_ADMIN_ENABLED"`
	PathPrefix string              `yaml:"path_prefix" env:"TMI_ADMIN_PATH_PREFIX"`
	Users      AdminUsersConfig    `yaml:"users"`
	Session    AdminSessionConfig  `yaml:"session"`
	Security   AdminSecurityConfig `yaml:"security"`
}

// AdminUsersConfig holds admin user configuration
type AdminUsersConfig struct {
	PrimaryEmail     string   `yaml:"primary_email" env:"TMI_ADMIN_USERS_PRIMARY_EMAIL"`
	AdditionalEmails []string `yaml:"additional_emails" env:"TMI_ADMIN_USERS_ADDITIONAL_EMAILS"`
	AutoPromoteFirst bool     `yaml:"auto_promote_first" env:"TMI_ADMIN_USERS_AUTO_PROMOTE_FIRST"`
}

// AdminSessionConfig holds admin session configuration
type AdminSessionConfig struct {
	TimeoutMinutes   int  `yaml:"timeout_minutes" env:"TMI_ADMIN_SESSION_TIMEOUT_MINUTES"`
	ReauthRequired   bool `yaml:"reauth_required" env:"TMI_ADMIN_SESSION_REAUTH_REQUIRED"`
	ExtendOnActivity bool `yaml:"extend_on_activity" env:"TMI_ADMIN_SESSION_EXTEND_ON_ACTIVITY"`
}

// AdminSecurityConfig holds admin security configuration
type AdminSecurityConfig struct {
	RequireMFA       bool     `yaml:"require_mfa" env:"TMI_ADMIN_SECURITY_REQUIRE_MFA"`
	AuditLogging     bool     `yaml:"audit_logging" env:"TMI_ADMIN_SECURITY_AUDIT_LOGGING"`
	RateLimitEnabled bool     `yaml:"rate_limit_enabled" env:"TMI_ADMIN_SECURITY_RATE_LIMIT_ENABLED"`
	IPAllowlist      []string `yaml:"ip_allowlist" env:"TMI_ADMIN_SECURITY_IP_ALLOWLIST"`
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
				CallbackURL: "http://localhost:8080/oauth2/callback",
				Providers:   getDefaultOAuthProviders(),
			},
		},
		WebSocket: WebSocketConfig{
			InactivityTimeoutSeconds: 300, // 5 minutes default
		},
		Logging: LoggingConfig{
			Level:                       "info",
			IsDev:                       true,
			IsTest:                      false,
			LogDir:                      "logs",
			MaxAgeDays:                  7,
			MaxSizeMB:                   100,
			MaxBackups:                  10,
			AlsoLogToConsole:            true,
			SuppressUnauthenticatedLogs: true,
		},
		Admin: AdminConfig{
			Enabled:    false,
			PathPrefix: "/admin",
			Users: AdminUsersConfig{
				PrimaryEmail:     "",
				AdditionalEmails: []string{},
				AutoPromoteFirst: false,
			},
			Session: AdminSessionConfig{
				TimeoutMinutes:   240,
				ReauthRequired:   true,
				ExtendOnActivity: true,
			},
			Security: AdminSecurityConfig{
				RequireMFA:       false,
				AuditLogging:     true,
				RateLimitEnabled: true,
				IPAllowlist:      []string{},
			},
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
			Icon:             "fa-brands fa-google",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://accounts.google.com/o/oauth2/auth",
			TokenURL:         "https://oauth2.googleapis.com/token",
			UserInfo: []UserInfoEndpoint{
				{
					URL:    "https://www.googleapis.com/oauth2/v3/userinfo",
					Claims: map[string]string{}, // Will use defaults
				},
			},
			Issuer:           "https://accounts.google.com",
			JWKSURL:          "https://www.googleapis.com/oauth2/v3/certs",
			Scopes:           []string{"openid", "profile", "email"},
			AdditionalParams: map[string]string{},
		},
		"github": {
			ID:               "github",
			Name:             "GitHub",
			Enabled:          true,
			Icon:             "fa-brands fa-github",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://github.com/login/oauth/authorize",
			TokenURL:         "https://github.com/login/oauth/access_token",
			UserInfo: []UserInfoEndpoint{
				{
					URL: "https://api.github.com/user",
					Claims: map[string]string{
						"subject_claim": "id",
						"name_claim":    "name",
						"picture_claim": "avatar_url",
					},
				},
				{
					URL: "https://api.github.com/user/emails",
					Claims: map[string]string{
						"email_claim":          "[0].email",
						"email_verified_claim": "[0].verified",
					},
				},
			},
			Scopes:           []string{"user:email"},
			AdditionalParams: map[string]string{},
			AuthHeaderFormat: "token %s",
			AcceptHeader:     "application/json",
		},
		"microsoft": {
			ID:               "microsoft",
			Name:             "Microsoft",
			Enabled:          true,
			Icon:             "fa-brands fa-microsoft",
			ClientID:         "",
			ClientSecret:     "",
			AuthorizationURL: "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			TokenURL:         "https://login.microsoftonline.com/common/oauth2/v2.0/token",
			UserInfo: []UserInfoEndpoint{
				{
					URL: "https://graph.microsoft.com/v1.0/me",
					Claims: map[string]string{
						"subject_claim":        "id",
						"email_claim":          "mail",
						"name_claim":           "displayName",
						"given_name_claim":     "givenName",
						"family_name_claim":    "surname",
						"email_verified_claim": "true", // Literal value
					},
				},
			},
			Issuer:           "https://login.microsoftonline.com/common/v2.0",
			JWKSURL:          "https://login.microsoftonline.com/common/discovery/v2.0/keys",
			Scopes:           []string{"openid", "profile", "email", "User.Read"},
			AdditionalParams: map[string]string{},
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
	case reflect.Slice:
		// Handle string slices (comma-separated values)
		if field.Type().Elem().Kind() == reflect.String {
			parts := strings.Split(value, ",")
			slice := make([]string, 0, len(parts))
			for _, part := range parts {
				trimmed := strings.TrimSpace(part)
				if trimmed != "" {
					slice = append(slice, trimmed)
				}
			}
			field.Set(reflect.ValueOf(slice))
		} else {
			return fmt.Errorf("unsupported slice type: %s", field.Type().Elem().Kind())
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

	// Validate WebSocket configuration
	if c.WebSocket.InactivityTimeoutSeconds < 15 {
		return fmt.Errorf("websocket inactivity timeout must be at least 15 seconds")
	}

	// Validate admin configuration
	if err := c.validateAdminConfig(); err != nil {
		return err
	}

	return nil
}

// validateAdminConfig validates admin-specific configuration
func (c *Config) validateAdminConfig() error {
	// If admin is disabled, no further validation needed
	if !c.Admin.Enabled {
		return nil
	}

	// If admin is enabled, primary email is required
	if c.Admin.Users.PrimaryEmail == "" {
		return fmt.Errorf("admin primary email is required when admin interface is enabled")
	}

	// Validate primary email format
	if !isValidEmail(c.Admin.Users.PrimaryEmail) {
		return fmt.Errorf("invalid primary admin email format: %s", c.Admin.Users.PrimaryEmail)
	}

	// Validate additional emails
	for _, email := range c.Admin.Users.AdditionalEmails {
		if !isValidEmail(email) {
			return fmt.Errorf("invalid additional admin email format: %s", email)
		}
	}

	// Validate session timeout
	if c.Admin.Session.TimeoutMinutes < 5 {
		return fmt.Errorf("admin session timeout must be at least 5 minutes")
	}

	// Validate IP allowlist format
	for _, ip := range c.Admin.Security.IPAllowlist {
		if !isValidIPOrCIDR(ip) {
			return fmt.Errorf("invalid IP/CIDR in admin allowlist: %s", ip)
		}
	}

	return nil
}

// isValidEmail validates email address format
func isValidEmail(email string) bool {
	emailRegex := regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	return emailRegex.MatchString(email)
}

// isValidIPOrCIDR validates IP address or CIDR notation
func isValidIPOrCIDR(ipStr string) bool {
	// Check if it's a valid IP address
	if net.ParseIP(ipStr) != nil {
		return true
	}

	// Check if it's a valid CIDR
	_, _, err := net.ParseCIDR(ipStr)
	return err == nil
}

// IsTestMode returns true if running in test mode
func (c *Config) IsTestMode() bool {
	return c.Logging.IsTest || isRunningInTest()
}

// isRunningInTest detects if we're running under 'go test'
func isRunningInTest() bool {
	return flag.Lookup("test.v") != nil
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

// Admin helper methods

// IsAdminEnabled returns true if admin interface is enabled
func (c *Config) IsAdminEnabled() bool {
	return c.Admin.Enabled
}

// IsUserAdmin checks if the given email is configured as an admin
func (c *Config) IsUserAdmin(email string) bool {
	if email == c.Admin.Users.PrimaryEmail {
		return true
	}

	for _, adminEmail := range c.Admin.Users.AdditionalEmails {
		if email == adminEmail {
			return true
		}
	}

	return false
}

// GetAdminTimeout returns the admin session timeout duration
func (c *Config) GetAdminTimeout() time.Duration {
	return time.Duration(c.Admin.Session.TimeoutMinutes) * time.Minute
}

// GetWebSocketInactivityTimeout returns the websocket inactivity timeout duration
func (c *Config) GetWebSocketInactivityTimeout() time.Duration {
	return time.Duration(c.WebSocket.InactivityTimeoutSeconds) * time.Second
}
