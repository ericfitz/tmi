//go:build dev || test || integration

package dbschema

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/require"
)

// TestDropRetiredMetadataIndexes_Integration verifies the PostgreSQL drop path
// against a live database. Every existing PostgreSQL deployment runs `DROP
// INDEX IF EXISTS` on first boot after #784, but make test-integration boots
// against a fresh schema built from the already-trimmed model, so that path
// never executes there and the SQLite unit tests only cover the no-op branch.
// This test seeds the four pre-#784 indexes by hand (same columns the retired
// struct tags carried, per 7ec52264), drops them twice, and checks the
// surviving six indexes (five named plus the primary key) are untouched.
// Reuses the DSN/bootstrap helpers from
// TestInstallPostgresDefaultIsolation_Integration (postgres_isolation_integration_test.go).
// SEM@71e0e25225d81c1ed3471b79ceaf458cbe5b17e7: verify the retired metadata indexes are dropped idempotently on PostgreSQL and survivors remain (reads DB)
func TestDropRetiredMetadataIndexes_Integration(t *testing.T) {
	dsn := pgIsolationDSN(t)
	db := openPG(t, dsn)

	table := (&models.Metadata{}).TableName()
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))

	retiredDDL := map[string]string{
		"idx_metadata_created":         "CREATE INDEX IF NOT EXISTS idx_metadata_created ON " + table + " (created_at)",
		"idx_metadata_modified":        "CREATE INDEX IF NOT EXISTS idx_metadata_modified ON " + table + " (modified_at)",
		"idx_metadata_entity_created":  "CREATE INDEX IF NOT EXISTS idx_metadata_entity_created ON " + table + " (entity_type, created_at)",
		"idx_metadata_entity_modified": "CREATE INDEX IF NOT EXISTS idx_metadata_entity_modified ON " + table + " (entity_type, modified_at)",
	}
	for _, name := range retiredMetadataIndexes {
		require.NoError(t, db.Exec(retiredDDL[name]).Error)
	}
	t.Cleanup(func() {
		for _, name := range retiredMetadataIndexes {
			_ = db.Exec("DROP INDEX IF EXISTS " + name).Error
		}
	})

	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, table)
		require.NoError(t, err)
		require.True(t, exists, "seeded index %s must exist before the drop", name)
	}

	require.NoError(t, DropRetiredMetadataIndexes(db))

	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, table)
		require.NoError(t, err)
		require.False(t, exists, "retired index %s must be gone after the drop", name)
	}

	require.NoError(t, DropRetiredMetadataIndexes(db), "must be idempotent")

	for _, name := range metadataInitransIndexes {
		exists, err := metadataIndexExists(db, name, table)
		require.NoError(t, err)
		require.True(t, exists, "surviving index %s must be untouched", name)
	}
	var indexCount int64
	require.NoError(t, db.Raw(
		"SELECT COUNT(*) FROM pg_indexes WHERE tablename = ? AND schemaname = current_schema()",
		table,
	).Scan(&indexCount).Error)
	require.Equal(t, int64(len(metadataInitransIndexes)+1), indexCount,
		"the five named survivors plus the primary key index must be the only indexes left on %s", table)
}
