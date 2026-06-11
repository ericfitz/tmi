//go:build dev || test || integration

package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openAppendOnlyIntegrationDB opens the real PostgreSQL integration database.
// The append-only triggers only exist on PG/Oracle, so this test SKIPS when
// TEST_DB_* is unset instead of falling back to SQLite.
func openAppendOnlyIntegrationDB(t *testing.T) *gorm.DB {
	t.Helper()

	host := os.Getenv("TEST_DB_HOST")
	port := os.Getenv("TEST_DB_PORT")
	user := os.Getenv("TEST_DB_USER")
	password := os.Getenv("TEST_DB_PASSWORD")
	dbname := os.Getenv("TEST_DB_NAME")
	if host == "" || port == "" || user == "" || dbname == "" {
		t.Skip("TEST_DB_* not set; append-only trigger test requires PostgreSQL")
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC",
		host, port, user, password, dbname,
	)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err, "open PostgreSQL integration DB")
	require.NoError(t, db.AutoMigrate(&models.AuditEntry{}, &models.VersionSnapshot{}))
	return db
}

// seedBackdatedEntry inserts an audit entry and then backdates created_at by
// rawSQL. Must run BEFORE trigger installation (the backdate is an UPDATE).
func seedBackdatedEntry(t *testing.T, db *gorm.DB, ageDays int) string {
	t.Helper()
	v := 1
	entry := models.AuditEntry{
		ThreatModelID:    models.DBVarchar(uuid.New().String()),
		ObjectType:       models.DBVarchar(models.ObjectTypeThreatModel),
		ObjectID:         models.DBVarchar(uuid.New().String()),
		Version:          &v,
		ChangeType:       models.DBVarchar(models.ChangeTypeCreated),
		ActorEmail:       models.DBVarchar("alice@tmi.local"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("alice"),
		ActorDisplayName: models.DBVarchar("Alice (TMI User)"),
	}
	require.NoError(t, db.Create(&entry).Error)
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	require.NoError(t, db.Exec("UPDATE audit_entries SET created_at = ? WHERE id = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

func TestAppendOnlyTriggersAgeFloorIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	ctx := context.Background()

	// Clean slate for this test's rows; remove any pre-existing triggers so
	// the backdating UPDATEs work.
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error)
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error)

	oldID := seedBackdatedEntry(t, db, 40)  // older than the 30-day floor below
	youngID := seedBackdatedEntry(t, db, 5) // younger than the floor

	// Install with floors: audit_entries 30d (retention 31 → 30), snapshots 7d.
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     31,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 8,
	}))
	t.Cleanup(func() {
		// Leave the dev DB in its normal state for other tests.
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
		_ = db.Exec("DELETE FROM audit_entries WHERE id IN ?", []string{youngID}).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM audit_entries WHERE id = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked and classifies", func(t *testing.T) {
		err := db.Exec("DELETE FROM audit_entries WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE audit_entries SET change_type = 'updated' WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})
}

func TestPruneAuditEntriesWorksWithTriggersIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t)
	ctx := context.Background()

	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error)
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error)

	// Entry aged past a 35-day retention (and past the 30-day hard-min floor).
	prunableID := seedBackdatedEntry(t, db, 40)

	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     35,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 30,
	}))
	t.Cleanup(func() {
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
	})

	t.Setenv("AUDIT_RETENTION_DAYS", "35")
	svc := NewGormAuditService(db)
	pruned, err := svc.PruneAuditEntries(ctx)
	require.NoError(t, err, "prune must succeed through the age-floored trigger")
	assert.GreaterOrEqual(t, pruned, 1)

	var count int64
	require.NoError(t, db.Model(&models.AuditEntry{}).Where("id = ?", prunableID).Count(&count).Error)
	assert.Equal(t, int64(0), count, "backdated entry should have been pruned")
}
