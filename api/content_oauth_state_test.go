package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStateStoreWithServer creates a miniredis instance and a ContentOAuthStateStore for testing.
// The miniredis instance is returned so callers can use FastForward etc.
func newTestStateStoreWithServer(t *testing.T) (*miniredis.Miniredis, *ContentOAuthStateStore) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, NewContentOAuthStateStore(client)
}

// newTestStateStore creates a ContentOAuthStateStore backed by miniredis for testing.
func newTestStateStore(t *testing.T) *ContentOAuthStateStore {
	t.Helper()
	_, store := newTestStateStoreWithServer(t)
	return store
}

func TestContentOAuthStateStore_PutAndConsume(t *testing.T) {
	store := newTestStateStore(t)
	p := ContentOAuthStatePayload{
		UserID:           "u",
		ProviderID:       "mock",
		ClientCallback:   "http://c",
		PKCECodeVerifier: "v",
		CreatedAt:        time.Now(),
	}
	nonce, err := store.Put(context.Background(), p, 10*time.Minute)
	require.NoError(t, err)
	assert.Len(t, nonce, 43) // base64url(32 bytes) without padding

	got, err := store.Consume(context.Background(), nonce)
	require.NoError(t, err)
	assert.Equal(t, "u", got.UserID)
	assert.Equal(t, "mock", got.ProviderID)
	assert.Equal(t, "http://c", got.ClientCallback)
	assert.Equal(t, "v", got.PKCECodeVerifier)

	// Consume is single-use
	_, err = store.Consume(context.Background(), nonce)
	assert.True(t, errors.Is(err, ErrContentOAuthStateNotFound))
}

func TestContentOAuthStateStore_ExpiredEntry(t *testing.T) {
	mr, store := newTestStateStoreWithServer(t)
	nonce, err := store.Put(context.Background(), ContentOAuthStatePayload{UserID: "u"}, 1*time.Second)
	require.NoError(t, err)
	mr.FastForward(2 * time.Second)
	_, err = store.Consume(context.Background(), nonce)
	assert.True(t, errors.Is(err, ErrContentOAuthStateNotFound))
}
