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

	config := Config{
		Postgres: PostgresConfig{
			Host:     envutil.Get("POSTGRES_HOST", "localhost"),
			Port:     envutil.Get("POSTGRES_PORT", "5432"),
			User:     envutil.Get("POSTGRES_USER", "postgres"),
			Password: envutil.Get("POSTGRES_PASSWORD", "postgres"),
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
			Secret:              envutil.Get("AUTH_JWT_SECRET", envutil.Get("JWT_SECRET", "your-secret-key")),
			ExpirationSeconds:   jwtExpiration,
			SigningMethod:       envutil.Get("AUTH_JWT_SIGNING_METHOD", envutil.Get("JWT_SIGNING_METHOD", "HS256")),
			KeyID:               envutil.Get("AUTH_JWT_KEY_ID", envutil.Get("JWT_KEY_ID", "1")),
			RSAPrivateKeyPath:   envutil.Get("AUTH_JWT_RSA_PRIVATE_KEY_PATH", envutil.Get("JWT_RSA_PRIVATE_KEY_PATH", "")),
			RSAPublicKeyPath:    envutil.Get("AUTH_JWT_RSA_PUBLIC_KEY_PATH", envutil.Get("JWT_RSA_PUBLIC_KEY_PATH", "")),
			RSAPrivateKey:       envutil.Get("AUTH_JWT_RSA_PRIVATE_KEY", envutil.Get("JWT_RSA_PRIVATE_KEY", "")),
			RSAPublicKey:        envutil.Get("AUTH_JWT_RSA_PUBLIC_KEY", envutil.Get("JWT_RSA_PUBLIC_KEY", "")),
			ECDSAPrivateKeyPath: envutil.Get("AUTH_JWT_ECDSA_PRIVATE_KEY_PATH", envutil.Get("JWT_ECDSA_PRIVATE_KEY_PATH", "")),
			ECDSAPublicKeyPath:  envutil.Get("AUTH_JWT_ECDSA_PUBLIC_KEY_PATH", envutil.Get("JWT_ECDSA_PUBLIC_KEY_PATH", "")),
			ECDSAPrivateKey:     envutil.Get("AUTH_JWT_ECDSA_PRIVATE_KEY", envutil.Get("JWT_ECDSA_PRIVATE_KEY", "")),
			ECDSAPublicKey:      envutil.Get("AUTH_JWT_ECDSA_PUBLIC_KEY", envutil.Get("JWT_ECDSA_PUBLIC_KEY", "")),
		},
		OAuth: OAuthConfig{
			CallbackURL: envutil.Get("AUTH_OAUTH_CALLBACK_URL", envutil.Get("OAUTH_CALLBACK_URL", "http://localhost:8080/oauth2/callback")),
			Providers:   loadOAuthProviders(),
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
	if envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_ENABLED", envutil.Get("OAUTH_GOOGLE_ENABLED", "true")) == "true" {
		logger.Debug("Configuring Google OAuth provider")
		providers["google"] = OAuthProviderConfig{
			ID:               "google",
			Name:             "Google",
			Enabled:          true,
			Icon:             "fa-brands fa-google",
			ClientID:         envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_ID", envutil.Get("OAUTH_GOOGLE_CLIENT_ID", "")),
			ClientSecret:     envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_CLIENT_SECRET", envutil.Get("OAUTH_GOOGLE_CLIENT_SECRET", "")),
			AuthorizationURL: envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_AUTHORIZATION_URL", envutil.Get("OAUTH_GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/auth")),
			TokenURL:         envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_TOKEN_URL", envutil.Get("OAUTH_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token")),
			UserInfo: []UserInfoEndpoint{
				{
					URL:    envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_USERINFO_URL", envutil.Get("OAUTH_GOOGLE_USERINFO_URL", "https://www.googleapis.com/oauth2/v3/userinfo")),
					Claims: map[string]string{}, // Will use defaults
				},
			},
			Issuer:           envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_ISSUER", envutil.Get("OAUTH_GOOGLE_ISSUER", "https://accounts.google.com")),
			JWKSURL:          envutil.Get("AUTH_OAUTH_PROVIDERS_GOOGLE_JWKS_URL", envutil.Get("OAUTH_GOOGLE_JWKS_URL", "https://www.googleapis.com/oauth2/v3/certs")),
			Scopes:           []string{"openid", "profile", "email"},
			AdditionalParams: map[string]string{},
		}
	}

	// GitHub OAuth configuration
	if envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_ENABLED", envutil.Get("OAUTH_GITHUB_ENABLED", "true")) == "true" {
		logger.Debug("Configuring GitHub OAuth provider")
		providers["github"] = OAuthProviderConfig{
			ID:               "github",
			Name:             "GitHub",
			Enabled:          true,
			Icon:             "fa-brands fa-github",
			ClientID:         envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_ID", envutil.Get("OAUTH_GITHUB_CLIENT_ID", "")),
			ClientSecret:     envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_CLIENT_SECRET", envutil.Get("OAUTH_GITHUB_CLIENT_SECRET", "")),
			AuthorizationURL: envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_AUTHORIZATION_URL", envutil.Get("OAUTH_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize")),
			TokenURL:         envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_TOKEN_URL", envutil.Get("OAUTH_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token")),
			UserInfo: []UserInfoEndpoint{
				{
					URL: envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_USERINFO_URL", envutil.Get("OAUTH_GITHUB_USERINFO_URL", "https://api.github.com/user")),
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
			AuthHeaderFormat: envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_AUTH_HEADER_FORMAT", "token %s"),
			AcceptHeader:     envutil.Get("AUTH_OAUTH_PROVIDERS_GITHUB_ACCEPT_HEADER", "application/json"),
		}
	}

	// Microsoft OAuth configuration
	if envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_ENABLED", envutil.Get("OAUTH_MICROSOFT_ENABLED", "true")) == "true" {
		logger.Debug("Configuring Microsoft OAuth provider")
		providers["microsoft"] = OAuthProviderConfig{
			ID:               "microsoft",
			Name:             "Microsoft",
			Enabled:          true,
			Icon:             "fa-brands fa-microsoft",
			ClientID:         envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_ID", envutil.Get("OAUTH_MICROSOFT_CLIENT_ID", "")),
			ClientSecret:     envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_CLIENT_SECRET", envutil.Get("OAUTH_MICROSOFT_CLIENT_SECRET", "")),
			AuthorizationURL: envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_AUTHORIZATION_URL", envutil.Get("OAUTH_MICROSOFT_AUTH_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize")),
			TokenURL:         envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_TOKEN_URL", envutil.Get("OAUTH_MICROSOFT_TOKEN_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/token")),
			UserInfo: []UserInfoEndpoint{
				{
					URL: envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_USERINFO_URL", envutil.Get("OAUTH_MICROSOFT_USERINFO_URL", "https://graph.microsoft.com/v1.0/me")),
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
			Issuer:           envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_ISSUER", envutil.Get("OAUTH_MICROSOFT_ISSUER", "https://login.microsoftonline.com/common/v2.0")),
			JWKSURL:          envutil.Get("AUTH_OAUTH_PROVIDERS_MICROSOFT_JWKS_URL", envutil.Get("OAUTH_MICROSOFT_JWKS_URL", "https://login.microsoftonline.com/common/discovery/v2.0/keys")),
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
