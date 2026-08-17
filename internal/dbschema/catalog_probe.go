// Package dbschema: owner-aware catalog probes for the startup schema checks
// (#736).
//
// The pre-migration invariant checks (CheckDuplicateUserProviderIdentities,
// #724; EnsureSparseUserEmailIndex, #720) each gate on "does the users table
// exist?" before doing any work. That gate used gorm.Migrator().HasTable,
// which on Oracle resolves through USER_TABLES -- a view scoped to objects
// *owned by the connected user*. On a deployment where the runtime user does
// not own the schema (the DDL-less-runtime-user model cmd/server/main.go
// already contemplates), USER_TABLES and USER_INDEXES are empty for that
// session even though USERS exists under another owner, so both checks
// returned nil having checked nothing, silently, with no log line.
//
// Everything here probes ALL_TABLES / ALL_INDEXES filtered to
// SYS_CONTEXT('USERENV','CURRENT_SCHEMA') instead: the schema an unqualified
// identifier actually resolves against on this session, which is exactly the
// schema TMI's own unquoted DDL reads and writes. That covers both the
// owner-is-the-connected-user case (CURRENT_SCHEMA defaults to the connected
// user) and the separated case (a logon trigger or ALTER SESSION SET
// CURRENT_SCHEMA points the runtime user at the owning schema). When the
// table still isn't found, the caller logs rather than returning silently.
package dbschema

import (
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// oracleCurrentSchema is the owner predicate every Oracle catalog probe in
// this package filters on. SYS_CONTEXT('USERENV','CURRENT_SCHEMA') is the
// schema unqualified names resolve against on this session -- not
// necessarily SESSION_USER, which is what USER_TABLES / USER_INDEXES are
// implicitly scoped to (#736).
const oracleCurrentSchema = "SYS_CONTEXT('USERENV','CURRENT_SCHEMA')"

// migrationTableExists reports whether table is visible in the schema this
// session resolves unqualified identifiers against.
//
// On Oracle this is ALL_TABLES filtered to CURRENT_SCHEMA rather than
// gorm.Migrator().HasTable's owner-scoped USER_TABLES (#736). On every other
// dialect HasTable is already current-schema-scoped and is used unchanged.
func migrationTableExists(db *gorm.DB, table string) (bool, error) {
	if db.Name() != "oracle" {
		return db.Migrator().HasTable(table), nil
	}
	// withMigrationRetry, like every other catalog read in this package: the
	// gorm HasTable this replaced swallowed its errors and returned a bare
	// false, whereas this probe surfaces them, and it is the *first* statement
	// the migration sequence issues -- the likeliest place to land on a stale
	// pooled ADB session and take ORA-02396 / ORA-03113 / ORA-01012 /
	// ORA-12537, all of which dberrors classifies transient precisely because
	// they are common on ADB. Without the retry a one-off blip turns a boot
	// into a crash-loop (oracle-db-admin review, #736).
	var cnt int64
	err := withMigrationRetry("startup table-existence probe", func() error {
		return db.Raw(
			"SELECT COUNT(*) FROM ALL_TABLES WHERE TABLE_NAME = ? AND OWNER = "+oracleCurrentSchema,
			strings.ToUpper(table),
		).Scan(&cnt).Error
	})
	if err != nil {
		return false, err
	}
	return cnt > 0, nil
}

// requireMigrationTable is the shared gate for the startup invariant checks:
// it probes for table and, when it is genuinely absent, says so at WARN
// naming the step being skipped instead of returning a silent nil (#736).
//
// On Oracle a "not found" result is worth more than a bare log line: if the
// table exists under some *other* owner, the deployment's CURRENT_SCHEMA is
// pointed at the wrong schema and every unqualified statement TMI issues is
// hitting the wrong place -- so the owners that do hold it are named in the
// warning, which turns an invisible no-op into an actionable misconfiguration
// report.
//
// Probe errors are returned, not swallowed: an unreachable database during
// startup migration is a genuine failure and every other probe in this
// package already surfaces it. Only a confirmed-absent table yields
// (false, nil).
func requireMigrationTable(db *gorm.DB, table, step string) (bool, error) {
	present, err := migrationTableExists(db, table)
	if err != nil {
		return false, fmt.Errorf("failed to check whether table %s exists: %w", table, err)
	}
	if present {
		return true, nil
	}

	logger := slogging.Get()
	if db.Name() == "oracle" {
		var owners []string
		// Ordered so the five owners named are stable across runs rather than
		// whichever five the optimizer happened to return first; this is a
		// diagnostic sample, not an exhaustive list.
		if oerr := db.Raw(
			"SELECT OWNER FROM (SELECT OWNER FROM ALL_TABLES WHERE TABLE_NAME = ? ORDER BY OWNER) WHERE ROWNUM <= 5",
			strings.ToUpper(table),
		).Scan(&owners).Error; oerr == nil && len(owners) > 0 {
			logger.Warn(
				"%s: skipped -- table %s does not exist in this session's CURRENT_SCHEMA, but IS visible under owner(s) %s. "+
					"This session resolves unqualified identifiers against the wrong schema; set CURRENT_SCHEMA (ALTER SESSION SET CURRENT_SCHEMA = <owner>, or a logon trigger) "+
					"so TMI's schema checks and DDL reach the schema that actually holds the data (#736)",
				step, strings.ToUpper(table), strings.Join(owners, ", "))
			return false, nil
		}
	}
	logger.Warn("%s: skipped -- table %s does not exist yet (#736)", step, table)
	return false, nil
}
