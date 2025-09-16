package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
)

// TokenBlacklist manages blacklisted JWT tokens using Redis
type TokenBlacklist struct {
	redis      *redis.Client
	keyManager *JWTKeyManager
}

// NewTokenBlacklist creates a new token blacklist service
func NewTokenBlacklist(redisClient *redis.Client, keyManager *JWTKeyManager) *TokenBlacklist {
	logger := slogging.Get()
	logger.Info("Initializing token blacklist service")
	return &TokenBlacklist{
		redis:      redisClient,
		keyManager: keyManager,
	}
}

// BlacklistToken adds a JWT token to the blacklist
func (tb *TokenBlacklist) BlacklistToken(ctx context.Context, tokenString string) error {
	logger := slogging.Get()
	logger.Debug("Attempting to blacklist token")

	// Parse the token with signature verification to get expiration time
	claims := jwt.MapClaims{}
	token, err := tb.keyManager.VerifyToken(tokenString, claims)
	if err != nil || !token.Valid {
		logger.Error("Failed to parse or validate token for blacklisting error=%v", err)
		return fmt.Errorf("failed to parse or validate token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		logger.Error("Invalid token claims type")
		return fmt.Errorf("invalid token claims")
	}

	// Get expiration time
	exp, ok := claims["exp"].(float64)
	if !ok {
		logger.Error("Token missing expiration claim")
		return fmt.Errorf("token missing expiration")
	}

	expTime := time.Unix(int64(exp), 0)
	if expTime.Before(time.Now()) {
		// Token is already expired, no need to blacklist
		logger.Debug("Token already expired, skipping blacklist expiration_time=%v", expTime)
		return nil
	}

	// Calculate TTL (time until token expires)
	ttl := time.Until(expTime)
	if ttl <= 0 {
		// Token is expired, no need to blacklist
		logger.Debug("Token TTL expired, skipping blacklist ttl=%v", ttl)
		return nil
	}

	// Create a hash of the token for the Redis key
	tokenHash := tb.hashToken(tokenString)
	key := fmt.Sprintf("blacklist:token:%s", tokenHash)

	logger.Debug("Storing token in blacklist token_hash=%v ttl_seconds=%v", tokenHash[:16]+"...", int(ttl.Seconds()))

	// Store in Redis with TTL matching token expiration
	err = tb.redis.Set(ctx, key, "blacklisted", ttl).Err()
	if err != nil {
		logger.Error("Failed to store token in blacklist token_hash=%v error=%v", tokenHash[:16]+"...", err)
		return fmt.Errorf("failed to blacklist token: %w", err)
	}

	logger.Info("Token successfully blacklisted token_hash=%v ttl_seconds=%v", tokenHash[:16]+"...", int(ttl.Seconds()))
	return nil
}

// IsTokenBlacklisted checks if a JWT token is blacklisted
func (tb *TokenBlacklist) IsTokenBlacklisted(ctx context.Context, tokenString string) (bool, error) {
	logger := slogging.Get()
	tokenHash := tb.hashToken(tokenString)
	key := fmt.Sprintf("blacklist:token:%s", tokenHash)

	logger.Debug("Checking token blacklist status token_hash=%v", tokenHash[:16]+"...")

	// Check if key exists in Redis
	exists, err := tb.redis.Exists(ctx, key).Result()
	if err != nil {
		logger.Error("Failed to check token blacklist token_hash=%v error=%v", tokenHash[:16]+"...", err)
		return false, fmt.Errorf("failed to check token blacklist: %w", err)
	}

	isBlacklisted := exists > 0
	logger.Debug("Token blacklist check completed token_hash=%v is_blacklisted=%v", tokenHash[:16]+"...", isBlacklisted)
	return isBlacklisted, nil
}

// hashToken creates a SHA-256 hash of the token for storage
func (tb *TokenBlacklist) hashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// CleanupExpiredTokens removes expired entries from the blacklist
// This is handled automatically by Redis TTL, but this method can be used
// for monitoring or manual cleanup if needed
func (tb *TokenBlacklist) CleanupExpiredTokens(ctx context.Context) error {
	logger := slogging.Get()
	logger.Debug("Cleanup expired tokens called - Redis handles this automatically via TTL")
	// Redis automatically expires keys based on TTL, so no manual cleanup needed
	// This method exists for interface completeness
	return nil
}
