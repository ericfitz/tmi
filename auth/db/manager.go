package db

import (
	"context"
	"fmt"
	"sync"
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
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.postgres != nil {
		return fmt.Errorf("postgres connection already initialized")
	}

	db, err := NewPostgresDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize postgres: %w", err)
	}

	m.postgres = db
	return nil
}

// InitRedis initializes the Redis connection
func (m *Manager) InitRedis(cfg RedisConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.redis != nil {
		return fmt.Errorf("redis connection already initialized")
	}

	db, err := NewRedisDB(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize redis: %w", err)
	}

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
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.postgres != nil {
		if err := m.postgres.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close postgres: %w", err))
		}
	}

	if m.redis != nil {
		if err := m.redis.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close redis: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing database connections: %v", errs)
	}

	return nil
}

// Ping checks if all database connections are alive
func (m *Manager) Ping(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	if m.postgres != nil {
		if err := m.postgres.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("postgres ping failed: %w", err))
		}
	}

	if m.redis != nil {
		if err := m.redis.Ping(ctx); err != nil {
			errs = append(errs, fmt.Errorf("redis ping failed: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("ping errors: %v", errs)
	}

	return nil
}
