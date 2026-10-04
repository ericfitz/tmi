package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

const (
	// blacklistCheckRetries is how many times a transient Redis failure on the
	// blacklist lookup is retried before the request is failed. Kept small
	// because this runs inline on every authenticated request.
	blacklistCheckRetries = 2
	// blacklistCheckBackoff is the delay before the first retry; it doubles on
	// each subsequent attempt (20ms, then 40ms).
	blacklistCheckBackoff = 20 * time.Millisecond
)

// isRetryableRedisError reports whether a Redis failure is transient enough to
// be worth retrying. Redis answering with an application-level error (a wrong
// type, a rejected command) will answer identically next time, so only
// connectivity failures qualify.
// SEM@7383e0ea99036c9a251ff7eefa5cb784ea3829a8: classify a Redis error as transient and worth retrying (pure)
func isRetryableRedisError(err error) bool {
	if err == nil || errors.Is(err, redis.Nil) {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// go-redis wraps pool exhaustion and shutdown as plain errors.
	msg := err.Error()
	return strings.Contains(msg, "i/o timeout") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "EOF") ||
		strings.Contains(msg, "connection pool timeout")
}

// ErrRevocationStorage marks a revocation that could not be recorded because the
// backing store (Redis) is unavailable or rejected the write (e.g. at the memory
// cap under noeviction). Callers fail closed (503) instead of reporting success.
var ErrRevocationStorage = errors.New("revocation storage unavailable")

// TokenBlacklist manages blacklisted JWT tokens using Redis
// SEM@41fea1c48a3526015f75a5e401ec4970c6c9dfcf: Redis-backed store for revoked JWT tokens to prevent reuse after logout (reads DB)
type TokenBlacklist struct {
	redis      *redis.Client
	keyManager *JWTKeyManager
}

// NewTokenBlacklist creates a new token blacklist service
// SEM@70ff47b7829f38ef04399520210ae8765d39495d: build a Redis-backed token blacklist service (reads DB)
func NewTokenBlacklist(redisClient *redis.Client, keyManager *JWTKeyManager) *TokenBlacklist {
	logger := slogging.Get()
	logger.Info("Initializing token blacklist service")
	return &TokenBlacklist{
		redis:      redisClient,
		keyManager: keyManager,
	}
}

// BlacklistToken adds a JWT token to the blacklist
// SEM@9750a568b8ffb60cfd241d61263b7e23f990899e: blacklist a session token until its expiry (mutates shared state)
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
		return fmt.Errorf("%w: failed to blacklist token: %w", ErrRevocationStorage, err)
	}

	logger.Info("Token successfully blacklisted token_hash=%v ttl_seconds=%v", tokenHash[:16]+"...", int(ttl.Seconds()))
	return nil
}

// IsTokenBlacklisted checks if a JWT token is blacklisted
// SEM@c161adfd8ba839441ccd825e818d342a09c63849: validate whether a session token is blacklisted (reads cache)
func (tb *TokenBlacklist) IsTokenBlacklisted(ctx context.Context, tokenString string) (bool, error) {
	return tb.IsTokenHashBlacklisted(ctx, HashToken(tokenString))
}

// IsTokenHashBlacklisted checks the blacklist by a hash from HashToken, for
// callers that retain only the hash (WebSocket tickets, #869).
// SEM@c161adfd8ba839441ccd825e818d342a09c63849: validate whether a session token hash is blacklisted (reads cache)
func (tb *TokenBlacklist) IsTokenHashBlacklisted(ctx context.Context, tokenHash string) (bool, error) {
	logger := slogging.Get()
	key := fmt.Sprintf("blacklist:token:%s", tokenHash)
	short := tokenHash
	if len(short) > 16 {
		short = short[:16] + "..."
	}

	logger.Debug("Checking token blacklist status token_hash=%v", short)
	isBlacklisted, err := tb.keyExists(ctx, key, "token_hash="+short)
	if err != nil {
		return false, err
	}
	logger.Debug("Token blacklist check completed token_hash=%v is_blacklisted=%v", short, isBlacklisted)
	return isBlacklisted, nil
}

