package db

import (
	"context"
	"time"
)

// MockRedisDB is a mock implementation of RedisDB for testing
type MockRedisDB struct{}

// NewMockRedisDB creates a new mock RedisDB
func NewMockRedisDB() *MockRedisDB {
	return &MockRedisDB{}
}

// Close is a mock implementation that does nothing
func (db *MockRedisDB) Close() error {
	return nil
}

// Ping is a mock implementation that always succeeds
func (db *MockRedisDB) Ping(ctx context.Context) error {
	return nil
}

// Set is a mock implementation that always succeeds
func (db *MockRedisDB) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	return nil
}

// Get is a mock implementation that returns an empty string
func (db *MockRedisDB) Get(ctx context.Context, key string) (string, error) {
	return "", nil
}

// Del is a mock implementation that always succeeds
func (db *MockRedisDB) Del(ctx context.Context, key string) error {
	return nil
}

// HSet is a mock implementation that always succeeds
func (db *MockRedisDB) HSet(ctx context.Context, key, field string, value any) error {
	return nil
}

// HGet is a mock implementation that returns an empty string
func (db *MockRedisDB) HGet(ctx context.Context, key, field string) (string, error) {
	return "", nil
}

// HGetAll is a mock implementation that returns an empty map
func (db *MockRedisDB) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return map[string]string{}, nil
}

// HDel is a mock implementation that always succeeds
func (db *MockRedisDB) HDel(ctx context.Context, key string, fields ...string) error {
	return nil
}

// Expire is a mock implementation that always succeeds
func (db *MockRedisDB) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return nil
}

// MockManager is a mock implementation of Manager for testing
type MockManager struct {
}

// NewMockManager creates a new mock Manager
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
