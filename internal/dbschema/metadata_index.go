// Package dbschema: retirement of the four dead metadata timestamp indexes
// (#784) and the INITRANS raise on METADATA and its surviving indexes (#783).
//
// #783 measured a false ORA-08177 on back-to-back BulkReplace calls against
// the same entity with no concurrent writer: GORM emits a bare CREATE TABLE,
// so metadata blocks carry Oracle's default INITRANS 1 (2 for index blocks),
// and the second transaction recycles the first one's ITL slot on the very
// blocks it just wrote. Ten delete-then-insert metadata callers still run
// inside SERIALIZABLE parent transactions, so INITRANS 16 is defense in
// depth. The aggravator was four timestamp-leading indexes whose right-edge
// leaf block every same-batch row landed on; nothing reads metadata by
// created_at or modified_at, so they are dropped rather than rebuilt.
//
// AutoMigrate never drops an index and never sets physical attributes, so
// both are raw DDL here, on the EnsureUserProviderLookupUnique template.
package dbschema

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// retiredMetadataIndexes are the four timestamp-leading indexes removed from
// models.Metadata in #784, spelled as the model spelled them. AutoMigrate never
// drops an index, so a database created before #784 keeps them until
// DropRetiredMetadataIndexes runs.
var retiredMetadataIndexes = []string{
	"idx_metadata_created",
	"idx_metadata_modified",
	"idx_metadata_entity_created",
	"idx_metadata_entity_modified",
}

// metadataInitransTarget is the ITL slot count EnsureMetadataInitrans raises
// the metadata table and its indexes to. Oracle's default is 1 for table
// blocks and 2 for index blocks; the #783 review recommended 8-16 and the
// approved design picked 16. Each slot costs 24 bytes per block.
const metadataInitransTarget = 16

