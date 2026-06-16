//go:build oracle

package db

import (
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"

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

	dialector := oracle.New(oracle.Config{
		Conn:                 sqlDB,
		SkipQuoteIdentifiers: true,
	})

	// Return the (non-secret) connect string for the caller's debug log rather
	// than the password-bearing DSN.
	return dialector, cfg.OracleConnectString
}
