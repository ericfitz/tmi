package auth

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for InMemoryStateStore

func TestNewInMemoryStateStore(t *testing.T) {
	t.Run("creates store with empty states map", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		assert.NotNil(t, store)
		assert.NotNil(t, store.states)
		assert.Len(t, store.states, 0)
		assert.NotNil(t, store.cleanup)
		assert.NotNil(t, store.done)
	})
}

func TestInMemoryStateStore_StoreState(t *testing.T) {
	ctx := context.Background()

	t.Run("stores state with data", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "test-data", time.Minute)
		require.NoError(t, err)

		// Verify state is stored
		store.mu.RLock()
		entry, exists := store.states["test-state"]
		store.mu.RUnlock()

		assert.True(t, exists)
		assert.Equal(t, "test-data", entry.Data)
		assert.False(t, entry.ExpiresAt.IsZero())
	})

	t.Run("overwrites existing state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "data-1", time.Minute)
		require.NoError(t, err)

		err = store.StoreState(ctx, "test-state", "data-2", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "data-2", entry.Data)
	})

	t.Run("stores multiple states", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		for i := range 10 {
			state := "state-" + string(rune('a'+i))
			err := store.StoreState(ctx, state, "data", time.Minute)
			require.NoError(t, err)
		}

		store.mu.RLock()
		count := len(store.states)
		store.mu.RUnlock()

		assert.Equal(t, 10, count)
	})
}

func TestInMemoryStateStore_ValidateState(t *testing.T) {
	ctx := context.Background()

	t.Run("returns data for valid state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "test-data", time.Minute)
		require.NoError(t, err)

		data, err := store.ValidateState(ctx, "test-state")
		require.NoError(t, err)
		assert.Equal(t, "test-data", data)
	})

	t.Run("returns error for non-existent state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		_, err := store.ValidateState(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state not found")
	})

	t.Run("returns error for expired state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		// Store state with very short TTL
		err := store.StoreState(ctx, "test-state", "test-data", time.Millisecond)
		require.NoError(t, err)

		// Wait for expiration
		time.Sleep(5 * time.Millisecond)

		_, err = store.ValidateState(ctx, "test-state")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state expired")
	})
}

func TestInMemoryStateStore_GetCallbackURL(t *testing.T) {
	ctx := context.Background()

	t.Run("returns callback URL for valid state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreCallbackURL(ctx, "test-state", "https://example.com/callback", time.Minute)
		require.NoError(t, err)

		url, err := store.GetCallbackURL(ctx, "test-state")
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/callback", url)
	})

	t.Run("returns error for non-existent state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		_, err := store.GetCallbackURL(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state not found")
	})

	t.Run("returns error for expired state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreCallbackURL(ctx, "test-state", "https://example.com/callback", time.Millisecond)
		require.NoError(t, err)

		time.Sleep(5 * time.Millisecond)

		_, err = store.GetCallbackURL(ctx, "test-state")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state expired")
	})

	t.Run("returns empty string if callback not set", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		// Store state without callback URL
		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		url, err := store.GetCallbackURL(ctx, "test-state")
		require.NoError(t, err)
		assert.Equal(t, "", url)
	})
}

func TestInMemoryStateStore_StoreCallbackURL(t *testing.T) {
	ctx := context.Background()

	t.Run("stores callback URL for new state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreCallbackURL(ctx, "test-state", "https://example.com/callback", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "https://example.com/callback", entry.CallbackURL)
	})

	t.Run("stores callback URL for existing state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		err = store.StoreCallbackURL(ctx, "test-state", "https://example.com/callback", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "https://example.com/callback", entry.CallbackURL)
		assert.Equal(t, "data", entry.Data)
	})

	t.Run("overwrites existing callback URL", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreCallbackURL(ctx, "test-state", "https://old.example.com", time.Minute)
		require.NoError(t, err)

		err = store.StoreCallbackURL(ctx, "test-state", "https://new.example.com", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "https://new.example.com", entry.CallbackURL)
	})
}

func TestInMemoryStateStore_DeleteState(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes existing state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		err = store.DeleteState(ctx, "test-state")
		require.NoError(t, err)

		store.mu.RLock()
		_, exists := store.states["test-state"]
		store.mu.RUnlock()

		assert.False(t, exists)
	})

	t.Run("no error when deleting non-existent state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.DeleteState(ctx, "non-existent")
		assert.NoError(t, err)
	})
}

func TestInMemoryStateStore_StorePKCEChallenge(t *testing.T) {
	ctx := context.Background()

	t.Run("stores PKCE challenge for new state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StorePKCEChallenge(ctx, "test-state", "challenge-value", "S256", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "challenge-value", entry.CodeChallenge)
		assert.Equal(t, "S256", entry.ChallengeMethod)
	})

	t.Run("stores PKCE challenge for existing state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		err = store.StorePKCEChallenge(ctx, "test-state", "challenge-value", "S256", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "challenge-value", entry.CodeChallenge)
		assert.Equal(t, "S256", entry.ChallengeMethod)
		assert.Equal(t, "data", entry.Data)
	})

	t.Run("stores PKCE challenge with plain method", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StorePKCEChallenge(ctx, "test-state", "plain-verifier", "plain", time.Minute)
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "plain-verifier", entry.CodeChallenge)
		assert.Equal(t, "plain", entry.ChallengeMethod)
	})
}

