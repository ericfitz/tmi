//go:build oracle

package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	// Official Oracle GORM driver - uses godror under the hood.
	// Requires CGO and Oracle Instant Client.
	"github.com/godror/godror"
	"github.com/oracle-samples/gorm-oracle/oracle"

	"github.com/ericfitz/tmi/internal/slogging"
)

// utcSessionInitStmt pins the DB-side session time zone to UTC on every new
// physical Oracle session. SESSIONTIMEZONE drives Oracle's implicit
// TIMESTAMP <-> TIMESTAMP WITH TIME ZONE conversions and date arithmetic, so a
// session that inherits the client host's local zone can silently shift values
// by that offset (up to ±14h). See issue #459.
const utcSessionInitStmt = "ALTER SESSION SET TIME_ZONE = '+00:00'"

// getOracleDialector returns the Oracle dialector when built with the oracle
// tag. Requires CGO and the Oracle Instant Client libraries.
//
// UTC session enforcement (#459) is wired PROGRAMMATICALLY via godror's
// ConnectionParams.OnInitStmts, not via DSN-string parameters. godror only
// recognizes `onInit` as a URL query parameter (dsn.ParseConnString sets
// P.OnInitStmts = q["onInit"]); the logfmt-style DSN this code builds carried
// the `onInit="ALTER SESSION ..."` token verbatim but it was never mapped to
// OnInitStmts, so the ALTER SESSION never ran and pooled connections kept the
// client host's local time zone (observed as SESSIONTIMEZONE=+07:00 on a
// UTC+7 host). Building the connector ourselves guarantees it runs.
//
// Uses SkipQuoteIdentifiers: true so Oracle folds all identifiers to unquoted
// uppercase, avoiding the driver's inconsistent quoting in WHERE/ORDER BY.
// See: https://github.com/oracle-samples/gorm-oracle/issues/49
func getOracleDialector(cfg GormConfig) (gorm.Dialector, string) {
	// godror "logfmt" connection string. configDir points at the wallet
	// directory (tnsnames.ora + cwallet.sso) for Oracle ADB. Passwords with
	// special characters are quoted, not URL-encoded, for godror.
	var dsn string
	if cfg.OracleWalletLocation != "" {
		dsn = fmt.Sprintf(`user="%s" password="%s" connectString="%s" configDir="%s"`,
			cfg.User, cfg.Password, cfg.OracleConnectString, cfg.OracleWalletLocation)
	} else {
		dsn = fmt.Sprintf(`user="%s" password="%s" connectString="%s"`,
			cfg.User, cfg.Password, cfg.OracleConnectString)
	}

	params, err := godror.ParseDSN(dsn)
	if err != nil {
		slogging.Get().Error("oracle: failed to parse godror DSN for connectString=%s: %v", cfg.OracleConnectString, err)
		return nil, ""
	}
	// Go-side: interpret/format naive TIMESTAMP and DATE as UTC, and let
	// godror short-circuit its per-connection SESSIONTIMEZONE probe.
	params.Timezone = time.UTC
	// DB-side: pin SESSIONTIMEZONE to UTC for every session (#459).
	params.OnInitStmts = []string{utcSessionInitStmt}
	// Run OnInit on EVERY connection acquisition, not only newly-created
	// sessions. godror's init() skips OnInit when InitOnNewConn is true and the
	// session is not new (`if InitOnNewConn && !isNew { return }`), which left
	// pooled sessions at the client host's local zone (observed +07:00). The
	// ALTER SESSION is a cheap, local statement; running it on each acquire
	// guarantees a UTC session basis across the whole pool.
	params.InitOnNewConn = false

	sqlDB := sql.OpenDB(godror.NewConnector(params))

	base := oracle.New(oracle.Config{
		Conn:                 sqlDB,
		SkipQuoteIdentifiers: true,
	})

	// Wrap the dialector so schema migration is additive on Oracle (#474):
	// new tables/columns/indexes are created, but existing columns are never
	// ALTERed (see additiveOracleMigrator.MigrateColumn). oracle.New returns a
	// *oracle.Dialector.
	dialector := oracleAdditiveDialector{Dialector: base.(*oracle.Dialector)}

	// Return the (non-secret) connect string for the caller's debug log rather
	// than the password-bearing DSN.
	return dialector, cfg.OracleConnectString
}

