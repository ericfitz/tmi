package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/crypto"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// RedisConfig holds the configuration for Redis connection
type RedisConfig struct {
	Host     string
	Port     string
	Password string //nolint:gosec // G117 - Redis connection password
	DB       int
}

// RedisDB represents a Redis database connection
type RedisDB struct {
	client    *redis.Client
	cfg       RedisConfig
	encryptor *crypto.SettingsEncryptor // nil = no encryption
}

// sensitiveKeyPrefixes lists Redis key prefixes whose values must be encrypted at rest.
// Keys matching these prefixes contain PII (emails, names), secrets (tokens), or
// other sensitive data that should not be readable if Redis is accidentally exposed.
var sensitiveKeyPrefixes = []string{
	"cache:user:",              // User structs: email, name, OAuth tokens
	"refresh_token:",           // Refresh token -> user UUID mapping (30-day TTL)
	"user_groups:",             // Email, IdP, group membership
	"tmi:settings:",            // Decrypted system settings from DB
	"oauth_state:",             // OAuth state data (provider, callback, login hint)
	"pkce:",                    // PKCE challenge/verifier
	"user_deletion_challenge:", // Account deletion tokens
	"session:",                 // Session data with user context
	"auth:state:",              // OAuth state (registered key pattern)
	"auth:refresh:",            // Refresh token data (registered key pattern)
}

// shouldEncrypt returns true if the given Redis key matches a sensitive pattern
// whose value should be encrypted at rest.
func shouldEncrypt(key string) bool {
	for _, prefix := range sensitiveKeyPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// SetEncryptor sets the encryptor for at-rest encryption of sensitive Redis values.
// When set and enabled, values for keys matching sensitive patterns are encrypted
// before writing and decrypted after reading. If nil or disabled, values pass through unchanged.
func (db *RedisDB) SetEncryptor(enc *crypto.SettingsEncryptor) {
	db.encryptor = enc
}

// NewRedisDB creates a new Redis database connection
func NewRedisDB(cfg RedisConfig) (*RedisDB, error) {
	logger := slogging.Get()
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
	logger := slogging.Get()
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
	logger := slogging.Get()
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
	logger := slogging.Get()

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

// Set sets a key-value pair with expiration.
// If an encryptor is configured and the key matches a sensitive pattern,
// the value is encrypted before writing to Redis.
func (db *RedisDB) Set(ctx context.Context, key string, value any, expiration time.Duration) error {
	if db.encryptor != nil && db.encryptor.IsEnabled() && shouldEncrypt(key) {
		strValue := fmt.Sprintf("%v", value)
		encrypted, err := db.encryptor.Encrypt(strValue)
		if err != nil {
			if errors.Is(err, crypto.ErrValueTooLong) {
				logger := slogging.Get()
				logger.Warn("Redis value too long to encrypt for key %s (storing unencrypted): %v", key, err)
			} else {
				return fmt.Errorf("failed to encrypt Redis value for key %s: %w", key, err)
			}
		} else {
			value = encrypted
		}
	}
	return db.client.Set(ctx, key, value, expiration).Err()
}

// Get gets a value by key.
// If an encryptor is configured and the stored value has the ENC: prefix,
// it is decrypted before being returned. Plaintext values pass through unchanged.
func (db *RedisDB) Get(ctx context.Context, key string) (string, error) {
	result, err := db.client.Get(ctx, key).Result()
	if err != nil {
		return result, err
	}

	if db.encryptor != nil && crypto.IsEncrypted(result) {
		decrypted, decErr := db.encryptor.Decrypt(result)
		if decErr != nil {
			return "", fmt.Errorf("failed to decrypt Redis value for key %s: %w", key, decErr)
		}
		return decrypted, nil
	}

	return result, nil
}

// Del deletes a key
func (db *RedisDB) Del(ctx context.Context, key string) error {
	return db.client.Del(ctx, key).Err()
}

// HSet sets a hash field.
// If an encryptor is configured and the key matches a sensitive pattern,
// the field value is encrypted before writing to Redis.
func (db *RedisDB) HSet(ctx context.Context, key, field string, value any) error {
	if db.encryptor != nil && db.encryptor.IsEnabled() && shouldEncrypt(key) {
		strValue := fmt.Sprintf("%v", value)
		encrypted, err := db.encryptor.Encrypt(strValue)
		if err != nil {
			if errors.Is(err, crypto.ErrValueTooLong) {
				logger := slogging.Get()
				logger.Warn("Redis hash value too long to encrypt for key %s field %s (storing unencrypted): %v", key, field, err)
			} else {
				return fmt.Errorf("failed to encrypt Redis hash value for key %s field %s: %w", key, field, err)
			}
		} else {
			value = encrypted
		}
	}
	return db.client.HSet(ctx, key, field, value).Err()
}

// HGet gets a hash field.
// If an encryptor is configured and the stored value has the ENC: prefix,
// it is decrypted before being returned. Plaintext values pass through unchanged.
func (db *RedisDB) HGet(ctx context.Context, key, field string) (string, error) {
	result, err := db.client.HGet(ctx, key, field).Result()
	if err != nil {
		return result, err
	}

	if db.encryptor != nil && crypto.IsEncrypted(result) {
		decrypted, decErr := db.encryptor.Decrypt(result)
		if decErr != nil {
			return "", fmt.Errorf("failed to decrypt Redis hash value for key %s field %s: %w", key, field, decErr)
		}
		return decrypted, nil
	}

	return result, nil
}

// HGetAll gets all fields in a hash.
// If an encryptor is configured, any field values with the ENC: prefix
// are decrypted before being returned. Plaintext values pass through unchanged.
func (db *RedisDB) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	result, err := db.client.HGetAll(ctx, key).Result()
	if err != nil {
		return result, err
	}

	if db.encryptor != nil {
		for field, value := range result {
			if crypto.IsEncrypted(value) {
				decrypted, decErr := db.encryptor.Decrypt(value)
				if decErr != nil {
					return nil, fmt.Errorf("failed to decrypt Redis hash value for key %s field %s: %w", key, field, decErr)
				}
				result[field] = decrypted
			}
		}
	}

	return result, nil
}

// HDel deletes a hash field
func (db *RedisDB) HDel(ctx context.Context, key string, fields ...string) error {
	return db.client.HDel(ctx, key, fields...).Err()
}

// Expire sets an expiration on a key
func (db *RedisDB) Expire(ctx context.Context, key string, expiration time.Duration) error {
	return db.client.Expire(ctx, key, expiration).Err()
}
