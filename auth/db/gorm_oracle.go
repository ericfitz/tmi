//go:build oracle

package db

import (
	"fmt"

	"gorm.io/gorm"

	// Official Oracle GORM driver - uses godror under the hood
	// Requires CGO and Oracle Instant Client
	"github.com/oracle-samples/gorm-oracle/oracle"
)

// oracleSessionInitParams are godror DSN parameters appended to every Oracle
// connection so the entire pool shares a single, deterministic UTC time basis.
//
//   - timezone="UTC" makes godror interpret and format naive TIMESTAMP and DATE
//     values as UTC on the Go side. A non-nil Timezone also makes godror's
//     initTZ short-circuit before its per-connection "SELECT SESSIONTIMEZONE
//     FROM DUAL" probe, so no round-trip is spent discovering the DB timezone.
//   - onInit runs "ALTER SESSION SET TIME_ZONE = '+00:00'" so the DB-side
//     SESSIONTIMEZONE is UTC for every pooled session — not just the single
//     init connection. This is what prevents Oracle from silently shifting a
//     value by the session offset (up to ±14h) when it implicitly converts
//     between TIMESTAMP and TIMESTAMP WITH TIME ZONE or does date arithmetic.
//   - initOnNewConnection=1 runs onInit once per newly created physical session
//     instead of on every pool checkout: ALTER SESSION persists for the life of
//     the session, so re-running it on each acquire would only add a per-query
//     round-trip.
//
// See issue #459. godror parses the DSN with go-logfmt, so the double-quoted
// onInit value (which itself contains '=' and single quotes) is preserved
// verbatim.
const oracleSessionInitParams = `timezone="UTC" onInit="ALTER SESSION SET TIME_ZONE = '+00:00'" initOnNewConnection=1`

// getOracleDialector returns the Oracle dialector when built with the oracle tag.
// This function requires CGO and the Oracle Instant Client libraries.
//
// Uses SkipQuoteIdentifiers: true to let Oracle handle all identifiers as unquoted uppercase.
// This avoids case sensitivity issues where the driver inconsistently quotes column names
// in WHERE/ORDER BY clauses. See: https://github.com/oracle-samples/gorm-oracle/issues/49
func getOracleDialector(cfg GormConfig) (gorm.Dialector, string) {
	// Oracle connection string format for godror driver (used by oracle-samples/gorm-oracle):
	// user="username" password="password" connectString="tns_alias_or_easy_connect" configDir="/path/to/wallet"
	// For Oracle ADB with wallet, configDir points to the wallet directory containing tnsnames.ora and cwallet.sso
	// Password containing special characters should be quoted, not URL-encoded for godror
	var dsn string
	if cfg.OracleWalletLocation != "" {
		dsn = fmt.Sprintf(`user="%s" password="%s" connectString="%s" configDir="%s"`,
			cfg.User, cfg.Password, cfg.OracleConnectString, cfg.OracleWalletLocation)
	} else {
		dsn = fmt.Sprintf(`user="%s" password="%s" connectString="%s"`,
			cfg.User, cfg.Password, cfg.OracleConnectString)
	}

	// Enforce UTC on every pooled session (issue #459).
	dsn = dsn + " " + oracleSessionInitParams

	// Use oracle.New() with SkipQuoteIdentifiers to avoid case sensitivity issues.
	// When true, the driver doesn't quote identifiers, allowing Oracle to fold them
	// to uppercase automatically. Combined with OracleNamingStrategy (which uppercases
	// all names), this ensures consistent uppercase identifiers throughout.
	dialector := oracle.New(oracle.Config{
		DataSourceName:       dsn,
		SkipQuoteIdentifiers: true,
	})

	return dialector, dsn
}
