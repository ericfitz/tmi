//go:build oracle

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMetadataBulkWriteOracleIntegration is the live-Oracle counterpart to the
// SQLite-backed api/metadata_repository_bulk_test.go, added for the #666
// batched bulk-metadata review (oracle-db-admin: APPROVED WITH NOTES). It
// exercises the two write shapes gorm-oracle folds into a single multi-row
// statement per batch — CreateInBatches (BulkCreate/BulkReplace) and
// OnConflict.CreateInBatches (BulkUpdate's multi-row MERGE) — against the
// live ADB, since neither has coverage of its own beyond the in-memory SQLite
// dialect:
//
//   - BulkCreate with >=2 entries is the ORA-00933 canary: gorm-oracle emits
//     multi-row `VALUES (:1,...),(:8,...)`, which only 23ai accepts as a table
//     value constructor (see the ORACLE VERSION FLOOR note on
//     metadataBatchSize in api/metadata_repository.go). 19c/21c would raise
//     ORA-00933, unmapped by dberrors, surfacing as a 500.
//   - BulkUpdate over a mixed existing/new key set exercises the
//     `MERGE INTO ... USING (SELECT ... UNION ALL ...)` path GORM builds from
//     clause.OnConflict, verified only by DryRun during the #666 review.
//   - BulkReplace with 100 entries (the spec-capped batch size) is the issue's
//     acceptance criterion: measure a 100-entry BulkReplace against Oracle.
//     The duration is logged, not asserted, since ADB latency is
//     environment-dependent; the log line is the record.
//
// Run via `make test-integration-oci`.
func TestMetadataBulkWriteOracleIntegration(t *testing.T) {
	db := openAuditAppendOnlyOracleDB(t)
	ctx := context.Background()

	// metadata already exists on the shared ADB (server-migrated); a
	// re-AutoMigrate is not idempotent on gorm-oracle (ORA-01442 on existing
	// NOT NULL columns), so only create it when genuinely absent — same
	// pattern as system_audit_entries in the sibling Oracle tests.
	if !db.Migrator().HasTable(&models.Metadata{}) {
		require.NoError(t, db.AutoMigrate(&models.Metadata{}))
	}

	repo := NewGormMetadataRepository(db, nil, nil)
	const entityType = "threat_model"

	cleanupEntity := func(entityID string) func() {
		return func() {
			_ = db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
				Delete(&models.Metadata{}).Error
		}
	}

	t.Run("BulkCreate multi-row VALUES canary", func(t *testing.T) {
		entityID := uuid.New().String()
		t.Cleanup(cleanupEntity(entityID))

		entries := []Metadata{
			{Key: "color", Value: "red"},
			{Key: "size", Value: "large"},
			{Key: "shape", Value: "round"},
			{Key: "weight", Value: "heavy"},
			{Key: "material", Value: "steel"},
		}
		require.NoError(t, repo.BulkCreate(ctx, entityType, entityID, entries),
			"BulkCreate of 5 entries must succeed against Oracle (ORA-00933 canary for the "+
				"multi-row VALUES table value constructor, 23ai-only)")

		list, err := repo.List(ctx, entityType, entityID)
		require.NoError(t, err)
		require.Len(t, list, len(entries))

		got := make(map[string]string, len(list))
		for _, m := range list {
			got[m.Key] = m.Value
		}
		for _, want := range entries {
			assert.Equal(t, want.Value, got[want.Key], "entry %q must be readable back with its written value", want.Key)
		}
	})

	t.Run("BulkUpdate multi-row MERGE over mixed existing and new keys", func(t *testing.T) {
		entityID := uuid.New().String()
		t.Cleanup(cleanupEntity(entityID))

		// Seed two existing keys, then capture their CREATED_AT so the MERGE
		// path's "insert vs update" branch can be told apart afterward.
		require.NoError(t, repo.BulkCreate(ctx, entityType, entityID, []Metadata{
			{Key: "k1", Value: "orig1"},
			{Key: "k2", Value: "orig2"},
		}))

		var before []models.Metadata
		require.NoError(t, db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
			Order("key").Find(&before).Error)
		require.Len(t, before, 2)
		createdAt := make(map[string]time.Time, 2)
		for _, m := range before {
			createdAt[string(m.Key)] = m.CreatedAt
		}

		err := repo.BulkUpdate(ctx, entityType, entityID, []Metadata{
			{Key: "k1", Value: "updated1"}, // existing -> MERGE UPDATE branch
			{Key: "k2", Value: "updated2"}, // existing -> MERGE UPDATE branch
			{Key: "k3", Value: "new3"},     // new -> MERGE INSERT branch
			{Key: "k4", Value: "new4"},     // new -> MERGE INSERT branch
		})
		require.NoError(t, err, "BulkUpdate multi-row MERGE INTO must succeed against Oracle "+
			"for a batch mixing existing and new keys")

		var after []models.Metadata
		require.NoError(t, db.Where("entity_type = ? AND entity_id = ?", entityType, entityID).
			Order("key").Find(&after).Error)
		require.Len(t, after, 4)

		byKey := make(map[string]models.Metadata, 4)
		for _, m := range after {
			byKey[string(m.Key)] = m
		}

		assert.Equal(t, "updated1", string(byKey["k1"].Value))
		assert.Equal(t, "updated2", string(byKey["k2"].Value))
		assert.Equal(t, "new3", string(byKey["k3"].Value))
		assert.Equal(t, "new4", string(byKey["k4"].Value))

		// CREATED_AT must be preserved (not re-inserted) on the two rows the
		// MERGE routed down its UPDATE branch.
		assert.True(t, createdAt["k1"].Equal(byKey["k1"].CreatedAt),
			"k1 CREATED_AT must be preserved across MERGE UPDATE, was %v now %v", createdAt["k1"], byKey["k1"].CreatedAt)
		assert.True(t, createdAt["k2"].Equal(byKey["k2"].CreatedAt),
			"k2 CREATED_AT must be preserved across MERGE UPDATE, was %v now %v", createdAt["k2"], byKey["k2"].CreatedAt)
	})

	t.Run("BulkReplace of 100 entries (#666 acceptance criterion)", func(t *testing.T) {
		entityID := uuid.New().String()
		t.Cleanup(cleanupEntity(entityID))

		// Seed stale keys that must be gone after the replace.
		require.NoError(t, repo.BulkCreate(ctx, entityType, entityID, []Metadata{
			{Key: "stale1", Value: "x"},
			{Key: "stale2", Value: "y"},
		}))

		const n = 100
		entries := make([]Metadata, n)
		for i := 0; i < n; i++ {
			entries[i] = Metadata{Key: fmt.Sprintf("bulk-key-%03d", i), Value: fmt.Sprintf("bulk-value-%03d", i)}
		}

		start := time.Now()
		require.NoError(t, repo.BulkReplace(ctx, entityType, entityID, entries),
			"BulkReplace of 100 entries (the spec-capped batch size) must succeed against Oracle")
		t.Logf("BulkReplace of %d entries against Oracle ADB took %s (cold)", n, time.Since(start))

		list, err := repo.List(ctx, entityType, entityID)
		require.NoError(t, err)
		require.Len(t, list, n, "all 100 replaced entries must be present")

		got := make(map[string]string, len(list))
		for _, m := range list {
			got[m.Key] = m.Value
		}
		for _, want := range entries {
			assert.Equal(t, want.Value, got[want.Key], "replaced entry %q must be readable back with its written value", want.Key)
		}
		_, hasStale1 := got["stale1"]
		_, hasStale2 := got["stale2"]
		assert.False(t, hasStale1, "prior key stale1 must be gone after BulkReplace")
		assert.False(t, hasStale2, "prior key stale2 must be gone after BulkReplace")

		// Repeat the identical call so a second, warm duration is also on
		// record. A back-to-back same-entity replace can exhaust the retry
		// wrapper with ORA-08177 even with NO concurrent writer: the metadata
		// blocks (and the hot right-edge leaf blocks of its timestamp-leading
		// indexes) carry Oracle's default INITRANS 1, so the second
		// SERIALIZABLE transaction recycles the first one's ITL slot and the
		// rows become unprovable — a false serialization failure, ranked
		// pre-existing (per-transaction/per-block, not per-statement) by the
		// oracle-db-admin review of #666. The durable fixes (INITRANS bump +
		// MOVE/REBUILD, index-set slimming, or a per-site isolation opt-down)
		// are tracked as follow-ups to #666. Until one lands, the documented
		// client-visible outcome of that failure is the retry-exhausted
		// transient classification (mapped to 503 + Retry-After), so this
		// test pins EXACTLY success-or-that: any other error is a regression.
		start = time.Now()
		if repErr := repo.BulkReplace(ctx, entityType, entityID, entries); repErr != nil {
			require.ErrorIs(t, dberrors.Classify(repErr), dberrors.ErrTransient,
				"warm back-to-back BulkReplace may only fail with the retry-exhausted transient classification (503 shape); anything else is a regression")
			t.Logf("warm BulkReplace hit the known ORA-08177/ITL false-serialization failure (documented 503 shape): %v", repErr)
		} else {
			t.Logf("BulkReplace of %d entries against Oracle ADB took %s (repeat)", n, time.Since(start))
			list, err = repo.List(ctx, entityType, entityID)
			require.NoError(t, err)
			assert.Len(t, list, n, "all 100 entries must still be present after the repeat replace")
		}
	})
}
