package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ownedCredRepo lists one credential for its owner so ownership checks pass.
type ownedCredRepo struct {
	stubCredRepo
	id, owner uuid.UUID
	deleted   bool
}

func (r *ownedCredRepo) ListByOwner(_ context.Context, owner uuid.UUID) ([]*repository.ClientCredential, error) {
	if owner != r.owner {
		return nil, nil
	}
	return []*repository.ClientCredential{{ID: r.id, OwnerUUID: owner}}, nil
}
func (r *ownedCredRepo) Delete(context.Context, uuid.UUID, uuid.UUID) error {
	r.deleted = true
	return nil
}

func postRevoke(t *testing.T, h *Handlers, token, bearer string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/oauth2/revoke", h.RevokeToken)
	req := httptest.NewRequest("POST", "/oauth2/revoke", strings.NewReader(url.Values{"token": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func signedTestToken(t *testing.T) string {
	t.Helper()
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "u", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
	}).SignedString([]byte("test-secret-key-for-ccg-tests"))
	require.NoError(t, err)
	return s
}

// TestRevokeTokenFailsClosedWhenStorageFails: a failed blacklist write must
// surface as 503, never 200; invalid tokens still return 200 (RFC 7009).
func TestRevokeTokenFailsClosedWhenStorageFails(t *testing.T) {
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
	defer cleanup()
	h := &Handlers{service: svc, config: svc.config}
	tok := signedTestToken(t)

	t.Run("happy path returns 200 and blacklists", func(t *testing.T) {
		w := postRevoke(t, h, tok, tok)
		assert.Equal(t, http.StatusOK, w.Code)
		tb := NewTokenBlacklist(svc.dbManager.Redis().GetClient(), svc.keyManager)
		ok, err := tb.IsTokenBlacklisted(context.Background(), tok)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("invalid token still returns 200", func(t *testing.T) {
		w := postRevoke(t, h, "not-a-jwt", tok)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("storage failure returns 503 with Retry-After", func(t *testing.T) {
		require.NoError(t, svc.dbManager.Redis().GetClient().Close()) // failing Redis double
		w := postRevoke(t, h, tok, tok)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		assert.Equal(t, "30", w.Header().Get("Retry-After"))
	})
}

// TestDeleteClientCredentialFailsClosed: revoke-before-delete; a failed
// revocation write returns ErrRevocationStorage and deletes nothing.
func TestDeleteClientCredentialFailsClosed(t *testing.T) {
	credID, owner := uuid.New(), uuid.New()
	repo := &ownedCredRepo{id: credID, owner: owner}
	svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, repo)
	defer cleanup()
	ctx := context.Background()

	t.Run("not owned returns not found and revokes nothing", func(t *testing.T) {
		err := svc.DeleteClientCredential(ctx, credID, uuid.New())
		assert.ErrorIs(t, err, repository.ErrClientCredentialNotFound)
		revoked, _ := NewTokenBlacklist(svc.dbManager.Redis().GetClient(), svc.keyManager).IsCredentialRevoked(ctx, credID.String())
		assert.False(t, revoked)
	})

	t.Run("storage failure returns ErrRevocationStorage, nothing deleted", func(t *testing.T) {
		require.NoError(t, svc.dbManager.Redis().GetClient().Close())
		err := svc.DeleteClientCredential(ctx, credID, owner)
		assert.ErrorIs(t, err, ErrRevocationStorage)
		assert.False(t, repo.deleted)
	})

	t.Run("no Redis returns ErrRevocationStorage, nothing deleted", func(t *testing.T) {
		s := &Service{credRepo: repo}
		assert.ErrorIs(t, s.DeleteClientCredential(ctx, credID, owner), ErrRevocationStorage)
		assert.False(t, repo.deleted)
	})
}
