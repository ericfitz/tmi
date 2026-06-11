//go:build dev || test || integration

package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSystemAuditAppendOnlyAgeFloorIntegration verifies the age-floored
// append-only trigger on system_audit_entries against a real PostgreSQL
// database. Skips when TEST_DB_* env vars are unset.
//
// Subtests:
//   - DELETE of a row aged past the 90-day hard-min floor succeeds.
//   - DELETE of a young row is blocked → ErrAppendOnlyViolation.
//   - UPDATE is blocked regardless of row age.
//   - End-to-end: PruneSystemAuditEntries removes aged rows through the trigger.
func TestSystemAuditAppendOnlyAgeFloorIntegration(t *testing.T) {
	db := openAppendOnlyIntegrationDB(t) // skips when TEST_DB_* unset
	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	ctx := context.Background()

	// Drop any pre-existing system-audit trigger so that the backdating UPDATEs
	// inside seedSystemAuditEntry are not blocked.
	require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error)

	// Seed before trigger installation so the backdate UPDATE succeeds.
	oldID := seedSystemAuditEntry(t, db, 100)  // older than the 90-day hard-min floor
	youngID := seedSystemAuditEntry(t, db, 10) // younger than the floor

	// Install all three triggers; SystemAuditRetentionDays=91 → floor=90.
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:       365,
		VersionRetentionDays:     90,
		TombstoneRetentionDays:   30,
		SystemAuditRetentionDays: 91, // floor = 90 (retention-1)
	}))
	t.Cleanup(func() {
		// Restore the DB to a neutral state for other tests.
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_audit_entries_no_mutate ON audit_entries").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_version_snapshots_no_mutate ON version_snapshots").Error
		_ = db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error
		// Remove the young row (oldID was deleted in the first subtest).
		_ = db.Exec("DELETE FROM system_audit_entries WHERE id = ?", youngID).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM system_audit_entries WHERE id = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked", func(t *testing.T) {
		err := db.Exec("DELETE FROM system_audit_entries WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE system_audit_entries SET actor_email = 'evil@tmi.local' WHERE id = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation, got: %v", err)
	})

	t.Run("prune deletes aged rows through the trigger", func(t *testing.T) {
		// The backdate inside seedSystemAuditEntry is an UPDATE — it must run
		// before the trigger is installed. Drop + reinstall around the seed.
		require.NoError(t, db.Exec("DROP TRIGGER IF EXISTS tmi_system_audit_entries_no_mutate ON system_audit_entries").Error)
		agedID := seedSystemAuditEntry(t, db, 200)
		require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
			AuditRetentionDays:       365,
			VersionRetentionDays:     90,
			TombstoneRetentionDays:   30,
			SystemAuditRetentionDays: 91,
		}))

		t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "91")
		svc := NewGormAuditService(db)
		pruned, err := svc.PruneSystemAuditEntries(ctx)
		require.NoError(t, err, "PruneSystemAuditEntries must succeed through the age-floored trigger")
		assert.GreaterOrEqual(t, pruned, 1)

		var count int64
		require.NoError(t, db.Model(&models.SystemAuditEntry{}).Where("id = ?", agedID).Count(&count).Error)
		assert.Equal(t, int64(0), count, "backdated entry should have been pruned")
	})
}
