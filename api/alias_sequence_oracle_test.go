//go:build oracle

package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// dropOracleAliasSequence drops the global ThreatModel alias sequence, ignoring
// ORA-02289 (sequence does not exist). Wrapped in anonymous PL/SQL so it is
// safe to call for clean-slate seeding and in cleanup.
func dropOracleAliasSequence(t *testing.T, db *gorm.DB) {
	t.Helper()
	sql := `BEGIN EXECUTE IMMEDIATE 'DROP SEQUENCE TMI_THREAT_MODEL_ALIAS_SEQ'; EXCEPTION WHEN OTHERS THEN IF SQLCODE != -2289 THEN RAISE; END IF; END;`
	require.NoError(t, db.Exec(sql).Error, "drop sequence (ignoring ORA-02289)")
}

// TestThreatModelAliasSequenceOracleIntegration is the Oracle-gated counterpart
// to TestThreatModelAliasSequence_Integration (PG). It verifies the #452
// sequence-backed global alias path on real Oracle ADB:
//   - InstallThreatModelAliasSequence creates the sequence via the PL/SQL
//     existence-guarded block (exercises GREATEST/NVL, EXECUTE IMMEDIATE,
//     identifier case-folding) seeded above the in-use max + deploy buffer.
//   - AllocateNextAlias draws monotonic values via tmi_threat_model_alias_seq.NEXTVAL.
//   - A NEXTVAL drawn in a rolled-back transaction is consumed, not reused
//     (proves the allocation is non-transactional → no ORA-08177 under SERIALIZABLE).
//
// Run via `make test-integration-oci`.
func TestThreatModelAliasSequenceOracleIntegration(t *testing.T) {
	ctx := context.Background()
	db := openAuditAppendOnlyOracleDB(t) // shared Oracle connection helper
	// alias_counters already exists on the shared ADB (server-migrated); a
	// re-AutoMigrate is not idempotent on gorm-oracle (ORA-01430), so only
	// create it when genuinely absent.
	if !db.Migrator().HasTable(&models.AliasCounter{}) {
		require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	}

	// Clean slate: drop any existing sequence so the install re-seeds.
	dropOracleAliasSequence(t, db)
	t.Cleanup(func() { dropOracleAliasSequence(t, db) })

	// Seed a known high-water mark via the legacy global counter row (no FK,
	// unlike threat_models). aliasSeedStart reads MAX(next_alias)-1 from it, so
	// the sequence must start strictly above seededHigh. Snapshot/restore any
	// pre-existing row so the shared ADB is left untouched.
	const seededHigh = int32(4242)
	var saved *models.AliasCounter
	var existing models.AliasCounter
	if err := db.Where("parent_id = ? AND object_type = ?", "__global__", "threat_model").First(&existing).Error; err == nil {
		c := existing
		saved = &c
	}
	require.NoError(t, db.Exec("DELETE FROM ALIAS_COUNTERS WHERE PARENT_ID = ? AND OBJECT_TYPE = ?", "__global__", "threat_model").Error)
	require.NoError(t, db.Create(&models.AliasCounter{
		ParentID:   models.DBVarchar("__global__"),
		ObjectType: models.DBVarchar("threat_model"),
		NextAlias:  seededHigh + 1,
	}).Error)
	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM ALIAS_COUNTERS WHERE PARENT_ID = ? AND OBJECT_TYPE = ?", "__global__", "threat_model").Error
		if saved != nil {
			_ = db.Create(saved).Error
		}
	})

	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db))
	prev := useAliasSequence.Load()
	EnableThreatModelAliasSequence()
	t.Cleanup(func() { useAliasSequence.Store(prev) })

	alloc := func() int32 {
		var got int32
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			v, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
			got = v
			return err
		}))
		return got
	}

	first := alloc()
	require.Greater(t, first, seededHigh, "sequence must start above the in-use max alias")
	second := alloc()
	require.Equal(t, first+1, second, "sequence allocations must be monotonic")

	var rolledBack int32
	require.Error(t, db.Transaction(func(tx *gorm.DB) error {
		v, err := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
		require.NoError(t, err)
		rolledBack = v
		return fmt.Errorf("force rollback")
	}))
	afterRollback := alloc()
	require.Greater(t, afterRollback, rolledBack, "rolled-back NEXTVAL must leave a gap, not be reused")
}
