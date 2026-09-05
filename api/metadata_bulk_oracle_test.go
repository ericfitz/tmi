//go:build oracle

package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
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
//   - The final subtest drives DropRetiredMetadataIndexes and
//     EnsureMetadataInitrans against the live catalog and asserts INI_TRANS
//     >= 16 on METADATA and its six surviving indexes and that the four
//     retired timestamp indexes are absent (#783, #784 acceptance).
//
// Run via `make test-integration-oci`.
// SEM@7702bac58f9ef99ee9208eaf02746834d824b686: verify metadata bulk writes, INITRANS raise, and retired-index removal against Oracle ADB (reads DB)
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
		// record — and as the #783 regression pin: under the #451 SERIALIZABLE
		// default this back-to-back same-entity replace exhausted the retry
		// wrapper with a FALSE ORA-08177 (INITRANS-1 ITL slot recycling on the
		// blocks the first call just wrote; time-independent, so retries
		// couldn't help). BulkReplace now opts down to READ COMMITTED (see the
		// comment in metadata_repository.go BulkReplace, approved on #783),
		// which makes that failure impossible, so this asserts plain success.
		start = time.Now()
		require.NoError(t, repo.BulkReplace(ctx, entityType, entityID, entries),
			"warm back-to-back BulkReplace must succeed deterministically at READ COMMITTED (#783)")
		t.Logf("BulkReplace of %d entries against Oracle ADB took %s (repeat)", n, time.Since(start))
		list, err = repo.List(ctx, entityType, entityID)
		require.NoError(t, err)
		assert.Len(t, list, n, "all 100 entries must still be present after the repeat replace")
	})

	t.Run("INITRANS >= 16 and retired timestamp indexes absent (#783, #784)", func(t *testing.T) {
		// Drive the two ensure-steps directly so the assertion does not
		// depend on which build last migrated the shared ADB, then call
		// them again to pin idempotence. Both must return nil on a
		// catalog-readable database whatever the DDL outcome; the probes
		// below are what decide pass/fail.
		require.NoError(t, dbschema.DropRetiredMetadataIndexes(db))
		require.NoError(t, dbschema.EnsureMetadataInitrans(db))
		require.NoError(t, dbschema.DropRetiredMetadataIndexes(db), "must be idempotent")
		require.NoError(t, dbschema.EnsureMetadataInitrans(db), "must be idempotent")

		// Same CURRENT_SCHEMA scoping as the production probes (#736);
		// spelled out rather than imported so the test asserts against what
		// Oracle holds, independently of the code under test.
		const owner = "SYS_CONTEXT('USERENV','CURRENT_SCHEMA')"

		var tableIni []int64
		require.NoError(t, db.Raw(
			"SELECT INI_TRANS FROM ALL_TABLES WHERE TABLE_NAME = 'METADATA' AND OWNER = "+owner,
		).Scan(&tableIni).Error)
		require.Len(t, tableIni, 1, "METADATA must exist in CURRENT_SCHEMA")
		assert.GreaterOrEqual(t, tableIni[0], int64(16), "METADATA INI_TRANS (#783)")

		var pk []string
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME FROM ALL_CONSTRAINTS WHERE TABLE_NAME = 'METADATA' AND CONSTRAINT_TYPE = 'P' "+
				"AND INDEX_NAME IS NOT NULL AND OWNER = "+owner,
		).Scan(&pk).Error)
		require.Len(t, pk, 1, "METADATA must have exactly one primary-key constraint backed by an index")

		survivors := append([]string{pk[0]},
			"IDX_METADATA_UNIQUE", "IDX_METADATA_ENTITY_TYPE_ID", "IDX_METADATA_ENTITY_ID",
			"IDX_METADATA_KEY", "IDX_METADATA_KEY_VALUE")
		var rows []struct {
			IndexName string
			IniTrans  int64
		}
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME, INI_TRANS FROM ALL_INDEXES WHERE TABLE_NAME = 'METADATA' AND INDEX_NAME IN ? "+
				"AND OWNER = "+owner+" AND TABLE_OWNER = "+owner,
			survivors,
		).Scan(&rows).Error)
		got := make(map[string]int64, len(rows))
		for _, r := range rows {
			got[r.IndexName] = r.IniTrans
		}
		for _, name := range survivors {
			ini, ok := got[name]
			assert.True(t, ok, "surviving index %s must exist on METADATA", name)
			assert.GreaterOrEqual(t, ini, int64(16), "%s INI_TRANS (#783)", name)
		}

		retired := []string{"IDX_METADATA_CREATED", "IDX_METADATA_MODIFIED", "IDX_METADATA_ENTITY_CREATED", "IDX_METADATA_ENTITY_MODIFIED"}
		var leftover []string
		require.NoError(t, db.Raw(
			"SELECT INDEX_NAME FROM ALL_INDEXES WHERE TABLE_NAME = 'METADATA' AND INDEX_NAME IN ? "+
				"AND OWNER = "+owner+" AND TABLE_OWNER = "+owner,
			retired,
		).Scan(&leftover).Error)
		assert.Empty(t, leftover, "retired timestamp indexes must be gone from METADATA (#784)")
	})
}
