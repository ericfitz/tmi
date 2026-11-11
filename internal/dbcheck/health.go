package dbcheck

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SchemaHealthResult reports the state of the database schema.
type SchemaHealthResult struct {
	DatabaseType    string   `json:"database_type"`
	DatabaseVersion string   `json:"database_version"`
	ExpectedTables  int      `json:"expected_tables"`
	PresentTables   int      `json:"present_tables"`
	MissingTables   []string `json:"missing_tables,omitempty"`
}

// IsCurrent returns true if all expected tables are present.
func (r *SchemaHealthResult) IsCurrent() bool {
	return len(r.MissingTables) == 0
}

// ExpectedTableNames returns the list of table names that TMI expects to exist.
// This is a hardcoded list derived from api/models — kept in sync manually.
// Using a hardcoded list avoids importing the models package (which pulls in GORM)
// and makes this package lightweight for use in both the server and dbtool.
func ExpectedTableNames() []string {
	return []string{
		// Core infrastructure
		"users", "refresh_tokens", "client_credentials",
		"groups", "group_members",
		// Business domain
		"threat_models", "threat_model_access",
		"threats", "diagrams", "assets", "documents", "notes", "repositories", "metadata",
		// Collaboration
		"collaboration_sessions", "session_participants",
		// Webhooks and addons
		"webhook_subscriptions", "webhook_quotas", "webhook_url_deny_list",
		"addons", "addon_invocation_quotas",
		// Administration
		"user_api_quotas", "user_preferences",
		// System settings
		"system_settings",
		// Surveys
		"survey_templates", "survey_responses", "survey_answers", "triage_notes",
		// Teams and projects
		"teams", "team_members", "projects", "project_notes", "team_notes",
		// AI assistant
		"timmy_sessions", "timmy_messages", "timmy_embeddings", "timmy_usage",
		// Audit
		"audit_entries", "version_snapshots",
	}
}

// CheckSchemaHealth queries the database to determine which expected tables exist.
// Works across PostgreSQL, Oracle, MySQL, SQL Server, and SQLite.
func CheckSchemaHealth(sqlDB *sql.DB, dbType string) (*SchemaHealthResult, error) {
	log := slogging.Get()

	result := &SchemaHealthResult{
		DatabaseType: dbType,
	}

	// Get database version
	version, err := getDatabaseVersion(sqlDB, dbType)
	if err != nil {
		log.Debug("Could not determine database version: %v", err)
		result.DatabaseVersion = "unknown"
	} else {
		result.DatabaseVersion = version
	}

	// Get expected table names
	expected := ExpectedTableNames()
	result.ExpectedTables = len(expected)

	// Check which tables exist
	for _, table := range expected {
		exists, err := tableExistsForType(sqlDB, dbType, table)
		if err != nil {
			return nil, fmt.Errorf("error checking table %s: %w", table, err)
		}
		if exists {
			result.PresentTables++
		} else {
			result.MissingTables = append(result.MissingTables, table)
		}
	}

	return result, nil
}

// getDatabaseVersion returns a human-readable database version string.
func getDatabaseVersion(db *sql.DB, dbType string) (string, error) {
	var query string
	switch dbType {
	case DBTypePostgres, DBTypePostgreSQL:
		query = "SELECT version()"
	case DBTypeOracle:
		query = "SELECT banner FROM v$version WHERE ROWNUM = 1"
	case DBTypeMySQL:
		query = "SELECT version()"
	case DBTypeSQLServer:
		query = "SELECT @@VERSION"
	case DBTypeSQLite:
		query = "SELECT sqlite_version()"
	default:
		return "unknown", nil
	}

	var version string
	if err := db.QueryRow(query).Scan(&version); err != nil {
		return "", err
	}
	return version, nil
}

// tableExistsForType checks if a table exists, handling different DB dialects.
func tableExistsForType(db *sql.DB, dbType, tableName string) (bool, error) {
	var query string
	var args []any

	switch dbType {
	case DBTypePostgres, DBTypePostgreSQL:
		query = "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1)"
		args = []any{tableName}
	case DBTypeOracle:
		query = "SELECT COUNT(*) FROM all_tables WHERE UPPER(table_name) = UPPER(:1)"
		args = []any{tableName}
	case DBTypeMySQL:
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
		args = []any{tableName}
	case DBTypeSQLServer:
		query = "SELECT COUNT(*) FROM information_schema.tables WHERE table_name = @p1"
		args = []any{tableName}
	case DBTypeSQLite:
		query = "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?"
		args = []any{tableName}
	default:
		return false, fmt.Errorf("unsupported database type: %s", dbType)
	}

	var result any
	if err := db.QueryRow(query, args...).Scan(&result); err != nil {
		return false, err
	}

	// PostgreSQL returns bool, others return count
	switch v := result.(type) {
	case bool:
		return v, nil
	case int64:
		return v > 0, nil
	default:
		return fmt.Sprint(v) != "0" && strings.ToLower(fmt.Sprint(v)) != "false", nil
	}
}
