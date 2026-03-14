package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

// TicketStore manages short-lived, single-use WebSocket authentication tickets.
type TicketStore interface {
	// IssueTicket creates a ticket bound to a user, provider, and session, returning the ticket string.
	IssueTicket(ctx context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error)
	// ValidateTicket validates and consumes a ticket (single-use). Returns the bound userID, provider, and sessionID.
	ValidateTicket(ctx context.Context, ticket string) (userID, provider, sessionID string, err error)
}

type ticketEntry struct {
	UserID    string
	Provider  string
	SessionID string
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

// IssueTicket creates a cryptographically random ticket bound to the given user, provider, and session.
func (s *InMemoryTicketStore) IssueTicket(_ context.Context, userID, provider, sessionID string, ttl time.Duration) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate ticket: %w", err)
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.tickets[ticket] = &ticketEntry{
		UserID:    userID,
		Provider:  provider,
		SessionID: sessionID,
		ExpiresAt: time.Now().Add(ttl),
	}

	return ticket, nil
}

// ValidateTicket validates and consumes a ticket. It is single-use: the ticket is deleted on first access.
func (s *InMemoryTicketStore) ValidateTicket(_ context.Context, ticket string) (string, string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.tickets[ticket]
	if !exists {
		return "", "", "", fmt.Errorf("ticket not found")
	}

	// Delete immediately (single-use)
	delete(s.tickets, ticket)

	if time.Now().After(entry.ExpiresAt) {
		return "", "", "", fmt.Errorf("ticket expired")
	}

	return entry.UserID, entry.Provider, entry.SessionID, nil
}

func (s *InMemoryTicketStore) cleanupExpired() {
	for {
		select {
		case <-s.cleanup.C:
			s.mu.Lock()
			now := time.Now()
			for ticket, entry := range s.tickets {
				if now.After(entry.ExpiresAt) {
					delete(s.tickets, ticket)
				}
			}
			s.mu.Unlock()
		case <-s.done:
			s.cleanup.Stop()
			return
		}
	}
}

// Close stops the cleanup goroutine.
func (s *InMemoryTicketStore) Close() {
	close(s.done)
}
