//go:build oracle

package db

import (
	"fmt"

	"gorm.io/gorm"

	// Official Oracle GORM driver - uses godror under the hood
	// Requires CGO and Oracle Instant Client
	"github.com/oracle-samples/gorm-oracle/oracle"
)

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
			cfg.OracleUser, cfg.OraclePassword, cfg.OracleConnectString, cfg.OracleWalletLocation)
	} else {
		dsn = fmt.Sprintf(`user="%s" password="%s" connectString="%s"`,
			cfg.OracleUser, cfg.OraclePassword, cfg.OracleConnectString)
	}

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
