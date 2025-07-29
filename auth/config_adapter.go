package auth

import (
	"context"
	"fmt"
	"log"
	"path/filepath"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/gin-gonic/gin"
)

// ConfigFromUnified converts unified config to auth-specific config
func ConfigFromUnified(unified *config.Config) Config {
	return Config{
		Postgres: PostgresConfig{
			Host:     unified.Database.Postgres.Host,
			Port:     unified.Database.Postgres.Port,
			User:     unified.Database.Postgres.User,
			Password: unified.Database.Postgres.Password,
			Database: unified.Database.Postgres.Database,
			SSLMode:  unified.Database.Postgres.SSLMode,
		},
		Redis: RedisConfig{
			Host:     unified.Database.Redis.Host,
			Port:     unified.Database.Redis.Port,
			Password: unified.Database.Redis.Password,
			DB:       unified.Database.Redis.DB,
		},
		JWT: JWTConfig{
			Secret:            unified.Auth.JWT.Secret,
			ExpirationSeconds: unified.Auth.JWT.ExpirationSeconds,
			SigningMethod:     unified.Auth.JWT.SigningMethod,
		},
		OAuth: OAuthConfig{
			CallbackURL: unified.Auth.OAuth.CallbackURL,
			Providers:   convertOAuthProviders(unified.Auth.OAuth.Providers),
		},
	}
}

// convertOAuthProviders converts unified OAuth providers to auth-specific format
func convertOAuthProviders(unified map[string]config.OAuthProviderConfig) map[string]OAuthProviderConfig {
	providers := make(map[string]OAuthProviderConfig)

	for id, provider := range unified {
		providers[id] = OAuthProviderConfig{
			ID:               provider.ID,
			Name:             provider.Name,
			Enabled:          provider.Enabled,
			Icon:             provider.Icon,
			ClientID:         provider.ClientID,
			ClientSecret:     provider.ClientSecret,
			AuthorizationURL: provider.AuthorizationURL,
			TokenURL:         provider.TokenURL,
			UserInfoURL:      provider.UserInfoURL,
			Issuer:           provider.Issuer,
			JWKSURL:          provider.JWKSURL,
			Scopes:           provider.Scopes,
			AdditionalParams: provider.AdditionalParams,
			EmailClaim:       provider.EmailClaim,
			NameClaim:        provider.NameClaim,
			SubjectClaim:     provider.SubjectClaim,
		}
	}

	return providers
}

// InitAuthWithConfig initializes the auth system with unified configuration
func InitAuthWithConfig(router *gin.Engine, unified *config.Config) error {
	authConfig := ConfigFromUnified(unified)

	// Create database manager
	dbManager := db.NewManager()

	// Initialize PostgreSQL
	pgConfig, redisConfig := authConfig.ToDBConfig()
	if err := dbManager.InitPostgres(pgConfig); err != nil {
		return fmt.Errorf("failed to initialize postgres: %w", err)
	}

	// Initialize Redis
	if err := dbManager.InitRedis(redisConfig); err != nil {
		return fmt.Errorf("failed to initialize redis: %w", err)
	}

	// Run database migrations
	migrationsPath := filepath.Join("auth", "migrations")
	if err := dbManager.RunMigrations(db.MigrationConfig{
		MigrationsPath: migrationsPath,
		DatabaseName:   authConfig.Postgres.Database,
	}); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create authentication service
	service, err := NewService(dbManager, authConfig)
	if err != nil {
		return fmt.Errorf("failed to create auth service: %w", err)
	}

	// Create authentication handlers
	handlers := NewHandlers(service, authConfig)

	// Register routes
	handlers.RegisterRoutes(router)

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	log.Println("Authentication system initialized successfully")
	return nil
}
