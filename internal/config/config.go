package config

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/envutil"
	"github.com/ericfitz/tmi/internal/slogging"
	"gopkg.in/yaml.v3"
)

// envTrueValue is the string used to represent boolean true in environment variables
const envTrueValue = "true"

// AdministratorConfig represents a single administrator entry configuration
type AdministratorConfig struct {
	Provider    string `yaml:"provider" json:"provider"`                           // OAuth/SAML provider ID (required)
	ProviderId  string `yaml:"provider_id,omitempty" json:"provider_id,omitempty"` // Provider's user ID (for users, preferred)
	Email       string `yaml:"email,omitempty" json:"email,omitempty"`             // Provider's email (for users, fallback)
	GroupName   string `yaml:"group_name,omitempty" json:"group_name,omitempty"`   // Group name (for groups)
	SubjectType string `yaml:"subject_type" json:"subject_type"`                   // "user" or "group" (required)
}

// Config holds all application configuration
type Config struct {
	Server                    ServerConfig          `yaml:"server"`
	Database                  DatabaseConfig        `yaml:"database"`
	Auth                      AuthConfig            `yaml:"auth"`
	WebSocket                 WebSocketConfig       `yaml:"websocket"`
	Webhooks                  WebhookConfig         `yaml:"webhooks"`
	Logging                   LoggingConfig         `yaml:"logging"`
	Operator                  OperatorConfig        `yaml:"operator"`
	Secrets                   SecretsConfig         `yaml:"secrets"`
	Administrators            []AdministratorConfig `yaml:"administrators"`
	Timmy                     TimmyConfig           `yaml:"timmy"`
	SSRF                      SSRFConfig            `yaml:"ssrf"`
	Observability             ObservabilityConfig   `yaml:"observability"`
	ContentSources            ContentSourcesConfig      `yaml:"content_sources"`
	ContentExtractors         ContentExtractorsConfig   `yaml:"content_extractors"`
	ContentOAuth              ContentOAuthConfig        `yaml:"content_oauth"`
	ContentTokenEncryptionKey string                `yaml:"content_token_encryption_key" env:"TMI_CONTENT_TOKEN_ENCRYPTION_KEY"`
}

// ObservabilityConfig holds OpenTelemetry configuration
type ObservabilityConfig struct {
	Enabled        bool    `yaml:"enabled" env:"TMI_OTEL_ENABLED"`
	SamplingRate   float64 `yaml:"sampling_rate" env:"TMI_OTEL_SAMPLING_RATE"`
	PrometheusPort int     `yaml:"prometheus_port" env:"TMI_OTEL_PROMETHEUS_PORT"`
}

// SSRFConfig holds SSRF protection settings for URI validation
type SSRFConfig struct {
	IssueURI      SSRFURIConfig `yaml:"issue_uri"`
	DocumentURI   SSRFURIConfig `yaml:"document_uri"`
	RepositoryURI SSRFURIConfig `yaml:"repository_uri"`
	Timmy         SSRFURIConfig `yaml:"timmy"`
}

// SSRFURIConfig holds allowlist and scheme configuration for a single URI type
type SSRFURIConfig struct {
	Allowlist string `yaml:"allowlist"`
	Schemes   string `yaml:"schemes"`
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port                string        `yaml:"port" env:"TMI_SERVER_PORT"`
	Interface           string        `yaml:"interface" env:"TMI_SERVER_INTERFACE"`
	BaseURL             string        `yaml:"base_url" env:"TMI_SERVER_BASE_URL"` // Public base URL for callbacks (auto-inferred if empty)
	ReadTimeout         time.Duration `yaml:"read_timeout" env:"TMI_SERVER_READ_TIMEOUT"`
	WriteTimeout        time.Duration `yaml:"write_timeout" env:"TMI_SERVER_WRITE_TIMEOUT"`
	IdleTimeout         time.Duration `yaml:"idle_timeout" env:"TMI_SERVER_IDLE_TIMEOUT"`
	TLSEnabled          bool          `yaml:"tls_enabled" env:"TMI_SERVER_TLS_ENABLED"`
	TLSCertFile         string        `yaml:"tls_cert_file" env:"TMI_SERVER_TLS_CERT_FILE"`
	TLSKeyFile          string        `yaml:"tls_key_file" env:"TMI_SERVER_TLS_KEY_FILE"`
	TLSSubjectName      string        `yaml:"tls_subject_name" env:"TMI_SERVER_TLS_SUBJECT_NAME"`
	HTTPToHTTPSRedirect bool          `yaml:"http_to_https_redirect" env:"TMI_SERVER_HTTP_TO_HTTPS_REDIRECT"`
	TrustedProxies      []string      `yaml:"trusted_proxies" env:"TMI_TRUSTED_PROXIES"`             // Comma-separated CIDRs/IPs for X-Forwarded-For validation
	RateLimitPublicRPM  int           `yaml:"ratelimit_public_rpm" env:"TMI_RATELIMIT_PUBLIC_RPM"`   // Requests/min per IP for public endpoints (default: 10)
	DisableRateLimiting bool          `yaml:"disable_rate_limiting" env:"TMI_DISABLE_RATE_LIMITING"` // Disable all rate limiting (dev/test only)
	CORS                CORSConfig    `yaml:"cors"`
}

