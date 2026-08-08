package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// setupAdminInitTestDB creates an in-memory SQLite database with the User
// schema for exercising findUserByProviderIdentityGorm's lookup/create
// decision logic (#701).
func setupAdminInitTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger:                                   gormlogger.Discard,
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.User{}))
	return db
}

// TestFindUserByProviderIdentityGorm_Found verifies that a lookup matching an
// existing row returns that row's internal UUID with no error, and that the
// scan struct's projection actually populates the field (the #699 mapping
// bug would leave it blank and downstream uuid.Parse would fail).
func TestFindUserByProviderIdentityGorm_Found(t *testing.T) {
	db := setupAdminInitTestDB(t)

	want := uuid.New()
	providerID := "provider-user-123"
	user := models.User{
		InternalUUID:   models.DBVarchar(want.String()),
		Provider:       models.DBVarchar("tmi"),
		ProviderUserID: models.NewNullableDBVarchar(&providerID),
		Email:          models.DBVarchar("alice@example.com"),
		Name:           models.DBVarchar("Alice"),
	}
	require.NoError(t, db.Create(&user).Error)

	got, err := findUserByProviderIdentityGorm(context.Background(), db, "tmi", providerID, "")
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// TestFindUserByProviderIdentityGorm_NotFound verifies that a lookup with no
// matching row returns gorm.ErrRecordNotFound specifically, which is the
// contract initializeAdministratorsGorm's caller now relies on (#701) to
// decide whether creating a user is safe.
func TestFindUserByProviderIdentityGorm_NotFound(t *testing.T) {
	db := setupAdminInitTestDB(t)

	_, err := findUserByProviderIdentityGorm(context.Background(), db, "tmi", "nonexistent-provider-id", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrRecordNotFound), "expected gorm.ErrRecordNotFound, got %v", err)
}

// TestFindUserByProviderIdentityGorm_RequiresIdentifier verifies the
// existing guard: a call with neither provider_id nor email is a caller
// error, not a not-found result, so it must not be classified as
// ErrRecordNotFound (which would incorrectly route to user creation).
func TestFindUserByProviderIdentityGorm_RequiresIdentifier(t *testing.T) {
	db := setupAdminInitTestDB(t)

	_, err := findUserByProviderIdentityGorm(context.Background(), db, "tmi", "", "")
	require.Error(t, err)
	assert.False(t, errors.Is(err, gorm.ErrRecordNotFound))
}
