// cmd/dbtool/backfill_empty_strings.go
package main

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/ericfitz/tmi/api"
	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/test/testdb"
	"gorm.io/gorm/schema"
)

// nullableStringColumnType identifies the two Go types whose columns can
// carry a stored empty string where NULL is now the write-side normal form (#700).
var nullableStringColumnTypes = []reflect.Type{
	reflect.TypeOf(models.NullableDBVarchar{}),
	reflect.TypeOf(models.NullableDBText{}),
}

// nullableStringColumn identifies one (table, column) pair backed by a
// models.NullableDBVarchar or models.NullableDBText field.
// SEM@a590912b68a0537a660bf71dd19959b3db635967: DTO naming a nullable-string-typed table column targeted by the #700 backfill (pure)
type nullableStringColumn struct {
	Table  string
	Column string
}

// backfillSQL returns the idempotent UPDATE statement that normalizes a
// stored empty string to NULL for this (table, column) pair (#700).
// SEM@a590912b68a0537a660bf71dd19959b3db635967: build the empty-to-NULL backfill UPDATE statement for one column (pure)
func (c nullableStringColumn) backfillSQL() string {
	return fmt.Sprintf(`UPDATE %s SET %s = NULL WHERE %s = ''`, c.Table, c.Column, c.Column)
}

// isNullableStringField reports whether a parsed schema field's Go type is
// models.NullableDBVarchar or models.NullableDBText (pure)
// SEM@a590912b68a0537a660bf71dd19959b3db635967: check whether a field type is a nullable-string wrapper type (pure)
func isNullableStringField(fieldType reflect.Type) bool {
	for _, t := range nullableStringColumnTypes {
		if fieldType == t {
			return true
		}
	}
	return false
}

// enumerateNullableStringColumns walks every registered GORM model
// (api.GetAllModels) and mechanically derives the (table, column) pairs
// backed by NullableDBVarchar/NullableDBText fields, using GORM's own
// schema parser so table/column names match exactly what AutoMigrate and
// the ORM itself would use — no hand-maintained list to drift (#700).
//
// namer controls table/column-name casing; callers pass schema.NamingStrategy{}
// for the PostgreSQL (lowercase) convention this tool targets.
// SEM@a590912b68a0537a660bf71dd19959b3db635967: derive (table, column) pairs for every nullable-string field across all registered models (pure)
func enumerateNullableStringColumns(namer schema.Namer) ([]nullableStringColumn, error) {
	cacheStore := &sync.Map{}
	var pairs []nullableStringColumn

	for _, model := range api.GetAllModels() {
		sch, err := schema.Parse(model, cacheStore, namer)
		if err != nil {
			return nil, fmt.Errorf("failed to parse schema for %T: %w", model, err)
		}
		for _, field := range sch.Fields {
			// sch.Fields includes every struct field GORM parsed, including
			// gorm:"-" and relation-only fields, which carry an empty DBName
			// (no backing column). Skip those — backfillSQL() would otherwise
			// emit "UPDATE t SET  = NULL WHERE  = ''" for them.
			if field.DBName == "" {
				continue
			}
			if isNullableStringField(field.FieldType) {
				pairs = append(pairs, nullableStringColumn{
					Table:  sch.Table,
					Column: field.DBName,
				})
			}
		}
	}

	return pairs, nil
}

// runBackfillEmptyStrings normalizes stored empty strings to NULL for every
// NullableDBVarchar/NullableDBText column, undoing pre-#700 writes that
// bypassed Value()'s empty-to-NULL normalization (e.g. raw *string values
// placed directly into a GORM update map, which never invoke the column's
// driver.Valuer).
//
// Note: for a column inside a composite unique index (e.g. group_members'
// idx_gm_group_user_type over group_internal_uuid/user_internal_uuid/
// subject_type), converting a stored empty string to NULL relaxes an
// enforcement PostgreSQL was previously providing there — the row keyed by
// an empty-string group_internal_uuid was unique-constrained, but the same
// row keyed by NULL is not, on either engine. The backfill itself cannot
// produce a duplicate-key error (a second empty-string row could never have
// been inserted while the constraint held), and the result converges
// PostgreSQL onto Oracle's pre-existing behavior, where an empty string was
// already NULL. See #700's audit of GroupMember.UserInternalUUID.
//
// PostgreSQL only: Oracle already coerces an empty string to NULL on write,
// so there is nothing to backfill there. This guard also protects against a
// subtler failure mode: even if it were removed, an empty-string WHERE
// comparison is equivalent to comparing against NULL on Oracle (never true,
// so the UPDATE would silently match zero rows) — but the statement would
// fail first on ORA-00942, since the lowercase unquoted
// identifiers here don't resolve against Oracle's uppercase table/column
// names. Idempotent re-runs are cheap and safe: the WHERE clause only ever
// matches rows still carrying the pre-#700 value, so an already-normalized
// column is a no-op on a later run, including a retry after a failure part
// way through the loop below.
// SEM@a590912b68a0537a660bf71dd19959b3db635967: backfill empty-string nullable columns to NULL on PostgreSQL, idempotently (writes DB)
func runBackfillEmptyStrings(db *testdb.TestDB, dryRun, verbose bool) error {
	log := slogging.Get()

	if db.DialectName() != string(authdb.DatabaseTypePostgres) {
		return fmt.Errorf(
			"--backfill-empty-strings is PostgreSQL-only (connected dialect: %s); Oracle already stores '' as NULL",
			db.DialectName(),
		)
	}

	pairs, err := enumerateNullableStringColumns(schema.NamingStrategy{})
	if err != nil {
		return fmt.Errorf("failed to enumerate nullable string columns: %w", err)
	}
	log.Info("Checking %d nullable string column(s) for backfill", len(pairs))

	// Each column is its own autocommit UPDATE, not one transaction spanning
	// every table: that keeps row-lock/WAL footprint bounded per statement,
	// and a retry (via WithRetryableGormTransaction) would just re-run
	// already-committed work for no benefit on an offline maintenance job.
	var totalRows int64
	for _, pair := range pairs {
		sqlStr := pair.backfillSQL()
		if dryRun || verbose {
			log.Info("[backfill-empty-strings] %s", sqlStr)
		}
		if dryRun {
			continue
		}

		result := db.DB().Exec(sqlStr)
		if result.Error != nil {
			return fmt.Errorf(
				"failed to backfill %s.%s (rerun is safe -- already-normalized columns are no-ops): %w",
				pair.Table, pair.Column, dberrors.Classify(result.Error),
			)
		}
		if result.RowsAffected > 0 {
			log.Info("Backfilled %d row(s) in %s.%s", result.RowsAffected, pair.Table, pair.Column)
		}
		totalRows += result.RowsAffected
	}

	if dryRun {
		log.Info("[DRY RUN] Would check %d column(s); no rows modified", len(pairs))
	} else {
		log.Info("Backfill complete: %d row(s) normalized across %d column(s)", totalRows, len(pairs))
	}
	return nil
}
