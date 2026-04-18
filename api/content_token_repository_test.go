package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestContentTokenErrors_WrapDBErrors(t *testing.T) {
	assert.True(t, errors.Is(ErrContentTokenNotFound, dberrors.ErrNotFound))
	assert.True(t, errors.Is(ErrContentTokenDuplicate, dberrors.ErrDuplicate))
}

// newInMemoryTestDB opens a GORM SQLite in-memory database and auto-migrates
// the UserContentToken model (without its foreign-key referent, since SQLite
// does not enforce foreign keys by default).
func newInMemoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	// Migrate only the table under test; SQLite won't enforce the FK to users.
	require.NoError(t, db.AutoMigrate(&models.UserContentToken{}))
	return db
}

// newTestContentTokenRepo wires up an in-memory SQLite repo for unit tests.
func newTestContentTokenRepo(t *testing.T) (ContentTokenRepository, *ContentTokenEncryptor, *gorm.DB) {
	t.Helper()
	db := newInMemoryTestDB(t)
	enc := mustNewTestEncryptor(t)
	return NewGormContentTokenRepository(db, enc), enc, db
}

func TestContentTokenRepo_UpsertThenGet(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()

	tok := &ContentToken{
		UserID:       "user-1",
		ProviderID:   "mock",
		AccessToken:  "at-1",
		RefreshToken: "rt-1",
		Scopes:       "read",
		Status:       ContentTokenStatusActive,
	}
	require.NoError(t, repo.Upsert(ctx, tok))

	got, err := repo.GetByUserAndProvider(ctx, "user-1", "mock")
	require.NoError(t, err)
	assert.Equal(t, "at-1", got.AccessToken)
	assert.Equal(t, "rt-1", got.RefreshToken)
	assert.Equal(t, ContentTokenStatusActive, got.Status)
}

func TestContentTokenRepo_Get_NotFound(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	_, err := repo.GetByUserAndProvider(context.Background(), "missing", "mock")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_Upsert_IsIdempotent(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "v1", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))
	tok.AccessToken = "v2"
	require.NoError(t, repo.Upsert(ctx, tok))
	got, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	assert.Equal(t, "v2", got.AccessToken)
}

func TestContentTokenRepo_ListByUser(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "a", AccessToken: "x", Status: "active"}))
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "b", AccessToken: "y", Status: "active"}))
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u2", ProviderID: "a", AccessToken: "z", Status: "active"}))

	list, err := repo.ListByUser(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, list, 2)
}

func TestContentTokenRepo_DeleteByUserAndProvider_ReturnsRowThenGone(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "x", Status: "active"}))

	deleted, err := repo.DeleteByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	require.NotNil(t, deleted)
	assert.Equal(t, "x", deleted.AccessToken)

	_, err = repo.GetByUserAndProvider(ctx, "u1", "p")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_DeleteByUserAndProvider_NotFoundError(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	_, err := repo.DeleteByUserAndProvider(context.Background(), "nope", "nope")
	assert.True(t, errors.Is(err, ErrContentTokenNotFound))
}

func TestContentTokenRepo_UpdateStatus(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "x", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))

	// Fetch to get the generated ID.
	fetched, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)

	require.NoError(t, repo.UpdateStatus(ctx, fetched.ID, ContentTokenStatusFailedRefresh, "invalid_grant"))
	got, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	assert.Equal(t, ContentTokenStatusFailedRefresh, got.Status)
	assert.Equal(t, "invalid_grant", got.LastError)
}

func TestContentTokenRepo_RefreshWithLock_UpdatesToken(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()
	tok := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "old", RefreshToken: "rt", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, tok))
	fetched, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)

	updated, err := repo.RefreshWithLock(ctx, fetched.ID, func(current *ContentToken) (*ContentToken, error) {
		current.AccessToken = "new"
		now := time.Now().Add(3600 * time.Second)
		current.ExpiresAt = &now
		return current, nil
	})
	require.NoError(t, err)
	assert.Equal(t, "new", updated.AccessToken)
	got, err := repo.GetByUserAndProvider(ctx, "u1", "p")
	require.NoError(t, err)
	assert.Equal(t, "new", got.AccessToken)
}