// CORSConfig holds CORS configuration
type CORSConfig struct {
	AllowedOrigins []string `yaml:"allowed_origins" env:"TMI_CORS_ALLOWED_ORIGINS"` // Comma-separated allowed origins
}

// DatabaseConfig holds database configuration.
// The primary configuration method is DATABASE_URL which contains all connection parameters.
// Database type is automatically detected from the URL scheme (postgres://, mysql://, etc.)
type DatabaseConfig struct {
	URL                  string               `yaml:"url" env:"TMI_DATABASE_URL"`                              // Connection string URL (12-factor app pattern) - REQUIRED
	OracleWalletLocation string               `yaml:"oracle_wallet_location" env:"TMI_ORACLE_WALLET_LOCATION"` // Path to Oracle wallet directory (Oracle ADB only)
	ConnectionPool       ConnectionPoolConfig `yaml:"connection_pool"`
	Redis                RedisConfig          `yaml:"redis"`
}

// ConnectionPoolConfig holds database connection pool settings
type ConnectionPoolConfig struct {
	MaxOpenConns    int `yaml:"max_open_conns" env:"TMI_DB_MAX_OPEN_CONNS"`         // Maximum open connections (default: 10)
	MaxIdleConns    int `yaml:"max_idle_conns" env:"TMI_DB_MAX_IDLE_CONNS"`         // Maximum idle connections (default: 2)
	ConnMaxLifetime int `yaml:"conn_max_lifetime" env:"TMI_DB_CONN_MAX_LIFETIME"`   // Max connection lifetime in seconds (default: 240)
	ConnMaxIdleTime int `yaml:"conn_max_idle_time" env:"TMI_DB_CONN_MAX_IDLE_TIME"` // Max idle time in seconds (default: 30)
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	URL      string `yaml:"url" env:"TMI_REDIS_URL"` // Connection string URL (redis://[:password@]host:port[/db]), takes precedence over individual fields
	Host     string `yaml:"host" env:"TMI_REDIS_HOST"`
	Port     string `yaml:"port" env:"TMI_REDIS_PORT"`
	Password string `yaml:"password" env:"TMI_REDIS_PASSWORD"` //nolint:gosec // G117 - Redis connection password
	DB       int    `yaml:"db" env:"TMI_REDIS_DB"`
}

// AuthConfig holds authentication configuration
type AuthConfig struct {
	JWT                  JWTConfig    `yaml:"jwt"`
	OAuth                OAuthConfig  `yaml:"oauth"`
	SAML                 SAMLConfig   `yaml:"saml"`
	Cookie               CookieConfig `yaml:"cookie"`
	AutoPromoteFirstUser bool         `yaml:"auto_promote_first_user" env:"TMI_AUTH_AUTO_PROMOTE_FIRST_USER"`
	EveryoneIsAReviewer  bool         `yaml:"everyone_is_a_reviewer" env:"TMI_AUTH_EVERYONE_IS_A_REVIEWER"`
	BuildMode            string       `yaml:"build_mode" env:"TMI_BUILD_MODE"` // dev, test, or production
}

// CookieConfig holds HttpOnly session cookie configuration
type CookieConfig struct {
	Enabled bool   `yaml:"enabled" env:"TMI_COOKIE_ENABLED"` // Enable HttpOnly cookie-based auth (default: true)
	Domain  string `yaml:"domain" env:"TMI_COOKIE_DOMAIN"`   // Cookie domain (auto-inferred from BaseURL if empty)
	Secure  bool   `yaml:"secure" env:"TMI_COOKIE_SECURE"`   // Require HTTPS (auto-derived from TLSEnabled if not set)
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret              string `yaml:"secret" env:"TMI_JWT_SECRET"` //nolint:gosec // G117 - JWT signing secret
	ExpirationSeconds   int    `yaml:"expiration_seconds" env:"TMI_JWT_EXPIRATION_SECONDS"`
	SigningMethod       string `yaml:"signing_method" env:"TMI_JWT_SIGNING_METHOD"`
	RefreshTokenDays    int    `yaml:"refresh_token_days" env:"TMI_REFRESH_TOKEN_DAYS"`       // Refresh token TTL in days (default: 7)
	SessionLifetimeDays int    `yaml:"session_lifetime_days" env:"TMI_SESSION_LIFETIME_DAYS"` // Absolute session lifetime in days (default: 7)
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	CallbackURL string                         `yaml:"callback_url" env:"TMI_OAUTH_CALLBACK_URL"`
	Providers   map[string]OAuthProviderConfig `yaml:"providers"`
}

