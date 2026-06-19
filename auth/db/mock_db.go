package db

import (
	"context"
	"time"
)

// MockRedisDB is a mock implementation of RedisDB for testing
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: no-op RedisDB stub for tests that require a Redis interface (pure)
type MockRedisDB struct{}

// NewMockRedisDB creates a new mock RedisDB
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: build a no-op MockRedisDB instance for use in tests (pure)
func NewMockRedisDB() *MockRedisDB {
	return &MockRedisDB{}
}

// Close is a mock implementation that does nothing
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: satisfy the RedisDB Close interface with a no-op (pure)
func (db *MockRedisDB) Close() error {
	return nil
}

// Ping is a mock implementation that always succeeds
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: satisfy the RedisDB Ping interface, always succeeding (pure)
func (db *MockRedisDB) Ping(ctx context.Context) error {
	return nil
}

// Set is a mock implementation that always succeeds
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: satisfy the RedisDB Set interface with a no-op (pure)
func (db *MockRedisDB) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return nil
}

// Get is a mock implementation that returns an empty string
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB Get interface, returning an empty string (pure)
func (db *MockRedisDB) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

// Del is a mock implementation that always succeeds
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB Del interface with a no-op (pure)
func (db *MockRedisDB) Del(ctx context.Context, key string) error {
	return nil
}

// HSet is a mock implementation that always succeeds
// SEM@3d0d5a8cf02fa74fad102f0f99c2b936a164bbea: satisfy the RedisDB HSet interface with a no-op (pure)
func (db *MockRedisDB) HSet(ctx context.Context, key, field string, value any) error {
	return nil
}

// HGet is a mock implementation that returns an empty string
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB HGet interface, returning an empty string (pure)
func (db *MockRedisDB) HGet(ctx context.Context, key, field string) (string, error) {
	return "", nil
}

// HGetAll is a mock implementation that returns an empty map
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB HGetAll interface, returning an empty map (pure)
func (db *MockRedisDB) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return map[string]string{}, nil
}

// HDel is a mock implementation that always succeeds
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB HDel interface with a no-op (pure)
func (db *MockRedisDB) HDel(ctx context.Context, key string, fields ...string) error {
	return nil
}

// Expire is a mock implementation that always succeeds
// SEM@0e6a42f2959c3aaa51e638008b4fe60d8e43ab23: satisfy the RedisDB Expire interface with a no-op (pure)
func (db *MockRedisDB) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return nil
}

// MockManager is a mock implementation of Manager for testing
// SEM@78f72a392d13a20f1af466d8da33c878dc7b1518: stub DB Manager for tests that need a Manager without real connections (pure)
type MockManager struct {
}

// NewMockManager creates a new mock Manager
// SEM@b4b216a8ad19c2ca17d1d9e7466281e90c7b2f41: build a Manager wired to a nil-client Redis stub for use in tests (pure)
func NewMockManager() *Manager {
	manager := NewManager()

	// Create a mock Redis implementation
	manager.redis = &RedisDB{
		client: nil,
		cfg: RedisConfig{
			Host:     "localhost",
			Port:     "6379",
			Password: "",
			DB:       0,
		},
	}

	return manager
}
