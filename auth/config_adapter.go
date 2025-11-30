package auth

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
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
		SAML: SAMLConfig{
			Enabled:   unified.Auth.SAML.Enabled,
			Providers: convertSAMLProviders(unified.Auth.SAML.Providers),
		},
	}
}

// convertOAuthProviders converts unified OAuth providers to auth-specific format
func convertOAuthProviders(unified map[string]config.OAuthProviderConfig) map[string]OAuthProviderConfig {
	providers := make(map[string]OAuthProviderConfig)

	for id, provider := range unified {
		// Convert UserInfo endpoints
		var userInfo []UserInfoEndpoint
		for _, endpoint := range provider.UserInfo {
			userInfo = append(userInfo, UserInfoEndpoint{
				URL:    endpoint.URL,
				Claims: endpoint.Claims,
			})
		}

		providers[id] = OAuthProviderConfig{
			ID:               provider.ID,
			Name:             provider.Name,
			Enabled:          provider.Enabled,
			Icon:             provider.Icon,
			ClientID:         provider.ClientID,
			ClientSecret:     provider.ClientSecret,
			AuthorizationURL: provider.AuthorizationURL,
			TokenURL:         provider.TokenURL,
			UserInfo:         userInfo,
			Issuer:           provider.Issuer,
			JWKSURL:          provider.JWKSURL,
			Scopes:           provider.Scopes,
			AdditionalParams: provider.AdditionalParams,
			AuthHeaderFormat: provider.AuthHeaderFormat,
			AcceptHeader:     provider.AcceptHeader,
		}
	}

	return providers
}

// convertSAMLProviders converts unified SAML providers to auth-specific format
func convertSAMLProviders(unified map[string]config.SAMLProviderConfig) map[string]SAMLProviderConfig {
	providers := make(map[string]SAMLProviderConfig)

	for id, provider := range unified {
		providers[id] = SAMLProviderConfig{
			ID:                id,
			Name:              provider.Name,
			Enabled:           provider.Enabled,
			Icon:              provider.Icon,
			EntityID:          provider.EntityID,
			MetadataURL:       provider.MetadataURL,
			MetadataXML:       provider.MetadataXML,
			ACSURL:            provider.ACSURL,
			SLOURL:            provider.SLOURL,
			SPPrivateKey:      provider.SPPrivateKey,
			SPPrivateKeyPath:  provider.SPPrivateKeyPath,
			SPCertificate:     provider.SPCertificate,
			SPCertificatePath: provider.SPCertificatePath,
			IDPMetadataURL:    provider.IDPMetadataURL,
			IDPMetadataXML:    provider.IDPMetadataXML,
			AllowIDPInitiated: provider.AllowIDPInitiated,
			ForceAuthn:        provider.ForceAuthn,
			SignRequests:      provider.SignRequests,
			NameIDAttribute:   provider.NameIDAttribute,
			EmailAttribute:    provider.EmailAttribute,
			NameAttribute:     provider.NameAttribute,
			GroupsAttribute:   provider.GroupsAttribute,
		}
	}

	return providers
}

// InitAuthWithConfig initializes the auth system with unified configuration
func InitAuthWithConfig(router *gin.Engine, unified *config.Config) (*Handlers, error) {
	authConfig := ConfigFromUnified(unified)

	// Create database manager
	dbManager := db.NewManager()

	// Initialize PostgreSQL
	pgConfig, redisConfig := authConfig.ToDBConfig()
	if err := dbManager.InitPostgres(pgConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize postgres: %w", err)
	}

	// Initialize Redis
	if err := dbManager.InitRedis(redisConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize redis: %w", err)
	}

	// Run database migrations
	migrationsPath := filepath.Join("auth", "migrations")
	if err := dbManager.RunMigrations(db.MigrationConfig{
		MigrationsPath: migrationsPath,
		DatabaseName:   authConfig.Postgres.Database,
	}); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create authentication service
	service, err := NewService(dbManager, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	// Create authentication handlers
	handlers := NewHandlers(service, authConfig)

	// Register routes
	// Skip route registration - routes will be handled by OpenAPI integration
	slogging.Get().Info("[AUTH_CONFIG_ADAPTER] Route registration DISABLED - delegating to OpenAPI")
	// handlers.RegisterRoutes(router) // Disabled to avoid conflicts with OpenAPI routes

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	// Store global reference to database manager
	globalDBManager = dbManager

	slogging.Get().Info("Authentication system initialized successfully")
	return handlers, nil
}
