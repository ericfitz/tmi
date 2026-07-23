package auth

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/envutil"
	"github.com/ericfitz/tmi/internal/slogging"
)

// JWT signing method constants
const (
	signingMethodHS256 = "HS256"
	signingMethodRS256 = "RS256"
	signingMethodES256 = "ES256"
)

// Config holds all authentication configuration
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: top-level auth configuration aggregating database, Redis, JWT, OAuth, and SAML settings (pure)
type Config struct {
	Database  DatabaseConfig // Database config with URL-based connection string
	Redis     RedisConfig
	JWT       JWTConfig
	OAuth     OAuthConfig
	SAML      SAMLConfig
	BuildMode string // dev, test, or production
}

// DatabaseConfig holds unified database configuration.
// Database type is determined from the URL scheme (postgres://, mysql://, etc.)
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: database connection settings including URL, Oracle wallet path, and connection pool limits (pure)
type DatabaseConfig struct {
	URL                  string // DATABASE_URL - contains all connection parameters
	OracleWalletLocation string // path to Oracle wallet for ADB (cannot be in URL)

	// Connection pool configuration
	MaxOpenConns    int // Maximum open connections (default: 10)
	MaxIdleConns    int // Maximum idle connections (default: 2)
	ConnMaxLifetime int // Max connection lifetime in seconds (default: 240)
	ConnMaxIdleTime int // Max idle time in seconds (default: 30)
}

// RedisConfig holds Redis configuration
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: Redis connection settings including host, port, password, and DB index (pure)
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// JWTConfig holds JWT configuration
// SEM@36538e427d89135597d0d3615fcf217f9f4088e4: JWT signing and expiration settings supporting HS256, RS256, and ES256 algorithms (pure)
type JWTConfig struct {
	Secret              string
	ExpirationSeconds   int
	SigningMethod       string // HS256, RS256, ES256
	KeyID               string // Key ID for JWKS (defaults to "1")
	RefreshTokenDays    int    // Refresh token TTL in days (default: 7)
	SessionLifetimeDays int    // Absolute session lifetime in days (default: 7)
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
// SEM@9bf8890e7d4a04bdbb3f0e80fb295392276e3a5d: OAuth callback URL, provider map, and client-callback allowlist settings (pure)
type OAuthConfig struct {
	CallbackURL string
	Providers   map[string]OAuthProviderConfig
	// ClientCallbackAllowList is the list of allowed client_callback URLs for
	// /oauth2/authorize. Each entry is an exact URL or a wildcard pattern
	// ending in "*". An empty list rejects any client_callback (fail-closed).
	ClientCallbackAllowList []string
}

// UserInfoEndpoint represents a single userinfo endpoint and its claim mappings
// SEM@93e972b21be4fbdf788d2884f25d14b33d41b98e: a single OAuth userinfo endpoint URL with its claim-to-field mappings (pure)
type UserInfoEndpoint struct {
	URL    string            `json:"url"`
	Claims map[string]string `json:"claims"`
}

// OAuthProviderConfig holds configuration for an OAuth provider
// SEM@93e972b21be4fbdf788d2884f25d14b33d41b98e: full configuration for a single OAuth provider including credentials, endpoints, and scopes (pure)
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: SAML enabled flag and map of SAML provider configurations (pure)
type SAMLConfig struct {
	Enabled   bool                          `json:"enabled"`
	Providers map[string]SAMLProviderConfig `json:"providers"`
}

// SAMLProviderConfig holds configuration for a SAML provider
// SEM@7e40aae7f066b1d045faeff914884107bde40f0e: full configuration for a single SAML provider including SP/IDP keys, endpoints, and attribute mappings (pure)
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
	IDPMetadataB64XML string `json:"idp_metadata_b64xml"`
	AllowIDPInitiated bool   `json:"allow_idp_initiated"`
	ForceAuthn        bool   `json:"force_authn"`
	SignRequests      bool   `json:"sign_requests"`
	NameIDAttribute   string `json:"name_id_attribute"`
	EmailAttribute    string `json:"email_attribute"`
	NameAttribute     string `json:"name_attribute"`
	GroupsAttribute   string `json:"groups_attribute"`
}

