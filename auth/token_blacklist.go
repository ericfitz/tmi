package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/golang-jwt/jwt/v5"
)

// TokenBlacklist manages blacklisted JWT tokens using Redis
type TokenBlacklist struct {
	redis     *redis.Client
	jwtSecret []byte
}

// NewTokenBlacklist creates a new token blacklist service
func NewTokenBlacklist(redisClient *redis.Client, jwtSecret []byte) *TokenBlacklist {
	return &TokenBlacklist{
		redis:     redisClient,
		jwtSecret: jwtSecret,
	}
}

// BlacklistToken adds a JWT token to the blacklist
func (tb *TokenBlacklist) BlacklistToken(ctx context.Context, tokenString string) error {
	// Parse the token with signature verification to get expiration time
	token, err := jwt.ParseWithClaims(tokenString, jwt.MapClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signing method
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return tb.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return fmt.Errorf("failed to parse or validate token: %w", err)
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return fmt.Errorf("invalid token claims")
	}

	// Get expiration time
	exp, ok := claims["exp"].(float64)
	if !ok {
		return fmt.Errorf("token missing expiration")
	}

	expTime := time.Unix(int64(exp), 0)
	if expTime.Before(time.Now()) {
		// Token is already expired, no need to blacklist
		return nil
	}

	// Calculate TTL (time until token expires)
	ttl := time.Until(expTime)
	if ttl <= 0 {
		// Token is expired, no need to blacklist
		return nil
	}

	// Create a hash of the token for the Redis key
	tokenHash := tb.hashToken(tokenString)
	key := fmt.Sprintf("blacklist:token:%s", tokenHash)

	// Store in Redis with TTL matching token expiration
	err = tb.redis.Set(ctx, key, "blacklisted", ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to blacklist token: %w", err)
	}

	return nil
}

// IsTokenBlacklisted checks if a JWT token is blacklisted
func (tb *TokenBlacklist) IsTokenBlacklisted(ctx context.Context, tokenString string) (bool, error) {
	tokenHash := tb.hashToken(tokenString)
	key := fmt.Sprintf("blacklist:token:%s", tokenHash)

	// Check if key exists in Redis
	exists, err := tb.redis.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check token blacklist: %w", err)
	}

	return exists > 0, nil
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
	// Redis automatically expires keys based on TTL, so no manual cleanup needed
	// This method exists for interface completeness
	return nil
}
