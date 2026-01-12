package auth

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

// Global database manager for access from other packages
var globalDBManager *db.Manager

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
	// Note: Route registration is handled via OpenAPI specification
	_ = NewHandlers(service, config)

	// Start background health monitor to keep connection pool warm
	// Use 25-second interval to stay under Heroku's ~30s idle timeout
	db.StartHealthMonitor(context.Background(), dbManager.Postgres().GetDB(), 25*time.Second)

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	// Store global reference to database manager
	globalDBManager = dbManager

	slogging.Get().Info("Authentication system initialized successfully")
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
			if err := rebuildCacheWithRetry(ctx, dbManager); err != nil {
				slogging.Get().Error("Failed to rebuild cache after retries: %v", err)
			} else {
				slogging.Get().Info("Cache rebuilt successfully")
			}
		}
	}
}

// rebuildCacheWithRetry attempts to rebuild the cache with exponential backoff retry
func rebuildCacheWithRetry(ctx context.Context, dbManager *db.Manager) error {
	const maxRetries = 3
	baseDelay := 5 * time.Second
	logger := slogging.Get()

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 5s, 10s, 20s
			// #nosec G115 - attempt is always in range [1, maxRetries-1] so no overflow possible
			delay := baseDelay * time.Duration(1<<uint(attempt-1))
			logger.Info("Retrying cache rebuild in %v (attempt %d/%d)", delay, attempt+1, maxRetries)

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		// CRITICAL: Refresh the connection pool before each attempt
		// This closes stale idle connections and creates fresh ones
		// A simple Ping() only validates ONE connection, but the subsequent
		// transaction may get a DIFFERENT (stale) connection from the pool
		sqlDB := dbManager.Postgres().GetDB()
		if err := db.RefreshConnectionPool(sqlDB); err != nil {
			lastErr = fmt.Errorf("failed to refresh connection pool: %w", err)
			logger.Warn("Connection pool refresh failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}
		logger.Debug("Connection pool refreshed successfully before cache rebuild")

		// Attempt cache rebuild with the refreshed pool
		if err := rebuildCache(ctx, dbManager); err != nil {
			lastErr = err
			// Check if this is a connection error that warrants retry
			if db.IsConnectionError(err) {
				logger.Warn("Cache rebuild failed with connection error (attempt %d/%d): %v", attempt+1, maxRetries, err)
				continue
			}
			// Non-connection error - log as warning and retry anyway for resilience
			logger.Warn("Cache rebuild failed (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}

		// Success
		return nil
	}

	return fmt.Errorf("cache rebuild failed after %d attempts: %w", maxRetries, lastErr)
}

// rebuildCache rebuilds the Redis cache from PostgreSQL
func rebuildCache(ctx context.Context, dbManager *db.Manager) error {
	postgres := dbManager.Postgres().GetDB()
	redis := dbManager.Redis().GetClient()

	tx, err := postgres.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			if err := tx.Rollback(); err != nil {
				slogging.Get().Error("Error rolling back transaction: %v", err)
			}
		}
	}()

	// Rebuild threat model authorization cache
	if err := rebuildThreatModelAuthCache(ctx, tx, redis); err != nil {
		return err
	}

	// Rebuild threat-to-threat-model mapping cache
	if err := rebuildThreatMappingCache(ctx, tx, redis); err != nil {
		return err
	}

	// Rebuild diagram-to-threat-model mapping cache
	if err := rebuildDiagramMappingCache(ctx, tx, redis); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	committed = true

	return nil
}

// rebuildThreatModelAuthCache rebuilds authorization data for threat models
func rebuildThreatModelAuthCache(ctx context.Context, tx *sql.Tx, redis *redis.Client) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT tm.id, u.email
		FROM threat_models tm
		JOIN users u ON tm.owner_internal_uuid = u.internal_uuid
	`)
	if err != nil {
		return fmt.Errorf("failed to get threat models: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slogging.Get().Error("Error closing rows: %v", err)
		}
	}()

	for rows.Next() {
		var id, ownerEmail string
		if err := rows.Scan(&id, &ownerEmail); err != nil {
			return fmt.Errorf("failed to scan threat model: %w", err)
		}

		roles, err := getThreatModelRoles(ctx, tx, id, ownerEmail)
		if err != nil {
			return err
		}

		if err := storeThreatModelRoles(ctx, redis, id, roles); err != nil {
			return err
		}
	}

	return rows.Err()
}

// getThreatModelRoles retrieves roles for a threat model
func getThreatModelRoles(ctx context.Context, tx *sql.Tx, threatModelID, ownerEmail string) (map[string]string, error) {
	accessRows, err := tx.QueryContext(ctx, `
		SELECT u.email, tma.role
		FROM threat_model_access tma
		JOIN users u ON tma.user_internal_uuid = u.internal_uuid
		WHERE tma.threat_model_id = $1
		  AND tma.subject_type = 'user'
	`, threatModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model access: %w", err)
	}
	defer func() {
		if err := accessRows.Close(); err != nil {
			slogging.Get().Error("Error closing accessRows: %v", err)
		}
	}()

	roles := make(map[string]string)
	roles[ownerEmail] = "owner"

	for accessRows.Next() {
		var email, role string
		if err := accessRows.Scan(&email, &role); err != nil {
			return nil, fmt.Errorf("failed to scan threat model access: %w", err)
		}
		roles[email] = role
	}

	if err := accessRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating threat model access: %w", err)
	}

	return roles, nil
}

// storeThreatModelRoles stores authorization roles in Redis
func storeThreatModelRoles(ctx context.Context, redis *redis.Client, threatModelID string, roles map[string]string) error {
	key := fmt.Sprintf("threatmodel:%s:roles", threatModelID)
	for email, role := range roles {
		if err := redis.HSet(ctx, key, email, role).Err(); err != nil {
			return fmt.Errorf("failed to store role in Redis: %w", err)
		}
	}

	if err := redis.Expire(ctx, key, 24*time.Hour).Err(); err != nil {
		return fmt.Errorf("failed to set expiration for Redis key: %w", err)
	}

	return nil
}

// rebuildThreatMappingCache rebuilds threat-to-threat-model mapping cache
func rebuildThreatMappingCache(ctx context.Context, tx *sql.Tx, redis *redis.Client) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, threat_model_id FROM threats`)
	if err != nil {
		return fmt.Errorf("failed to get threats: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slogging.Get().Error("Error closing rows: %v", err)
		}
	}()

	for rows.Next() {
		var id, threatModelID string
		if err := rows.Scan(&id, &threatModelID); err != nil {
			return fmt.Errorf("failed to scan threat: %w", err)
		}

		key := fmt.Sprintf("threat:%s:threatmodel", id)
		if err := redis.Set(ctx, key, threatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store threat mapping in Redis: %w", err)
		}
	}

	return rows.Err()
}

// rebuildDiagramMappingCache rebuilds diagram-to-threat-model mapping cache
func rebuildDiagramMappingCache(ctx context.Context, tx *sql.Tx, redis *redis.Client) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, threat_model_id FROM diagrams`)
	if err != nil {
		return fmt.Errorf("failed to get diagrams: %w", err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			slogging.Get().Error("Error closing rows: %v", err)
		}
	}()

	for rows.Next() {
		var id, threatModelID string
		if err := rows.Scan(&id, &threatModelID); err != nil {
			return fmt.Errorf("failed to scan diagram: %w", err)
		}

		key := fmt.Sprintf("diagram:%s:threatmodel", id)
		if err := redis.Set(ctx, key, threatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store diagram mapping in Redis: %w", err)
		}
	}

	return rows.Err()
}

// GetDatabaseManager returns the global database manager
func GetDatabaseManager() *db.Manager {
	return globalDBManager
}

// Shutdown gracefully shuts down the authentication system
func Shutdown(ctx context.Context) error {
	if globalDBManager != nil {
		return globalDBManager.Close()
	}
	return nil
}
