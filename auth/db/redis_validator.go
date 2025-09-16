package db

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
)

// RedisKeyPattern defines a pattern for Redis keys
type RedisKeyPattern struct {
	Pattern     string
	Regex       *regexp.Regexp
	DataType    string
	MaxTTL      time.Duration
	Description string
}

// RedisKeyValidator validates Redis keys against defined patterns
type RedisKeyValidator struct {
	patterns map[string]RedisKeyPattern
	logger   *slogging.Logger
}

// NewRedisKeyValidator creates a new Redis key validator
func NewRedisKeyValidator() *RedisKeyValidator {
	validator := &RedisKeyValidator{
		patterns: make(map[string]RedisKeyPattern),
		logger:   slogging.Get(),
	}
	validator.initializePatterns()
	return validator
}

// initializePatterns sets up all the key patterns
func (v *RedisKeyValidator) initializePatterns() {
	// Session keys
	v.addPattern("session", RedisKeyPattern{
		Pattern:     "session:{user_id}:{session_id}",
		Regex:       regexp.MustCompile(`^session:[a-f0-9-]+:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      24 * time.Hour,
		Description: "User session data",
	})

	// Auth token keys
	v.addPattern("auth_token", RedisKeyPattern{
		Pattern:     "auth:token:{token_id}",
		Regex:       regexp.MustCompile(`^auth:token:[a-zA-Z0-9._-]+$`),
		DataType:    "string",
		MaxTTL:      24 * time.Hour,
		Description: "JWT token cache",
	})

	// Refresh token keys
	v.addPattern("auth_refresh", RedisKeyPattern{
		Pattern:     "auth:refresh:{refresh_token_id}",
		Regex:       regexp.MustCompile(`^auth:refresh:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      30 * 24 * time.Hour,
		Description: "Refresh token data",
	})

	// OAuth state keys
	v.addPattern("auth_state", RedisKeyPattern{
		Pattern:     "auth:state:{state}",
		Regex:       regexp.MustCompile(`^auth:state:[a-zA-Z0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      10 * time.Minute,
		Description: "OAuth state data",
	})

	// Token blacklist keys
	v.addPattern("blacklist_token", RedisKeyPattern{
		Pattern:     "blacklist:token:{jti}",
		Regex:       regexp.MustCompile(`^blacklist:token:[a-zA-Z0-9._-]+$`),
		DataType:    "string",
		MaxTTL:      24 * time.Hour,
		Description: "Revoked JWT tokens",
	})

	// Rate limiting keys
	v.addPattern("rate_limit_global", RedisKeyPattern{
		Pattern:     "rate_limit:global:{ip}:{endpoint}",
		Regex:       regexp.MustCompile(`^rate_limit:global:[0-9a-fA-F.:]+:[a-zA-Z0-9/_-]+$`),
		DataType:    "string",
		MaxTTL:      1 * time.Minute,
		Description: "Global rate limiting per IP/endpoint",
	})

	v.addPattern("rate_limit_user", RedisKeyPattern{
		Pattern:     "rate_limit:user:{user_id}:{action}",
		Regex:       regexp.MustCompile(`^rate_limit:user:[a-f0-9-]+:[a-zA-Z0-9_-]+$`),
		DataType:    "string",
		MaxTTL:      1 * time.Minute,
		Description: "User-specific rate limiting",
	})

	v.addPattern("rate_limit_api", RedisKeyPattern{
		Pattern:     "rate_limit:api:{api_key}:{endpoint}",
		Regex:       regexp.MustCompile(`^rate_limit:api:[a-zA-Z0-9-]+:[a-zA-Z0-9/_-]+$`),
		DataType:    "string",
		MaxTTL:      1 * time.Hour,
		Description: "API key rate limiting",
	})

	// Cache keys
	v.addPattern("cache_user", RedisKeyPattern{
		Pattern:     "cache:user:{user_id}",
		Regex:       regexp.MustCompile(`^cache:user:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      5 * time.Minute,
		Description: "User profile cache",
	})

	v.addPattern("cache_threat_model", RedisKeyPattern{
		Pattern:     "cache:threat_model:{model_id}",
		Regex:       regexp.MustCompile(`^cache:threat_model:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      10 * time.Minute,
		Description: "Threat model cache",
	})

	v.addPattern("cache_diagram", RedisKeyPattern{
		Pattern:     "cache:diagram:{diagram_id}",
		Regex:       regexp.MustCompile(`^cache:diagram:[a-f0-9-]+$`),
		DataType:    "string",
		MaxTTL:      10 * time.Minute,
		Description: "Diagram data cache",
	})

	// Temporary operation keys
	v.addPattern("temp_export", RedisKeyPattern{
		Pattern:     "temp:export:{job_id}",
		Regex:       regexp.MustCompile(`^temp:export:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      1 * time.Hour,
		Description: "Export job status",
	})

	v.addPattern("temp_import", RedisKeyPattern{
		Pattern:     "temp:import:{job_id}",
		Regex:       regexp.MustCompile(`^temp:import:[a-f0-9-]+$`),
		DataType:    "hash",
		MaxTTL:      1 * time.Hour,
		Description: "Import job status",
	})

	v.addPattern("lock", RedisKeyPattern{
		Pattern:     "lock:{resource}:{id}",
		Regex:       regexp.MustCompile(`^lock:[a-zA-Z0-9_-]+:[a-f0-9-]+$`),
		DataType:    "string",
		MaxTTL:      30 * time.Second,
		Description: "Distributed locks",
	})
}

// addPattern adds a pattern to the validator
func (v *RedisKeyValidator) addPattern(name string, pattern RedisKeyPattern) {
	v.patterns[name] = pattern
}

// ValidateKey validates a Redis key against defined patterns
func (v *RedisKeyValidator) ValidateKey(key string) error {
	for _, pattern := range v.patterns {
		if pattern.Regex.MatchString(key) {
			return nil
		}
	}
	return fmt.Errorf("key '%s' does not match any valid pattern", key)
}

// GetPatternForKey returns the pattern that matches the given key
func (v *RedisKeyValidator) GetPatternForKey(key string) (*RedisKeyPattern, error) {
	for _, pattern := range v.patterns {
		if pattern.Regex.MatchString(key) {
			return &pattern, nil
		}
	}
	return nil, fmt.Errorf("no pattern found for key '%s'", key)
}

// ValidateKeyWithTTL validates a key and checks if TTL is within limits
func (v *RedisKeyValidator) ValidateKeyWithTTL(ctx context.Context, client *redis.Client, key string) error {
	pattern, err := v.GetPatternForKey(key)
	if err != nil {
		return err
	}

	// Check TTL
	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get TTL for key '%s': %w", key, err)
	}

	// -1 means no TTL, -2 means key doesn't exist
	if ttl == -1 {
		return fmt.Errorf("key '%s' has no TTL set (pattern: %s requires max TTL of %v)",
			key, pattern.Pattern, pattern.MaxTTL)
	}

	if ttl > 0 && ttl > pattern.MaxTTL {
		return fmt.Errorf("key '%s' has TTL of %v, exceeds max TTL of %v for pattern %s",
			key, ttl, pattern.MaxTTL, pattern.Pattern)
	}

	return nil
}

// ValidateDataType validates that a key has the expected data type
func (v *RedisKeyValidator) ValidateDataType(ctx context.Context, client *redis.Client, key string) error {
	pattern, err := v.GetPatternForKey(key)
	if err != nil {
		return err
	}

	keyType, err := client.Type(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to get type for key '%s': %w", key, err)
	}

	if keyType != pattern.DataType {
		return fmt.Errorf("key '%s' has type '%s', expected '%s' for pattern %s",
			key, keyType, pattern.DataType, pattern.Pattern)
	}

	return nil
}

// ScanAndValidate scans all keys and validates them
func (v *RedisKeyValidator) ScanAndValidate(ctx context.Context, client *redis.Client) ([]ValidationResult, error) {
	var results []ValidationResult
	var cursor uint64

	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, "*", 100).Result()
		if err != nil {
			return results, fmt.Errorf("failed to scan keys: %w", err)
		}

		for _, key := range keys {
			result := v.validateSingleKey(ctx, client, key)
			results = append(results, result)
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return results, nil
}

// validateSingleKey validates a single key
func (v *RedisKeyValidator) validateSingleKey(ctx context.Context, client *redis.Client, key string) ValidationResult {
	result := ValidationResult{
		Key:    key,
		Valid:  true,
		Errors: []string{},
	}

	// Validate key pattern
	if err := v.ValidateKey(key); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
		return result // Can't continue if pattern is invalid
	}

	// Validate data type
	if err := v.ValidateDataType(ctx, client, key); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}

	// Validate TTL
	if err := v.ValidateKeyWithTTL(ctx, client, key); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}

	return result
}

