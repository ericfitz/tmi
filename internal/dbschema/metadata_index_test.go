package dbschema

import (
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newMetadataIndexTestDB builds an in-memory SQLite database carrying the real
// models.Metadata table plus the four pre-#784 timestamp indexes, i.e. the
// state every database created before #784 is in. The real model is used
// rather than a test-local mirror because the point of these tests is the
// model's own index set. IF NOT EXISTS because AutoMigrate on the
// still-tagged model already creates these same indexes from the model's own
// tags; this fixture must represent the pre-#784 state both before and after
// Task 3 strips those tags.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build an in-memory SQLite DB with the metadata table and its pre-#784 retired indexes (pure)
func newMetadataIndexTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))
	for _, name := range retiredMetadataIndexes {
		require.NoError(t, db.Exec("CREATE INDEX IF NOT EXISTS "+name+" ON metadata (created_at)").Error)
	}
	return db
}

// TestRetiredMetadataIndexDropDDL pins the per-dialect DROP text: PostgreSQL
// has IF EXISTS; Oracle does not and folds unquoted names to upper case, so a
// lowercase DROP would raise ORA-01418.
func TestRetiredMetadataIndexDropDDL(t *testing.T) {
	assert.Equal(t, "DROP INDEX IF EXISTS idx_metadata_created", retiredMetadataIndexDropDDL("postgres", "idx_metadata_created"))
	assert.Equal(t, "DROP INDEX IDX_METADATA_CREATED", retiredMetadataIndexDropDDL("oracle", "idx_metadata_created"))
}

// TestMetadataInitransDDL pins the Oracle statements: a single MOVE ONLINE
// with INITRANS in its physical_attributes_clause, so the catalog only ever
// reads the new INITRANS if the block rewrite actually committed (a split
// INITRANS/MOVE ONLINE let a failed MOVE hide behind an already-raised
// catalog value, oracle-db-admin review, #783); each index is rebuilt online
// with the new INITRANS.
func TestMetadataInitransDDL(t *testing.T) {
	assert.Equal(t, "ALTER TABLE METADATA MOVE ONLINE INITRANS 16", metadataTableInitransDDL("METADATA"))
	assert.Equal(t, "ALTER INDEX IDX_METADATA_KEY REBUILD ONLINE INITRANS 16", metadataIndexInitransDDL("IDX_METADATA_KEY"))
}

// TestMetadataIndexExists_SQLite covers the sqlite branch of the probe, which
// the no-op test in Task 4 relies on to prove nothing was dropped.
func TestMetadataIndexExists_SQLite(t *testing.T) {
	db := newMetadataIndexTestDB(t)

	exists, err := metadataIndexExists(db, "idx_metadata_created", "metadata")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = metadataIndexExists(db, "idx_metadata_does_not_exist", "metadata")
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestMetadataInitransIndexes_NamesMatchModel pins the survivor list against
// the model: every name EnsureMetadataInitrans will rebuild must be an index
// AutoMigrate actually creates, and no retired name may survive in the model.
func TestMetadataInitransIndexes_NamesMatchModel(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Metadata{}))

	for _, name := range metadataInitransIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.True(t, exists, "surviving index %s must be created by the model", name)
	}
	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.False(t, exists, "retired index %s must no longer be in the model (#784)", name)
	}
}

// TestDropRetiredMetadataIndexes_SQLiteIsNoOp pins the design decision that
// the drop only runs on the two production dialects: on the SQLite test
// fixture it must return nil and leave every retired index in place.
func TestDropRetiredMetadataIndexes_SQLiteIsNoOp(t *testing.T) {
	db := newMetadataIndexTestDB(t)

	require.NoError(t, DropRetiredMetadataIndexes(db))
	require.NoError(t, DropRetiredMetadataIndexes(db), "must be idempotent")

	for _, name := range retiredMetadataIndexes {
		exists, err := metadataIndexExists(db, name, "metadata")
		require.NoError(t, err)
		assert.True(t, exists, "SQLite is a test-only dialect; %s must be left alone", name)
	}
}

// TestDropRetiredMetadataIndexes_NoTable covers a database whose metadata
// table does not exist yet: nothing to do, no error.
func TestDropRetiredMetadataIndexes_NoTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, DropRetiredMetadataIndexes(db))
}

// TestMetadataInitransState_Below pins the skip decision: only objects
// strictly below the target need DDL, and the index list is sorted so the
// log line and the DDL order are stable across boots.
func TestMetadataInitransState_Below(t *testing.T) {
	state := metadataInitransState{
		Table: 1,
		Indexes: map[string]int64{
			"SYS_C0012345":                2,
			"IDX_METADATA_KEY":            16,
			"IDX_METADATA_ENTITY_TYPE_ID": 2,
			"IDX_METADATA_UNIQUE":         32,
		},
	}
	tableBelow, indexesBelow := state.below()
	assert.True(t, tableBelow)
	assert.Equal(t, []string{"IDX_METADATA_ENTITY_TYPE_ID", "SYS_C0012345"}, indexesBelow)

	done := metadataInitransState{Table: 16, Indexes: map[string]int64{"IDX_METADATA_KEY": 16}}
	tableBelow, indexesBelow = done.below()
	assert.False(t, tableBelow)
	assert.Empty(t, indexesBelow)
}

// TestEnsureMetadataInitrans_NonOracleIsNoOp covers every non-Oracle dialect:
// INITRANS is an Oracle physical attribute, so the step returns before it
// even looks for the table.
func TestEnsureMetadataInitrans_NonOracleIsNoOp(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, EnsureMetadataInitrans(db), "no table, no Oracle, no error")

	db = newMetadataIndexTestDB(t)
	require.NoError(t, EnsureMetadataInitrans(db))
	require.NoError(t, EnsureMetadataInitrans(db), "must be idempotent")
}
