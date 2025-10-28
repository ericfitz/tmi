package db

import (
	"fmt"
	"strings"
)

// RedisKeyBuilder provides methods to build Redis keys following the defined patterns
type RedisKeyBuilder struct{}

// NewRedisKeyBuilder creates a new Redis key builder
func NewRedisKeyBuilder() *RedisKeyBuilder {
	return &RedisKeyBuilder{}
}

// Session keys

// SessionKey builds a session key
func (b *RedisKeyBuilder) SessionKey(userID, sessionID string) string {
	return fmt.Sprintf("session:%s:%s", userID, sessionID)
}

// AuthTokenKey builds an auth token key
func (b *RedisKeyBuilder) AuthTokenKey(tokenID string) string {
	return fmt.Sprintf("auth:token:%s", tokenID)
}

// AuthRefreshKey builds an auth refresh token key
func (b *RedisKeyBuilder) AuthRefreshKey(refreshTokenID string) string {
	return fmt.Sprintf("auth:refresh:%s", refreshTokenID)
}

// AuthStateKey builds an OAuth state key
func (b *RedisKeyBuilder) AuthStateKey(state string) string {
	return fmt.Sprintf("auth:state:%s", state)
}

// BlacklistTokenKey builds a token blacklist key
func (b *RedisKeyBuilder) BlacklistTokenKey(jti string) string {
	return fmt.Sprintf("blacklist:token:%s", jti)
}

// Rate limiting keys

// RateLimitGlobalKey builds a global rate limit key
func (b *RedisKeyBuilder) RateLimitGlobalKey(ip, endpoint string) string {
	// Clean endpoint to be URL-safe
	endpoint = strings.ReplaceAll(endpoint, "/", "_")
	return fmt.Sprintf("rate_limit:global:%s:%s", ip, endpoint)
}

// RateLimitUserKey builds a user rate limit key
func (b *RedisKeyBuilder) RateLimitUserKey(userID, action string) string {
	return fmt.Sprintf("rate_limit:user:%s:%s", userID, action)
}

// RateLimitAPIKey builds an API key rate limit key
func (b *RedisKeyBuilder) RateLimitAPIKey(apiKey, endpoint string) string {
	// Clean endpoint to be URL-safe
	endpoint = strings.ReplaceAll(endpoint, "/", "_")
	return fmt.Sprintf("rate_limit:api:%s:%s", apiKey, endpoint)
}

// Cache keys

// CacheUserKey builds a user cache key
func (b *RedisKeyBuilder) CacheUserKey(userID string) string {
	return fmt.Sprintf("cache:user:%s", userID)
}

// CacheThreatModelKey builds a threat model cache key
func (b *RedisKeyBuilder) CacheThreatModelKey(modelID string) string {
	return fmt.Sprintf("cache:threat_model:%s", modelID)
}

// CacheDiagramKey builds a diagram cache key
func (b *RedisKeyBuilder) CacheDiagramKey(diagramID string) string {
	return fmt.Sprintf("cache:diagram:%s", diagramID)
}

// Sub-resource cache keys

// CacheThreatKey builds a individual threat cache key
func (b *RedisKeyBuilder) CacheThreatKey(threatID string) string {
	return fmt.Sprintf("cache:threat:%s", threatID)
}

// CacheDocumentKey builds a document cache key
func (b *RedisKeyBuilder) CacheDocumentKey(docID string) string {
	return fmt.Sprintf("cache:document:%s", docID)
}

// CacheNoteKey builds a note cache key
func (b *RedisKeyBuilder) CacheNoteKey(noteID string) string {
	return fmt.Sprintf("cache:note:%s", noteID)
}

// CacheRepositoryKey builds a source code cache key
func (b *RedisKeyBuilder) CacheRepositoryKey(sourceID string) string {
	return fmt.Sprintf("cache:repository:%s", sourceID)
}

// CacheAssetKey builds an asset cache key
func (b *RedisKeyBuilder) CacheAssetKey(assetID string) string {
	return fmt.Sprintf("cache:asset:%s", assetID)
}

// CacheMetadataKey builds a metadata collection cache key
func (b *RedisKeyBuilder) CacheMetadataKey(entityType, entityID string) string {
	return fmt.Sprintf("cache:metadata:%s:%s", entityType, entityID)
}

// CacheCellsKey builds a diagram cells collection cache key
func (b *RedisKeyBuilder) CacheCellsKey(diagramID string) string {
	return fmt.Sprintf("cache:cells:%s", diagramID)
}

// CacheAuthKey builds an authorization data cache key
func (b *RedisKeyBuilder) CacheAuthKey(threatModelID string) string {
	return fmt.Sprintf("cache:auth:%s", threatModelID)
}

// CacheListKey builds a paginated list cache key
func (b *RedisKeyBuilder) CacheListKey(entityType, parentID string, offset, limit int) string {
	return fmt.Sprintf("cache:list:%s:%s:%d:%d", entityType, parentID, offset, limit)
}

// Temporary operation keys

// TempExportKey builds a temporary export job key
func (b *RedisKeyBuilder) TempExportKey(jobID string) string {
	return fmt.Sprintf("temp:export:%s", jobID)
}

// TempImportKey builds a temporary import job key
func (b *RedisKeyBuilder) TempImportKey(jobID string) string {
	return fmt.Sprintf("temp:import:%s", jobID)
}

// LockKey builds a distributed lock key
func (b *RedisKeyBuilder) LockKey(resource, id string) string {
	return fmt.Sprintf("lock:%s:%s", resource, id)
}

// ParseSessionKey parses a session key into its components
func (b *RedisKeyBuilder) ParseSessionKey(key string) (userID, sessionID string, err error) {
	parts := strings.Split(key, ":")
	if len(parts) != 3 || parts[0] != "session" {
		return "", "", fmt.Errorf("invalid session key format: %s", key)
	}
	return parts[1], parts[2], nil
}

// ParseRateLimitKey parses a rate limit key into its components
func (b *RedisKeyBuilder) ParseRateLimitKey(key string) (limitType, identifier, action string, err error) {
	parts := strings.Split(key, ":")
	if len(parts) != 4 || parts[0] != "rate_limit" {
		return "", "", "", fmt.Errorf("invalid rate limit key format: %s", key)
	}
	return parts[1], parts[2], parts[3], nil
}