// LoadConfig loads configuration from environment variables.
// This uses DATABASE_URL as the primary database configuration method.
// SEM@e03fc554584eab95175850a0591c019a25ec0d56: build the auth Config from environment variables; fail if TMI_DATABASE_URL is absent (reads env)
func LoadConfig() (Config, error) {
	logger := slogging.Get()
	logger.Info("TRACE: LoadConfig() function called - START")
	logger.Info("Loading authentication configuration from environment variables")

	redisDB, err := strconv.Atoi(envutil.Get("REDIS_DB", envutil.Get("TMI_REDIS_DB", "0")))
	if err != nil {
		logger.Warn("Invalid REDIS_DB value, using default value=%v default=%v", envutil.Get("REDIS_DB", "0"), 0)
		redisDB = 0
	}

	jwtExpiration, err := strconv.Atoi(envutil.Get("TMI_JWT_EXPIRATION_SECONDS", envutil.Get("JWT_EXPIRATION_SECONDS", "3600")))
	if err != nil {
		logger.Warn("Invalid JWT_EXPIRATION_SECONDS value, using default value=%v default=%v", envutil.Get("JWT_EXPIRATION_SECONDS", "3600"), 3600)
		jwtExpiration = 3600
	}

	// Load database configuration from DATABASE_URL
	databaseURL := envutil.Get("TMI_DATABASE_URL", envutil.Get("DATABASE_URL", ""))
	if databaseURL == "" {
		logger.Error("Database configuration missing: TMI_DATABASE_URL environment variable must be set")
		return Config{}, fmt.Errorf("TMI_DATABASE_URL environment variable is required")
	}
	logger.Debug("Database URL configured (scheme extracted from URL)")

	dbConfig := DatabaseConfig{
		URL:                  databaseURL,
		OracleWalletLocation: envutil.Get("TMI_ORACLE_WALLET_LOCATION", envutil.Get("ORACLE_WALLET_LOCATION", "")),
	}

	config := Config{
		Database: dbConfig,
		Redis: RedisConfig{
			Host:     envutil.Get("TMI_REDIS_HOST", envutil.Get("REDIS_HOST", "localhost")),
			Port:     envutil.Get("TMI_REDIS_PORT", envutil.Get("REDIS_PORT", "6379")),
			Password: envutil.Get("TMI_REDIS_PASSWORD", envutil.Get("REDIS_PASSWORD", "")),
			DB:       redisDB,
		},
		JWT: JWTConfig{
			Secret:              envutil.Get("TMI_JWT_SECRET", envutil.Get("JWT_SECRET", "your-secret-key")),
			ExpirationSeconds:   jwtExpiration,
			SigningMethod:       envutil.Get("TMI_JWT_SIGNING_METHOD", envutil.Get("JWT_SIGNING_METHOD", signingMethodHS256)),
			KeyID:               envutil.Get("TMI_JWT_KEY_ID", envutil.Get("JWT_KEY_ID", "1")),
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
			CallbackURL: envutil.Get("TMI_OAUTH_CALLBACK_URL", envutil.Get("OAUTH_CALLBACK_URL", "http://localhost:8080/oauth2/callback")),
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

	logger.Info("Authentication configuration loaded successfully database_url_set=%v redis_host=%v jwt_signing_method=%v oauth_providers_count=%v", databaseURL != "", config.Redis.Host, config.JWT.SigningMethod, len(config.OAuth.Providers))
	logger.Info("TRACE: LoadConfig() function - END - SAML enabled=%v provider_count=%v", config.SAML.Enabled, len(config.SAML.Providers))
	return config, nil
}

// ToGormConfig converts Config to db.GormConfig for GORM database connections.
// It parses the DATABASE_URL to extract connection parameters.
// SEM@fe6575f1c15d84b67ee9853a0e59055c1ebe44b6: convert the auth database config to a GORM connection config, parsing the database URL (pure)
func (c *Config) ToGormConfig() db.GormConfig {
	logger := slogging.Get()

	// Parse DATABASE_URL to get connection parameters
	gormConfig, err := db.ParseDatabaseURL(c.Database.URL)
	if err != nil {
		logger.Error("Failed to parse DATABASE_URL: %v", err)
		return db.GormConfig{}
	}

	// Copy Oracle wallet location if set (cannot be encoded in URL)
	if c.Database.OracleWalletLocation != "" {
		gormConfig.OracleWalletLocation = c.Database.OracleWalletLocation
	}

	// Copy connection pool configuration
	gormConfig.MaxOpenConns = c.Database.MaxOpenConns
	gormConfig.MaxIdleConns = c.Database.MaxIdleConns
	gormConfig.ConnMaxLifetime = c.Database.ConnMaxLifetime
	gormConfig.ConnMaxIdleTime = c.Database.ConnMaxIdleTime

	return *gormConfig
}

// ToRedisConfig converts Config to db.RedisConfig
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: convert the auth Redis config to a db.RedisConfig for initializing the Redis client (pure)
func (c *Config) ToRedisConfig() db.RedisConfig {
	return db.RedisConfig{
		Host:     c.Redis.Host,
		Port:     c.Redis.Port,
		Password: c.Redis.Password,
		DB:       c.Redis.DB,
	}
}

// GetJWTDuration returns the JWT expiration duration
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: return the JWT expiration as a time.Duration (pure)
func (c *Config) GetJWTDuration() time.Duration {
	return time.Duration(c.JWT.ExpirationSeconds) * time.Second
}

// loadOAuthProviders loads OAuth provider configurations from environment
// SEM@74c3372fd86df9011559420de319ea92b3dec3df: discover and build enabled OAuth provider configs from environment variable prefixes (reads env)
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
//
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: scan environment variables under a prefix and return a lowercase claim-name-to-value map (reads env)
func parseClaimMappings(prefix string) map[string]string {
	return envutil.ScanPrefixedMap(prefix)
}

// parseAdditionalParams parses additional OAuth parameters from environment variables
// For example, with prefix "OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_":
//
//	OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_ACCESS_TYPE=offline
//	OAUTH_PROVIDERS_GOOGLE_ADDITIONAL_PARAMS_PROMPT=consent
//
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: scan environment variables under a prefix and return a lowercase param-name-to-value map (reads env)
func parseAdditionalParams(prefix string) map[string]string {
	return envutil.ScanPrefixedMap(prefix)
}

// getEnabledProviderIDs returns a slice of enabled provider IDs for logging
// SEM@70ff47b7829f38ef04399520210ae8765d39495d: list the IDs of all enabled OAuth providers in the given map (pure)
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
// SEM@e03fc554584eab95175850a0591c019a25ec0d56: validate auth config fields including signing-method key requirements and secret uniqueness; reject invalid configs (pure)
func (c *Config) ValidateConfig() error {
	logger := slogging.Get()
	logger.Debug("Validating authentication configuration")

	// Validate database URL
	if c.Database.URL == "" {
		logger.Error("DATABASE_URL is required but not configured")
		return fmt.Errorf("database url is required (TMI_DATABASE_URL)")
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
	case signingMethodHS256:
		if c.JWT.Secret == "" || c.JWT.Secret == "your-secret-key" {
			logger.Error("JWT secret is required and should not be the default value for HS256 signing_method=%v", c.JWT.SigningMethod)
			return fmt.Errorf("jwt secret is required and should not be the default value for HS256")
		}
	case signingMethodRS256:
		if (c.JWT.RSAPrivateKeyPath == "" && c.JWT.RSAPrivateKey == "") ||
			(c.JWT.RSAPublicKeyPath == "" && c.JWT.RSAPublicKey == "") {
			logger.Error("RSA keys are required for RS256 signing_method=%v has_private_key_path=%v has_public_key_path=%v", c.JWT.SigningMethod, c.JWT.RSAPrivateKeyPath != "", c.JWT.RSAPublicKeyPath != "")
			return fmt.Errorf("rsa private and public keys are required for RS256 (provide either key paths or key content)")
		}
	case signingMethodES256:
		if (c.JWT.ECDSAPrivateKeyPath == "" && c.JWT.ECDSAPrivateKey == "") ||
			(c.JWT.ECDSAPublicKeyPath == "" && c.JWT.ECDSAPublicKey == "") {
			logger.Error("ECDSA keys are required for ES256 signing_method=%v has_private_key_path=%v has_public_key_path=%v", c.JWT.SigningMethod, c.JWT.ECDSAPrivateKeyPath != "", c.JWT.ECDSAPublicKeyPath != "")
			return fmt.Errorf("ecdsa private and public keys are required for ES256 (provide either key paths or key content)")
		}
	default:
		logger.Error("Unsupported JWT signing method signing_method=%v", c.JWT.SigningMethod)
		return fmt.Errorf("unsupported jwt signing method: %s (supported: HS256, RS256, ES256)", c.JWT.SigningMethod)
	}

	// Validate JWT secret is not reused as another secret in the configuration
	if c.JWT.SigningMethod == signingMethodHS256 && c.JWT.Secret != "" {
		if c.Redis.Password != "" && c.JWT.Secret == c.Redis.Password {
			logger.Error("JWT secret must not match Redis password")
			return fmt.Errorf("jwt secret must not be the same as another secret in configuration (redis password)")
		}
		for providerID, provider := range c.OAuth.Providers {
			if provider.ClientSecret != "" && c.JWT.Secret == provider.ClientSecret {
				logger.Error("JWT secret must not match OAuth provider client secret provider=%v", providerID)
				return fmt.Errorf("jwt secret must not be the same as another secret in configuration (oauth provider %s client secret)", providerID)
			}
		}
	}

	// Validate OAuth configuration
	if c.OAuth.CallbackURL == "" {
		logger.Error("OAuth callback URL is required but not configured")
		return fmt.Errorf("oauth callback url is required")
	}
	if len(c.OAuth.Providers) == 0 {
		logger.Warn("No OAuth providers configured - authentication may be limited")
	}

	logger.Info("Authentication configuration validated successfully jwt_signing_method=%v oauth_providers_count=%v", c.JWT.SigningMethod, len(c.OAuth.Providers))
	return nil
}

// GetEnabledProviders returns a slice of enabled OAuth providers
// SEM@0ecd9032832c43f28451afc85a42dd56721c8b3c: list all enabled OAuth provider configs (pure)
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
// SEM@0ecd9032832c43f28451afc85a42dd56721c8b3c: fetch an enabled OAuth provider by ID; return false if absent or disabled (pure)
func (c *Config) GetProvider(providerID string) (OAuthProviderConfig, bool) {
	provider, exists := c.OAuth.Providers[providerID]
	if !exists || !provider.Enabled {
		return OAuthProviderConfig{}, false
	}
	return provider, true
}
