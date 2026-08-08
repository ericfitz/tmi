package api

import (
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

// setupGroupOnlyTestDB creates a minimal in-memory SQLite DB for testing
// ensureGroupExists in isolation.
func setupGroupOnlyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Group{}))
	return db
}

func TestEnsureGroupExists_CreateThenIdempotent(t *testing.T) {
	db := setupGroupOnlyTestDB(t)
	store := NewGormThreatModelStore(db)
	provider := "okta"

	id1, err := store.ensureGroupExists(db, "engineering", &provider)
	require.NoError(t, err)
	require.NotEmpty(t, id1)

	id2, err := store.ensureGroupExists(db, "engineering", &provider)
	require.NoError(t, err)
	assert.Equal(t, id1, id2, "a second call for the same (provider, group_name) must return the same row")

	var count int64
	require.NoError(t, db.Model(&models.Group{}).Where("provider = ? AND group_name = ?", provider, "engineering").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// TestEnsureGroupExists_ToleratesDuplicateRace guards #704:
// uniq_groups_provider_group_name backs the ON CONFLICT target, but a
// concurrent first-time upsert on Oracle (whose MERGE INTO is not atomic the
// way PostgreSQL's/SQLite's ON CONFLICT is) can still lose the create race --
// Create() returns a genuine duplicate-key error for a row a "concurrent"
// session already won. This is simulated with a GORM callback (the same
// technique optimistic_locking_test.go uses to inject a synthetic
// gorm-oracle error) rather than a real race: SQLite's ON CONFLICT
// resolution is atomic per-statement and cannot itself reproduce the
// underlying Oracle MERGE anomaly. ensureGroupExists must not surface the
// duplicate error -- it must fall through to the existing re-SELECT and
// return the winner's row.
func TestEnsureGroupExists_ToleratesDuplicateRace(t *testing.T) {
	db := setupGroupOnlyTestDB(t)
	store := NewGormThreatModelStore(db)
	provider := "okta"

	// Seed the row a "concurrent" session already committed.
	winner := models.Group{
		InternalUUID: models.DBVarchar(uuid.New().String()),
		Provider:     models.DBVarchar(provider),
		GroupName:    models.DBVarchar("engineering"),
		UsageCount:   1,
	}
	require.NoError(t, db.Create(&winner).Error)

	injected := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_group_duplicate", func(tx *gorm.DB) {
		if injected {
			return
		}
		if _, ok := tx.Statement.Dest.(*models.Group); !ok {
			return
		}
		injected = true
		tx.Error = errors.New(`duplicate key value violates unique constraint "uniq_groups_provider_group_name"`)
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_group_duplicate") })

	id, err := store.ensureGroupExists(db, "engineering", &provider)
	require.NoError(t, err, "a duplicate-key race must be swallowed, not surfaced")
	assert.Equal(t, string(winner.InternalUUID), id, "must return the winner's row, not fail or fabricate a new one")
	assert.True(t, injected, "the synthetic duplicate error must actually have fired")

	var count int64
	require.NoError(t, db.Model(&models.Group{}).Where("provider = ? AND group_name = ?", provider, "engineering").Count(&count).Error)
	assert.Equal(t, int64(1), count, "no second row must have been created")
}

// TestEnsureGroupExists_NonDuplicateCreateErrorStillFails guards against the
// new fallback over-swallowing: a Create failure that is NOT a duplicate-key
// race (e.g. a dropped connection) must still surface as an error.
func TestEnsureGroupExists_NonDuplicateCreateErrorStillFails(t *testing.T) {
	db := setupGroupOnlyTestDB(t)
	store := NewGormThreatModelStore(db)
	provider := "okta"

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_group_transient", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.Group); !ok {
			return
		}
		tx.Error = errors.New("connection reset by peer")
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_group_transient") })

	_, err := store.ensureGroupExists(db, "engineering", &provider)
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrTransient), "a non-duplicate Create error must still surface, got: %v", err)
}
