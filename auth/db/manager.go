package db

import (
	"context"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/internal/slogging"
)

// globalManager is the singleton database manager instance
var (
	globalManager   *Manager
	globalManagerMu sync.RWMutex
)

// SetGlobalManager sets the global database manager singleton.
// This should be called once during application startup after the manager is fully initialized.
// SEM@3080aafd268e1adeeb4b0e7b35049f3b5e926c7c: register the database manager singleton for process-wide access (mutates shared state)
func SetGlobalManager(m *Manager) {
	globalManagerMu.Lock()
	defer globalManagerMu.Unlock()
	globalManager = m
}

// GetGlobalManager returns the global database manager singleton.
// Returns nil if SetGlobalManager has not been called.
// SEM@3080aafd268e1adeeb4b0e7b35049f3b5e926c7c: fetch the process-wide database manager singleton (reads shared state)
func GetGlobalManager() *Manager {
	globalManagerMu.RLock()
	defer globalManagerMu.RUnlock()
	return globalManager
}

// Manager handles database connections
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: container holding GORM and Redis database connections with a mutex
type Manager struct {
	gorm  *GormDB
	redis *RedisDB
	mu    sync.Mutex
}

// NewManager creates a new database manager
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: build an empty database manager with no initialized connections (pure)
func NewManager() *Manager {
	return &Manager{}
}

// InitGorm initializes the GORM database connection (supports PostgreSQL, Oracle, MySQL, SQL Server, SQLite)
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: connect GORM to the configured relational database, failing if already initialized (mutates shared state)
func (m *Manager) InitGorm(cfg GormConfig) error {
	logger := slogging.Get()
	logger.Debug("Initializing GORM connection in database manager (type: %s)", cfg.Type)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gorm != nil {
		logger.Warn("GORM connection already initialized")
		return fmt.Errorf("gorm connection already initialized")
	}

	db, err := NewGormDB(cfg)
	if err != nil {
		logger.Error("Failed to initialize GORM: %v", err)
		return fmt.Errorf("failed to initialize gorm: %w", err)
	}

	logger.Debug("GORM connection successfully initialized in database manager")
	m.gorm = db
	return nil
}

// InitRedis initializes the Redis connection
// SEM@1d6e8926b4e58c0d98fff4d43bd3f6df1852d61a: connect to Redis using the supplied config, failing if already initialized (mutates shared state)
func (m *Manager) InitRedis(cfg RedisConfig) error {
	logger := slogging.Get()
	logger.Debug("Initializing Redis connection in database manager")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.redis != nil {
		logger.Warn("Redis connection already initialized")
		return fmt.Errorf("redis connection already initialized")
	}

	db, err := NewRedisDB(cfg)
	if err != nil {
		logger.Error("Failed to initialize Redis: %v", err)
		return fmt.Errorf("failed to initialize redis: %w", err)
	}

	logger.Debug("Redis connection successfully initialized in database manager")
	m.redis = db
	return nil
}

// Gorm returns the GORM database connection
// SEM@a251f60c11fe9831021be2539ff7d746fbd65b2c: return the initialized GORM connection, or nil if not yet connected (reads shared state)
func (m *Manager) Gorm() *GormDB {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gorm
}

// Redis returns the Redis connection
// SEM@d885c7955d5a30affb8ddde84ee1cf757aab2a6b: return the initialized Redis connection, or nil if not yet connected (reads shared state)
func (m *Manager) Redis() *RedisDB {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.redis
}

// Close closes all database connections
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: close all held database connections, collecting and returning any errors (mutates shared state)
func (m *Manager) Close() error {
	logger := slogging.Get()
	logger.Debug("Closing all database connections in manager")

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.gorm != nil {
		logger.Debug("Closing GORM connection")
		if err := m.gorm.Close(); err != nil {
			logger.Error("Failed to close GORM connection: %v", err)
			errs = append(errs, fmt.Errorf("failed to close gorm: %w", err))
		}
	}

	if m.redis != nil {
		logger.Debug("Closing Redis connection")
		if err := m.redis.Close(); err != nil {
			logger.Error("Failed to close Redis connection: %v", err)
			errs = append(errs, fmt.Errorf("failed to close redis: %w", err))
		}
	}

	if len(errs) > 0 {
		logger.Error("Errors occurred while closing database connections: %v", errs)
		return fmt.Errorf("errors closing database connections: %v", errs)
	}

	logger.Debug("All database connections closed successfully")
	return nil
}

// Ping checks if all database connections are alive
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: health-check all initialized database connections and return aggregated errors
func (m *Manager) Ping(ctx context.Context) error {
	logger := slogging.Get()
	logger.Debug("Pinging all database connections")

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.gorm != nil {
		logger.Debug("Pinging GORM connection")
		if err := m.gorm.Ping(ctx); err != nil {
			logger.Error("GORM ping failed: %v", err)
			errs = append(errs, fmt.Errorf("gorm ping failed: %w", err))
		} else {
			logger.Debug("GORM ping successful")
		}
	} else {
		logger.Debug("GORM connection not initialized, skipping ping")
	}

	if m.redis != nil {
		logger.Debug("Pinging Redis connection")
		if err := m.redis.Ping(ctx); err != nil {
			logger.Error("Redis ping failed: %v", err)
			errs = append(errs, fmt.Errorf("redis ping failed: %w", err))
		} else {
			logger.Debug("Redis ping successful")
		}
	} else {
		logger.Debug("Redis connection not initialized, skipping ping")
	}

	if len(errs) > 0 {
		logger.Error("Ping errors occurred: %v", errs)
		return fmt.Errorf("ping errors: %v", errs)
	}

	logger.Debug("All database connections pinged successfully")
	return nil
}

// LogConnectionStats logs statistics about all database connections
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: log pool and connection statistics for all active database backends
func (m *Manager) LogConnectionStats(ctx context.Context) {
	logger := slogging.Get()
	logger.Debug("Logging database connection statistics")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.gorm != nil {
		logger.Debug("Logging GORM connection stats")
		m.gorm.LogStats()
	}

	if m.redis != nil {
		logger.Debug("Logging Redis connection stats")
		m.redis.LogStats(ctx)
	}
}
