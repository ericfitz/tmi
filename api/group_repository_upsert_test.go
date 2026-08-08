package api

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

// newUpsertGroupTestRepo creates an in-memory SQLite-backed GormGroupRepository
// for testing UpsertGroup in isolation.
func newUpsertGroupTestRepo(t *testing.T) (*GormGroupRepository, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: gormlogger.Discard,
	})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Group{}))
	return NewGormGroupRepository(db), db
}

func TestUpsertGroup_CreateThenIdempotent(t *testing.T) {
	repo, db := newUpsertGroupTestRepo(t)
	ctx := context.Background()

	g := Group{Provider: "okta", GroupName: "engineering"}
	require.NoError(t, repo.UpsertGroup(ctx, g))
	require.NoError(t, repo.UpsertGroup(ctx, g))

	var count int64
	require.NoError(t, db.Model(&models.Group{}).Where("provider = ? AND group_name = ?", "okta", "engineering").Count(&count).Error)
	assert.Equal(t, int64(1), count, "a second upsert for the same (provider, group_name) must not create a second row")
}

// TestUpsertGroup_ToleratesDuplicateRace guards #704: uniq_groups_provider_group_name
// backs the ON CONFLICT target, but a concurrent first-time upsert on Oracle
// (whose MERGE INTO is not atomic the way PostgreSQL's/SQLite's ON CONFLICT
// is) can still lose the create race -- Create() returns a genuine
// duplicate-key error for a row a "concurrent" session already won. This is
// simulated with a GORM callback (see api/optimistic_locking_test.go for the
// same technique) rather than a real race: SQLite's ON CONFLICT resolution
// is atomic per-statement and cannot itself reproduce the underlying Oracle
// MERGE anomaly. UpsertGroup must not surface the duplicate error as a
// failure -- it must tolerate the race (the winner's row already satisfies
// the call's intent).
func TestUpsertGroup_ToleratesDuplicateRace(t *testing.T) {
	repo, db := newUpsertGroupTestRepo(t)
	ctx := context.Background()

	// Seed the row a "concurrent" session already committed.
	winner := models.Group{
		InternalUUID: models.DBVarchar(uuid.New().String()),
		Provider:     "okta",
		GroupName:    "engineering",
		UsageCount:   1,
	}
	require.NoError(t, db.Create(&winner).Error)

	injected := false
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_upsertgroup_duplicate", func(tx *gorm.DB) {
		if injected {
			return
		}
		if _, ok := tx.Statement.Dest.(*models.Group); !ok {
			return
		}
		injected = true
		tx.Error = errors.New(`duplicate key value violates unique constraint "uniq_groups_provider_group_name"`)
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_upsertgroup_duplicate") })

	err := repo.UpsertGroup(ctx, Group{Provider: "okta", GroupName: "engineering"})
	require.NoError(t, err, "a duplicate-key race must be tolerated, not surfaced")
	assert.True(t, injected, "the synthetic duplicate error must actually have fired")

	var count int64
	require.NoError(t, db.Model(&models.Group{}).Where("provider = ? AND group_name = ?", "okta", "engineering").Count(&count).Error)
	assert.Equal(t, int64(1), count, "no second row must have been created")
}

// TestUpsertGroup_NonDuplicateCreateErrorStillFails guards against the new
// fallback over-swallowing: a Create failure that is NOT a duplicate-key race
// (e.g. a dropped connection) must still surface as an error.
func TestUpsertGroup_NonDuplicateCreateErrorStillFails(t *testing.T) {
	repo, db := newUpsertGroupTestRepo(t)
	ctx := context.Background()

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_upsertgroup_transient", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.Group); !ok {
			return
		}
		tx.Error = errors.New("connection reset by peer")
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_upsertgroup_transient") })

	err := repo.UpsertGroup(ctx, Group{Provider: "okta", GroupName: "engineering"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, dberrors.ErrTransient), "a non-duplicate Create error must still surface, got: %v", err)
}

// TestUpsertGroup_DuplicateRaceWithEmptyGroupName_DoesNotMatchWrongRow guards
// against #502-style predicate degradation in the new re-SELECT: GORM's
// struct-based Where omits zero-value fields, so an empty GroupName would
// otherwise drop that predicate and the re-SELECT could match an arbitrary
// group for the same provider instead of failing honestly.
func TestUpsertGroup_DuplicateRaceWithEmptyGroupName_DoesNotMatchWrongRow(t *testing.T) {
	repo, db := newUpsertGroupTestRepo(t)
	ctx := context.Background()

	// A real group for the same provider that an unguarded re-SELECT could
	// wrongly match once the (empty) group_name predicate is dropped.
	require.NoError(t, db.Create(&models.Group{
		InternalUUID: models.DBVarchar(uuid.New().String()),
		Provider:     "okta",
		GroupName:    "unrelated-group",
		UsageCount:   1,
	}).Error)

	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:inject_upsertgroup_duplicate_empty_name", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*models.Group); !ok {
			return
		}
		tx.Error = errors.New(`duplicate key value violates unique constraint "uniq_groups_provider_group_name"`)
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("test:inject_upsertgroup_duplicate_empty_name") })

	err := repo.UpsertGroup(ctx, Group{Provider: "okta", GroupName: ""})
	require.Error(t, err, "an empty group_name must not be silently treated as a resolved race")
	assert.True(t, errors.Is(err, dberrors.ErrDuplicate), "must surface the original duplicate error, got: %v", err)
}
