//go:build oracle

package api

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openAuditAppendOnlyOracleDB opens a direct GORM connection to the Oracle ADB
// backend used by `make test-integration-oci`. Reuses authdb.ParseDatabaseURL +
// authdb.NewGormDB so the oracle-samples/gorm-oracle dialector is configured
// exactly as in production (OracleNamingStrategy uppercasing +
// SkipQuoteIdentifiers). Reads TMI_DATABASE_URL (oracle://…), ORACLE_PASSWORD,
// and the wallet directory from TMI_ORACLE_WALLET_LOCATION (falling back to
// TNS_ADMIN). When TMI_DATABASE_URL is unset the test skips.
func openAuditAppendOnlyOracleDB(t *testing.T) *gorm.DB {
	t.Helper()

	dbURL := os.Getenv("TMI_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TMI_DATABASE_URL not set; run under `make test-integration-oci` with scripts/oci-env.sh sourced")
	}

	cfg, err := authdb.ParseDatabaseURL(dbURL)
	require.NoError(t, err, "parse TMI_DATABASE_URL")
	require.Equal(t, authdb.DatabaseTypeOracle, cfg.Type,
		"this test requires an oracle:// TMI_DATABASE_URL (got %q)", cfg.Type)

	if cfg.OracleWalletLocation == "" {
		if w := os.Getenv("TMI_ORACLE_WALLET_LOCATION"); w != "" {
			cfg.OracleWalletLocation = w
		} else if w := os.Getenv("TNS_ADMIN"); w != "" {
			cfg.OracleWalletLocation = w
		}
	}

	gormDB, err := authdb.NewGormDB(*cfg)
	require.NoError(t, err, "open Oracle ADB connection")
	t.Cleanup(func() { _ = gormDB.Close() })

	require.NoError(t, gormDB.AutoMigrate(&models.AuditEntry{}, &models.VersionSnapshot{}))
	return gormDB.DB()
}

// dropOracleAuditTriggers drops all three audit triggers on Oracle, ignoring
// ORA-04080 (trigger does not exist). Wrapped in anonymous PL/SQL so that the
// caller can call this before trigger installation (for clean-slate seeding).
func dropOracleAuditTriggers(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, name := range []string{
		"tmi_audit_entries_no_mutate",
		"tmi_version_snapshots_no_mutate",
		"tmi_system_audit_entries_no_mutate",
	} {
		sql := `BEGIN EXECUTE IMMEDIATE 'DROP TRIGGER ` + name + `'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -4080 THEN RAISE; END IF; END;`
		require.NoError(t, db.Exec(sql).Error, "drop trigger %s (ignoring ORA-04080)", name)
	}
}

// seedOracleBackdatedEntry inserts an AuditEntry and then backdates CREATED_AT
// by raw UPDATE. Must run BEFORE trigger installation (the backdate is an UPDATE).
// On Oracle, table and column identifiers are uppercase.
func seedOracleBackdatedEntry(t *testing.T, db *gorm.DB, ageDays int) string {
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
	// Oracle naming strategy uppercases column and table identifiers.
	require.NoError(t, db.Exec("UPDATE AUDIT_ENTRIES SET CREATED_AT = ? WHERE ID = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

// TestAuditAppendOnlyTriggersAgeFloorOracleIntegration is the Oracle-gated
// counterpart to TestAppendOnlyTriggersAgeFloorIntegration (PG). It verifies:
//   - DELETE of a row aged past the trigger floor succeeds (RowsAffected == 1)
//   - DELETE of a young row is blocked and classifies as ErrAppendOnlyViolation
//     (ORA-20001 → dberrors.Classify)
//   - UPDATE is blocked regardless of row age
//
// Run via `make test-integration-oci`.
func TestAuditAppendOnlyTriggersAgeFloorOracleIntegration(t *testing.T) {
	db := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()

	// Drop any pre-existing triggers so the backdate UPDATEs are not blocked.
	dropOracleAuditTriggers(t, db)

	oldID := seedOracleBackdatedEntry(t, db, 40)  // older than the 30-day floor
	youngID := seedOracleBackdatedEntry(t, db, 5) // younger than the floor

	// Install with floors: audit_entries 30d (retention 31 → floor 30),
	// version_snapshots 7d (min(90,8)-1 = 7, which equals the hard min).
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     31,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 8,
	}))

	t.Cleanup(func() {
		// Drop triggers first so cleanup DELETEs are not blocked.
		dropOracleAuditTriggers(t, db)
		_ = db.Exec("DELETE FROM AUDIT_ENTRIES WHERE ID = ?", youngID).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM AUDIT_ENTRIES WHERE ID = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked and classifies", func(t *testing.T) {
		err := db.Exec("DELETE FROM AUDIT_ENTRIES WHERE ID = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation (ORA-20001), got: %v", err)
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE AUDIT_ENTRIES SET CHANGE_TYPE = 'updated' WHERE ID = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation (ORA-20001), got: %v", err)
	})
}

