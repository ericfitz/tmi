//go:build oracle

package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPruneSystemAuditEntriesBoundedOracleIntegration verifies the #460 bounded
// per-transaction batch prune on real Oracle ADB. It seeds more than one batch
// (systemAuditPruneBatchSize == 1000) of backdated system_audit_entries,
// installs the age-floored append-only trigger, and asserts PruneSystemAuditEntries
// drains every expired row across multiple batches through the trigger —
// exercising the FETCH FIRST n ROWS ONLY + chunked id IN (...) delete path
// (ORA-01795 compliance) end to end.
//
// Run via `make test-integration-oci`.
func TestPruneSystemAuditEntriesBoundedOracleIntegration(t *testing.T) {
	db := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()

	// system_audit_entries already exists on the shared ADB (server-migrated);
	// a re-AutoMigrate is not idempotent on gorm-oracle (ORA-01442), so only
	// create it when genuinely absent.
	if !db.Migrator().HasTable(&models.SystemAuditEntry{}) {
		require.NoError(t, db.AutoMigrate(&models.SystemAuditEntry{}))
	}

	// Remove any lingering triggers so INSERT + backdate UPDATE are unblocked.
	dropOracleAuditTriggers(t, db)

	// Seed 1100 backdated entries (> one batch, < two) all aged past the 90-day
	// hard-min floor, forcing two prune iterations (1000 + 100).
	const seedCount = systemAuditPruneBatchSize + 100
	const ageDays = 120

	entries := make([]models.SystemAuditEntry, seedCount)
	seededIDs := make([]string, seedCount)
	for i := range entries {
		id := uuid.New().String()
		seededIDs[i] = id
		entries[i] = models.SystemAuditEntry{
			ID:               models.DBVarchar(id),
			ActorEmail:       models.DBVarchar("oracle-prune@tmi.local"),
			ActorProvider:    models.DBVarchar("tmi"),
			ActorProviderID:  models.DBVarchar("oracle-prune"),
			ActorDisplayName: models.DBVarchar("Oracle Prune Test"),
			HTTPMethod:       models.DBVarchar("PUT"),
			HTTPPath:         models.DBText("/admin/settings/test"),
			FieldPath:        models.DBVarchar("test"),
		}
	}
	// CreateInBatches in groups of 100 to stay under Oracle's bind-variable limit.
	require.NoError(t, db.CreateInBatches(&entries, 100).Error, "batch-seed system audit entries")

	// Backdate in chunks to stay under ORA-01795 (1000-element IN-list cap).
	backdated := time.Now().UTC().AddDate(0, 0, -ageDays)
	for _, chunk := range chunkIDs(seededIDs, 1000) {
		require.NoError(t,
			db.Exec("UPDATE SYSTEM_AUDIT_ENTRIES SET CREATED_AT = ? WHERE ID IN ?", backdated, chunk).Error,
			"backdate chunk of %d entries", len(chunk),
		)
	}

	// Install the system_audit trigger with retention 91 → floor 90 (hard min),
	// so 120-day-old rows are past the floor and DELETE is permitted.
	require.NoError(t, dbschema.InstallAuditAppendOnlyTriggers(ctx, db, dbschema.AuditFloorConfig{
		AuditRetentionDays:       365,
		VersionRetentionDays:     90,
		TombstoneRetentionDays:   30,
		SystemAuditRetentionDays: 91,
	}))

	t.Cleanup(func() {
		dropOracleAuditTriggers(t, db)
		for _, chunk := range chunkIDs(seededIDs, 1000) {
			_ = db.Exec("DELETE FROM SYSTEM_AUDIT_ENTRIES WHERE ID IN ?", chunk).Error
		}
	})

	// Pruner cutoff = now - 91d; the 120-day-old rows are well past it.
	t.Setenv("SYSTEM_AUDIT_RETENTION_DAYS", "91")
	svc := NewGormAuditService(db)
	pruned, err := svc.PruneSystemAuditEntries(ctx)
	require.NoError(t, err, "bounded prune must succeed through the age-floored trigger on Oracle")
	assert.GreaterOrEqual(t, pruned, seedCount,
		"all %d seeded backdated rows should be pruned across batches (got %d)", seedCount, pruned)
}
