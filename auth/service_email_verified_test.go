package auth

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/auth/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingUserRepo captures the last user passed to Update so tests can
// assert what would be persisted.
type recordingUserRepo struct {
	stubUserRepo
	updated []repository.User
}

func (r *recordingUserRepo) Update(_ context.Context, u *repository.User) error {
	r.updated = append(r.updated, *u)
	return nil
}

func TestGenerateTokensWithUserInfo_EmailVerifiedOneWay(t *testing.T) {
	t.Run("synthesized_saml_email_cannot_downgrade", func(t *testing.T) {
		repo := &recordingUserRepo{}
		svc, cleanup := setupTestServiceWithRepos(t, repo, nil)
		defer cleanup()

		user := User{
			InternalUUID:   "u1",
			Email:          "alice@example.com",
			ProviderUserID: "saml-sub-1",
			EmailVerified:  true,
		}
		info := &UserInfo{IdP: "saml-test", EmailVerified: false} // synthesized email path

		pair, err := svc.GenerateTokensWithAuthTime(context.Background(), user, info, time.Now())
		require.NoError(t, err)
		require.NotEmpty(t, pair.AccessToken)

		require.NotEmpty(t, repo.updated, "token mint should persist provider data")
		assert.True(t, repo.updated[len(repo.updated)-1].EmailVerified,
			"stored EmailVerified=true must never be downgraded by an unverified provider payload")
	})

	t.Run("verified_provider_payload_upgrades", func(t *testing.T) {
		repo := &recordingUserRepo{}
		svc, cleanup := setupTestServiceWithRepos(t, repo, nil)
		defer cleanup()

		user := User{
			InternalUUID:   "u2",
			Email:          "bob@example.com",
			ProviderUserID: "test-sub-2",
			EmailVerified:  false,
		}
		info := &UserInfo{IdP: "test", EmailVerified: true}

		_, err := svc.GenerateTokensWithAuthTime(context.Background(), user, info, time.Now())
		require.NoError(t, err)
		require.NotEmpty(t, repo.updated)
		assert.True(t, repo.updated[len(repo.updated)-1].EmailVerified,
			"EmailVerified must transition false→true")
	})
}
