package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientCredentialDeleteRevokesTokens pins #862: deleting or deactivating a
// client credential marks its already-issued service-account tokens revoked
// for the access-token lifetime.
func TestClientCredentialDeleteRevokesTokens(t *testing.T) {
	for name, op := range map[string]func(*Service, context.Context, uuid.UUID, uuid.UUID) error{
		"Delete":     (*Service).DeleteClientCredential,
		"Deactivate": (*Service).DeactivateClientCredential,
	} {
		t.Run(name, func(t *testing.T) {
			svc, cleanup := setupTestServiceWithRepos(t, &stubUserRepo{}, &stubCredRepo{})
			defer cleanup()
			ctx := context.Background()
			credID, ownerID := uuid.New(), uuid.New()

			require.NoError(t, op(svc, ctx, credID, ownerID))

			tb := NewTokenBlacklist(svc.dbManager.Redis().GetClient(), svc.keyManager)
			revoked, err := tb.IsCredentialRevoked(ctx, credID.String())
			require.NoError(t, err)
			assert.True(t, revoked)

			ttl, err := svc.dbManager.Redis().GetClient().TTL(ctx, "blacklist:credential:"+credID.String()).Result()
			require.NoError(t, err)
			assert.Equal(t, svc.config.GetJWTDuration(), ttl)
		})
	}
}

// TestClientCredentialDeleteWithoutRedis pins that revocation is best-effort:
// a Service without Redis still deletes the row and returns nil.
func TestClientCredentialDeleteWithoutRedis(t *testing.T) {
	svc := &Service{credRepo: &stubCredRepo{}}
	require.NoError(t, svc.DeleteClientCredential(context.Background(), uuid.New(), uuid.New()))
}
