package db

import (
	"context"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/internal/logging"
)

// Manager handles database connections
type Manager struct {
	postgres *PostgresDB
	redis    *RedisDB
	mu       sync.Mutex
}

// NewManager creates a new database manager
func NewManager() *Manager {
	return &Manager{}
}

// InitPostgres initializes the PostgreSQL connection
func (m *Manager) InitPostgres(cfg PostgresConfig) error {
	logger := logging.Get()
	logger.Debug("Initializing PostgreSQL connection in database manager")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.postgres != nil {
		logger.Warn("PostgreSQL connection already initialized")
		return fmt.Errorf("postgres connection already initialized")
	}

	db, err := NewPostgresDB(cfg)
	if err != nil {
		logger.Error("Failed to initialize PostgreSQL: %v", err)
		return fmt.Errorf("failed to initialize postgres: %w", err)
	}

	logger.Debug("PostgreSQL connection successfully initialized in database manager")
	m.postgres = db
	return nil
}

// InitRedis initializes the Redis connection
func (m *Manager) InitRedis(cfg RedisConfig) error {
	logger := logging.Get()
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

// Postgres returns the PostgreSQL connection
func (m *Manager) Postgres() *PostgresDB {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.postgres
}

// Redis returns the Redis connection
func (m *Manager) Redis() *RedisDB {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.redis
}

// Close closes all database connections
func (m *Manager) Close() error {
	logger := logging.Get()
	logger.Debug("Closing all database connections in manager")

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.postgres != nil {
		logger.Debug("Closing PostgreSQL connection")
		if err := m.postgres.Close(); err != nil {
			logger.Error("Failed to close PostgreSQL connection: %v", err)
			errs = append(errs, fmt.Errorf("failed to close postgres: %w", err))
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
func (m *Manager) Ping(ctx context.Context) error {
	logger := logging.Get()
	logger.Debug("Pinging all database connections")

	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.postgres != nil {
		logger.Debug("Pinging PostgreSQL connection")
		if err := m.postgres.Ping(ctx); err != nil {
			logger.Error("PostgreSQL ping failed: %v", err)
			errs = append(errs, fmt.Errorf("postgres ping failed: %w", err))
		} else {
			logger.Debug("PostgreSQL ping successful")
		}
	} else {
		logger.Debug("PostgreSQL connection not initialized, skipping ping")
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
func (m *Manager) LogConnectionStats(ctx context.Context) {
	logger := logging.Get()
	logger.Debug("Logging database connection statistics")

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.postgres != nil {
		logger.Debug("Logging PostgreSQL connection stats")
		m.postgres.LogStats()
	}

	if m.redis != nil {
		logger.Debug("Logging Redis connection stats")
		m.redis.LogStats(ctx)
	}
}
