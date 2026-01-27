package auth

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/envutil"
	"github.com/ericfitz/tmi/internal/slogging"
)

// Config holds all authentication configuration
type Config struct {
	Database  DatabaseConfig // New unified database config
	Postgres  PostgresConfig // Legacy - kept for backward compatibility
	Redis     RedisConfig
	JWT       JWTConfig
	OAuth     OAuthConfig
	SAML      SAMLConfig
	BuildMode string // dev, test, or production
}

// DatabaseConfig holds unified database configuration for both PostgreSQL and Oracle
type DatabaseConfig struct {
	Type string // "postgres" or "oracle"

	// PostgreSQL configuration
	PostgresHost     string
	PostgresPort     string
	PostgresUser     string
	PostgresPassword string
	PostgresDatabase string
	PostgresSSLMode  string

	// Oracle configuration
	OracleUser           string
	OraclePassword       string
	OracleConnectString  string // format: host:port/service or tnsnames alias
	OracleWalletLocation string // path to Oracle wallet for ADB

	// Connection pool configuration
	MaxOpenConns    int // Maximum open connections (default: 10)
	MaxIdleConns    int // Maximum idle connections (default: 2)
	ConnMaxLifetime int // Max connection lifetime in seconds (default: 240)
	ConnMaxIdleTime int // Max idle time in seconds (default: 30)
}

// PostgresConfig holds PostgreSQL configuration (legacy - for backward compatibility)
type PostgresConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
	SSLMode  string
}

// RedisConfig holds Redis configuration
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// JWTConfig holds JWT configuration
type JWTConfig struct {
	Secret            string // Used for HS256
	ExpirationSeconds int
	SigningMethod     string // HS256, RS256, ES256
	KeyID             string // Key ID for JWKS (defaults to "1")
	// RSA Keys (for RS256)
	RSAPrivateKeyPath string // Path to RSA private key file
	RSAPublicKeyPath  string // Path to RSA public key file
	RSAPrivateKey     string // RSA private key as string (alternative to file path)
	RSAPublicKey      string // RSA public key as string (alternative to file path)
	// ECDSA Keys (for ES256)
	ECDSAPrivateKeyPath string // Path to ECDSA private key file
	ECDSAPublicKeyPath  string // Path to ECDSA public key file
	ECDSAPrivateKey     string // ECDSA private key as string (alternative to file path)
	ECDSAPublicKey      string // ECDSA public key as string (alternative to file path)
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	CallbackURL string
	Providers   map[string]OAuthProviderConfig
}

// UserInfoEndpoint represents a single userinfo endpoint and its claim mappings
type UserInfoEndpoint struct {
	URL    string            `json:"url"`
	Claims map[string]string `json:"claims"`
}

// OAuthProviderConfig holds configuration for an OAuth provider
type OAuthProviderConfig struct {
	ID               string             `json:"id"`
	Name             string             `json:"name"`
	Enabled          bool               `json:"enabled"`
	Icon             string             `json:"icon"`
	ClientID         string             `json:"client_id"`
	ClientSecret     string             `json:"client_secret"`
	AuthorizationURL string             `json:"authorization_url"`
	TokenURL         string             `json:"token_url"`
	UserInfo         []UserInfoEndpoint `json:"userinfo"`
	Issuer           string             `json:"issuer"`
	JWKSURL          string             `json:"jwks_url"`
	Scopes           []string           `json:"scopes"`
	AdditionalParams map[string]string  `json:"additional_params"`
	AuthHeaderFormat string             `json:"auth_header_format,omitempty"` // Default: "Bearer %s"
	AcceptHeader     string             `json:"accept_header,omitempty"`      // Default: "application/json"
}

// SAMLConfig holds SAML configuration
type SAMLConfig struct {
	Enabled   bool                          `json:"enabled"`
	Providers map[string]SAMLProviderConfig `json:"providers"`
}

