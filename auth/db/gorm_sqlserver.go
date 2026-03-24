//go:build sqlserver

package db

import (
	"fmt"

	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
)

// getSQLServerDialector returns the SQL Server dialector when built with the sqlserver tag.
func getSQLServerDialector(cfg GormConfig) gorm.Dialector {
	// SQL Server DSN format: sqlserver://user:password@host:port?database=dbname
	dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%s?database=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	return sqlserver.Open(dsn)
}
