// Package api provides dialect-specific SQL helpers for cross-database compatibility.
// These helpers abstract database-specific SQL syntax differences that GORM doesn't
// automatically handle.
package api

import (
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ValidTableNames contains all valid table names for the TMI schema.
// This whitelist prevents SQL injection via table name parameters.
var ValidTableNames = map[string]bool{
	"users":                   true,
	"threat_models":           true,
	"diagrams":                true,
	"threats":                 true,
	"documents":               true,
	"metadata":                true,
	"client_credentials":      true,
	"webhook_subscriptions":   true,
	"webhook_deliveries":      true,
	"webhook_quotas":          true,
	"webhook_url_deny_lists":  true,
	"addon_invocation_quotas": true,
	"addons":                  true,
	"user_api_quotas":         true,
	"group_members":           true,
	"threat_model_access":     true,
	"repositories":            true,
	"notes":                   true,
	"assets":                  true,
	"collaboration_sessions":  true,
	"session_participants":    true,
	"refresh_token_records":   true,
	"groups":                  true,
	"schema_migrations":       true,
}

// ErrInvalidTableName is returned when an invalid table name is provided
var ErrInvalidTableName = fmt.Errorf("invalid table name")

// ValidateTableName checks if a table name is in the allowed whitelist.
// Returns ErrInvalidTableName if the table name is not whitelisted.
func ValidateTableName(tableName string) error {
	if !ValidTableNames[tableName] {
		return fmt.Errorf("%w: %s", ErrInvalidTableName, tableName)
	}
	return nil
}

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
// Returns ErrInvalidTableName if the table name is not in the allowed whitelist.
func TruncateTable(dialectName, tableName string) (string, error) {
	if err := ValidateTableName(tableName); err != nil {
		return "", err
	}
	switch dialectName {
	case DialectPostgres:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName), nil
	case DialectOracle:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE CONSTRAINTS", tableName), nil
	case DialectMySQL:
		return fmt.Sprintf("TRUNCATE TABLE %s", tableName), nil
	case DialectSQLServer:
		return fmt.Sprintf("TRUNCATE TABLE %s", tableName), nil
	case DialectSQLite:
		// SQLite doesn't have TRUNCATE, use DELETE instead
		return fmt.Sprintf("DELETE FROM %s", tableName), nil
	default:
		return fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName), nil
	}
}

// GetDialectName is a convenience function to get the dialect name from a GORM DB instance.
func GetDialectName(db *gorm.DB) string {
	return db.Name()
}

// ColumnName returns the column name in the correct case for the database dialect.
// Oracle requires uppercase column names because the oracle-samples/gorm-oracle driver
// doesn't consistently apply the NamingStrategy to column names in WHERE/ORDER BY clauses.
func ColumnName(dialectName, column string) string {
	if dialectName == DialectOracle {
		return toUpperSnakeCase(column)
	}
	return column
}

// toUpperSnakeCase converts a string to uppercase.
// Column names are already snake_case, so we just need to uppercase them.
func toUpperSnakeCase(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			result[i] = c - 32 // Convert to uppercase
		} else {
			result[i] = c
		}
	}
	return string(result)
}

// Col returns a clause.Column with the correct column name for the database dialect.
// Oracle requires uppercase column names because the oracle-samples/gorm-oracle driver
// doesn't consistently apply the NamingStrategy to column names in WHERE/ORDER BY clauses.
func Col(dialectName, column string) clause.Column {
	return clause.Column{Name: ColumnName(dialectName, column)}
}

// OrderByCol returns a clause.OrderByColumn for use in ORDER BY clauses.
// Oracle requires uppercase column names.
func OrderByCol(dialectName, column string, desc bool) clause.OrderByColumn {
	return clause.OrderByColumn{
		Column: Col(dialectName, column),
		Desc:   desc,
	}
}
