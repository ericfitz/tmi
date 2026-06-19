package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// InitAuth initializes the authentication system
// SEM@acf29174839ed9f1cb1950265092e2bdacdcb5bd: initialize the auth subsystem: DB, Redis, migrations, service, and background jobs
func InitAuth(router *gin.Engine) error {
	// Load configuration
	config, err := LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create database manager
	dbManager := db.NewManager()

	// Initialize GORM (supports PostgreSQL, Oracle, MySQL, SQL Server, SQLite)
	gormConfig := config.ToGormConfig()
	if err := dbManager.InitGorm(gormConfig); err != nil {
		return fmt.Errorf("failed to initialize gorm: %w", err)
	}

	// Initialize Redis
	redisConfig := config.ToRedisConfig()
	if err := dbManager.InitRedis(redisConfig); err != nil {
		return fmt.Errorf("failed to initialize redis: %w", err)
	}

	// Run database migrations using GORM AutoMigrate for all databases
	// This provides a single source of truth (api/models/models.go) for all supported databases
	gormDB := dbManager.Gorm()
	if gormDB == nil {
		return fmt.Errorf("GORM database not initialized")
	}
	if err := gormDB.AutoMigrate(models.AllModels()...); err != nil {
		return fmt.Errorf("failed to auto-migrate schema: %w", err)
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
	// Get underlying *sql.DB from GORM for health monitoring
	sqlDB, err := dbManager.Gorm().DB().DB()
	if err != nil {
		return fmt.Errorf("failed to get underlying sql.DB from GORM: %w", err)
	}
	db.StartHealthMonitor(context.Background(), sqlDB, 25*time.Second)

	// Start background job for periodic cache rebuilding
	go startCacheRebuildJob(context.Background(), dbManager)

	// Store global reference to database manager
	db.SetGlobalManager(dbManager)

	slogging.Get().Info("Authentication system initialized successfully")
	return nil
}

// startCacheRebuildJob starts a background job to periodically rebuild the Redis cache
// SEM@d8df570eb0fcf431602cc34810bdf6bc7d933155: run a hourly background loop that rebuilds the Redis authorization cache (mutates shared state)
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
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: rebuild the auth cache with connection-pool refresh and exponential-backoff retry (reads DB)
func rebuildCacheWithRetry(ctx context.Context, dbManager *db.Manager) error {
	const maxRetries = 3
	baseDelay := 5 * time.Second
	logger := slogging.Get()

	var lastErr error
	for attempt := range maxRetries {
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
		sqlDB, err := dbManager.Gorm().DB().DB()
		if err != nil {
			lastErr = fmt.Errorf("failed to get underlying sql.DB from GORM: %w", err)
			logger.Warn("Failed to get sql.DB (attempt %d/%d): %v", attempt+1, maxRetries, err)
			continue
		}
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

// rebuildCache rebuilds the Redis cache from the database using GORM
// SEM@d0742bff5d3b93b3ab7b22df0377398a720a8d9c: repopulate Redis authorization and mapping caches from the DB in a serializable transaction (reads DB)
func rebuildCache(ctx context.Context, dbManager *db.Manager) error {
	gormDB := dbManager.Gorm().DB()
	redisClient := dbManager.Redis().GetClient()

	// Use GORM transaction for consistent reads. This is a read-only path (all
	// writes go to Redis); a serializable, read-only transaction gives the
	// consistent cross-statement snapshot the cache rebuild needs. ReadOnly is
	// declared explicitly so Oracle can optimize and DML is forbidden — it cannot
	// raise ORA-08177 since it issues no DML.
	return db.WithRetryableGormTransaction(ctx, gormDB, db.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Rebuild threat model authorization cache
		if err := rebuildThreatModelAuthCache(ctx, tx, redisClient); err != nil {
			return err
		}

		// Rebuild threat-to-threat-model mapping cache
		if err := rebuildThreatMappingCache(ctx, tx, redisClient); err != nil {
			return err
		}

		// Rebuild diagram-to-threat-model mapping cache
		if err := rebuildDiagramMappingCache(ctx, tx, redisClient); err != nil {
			return err
		}

		return nil
	}, &sql.TxOptions{ReadOnly: true})
}

// threatModelWithOwner is a helper struct for the join query result
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: query result struct pairing a threat model ID with its owner's email (pure)
type threatModelWithOwner struct {
	ID         string
	OwnerEmail string
}

// rebuildThreatModelAuthCache rebuilds authorization data for threat models
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: store per-threat-model role assignments in Redis from a live DB transaction (reads DB)
func rebuildThreatModelAuthCache(ctx context.Context, tx *gorm.DB, redisClient *redis.Client) error {
	// Query threat models with owner email via join
	var results []threatModelWithOwner
	err := tx.Model(&models.ThreatModel{}).
		Select("threat_models.id, users.email as owner_email").
		Joins("JOIN users ON threat_models.owner_internal_uuid = users.internal_uuid").
		Find(&results).Error
	if err != nil {
		return fmt.Errorf("failed to get threat models: %w", err)
	}

	for _, result := range results {
		roles, err := getThreatModelRoles(ctx, tx, result.ID, result.OwnerEmail)
		if err != nil {
			return err
		}

		if err := storeThreatModelRoles(ctx, redisClient, result.ID, roles); err != nil {
			return err
		}
	}

	return nil
}

// accessWithEmail is a helper struct for the access query result
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: query result struct pairing an email with its access role (pure)
type accessWithEmail struct {
	Email string
	Role  string
}

// getThreatModelRoles retrieves roles for a threat model
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: fetch user role assignments for a threat model via DB join, including the owner (reads DB)
func getThreatModelRoles(_ context.Context, tx *gorm.DB, threatModelID, ownerEmail string) (map[string]string, error) {
	// Query access records with user email via join
	var accessResults []accessWithEmail
	err := tx.Model(&models.ThreatModelAccess{}).
		Select("users.email, threat_model_access.role").
		Joins("JOIN users ON threat_model_access.user_internal_uuid = users.internal_uuid").
		Where("threat_model_access.threat_model_id = ? AND threat_model_access.subject_type = ?", threatModelID, "user").
		Find(&accessResults).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model access: %w", err)
	}

	roles := make(map[string]string)
	roles[ownerEmail] = "owner"

	for _, access := range accessResults {
		roles[access.Email] = access.Role
	}

	return roles, nil
}

// storeThreatModelRoles stores authorization roles in Redis
// SEM@e9624ed5a78358edd81f110113b74d2890b61e73: write a threat model's email-to-role map into Redis with a 24-hour TTL
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
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: store threat-to-threat-model ID mappings in Redis from the DB (reads DB)
func rebuildThreatMappingCache(ctx context.Context, tx *gorm.DB, redisClient *redis.Client) error {
	var threats []models.Threat
	if err := tx.Select("id, threat_model_id").Find(&threats).Error; err != nil {
		return fmt.Errorf("failed to get threats: %w", err)
	}

	for _, threat := range threats {
		key := fmt.Sprintf("threat:%s:threatmodel", threat.ID)
		if err := redisClient.Set(ctx, key, threat.ThreatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store threat mapping in Redis: %w", err)
		}
	}

	return nil
}

// rebuildDiagramMappingCache rebuilds diagram-to-threat-model mapping cache
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: store diagram-to-threat-model ID mappings in Redis from the DB (reads DB)
func rebuildDiagramMappingCache(ctx context.Context, tx *gorm.DB, redisClient *redis.Client) error {
	var diagrams []models.Diagram
	if err := tx.Select("id, threat_model_id").Find(&diagrams).Error; err != nil {
		return fmt.Errorf("failed to get diagrams: %w", err)
	}

	for _, diagram := range diagrams {
		key := fmt.Sprintf("diagram:%s:threatmodel", diagram.ID)
		if err := redisClient.Set(ctx, key, diagram.ThreatModelID, 24*time.Hour).Err(); err != nil {
			return fmt.Errorf("failed to store diagram mapping in Redis: %w", err)
		}
	}

	return nil
}

// GetDatabaseManager returns the global database manager.
//
// Deprecated: Use db.GetGlobalManager() instead.
// This function is retained for backward compatibility with code that uses
// auth.GetDatabaseManager() after calling auth.InitAuthWithConfig().
// SEM@3080aafd268e1adeeb4b0e7b35049f3b5e926c7c: fetch the global database manager; deprecated in favor of db.GetGlobalManager (pure)
func GetDatabaseManager() *db.Manager {
	return db.GetGlobalManager()
}

// Shutdown gracefully shuts down the authentication system
// SEM@3080aafd268e1adeeb4b0e7b35049f3b5e926c7c: close all database and Redis connections held by the global auth manager
func Shutdown(ctx context.Context) error {
	if mgr := db.GetGlobalManager(); mgr != nil {
		return mgr.Close()
	}
	return nil
}
