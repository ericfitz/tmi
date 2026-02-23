package framework

import (
	"database/sql"
	"fmt"
	"os"

	_ "github.com/lib/pq"
)

// validTableNames is a whitelist of allowed table names for SQL operations.
// This prevents SQL injection when executing raw SQL in tests.
var validTableNames = map[string]bool{
	"users":                   true,
	"threat_models":           true,
	"threat_model_access":     true,
	"diagrams":                true,
	"threats":                 true,
	"assets":                  true,
	"groups":                  true,
	"group_members":           true,
	"documents":               true,
	"metadata":                true,
	"repositories":            true,
	"collaboration_sessions":  true,
	"session_participants":    true,
	"webhook_subscriptions":   true,
	"webhook_deliveries":      true,
	"webhook_quotas":          true,
	"addons":                  true,
	"addon_invocation_quotas": true,
	"user_api_quotas":         true,
	"client_credentials":      true,
	"notes":                   true,
	"survey_templates":         true,
	"survey_responses":         true,
	"survey_response_access":   true,
	"teams":                    true,
	"team_members":             true,
	"team_responsible_parties": true,
	"team_relationships":       true,
	"projects":                 true,
	"project_responsible_parties": true,
	"project_relationships":    true,
}

// validateTableName checks if a table name is in the allowed whitelist.
// Returns an error if the table name is not whitelisted (prevents SQL injection).
func validateTableName(tableName string) error {
	if !validTableNames[tableName] {
		return fmt.Errorf("invalid table name: %s", tableName)
	}
	return nil
}

// TestDatabase provides direct database access for integration tests
type TestDatabase struct {
	db *sql.DB
}

// NewTestDatabase creates a new test database connection using environment variables
func NewTestDatabase() (*TestDatabase, error) {
	host := getEnvOrDefault("TEST_DB_HOST", "127.0.0.1")
	port := getEnvOrDefault("TEST_DB_PORT", "5432")
	user := getEnvOrDefault("TEST_DB_USER", "tmi_dev")
	password := getEnvOrDefault("TEST_DB_PASSWORD", "dev123")
	dbname := getEnvOrDefault("TEST_DB_NAME", "tmi_dev")

	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &TestDatabase{db: db}, nil
}

// Close closes the database connection
func (t *TestDatabase) Close() error {
	if t.db != nil {
		return t.db.Close()
	}
	return nil
}

// TruncateTable truncates a specific table
func (t *TestDatabase) TruncateTable(tableName string) error {
	if err := validateTableName(tableName); err != nil {
		return fmt.Errorf("invalid table name for truncate: %w", err)
	}
	_, err := t.db.Exec(fmt.Sprintf("TRUNCATE TABLE %s CASCADE", tableName))
	if err != nil {
		return fmt.Errorf("failed to truncate table %s: %w", tableName, err)
	}
	return nil
}

// CountRows returns the count of rows in a table
func (t *TestDatabase) CountRows(tableName string) (int64, error) {
	if err := validateTableName(tableName); err != nil {
		return 0, fmt.Errorf("invalid table name for count: %w", err)
	}
	var count int64
	err := t.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", tableName)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count rows in %s: %w", tableName, err)
	}
	return count, nil
}

// ExecSQL executes a raw SQL statement
func (t *TestDatabase) ExecSQL(sql string) error {
	_, err := t.db.Exec(sql)
	return err
}

// QueryString executes a query and returns a single string value
func (t *TestDatabase) QueryString(sql string) (string, error) {
	var result string
	err := t.db.QueryRow(sql).Scan(&result)
	if err != nil {
		return "", err
	}
	return result, nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
