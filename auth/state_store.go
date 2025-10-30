package auth

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// StateStore is an interface for storing and retrieving state information
type StateStore interface {
	// StoreState stores state with associated data and expiration
	StoreState(ctx context.Context, state, data string, ttl time.Duration) error
	// ValidateState checks if state is valid and returns associated data
	ValidateState(ctx context.Context, state string) (string, error)
	// GetCallbackURL retrieves the callback URL associated with a state
	GetCallbackURL(ctx context.Context, state string) (string, error)
	// StoreCallbackURL stores a callback URL with a state
	StoreCallbackURL(ctx context.Context, state, callbackURL string, ttl time.Duration) error
	// DeleteState removes state from store
	DeleteState(ctx context.Context, state string) error
}

// stateEntry represents a stored state entry
type stateEntry struct {
	Data        string
	CallbackURL string
	ExpiresAt   time.Time
}

// InMemoryStateStore implements StateStore using in-memory storage
type InMemoryStateStore struct {
	mu      sync.RWMutex
	states  map[string]*stateEntry
	cleanup *time.Ticker
	done    chan bool
}

// NewInMemoryStateStore creates a new in-memory state store
func NewInMemoryStateStore() *InMemoryStateStore {
	store := &InMemoryStateStore{
		states:  make(map[string]*stateEntry),
		cleanup: time.NewTicker(1 * time.Minute),
		done:    make(chan bool),
	}

	// Start cleanup goroutine
	go store.cleanupExpired()

	return store
}

// StoreState stores state with associated data
func (s *InMemoryStateStore) StoreState(ctx context.Context, state, data string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.states[state] = &stateEntry{
		Data:      data,
		ExpiresAt: time.Now().Add(ttl),
	}

	return nil
}

// ValidateState validates state and returns associated data
func (s *InMemoryStateStore) ValidateState(ctx context.Context, state string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.states[state]
	if !exists {
		return "", fmt.Errorf("state not found")
	}

	if time.Now().After(entry.ExpiresAt) {
		return "", fmt.Errorf("state expired")
	}

	return entry.Data, nil
}

// GetCallbackURL retrieves the callback URL for a state
func (s *InMemoryStateStore) GetCallbackURL(ctx context.Context, state string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.states[state]
	if !exists {
		return "", fmt.Errorf("state not found")
	}

	if time.Now().After(entry.ExpiresAt) {
		return "", fmt.Errorf("state expired")
	}

	return entry.CallbackURL, nil
}

// StoreCallbackURL stores a callback URL with a state
func (s *InMemoryStateStore) StoreCallbackURL(ctx context.Context, state, callbackURL string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.states[state]
	if !exists {
		entry = &stateEntry{
			ExpiresAt: time.Now().Add(ttl),
		}
		s.states[state] = entry
	}

	entry.CallbackURL = callbackURL

	return nil
}

// DeleteState removes a state from the store
func (s *InMemoryStateStore) DeleteState(ctx context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, state)
	return nil
}

// cleanupExpired removes expired states periodically
func (s *InMemoryStateStore) cleanupExpired() {
	for {
		select {
		case <-s.cleanup.C:
			s.mu.Lock()
			now := time.Now()
			for state, entry := range s.states {
				if now.After(entry.ExpiresAt) {
					delete(s.states, state)
				}
			}
			s.mu.Unlock()
		case <-s.done:
			s.cleanup.Stop()
			return
		}
	}
}

// Close stops the cleanup goroutine
func (s *InMemoryStateStore) Close() {
	close(s.done)
}
