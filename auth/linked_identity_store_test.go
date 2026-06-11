package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// linkedIdentityTestStrPtr is a local helper that returns a pointer to a string.
func linkedIdentityTestStrPtr(s string) *string { return &s }

// setupLinkedIdentityTestDB opens an in-memory SQLite database and migrates
// the tables needed for LinkedIdentityStore tests.
func setupLinkedIdentityTestDB(t *testing.T) (*gorm.DB, *models.User, *models.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&models.User{},
		&models.LinkedIdentity{},
	))

	alice := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(linkedIdentityTestStrPtr("alice-provider-id")),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(alice).Error)

	bob := &models.User{
		InternalUUID:   models.DBVarchar(uuid.New().String()),
		Provider:       "test",
		ProviderUserID: models.NewNullableDBVarchar(linkedIdentityTestStrPtr("bob-provider-id")),
		Email:          models.DBVarchar("bob@example.com"),
		Name:           models.DBVarchar("Bob"),
	}
	require.NoError(t, db.Create(bob).Error)

	return db, alice, bob
}

func TestLinkedIdentityStore_Create(t *testing.T) {
	db, alice, _ := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	t.Run("creates a linked identity successfully", func(t *testing.T) {
		input := LinkedIdentityInput{
			UserInternalUUID: string(alice.InternalUUID),
			Provider:         "google",
			ProviderUserID:   "google-sub-123",
			Email:            "alice@gmail.com",
			Name:             "Alice G",
		}
		row, err := store.Create(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, row.ID)
		assert.Equal(t, string(alice.InternalUUID), string(row.UserInternalUUID))
		assert.Equal(t, "google", string(row.Provider))
		assert.Equal(t, "google-sub-123", string(row.ProviderUserID))
		assert.Equal(t, "alice@gmail.com", string(row.Email))
		assert.Equal(t, "Alice G", string(row.Name))
		assert.False(t, row.LinkedAt.IsZero())
	})

	t.Run("duplicate (provider, sub) for same user returns dberrors.ErrConstraint", func(t *testing.T) {
		input := LinkedIdentityInput{
			UserInternalUUID: string(alice.InternalUUID),
			Provider:         "github",
			ProviderUserID:   "gh-dup-sub",
		}
		_, err := store.Create(ctx, input)
		require.NoError(t, err)

		_, err = store.Create(ctx, input)
		require.Error(t, err)
		assert.True(t, errors.Is(err, dberrors.ErrConstraint), "expected ErrConstraint, got: %v", err)
	})
}

func TestLinkedIdentityStore_GlobalUniqueness(t *testing.T) {
	db, alice, bob := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	// Alice links "google" / "shared-sub"
	_, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "shared-sub",
	})
	require.NoError(t, err)

	// Bob tries to link the same (provider, sub) — must fail
	_, err = store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(bob.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "shared-sub",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrConstraint), "expected ErrConstraint, got: %v", err)
}

func TestLinkedIdentityStore_GetByProviderSub(t *testing.T) {
	db, alice, _ := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	_, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "github",
		ProviderUserID:   "gh-alice",
		Email:            "alice@github.com",
	})
	require.NoError(t, err)

	t.Run("found", func(t *testing.T) {
		row, err := store.GetByProviderSub(ctx, "github", "gh-alice")
		require.NoError(t, err)
		assert.Equal(t, string(alice.InternalUUID), string(row.UserInternalUUID))
		assert.Equal(t, "alice@github.com", string(row.Email))
	})

	t.Run("not found returns ErrLinkedIdentityNotFound", func(t *testing.T) {
		_, err := store.GetByProviderSub(ctx, "github", "nonexistent-sub")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrLinkedIdentityNotFound)
	})
}

func TestLinkedIdentityStore_ListByUser(t *testing.T) {
	db, alice, bob := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	// Seed two identities for alice, one for bob
	_, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "google-alice",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "github",
		ProviderUserID:   "github-alice",
	})
	require.NoError(t, err)

	_, err = store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(bob.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "google-bob",
	})
	require.NoError(t, err)

	t.Run("returns only alice's identities", func(t *testing.T) {
		rows, err := store.ListByUser(ctx, string(alice.InternalUUID))
		require.NoError(t, err)
		assert.Len(t, rows, 2)
		for _, r := range rows {
			assert.Equal(t, string(alice.InternalUUID), string(r.UserInternalUUID))
		}
	})

	t.Run("returns empty slice for user with no identities", func(t *testing.T) {
		nobody := uuid.New().String()
		rows, err := store.ListByUser(ctx, nobody)
		require.NoError(t, err)
		assert.Empty(t, rows)
	})
}

func TestLinkedIdentityStore_TouchLastUsed(t *testing.T) {
	db, alice, _ := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	row, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "google-touch",
	})
	require.NoError(t, err)
	assert.Nil(t, row.LastUsedAt)

	err = store.TouchLastUsed(ctx, string(row.ID))
	require.NoError(t, err)

	updated, err := store.GetByProviderSub(ctx, "google", "google-touch")
	require.NoError(t, err)
	assert.NotNil(t, updated.LastUsedAt)
}

func TestLinkedIdentityStore_Delete(t *testing.T) {
	db, alice, bob := setupLinkedIdentityTestDB(t)
	store := NewGormLinkedIdentityStore(db)
	ctx := context.Background()

	row, err := store.Create(ctx, LinkedIdentityInput{
		UserInternalUUID: string(alice.InternalUUID),
		Provider:         "google",
		ProviderUserID:   "google-delete",
	})
	require.NoError(t, err)

	t.Run("delete scoped to wrong owner returns ErrLinkedIdentityNotFound", func(t *testing.T) {
		err := store.Delete(ctx, string(row.ID), string(bob.InternalUUID))
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrLinkedIdentityNotFound)
	})

	t.Run("delete with correct owner succeeds", func(t *testing.T) {
		err := store.Delete(ctx, string(row.ID), string(alice.InternalUUID))
		require.NoError(t, err)
	})

	t.Run("subsequent GetByProviderSub returns ErrLinkedIdentityNotFound", func(t *testing.T) {
		_, err := store.GetByProviderSub(ctx, "google", "google-delete")
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrLinkedIdentityNotFound)
	})
}
