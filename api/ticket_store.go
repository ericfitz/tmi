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

// TicketStore manages short-lived, single-use WebSocket authentication tickets.
// SEM@ab27b1c7ef336f1860c29d6f19f34f84adfc5b02: interface for issuing and consuming single-use WebSocket authentication tickets (pure)
type TicketStore interface {
	// IssueTicket creates a ticket bound to a user, provider, internal UUID, and session, returning the ticket string.
	IssueTicket(ctx context.Context, userID, provider, internalUUID, sessionID string, ttl time.Duration) (string, error)
	// ValidateTicket validates and consumes a ticket (single-use). Returns the bound userID, provider, internalUUID, and sessionID.
	ValidateTicket(ctx context.Context, ticket string) (userID, provider, internalUUID, sessionID string, err error)
}

// SEM@ab27b1c7ef336f1860c29d6f19f34f84adfc5b02: in-memory record of a WebSocket authentication ticket and its expiry (pure)
type ticketEntry struct {
	UserID       string
	Provider     string
	InternalUUID string
	SessionID    string
	ExpiresAt    time.Time
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

// IssueTicket creates a cryptographically random ticket bound to the given user, provider, internal UUID, and session.
// SEM@ab27b1c7ef336f1860c29d6f19f34f84adfc5b02: generate a cryptographically random single-use ticket bound to a user session (mutates shared state)
func (s *InMemoryTicketStore) IssueTicket(_ context.Context, userID, provider, internalUUID, sessionID string, ttl time.Duration) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickets[ticket] = &ticketEntry{
		UserID:       userID,
		Provider:     provider,
		InternalUUID: internalUUID,
		SessionID:    sessionID,
		ExpiresAt:    time.Now().Add(ttl),
	}

	return ticket, nil
}

// ValidateTicket validates and consumes a ticket. It is single-use: the ticket is deleted on first access.
// SEM@6a6c15749391c2817c30c64c8b54f8e0a4082a91: consume and validate a single-use ticket, returning its bound identity claims (mutates shared state)
func (s *InMemoryTicketStore) ValidateTicket(_ context.Context, ticket string) (string, string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tickets[ticket]
	if !exists {
		return "", "", "", "", ErrTicketNotFound
	}

	// Delete immediately (single-use)
	delete(s.tickets, ticket)

	if time.Now().After(entry.ExpiresAt) {
		return "", "", "", "", ErrTicketNotFound
	}

	return entry.UserID, entry.Provider, entry.InternalUUID, entry.SessionID, nil
}

// SEM@c20da21da7db5dfa407cb89aae96e43a1e972644: periodically purge expired ticket entries from the in-memory store (mutates shared state)
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
