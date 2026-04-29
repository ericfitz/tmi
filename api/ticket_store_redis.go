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

type ticketData struct {
	UserID       string `json:"user_id"`
	Provider     string `json:"provider"`
	InternalUUID string `json:"internal_uuid,omitempty"`
	SessionID    string `json:"session_id"`
}

// RedisTicketStore implements TicketStore using Redis with atomic GETDEL for single-use semantics.
type RedisTicketStore struct {
	redis *db.RedisDB
}

// NewRedisTicketStore creates a new Redis-backed ticket store.
func NewRedisTicketStore(redis *db.RedisDB) *RedisTicketStore {
	return &RedisTicketStore{redis: redis}
}

func (s *RedisTicketStore) ticketKey(ticket string) string {
	return fmt.Sprintf("ws_ticket:%s", ticket)
}

// IssueTicket creates a cryptographically random ticket and stores it in Redis with the given TTL.
func (s *RedisTicketStore) IssueTicket(ctx context.Context, userID, provider, internalUUID, sessionID string, ttl time.Duration) (string, error) {
	logger := slogging.Get()

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	data, err := json.Marshal(ticketData{
		UserID:       userID,
		Provider:     provider,
		InternalUUID: internalUUID,
		SessionID:    sessionID,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal ticket data: %w", err)
	}

	if err := s.redis.Set(ctx, s.ticketKey(ticket), string(data), ttl); err != nil {
		logger.Error("Failed to store ticket in Redis: %v", err)
		return "", fmt.Errorf("failed to store ticket: %w", err)
	}

	return ticket, nil
}

// ValidateTicket atomically retrieves and deletes a ticket from Redis (single-use). Returns the bound userID, provider, internalUUID, and sessionID.
func (s *RedisTicketStore) ValidateTicket(ctx context.Context, ticket string) (string, string, string, string, error) {
	logger := slogging.Get()
	key := s.ticketKey(ticket)

	// Atomic get-and-delete to prevent race conditions (Redis 6.2+)
	result, err := s.redis.GetClient().GetDel(ctx, key).Result()
	if err != nil {
		logger.Debug("Ticket validation failed (not found or expired): %v", err)
		return "", "", "", "", ErrTicketNotFound
	}

	var data ticketData
	if err := json.Unmarshal([]byte(result), &data); err != nil {
		logger.Error("Failed to unmarshal ticket data: %v", err)
		return "", "", "", "", fmt.Errorf("invalid ticket data")
	}

	return data.UserID, data.Provider, data.InternalUUID, data.SessionID, nil
}