// metadataInitransIndexes are the named secondary indexes of models.Metadata
// that survive #784, spelled as the model spells them. The primary-key index
// is deliberately absent: gorm-oracle emits a bare PRIMARY KEY clause, so its
// index is system-named (SYS_C...) and is discovered from ALL_CONSTRAINTS at
// runtime by metadataInitransProbe.
var metadataInitransIndexes = []string{
	"idx_metadata_unique",
	"idx_metadata_entity_type_id",
	"idx_metadata_entity_id",
	"idx_metadata_key",
	"idx_metadata_key_value",
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the per-dialect DROP INDEX statement for a retired metadata index (pure)
func retiredMetadataIndexDropDDL(dialect, indexName string) string {
	if dialect == "oracle" {
		// No IF EXISTS on Oracle index DDL; the caller probes the catalog
		// first. Uppercase because TMI runs with SkipQuoteIdentifiers.
		return "DROP INDEX " + strings.ToUpper(indexName)
	}
	return "DROP INDEX IF EXISTS " + indexName
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the ALTER TABLE statements that raise INITRANS and rewrite existing blocks (pure)
func metadataTableInitransDDL(upperTable string) []string {
	// INITRANS on a table applies only to blocks formatted afterwards; MOVE
	// ONLINE rewrites the existing blocks with the new setting while keeping
	// the indexes usable (12.2+). Two statements rather than one MOVE with
	// physical attributes so the catalog change and the block rewrite are
	// separately visible in the log.
	return []string{
		fmt.Sprintf("ALTER TABLE %s INITRANS %d", upperTable, metadataInitransTarget),
		fmt.Sprintf("ALTER TABLE %s MOVE ONLINE", upperTable),
	}
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: build the ALTER INDEX statement that rebuilds an index online with the target INITRANS (pure)
func metadataIndexInitransDDL(upperIndex string) string {
	return fmt.Sprintf("ALTER INDEX %s REBUILD ONLINE INITRANS %d", upperIndex, metadataInitransTarget)
}

// metadataIndexExists reports whether indexName exists on tableName in the
// schema this session resolves unqualified identifiers against.
//
// Oracle probes ALL_INDEXES pinned to CURRENT_SCHEMA for both OWNER and
// TABLE_OWNER (#736): an unqualified DROP INDEX resolves the name in
// CURRENT_SCHEMA, so a foreign-owned same-named index must not read as ours.
// PostgreSQL and SQLite have no such split. gorm's HasIndex is not used for
// the reason userProviderLookupIndexExists gives: on Oracle it binds the
// lowercase tag name against the uppercase catalog and always answers absent.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: probe whether a named index exists on a table in the current schema, per dialect (reads DB)
func metadataIndexExists(db *gorm.DB, indexName, tableName string) (bool, error) {
	var cnt int64
	var err error
	switch db.Name() {
	case "oracle":
		err = db.Raw(
			"SELECT COUNT(*) FROM ALL_INDEXES WHERE INDEX_NAME = ? AND TABLE_NAME = ? "+
				"AND OWNER = "+oracleCurrentSchema+" AND TABLE_OWNER = "+oracleCurrentSchema,
			strings.ToUpper(indexName), strings.ToUpper(tableName),
		).Scan(&cnt).Error
	case "postgres":
		err = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE indexname = ? AND tablename = ? AND schemaname = current_schema()",
			indexName, tableName,
		).Scan(&cnt).Error
	case "sqlite":
		// Test-fixture dialect only.
		err = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ? AND tbl_name = ?",
			indexName, tableName,
		).Scan(&cnt).Error
	default:
		return false, fmt.Errorf("metadataIndexExists: unsupported dialect %q", db.Name())
	}
	return cnt > 0, err
}

// DropRetiredMetadataIndexes removes the four timestamp-leading metadata
// indexes that #784 took out of models.Metadata from a database that still
// carries them. Idempotent: on a current database it costs one catalog query
// per retired name.
//
// Safe, and required, to call on every boot after AutoMigrate, including when
// the #480 fingerprint fast path skips AutoMigrate: AutoMigrate never drops
// an index, so nothing else would ever remove them.
//
// Runs on PostgreSQL and Oracle only. SQLite is the unit-test dialect and
// carries no production data, so it is left untouched (the design's chosen
// no-op path, pinned by TestDropRetiredMetadataIndexes_SQLiteIsNoOp).
//
// Never aborts startup on a DDL failure, only on a failure to read the
// catalog: a retired index that survives is a write-cost nuisance, not a
// correctness problem, and the next boot or `tmi-dbtool --schema` retries.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: drop the retired metadata timestamp indexes if present, else warn and continue (mutates DB)
func DropRetiredMetadataIndexes(db *gorm.DB) error {
	table := (&models.Metadata{}).TableName()
	present, err := requireMigrationTable(db, table, "retired metadata index drop (#784)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	dialect := db.Name()
	if dialect != "postgres" && dialect != "oracle" {
		return nil
	}

	logger := slogging.Get()
	for _, name := range retiredMetadataIndexes {
		var exists bool
		if err := withMigrationRetry("retired metadata index probe", func() error {
			var probeErr error
			exists, probeErr = metadataIndexExists(db, name, table)
			return probeErr
		}); err != nil {
			return fmt.Errorf("failed to check whether %s exists on %s: %w", name, table, err)
		}
		if !exists {
			continue
		}

		ddl := retiredMetadataIndexDropDDL(dialect, name)
		if err := withDDLRetry("retired metadata index drop "+name, func() error {
			return execMigrationDDL(db, ddl)
		}); err != nil {
			// Oracle commits before and after every DDL statement, so a
			// transient failure can arrive after the DROP took effect. Ask
			// the catalog what is true rather than inferring it from the
			// error (the same reasoning as EnsureUserProviderLookupUnique).
			var still bool
			probeErr := withMigrationRetry("retired metadata index post-drop probe", func() error {
				var e error
				still, e = metadataIndexExists(db, name, table)
				return e
			})
			if probeErr == nil && !still {
				logger.Warn("the DROP of %s reported an error (%v) but the index is gone; continuing (#784)", name, err)
				continue
			}
			logger.Error(
				"failed to drop retired metadata index %s: %v. The index is harmless but still costs every metadata write; "+
					"startup is continuing and the next boot or `tmi-dbtool --schema` with an admin-privileged database user retries (#784)",
				name, err)
			continue
		}
		logger.Info("dropped retired metadata index %s from %s (#784)", name, table)
	}
	return nil
}

// metadataInitransState is one reading of the ITL configuration of the
// metadata table and its surviving indexes. Indexes is keyed by the uppercase
// catalog name and holds only the indexes the catalog actually returned; a
// surviving index that is missing from the catalog is warned about by the
// caller and otherwise ignored, since there is nothing to rebuild.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: hold the catalog INITRANS values of the metadata table and its indexes (pure)
type metadataInitransState struct {
	Table   int64
	Indexes map[string]int64
}

// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: report which of the table and its indexes sit below the INITRANS target (pure)
func (s metadataInitransState) below() (tableBelow bool, indexesBelow []string) {
	tableBelow = s.Table < metadataInitransTarget
	for name, ini := range s.Indexes {
		if ini < metadataInitransTarget {
			indexesBelow = append(indexesBelow, name)
		}
	}
	sort.Strings(indexesBelow)
	return tableBelow, indexesBelow
}

// metadataInitransProbe reads INI_TRANS for the metadata table, its
// primary-key index, and its named surviving indexes from the Oracle catalog,
// scoped to the session's CURRENT_SCHEMA (#736). The primary-key index is
// system-named, so its name comes from ALL_CONSTRAINTS.INDEX_NAME for the
// table's CONSTRAINT_TYPE = 'P' row.
//
// The scan targets are untagged on purpose: Oracle returns UPPERCASE result
// labels and an untagged field's DBName follows the active dialect's naming
// strategy, so IndexName binds to INDEX_NAME and IniTrans to INI_TRANS (the
// same convention as userProviderLookupIndexState).
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: fetch INITRANS of the metadata table, PK index, and named indexes from the Oracle catalog (reads DB)
func metadataInitransProbe(db *gorm.DB, table string) (metadataInitransState, error) {
	state := metadataInitransState{Indexes: map[string]int64{}}
	upperTable := strings.ToUpper(table)

	var tableIni []int64
	if err := db.Raw(
		"SELECT INI_TRANS FROM ALL_TABLES WHERE TABLE_NAME = ? AND OWNER = "+oracleCurrentSchema,
		upperTable,
	).Scan(&tableIni).Error; err != nil {
		return state, err
	}
	if len(tableIni) == 0 {
		return state, fmt.Errorf("table %s not found in ALL_TABLES for CURRENT_SCHEMA", upperTable)
	}
	state.Table = tableIni[0]

	var pkIndexes []string
	if err := db.Raw(
		"SELECT INDEX_NAME FROM ALL_CONSTRAINTS WHERE TABLE_NAME = ? AND CONSTRAINT_TYPE = 'P' "+
			"AND INDEX_NAME IS NOT NULL AND OWNER = "+oracleCurrentSchema,
		upperTable,
	).Scan(&pkIndexes).Error; err != nil {
		return state, err
	}

	names := make([]string, 0, len(pkIndexes)+len(metadataInitransIndexes))
	names = append(names, pkIndexes...)
	for _, n := range metadataInitransIndexes {
		names = append(names, strings.ToUpper(n))
	}

	var rows []struct {
		IndexName string
		IniTrans  int64
	}
	if err := db.Raw(
		"SELECT INDEX_NAME, INI_TRANS FROM ALL_INDEXES WHERE TABLE_NAME = ? AND INDEX_NAME IN ? "+
			"AND OWNER = "+oracleCurrentSchema+" AND TABLE_OWNER = "+oracleCurrentSchema,
		upperTable, names,
	).Scan(&rows).Error; err != nil {
		return state, err
	}
	for _, r := range rows {
		state.Indexes[r.IndexName] = r.IniTrans
	}
	return state, nil
}

// EnsureMetadataInitrans raises INITRANS to metadataInitransTarget on the
// Oracle metadata table and its six surviving indexes (#783). Idempotent: on
// a database already at or above the target it costs three catalog queries.
// No-op on every other dialect; INITRANS is an Oracle physical attribute.
//
// Safe, and required, to call on every boot after AutoMigrate, including when
// the #480 fingerprint fast path skips AutoMigrate: physical attributes are
// not part of the model fingerprint, and AutoMigrate could not set them
// anyway.
//
// Order matters at the call sites: DropRetiredMetadataIndexes runs first so
// the four retired indexes are never rebuilt here.
//
// Never aborts startup on a DDL failure, only on a failure to read the
// catalog. ALTER TABLE ... MOVE ONLINE and ALTER INDEX ... REBUILD ONLINE are
// privileged, take DDL locks, and can fail on a busy table or under a
// runtime user without ALTER; in every such case the database is left as it
// was (each statement is its own transaction) and the next boot or
// `tmi-dbtool --schema` retries. The outcome is decided by re-reading the
// catalog after the DDL, not by trusting the DDL's error.
// SEM@2e43fddcc4f977a73637e4f1a1d5798b170d79ed: raise INITRANS on the Oracle metadata table and its indexes to the target, else warn and continue (mutates DB)
func EnsureMetadataInitrans(db *gorm.DB) error {
	if db.Name() != "oracle" {
		return nil
	}
	table := (&models.Metadata{}).TableName()
	present, err := requireMigrationTable(db, table, "metadata INITRANS raise (#783)")
	if err != nil {
		return err
	}
	if !present {
		return nil
	}

	logger := slogging.Get()
	upperTable := strings.ToUpper(table)

	var state metadataInitransState
	if err := withMigrationRetry("metadata INITRANS probe", func() error {
		var probeErr error
		state, probeErr = metadataInitransProbe(db, table)
		return probeErr
	}); err != nil {
		return fmt.Errorf("failed to read the INITRANS settings of %s: %w", upperTable, err)
	}
	for _, n := range metadataInitransIndexes {
		if _, ok := state.Indexes[strings.ToUpper(n)]; !ok {
			logger.Warn("%s not found on %s in CURRENT_SCHEMA; skipping its INITRANS raise (#783)", strings.ToUpper(n), upperTable)
		}
	}

	tableBelow, indexesBelow := state.below()
	if !tableBelow && len(indexesBelow) == 0 {
		return nil
	}
	logger.Info("raising INITRANS to %d on %s (table at %d; indexes below target: %v) (#783)",
		metadataInitransTarget, upperTable, state.Table, indexesBelow)

	if tableBelow {
		for _, ddl := range metadataTableInitransDDL(upperTable) {
			if err := withDDLRetry("metadata INITRANS "+ddl, func() error {
				return execMigrationDDL(db, ddl)
			}); err != nil {
				logger.Error("%q failed: %v (#783)", ddl, err)
				break
			}
		}
	}
	for _, name := range indexesBelow {
		ddl := metadataIndexInitransDDL(name)
		if err := withDDLRetry("metadata INITRANS "+ddl, func() error {
			return execMigrationDDL(db, ddl)
		}); err != nil {
			logger.Error("%q failed: %v (#783)", ddl, err)
		}
	}

	if err := withMigrationRetry("metadata INITRANS post-DDL probe", func() error {
		var probeErr error
		state, probeErr = metadataInitransProbe(db, table)
		return probeErr
	}); err != nil {
		return fmt.Errorf("failed to verify the INITRANS settings of %s after raising them: %w", upperTable, err)
	}
	tableBelow, indexesBelow = state.below()
	if tableBelow || len(indexesBelow) > 0 {
		logger.Error(
			"INITRANS on %s is still below %d after the raise (table at %d; indexes below target: %v). "+
				"Back-to-back metadata writes on one entity inside a SERIALIZABLE transaction remain exposed to a false ORA-08177; "+
				"startup is continuing and the next boot or `tmi-dbtool --schema` with an admin-privileged database user retries (#783)",
			upperTable, metadataInitransTarget, state.Table, indexesBelow)
		return nil
	}
	logger.Info("INITRANS on %s and its %d surviving indexes is now >= %d (#783)", upperTable, len(state.Indexes), metadataInitransTarget)
	return nil
}
