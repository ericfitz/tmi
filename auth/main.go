package auth

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/gin-gonic/gin"
)

// InitAuth initializes the authentication system
func InitAuth(router *gin.Engine) error {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create database manager
	dbManager := db.NewManager()

	// Initialize PostgreSQL
	pgConfig, redisConfig := config.ToDBConfig()
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
		DatabaseName:   config.Postgres.Database,
	}); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Create authentication service
	service, err := NewService(dbManager, config)
	if err != nil {
		return fmt.Errorf("failed to create auth service: %w", err)
	}

	// Create authentication handlers
	handlers := NewHandlers(service, config)

	// Register routes
	handlers.RegisterRoutes(router)

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	log.Println("Authentication system initialized successfully")
	return nil
}

// startCacheRebuildJob starts a background job to periodically rebuild the Redis cache
func startCacheRebuildJob(ctx context.Context, dbManager *db.Manager) {
	ticker := time.NewTicker(1 * time.Hour) // Rebuild cache every hour
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := rebuildCache(ctx, dbManager); err != nil {
				log.Printf("Failed to rebuild cache: %v", err)
			} else {
				log.Println("Cache rebuilt successfully")
			}
		}
	}
}

// rebuildCache rebuilds the Redis cache from PostgreSQL
func rebuildCache(ctx context.Context, dbManager *db.Manager) error {
	// TODO: Implement cache rebuilding logic
	// 1. Get all threat models from PostgreSQL
	// 2. For each threat model, get its authorization data
	// 3. Store the authorization data in Redis
	// 4. Get all threats and their parent threat model IDs
	// 5. Store the threat-to-threat-model mapping in Redis
	// 6. Get all diagrams and their parent threat model IDs
	// 7. Store the diagram-to-threat-model mapping in Redis

	// For now, just return nil
	return nil
}

// Shutdown gracefully shuts down the authentication system
func Shutdown(dbManager *db.Manager) error {
	if dbManager != nil {
		return dbManager.Close()
	}
	return nil
}
