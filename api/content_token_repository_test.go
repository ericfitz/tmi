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

// TestContentTokenRepo_Upsert_ReturnsStoredIDOnUpdate guards against #705: on
// the ON CONFLICT UPDATE branch, the struct passed to Upsert must come back
// with the ID of the row actually persisted, not a fresh client-side
// BeforeCreate UUID that was never inserted (Oracle's MERGE has no
// RETURNING, so trusting the struct's PK silently hands out a bogus ID).
func TestContentTokenRepo_Upsert_ReturnsStoredIDOnUpdate(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()

	first := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "at-1", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, first))
	require.NotEmpty(t, first.ID)

	second := &ContentToken{UserID: "u1", ProviderID: "p", AccessToken: "at-2", Status: "active"}
	require.NoError(t, repo.Upsert(ctx, second))

	assert.Equal(t, first.ID, second.ID, "upsert on the UPDATE branch must return the stored row's ID")

	list, err := repo.ListByUser(ctx, "u1")
	require.NoError(t, err)
	assert.Len(t, list, 1, "upsert on the UPDATE branch must not create a second row")
}

// TestContentTokenRepo_Upsert_EmptyProviderIDFailsClosed guards the #705
// read-back guard: GORM's struct-based Where used for the post-upsert
// read-back omits zero-value fields, so an empty ProviderID would otherwise
// drop that predicate and the re-SELECT could match an arbitrary row for the
// same user -- handing back another token's stored ID. It must instead fail
// closed with ErrContentTokenInvalidKey.
func TestContentTokenRepo_Upsert_EmptyProviderIDFailsClosed(t *testing.T) {
	repo, _, _ := newTestContentTokenRepo(t)
	ctx := context.Background()

	// A real token for the same user that an unguarded re-SELECT could
	// wrongly match once the (empty) provider_id predicate is dropped.
	require.NoError(t, repo.Upsert(ctx, &ContentToken{UserID: "u1", ProviderID: "other-provider", AccessToken: "x", Status: "active"}))

	bogus := &ContentToken{UserID: "u1", ProviderID: "", AccessToken: "y", Status: "active"}
	err := repo.Upsert(ctx, bogus)
	require.Error(t, err, "an empty ProviderID must not be silently matched to an unrelated row")
	assert.True(t, errors.Is(err, ErrContentTokenInvalidKey), "got: %v", err)
	assert.Empty(t, bogus.ID, "must not stamp an ID from a mismatched row")
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
