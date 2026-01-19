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
	return oracle.Open(dsn), dsn
}
