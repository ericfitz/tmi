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
	// Get the database connections
	postgres := dbManager.Postgres().GetDB()
	redis := dbManager.Redis().GetClient()

	// Start a transaction
	tx, err := postgres.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if not committed

	// 1. Get all threat models from PostgreSQL
	rows, err := tx.QueryContext(ctx, `
		SELECT id, owner_email FROM threat_models
	`)
	if err != nil {
		return fmt.Errorf("failed to get threat models: %w", err)
	}
	defer rows.Close()

	// 2. For each threat model, get its authorization data and store in Redis
	for rows.Next() {
		var id, ownerEmail string
		if err := rows.Scan(&id, &ownerEmail); err != nil {
			return fmt.Errorf("failed to scan threat model: %w", err)
		}

		// Get all users with access to this threat model
		accessRows, err := tx.QueryContext(ctx, `
			SELECT user_email, role FROM threat_model_access
			WHERE threat_model_id = $1
		`, id)
		if err != nil {
			return fmt.Errorf("failed to get threat model access: %w", err)
		}
		defer accessRows.Close()

		// Create a map of user emails to roles
		roles := make(map[string]string)
		roles[ownerEmail] = "owner" // The owner always has owner role

		for accessRows.Next() {
			var email, role string
			if err := accessRows.Scan(&email, &role); err != nil {
				return fmt.Errorf("failed to scan threat model access: %w", err)
			}
			roles[email] = role
		}

		if err := accessRows.Err(); err != nil {
			return fmt.Errorf("error iterating threat model access: %w", err)
		}

		// 3. Store the authorization data in Redis
		key := fmt.Sprintf("threatmodel:%s:roles", id)
		for email, role := range roles {
			if err := redis.HSet(ctx, key, email, role).Err(); err != nil {
				return fmt.Errorf("failed to store role in Redis: %w", err)
			}
		}

		// Set expiration for the key
		if err := redis.Expire(ctx, key, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to set expiration for Redis key: %w", err)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating threat models: %w", err)
	}

	// 4. Get all threats and their parent threat model IDs
	threatRows, err := tx.QueryContext(ctx, `
		SELECT id, threat_model_id FROM threats
	`)
	if err != nil {
		return fmt.Errorf("failed to get threats: %w", err)
	}
	defer threatRows.Close()

	// 5. Store the threat-to-threat-model mapping in Redis
	for threatRows.Next() {
		var id, threatModelID string
		if err := threatRows.Scan(&id, &threatModelID); err != nil {
			return fmt.Errorf("failed to scan threat: %w", err)
		}

		key := fmt.Sprintf("threat:%s:threatmodel", id)
		if err := redis.Set(ctx, key, threatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store threat mapping in Redis: %w", err)
		}
	}

	if err := threatRows.Err(); err != nil {
		return fmt.Errorf("error iterating threats: %w", err)
	}

	// 6. Get all diagrams and their parent threat model IDs
	diagramRows, err := tx.QueryContext(ctx, `
		SELECT id, threat_model_id FROM diagrams
	`)
	if err != nil {
		return fmt.Errorf("failed to get diagrams: %w", err)
	}
	defer diagramRows.Close()

	// 7. Store the diagram-to-threat-model mapping in Redis
	for diagramRows.Next() {
		var id, threatModelID string
		if err := diagramRows.Scan(&id, &threatModelID); err != nil {
			return fmt.Errorf("failed to scan diagram: %w", err)
		}

		key := fmt.Sprintf("diagram:%s:threatmodel", id)
		if err := redis.Set(ctx, key, threatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store diagram mapping in Redis: %w", err)
		}
	}

	if err := diagramRows.Err(); err != nil {
		return fmt.Errorf("error iterating diagrams: %w", err)
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Shutdown gracefully shuts down the authentication system
func Shutdown(dbManager *db.Manager) error {
	if dbManager != nil {
		return dbManager.Close()
	}
	return nil
}
