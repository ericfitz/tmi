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
		"user_providers",
		"threat_models",
		"threat_model_access",
		"threats",
		"diagrams",
		"schema_migrations",
		"refresh_tokens",
		"documents",
		"sources",
		"metadata",
		"collaboration_sessions",
		"session_participants",
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
		"id", "name", "email", "created_at", "updated_at", "last_login",
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

	// Test that the ID column is properly configured
	idCol, exists := columnMap["id"]
	if !exists {
		t.Fatal("ID column not found in users table")
	}

	if !strings.EqualFold(idCol.DataType, "uuid") {
		t.Errorf("Expected ID column to be UUID, got %s", idCol.DataType)
	}

	if idCol.IsNullable {
		t.Error("Expected ID column to be not nullable")
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
