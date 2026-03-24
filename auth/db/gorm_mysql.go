//go:build mysql

package db

import (
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// getMySQLDialector returns the MySQL dialector when built with the mysql tag.
func getMySQLDialector(cfg GormConfig) gorm.Dialector {
	// MySQL DSN format: user:password@tcp(host:port)/dbname?parseTime=true
	// parseTime=true is required for proper time.Time scanning
	// loc=UTC ensures all timestamps are interpreted in UTC, preventing timezone offset issues
	// when the MySQL server or client system is in a non-UTC timezone
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&loc=UTC&charset=utf8mb4&collation=utf8mb4_unicode_ci",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	return mysql.Open(dsn)
}
