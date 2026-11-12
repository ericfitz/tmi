package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/periodic"
)

// TicketClaims is the identity bound to a WebSocket ticket. TokenHash and
// CredentialID identify the token that minted the ticket so the upgrade can be
// refused after that token or its client credential is revoked (#869).
type TicketClaims struct {
	UserID       string `json:"user_id"`
	Provider     string `json:"provider"`
	InternalUUID string `json:"internal_uuid,omitempty"`
	SessionID    string `json:"session_id"`
	TokenHash    string `json:"token_hash,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
	// Groups are the minting token's group claims, frozen at issue time so the
	// ticket-authenticated upgrade can make group-aware access checks. Absent
	// (nil) on tickets issued before this field existed, which fails closed.
	Groups []string `json:"groups,omitempty"`
}

// TicketStore manages short-lived, single-use WebSocket authentication tickets.
type TicketStore interface {
	// IssueTicket creates a ticket bound to the given claims, returning the ticket string.
	IssueTicket(ctx context.Context, claims TicketClaims, ttl time.Duration) (string, error)
	// ValidateTicket validates and consumes a ticket (single-use), returning its bound claims.
	ValidateTicket(ctx context.Context, ticket string) (TicketClaims, error)
}

type ticketEntry struct {
	TicketClaims
	ExpiresAt time.Time
}

// InMemoryTicketStore implements TicketStore using in-memory storage.
type InMemoryTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*ticketEntry
	cleanup *time.Ticker
	done    chan bool
}

// NewInMemoryTicketStore creates a new in-memory ticket store.
func NewInMemoryTicketStore() *InMemoryTicketStore {
	store := &InMemoryTicketStore{
		tickets: make(map[string]*ticketEntry),
		cleanup: time.NewTicker(30 * time.Second),
		done:    make(chan bool),
	}
	go store.cleanupExpired()
	return store
}

// IssueTicket creates a cryptographically random ticket bound to the given claims.
func (s *InMemoryTicketStore) IssueTicket(_ context.Context, claims TicketClaims, ttl time.Duration) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickets[ticket] = &ticketEntry{TicketClaims: claims, ExpiresAt: time.Now().Add(ttl)}

	return ticket, nil
}

// ValidateTicket validates and consumes a ticket. It is single-use: the ticket is deleted on first access.
func (s *InMemoryTicketStore) ValidateTicket(_ context.Context, ticket string) (TicketClaims, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tickets[ticket]
	if !exists {
		return TicketClaims{}, ErrTicketNotFound
	}

	// Delete immediately (single-use)
	delete(s.tickets, ticket)

	if time.Now().After(entry.ExpiresAt) {
		return TicketClaims{}, ErrTicketNotFound
	}

	return entry.TicketClaims, nil
}

func (s *InMemoryTicketStore) cleanupExpired() {
	periodic.RunCleanup(s.cleanup, s.done, func() {
		s.mu.Lock()
		now := time.Now()
		for ticket, entry := range s.tickets {
			if now.After(entry.ExpiresAt) {
				delete(s.tickets, ticket)
			}
		}
		s.mu.Unlock()
	})
}

// Close stops the cleanup goroutine.
func (s *InMemoryTicketStore) Close() {
	close(s.done)
}