// oracleAdditiveDialector wraps gorm-oracle's dialector to make schema
// migration ADDITIVE: it delegates everything except Migrator, which returns a
// migrator whose MigrateColumn is a no-op. See #474.
type oracleAdditiveDialector struct {
	*oracle.Dialector
}

// Migrator returns the additive migrator instead of gorm-oracle's default.
func (d oracleAdditiveDialector) Migrator(db *gorm.DB) gorm.Migrator {
	base := d.Dialector.Migrator(db).(oracle.Migrator)
	return additiveOracleMigrator{Migrator: base}
}

// additiveOracleMigrator embeds gorm-oracle's concrete Migrator — so every
// extended migrator interface it satisfies (e.g. BuildIndexOptionsInterface,
// GetTypeAliases, which GORM core type-asserts during AutoMigrate) keeps
// working — and overrides only MigrateColumn.
type additiveOracleMigrator struct {
	oracle.Migrator
}

// MigrateColumn is a deliberate no-op on Oracle (#474). GORM core's AutoMigrate
// calls MigrateColumn for every column that ALREADY exists; gorm-oracle's
// inherited implementation then emits ALTER ... MODIFY for purely cosmetic
// mismatches (godror reports a VARCHAR2 byte buffer size vs the model's char
// count, NUMBER vs NUMBER(1), CLOB vs clob, etc.), which Oracle rejects as
// ORA-01442 ("column already NOT NULL") or ORA-01430 ("column already exists").
// Because AutoMigrate is a single batched call that aborts on the first error,
// that benign rejection silently skipped every subsequent AddColumn — so a
// column newly added to a model never reached an existing Oracle table (schema
// drift, later surfacing as ORA-00904 at query time).
//
// TMI uses a single fresh-schema baseline (#412): there is no version-to-version
// column-type ALTER path, so suppressing in-place column reconciliation loses
// nothing functional. Genuinely-new columns still go through AddColumn, and new
// tables, indexes, and constraints are unaffected. A structural column type
// change remains a drop-and-recreate operation, as it already was.
func (additiveOracleMigrator) MigrateColumn(dst interface{}, field *schema.Field, columnType gorm.ColumnType) error {
	return nil
}

// The create-if-missing operations below are made idempotent because
// gorm-oracle's existence detection is unreliable on Oracle (identifier
// case-folding): GORM core's AutoMigrate decides a column/index/constraint is
// missing and re-issues the create, which Oracle then rejects because the
// object already exists. Swallowing exactly the "already exists" error — per
// operation, not per batch — leaves the existing object in place without
// aborting the rest of the migration (notably a genuinely-new AddColumn later
// in the same table). A create for a genuinely-missing object still succeeds.
// See #474.

// AddColumn is idempotent: ColumnTypes can miss an existing column (e.g. a
// field with an explicit lower-case `column:` tag whose DB name Oracle stores
// upper-case), so AutoMigrate re-issues ADD and Oracle returns ORA-01430
// ("column being added already exists").
func (m additiveOracleMigrator) AddColumn(value interface{}, field string) error {
	return ignoreOracleAlreadyExists(m.Migrator.AddColumn(value, field))
}

// CreateIndex is idempotent: HasIndex can miss an existing index, so the
// re-create is swallowed when Oracle returns ORA-00955.
func (m additiveOracleMigrator) CreateIndex(value interface{}, name string) error {
	return ignoreOracleAlreadyExists(m.Migrator.CreateIndex(value, name))
}

// CreateConstraint is idempotent: HasConstraint can miss an existing
// constraint, so the re-create is swallowed when Oracle returns ORA-00955.
func (m additiveOracleMigrator) CreateConstraint(value interface{}, name string) error {
	return ignoreOracleAlreadyExists(m.Migrator.CreateConstraint(value, name))
}

// ignoreOracleAlreadyExists treats Oracle's "object already exists" errors as
// success — ORA-00955 ("name is already used by an existing object", indexes /
// constraints) and ORA-01430 ("column being added already exists"). Both mean
// the object is already in the desired additive end state. Classification is by
// godror error code (not message substring) to match internal/dberrors and to
// be robust against message-format changes.
func ignoreOracleAlreadyExists(err error) error {
	if err == nil {
		return nil
	}
	var oraErr *godror.OraErr
	if errors.As(err, &oraErr) {
		switch oraErr.Code() {
		case 955, 1430: // ORA-00955 name already used; ORA-01430 column already exists
			return nil
		}
	}
	return err
}