// SAMLProviderConfig holds configuration for a SAML provider
type SAMLProviderConfig struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	Icon              string `json:"icon"`
	EntityID          string `json:"entity_id"`
	MetadataURL       string `json:"metadata_url"`
	MetadataXML       string `json:"metadata_xml"`
	ACSURL            string `json:"acs_url"`
	SLOURL            string `json:"slo_url"`
	SPPrivateKey      string `json:"sp_private_key"`
	SPPrivateKeyPath  string `json:"sp_private_key_path"`
	SPCertificate     string `json:"sp_certificate"`
	SPCertificatePath string `json:"sp_certificate_path"`
	IDPMetadataURL    string `json:"idp_metadata_url"`
	IDPMetadataXML    string `json:"idp_metadata_xml"`
	AllowIDPInitiated bool   `json:"allow_idp_initiated"`
	ForceAuthn        bool   `json:"force_authn"`
	SignRequests      bool   `json:"sign_requests"`
	NameIDAttribute   string `json:"name_id_attribute"`
	EmailAttribute    string `json:"email_attribute"`
	NameAttribute     string `json:"name_attribute"`
	GroupsAttribute   string `json:"groups_attribute"`
}

// LoadConfig loads configuration from environment variables
func LoadConfig() (Config, error) {
	logger := slogging.Get()
	logger.Info("TRACE: LoadConfig() function called - START")
	logger.Info("Loading authentication configuration from environment variables")

	redisDB, err := strconv.Atoi(envutil.Get("REDIS_DB", "0"))
	if err != nil {
		logger.Warn("Invalid REDIS_DB value, using default value=%v default=%v", envutil.Get("REDIS_DB", "0"), 0)
		redisDB = 0
	}

	jwtExpiration, err := strconv.Atoi(envutil.Get("AUTH_JWT_EXPIRATION_SECONDS", envutil.Get("JWT_EXPIRATION_SECONDS", "3600")))
	if err != nil {
		logger.Warn("Invalid JWT_EXPIRATION_SECONDS value, using default value=%v default=%v", envutil.Get("JWT_EXPIRATION_SECONDS", "3600"), 3600)
		jwtExpiration = 3600
	}

	// Database type selection (defaults to postgres for backward compatibility)
	databaseType := envutil.Get("DATABASE_TYPE", "postgres")
	logger.Debug("Database type configured: %s", databaseType)

	// Load database configuration based on type
	var dbConfig DatabaseConfig
	dbConfig.Type = databaseType

	switch databaseType {
	case "oracle":
		// Oracle configuration
		dbConfig.OracleUser = envutil.Get("ORACLE_USER", "")
		dbConfig.OraclePassword = envutil.Get("ORACLE_PASSWORD", "")
		dbConfig.OracleConnectString = envutil.Get("ORACLE_CONNECT_STRING", "")
		dbConfig.OracleWalletLocation = envutil.Get("ORACLE_WALLET_LOCATION", "")

		if dbConfig.OracleConnectString == "" {
			logger.Error("Database configuration missing: ORACLE_CONNECT_STRING environment variable must be set")
			return Config{}, fmt.Errorf("ORACLE_CONNECT_STRING environment variable is required for Oracle database")
		}
		logger.Debug("Oracle connection string configured: %s", dbConfig.OracleConnectString)

	case "postgres":
		fallthrough
	default:
		// PostgreSQL configuration (default)
		postgresHost := envutil.Get("POSTGRES_HOST", "")
		if postgresHost == "" {
			logger.Error("Database configuration missing: POSTGRES_HOST environment variable must be set")
			return Config{}, fmt.Errorf("POSTGRES_HOST environment variable is required")
		}
		logger.Debug("PostgreSQL host configured: %s", postgresHost)

		dbConfig.PostgresHost = postgresHost
		dbConfig.PostgresPort = envutil.Get("POSTGRES_PORT", "5432")
		dbConfig.PostgresUser = envutil.Get("POSTGRES_USER", "postgres")
		dbConfig.PostgresPassword = envutil.Get("POSTGRES_PASSWORD", "")
		dbConfig.PostgresDatabase = envutil.Get("POSTGRES_DATABASE", envutil.Get("POSTGRES_DB", "tmi"))
		dbConfig.PostgresSSLMode = envutil.Get("POSTGRES_SSL_MODE", envutil.Get("POSTGRES_SSLMODE", "disable"))
	}

	config := Config{
		Database: dbConfig,
		// Legacy Postgres config - populated from Database config for backward compatibility
		Postgres: PostgresConfig{
			Host:     dbConfig.PostgresHost,
			Port:     dbConfig.PostgresPort,
			User:     dbConfig.PostgresUser,
			Password: dbConfig.PostgresPassword,
			Database: dbConfig.PostgresDatabase,
			SSLMode:  dbConfig.PostgresSSLMode,
		},
		Redis: RedisConfig{
			Host:     envutil.Get("REDIS_HOST", "localhost"),
			Port:     envutil.Get("REDIS_PORT", "6379"),
			Password: envutil.Get("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		JWT: JWTConfig{
			Secret:              envutil.Get("JWT_SECRET", "your-secret-key"),
			ExpirationSeconds:   jwtExpiration,
			SigningMethod:       envutil.Get("JWT_SIGNING_METHOD", "HS256"),
			KeyID:               envutil.Get("JWT_KEY_ID", "1"),
			RSAPrivateKeyPath:   envutil.Get("JWT_RSA_PRIVATE_KEY_PATH", ""),
			RSAPublicKeyPath:    envutil.Get("JWT_RSA_PUBLIC_KEY_PATH", ""),
			RSAPrivateKey:       envutil.Get("JWT_RSA_PRIVATE_KEY", ""),
			RSAPublicKey:        envutil.Get("JWT_RSA_PUBLIC_KEY", ""),
			ECDSAPrivateKeyPath: envutil.Get("JWT_ECDSA_PRIVATE_KEY_PATH", ""),
			ECDSAPublicKeyPath:  envutil.Get("JWT_ECDSA_PUBLIC_KEY_PATH", ""),
			ECDSAPrivateKey:     envutil.Get("JWT_ECDSA_PRIVATE_KEY", ""),
			ECDSAPublicKey:      envutil.Get("JWT_ECDSA_PUBLIC_KEY", ""),
		},
		OAuth: OAuthConfig{
			CallbackURL: envutil.Get("OAUTH_CALLBACK_URL", "http://localhost:8080/oauth2/callback"),
			Providers:   loadOAuthProviders(),
		},
		SAML: SAMLConfig{
			Enabled: envutil.Get("SAML_ENABLED", "false") == "true",
			// NOTE: SAML provider configuration is loaded via the unified config system
			// in internal/config/config.go, not here. This field will be populated
			// by auth/config_adapter.go:convertSAMLProviders() when using InitAuthWithConfig().
			// If using the deprecated InitAuth() function, providers will not be loaded.
			Providers: make(map[string]SAMLProviderConfig),
		},
	}

	logger.Info("Authentication configuration loaded successfully postgres_host=%v redis_host=%v jwt_signing_method=%v oauth_providers_count=%v", config.Postgres.Host, config.Redis.Host, config.JWT.SigningMethod, len(config.OAuth.Providers))
	logger.Info("TRACE: LoadConfig() function - END - SAML enabled=%v provider_count=%v", config.SAML.Enabled, len(config.SAML.Providers))
	return config, nil
}

// ToGormConfig converts Config to db.GormConfig for GORM database connections
func (c *Config) ToGormConfig() db.GormConfig {
	return db.GormConfig{
		Type: db.DatabaseType(c.Database.Type),

		// PostgreSQL configuration
		PostgresHost:     c.Database.PostgresHost,
		PostgresPort:     c.Database.PostgresPort,
		PostgresUser:     c.Database.PostgresUser,
		PostgresPassword: c.Database.PostgresPassword,
		PostgresDatabase: c.Database.PostgresDatabase,
		PostgresSSLMode:  c.Database.PostgresSSLMode,

		// Oracle configuration
		OracleUser:           c.Database.OracleUser,
		OraclePassword:       c.Database.OraclePassword,
		OracleConnectString:  c.Database.OracleConnectString,
		OracleWalletLocation: c.Database.OracleWalletLocation,

		// Connection pool configuration
		MaxOpenConns:    c.Database.MaxOpenConns,
		MaxIdleConns:    c.Database.MaxIdleConns,
		ConnMaxLifetime: c.Database.ConnMaxLifetime,
		ConnMaxIdleTime: c.Database.ConnMaxIdleTime,
	}
}

// ToRedisConfig converts Config to db.RedisConfig
func (c *Config) ToRedisConfig() db.RedisConfig {
	return db.RedisConfig{
		Host:     c.Redis.Host,
		Port:     c.Redis.Port,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	}
}

// GetJWTDuration returns the JWT expiration duration
func (c *Config) GetJWTDuration() time.Duration {
	return time.Duration(c.JWT.ExpirationSeconds) * time.Second
}

// loadOAuthProviders loads OAuth provider configurations from environment
func loadOAuthProviders() map[string]OAuthProviderConfig {
	logger := slogging.Get()
	logger.Info("loadOAuthProviders function called - starting provider discovery")
	logger.Debug("Loading OAuth provider configurations")
	providers := make(map[string]OAuthProviderConfig)

	// Dynamically discover OAuth providers from environment variables
	// Environment variables follow the pattern: OAUTH_PROVIDERS_<PROVIDER_ID>_<FIELD>
	// We scan for _ENABLED variables to discover configured providers
	providerIDs := envutil.DiscoverProviders("OAUTH_PROVIDERS_", "_ENABLED")
	logger.Info("Discovered %d potential OAuth provider IDs: %v", len(providerIDs), providerIDs)

	for _, providerID := range providerIDs {
		prefix := fmt.Sprintf("OAUTH_PROVIDERS_%s_", providerID)

		// Check if provider is enabled
		if envutil.Get(prefix+"ENABLED", "false") != "true" {
			logger.Debug("OAuth provider %s is disabled, skipping", providerID)
			continue
		}

		// Convert environment variable provider ID to lowercase for use as provider key
		// e.g., GOOGLE -> google, GITHUB -> github, MICROSOFT -> microsoft
		providerKey := envutil.ProviderIDToKey(providerID)

		logger.Debug("Loading OAuth provider configuration provider_id=%s provider_key=%s", providerID, providerKey)

		// Build userinfo endpoints array
		var userInfoEndpoints []UserInfoEndpoint

		// Primary userinfo endpoint (required)
		primaryURL := envutil.Get(prefix+"USERINFO_URL", "")
		if primaryURL != "" {
			endpoint := UserInfoEndpoint{
				URL:    primaryURL,
				Claims: parseClaimMappings(prefix + "USERINFO_CLAIMS_"),
			}
			userInfoEndpoints = append(userInfoEndpoints, endpoint)
		}

		// Secondary userinfo endpoint (optional, for providers like GitHub that need multiple endpoints)
		secondaryURL := envutil.Get(prefix+"USERINFO_SECONDARY_URL", "")
		if secondaryURL != "" {
			endpoint := UserInfoEndpoint{
				URL:    secondaryURL,
				Claims: parseClaimMappings(prefix + "USERINFO_SECONDARY_CLAIMS_"),
			}
			userInfoEndpoints = append(userInfoEndpoints, endpoint)
		}

		// Additional userinfo endpoint (optional, for providers like Microsoft groups)
		additionalURL := envutil.Get(prefix+"USERINFO_ADDITIONAL_URL", "")
		if additionalURL != "" {
			endpoint := UserInfoEndpoint{
				URL:    additionalURL,
				Claims: parseClaimMappings(prefix + "USERINFO_ADDITIONAL_CLAIMS_"),
			}
			userInfoEndpoints = append(userInfoEndpoints, endpoint)
		}

		// Parse scopes (comma-separated)
		scopesStr := envutil.Get(prefix+"SCOPES", "")
		var scopes []string
		if scopesStr != "" {
			scopes = strings.Split(scopesStr, ",")
			// Trim whitespace from each scope
			for i := range scopes {
				scopes[i] = strings.TrimSpace(scopes[i])
			}
		}

		providers[providerKey] = OAuthProviderConfig{
			ID:               envutil.Get(prefix+"ID", providerKey),
			Name:             envutil.Get(prefix+"NAME", providerKey),
			Enabled:          true,
			Icon:             envutil.Get(prefix+"ICON", ""),
			ClientID:         envutil.Get(prefix+"CLIENT_ID", ""),
			ClientSecret:     envutil.Get(prefix+"CLIENT_SECRET", ""),
			AuthorizationURL: envutil.Get(prefix+"AUTHORIZATION_URL", ""),
			TokenURL:         envutil.Get(prefix+"TOKEN_URL", ""),
			UserInfo:         userInfoEndpoints,
			Issuer:           envutil.Get(prefix+"ISSUER", ""),
			JWKSURL:          envutil.Get(prefix+"JWKS_URL", ""),
			Scopes:           scopes,
			AdditionalParams: parseAdditionalParams(prefix + "ADDITIONAL_PARAMS_"),
			AuthHeaderFormat: envutil.Get(prefix+"AUTH_HEADER_FORMAT", ""),
			AcceptHeader:     envutil.Get(prefix+"ACCEPT_HEADER", ""),
		}

		logger.Info("Loaded OAuth provider configuration provider_key=%s name=%s", providerKey, providers[providerKey].Name)
	}

	logger.Info("OAuth providers loaded providers_count=%v enabled_providers=%v", len(providers), getEnabledProviderIDs(providers))
	return providers
}

// parseClaimMappings parses claim mappings from environment variables with a given prefix
// For example, with prefix "OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_":
//
//	OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_SUBJECT_CLAIM=sub
//	OAUTH_PROVIDERS_GOOGLE_USERINFO_CLAIMS_EMAIL_CLAIM=email
func parseClaimMappings(prefix string) map[string]string {
	claims := make(map[string]string)

	// Scan all environment variables for claim mappings
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		if strings.HasPrefix(key, prefix) {
			// Extract claim name by removing prefix and converting to lowercase
			claimName := strings.TrimPrefix(key, prefix)
			claimName = strings.ToLower(claimName)
			claims[claimName] = value
		}
	}

	return claims
}

