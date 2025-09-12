package auth

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/ericfitz/tmi/auth/db"
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
	redisDB, err := strconv.Atoi(getEnv("REDIS_DB", "0"))
	if err != nil {
		redisDB = 0
	}

	jwtExpiration, err := strconv.Atoi(getEnv("JWT_EXPIRATION_SECONDS", "3600"))
	if err != nil {
		jwtExpiration = 3600
	}

	return Config{
		Postgres: PostgresConfig{
			Host:     getEnv("POSTGRES_HOST", "localhost"),
			Port:     getEnv("POSTGRES_PORT", "5432"),
			User:     getEnv("POSTGRES_USER", "postgres"),
			Password: getEnv("POSTGRES_PASSWORD", "postgres"),
			Database: getEnv("POSTGRES_DB", "tmi"),
			SSLMode:  getEnv("POSTGRES_SSLMODE", "disable"),
		},
		Redis: RedisConfig{
			Host:     getEnv("REDIS_HOST", "localhost"),
			Port:     getEnv("REDIS_PORT", "6379"),
			Password: getEnv("REDIS_PASSWORD", ""),
			DB:       redisDB,
		},
		JWT: JWTConfig{
			Secret:              getEnv("JWT_SECRET", "your-secret-key"),
			ExpirationSeconds:   jwtExpiration,
			SigningMethod:       getEnv("JWT_SIGNING_METHOD", "HS256"),
			RSAPrivateKeyPath:   getEnv("JWT_RSA_PRIVATE_KEY_PATH", ""),
			RSAPublicKeyPath:    getEnv("JWT_RSA_PUBLIC_KEY_PATH", ""),
			RSAPrivateKey:       getEnv("JWT_RSA_PRIVATE_KEY", ""),
			RSAPublicKey:        getEnv("JWT_RSA_PUBLIC_KEY", ""),
			ECDSAPrivateKeyPath: getEnv("JWT_ECDSA_PRIVATE_KEY_PATH", ""),
			ECDSAPublicKeyPath:  getEnv("JWT_ECDSA_PUBLIC_KEY_PATH", ""),
			ECDSAPrivateKey:     getEnv("JWT_ECDSA_PRIVATE_KEY", ""),
			ECDSAPublicKey:      getEnv("JWT_ECDSA_PUBLIC_KEY", ""),
		},
		OAuth: OAuthConfig{
			CallbackURL: getEnv("OAUTH_CALLBACK_URL", "http://localhost:8080/oauth2/callback"),
			Providers:   loadOAuthProviders(),
		},
	}, nil
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
	providers := make(map[string]OAuthProviderConfig)

	// Google OAuth configuration
	if getEnv("OAUTH_GOOGLE_ENABLED", "true") == "true" {
		providers["google"] = OAuthProviderConfig{
			ID:               "google",
			Name:             "Google",
			Enabled:          true,
			Icon:             "fa-brands fa-google",
			ClientID:         getEnv("OAUTH_GOOGLE_CLIENT_ID", ""),
			ClientSecret:     getEnv("OAUTH_GOOGLE_CLIENT_SECRET", ""),
			AuthorizationURL: getEnv("OAUTH_GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/auth"),
			TokenURL:         getEnv("OAUTH_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL:    getEnv("OAUTH_GOOGLE_USERINFO_URL", "https://www.googleapis.com/oauth2/v3/userinfo"),
					Claims: map[string]string{}, // Will use defaults
				},
			},
			Issuer:           getEnv("OAUTH_GOOGLE_ISSUER", "https://accounts.google.com"),
			JWKSURL:          getEnv("OAUTH_GOOGLE_JWKS_URL", "https://www.googleapis.com/oauth2/v3/certs"),
			Scopes:           []string{"openid", "profile", "email"},
			AdditionalParams: map[string]string{},
		}
	}

	// GitHub OAuth configuration
	if getEnv("OAUTH_GITHUB_ENABLED", "true") == "true" {
		providers["github"] = OAuthProviderConfig{
			ID:               "github",
			Name:             "GitHub",
			Enabled:          true,
			Icon:             "fa-brands fa-github",
			ClientID:         getEnv("OAUTH_GITHUB_CLIENT_ID", ""),
			ClientSecret:     getEnv("OAUTH_GITHUB_CLIENT_SECRET", ""),
			AuthorizationURL: getEnv("OAUTH_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize"),
			TokenURL:         getEnv("OAUTH_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL: getEnv("OAUTH_GITHUB_USERINFO_URL", "https://api.github.com/user"),
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
	if getEnv("OAUTH_MICROSOFT_ENABLED", "true") == "true" {
		providers["microsoft"] = OAuthProviderConfig{
			ID:               "microsoft",
			Name:             "Microsoft",
			Enabled:          true,
			Icon:             "fa-brands fa-microsoft",
			ClientID:         getEnv("OAUTH_MICROSOFT_CLIENT_ID", ""),
			ClientSecret:     getEnv("OAUTH_MICROSOFT_CLIENT_SECRET", ""),
			AuthorizationURL: getEnv("OAUTH_MICROSOFT_AUTH_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"),
			TokenURL:         getEnv("OAUTH_MICROSOFT_TOKEN_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/token"),
			UserInfo: []UserInfoEndpoint{
				{
					URL: getEnv("OAUTH_MICROSOFT_USERINFO_URL", "https://graph.microsoft.com/v1.0/me"),
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
			Issuer:           getEnv("OAUTH_MICROSOFT_ISSUER", "https://login.microsoftonline.com/common/v2.0"),
			JWKSURL:          getEnv("OAUTH_MICROSOFT_JWKS_URL", "https://login.microsoftonline.com/common/discovery/v2.0/keys"),
			Scopes:           []string{"openid", "profile", "email", "User.Read"},
			AdditionalParams: map[string]string{},
		}
	}

	return providers
}

// Helper function to get environment variables with fallback
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// ValidateConfig validates the configuration
func (c *Config) ValidateConfig() error {
	// Validate PostgreSQL configuration
	if c.Postgres.Host == "" {
		return fmt.Errorf("postgres host is required")
	}
	if c.Postgres.Port == "" {
		return fmt.Errorf("postgres port is required")
	}
	if c.Postgres.User == "" {
		return fmt.Errorf("postgres user is required")
	}
	if c.Postgres.Database == "" {
		return fmt.Errorf("postgres database is required")
	}

	// Validate Redis configuration
	if c.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}
	if c.Redis.Port == "" {
		return fmt.Errorf("redis port is required")
	}

	// Validate JWT configuration
	if c.JWT.ExpirationSeconds <= 0 {
		return fmt.Errorf("jwt expiration must be greater than 0")
	}

	// Validate signing method and required keys
	switch c.JWT.SigningMethod {
	case "HS256":
		if c.JWT.Secret == "" || c.JWT.Secret == "your-secret-key" {
			return fmt.Errorf("jwt secret is required and should not be the default value for HS256")
		}
	case "RS256":
		if (c.JWT.RSAPrivateKeyPath == "" && c.JWT.RSAPrivateKey == "") ||
			(c.JWT.RSAPublicKeyPath == "" && c.JWT.RSAPublicKey == "") {
			return fmt.Errorf("rsa private and public keys are required for RS256 (provide either key paths or key content)")
		}
	case "ES256":
		if (c.JWT.ECDSAPrivateKeyPath == "" && c.JWT.ECDSAPrivateKey == "") ||
			(c.JWT.ECDSAPublicKeyPath == "" && c.JWT.ECDSAPublicKey == "") {
			return fmt.Errorf("ecdsa private and public keys are required for ES256 (provide either key paths or key content)")
		}
	default:
		return fmt.Errorf("unsupported jwt signing method: %s (supported: HS256, RS256, ES256)", c.JWT.SigningMethod)
	}

	// Validate OAuth configuration
	if c.OAuth.CallbackURL == "" {
		return fmt.Errorf("oauth callback url is required")
	}
	if len(c.OAuth.Providers) == 0 {
		return fmt.Errorf("at least one oauth provider is required")
	}

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
