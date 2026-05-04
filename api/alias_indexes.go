package api

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// AddAliasUniqueIndexes creates the seven post-backfill unique indexes.
// Idempotent: skips indexes that already exist via hasIndex checks.
func AddAliasUniqueIndexes(ctx context.Context, db *gorm.DB) error {
	logger := slogging.Get()
	type idx struct {
		name    string
		table   string
		columns string // SQL column list
	}
	indexes := []idx{
		{"uniq_threat_models_alias", "threat_models", "(alias)"},
		{"uniq_diagrams_tm_alias", "diagrams", "(threat_model_id, alias)"},
		{"uniq_threats_tm_alias", "threats", "(threat_model_id, alias)"},
		{"uniq_assets_tm_alias", "assets", "(threat_model_id, alias)"},
		{"uniq_repositories_tm_alias", "repositories", "(threat_model_id, alias)"},
		{"uniq_notes_tm_alias", "notes", "(threat_model_id, alias)"},
		{"uniq_documents_tm_alias", "documents", "(threat_model_id, alias)"},
	}

	for _, i := range indexes {
		table := tableNameForDialect(db, i.table)
		if hasIndex(db, table, i.name) {
			logger.Debug("alias index %s already exists; skipping", i.name)
			continue
		}
		sql := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s %s", i.name, table, i.columns)
		if err := db.WithContext(ctx).Exec(sql).Error; err != nil {
			return fmt.Errorf("create %s: %w", i.name, err)
		}
		logger.Info("created unique index %s", i.name)
	}
	return nil
}

// hasIndex checks whether a named index exists on the given table.
// Uses dialect-specific system catalog queries (pg_indexes, USER_INDEXES,
// sqlite_master). Returns false when the index does not exist or on query error.
func hasIndex(db *gorm.DB, table, indexName string) bool {
	switch db.Name() {
	case DialectPostgres:
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM pg_indexes WHERE schemaname = current_schema() AND tablename = ? AND indexname = ?",
			table, indexName,
		).Scan(&n).Error
		return n > 0
	case DialectOracle:
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM USER_INDEXES WHERE TABLE_NAME = UPPER(?) AND INDEX_NAME = UPPER(?)",
			table, indexName,
		).Scan(&n).Error
		return n > 0
	case DialectSQLite:
		var n int64
		_ = db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?",
			indexName,
		).Scan(&n).Error
		return n > 0
	}
	return false
}
