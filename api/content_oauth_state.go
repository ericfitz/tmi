package api

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrContentOAuthStateNotFound is returned when the OAuth state nonce is not found or has expired.
var ErrContentOAuthStateNotFound = errors.New("content oauth state not found or expired")

// ContentOAuthStatePayload holds the data associated with a pending OAuth authorization flow.
// SEM@0ee4fc6b79c5a6c5a7dade501546a8eec4509aed: data held in a pending content OAuth authorization state nonce (pure)
type ContentOAuthStatePayload struct {
	UserID           string    `json:"user_id"`
	ProviderID       string    `json:"provider_id"`
	ClientCallback   string    `json:"client_callback"`
	PKCECodeVerifier string    `json:"pkce_code_verifier"`
	CreatedAt        time.Time `json:"created_at"`
}

// ContentOAuthStateStore stores short-lived OAuth state nonces in Redis.
// SEM@0ee4fc6b79c5a6c5a7dade501546a8eec4509aed: Redis-backed store for short-lived content OAuth state nonces
type ContentOAuthStateStore struct {
	rdb       redis.UniversalClient
	keyPrefix string
}

// NewContentOAuthStateStore creates a new ContentOAuthStateStore backed by the given Redis client.
// SEM@0ee4fc6b79c5a6c5a7dade501546a8eec4509aed: build a ContentOAuthStateStore backed by the given Redis client (pure)
func NewContentOAuthStateStore(rdb redis.UniversalClient) *ContentOAuthStateStore {
	return &ContentOAuthStateStore{rdb: rdb, keyPrefix: "content_oauth_state:"}
}

// Put stores the payload under a freshly generated nonce and returns the nonce.
// The entry expires after ttl.
// SEM@0ee4fc6b79c5a6c5a7dade501546a8eec4509aed: store an OAuth state payload under a new random nonce with a TTL; return the nonce (reads DB)
func (s *ContentOAuthStateStore) Put(ctx context.Context, p ContentOAuthStatePayload, ttl time.Duration) (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(buf[:])
	payload, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	if err := s.rdb.Set(ctx, s.keyPrefix+nonce, payload, ttl).Err(); err != nil {
		return "", fmt.Errorf("put state: %w", err)
	}
	return nonce, nil
}

// Consume retrieves and atomically deletes the payload for the given nonce.
// Returns ErrContentOAuthStateNotFound if the nonce does not exist or has expired.
// SEM@0ee4fc6b79c5a6c5a7dade501546a8eec4509aed: atomically fetch and delete an OAuth state payload by nonce; error if absent or expired (reads DB)
func (s *ContentOAuthStateStore) Consume(ctx context.Context, nonce string) (*ContentOAuthStatePayload, error) {
	key := s.keyPrefix + nonce
	val, err := s.rdb.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrContentOAuthStateNotFound
	}
	if err != nil {
		return nil, err
	}
	var out ContentOAuthStatePayload
	if err := json.Unmarshal(val, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