// parseAdditionalParams parses additional OAuth parameters from environment variables
// For example, with prefix "OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_":
//
//	OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_ACCESS_TYPE=offline
//	OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_PROMPT=consent
func parseAdditionalParams(prefix string) map[string]string {
	params := make(map[string]string)

	// Scan all environment variables for additional params
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := parts[0]
		value := parts[1]

		if strings.HasPrefix(key, prefix) {
			// Extract param name by removing prefix and converting to lowercase
			paramName := strings.TrimPrefix(key, prefix)
			paramName = strings.ToLower(paramName)
			params[paramName] = value
		}
	}

	return params
}

// getEnabledProviderIDs returns a slice of enabled provider IDs for logging
func getEnabledProviderIDs(providers map[string]OAuthProviderConfig) []string {
	var enabled []string
	for id, provider := range providers {
		if provider.Enabled {
			enabled = append(enabled, id)
		}
	}
	return enabled
}

// NOTE: SAML provider configuration loading has been moved to the unified config system.
// See internal/config/config.go:overrideSAMLProviders() for the actual implementation.
// The conversion from unified config to auth config happens in auth/config_adapter.go:convertSAMLProviders().
//
// DEPRECATED: loadSAMLProviders() has been removed. Use InitAuthWithConfig() instead of InitAuth()
// to ensure SAML providers are loaded correctly from environment variables.

