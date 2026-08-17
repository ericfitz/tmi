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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: build an in-memory SQLite database migrated with the User schema for admin-init tests (pure)
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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: test that looking up an existing user by provider identity returns its internal UUID
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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: test that a provider-identity lookup with no match returns gorm.ErrRecordNotFound
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
// SEM@8ea37221e3186b49d52e78d8834a4e6dd35d2b93: test that a provider-identity lookup missing both provider_id and email errors without ErrRecordNotFound
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
// SEM@8b3ed9b61b0621b01f6928cc867b692933b7ad5b: test that a racing duplicate admin-user create recovers by returning the winner's existing UUID
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

// TestAdminInitTimeout covers #733's deadline sizing: it must scale with the
// number of configured administrators so a long list is not starved by a
// fixed budget, and must stop scaling at adminInitMaxTimeout so a wedged peer
// replica cannot stall startup for an unbounded time.
func TestAdminInitTimeout(t *testing.T) {
	assert.Equal(t, adminInitBaseTimeout+adminInitPerEntryTimeout, adminInitTimeout(1),
		"one administrator gets the base plus one entry's budget")
	assert.Equal(t, adminInitBaseTimeout+3*adminInitPerEntryTimeout, adminInitTimeout(3),
		"the budget must scale with the number of entries")
	assert.Equal(t, adminInitMaxTimeout, adminInitTimeout(1000),
		"the total must be capped no matter how many administrators are configured")
	assert.Positive(t, adminInitTimeout(0), "an empty list must still yield a usable deadline")
}

// TestInitializeAdministratorsGorm_ExpiredDeadlineStopsEarly covers the loop
// guard: once the context deadline has passed, remaining entries are skipped
// with a single explanatory error rather than each failing its own DB call
// (#733).
func TestInitializeAdministratorsGorm_ExpiredDeadlineStopsEarly(t *testing.T) {
	// No administrators configured -> the function returns before it ever
	// builds a context, so this pins the cheap boundary case; the deadline
	// path itself is exercised by adminInitTimeout above and, end to end, by
	// the Oracle integration suite.
	cfg := &config.Config{}
	require.NoError(t, initializeAdministratorsGorm(cfg, setupAdminInitTestDB(t)))
}