// UserInfoEndpoint represents a single userinfo endpoint and its claim mappings
type UserInfoEndpoint struct {
	URL    string            `yaml:"url" json:"url"`
	Claims map[string]string `yaml:"claims" json:"claims,omitempty"`
}

// OAuthProviderConfig holds configuration for an OAuth provider
type OAuthProviderConfig struct {
	ID               string             `yaml:"id"`
	Name             string             `yaml:"name"`
	Enabled          bool               `yaml:"enabled"`
	Icon             string             `yaml:"icon"`
	ClientID         string             `yaml:"client_id"`
	ClientSecret     string             `yaml:"client_secret"` //nolint:gosec // G117 - OAuth provider client secret
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

// SAMLConfig holds SAML configuration
type SAMLConfig struct {
	Enabled   bool                          `yaml:"enabled" env:"TMI_SAML_ENABLED"`
	Providers map[string]SAMLProviderConfig `yaml:"providers"`
}

// SAMLProviderConfig holds configuration for a SAML provider
type SAMLProviderConfig struct {
	ID                string `yaml:"id"`
	Name              string `yaml:"name"`
	Enabled           bool   `yaml:"enabled"`
	Icon              string `yaml:"icon"`
	EntityID          string `yaml:"entity_id" env:"TMI_SAML_ENTITY_ID"`
	MetadataURL       string `yaml:"metadata_url" env:"TMI_SAML_METADATA_URL"`
	MetadataXML       string `yaml:"metadata_xml" env:"TMI_SAML_METADATA_XML"`
	ACSURL            string `yaml:"acs_url" env:"TMI_SAML_ACS_URL"`
	SLOURL            string `yaml:"slo_url" env:"TMI_SAML_SLO_URL"`
	SPPrivateKey      string `yaml:"sp_private_key" env:"TMI_SAML_SP_PRIVATE_KEY"`
	SPPrivateKeyPath  string `yaml:"sp_private_key_path" env:"TMI_SAML_SP_PRIVATE_KEY_PATH"`
	SPCertificate     string `yaml:"sp_certificate" env:"TMI_SAML_SP_CERTIFICATE"`
	SPCertificatePath string `yaml:"sp_certificate_path" env:"TMI_SAML_SP_CERTIFICATE_PATH"`
	IDPMetadataURL    string `yaml:"idp_metadata_url" env:"TMI_SAML_IDP_METADATA_URL"`
	IDPMetadataB64XML string `yaml:"idp_metadata_b64xml" env:"TMI_SAML_IDP_METADATA_B64XML"` // Base64-encoded IdP metadata XML
	AllowIDPInitiated bool   `yaml:"allow_idp_initiated" env:"TMI_SAML_ALLOW_IDP_INITIATED"`
	ForceAuthn        bool   `yaml:"force_authn" env:"TMI_SAML_FORCE_AUTHN"`
	SignRequests      bool   `yaml:"sign_requests" env:"TMI_SAML_SIGN_REQUESTS"`
	NameIDAttribute   string `yaml:"name_id_attribute" env:"TMI_SAML_NAME_ID_ATTRIBUTE"`
	EmailAttribute    string `yaml:"email_attribute" env:"TMI_SAML_EMAIL_ATTRIBUTE"`
	NameAttribute     string `yaml:"name_attribute" env:"TMI_SAML_NAME_ATTRIBUTE"`
	GroupsAttribute   string `yaml:"groups_attribute" env:"TMI_SAML_GROUPS_ATTRIBUTE"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level            string `yaml:"level" env:"TMI_LOG_LEVEL"`
	IsDev            bool   `yaml:"is_dev" env:"TMI_LOG_IS_DEV"`
	IsTest           bool   `yaml:"is_test" env:"TMI_LOG_IS_TEST"`
	LogDir           string `yaml:"log_dir" env:"TMI_LOG_DIR"`
	MaxAgeDays       int    `yaml:"max_age_days" env:"TMI_LOG_MAX_AGE_DAYS"`
	MaxSizeMB        int    `yaml:"max_size_mb" env:"TMI_LOG_MAX_SIZE_MB"`
	MaxBackups       int    `yaml:"max_backups" env:"TMI_LOG_MAX_BACKUPS"`
	AlsoLogToConsole bool   `yaml:"also_log_to_console" env:"TMI_LOG_ALSO_LOG_TO_CONSOLE"`
	// Enhanced debug logging options
	LogAPIRequests              bool `yaml:"log_api_requests" env:"TMI_LOG_API_REQUESTS"`
	LogAPIResponses             bool `yaml:"log_api_responses" env:"TMI_LOG_API_RESPONSES"`
	LogWebSocketMsg             bool `yaml:"log_websocket_messages" env:"TMI_LOG_WEBSOCKET_MESSAGES"`
	RedactAuthTokens            bool `yaml:"redact_auth_tokens" env:"TMI_LOG_REDACT_AUTH_TOKENS"`
	SuppressUnauthenticatedLogs bool `yaml:"suppress_unauthenticated_logs" env:"TMI_LOG_SUPPRESS_UNAUTH_LOGS"`
}

// WebSocketConfig holds WebSocket timeout configuration
type WebSocketConfig struct {
	InactivityTimeoutSeconds int `yaml:"inactivity_timeout_seconds" env:"TMI_WEBSOCKET_INACTIVITY_TIMEOUT_SECONDS"`
}

// WebhookConfig holds webhook configuration
type WebhookConfig struct {
	AllowHTTPTargets bool `yaml:"allow_http_targets" env:"TMI_WEBHOOK_ALLOW_HTTP_TARGETS"` // Allow non-HTTPS webhook URLs (e.g., for intra-cluster communication)
}

// OperatorConfig holds operator/maintainer information
type OperatorConfig struct {
	Name    string `yaml:"name" env:"TMI_OPERATOR_NAME"`
	Contact string `yaml:"contact" env:"TMI_OPERATOR_CONTACT"`
}

// SecretsConfig holds configuration for external secret providers
type SecretsConfig struct {
	Provider string `yaml:"provider" env:"TMI_SECRETS_PROVIDER"` // "env" (default), "vault", "aws", "azure", "gcp", "oci"

	// HashiCorp Vault (design only - implementation deferred)
	VaultAddress string `yaml:"vault_address" env:"TMI_VAULT_ADDRESS"`
	VaultToken   string `yaml:"vault_token" env:"TMI_VAULT_TOKEN"`
	VaultPath    string `yaml:"vault_path" env:"TMI_VAULT_PATH"`

	// AWS Secrets Manager
	AWSRegion     string `yaml:"aws_region" env:"TMI_AWS_REGION"`
	AWSSecretName string `yaml:"aws_secret_name" env:"TMI_AWS_SECRET_NAME"`

	// Azure Key Vault (design only - implementation deferred)
	AzureVaultURL string `yaml:"azure_vault_url" env:"TMI_AZURE_VAULT_URL"`

	// GCP Secret Manager (design only - implementation deferred)
	GCPProjectID  string `yaml:"gcp_project_id" env:"TMI_GCP_PROJECT_ID"`
	GCPSecretName string `yaml:"gcp_secret_name" env:"TMI_GCP_SECRET_NAME"`

	// OCI Secrets Management Service
	OCICompartmentID string `yaml:"oci_compartment_id" env:"TMI_OCI_COMPARTMENT_ID"`
	OCIVaultID       string `yaml:"oci_vault_id" env:"TMI_OCI_VAULT_ID"`
	OCISecretName    string `yaml:"oci_secret_name" env:"TMI_OCI_SECRET_NAME"`
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

	// Override with environment variables (includes deprecated alias support)
	if err := overrideWithEnv(config); err != nil {
		return nil, fmt.Errorf("failed to override with environment variables: %w", err)
	}

	// Heroku compatibility: Use PORT env var if TMI_SERVER_PORT is not set
	// Heroku dynamically assigns a port via PORT and apps must bind to it
	if port := os.Getenv("PORT"); port != "" && os.Getenv("TMI_SERVER_PORT") == "" {
		config.Server.Port = port
	}

	// Load single administrator from environment variables (Heroku-friendly)
	if provider := os.Getenv("TMI_ADMIN_PROVIDER"); provider != "" {
		adminConfig := AdministratorConfig{
			Provider:    provider,
			SubjectType: envutil.Get("TMI_ADMIN_SUBJECT_TYPE", "user"),
		}

		if providerID := os.Getenv("TMI_ADMIN_PROVIDER_ID"); providerID != "" {
			adminConfig.ProviderId = providerID
		}

		if email := os.Getenv("TMI_ADMIN_EMAIL"); email != "" {
			adminConfig.Email = email
		}

		if groupName := os.Getenv("TMI_ADMIN_GROUP_NAME"); groupName != "" {
			adminConfig.GroupName = groupName
		}

		// Add to administrators list (append to any YAML-configured admins)
		config.Administrators = append(config.Administrators, adminConfig)
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	// Warn if CORS allowed origins is empty in production mode
	if !config.Logging.IsDev && len(config.Server.CORS.AllowedOrigins) == 0 {
		logger := slogging.Get()
		logger.Warn("CORS allowed_origins is empty in production mode; cross-origin requests will be blocked. Set TMI_CORS_ALLOWED_ORIGINS to allow client origins.")
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
			URL:                  "", // DATABASE_URL is required - no default
			OracleWalletLocation: "",
			ConnectionPool: ConnectionPoolConfig{
				MaxOpenConns:    10,
				MaxIdleConns:    2,
				ConnMaxLifetime: 240, // seconds
				ConnMaxIdleTime: 30,  // seconds
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
				Secret:              "",
				ExpirationSeconds:   3600,
				SigningMethod:       "HS256",
				RefreshTokenDays:    7,
				SessionLifetimeDays: 7,
			},
			OAuth: OAuthConfig{
				CallbackURL: "http://localhost:8080/oauth2/callback",
				Providers:   make(map[string]OAuthProviderConfig),
			},
			Cookie: CookieConfig{
				Enabled: true,
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
		Operator: OperatorConfig{
			Name:    "",
			Contact: "",
		},
		Secrets: SecretsConfig{
			Provider: "env", // Default to environment variables
		},
		Timmy:             DefaultTimmyConfig(),
		ContentExtractors: DefaultContentExtractorsConfig(),
		ContentOAuth: ContentOAuthConfig{
			Providers: make(map[string]ContentOAuthProviderConfig),
		},
		Observability: ObservabilityConfig{
			Enabled:        false,
			SamplingRate:   1.0,
			PrometheusPort: 0,
		},
	}
}

// loadFromYAML loads configuration from a YAML file
func loadFromYAML(config *Config, filename string) error {
	data, err := os.ReadFile(filename) // #nosec G304 -- filename comes from CLI flag set by server operator, not untrusted input
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

		// Handle maps for provider configurations
		if field.Kind() == reflect.Map && fieldType.Name == "Providers" {
			// Determine if this is OAuth or SAML providers based on parent struct
			parentType := v.Type().Name()
			switch parentType {
			case "OAuthConfig":
				if err := overrideOAuthProviders(field); err != nil {
					return err
				}
			case "SAMLConfig":
				if err := overrideSAMLProviders(field); err != nil {
					return err
				}
			case "ContentOAuthConfig":
				if err := overrideContentOAuthProviders(field); err != nil {
					return err
				}
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
	logger := slogging.Get()
	logger.Info("[CONFIG] overrideOAuthProviders called - starting dynamic OAuth provider discovery")

	if mapField.IsNil() {
		logger.Info("[CONFIG] OAuth providers map is nil, initializing it")
		mapField.Set(reflect.MakeMap(mapField.Type()))
	}

	// Discover OAuth providers from environment variables
	providerIDs := envutil.DiscoverProviders("OAUTH_PROVIDERS_", "_ENABLED")
	logger.Info("[CONFIG] Discovered %d OAuth provider IDs: %v", len(providerIDs), providerIDs)

	for _, providerID := range providerIDs {
		envPrefix := fmt.Sprintf("OAUTH_PROVIDERS_%s_", providerID)

		// Check if this provider is enabled
		enabledStr := os.Getenv(envPrefix + "ENABLED")
		if enabledStr != envTrueValue {
			logger.Info("[CONFIG] OAuth provider %s is not enabled (ENABLED=%s), skipping", providerID, enabledStr)
			continue
		}

		// Convert provider ID to key (e.g., "GOOGLE" -> "google", "GITHUB" -> "github")
		providerKey := envutil.ProviderIDToKey(providerID)
		logger.Info("[CONFIG] Processing OAuth provider: %s (key: %s)", providerID, providerKey)

		// Parse scopes (comma-separated)
		scopesStr := os.Getenv(envPrefix + "SCOPES")
		var scopes []string
		if scopesStr != "" {
			scopes = strings.Split(scopesStr, ",")
			for i := range scopes {
				scopes[i] = strings.TrimSpace(scopes[i])
			}
		}

		// Build userinfo endpoints array
		var userInfoEndpoints []UserInfoEndpoint
		if userinfoURL := os.Getenv(envPrefix + "USERINFO_URL"); userinfoURL != "" {
			userInfoEndpoints = append(userInfoEndpoints, UserInfoEndpoint{
				URL: userinfoURL,
			})
		}

		// Create new OAuth provider config
		provider := OAuthProviderConfig{
			ID:               providerKey,
			Name:             os.Getenv(envPrefix + "NAME"),
			Enabled:          true,
			Icon:             os.Getenv(envPrefix + "ICON"),
			ClientID:         os.Getenv(envPrefix + "CLIENT_ID"),
			ClientSecret:     os.Getenv(envPrefix + "CLIENT_SECRET"),
			AuthorizationURL: os.Getenv(envPrefix + "AUTHORIZATION_URL"),
			TokenURL:         os.Getenv(envPrefix + "TOKEN_URL"),
			Issuer:           os.Getenv(envPrefix + "ISSUER"),
			JWKSURL:          os.Getenv(envPrefix + "JWKS_URL"),
			Scopes:           scopes,
			UserInfo:         userInfoEndpoints,
		}

		// Use key as default name if not set
		if provider.Name == "" {
			provider.Name = providerKey
		}

		logger.Info("[CONFIG] Adding OAuth provider %s to map (ID: %s, Name: %s, ClientID set: %v)",
			providerKey, provider.ID, provider.Name, provider.ClientID != "")

		// Set the provider in the map
		mapField.SetMapIndex(reflect.ValueOf(providerKey), reflect.ValueOf(provider))
	}

	logger.Info("[CONFIG] OAuth provider discovery complete, %d providers in map", mapField.Len())
	return nil
}

// overrideSAMLProviders handles environment variable overrides for SAML providers
func overrideSAMLProviders(mapField reflect.Value) error {
	logger := slogging.Get()
	logger.Info("[CONFIG] overrideSAMLProviders called - starting dynamic SAML provider discovery")

	if mapField.IsNil() {
		logger.Info("[CONFIG] SAML providers map is nil, initializing it")
		mapField.Set(reflect.MakeMap(mapField.Type()))
	}

	// Discover SAML providers from environment variables
	providerIDs := envutil.DiscoverProviders("SAML_PROVIDERS_", "_ENABLED")
	logger.Info("[CONFIG] Discovered %d SAML provider IDs: %v", len(providerIDs), providerIDs)

	for _, providerID := range providerIDs {
		envPrefix := fmt.Sprintf("SAML_PROVIDERS_%s_", providerID)

		// Check if this provider is enabled
		enabledStr := os.Getenv(envPrefix + "ENABLED")
		if enabledStr != envTrueValue {
			logger.Info("[CONFIG] SAML provider %s is not enabled (ENABLED=%s), skipping", providerID, enabledStr)
			continue
		}

		// Convert provider ID to key (e.g., "ENTRA_TMIDEV_SAML" -> "entra-tmidev-saml")
		providerKey := envutil.ProviderIDToKey(providerID)
		logger.Info("[CONFIG] Processing SAML provider: %s (key: %s)", providerID, providerKey)

		// Read attribute mapping environment variables
		nameAttr := os.Getenv(envPrefix + "NAME_ATTRIBUTE")
		emailAttr := os.Getenv(envPrefix + "EMAIL_ATTRIBUTE")
		groupsAttr := os.Getenv(envPrefix + "GROUPS_ATTRIBUTE")

		// DEBUG: Log attribute environment variable values
		logger.Info("[CONFIG] SAML provider %s attribute mappings - NAME_ATTRIBUTE=%q, EMAIL_ATTRIBUTE=%q, GROUPS_ATTRIBUTE=%q",
			providerID, nameAttr, emailAttr, groupsAttr)

		// Create new SAML provider config
		provider := SAMLProviderConfig{
			ID:                os.Getenv(envPrefix + "ID"),
			Name:              os.Getenv(envPrefix + "NAME"),
			Enabled:           true,
			Icon:              os.Getenv(envPrefix + "ICON"),
			EntityID:          os.Getenv(envPrefix + "ENTITY_ID"),
			MetadataURL:       os.Getenv(envPrefix + "METADATA_URL"),
			MetadataXML:       os.Getenv(envPrefix + "METADATA_XML"),
			ACSURL:            os.Getenv(envPrefix + "ACS_URL"),
			SLOURL:            os.Getenv(envPrefix + "SLO_URL"),
			SPPrivateKey:      os.Getenv(envPrefix + "SP_PRIVATE_KEY"),
			SPPrivateKeyPath:  os.Getenv(envPrefix + "SP_PRIVATE_KEY_PATH"),
			SPCertificate:     os.Getenv(envPrefix + "SP_CERTIFICATE"),
			SPCertificatePath: os.Getenv(envPrefix + "SP_CERTIFICATE_PATH"),
			IDPMetadataURL:    os.Getenv(envPrefix + "IDP_METADATA_URL"),
			IDPMetadataB64XML: os.Getenv(envPrefix + "IDP_METADATA_B64XML"),
			NameIDAttribute:   os.Getenv(envPrefix + "NAME_ID_ATTRIBUTE"),
			EmailAttribute:    emailAttr,
			NameAttribute:     nameAttr,
			GroupsAttribute:   groupsAttr,
		}

		// Parse boolean fields
		if val := os.Getenv(envPrefix + "ALLOW_IDP_INITIATED"); val != "" {
			provider.AllowIDPInitiated = val == envTrueValue
		}
		if val := os.Getenv(envPrefix + "FORCE_AUTHN"); val != "" {
			provider.ForceAuthn = val == envTrueValue
		}
		if val := os.Getenv(envPrefix + "SIGN_REQUESTS"); val != "" {
			provider.SignRequests = val == envTrueValue
		}

		// Use ID as default if not set
		if provider.ID == "" {
			provider.ID = providerKey
		}
		if provider.Name == "" {
			provider.Name = providerKey
		}

		logger.Info("[CONFIG] Adding SAML provider %s to map (ID: %s, Name: %s, EntityID: %s, ACSURL: %s)",
			providerKey, provider.ID, provider.Name, provider.EntityID, provider.ACSURL)

		// Set the provider in the map
		mapField.SetMapIndex(reflect.ValueOf(providerKey), reflect.ValueOf(provider))
	}

	logger.Info("[CONFIG] SAML provider discovery complete, %d providers in map", mapField.Len())
	return nil
}

// overrideContentOAuthProviders handles environment variable overrides for content OAuth providers.
// It discovers providers via TMI_CONTENT_OAUTH_PROVIDERS_<ID>_ENABLED and populates
// a map[string]ContentOAuthProviderConfig.
func overrideContentOAuthProviders(mapField reflect.Value) error {
	logger := slogging.Get()
	logger.Info("[CONFIG] overrideContentOAuthProviders called - starting dynamic content OAuth provider discovery")

	if mapField.IsNil() {
		logger.Info("[CONFIG] Content OAuth providers map is nil, initializing it")
		mapField.Set(reflect.MakeMap(mapField.Type()))
	}

	// Discover content OAuth providers from environment variables.
	// Environment variables follow the pattern: TMI_CONTENT_OAUTH_PROVIDERS_<ID>_ENABLED
	providerIDs := envutil.DiscoverProviders("TMI_CONTENT_OAUTH_PROVIDERS_", "_ENABLED")
	logger.Info("[CONFIG] Discovered %d content OAuth provider IDs: %v", len(providerIDs), providerIDs)

	for _, providerID := range providerIDs {
		envPrefix := fmt.Sprintf("TMI_CONTENT_OAUTH_PROVIDERS_%s_", providerID)

		// Only process enabled providers.
		enabledStr := os.Getenv(envPrefix + "ENABLED")
		if enabledStr != envTrueValue {
			logger.Info("[CONFIG] Content OAuth provider %s is not enabled (ENABLED=%s), skipping", providerID, enabledStr)
			continue
		}

		// Convert provider ID to key (e.g., "GOOGLE_DRIVE" -> "google-drive").
		providerKey := envutil.ProviderIDToKey(providerID)
		logger.Info("[CONFIG] Processing content OAuth provider: %s (key: %s)", providerID, providerKey)

		// Parse required_scopes (space-separated).
		var requiredScopes []string
		if scopesStr := os.Getenv(envPrefix + "REQUIRED_SCOPES"); scopesStr != "" {
			requiredScopes = append(requiredScopes, strings.Fields(scopesStr)...)
		}

		provider := ContentOAuthProviderConfig{
			Enabled:        true,
			ClientID:       os.Getenv(envPrefix + "CLIENT_ID"),
			ClientSecret:   os.Getenv(envPrefix + "CLIENT_SECRET"),
			AuthURL:        os.Getenv(envPrefix + "AUTH_URL"),
			TokenURL:       os.Getenv(envPrefix + "TOKEN_URL"),
			UserinfoURL:    os.Getenv(envPrefix + "USERINFO_URL"),
			RevocationURL:  os.Getenv(envPrefix + "REVOCATION_URL"),
			RequiredScopes: requiredScopes,
		}

		logger.Info("[CONFIG] Adding content OAuth provider %s to map (ClientID set: %v)",
			providerKey, provider.ClientID != "")

		mapField.SetMapIndex(reflect.ValueOf(providerKey), reflect.ValueOf(provider))
	}

	logger.Info("[CONFIG] Content OAuth provider discovery complete, %d providers in map", mapField.Len())
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
		if field.Type() == reflect.TypeFor[time.Duration]() {
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
	case reflect.Float64:
		floatVal, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("invalid float64 value: %s", value)
		}
		field.SetFloat(floatVal)
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
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.validateDatabase(); err != nil {
		return err
	}
	if err := c.validateAuth(); err != nil {
		return err
	}
	if err := c.validateWebSocket(); err != nil {
		return err
	}
	if err := c.validateAdministrators(); err != nil {
		return err
	}
	if err := c.validateCORS(); err != nil {
		return err
	}
	if err := c.ContentOAuth.Validate(c.ContentTokenEncryptionKey); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateServer() error {
	if c.Server.Port == "" {
		return fmt.Errorf("server port is required")
	}
	return nil
}

func (c *Config) validateDatabase() error {
	// DATABASE_URL is required (contains all connection parameters including type, host, port, user, password, database)
	if c.Database.URL == "" {
		return fmt.Errorf("database url is required (TMI_DATABASE_URL)")
	}

	// Redis is always required
	// Allow Redis URL as alternative to host/port
	if c.Database.Redis.URL == "" && c.Database.Redis.Host == "" {
		return fmt.Errorf("redis configuration is required (TMI_REDIS_URL or TMI_REDIS_HOST)")
	}
	if c.Database.Redis.URL == "" && c.Database.Redis.Port == "" {
		return fmt.Errorf("redis port is required when not using TMI_REDIS_URL")
	}
	return nil
}

func (c *Config) validateAuth() error {
	if err := c.validateJWT(); err != nil {
		return err
	}
	if err := c.validateOAuth(); err != nil {
		return err
	}
	return nil
}

func (c *Config) validateJWT() error {
	if c.Auth.JWT.Secret == "" {
		return fmt.Errorf("jwt secret is required")
	}
	if c.Auth.JWT.ExpirationSeconds <= 0 {
		return fmt.Errorf("jwt expiration must be greater than 0")
	}
	return nil
}

func (c *Config) validateOAuth() error {
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

func (c *Config) validateWebSocket() error {
	if c.WebSocket.InactivityTimeoutSeconds < 15 {
		return fmt.Errorf("websocket inactivity timeout must be at least 15 seconds")
	}
	return nil
}

func (c *Config) validateAdministrators() error {
	for i, admin := range c.Administrators {
		if err := c.validateAdministrator(i, admin); err != nil {
			return err
		}
	}
	return nil
}

func (c *Config) validateAdministrator(index int, admin AdministratorConfig) error {
	if admin.Provider == "" {
		return fmt.Errorf("administrator[%d]: provider is required", index)
	}

	if admin.SubjectType != "user" && admin.SubjectType != "group" {
		return fmt.Errorf("administrator[%d]: subject_type must be 'user' or 'group'", index)
	}

	if err := c.validateAdministratorSubject(index, admin); err != nil {
		return err
	}

	if err := c.validateAdministratorProvider(index, admin); err != nil {
		return err
	}

	return nil
}

func (c *Config) validateAdministratorSubject(index int, admin AdministratorConfig) error {
	switch admin.SubjectType {
	case "user":
		// For users, require either provider_id or email
		if admin.ProviderId == "" && admin.Email == "" {
			return fmt.Errorf("administrator[%d]: user-type admin must have either provider_id or email", index)
		}
	case "group":
		// For groups, require group_name
		if admin.GroupName == "" {
			return fmt.Errorf("administrator[%d]: group-type admin must have group_name", index)
		}
	}
	return nil
}

func (c *Config) validateAdministratorProvider(index int, admin AdministratorConfig) error {
	// Verify provider exists in configured OAuth/SAML providers
	if c.isProviderConfigured(admin.Provider) {
		return nil
	}
	return fmt.Errorf("administrator[%d]: provider '%s' is not configured or not enabled", index, admin.Provider)
}

func (c *Config) validateCORS() error {
	for _, origin := range c.Server.CORS.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("CORS allowed_origins must not contain wildcard '*' (incompatible with credentials)")
		}
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("CORS allowed_origins contains invalid origin: %q (must include scheme and host)", origin)
		}
	}
	return nil
}

func (c *Config) isProviderConfigured(providerID string) bool {
	// Check OAuth providers
	for id, provider := range c.Auth.OAuth.Providers {
		if id == providerID && provider.Enabled {
			return true
		}
	}
	// Check SAML providers
	if c.Auth.SAML.Enabled {
		for id, provider := range c.Auth.SAML.Providers {
			if id == providerID && provider.Enabled {
				return true
			}
		}
	}
	return false
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
func (c *Config) GetLogLevel() slogging.LogLevel {
	return slogging.ParseLogLevel(c.Logging.Level)
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

// GetWebSocketInactivityTimeout returns the websocket inactivity timeout duration
func (c *Config) GetWebSocketInactivityTimeout() time.Duration {
	return time.Duration(c.WebSocket.InactivityTimeoutSeconds) * time.Second
}

// GetBaseURL returns the server's public base URL for callbacks.
// If BaseURL is explicitly configured, it is returned as-is.
// Otherwise, the URL is auto-inferred from Interface, Port, and TLSEnabled.
func (c *Config) GetBaseURL() string {
	if c.Server.BaseURL != "" {
		return c.Server.BaseURL
	}

	// Auto-infer from server configuration
	scheme := "http"
	if c.Server.TLSEnabled {
		scheme = "https"
	}

	host := c.Server.Interface
	if host == "" {
		host = "localhost"
	}

	port := c.Server.Port
	if port == "" {
		port = "8080"
	}

	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// IsSecureCookies returns whether cookies should have the Secure flag set.
// Returns true if explicitly configured or if TLS is enabled.
func (c *Config) IsSecureCookies() bool {
	return c.Auth.Cookie.Secure || c.Server.TLSEnabled
}

// GetCookieDomain returns the cookie domain. If not explicitly configured,
// it extracts the hostname from GetBaseURL().
func (c *Config) GetCookieDomain() string {
	if c.Auth.Cookie.Domain != "" {
		return c.Auth.Cookie.Domain
	}

	parsed, err := url.Parse(c.GetBaseURL())
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
