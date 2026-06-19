package db

import (
	"fmt"
	"strings"
)

// RedisKeyBuilder provides methods to build Redis keys following the defined patterns
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: namespace for methods that build namespaced Redis keys for all entity types
type RedisKeyBuilder struct{}

// NewRedisKeyBuilder creates a new Redis key builder
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build a RedisKeyBuilder instance (pure)
func NewRedisKeyBuilder() *RedisKeyBuilder {
	return &RedisKeyBuilder{}
}

// Session keys

// SessionKey builds a session key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a user session (pure)
func (b *RedisKeyBuilder) SessionKey(userID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", userID, sessionID)
}

// AuthTokenKey builds an auth token key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for an auth access token (pure)
func (b *RedisKeyBuilder) AuthTokenKey(tokenID string) string {
	return fmt.Sprintf("auth:token:%s", tokenID)
}

// AuthRefreshKey builds an auth refresh token key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a refresh token (pure)
func (b *RedisKeyBuilder) AuthRefreshKey(refreshTokenID string) string {
	return fmt.Sprintf("auth:refresh:%s", refreshTokenID)
}

// AuthStateKey builds an OAuth state key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for an OAuth state parameter (pure)
func (b *RedisKeyBuilder) AuthStateKey(state string) string {
	return fmt.Sprintf("auth:state:%s", state)
}

// BlacklistTokenKey builds a token blacklist key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a blacklisted token JTI (pure)
func (b *RedisKeyBuilder) BlacklistTokenKey(jti string) string {
	return fmt.Sprintf("blacklist:token:%s", jti)
}

// Rate limiting keys

// RateLimitGlobalKey builds a global rate limit key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a global per-IP per-endpoint rate limit counter (pure)
func (b *RedisKeyBuilder) RateLimitGlobalKey(ip, endpoint string) string {
	// Clean endpoint to be URL-safe
	endpoint = strings.ReplaceAll(endpoint, "/", "_")
	return fmt.Sprintf("rate_limit:global:%s:%s", ip, endpoint)
}

// RateLimitUserKey builds a user rate limit key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a per-user per-action rate limit counter (pure)
func (b *RedisKeyBuilder) RateLimitUserKey(userID, action string) string {
	return fmt.Sprintf("rate_limit:user:%s:%s", userID, action)
}

// RateLimitAPIKey builds an API key rate limit key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a per-API-key per-endpoint rate limit counter (pure)
func (b *RedisKeyBuilder) RateLimitAPIKey(apiKey, endpoint string) string {
	// Clean endpoint to be URL-safe
	endpoint = strings.ReplaceAll(endpoint, "/", "_")
	return fmt.Sprintf("rate_limit:api:%s:%s", apiKey, endpoint)
}

// Cache keys

// CacheUserKey builds a user cache key by internal UUID
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis cache key for a user by internal UUID (pure)
func (b *RedisKeyBuilder) CacheUserKey(userID string) string {
	return fmt.Sprintf("cache:user:%s", userID)
}

// CacheUserByEmailKey builds a user cache key by email
// SEM@89d554e793900a75b5703e1d10c9d58f57ceadc6: build the Redis cache key for a user by email address (pure)
func (b *RedisKeyBuilder) CacheUserByEmailKey(email string) string {
	return fmt.Sprintf("cache:user:email:%s", email)
}

// CacheUserByProviderKey builds a user cache key by provider and provider user ID
// SEM@89d554e793900a75b5703e1d10c9d58f57ceadc6: build the Redis cache key for a user by OAuth provider and provider user ID (pure)
func (b *RedisKeyBuilder) CacheUserByProviderKey(provider, providerUserID string) string {
	return fmt.Sprintf("cache:user:provider:%s:%s", provider, providerUserID)
}

// CacheThreatModelKey builds a threat model cache key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis cache key for a threat model by ID (pure)
func (b *RedisKeyBuilder) CacheThreatModelKey(modelID string) string {
	return fmt.Sprintf("cache:threat_model:%s", modelID)
}

// CacheDiagramKey builds a diagram cache key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis cache key for a diagram by ID (pure)
func (b *RedisKeyBuilder) CacheDiagramKey(diagramID string) string {
	return fmt.Sprintf("cache:diagram:%s", diagramID)
}

