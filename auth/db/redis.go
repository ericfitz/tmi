package db

import (
	"context"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisConfig holds the configuration for Redis connection
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// RedisDB represents a Redis database connection
type RedisDB struct {
	client *redis.Client
	cfg    RedisConfig
}

// NewRedisDB creates a new Redis database connection
func NewRedisDB(cfg RedisConfig) (*RedisDB, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
		MaxConnAge:   time.Hour,
		IdleTimeout:  30 * time.Minute,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}

	return &RedisDB{
		client: client,
		cfg:    cfg,
	}, nil
}

// Close closes the Redis connection
func (db *RedisDB) Close() error {
	if db.client != nil {
		return db.client.Close()
	}
	return nil
}

// GetClient returns the Redis client
func (db *RedisDB) GetClient() *redis.Client {
	return db.client
}

// Ping checks if the Redis connection is alive
func (db *RedisDB) Ping(ctx context.Context) error {
	return db.client.Ping(ctx).Err()
}

// Set sets a key-value pair with expiration
func (db *RedisDB) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return db.client.Set(ctx, key, value, expiration).Err()
}

// Get gets a value by key
func (db *RedisDB) Get(ctx context.Context, key string) (string, error) {
	return db.client.Get(ctx, key).Result()
}

// Del deletes a key
func (db *RedisDB) Del(ctx context.Context, key string) error {
	return db.client.Del(ctx, key).Err()
}

// HSet sets a hash field
func (db *RedisDB) HSet(ctx context.Context, key, field string, value interface{}) error {
	return db.client.HSet(ctx, key, field, value).Err()
}

// HGet gets a hash field
func (db *RedisDB) HGet(ctx context.Context, key, field string) (string, error) {
	return db.client.HGet(ctx, key, field).Result()
}

// HGetAll gets all fields in a hash
func (db *RedisDB) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return db.client.HGetAll(ctx, key).Result()
}

// HDel deletes a hash field
func (db *RedisDB) HDel(ctx context.Context, key string, fields ...string) error {
	return db.client.HDel(ctx, key, fields...).Err()
}

// Expire sets an expiration on a key
func (db *RedisDB) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return db.client.Expire(ctx, key, expiration).Err()
}