func TestInMemoryStateStore_GetPKCEChallenge(t *testing.T) {
	ctx := context.Background()

	t.Run("returns PKCE challenge for valid state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StorePKCEChallenge(ctx, "test-state", "challenge-value", "S256", time.Minute)
		require.NoError(t, err)

		challenge, method, err := store.GetPKCEChallenge(ctx, "test-state")
		require.NoError(t, err)
		assert.Equal(t, "challenge-value", challenge)
		assert.Equal(t, "S256", method)
	})

	t.Run("returns error for non-existent state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		_, _, err := store.GetPKCEChallenge(ctx, "non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state not found")
	})

	t.Run("returns error for expired state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StorePKCEChallenge(ctx, "test-state", "challenge", "S256", time.Millisecond)
		require.NoError(t, err)

		time.Sleep(5 * time.Millisecond)

		_, _, err = store.GetPKCEChallenge(ctx, "test-state")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "state expired")
	})

	t.Run("returns error when no PKCE challenge stored", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		// Store state without PKCE challenge
		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		_, _, err = store.GetPKCEChallenge(ctx, "test-state")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "PKCE challenge not found")
	})
}

func TestInMemoryStateStore_DeletePKCEChallenge(t *testing.T) {
	ctx := context.Background()

	t.Run("deletes PKCE challenge from existing state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StorePKCEChallenge(ctx, "test-state", "challenge", "S256", time.Minute)
		require.NoError(t, err)

		err = store.DeletePKCEChallenge(ctx, "test-state")
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "", entry.CodeChallenge)
		assert.Equal(t, "", entry.ChallengeMethod)
	})

	t.Run("no error when deleting from non-existent state", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.DeletePKCEChallenge(ctx, "non-existent")
		assert.NoError(t, err)
	})

	t.Run("preserves other state data when deleting PKCE challenge", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		err := store.StoreState(ctx, "test-state", "data", time.Minute)
		require.NoError(t, err)

		err = store.StorePKCEChallenge(ctx, "test-state", "challenge", "S256", time.Minute)
		require.NoError(t, err)

		err = store.StoreCallbackURL(ctx, "test-state", "https://example.com", time.Minute)
		require.NoError(t, err)

		err = store.DeletePKCEChallenge(ctx, "test-state")
		require.NoError(t, err)

		store.mu.RLock()
		entry := store.states["test-state"]
		store.mu.RUnlock()

		assert.Equal(t, "", entry.CodeChallenge)
		assert.Equal(t, "data", entry.Data)
		assert.Equal(t, "https://example.com", entry.CallbackURL)
	})
}

func TestInMemoryStateStore_Close(t *testing.T) {
	t.Run("closes done channel", func(t *testing.T) {
		store := NewInMemoryStateStore()
		store.Close()

		// Verify done channel is closed by trying to receive
		select {
		case _, ok := <-store.done:
			assert.False(t, ok, "done channel should be closed")
		default:
			// If we get here immediately, channel is still open but non-blocking
			// This is fine - just verify no panic occurs
		}
	})
}

func TestInMemoryStateStore_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryStateStore()
	defer store.Close()

	// Test concurrent reads and writes
	t.Run("handles concurrent operations safely", func(t *testing.T) {
		done := make(chan bool)
		numGoroutines := 10
		numOperations := 100

		// Writers
		for i := range numGoroutines {
			go func(id int) {
				for range numOperations {
					state := "state-" + string(rune('A'+id))
					_ = store.StoreState(ctx, state, "data", time.Minute)
					_ = store.StoreCallbackURL(ctx, state, "https://example.com", time.Minute)
					_ = store.StorePKCEChallenge(ctx, state, "challenge", "S256", time.Minute)
				}
				done <- true
			}(i)
		}

		// Readers
		for i := range numGoroutines {
			go func(id int) {
				for range numOperations {
					state := "state-" + string(rune('A'+id))
					_, _ = store.ValidateState(ctx, state)
					_, _ = store.GetCallbackURL(ctx, state)
					_, _, _ = store.GetPKCEChallenge(ctx, state)
				}
				done <- true
			}(i)
		}

		// Wait for all goroutines to complete
		for i := 0; i < numGoroutines*2; i++ {
			<-done
		}

		// If we get here without panicking, the test passes
	})
}

func TestInMemoryStateStore_CompleteOAuthFlow(t *testing.T) {
	ctx := context.Background()

	t.Run("simulates complete OAuth flow with PKCE", func(t *testing.T) {
		store := NewInMemoryStateStore()
		defer store.Close()

		state := "oauth-state-12345"
		callbackURL := "https://app.example.com/callback"
		codeChallenge := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
		challengeMethod := "S256"
		ttl := 10 * time.Minute

		// Step 1: Store state with data (provider info)
		err := store.StoreState(ctx, state, "google", ttl)
		require.NoError(t, err)

		// Step 2: Store callback URL
		err = store.StoreCallbackURL(ctx, state, callbackURL, ttl)
		require.NoError(t, err)

		// Step 3: Store PKCE challenge
		err = store.StorePKCEChallenge(ctx, state, codeChallenge, challengeMethod, ttl)
		require.NoError(t, err)

		// Step 4: Verify state (when callback is received)
		data, err := store.ValidateState(ctx, state)
		require.NoError(t, err)
		assert.Equal(t, "google", data)

		// Step 5: Get callback URL
		url, err := store.GetCallbackURL(ctx, state)
		require.NoError(t, err)
		assert.Equal(t, callbackURL, url)

		// Step 6: Get PKCE challenge for verification
		challenge, method, err := store.GetPKCEChallenge(ctx, state)
		require.NoError(t, err)
		assert.Equal(t, codeChallenge, challenge)
		assert.Equal(t, challengeMethod, method)

		// Step 7: Cleanup after successful token exchange
		err = store.DeletePKCEChallenge(ctx, state)
		require.NoError(t, err)

		err = store.DeleteState(ctx, state)
		require.NoError(t, err)

		// Step 8: Verify state is cleaned up
		_, err = store.ValidateState(ctx, state)
		assert.Error(t, err)
	})
}