// TestPruneAuditEntriesChunkedOracleIntegration verifies that PruneAuditEntries
// works correctly through the age-floored Oracle trigger when there are more
// than 1000 eligible rows — the chunked IN-list delete path that avoids
// ORA-01795. Seeds 1200 backdated entries, installs triggers, then calls
// PruneAuditEntries with AUDIT_RETENTION_DAYS=35 and asserts ≥1200 pruned.
func TestPruneAuditEntriesChunkedOracleIntegration(t *testing.T) {
	db := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()

	// Remove any lingering triggers so INSERT + backdate UPDATE are unblocked.
	dropOracleAuditTriggers(t, db)

	// Seed 1200 backdated entries (all aged past the 35-day retention → 34-day floor).
	// Use CreateInBatches for speed; backdate in one bulk UPDATE per seed batch.
	const batchCount = 1200
	const ageDays = 40

	entries := make([]models.AuditEntry, batchCount)
	v := 1
	seededIDs := make([]string, batchCount)
	for i := range entries {
		id := uuid.New().String()
		seededIDs[i] = id
		entries[i] = models.AuditEntry{
			ID:               models.DBVarchar(id),
			ThreatModelID:    models.DBVarchar(uuid.New().String()),
			ObjectType:       models.DBVarchar(models.ObjectTypeThreatModel),
			ObjectID:         models.DBVarchar(uuid.New().String()),
			Version:          &v,
			ChangeType:       models.DBVarchar(models.ChangeTypeCreated),
			ActorEmail:       models.DBVarchar("oracle-test@tmi.local"),
			ActorProvider:    models.DBVarchar("tmi"),
			ActorProviderID:  models.DBVarchar("oracle-test"),
			ActorDisplayName: models.DBVarchar("Oracle Test User"),
		}
	}

	// CreateInBatches inserts in groups of 100 to avoid Oracle's max-bind-variable limit.
	require.NoError(t, db.CreateInBatches(&entries, 100).Error, "batch-seed audit entries")

	// Backdate in chunks to stay under ORA-01795 (1000-element IN-list cap).
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	for _, chunk := range chunkIDs(seededIDs, 1000) {
		require.NoError(t,
			db.Exec("UPDATE AUDIT_ENTRIES SET CREATED_AT = ? WHERE ID IN ?", backdated, chunk).Error,
			"backdate chunk of %d entries", len(chunk),
		)
	}

	// Install triggers with 35-day retention → 34-day floor (above hard min 30).
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:     35,
		VersionRetentionDays:   90,
		TombstoneRetentionDays: 30,
	}))

	t.Cleanup(func() {
		dropOracleAuditTriggers(t, db)
		for _, chunk := range chunkIDs(seededIDs, 1000) {
			_ = db.Exec("DELETE FROM AUDIT_ENTRIES WHERE ID IN ?", chunk).Error
		}
	})

	t.Setenv("AUDIT_RETENTION_DAYS", "35")
	svc := NewGormAuditService(db)
	pruned, err := svc.PruneAuditEntries(ctx)
	require.NoError(t, err, "PruneAuditEntries must succeed through age-floored trigger on Oracle (chunked IN-list)")
	assert.GreaterOrEqual(t, pruned, batchCount,
		"all %d seeded backdated entries should have been pruned (got %d)", batchCount, pruned)
}

