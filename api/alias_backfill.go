package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dbschema"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// subObjectTable describes a sub-object table participating in alias backfill.
type subObjectTable struct {
	tableName  string // schema-side table name (e.g., "notes")
	objectType string // alias_counters object_type value (e.g., "note")
}

// subObjectTables is the canonical list of tables that need backfill, in
// dependency-safe order (these tables only depend on threat_models).
var subObjectTables = []subObjectTable{
	{"diagrams", "diagram"},
	{"threats", "threat"},
	{"assets", "asset"},
	{"repositories", "repository"},
	{"notes", "note"},
	{"documents", "document"},
}

// RunAliasBackfill brings all aliased tables to a fully-populated state.
// Idempotent: skips tables whose rows all have alias > 0. Acquires a
// cross-DB advisory lock so multi-replica startups serialize. For dialects
// that do not support advisory locks (e.g., SQLite in tests), the lock is
// skipped with a warning.
func RunAliasBackfill(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()

	release, err := dbschema.AcquireMigrationLock(ctx, db, "tmi_alias_backfill")
	if err != nil {
		if strings.Contains(err.Error(), "unsupported dialect") {
			// SQLite (used in tests) does not support advisory locks.
			// Log a warning and proceed without the lock — single-process
			// in-memory SQLite is inherently single-writer.
			logger.Warn("alias backfill: skipping advisory lock for dialect %q: %v", db.Name(), err)
			release = func() {}
		} else {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
	}
	defer release()

	if err := backfillThreatModelAliases(ctx, db, logger); err != nil {
		return fmt.Errorf("backfill threat_models: %w", err)
	}
	for _, t := range subObjectTables {
		if err := backfillSubObjectAliases(ctx, db, t, logger); err != nil {
			return fmt.Errorf("backfill %s: %w", t.tableName, err)
		}
	}
	return nil
}

func backfillThreatModelAliases(ctx context.Context, db *gorm.DB, logger *slogging.Logger) error {
	const tn = "threat_models"
	tmTable := tableNameForDialect(db, tn)

	// Fast-skip if no rows have alias=0.
	var pending int64
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE alias = 0", tmTable),
	).Scan(&pending).Error; err != nil {
		return fmt.Errorf("count pending: %w", err)
	}
	if pending == 0 {
		logger.Debug("alias backfill: threat_models is fully populated, skipping")
		return nil
	}

	logger.Info("alias backfill: assigning aliases to %d threat_models rows", pending)

	if err := bulkAssignThreatModelAliases(ctx, db, tmTable); err != nil {
		return err
	}

	// Initialize the counter to MAX(alias) + 1.
	var maxAlias int32
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COALESCE(MAX(alias), 0) FROM %s", tmTable),
	).Scan(&maxAlias).Error; err != nil {
		return fmt.Errorf("read MAX(alias): %w", err)
	}
	counter := models.AliasCounter{ParentID: "__global__", ObjectType: "threat_model", NextAlias: maxAlias + 1}
	if err := db.WithContext(ctx).Save(&counter).Error; err != nil {
		return fmt.Errorf("save counter: %w", err)
	}
	logger.Info("alias backfill: threat_models complete; next_alias=%d", maxAlias+1)
	return nil
}

func backfillSubObjectAliases(ctx context.Context, db *gorm.DB, t subObjectTable, logger *slogging.Logger) error {
	resolvedTable := tableNameForDialect(db, t.tableName)

	// Skip tables that have not been migrated yet (e.g., in narrow unit-test DBs).
	if !db.Migrator().HasTable(resolvedTable) {
		logger.Debug("alias backfill: table %s does not exist, skipping", t.tableName)
		return nil
	}

	var pending int64
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE alias = 0", resolvedTable),
	).Scan(&pending).Error; err != nil {
		return fmt.Errorf("count pending: %w", err)
	}
	if pending == 0 {
		logger.Debug("alias backfill: %s is fully populated, skipping", t.tableName)
		return nil
	}

	logger.Info("alias backfill: assigning aliases to %d %s rows", pending, t.tableName)

	if err := bulkAssignSubObjectAliases(ctx, db, resolvedTable); err != nil {
		return err
	}

	// Initialize per-TM counters from MAX(alias).
	type counterRow struct {
		ThreatModelID string
		MaxAlias      int32
	}
	var rows []counterRow
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf(
			"SELECT threat_model_id, MAX(alias) AS max_alias FROM %s GROUP BY threat_model_id",
			resolvedTable,
		),
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read MAX(alias) per TM: %w", err)
	}
	for _, r := range rows {
		counter := models.AliasCounter{ParentID: r.ThreatModelID, ObjectType: t.objectType, NextAlias: r.MaxAlias + 1}
		if err := db.WithContext(ctx).Save(&counter).Error; err != nil {
			return fmt.Errorf("save counter for tm=%s: %w", r.ThreatModelID, err)
		}
	}
	logger.Info("alias backfill: %s complete (%d threat models)", t.tableName, len(rows))
	return nil
}

