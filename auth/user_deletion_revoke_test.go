package auth

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type ownerCredRepo struct {
	stubCredRepo
	owner uuid.UUID
	ids   []uuid.UUID
}

func (r *ownerCredRepo) ListByOwner(_ context.Context, owner uuid.UUID) ([]*repository.ClientCredential, error) {
	if owner != r.owner {
		return nil, nil
	}
	out := make([]*repository.ClientCredential, 0, len(r.ids))
	for _, id := range r.ids {
		out = append(out, &repository.ClientCredential{ID: id, OwnerUUID: owner})
	}
	return out, nil
}

type emailUserRepo struct {
	stubUserRepo
	user *repository.User
}

func (r *emailUserRepo) GetByEmail(context.Context, string) (*repository.User, error) {
	return r.user, nil
}

type stubDeletionRepo struct {
	repository.DeletionRepository
	calls int
}

func (r *stubDeletionRepo) DeleteUserAndData(context.Context, string) (*repository.DeletionResult, error) {
	r.calls++
	return &repository.DeletionResult{}, nil
}

func (r *stubDeletionRepo) DeleteUserByInternalUUID(context.Context, string) (*repository.DeletionResult, error) {
	r.calls++
	return &repository.DeletionResult{}, nil
}

// TestUserDeleteRevokesClientCredentialTokens pins the #862 follow-up: deleting
// a user (admin path by UUID, self-service path by email) revokes the
// service-account tokens of every client credential the user owned, and only
// those.
func TestUserDeleteRevokesClientCredentialTokens(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	mine := []uuid.UUID{uuid.New(), uuid.New()}
	theirs := uuid.New()

	for name, del := range map[string]func(*Service, context.Context) error{
		"ByInternalUUID": func(s *Service, ctx context.Context) error {
			_, err := s.DeleteUserByInternalUUID(ctx, owner.String())
			return err
		},
		"ByEmail": func(s *Service, ctx context.Context) error {
			_, err := s.DeleteUserAndData(ctx, "owner@example.com")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			svc, cleanup := setupTestServiceWithRepos(t,
				&emailUserRepo{user: &repository.User{InternalUUID: owner.String(), Email: "owner@example.com"}},
				&ownerCredRepo{owner: owner, ids: mine})
			defer cleanup()
			deletion := &stubDeletionRepo{}
			svc.deletionRepo = deletion
			ctx := context.Background()

			require.NoError(t, del(svc, ctx))
			assert.Equal(t, 1, deletion.calls)

			tb := NewTokenBlacklist(svc.dbManager.Redis().GetClient(), svc.keyManager)
			for _, id := range mine {
				revoked, err := tb.IsCredentialRevoked(ctx, id.String())
				require.NoError(t, err)
				assert.True(t, revoked, "credential %s of the deleted user must be revoked", id)
			}
			revoked, err := tb.IsCredentialRevoked(ctx, theirs.String())
			require.NoError(t, err)
			assert.False(t, revoked, "credentials of other users (%s) must be untouched", other)
		})
	}
}