// seedOracleSystemAuditEntry inserts a SystemAuditEntry and then backdates
// CREATED_AT by raw UPDATE. Must run BEFORE trigger installation (the backdate
// is an UPDATE). On Oracle, table and column identifiers are uppercase.
func seedOracleSystemAuditEntry(t *testing.T, db *gorm.DB, ageDays int) string {
	t.Helper()
	entry := models.SystemAuditEntry{
		ID:               models.DBVarchar(uuid.New().String()),
		ActorEmail:       models.DBVarchar("charlie@tmi.local"),
		ActorProvider:    models.DBVarchar("tmi"),
		ActorProviderID:  models.DBVarchar("charlie"),
		ActorDisplayName: models.DBVarchar("Charlie"),
		HTTPMethod:       models.DBVarchar("PUT"),
		HTTPPath:         models.DBText("/admin/settings/test"),
		FieldPath:        models.DBVarchar("test"),
	}
	require.NoError(t, db.Create(&entry).Error)
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	// Oracle naming strategy uppercases column and table identifiers.
	require.NoError(t, db.Exec("UPDATE SYSTEM_AUDIT_ENTRIES SET CREATED_AT = ? WHERE ID = ?", backdated, entry.ID).Error)
	return string(entry.ID)
}

// TestSystemAuditAppendOnlyOracleIntegration is the Oracle-gated counterpart
// to TestSystemAuditAppendOnlyAgeFloorIntegration (PG). It verifies:
//   - DELETE of a row aged past the 90-day hard-min floor succeeds.
//   - DELETE of a young row is blocked → ErrAppendOnlyViolation (ORA-20001).
//   - UPDATE is blocked regardless of row age.
//
// Run via `make test-integration-oci`.
func TestSystemAuditAppendOnlyOracleIntegration(t *testing.T) {
	db := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()

	require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))

	// Drop all three audit triggers so backdate UPDATEs are unblocked.
	dropOracleAuditTriggers(t, db)

	oldID := seedOracleSystemAuditEntry(t, db, 100)  // older than the 90-day hard-min floor
	youngID := seedOracleSystemAuditEntry(t, db, 10) // younger than the floor

	// Install with SystemAuditRetentionDays=91 → floor=90 (retention-1).
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:       365,
		VersionRetentionDays:     90,
		TombstoneRetentionDays:   30,
		SystemAuditRetentionDays: 91,
	}))

	t.Cleanup(func() {
		// Drop triggers first so cleanup DELETEs are not blocked.
		dropOracleAuditTriggers(t, db)
		_ = db.Exec("DELETE FROM SYSTEM_AUDIT_ENTRIES WHERE ID = ?", youngID).Error
	})

	t.Run("delete of aged row succeeds", func(t *testing.T) {
		res := db.Exec("DELETE FROM SYSTEM_AUDIT_ENTRIES WHERE ID = ?", oldID)
		require.NoError(t, res.Error)
		assert.Equal(t, int64(1), res.RowsAffected)
	})

	t.Run("delete of young row is blocked and classifies", func(t *testing.T) {
		err := db.Exec("DELETE FROM SYSTEM_AUDIT_ENTRIES WHERE ID = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation (ORA-20001), got: %v", err)
	})

	t.Run("update is blocked regardless of age", func(t *testing.T) {
		err := db.Exec("UPDATE SYSTEM_AUDIT_ENTRIES SET ACTOR_EMAIL = 'evil@tmi.local' WHERE ID = ?", youngID).Error
		require.Error(t, err)
		assert.True(t, errors.Is(dberrors.Classify(err), dberrors.ErrAppendOnlyViolation),
			"expected ErrAppendOnlyViolation (ORA-20001), got: %v", err)
	})
}
