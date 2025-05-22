package db

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/logging"
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
	logger := logging.Get()
	logger.Debug("Initializing Redis connection to %s:%s DB=%d", cfg.Host, cfg.Port, cfg.DB)

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

	logger.Debug("Redis connection pool parameters: poolSize=10, minIdleConns=2, maxConnAge=1h, idleTimeout=30m")

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger.Debug("Testing Redis connection with ping")
	if err := client.Ping(ctx).Err(); err != nil {
		logger.Error("Failed to ping Redis: %v", err)
		return nil, fmt.Errorf("failed to ping redis: %w", err)
	}
	logger.Debug("Redis connection established successfully")

	return &RedisDB{
		client: client,
		cfg:    cfg,
	}, nil
}

// Close closes the Redis connection
func (db *RedisDB) Close() error {
	logger := logging.Get()
	logger.Debug("Closing Redis connection to %s:%s DB=%d", db.cfg.Host, db.cfg.Port, db.cfg.DB)

	if db.client != nil {
		err := db.client.Close()
		if err != nil {
			logger.Error("Error closing Redis connection: %v", err)
		} else {
			logger.Debug("Redis connection closed successfully")
		}
		return err
	}
	return nil
}

// GetClient returns the Redis client
func (db *RedisDB) GetClient() *redis.Client {
	return db.client
}

// Ping checks if the Redis connection is alive
func (db *RedisDB) Ping(ctx context.Context) error {
	logger := logging.Get()
	logger.Debug("Pinging Redis connection to %s:%s DB=%d", db.cfg.Host, db.cfg.Port, db.cfg.DB)

	err := db.client.Ping(ctx).Err()
	if err != nil {
		logger.Error("Redis ping failed: %v", err)
	} else {
		logger.Debug("Redis ping successful")
	}
	return err
}

// LogStats logs statistics about the Redis connection
func (db *RedisDB) LogStats(ctx context.Context) {
	logger := logging.Get()

	// Get pool stats
	poolStats := db.client.PoolStats()
	logger.Debug("Redis connection pool stats: hits=%d, misses=%d, timeouts=%d, totalConns=%d, idleConns=%d, staleConns=%d",
		poolStats.Hits,
		poolStats.Misses,
		poolStats.Timeouts,
		poolStats.TotalConns,
		poolStats.IdleConns,
		poolStats.StaleConns,
	)
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