// ValidationResult represents the result of validating a Redis key
type ValidationResult struct {
	Key    string
	Valid  bool
	Errors []string
}

// LogValidationResults logs the validation results
func (v *RedisKeyValidator) LogValidationResults(results []ValidationResult) {
	validCount := 0
	invalidCount := 0

	for _, result := range results {
		if result.Valid {
			validCount++
			v.logger.Debug("Key '%s' is valid", result.Key)
		} else {
			invalidCount++
			v.logger.Error("Key '%s' validation failed: %s", result.Key, strings.Join(result.Errors, "; "))
		}
	}

	v.logger.Info("Redis key validation completed: %d valid, %d invalid out of %d total keys",
		validCount, invalidCount, len(results))
}

// GetPatternDocumentation returns documentation for all patterns
func (v *RedisKeyValidator) GetPatternDocumentation() []PatternDoc {
	var docs []PatternDoc
	for name, pattern := range v.patterns {
		docs = append(docs, PatternDoc{
			Name:        name,
			Pattern:     pattern.Pattern,
			DataType:    pattern.DataType,
			MaxTTL:      pattern.MaxTTL,
			Description: pattern.Description,
		})
	}
	return docs
}

// PatternDoc represents documentation for a key pattern
type PatternDoc struct {
	Name        string
	Pattern     string
	DataType    string
	MaxTTL      time.Duration
	Description string
}