// Sub-resource cache keys

// CacheThreatKey builds a individual threat cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for an individual threat by ID (pure)
func (b *RedisKeyBuilder) CacheThreatKey(threatID string) string {
	return fmt.Sprintf("cache:threat:%s", threatID)
}

// CacheDocumentKey builds a document cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for a document by ID (pure)
func (b *RedisKeyBuilder) CacheDocumentKey(docID string) string {
	return fmt.Sprintf("cache:document:%s", docID)
}

// CacheNoteKey builds a note cache key
// SEM@bc24b01d8fe51390e6178a0cbe35e701f76556ce: build the Redis cache key for a note by ID (pure)
func (b *RedisKeyBuilder) CacheNoteKey(noteID string) string {
	return fmt.Sprintf("cache:note:%s", noteID)
}

// CacheRepositoryKey builds a source code cache key
// SEM@73296f71a63ff321440eff41ab087d4043aa8f68: build the Redis cache key for a source repository by ID (pure)
func (b *RedisKeyBuilder) CacheRepositoryKey(sourceID string) string {
	return fmt.Sprintf("cache:repository:%s", sourceID)
}

// CacheAssetKey builds an asset cache key
// SEM@f2c738b899d06c4246bd8283b568260c596d5168: build the Redis cache key for an asset by ID (pure)
func (b *RedisKeyBuilder) CacheAssetKey(assetID string) string {
	return fmt.Sprintf("cache:asset:%s", assetID)
}

// CacheMetadataKey builds a metadata collection cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for a metadata collection by entity type and ID (pure)
func (b *RedisKeyBuilder) CacheMetadataKey(entityType, entityID string) string {
	return fmt.Sprintf("cache:metadata:%s:%s", entityType, entityID)
}

// CacheCellsKey builds a diagram cells collection cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for the cells collection of a diagram (pure)
func (b *RedisKeyBuilder) CacheCellsKey(diagramID string) string {
	return fmt.Sprintf("cache:cells:%s", diagramID)
}

// CacheAuthKey builds an authorization data cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for authorization data of a threat model (pure)
func (b *RedisKeyBuilder) CacheAuthKey(threatModelID string) string {
	return fmt.Sprintf("cache:auth:%s", threatModelID)
}

// CacheListKey builds a paginated list cache key
// SEM@6a25ed41f4450e7eba44de39fb07a07cac216f26: build the Redis cache key for a paginated entity list by type, parent, and page bounds (pure)
func (b *RedisKeyBuilder) CacheListKey(entityType, parentID string, offset, limit int) string {
	return fmt.Sprintf("cache:list:%s:%s:%d:%d", entityType, parentID, offset, limit)
}

// Temporary operation keys

// TempExportKey builds a temporary export job key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a temporary export job (pure)
func (b *RedisKeyBuilder) TempExportKey(jobID string) string {
	return fmt.Sprintf("temp:export:%s", jobID)
}

// TempImportKey builds a temporary import job key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a temporary import job (pure)
func (b *RedisKeyBuilder) TempImportKey(jobID string) string {
	return fmt.Sprintf("temp:import:%s", jobID)
}

// LockKey builds a distributed lock key
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: build the Redis key for a distributed lock on a named resource (pure)
func (b *RedisKeyBuilder) LockKey(resource, id string) string {
	return fmt.Sprintf("lock:%s:%s", resource, id)
}

// ParseSessionKey parses a session key into its components
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: parse a Redis session key into user ID and session ID components (pure)
func (b *RedisKeyBuilder) ParseSessionKey(key string) (userID, sessionID string, err error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != "session" {
		return "", "", fmt.Errorf("invalid session key format: %s", key)
	}
	return parts[1], parts[2], nil
}

// ParseRateLimitKey parses a rate limit key into its components
// SEM@27f75e455935db4d67b8511cf30f5f77c118fc2f: parse a Redis rate-limit key into limit type, identifier, and action components (pure)
func (b *RedisKeyBuilder) ParseRateLimitKey(key string) (limitType, identifier, action string, err error) {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[0] != "rate_limit" {
		return "", "", "", fmt.Errorf("invalid rate limit key format: %s", key)
	}
	return parts[1], parts[2], parts[3], nil
}