// RevokeCredential marks every service-account token minted from a client
// credential as revoked (#862). The server does not retain issued SA tokens,
// so they cannot be blacklisted individually; instead the credential ID is
// marked for ttl, which must be at least the access-token lifetime so that the
// last token minted before the revoke has expired by the time the marker does.
// SEM@9750a568b8ffb60cfd241d61263b7e23f990899e: revoke all tokens issued to a client credential for a TTL (mutates shared state)
func (tb *TokenBlacklist) RevokeCredential(ctx context.Context, credentialID string, ttl time.Duration) error {
	key := fmt.Sprintf("blacklist:credential:%s", credentialID)
	if err := tb.redis.Set(ctx, key, "revoked", ttl).Err(); err != nil {
		slogging.Get().Error("Failed to revoke client credential tokens credential_id=%v error=%v", credentialID, err)
		return fmt.Errorf("%w: failed to revoke credential tokens: %w", ErrRevocationStorage, err)
	}
	slogging.Get().Info("Client credential tokens revoked credential_id=%v ttl_seconds=%v", credentialID, int(ttl.Seconds()))
	return nil
}

// IsCredentialRevoked reports whether service-account tokens minted from a
// client credential have been revoked via RevokeCredential.
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: validate whether a client credential's tokens are revoked (reads cache)
func (tb *TokenBlacklist) IsCredentialRevoked(ctx context.Context, credentialID string) (bool, error) {
	return tb.keyExists(ctx, fmt.Sprintf("blacklist:credential:%s", credentialID), "credential_id="+credentialID)
}

// keyExists checks a revocation key in Redis. This sits on the authenticated
// path of every request, so a brief Redis blip would otherwise fail an
// otherwise valid request outright. Retry transient failures a bounded number
// of times before giving up; a genuine outage still surfaces, just as 503
// rather than 500 (see issue #660).
// SEM@24d835d0aa601cdfaea187838ff139f49228ea26: check a cache key exists, retrying transient failures (reads cache)
func (tb *TokenBlacklist) keyExists(ctx context.Context, key, logID string) (bool, error) {
	logger := slogging.Get()
	var exists int64
	var err error
	for attempt := 0; ; attempt++ {
		exists, err = tb.redis.Exists(ctx, key).Result()
		if err == nil {
			break
		}
		if attempt >= blacklistCheckRetries || !isRetryableRedisError(err) {
			logger.Error("Failed to check token blacklist %s attempts=%d error=%v", logID, attempt+1, err)
			return false, fmt.Errorf("failed to check token blacklist: %w", err)
		}
		logger.Warn("Transient Redis failure checking token blacklist, retrying %s attempt=%d error=%v", logID, attempt+1, err)
		select {
		case <-ctx.Done():
			return false, fmt.Errorf("failed to check token blacklist: %w", ctx.Err())
		case <-time.After(blacklistCheckBackoff << attempt):
		}
	}
	return exists > 0, nil
}

// hashToken creates a SHA-256 hash of the token for storage
// SEM@c161adfd8ba839441ccd825e818d342a09c63849: compute the SHA-256 hex digest of a session token (pure)
func (tb *TokenBlacklist) hashToken(token string) string {
	return HashToken(token)
}

// HashToken returns the blacklist key hash for a token string.
// SEM@c161adfd8ba839441ccd825e818d342a09c63849: compute the SHA-256 hex digest of a session token (pure)
func HashToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

// CleanupExpiredTokens removes expired entries from the blacklist
// This is handled automatically by Redis TTL, but this method can be used
// for monitoring or manual cleanup if needed
// SEM@70ff47b7829f38ef04399520210ae8765d39495d: no-op stub; Redis TTL handles blacklist expiry automatically (pure)
func (tb *TokenBlacklist) CleanupExpiredTokens(ctx context.Context) error {
	logger := slogging.Get()
	logger.Debug("Cleanup expired tokens called - Redis handles this automatically via TTL")
	// Redis automatically expires keys based on TTL, so no manual cleanup needed
	// This method exists for interface completeness
	return nil
}