// bulkAssignThreatModelAliases assigns aliases to rows where alias = 0,
// continuing from the existing high-water mark (MAX(alias) among rows with
// alias > 0) so previously-allocated aliases are never reused. Rows are
// numbered in created_at ASC, id ASC order. Dialect-specific.
func bulkAssignThreatModelAliases(ctx context.Context, db *gorm.DB, tmTable string) error {
	// Read the current high-water mark so the new aliases continue past it.
	var startAt int32
	if err := db.WithContext(ctx).Raw(
		fmt.Sprintf("SELECT COALESCE(MAX(alias), 0) FROM %s WHERE alias > 0", tmTable),
	).Scan(&startAt).Error; err != nil {
		return fmt.Errorf("read existing MAX(alias): %w", err)
	}

	switch db.Name() {
	case DialectPostgres:
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s t SET alias = numbered.rn + %d
			FROM numbered WHERE t.id = numbered.id
		`, tmTable, tmTable, startAt)
		return db.WithContext(ctx).Exec(sql).Error

	case DialectOracle:
		sql := fmt.Sprintf(`
			MERGE INTO %s t USING (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			) numbered
			ON (t.id = numbered.id)
			WHEN MATCHED THEN UPDATE SET t.alias = numbered.rn + %d
		`, tmTable, tmTable, startAt)
		return db.WithContext(ctx).Exec(sql).Error

	case DialectSQLite:
		sql := fmt.Sprintf(`
			WITH numbered AS (
				SELECT id, ROW_NUMBER() OVER (ORDER BY created_at ASC, id ASC) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s SET alias = (SELECT rn FROM numbered WHERE numbered.id = %s.id) + %d
			WHERE id IN (SELECT id FROM numbered)
		`, tmTable, tmTable, tmTable, startAt)
		return db.WithContext(ctx).Exec(sql).Error

	default:
		return fmt.Errorf("alias backfill: unsupported dialect %q", db.Name())
	}
}

// bulkAssignSubObjectAliases assigns aliases to rows where alias = 0,
// scoped per threat_model_id and continuing from each scope's existing
// high-water mark (so previously-allocated aliases within a TM are never
// reused). Rows within each partition are numbered in created_at ASC, id ASC
// order. Dialect-specific.
//
// The "starts" CTE computes MAX(alias) per threat_model_id from rows with
// alias > 0; rows with alias = 0 then get rn + COALESCE(start, 0).
func bulkAssignSubObjectAliases(ctx context.Context, db *gorm.DB, table string) error {
	switch db.Name() {
	case DialectPostgres:
		sql := fmt.Sprintf(`
			WITH starts AS (
				SELECT threat_model_id, MAX(alias) AS start_at
				FROM %s WHERE alias > 0
				GROUP BY threat_model_id
			),
			numbered AS (
				SELECT id, threat_model_id,
					ROW_NUMBER() OVER (
						PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
					) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s t
			SET alias = numbered.rn + COALESCE(starts.start_at, 0)
			FROM numbered LEFT JOIN starts ON starts.threat_model_id = numbered.threat_model_id
			WHERE t.id = numbered.id
		`, table, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	case DialectOracle:
		sql := fmt.Sprintf(`
			MERGE INTO %s t USING (
				SELECT n.id, n.rn + COALESCE(s.start_at, 0) AS new_alias
				FROM (
					SELECT id, threat_model_id,
						ROW_NUMBER() OVER (
							PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
						) AS rn
					FROM %s WHERE alias = 0
				) n
				LEFT JOIN (
					SELECT threat_model_id, MAX(alias) AS start_at
					FROM %s WHERE alias > 0
					GROUP BY threat_model_id
				) s ON s.threat_model_id = n.threat_model_id
			) numbered
			ON (t.id = numbered.id)
			WHEN MATCHED THEN UPDATE SET t.alias = numbered.new_alias
		`, table, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	case DialectSQLite:
		sql := fmt.Sprintf(`
			WITH starts AS (
				SELECT threat_model_id, MAX(alias) AS start_at
				FROM %s WHERE alias > 0
				GROUP BY threat_model_id
			),
			numbered AS (
				SELECT id, threat_model_id,
					ROW_NUMBER() OVER (
						PARTITION BY threat_model_id ORDER BY created_at ASC, id ASC
					) AS rn
				FROM %s WHERE alias = 0
			)
			UPDATE %s SET alias = (
				SELECT n.rn + COALESCE(s.start_at, 0)
				FROM numbered n LEFT JOIN starts s ON s.threat_model_id = n.threat_model_id
				WHERE n.id = %s.id
			)
			WHERE id IN (SELECT id FROM numbered)
		`, table, table, table, table)
		return db.WithContext(ctx).Exec(sql).Error

	default:
		return fmt.Errorf("alias backfill: unsupported dialect %q", db.Name())
	}
}

// tableNameForDialect returns the table name with appropriate casing for the
// dialect (lowercase on PG/SQLite, UPPERCASE on Oracle when
// UseUppercaseTableNames is set).
func tableNameForDialect(db *gorm.DB, name string) string {
	if db.Name() == DialectOracle {
		// Match the project's UseUppercaseTableNames pattern.
		runes := []rune(name)
		for i, r := range runes {
			if r >= 'a' && r <= 'z' {
				runes[i] = r - 32
			}
		}
		return string(runes)
	}
	return name
}
