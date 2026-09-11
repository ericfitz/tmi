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
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: identity and revocation handle bound to a WebSocket upgrade ticket (pure)
type TicketClaims struct {
	UserID       string `json:"user_id"`
	Provider     string `json:"provider"`
	InternalUUID string `json:"internal_uuid,omitempty"`
	SessionID    string `json:"session_id"`
	TokenHash    string `json:"token_hash,omitempty"`
	CredentialID string `json:"credential_id,omitempty"`
}

// TicketStore manages short-lived, single-use WebSocket authentication tickets.
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: interface for issuing and consuming single-use WebSocket authentication tickets (pure)
type TicketStore interface {
	// IssueTicket creates a ticket bound to the given claims, returning the ticket string.
	IssueTicket(ctx context.Context, claims TicketClaims, ttl time.Duration) (string, error)
	// ValidateTicket validates and consumes a ticket (single-use), returning its bound claims.
	ValidateTicket(ctx context.Context, ticket string) (TicketClaims, error)
}

// SEM@722ae4c635149d53c73f2831ee3d366695967cce: in-memory record of a WebSocket authentication ticket and its expiry (pure)
type ticketEntry struct {
	TicketClaims
	ExpiresAt time.Time
}

// InMemoryTicketStore implements TicketStore using in-memory storage.
// SEM@c20da21da7db5dfa407cb89aae96e43a1e972644: in-memory TicketStore with background expiry cleanup (mutates shared state)
type InMemoryTicketStore struct {
	mu      sync.Mutex
	tickets map[string]*ticketEntry
	cleanup *time.Ticker
	done    chan bool
}

// NewInMemoryTicketStore creates a new in-memory ticket store.
// SEM@c20da21da7db5dfa407cb89aae96e43a1e972644: build an InMemoryTicketStore and start its background expiry cleanup goroutine
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
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: generate a cryptographically random single-use ticket bound to a user session (mutates shared state)
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
// SEM@722ae4c635149d53c73f2831ee3d366695967cce: consume and validate a single-use ticket, returning its bound identity claims (mutates shared state)
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

// SEM@fcd7743e746718c31b33ef56fb3ba2f8ccf669c7: periodically delete expired tickets from the in-memory store (mutates shared state)
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
// SEM@c20da21da7db5dfa407cb89aae96e43a1e972644: stop the background cleanup goroutine for the ticket store
func (s *InMemoryTicketStore) Close() {
	close(s.done)
}
