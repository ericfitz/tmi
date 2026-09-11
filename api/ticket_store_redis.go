package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/slogging"
)

// RedisTicketStore implements TicketStore using Redis with atomic GETDEL for single-use semantics.
// SEM@7118d848c0cc54f6062c586bb5adde9c5aa9ae4f: Redis-backed store implementing single-use WebSocket upgrade tickets (pure)
type RedisTicketStore struct {
	redis *db.RedisDB
}

// NewRedisTicketStore creates a new Redis-backed ticket store.
// SEM@7118d848c0cc54f6062c586bb5adde9c5aa9ae4f: build a RedisTicketStore from a Redis connection (pure)
func NewRedisTicketStore(redis *db.RedisDB) *RedisTicketStore {
	return &RedisTicketStore{redis: redis}
}

// SEM@7118d848c0cc54f6062c586bb5adde9c5aa9ae4f: compute the Redis key for a WebSocket upgrade ticket (pure)
func (s *RedisTicketStore) ticketKey(ticket string) string {
	return fmt.Sprintf("ws_ticket:%s", ticket)
}

// IssueTicket creates a cryptographically random ticket and stores it in Redis with the given TTL.
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: generate a cryptographically random upgrade ticket and store it in Redis with a TTL (mutates shared state)
func (s *RedisTicketStore) IssueTicket(ctx context.Context, claims TicketClaims, ttl time.Duration) (string, error) {
	logger := slogging.Get()

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	data, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("failed to marshal ticket data: %w", err)
	}

	if err := s.redis.Set(ctx, s.ticketKey(ticket), string(data), ttl); err != nil {
		logger.Error("Failed to store ticket in Redis: %v", err)
		return "", fmt.Errorf("failed to store ticket: %w", err)
	}

	return ticket, nil
}

// ValidateTicket atomically retrieves and deletes a ticket from Redis (single-use), returning its bound claims.
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: atomically consume and validate a single-use upgrade ticket from Redis (mutates shared state)
func (s *RedisTicketStore) ValidateTicket(ctx context.Context, ticket string) (TicketClaims, error) {
	logger := slogging.Get()
	key := s.ticketKey(ticket)

	// Atomic get-and-delete to prevent race conditions (Redis 6.2+)
	result, err := s.redis.GetClient().GetDel(ctx, key).Result()
	if err != nil {
		logger.Debug("Ticket validation failed (not found or expired): %v", err)
		return TicketClaims{}, ErrTicketNotFound
	}

	var claims TicketClaims
	if err := json.Unmarshal([]byte(result), &claims); err != nil {
		logger.Error("Failed to unmarshal ticket data: %v", err)
		return TicketClaims{}, fmt.Errorf("invalid ticket data")
	}

	return claims, nil
}
