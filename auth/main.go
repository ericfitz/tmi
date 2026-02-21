package auth

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
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
func rebuildCache(ctx context.Context, dbManager *db.Manager) error {
	gormDB := dbManager.Gorm().DB()
	redisClient := dbManager.Redis().GetClient()

	// Use GORM transaction for consistent reads
	return gormDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
	})
}

// threatModelWithOwner is a helper struct for the join query result
type threatModelWithOwner struct {
	ID         string
	OwnerEmail string
}

// rebuildThreatModelAuthCache rebuilds authorization data for threat models
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
type accessWithEmail struct {
	Email string
	Role  string
}

// getThreatModelRoles retrieves roles for a threat model
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
func GetDatabaseManager() *db.Manager {
	return db.GetGlobalManager()
}

// Shutdown gracefully shuts down the authentication system
func Shutdown(ctx context.Context) error {
	if mgr := db.GetGlobalManager(); mgr != nil {
		return mgr.Close()
	}
	return nil
}
