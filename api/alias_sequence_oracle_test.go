//go:build oracle

package api

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/stretchr/testify/assert"
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

// TestThreatModelAliasSequenceSelfHealOracleIntegration is the Oracle-gated
// verification of the #476 self-heal: when the alias sequence is dropped out
// from under a running server (schema drift), the allocator must classify the
// resulting ORA-02289 as the recoverable ErrAliasSequenceMissing (chaining to
// dberrors.ErrUndefinedObject), and InstallThreatModelAliasSequence must
// re-create the sequence — idempotently, with the ORA-00955 swallow that lets
// concurrent multi-pod self-heals converge — seeded above the in-use max so
// the next NEXTVAL cannot collide with the unique threat_models.alias index.
//
// This exercises the Oracle-specific risk surface of #476 (ORA-02289
// classification + the PL/SQL reinstall) at the allocator level; the
// Create-level reinstall+retry wrapper is dialect-agnostic Go already proven on
// PostgreSQL. Run via `make test-integration-oci`.
func TestThreatModelAliasSequenceSelfHealOracleIntegration(t *testing.T) {
	ctx := context.Background()
	db := openAuditAppendOnlyOracleDB(t)
	if !db.Migrator().HasTable(&models.AliasCounter{}) {
		require.NoError(t, db.AutoMigrate(&models.AliasCounter{}))
	}

	// Clean slate, restored on exit.
	dropOracleAliasSequence(t, db)
	t.Cleanup(func() { dropOracleAliasSequence(t, db) })

	const seededHigh = int32(7777)
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

	alloc := func() (int32, error) {
		var got int32
		err := db.Transaction(func(tx *gorm.DB) error {
			v, e := AllocateNextAlias(ctx, tx, "__global__", "threat_model")
			got = v
			return e
		})
		return got, err
	}

	// Baseline allocation works on the sequence path.
	first, err := alloc()
	require.NoError(t, err)
	require.Greater(t, first, seededHigh, "sequence must start above the in-use max")

	// Record `first` as the in-use high-water mark. The seed query reads
	// GREATEST(MAX(threat_models.alias), MAX(alias_counters.next_alias)-1); this
	// allocator-level test inserts no threat_models rows (avoids the owner FK),
	// so the legacy counter stands in for "max alias in use" — exactly as the
	// happy-path Oracle test seeds it. Without this the reinstall would re-seed
	// from the original high-water and legitimately hand out `first` again (no
	// collision, since nothing persisted it); bumping the counter lets us assert
	// the reinstall re-seeds strictly above the in-use mark.
	require.NoError(t, db.Exec("UPDATE ALIAS_COUNTERS SET NEXT_ALIAS = ? WHERE PARENT_ID = ? AND OBJECT_TYPE = ?", first+1, "__global__", "threat_model").Error)

	// Simulate schema drift: drop the sequence while the gate stays on.
	dropOracleAliasSequence(t, db)

	// The allocator must classify ORA-02289 as the recoverable sentinel.
	_, missingErr := alloc()
	require.Error(t, missingErr)
	assert.True(t, errors.Is(missingErr, ErrAliasSequenceMissing), "ORA-02289 must surface ErrAliasSequenceMissing")
	assert.True(t, errors.Is(missingErr, dberrors.ErrUndefinedObject), "and chain to dberrors.ErrUndefinedObject")

	// The self-heal's reinstall step must recreate the sequence, and be
	// idempotent under a second call (the ORA-00955-guarded multi-pod path).
	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db), "reinstall must recreate the dropped sequence")
	require.NoError(t, dbschema.InstallThreatModelAliasSequence(ctx, db), "reinstall must be idempotent (ORA-00955 swallowed)")

	// Allocation recovers, and the reinstalled sequence is seeded strictly above
	// the in-use high-water mark — so it cannot collide with the unique
	// threat_models.alias index.
	recovered, err := alloc()
	require.NoError(t, err, "allocation must recover after reinstall")
	require.Greater(t, recovered, first, "reinstalled sequence must seed above the in-use max alias")
}