// ValidateConfig validates the configuration
func (c *Config) ValidateConfig() error {
	logger := slogging.Get()
	logger.Debug("Validating authentication configuration")

	// Validate PostgreSQL configuration
	if c.Postgres.Host == "" {
		logger.Error("PostgreSQL host is required but not configured")
		return fmt.Errorf("postgres host is required")
	}
	if c.Postgres.Port == "" {
		logger.Error("PostgreSQL port is required but not configured")
		return fmt.Errorf("postgres port is required")
	}
	if c.Postgres.User == "" {
		logger.Error("PostgreSQL user is required but not configured")
		return fmt.Errorf("postgres user is required")
	}
	if c.Postgres.Database == "" {
		logger.Error("PostgreSQL database is required but not configured")
		return fmt.Errorf("postgres database is required")
	}

	// Validate Redis configuration
	if c.Redis.Host == "" {
		logger.Error("Redis host is required but not configured")
		return fmt.Errorf("redis host is required")
	}
	if c.Redis.Port == "" {
		logger.Error("Redis port is required but not configured")
		return fmt.Errorf("redis port is required")
	}

	// Validate JWT configuration
	if c.JWT.ExpirationSeconds <= 0 {
		logger.Error("JWT expiration must be greater than 0 expiration_seconds=%v", c.JWT.ExpirationSeconds)
		return fmt.Errorf("jwt expiration must be greater than 0")
	}

	// Validate signing method and required keys
	switch c.JWT.SigningMethod {
	case "HS256":
		if c.JWT.Secret == "" || c.JWT.Secret == "your-secret-key" {
			logger.Error("JWT secret is required and should not be the default value for HS256 signing_method=%v", c.JWT.SigningMethod)
			return fmt.Errorf("jwt secret is required and should not be the default value for HS256")
		}
	case "RS256":
		if (c.JWT.RSAPrivateKeyPath == "" && c.JWT.RSAPrivateKey == "") ||
			(c.JWT.RSAPublicKeyPath == "" && c.JWT.RSAPublicKey == "") {
			logger.Error("RSA keys are required for RS256 signing_method=%v has_private_key_path=%v has_public_key_path=%v", c.JWT.SigningMethod, c.JWT.RSAPrivateKeyPath != "", c.JWT.RSAPublicKeyPath != "")
			return fmt.Errorf("rsa private and public keys are required for RS256 (provide either key paths or key content)")
		}
	case "ES256":
		if (c.JWT.ECDSAPrivateKeyPath == "" && c.JWT.ECDSAPrivateKey == "") ||
			(c.JWT.ECDSAPublicKeyPath == "" && c.JWT.ECDSAPublicKey == "") {
			logger.Error("ECDSA keys are required for ES256 signing_method=%v has_private_key_path=%v has_public_key_path=%v", c.JWT.SigningMethod, c.JWT.ECDSAPrivateKeyPath != "", c.JWT.ECDSAPublicKeyPath != "")
			return fmt.Errorf("ecdsa private and public keys are required for ES256 (provide either key paths or key content)")
		}
	default:
		logger.Error("Unsupported JWT signing method signing_method=%v", c.JWT.SigningMethod)
		return fmt.Errorf("unsupported jwt signing method: %s (supported: HS256, RS256, ES256)", c.JWT.SigningMethod)
	}

	// Validate OAuth configuration
	if c.OAuth.CallbackURL == "" {
		logger.Error("OAuth callback URL is required but not configured")
		return fmt.Errorf("oauth callback url is required")
	}
	if len(c.OAuth.Providers) == 0 {
		logger.Error("At least one OAuth provider is required but none configured")
		return fmt.Errorf("at least one oauth provider is required")
	}

	logger.Info("Authentication configuration validated successfully jwt_signing_method=%v oauth_providers_count=%v", c.JWT.SigningMethod, len(c.OAuth.Providers))
	return nil
}

// GetEnabledProviders returns a slice of enabled OAuth providers
func (c *Config) GetEnabledProviders() []OAuthProviderConfig {
	var enabled []OAuthProviderConfig
	for _, provider := range c.OAuth.Providers {
		if provider.Enabled {
			enabled = append(enabled, provider)
		}
	}
	return enabled
}

// GetProvider returns a specific OAuth provider configuration
func (c *Config) GetProvider(providerID string) (OAuthProviderConfig, bool) {
	provider, exists := c.OAuth.Providers[providerID]
	if !exists || !provider.Enabled {
		return OAuthProviderConfig{}, false
	}
	return provider, true
}
