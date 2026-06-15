package dbschema

import (
	"strings"
	"testing"
)

func TestGetExpectedSchema(t *testing.T) {
	schemas := GetExpectedSchema()

	// Test that we have the expected number of tables
	expectedTables := []string{
		"users",
		"threat_models",
		"threat_model_access",
		"threats",
		"diagrams",
		"schema_migrations",
		"refresh_tokens",
		"documents",
		"repositories",
		"metadata",
		"collaboration_sessions",
		"session_participants",
		"teams",
		"team_members",
		"team_responsible_parties",
		"team_relationships",
		"projects",
		"project_responsible_parties",
		"project_relationships",
		"audit_entries",
		"version_snapshots",
		"system_audit_entries",
		"timmy_sessions",
		"timmy_messages",
		"timmy_embeddings",
		"timmy_usage",
		"notes",
		"usability_feedback",
		"content_feedback",
		"alias_counters",
		"linked_identities",
	}

	if len(schemas) != len(expectedTables) {
		t.Errorf("Expected %d tables, got %d", len(expectedTables), len(schemas))
	}

	// Check that all expected tables exist
	tableMap := make(map[string]*TableSchema)
	for _, table := range schemas {
		tableMap[table.Name] = &table
	}

	for _, expectedTable := range expectedTables {
		if _, exists := tableMap[expectedTable]; !exists {
			t.Errorf("Expected table '%s' not found in schema", expectedTable)
		}
	}

	// Test that users table has expected structure
	usersTable, exists := tableMap["users"]
	if !exists {
		t.Fatal("Users table not found")
	}

	expectedUserColumns := []string{
		"internal_uuid", "provider", "provider_user_id", "name", "email", "created_at", "modified_at", "last_login",
	}

	if len(usersTable.Columns) < len(expectedUserColumns) {
		t.Errorf("Users table should have at least %d columns, got %d",
			len(expectedUserColumns), len(usersTable.Columns))
	}

	columnMap := make(map[string]ColumnSchema)
	for _, col := range usersTable.Columns {
		columnMap[col.Name] = col
	}

	for _, expectedCol := range expectedUserColumns {
		if _, exists := columnMap[expectedCol]; !exists {
			t.Errorf("Expected column '%s' not found in users table", expectedCol)
		}
	}

	// Test that the internal_uuid column is properly configured
	idCol, exists := columnMap["internal_uuid"]
	if !exists {
		t.Fatal("internal_uuid column not found in users table")
	}

	if !strings.EqualFold(idCol.DataType, "uuid") {
		t.Errorf("Expected internal_uuid column to be UUID, got %s", idCol.DataType)
	}

	if idCol.IsNullable {
		t.Error("Expected ID column to be not nullable")
	}
}

// TestGetExpectedSchema_SystemAuditEntries verifies the system_audit_entries
// table (#355) is registered in the expected schema with the columns and
// indexes the validator must detect drift against (#461). The composite
// idx_sysaudit_created_id supersedes the dropped single-column
// idx_sysaudit_created, so the latter must NOT be expected.
func TestGetExpectedSchema_SystemAuditEntries(t *testing.T) {
	schemas := GetExpectedSchema()

	var table *TableSchema
	for i := range schemas {
		if schemas[i].Name == "system_audit_entries" {
			table = &schemas[i]
			break
		}
	}
	if table == nil {
		t.Fatal("system_audit_entries table not found in expected schema")
	}

	expectedColumns := []string{
		"id", "actor_email", "actor_provider", "actor_provider_id",
		"actor_display_name", "http_method", "http_path", "field_path",
		"old_value_redacted", "new_value_redacted", "change_summary", "created_at",
	}
	columnMap := make(map[string]ColumnSchema)
	for _, col := range table.Columns {
		columnMap[col.Name] = col
	}
	for _, name := range expectedColumns {
		if _, ok := columnMap[name]; !ok {
			t.Errorf("Expected column '%s' not found in system_audit_entries", name)
		}
	}
	if idCol, ok := columnMap["id"]; !ok || !idCol.IsPrimaryKey {
		t.Error("Expected id column to be the primary key of system_audit_entries")
	}

	expectedIndexes := []string{
		"system_audit_entries_pkey",
		"idx_sysaudit_actor",
		"idx_sysaudit_field",
		"idx_sysaudit_created_id",
	}
	indexMap := make(map[string]IndexSchema)
	for _, idx := range table.Indexes {
		indexMap[idx.Name] = idx
	}
	for _, name := range expectedIndexes {
		if _, ok := indexMap[name]; !ok {
			t.Errorf("Expected index '%s' not found in system_audit_entries", name)
		}
	}
	if _, ok := indexMap["idx_sysaudit_created"]; ok {
		t.Error("Dropped single-column idx_sysaudit_created must not be in the expected schema")
	}
}

func TestNormalizeDataType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"CHARACTER VARYING(255)", "character varying"},
		{"character varying", "character varying"},
		{"BOOLEAN", "boolean"},
		{"INTEGER", "integer"},
		{"BIGINT", "bigint"},
		{"TIMESTAMP WITHOUT TIME ZONE", "timestamp without time zone"},
		{"UUID", "uuid"},
		{"TEXT", "text"},
		{"JSONB", "jsonb"},
	}

	for _, test := range tests {
		result := normalizeDataType(test.input)
		if result != test.expected {
			t.Errorf("normalizeDataType(%s) = %s, expected %s",
				test.input, result, test.expected)
		}
	}
}

func TestCompareDataTypes(t *testing.T) {
	tests := []struct {
		type1    string
		type2    string
		expected bool
	}{
		{"VARCHAR(255)", "character varying(255)", true},
		{"BOOLEAN", "boolean", true},
		{"INTEGER", "integer", true},
		{"UUID", "uuid", true},
		{"TEXT", "text", true},
		{"JSONB", "jsonb", true},
		{"VARCHAR(255)", "VARCHAR(100)", true},
		{"INTEGER", "BIGINT", false},
	}

	for _, test := range tests {
		result := compareDataTypes(test.type1, test.type2)
		if result != test.expected {
			t.Errorf("compareDataTypes(%s, %s) = %v, expected %v",
				test.type1, test.type2, result, test.expected)
		}
	}
}
