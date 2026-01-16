// Package api provides dialect-specific SQL helpers for cross-database compatibility.
// These helpers abstract database-specific SQL syntax differences that GORM doesn't
// automatically handle.
package api

import (
	"fmt"

	"gorm.io/gorm"
)

// Dialect names as returned by GORM's Dialector.Name()
const (
	DialectPostgres  = "postgres"
	DialectOracle    = "oracle"
	DialectMySQL     = "mysql"
	DialectSQLServer = "sqlserver"
	DialectSQLite    = "sqlite"
)

// DateSubDays returns a dialect-specific WHERE clause for "column < now - N days".
// This handles the different date arithmetic syntax across databases.
//
// Example usage:
//
//	result := s.db.Where("status IN ?", statuses).
//	    Where(DateSubDays(s.db.Dialector.Name(), "created_at", daysOld)).
//	    Delete(&models.WebhookDelivery{})
func DateSubDays(dialectName, column string, days int) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("%s < NOW() - INTERVAL '%d days'", column, days)
	case DialectOracle:
		return fmt.Sprintf("%s < SYSDATE - %d", column, days)
	case DialectMySQL:
		return fmt.Sprintf("%s < DATE_SUB(NOW(), INTERVAL %d DAY)", column, days)
	case DialectSQLServer:
		return fmt.Sprintf("%s < DATEADD(day, -%d, GETDATE())", column, days)
	case DialectSQLite:
		return fmt.Sprintf("%s < datetime('now', '-%d days')", column, days)
	default:
		// Default to PostgreSQL syntax
		return fmt.Sprintf("%s < NOW() - INTERVAL '%d days'", column, days)
	}
}

// DateAddDays returns a dialect-specific expression for "now + N days".
// Useful for setting expiration dates or retry times.
//
// Example usage:
//
//	updates := map[string]interface{}{
//	    "next_retry_at": gorm.Expr(DateAddDays(s.db.Dialector.Name(), retryDelayDays)),
//	}
func DateAddDays(dialectName string, days int) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("NOW() + INTERVAL '%d days'", days)
	case DialectOracle:
		return fmt.Sprintf("SYSDATE + %d", days)
	case DialectMySQL:
		return fmt.Sprintf("DATE_ADD(NOW(), INTERVAL %d DAY)", days)
	case DialectSQLServer:
		return fmt.Sprintf("DATEADD(day, %d, GETDATE())", days)
	case DialectSQLite:
		return fmt.Sprintf("datetime('now', '+%d days')", days)
	default:
		return fmt.Sprintf("NOW() + INTERVAL '%d days'", days)
	}
}

// DateAddMinutes returns a dialect-specific expression for "now + N minutes".
// Useful for setting short-term retry times.
func DateAddMinutes(dialectName string, minutes int) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("NOW() + INTERVAL '%d minutes'", minutes)
	case DialectOracle:
		return fmt.Sprintf("SYSDATE + (%d / 1440)", minutes) // 1440 minutes in a day
	case DialectMySQL:
		return fmt.Sprintf("DATE_ADD(NOW(), INTERVAL %d MINUTE)", minutes)
	case DialectSQLServer:
		return fmt.Sprintf("DATEADD(minute, %d, GETDATE())", minutes)
	case DialectSQLite:
		return fmt.Sprintf("datetime('now', '+%d minutes')", minutes)
	default:
		return fmt.Sprintf("NOW() + INTERVAL '%d minutes'", minutes)
	}
}

// NowLessThanColumn returns a dialect-specific WHERE clause for "now < column".
// Useful for checking if a timestamp is in the future (e.g., retry_at > now means not yet ready).
func NowLessThanColumn(dialectName, column string) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("NOW() <= %s", column)
	case DialectOracle:
		return fmt.Sprintf("SYSDATE <= %s", column)
	case DialectMySQL:
		return fmt.Sprintf("NOW() <= %s", column)
	case DialectSQLServer:
		return fmt.Sprintf("GETDATE() <= %s", column)
	case DialectSQLite:
		return fmt.Sprintf("datetime('now') <= %s", column)
	default:
		return fmt.Sprintf("NOW() <= %s", column)
	}
}

// NowGreaterThanColumn returns a dialect-specific WHERE clause for "now > column".
// Useful for checking if a timestamp has passed (e.g., retry_at <= now means ready to retry).
func NowGreaterThanColumn(dialectName, column string) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("NOW() >= %s", column)
	case DialectOracle:
		return fmt.Sprintf("SYSDATE >= %s", column)
	case DialectMySQL:
		return fmt.Sprintf("NOW() >= %s", column)
	case DialectSQLServer:
		return fmt.Sprintf("GETDATE() >= %s", column)
	case DialectSQLite:
		return fmt.Sprintf("datetime('now') >= %s", column)
	default:
		return fmt.Sprintf("NOW() >= %s", column)
	}
}

// TruncateTable returns a dialect-specific SQL statement to truncate a table.
// Note: This bypasses GORM's soft delete and foreign key checks.
// Use with caution, primarily for test cleanup.
func TruncateTable(dialectName, tableName string) string {
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName)
	case DialectOracle:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE CONSTRAINTS", tableName)
	case DialectMySQL:
		return fmt.Sprintf("TRUNCATE TABLE %s", tableName)
	case DialectSQLServer:
		return fmt.Sprintf("TRUNCATE TABLE %s", tableName)
	case DialectSQLite:
		// SQLite doesn't have TRUNCATE, use DELETE instead
		return fmt.Sprintf("DELETE FROM %s", tableName)
	default:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName)
	}
}

// GetDialectName is a convenience function to get the dialect name from a GORM DB instance.
func GetDialectName(db *gorm.DB) string {
	return db.Name()
}
