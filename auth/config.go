package auth

import (
	"fmt"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/envutil"
	"github.com/ericfitz/tmi/internal/slogging"
)

// Config holds all authentication configuration
type Config struct {
	Postgres PostgresConfig
	Redis    RedisConfig
	JWT      JWTConfig
	OAuth    OAuthConfig
	SAML     SAMLConfig
}

// PostgresConfig holds PostgreSQL configuration
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

	// Require explicit database configuration - no fallback to localhost
	postgresHost := envutil.Get("POSTGRES_HOST", "")
	if postgresHost == "" {
		logger.Error("Database configuration missing: POSTGRES_HOST environment variable must be set")
		return Config{}, fmt.Errorf("POSTGRES_HOST environment variable is required")
	}
	logger.Debug("PostgreSQL host configured: %s", postgresHost)

	config := Config{
		Postgres: PostgresConfig{
			Host:     postgresHost,
			Port:     envutil.Get("POSTGRES_PORT", "5432"),
			User:     envutil.Get("POSTGRES_USER", "postgres"),
			Password: envutil.Get("POSTGRES_PASSWORD", ""),
			Database: envutil.Get("POSTGRES_DATABASE", envutil.Get("POSTGRES_DB", "tmi")),
			SSLMode:  envutil.Get("POSTGRES_SSL_MODE", envutil.Get("POSTGRES_SSLMODE", "disable")),
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
			Enabled:   envutil.Get("SAML_ENABLED", "false") == "true",
			Providers: loadSAMLProviders(),
		},
	}

	logger.Info("Authentication configuration loaded successfully postgres_host=%v redis_host=%v jwt_signing_method=%v oauth_providers_count=%v", config.Postgres.Host, config.Redis.Host, config.JWT.SigningMethod, len(config.OAuth.Providers))
	return config, nil
}

