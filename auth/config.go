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
	Secret            string
	ExpirationSeconds int
	SigningMethod     string
}

// OAuthConfig holds OAuth configuration
type OAuthConfig struct {
	CallbackURL string
	Providers   map[string]OAuthProviderConfig
}

// OAuthProviderConfig holds configuration for an OAuth provider
type OAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	Scopes       []string
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
			Secret:            getEnv("JWT_SECRET", "your-secret-key"),
			ExpirationSeconds: jwtExpiration,
			SigningMethod:     getEnv("JWT_SIGNING_METHOD", "HS256"),
		},
		OAuth: OAuthConfig{
			CallbackURL: getEnv("OAUTH_CALLBACK_URL", "http://localhost:8080/auth/callback"),
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
			ClientID:     getEnv("OAUTH_GOOGLE_CLIENT_ID", ""),
			ClientSecret: getEnv("OAUTH_GOOGLE_CLIENT_SECRET", ""),
			AuthURL:      getEnv("OAUTH_GOOGLE_AUTH_URL", "https://accounts.google.com/o/oauth2/auth"),
			TokenURL:     getEnv("OAUTH_GOOGLE_TOKEN_URL", "https://oauth2.googleapis.com/token"),
			UserInfoURL:  getEnv("OAUTH_GOOGLE_USERINFO_URL", "https://www.googleapis.com/oauth2/v3/userinfo"),
			Scopes:       []string{"openid", "profile", "email"},
		}
	}

	// GitHub OAuth configuration
	if getEnv("OAUTH_GITHUB_ENABLED", "true") == "true" {
		providers["github"] = OAuthProviderConfig{
			ClientID:     getEnv("OAUTH_GITHUB_CLIENT_ID", ""),
			ClientSecret: getEnv("OAUTH_GITHUB_CLIENT_SECRET", ""),
			AuthURL:      getEnv("OAUTH_GITHUB_AUTH_URL", "https://github.com/login/oauth/authorize"),
			TokenURL:     getEnv("OAUTH_GITHUB_TOKEN_URL", "https://github.com/login/oauth/access_token"),
			UserInfoURL:  getEnv("OAUTH_GITHUB_USERINFO_URL", "https://api.github.com/user"),
			Scopes:       []string{"user:email"},
		}
	}

	// Microsoft OAuth configuration
	if getEnv("OAUTH_MICROSOFT_ENABLED", "true") == "true" {
		providers["microsoft"] = OAuthProviderConfig{
			ClientID:     getEnv("OAUTH_MICROSOFT_CLIENT_ID", ""),
			ClientSecret: getEnv("OAUTH_MICROSOFT_CLIENT_SECRET", ""),
			AuthURL:      getEnv("OAUTH_MICROSOFT_AUTH_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize"),
			TokenURL:     getEnv("OAUTH_MICROSOFT_TOKEN_URL", "https://login.microsoftonline.com/common/oauth2/v2.0/token"),
			UserInfoURL:  getEnv("OAUTH_MICROSOFT_USERINFO_URL", "https://graph.microsoft.com/v1.0/me"),
			Scopes:       []string{"openid", "profile", "email", "User.Read"},
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
	if c.JWT.Secret == "" || c.JWT.Secret == "your-secret-key" {
		return fmt.Errorf("jwt secret is required and should not be the default value")
	}
	if c.JWT.ExpirationSeconds <= 0 {
		return fmt.Errorf("jwt expiration must be greater than 0")
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
