package main

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/config"
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
// scan struct's projection actually populates the field. Note: this runs
// against SQLite, which lowercases result-set labels the same as Postgres --
// it cannot reproduce the #699 label-folding bug class (uppercase labels from
// Oracle silently failing a lowercase-tagged scan field). The real regression
// guard for that class is scripts/check-scan-struct-column-tags.py (#725).
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

// TestCreateUserForAdministratorGorm_DuplicateOnCreateReturnsWinner verifies
// that when two replicas race to create the same first-boot administrator
// row on idx_users_provider_lookup (#701), the loser recovers by re-fetching
// the winner's existing UUID instead of erroring out and skipping that
// admin's group-add for the boot (#725).
func TestCreateUserForAdministratorGorm_DuplicateOnCreateReturnsWinner(t *testing.T) {
	db := setupAdminInitTestDB(t)

	existing := uuid.New()
	providerID := "admin-provider-id"
	winner := models.User{
		InternalUUID:   models.DBVarchar(existing.String()),
		Provider:       models.DBVarchar("tmi"),
		ProviderUserID: models.NewNullableDBVarchar(&providerID),
		Email:          models.DBVarchar("admin@example.com"),
		Name:           models.DBVarchar("admin"),
	}
	require.NoError(t, db.Create(&winner).Error)

	adminCfg := config.AdministratorConfig{
		Provider:    "tmi",
		ProviderId:  providerID,
		Email:       "admin@example.com",
		SubjectType: "user",
	}

	got, err := createUserForAdministratorGorm(context.Background(), db, adminCfg)
	require.NoError(t, err)
	assert.Equal(t, existing, got, "loser must return the winner's existing UUID, not a fresh one")
}