// ToDBConfig converts Config to db.PostgresConfig and db.RedisConfig
func (c *Config) ToDBConfig() (db.PostgresConfig, db.RedisConfig) {
	return db.PostgresConfig{
			Host:     c.Postgres.Host,
			Port:     c.Postgres.Port,
			User:     c.Postgres.User,
			Password: c.Postgres.Password,
			Database: c.Postgres.Database,
			SSLMode:  c.Postgres.SSLMode,
		}, db.RedisConfig{
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

// loadOAuthProviders loads OAuth provider configurations
func loadOAuthProviders() map[string]OAuthProviderConfig {
	logger := slogging.Get()
	logger.Debug("Loading OAuth provider configurations")
	providers := make(map[string]OAuthProviderConfig)

	// Google OAuth configuration
	if envutil.Get("OAUTH_PROVIDERS_GOOGLE_ENABLED", "true") == "true" {
		logger.Debug("Configuring Google OAuth provider")
		providers["google"] = OAuthProviderConfig{
			ID:               "google",
			Name:             "Google",
			Enabled:          true,
			Icon:             "fa-brands fa-google",
			ClientID:         envutil.Get("OAUTH_PROVIDERS_GOOGLE_CLIENT_ID", ""),
			ClientSecret:     envutil.Get("OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET", ""),
			AuthorizationURL: envutil.Get("OAUTH_PROVIDERS_GOOGLE_AUTHORIZATION_URL", "https://accounts.google.com/o/oauth2/auth"),
			TokenURL:         envutil.Get("OAUTH_PROVIDERS_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL:    envutil.Get("OAUTH_PROVIDERS_GOOGLE_USERINFO_URL", "https://www.googleapis.com/oauth2/v3/userinfo"),
					Claims: map[string]string{}, // Will use defaults
				},
			},
			Issuer:           envutil.Get("OAUTH_PROVIDERS_GOOGLE_ISSUER", "https://accounts.google.com"),
			JWKSURL:          envutil.Get("OAUTH_PROVIDERS_GOOGLE_JWKS_URL", "https://www.googleapis.com/oauth2/v3/certs"),
			Scopes:           []string{"openid", "profile", "email"},
			AdditionalParams: map[string]string{},
		}
	}

	// GitHub OAuth configuration
	if envutil.Get("OAUTH_PROVIDERS_GITHUB_ENABLED", "true") == "true" {
		logger.Debug("Configuring GitHub OAuth provider")
		providers["github"] = OAuthProviderConfig{
			ID:               "github",
			Name:             "GitHub",
			Enabled:          true,
			Icon:             "fa-brands fa-github",
			ClientID:         envutil.Get("OAUTH_PROVIDERS_GITHUB_CLIENT_ID", ""),
			ClientSecret:     envutil.Get("OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET", ""),
			AuthorizationURL: envutil.Get("OAUTH_PROVIDERS_GITHUB_AUTHORIZATION_URL", "https://github.com/login/oauth/authorize"),
			TokenURL:         envutil.Get("OAUTH_PROVIDERS_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL: envutil.Get("OAUTH_PROVIDERS_GITHUB_USERINFO_URL", "https://api.github.com/user"),
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
		}
	}

	// Microsoft OAuth configuration
	if envutil.Get("OAUTH_PROVIDERS_MICROSOFT_ENABLED", "true") == "true" {
		logger.Debug("Configuring Microsoft OAuth provider")
		providers["microsoft"] = OAuthProviderConfig{
			ID:               "microsoft",
			Name:             "Microsoft",
			Enabled:          true,
			Icon:             "fa-brands fa-microsoft",
			ClientID:         envutil.Get("OAUTH_PROVIDERS_MICROSOFT_CLIENT_ID", ""),
			ClientSecret:     envutil.Get("OAUTH_PROVIDERS_MICROSOFT_CLIENT_SECRET", ""),
			AuthorizationURL: envutil.Get("OAUTH_PROVIDERS_MICROSOFT_AUTHORIZATION_URL", "https://login.microsoftonline.com/consumers/oauth2/v2.0/authorize"),
			TokenURL:         envutil.Get("OAUTH_PROVIDERS_MICROSOFT_TOKEN_URL", "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL: envutil.Get("OAUTH_PROVIDERS_MICROSOFT_USERINFO_URL", "https://graph.microsoft.com/v1.0/me"),
					Claims: map[string]string{
						"subject_claim":        "id",
						"email_claim":          "mail",
						"name_claim":           "displayName",
						"given_name_claim":     "givenName",
						"family_name_claim":    "surname",
						"email_verified_claim": "true", // Literal value
					},
				},
				// Optional: fetch group memberships from Microsoft Graph API
				// Requires "GroupMember.Read.All" or "Directory.Read.All" scope
				// Note: If the provider returns groups in the standard "groups" claim (RFC 9068),
				// this additional endpoint is not needed - groups will be extracted automatically
				{
					URL: envutil.Get("OAUTH_PROVIDERS_MICROSOFT_GROUPS_URL", ""),
					Claims: map[string]string{
						"groups_claim": "value.[*].displayName", // Microsoft Graph API structure
					},
				},
			},
			Issuer:           envutil.Get("OAUTH_PROVIDERS_MICROSOFT_ISSUER", "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/v2.0"),
			JWKSURL:          envutil.Get("OAUTH_PROVIDERS_MICROSOFT_JWKS_URL", "https://login.microsoftonline.com/9188040d-6c67-4c5b-b112-36a304b66dad/discovery/v2.0/keys"),
			Scopes:           []string{"openid", "profile", "email", "User.Read"},
			AdditionalParams: map[string]string{},
		}
	}

	logger.Info("OAuth providers loaded providers_count=%v enabled_providers=%v", len(providers), getEnabledProviderIDs(providers))
	return providers
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

// loadSAMLProviders loads SAML provider configurations from environment
func loadSAMLProviders() map[string]SAMLProviderConfig {
	logger := slogging.Get()
	logger.Info("loadSAMLProviders function called - starting provider discovery")
	logger.Debug("Loading SAML provider configurations")
	providers := make(map[string]SAMLProviderConfig)

	// Dynamically discover SAML providers from environment variables
	// Environment variables follow the pattern: SAML_PROVIDERS_<PROVIDER_ID>_<FIELD>
	// We scan for _ENABLED variables to discover configured providers
	providerIDs := envutil.DiscoverProviders("SAML_PROVIDERS_", "_ENABLED")
	logger.Info("Discovered %d potential SAML provider IDs: %v", len(providerIDs), providerIDs)

	for _, providerID := range providerIDs {
		prefix := fmt.Sprintf("SAML_PROVIDERS_%s_", providerID)

		// Check if provider is enabled
		if envutil.Get(prefix+"ENABLED", "false") != "true" {
			logger.Debug("SAML provider %s is disabled, skipping", providerID)
			continue
		}

		// Convert environment variable provider ID to lowercase for use as provider key
		// e.g., ENTRA_TMIDEV_SAML -> entra-tmidev-saml
		providerKey := envutil.ProviderIDToKey(providerID)

		logger.Debug("Loading SAML provider configuration provider_id=%s provider_key=%s", providerID, providerKey)

		providers[providerKey] = SAMLProviderConfig{
			ID:                envutil.Get(prefix+"ID", providerKey),
			Name:              envutil.Get(prefix+"NAME", providerKey),
			Enabled:           true,
			Icon:              envutil.Get(prefix+"ICON", "fa-solid fa-key"),
			EntityID:          envutil.Get(prefix+"ENTITY_ID", ""),
			ACSURL:            envutil.Get(prefix+"ACS_URL", ""),
			SLOURL:            envutil.Get(prefix+"SLO_URL", ""),
			SPPrivateKey:      envutil.Get(prefix+"SP_PRIVATE_KEY", ""),
			SPPrivateKeyPath:  envutil.Get(prefix+"SP_PRIVATE_KEY_PATH", ""),
			SPCertificate:     envutil.Get(prefix+"SP_CERTIFICATE", ""),
			SPCertificatePath: envutil.Get(prefix+"SP_CERTIFICATE_PATH", ""),
			IDPMetadataURL:    envutil.Get(prefix+"IDP_METADATA_URL", ""),
			IDPMetadataXML:    envutil.Get(prefix+"IDP_METADATA_XML", ""),
			AllowIDPInitiated: envutil.Get(prefix+"ALLOW_IDP_INITIATED", "false") == "true",
			ForceAuthn:        envutil.Get(prefix+"FORCE_AUTHN", "false") == "true",
			SignRequests:      envutil.Get(prefix+"SIGN_REQUESTS", "true") == "true",
			NameIDAttribute:   envutil.Get(prefix+"NAMEID_ATTRIBUTE", ""),
			EmailAttribute:    envutil.Get(prefix+"EMAIL_ATTRIBUTE", "email"),
			NameAttribute:     envutil.Get(prefix+"NAME_ATTRIBUTE", "name"),
			GroupsAttribute:   envutil.Get(prefix+"GROUPS_ATTRIBUTE", "groups"),
		}

		logger.Info("Loaded SAML provider configuration provider_key=%s name=%s", providerKey, providers[providerKey].Name)
	}

	logger.Info("SAML providers loaded providers_count=%v", len(providers))
	return providers
}

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
