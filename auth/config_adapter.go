package auth

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/config"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// ConfigFromUnified converts unified config to auth-specific config
func ConfigFromUnified(unified *config.Config) Config {
	return Config{
		Database: DatabaseConfig{
			URL:                  unified.Database.URL,
			OracleWalletLocation: unified.Database.OracleWalletLocation,
			// Connection pool configuration
			MaxOpenConns:    unified.Database.ConnectionPool.MaxOpenConns,
			MaxIdleConns:    unified.Database.ConnectionPool.MaxIdleConns,
			ConnMaxLifetime: unified.Database.ConnectionPool.ConnMaxLifetime,
			ConnMaxIdleTime: unified.Database.ConnectionPool.ConnMaxIdleTime,
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
		BuildMode: unified.Auth.BuildMode,
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
			IDPMetadataB64XML: provider.IDPMetadataB64XML,
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

// InitAuthWithDB initializes the auth system with an existing database manager.
// This is the preferred initialization method for explicit dependency injection.
// The caller is responsible for initializing the database connections before calling this function.
func InitAuthWithDB(dbManager *db.Manager, unified *config.Config) (*Handlers, error) {
	if dbManager == nil {
		return nil, fmt.Errorf("database manager is required")
	}

	authConfig := ConfigFromUnified(unified)
	logger := slogging.Get()

	logger.Info("[AUTH_CONFIG_ADAPTER] Initializing auth system with provided database manager")

	// Create authentication service
	service, err := NewService(dbManager, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	// Create authentication handlers
	handlers := NewHandlers(service, authConfig)

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	// Note: db.SetGlobalManager is called by main.go during Phase 1
	// We don't set it here to avoid duplicate initialization

	logger.Info("Authentication system initialized successfully with injected database manager")
	return handlers, nil
}

// InitAuthWithConfig initializes the auth system with unified configuration.
// Deprecated: Use InitAuthWithDB for explicit dependency injection.
// This function creates its own database manager internally, which can lead to
// duplicate initialization and DRY violations. Prefer passing a pre-initialized
// db.Manager to InitAuthWithDB instead.
func InitAuthWithConfig(router *gin.Engine, unified *config.Config) (*Handlers, error) {
	authConfig := ConfigFromUnified(unified)
	logger := slogging.Get()

	logger.Warn("[AUTH_CONFIG_ADAPTER] Using deprecated InitAuthWithConfig - prefer InitAuthWithDB for explicit dependency injection")

	// Create database manager
	dbManager := db.NewManager()

	// Initialize database based on type (use GORM for unified PostgreSQL/Oracle support)
	gormConfig := authConfig.ToGormConfig()
	logger.Info("[AUTH_CONFIG_ADAPTER] Initializing database connection (type: %s)", gormConfig.Type)

	if err := dbManager.InitGorm(gormConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize database (%s): %w", gormConfig.Type, err)
	}

	// Initialize Redis
	redisConfig := authConfig.ToRedisConfig()
	if err := dbManager.InitRedis(redisConfig); err != nil {
		return nil, fmt.Errorf("failed to initialize redis: %w", err)
	}

	// Run database migrations using GORM AutoMigrate for all databases
	// This provides a single source of truth (api/models/models.go) for all supported databases
	gormDB := dbManager.Gorm()
	if gormDB == nil {
		return nil, fmt.Errorf("GORM database not initialized")
	}
	if err := gormDB.AutoMigrate(models.AllModels()...); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate schema: %w", err)
	}
	logger.Info("[AUTH_CONFIG_ADAPTER] GORM AutoMigrate completed")

	// Create authentication service
	service, err := NewService(dbManager, authConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth service: %w", err)
	}

	// Create authentication handlers
	handlers := NewHandlers(service, authConfig)

	// Register routes
	// Skip route registration - routes will be handled by OpenAPI integration
	logger.Info("[AUTH_CONFIG_ADAPTER] Route registration DISABLED - delegating to OpenAPI")
	// handlers.RegisterRoutes(router) // Disabled to avoid conflicts with OpenAPI routes

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	// Store global reference to database manager
	db.SetGlobalManager(dbManager)

	logger.Info("Authentication system initialized successfully (database type: %s)", gormConfig.Type)
	return handlers, nil
}
