package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/periodic"
)

// StateStore is an interface for storing and retrieving state information
// SEM@7f2e891b97d9b875349295375fb64355109504b5: interface for storing and validating OAuth state, callback URLs, and PKCE challenges
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
	// StorePKCEChallenge stores PKCE code challenge with associated method
	StorePKCEChallenge(ctx context.Context, state, codeChallenge, challengeMethod string, ttl time.Duration) error
	// GetPKCEChallenge retrieves PKCE code challenge and method for a state
	GetPKCEChallenge(ctx context.Context, state string) (challenge, method string, err error)
	// DeletePKCEChallenge removes PKCE challenge from store
	DeletePKCEChallenge(ctx context.Context, state string) error
}

// stateEntry represents a stored state entry
// SEM@7f2e891b97d9b875349295375fb64355109504b5: in-memory record holding OAuth state data, callback URL, PKCE challenge, and expiry (pure)
type stateEntry struct {
	Data            string
	CallbackURL     string
	CodeChallenge   string
	ChallengeMethod string
	ExpiresAt       time.Time
}

// InMemoryStateStore implements StateStore using in-memory storage
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: in-memory StateStore implementation with mutex protection and periodic expiry cleanup (mutates shared state)
type InMemoryStateStore struct {
	mu      sync.RWMutex
	states  map[string]*stateEntry
	cleanup *time.Ticker
	done    chan bool
}

// NewInMemoryStateStore creates a new in-memory state store
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: build and start an in-memory state store with background expiry cleanup
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: store an OAuth state token with associated data and TTL (mutates shared state)
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: validate an OAuth state token and return its associated data; reject if expired (pure)
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: fetch the callback URL associated with a valid OAuth state token (pure)
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: store a callback URL against an OAuth state token with TTL (mutates shared state)
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
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: delete an OAuth state token and all associated data (mutates shared state)
func (s *InMemoryStateStore) DeleteState(ctx context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.states, state)
	return nil
}

// cleanupExpired removes expired states periodically
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: periodically purge expired state entries from the in-memory store (mutates shared state)
func (s *InMemoryStateStore) cleanupExpired() {
	periodic.RunCleanup(s.cleanup, s.done, func() {
		s.mu.Lock()
		now := time.Now()
		for state, entry := range s.states {
			if now.After(entry.ExpiresAt) {
				delete(s.states, state)
			}
		}
		s.mu.Unlock()
	})
}

// StorePKCEChallenge stores PKCE code challenge with associated method
// SEM@7f2e891b97d9b875349295375fb64355109504b5: store a PKCE code challenge and method against an OAuth state token (mutates shared state)
func (s *InMemoryStateStore) StorePKCEChallenge(ctx context.Context, state, codeChallenge, challengeMethod string, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.states[state]
	if !exists {
		entry = &stateEntry{
			ExpiresAt: time.Now().Add(ttl),
		}
		s.states[state] = entry
	}

	entry.CodeChallenge = codeChallenge
	entry.ChallengeMethod = challengeMethod

	return nil
}

// GetPKCEChallenge retrieves PKCE code challenge and method for a state
// SEM@7f2e891b97d9b875349295375fb64355109504b5: fetch the PKCE code challenge and method for a valid OAuth state token; reject if expired (pure)
func (s *InMemoryStateStore) GetPKCEChallenge(ctx context.Context, state string) (challenge, method string, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, exists := s.states[state]
	if !exists {
		return "", "", fmt.Errorf("state not found")
	}

	if time.Now().After(entry.ExpiresAt) {
		return "", "", fmt.Errorf("state expired")
	}

	if entry.CodeChallenge == "" {
		return "", "", fmt.Errorf("PKCE challenge not found for state")
	}

	return entry.CodeChallenge, entry.ChallengeMethod, nil
}

// DeletePKCEChallenge removes PKCE challenge from store
// SEM@7f2e891b97d9b875349295375fb64355109504b5: clear the PKCE challenge from a state entry without removing the entry itself (mutates shared state)
func (s *InMemoryStateStore) DeletePKCEChallenge(ctx context.Context, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, exists := s.states[state]
	if exists {
		entry.CodeChallenge = ""
		entry.ChallengeMethod = ""
	}

	return nil
}

// Close stops the cleanup goroutine
// SEM@2fbab585a899780eb5d718ec784b7c730c732113: stop the background expiry cleanup goroutine (mutates shared state)
func (s *InMemoryStateStore) Close() {
	close(s.done)
}
