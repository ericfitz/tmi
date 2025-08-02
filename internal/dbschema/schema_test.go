package dbschema

import (
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

	// Create a map for easy lookup
	schemaMap := make(map[string]TableSchema)
	for _, schema := range schemas {
		schemaMap[schema.Name] = schema
	}

	// Verify each expected table exists
	for _, tableName := range expectedTables {
		if _, exists := schemaMap[tableName]; !exists {
			t.Errorf("Expected table '%s' not found in schema", tableName)
		}
	}

	// Test specific table details
	t.Run("users_table", func(t *testing.T) {
		usersTable, exists := schemaMap["users"]
		if !exists {
			t.Fatal("users table not found")
		}

		// Check column count
		if len(usersTable.Columns) != 6 {
			t.Errorf("Expected 6 columns in users table, got %d", len(usersTable.Columns))
		}

		// Check for specific columns
		columnNames := make(map[string]bool)
		for _, col := range usersTable.Columns {
			columnNames[col.Name] = true
		}

		expectedColumns := []string{"id", "email", "name", "created_at", "updated_at", "last_login"}
		for _, colName := range expectedColumns {
			if !columnNames[colName] {
				t.Errorf("Expected column '%s' not found in users table", colName)
			}
		}
	})

	t.Run("threat_model_access_constraints", func(t *testing.T) {
		accessTable, exists := schemaMap["threat_model_access"]
		if !exists {
			t.Fatal("threat_model_access table not found")
		}

		// Check for CHECK constraint
		hasCheckConstraint := false
		for _, constraint := range accessTable.Constraints {
			if constraint.Type == "CHECK" {
				hasCheckConstraint = true
				break
			}
		}

		if !hasCheckConstraint {
			t.Error("Expected CHECK constraint on threat_model_access table")
		}
	})
}

func TestNormalizeDataType(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"character varying(255)", "character varying"},
		{"varchar(100)", "character varying"},
		{"CHARACTER VARYING", "character varying"},
		{"timestamp without time zone", "timestamp without time zone"},
		{"timestamp with time zone", "timestamp with time zone"},
		{"boolean", "boolean"},
		{"bool", "boolean"},
		{"int8", "bigint"},
		{"int4", "integer"},
		{"text", "text"},
	}

	for _, test := range tests {
		result := normalizeDataType(test.input)
		if result != test.expected {
			t.Errorf("normalizeDataType(%s) = %s, expected %s", test.input, result, test.expected)
		}
	}
}

func TestCompareDataTypes(t *testing.T) {
	tests := []struct {
		type1    string
		type2    string
		expected bool
	}{
		{"character varying", "varchar(255)", true},
		{"timestamp without time zone", "timestamp", true},
		{"boolean", "bool", true},
		{"text", "varchar", false},
		{"integer", "bigint", false},
	}

	for _, test := range tests {
		result := compareDataTypes(test.type1, test.type2)
		if result != test.expected {
			t.Errorf("compareDataTypes(%s, %s) = %v, expected %v",
				test.type1, test.type2, result, test.expected)
		}
	}
}

func TestCheckConstraintMatches(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		match    bool
	}{
		{
			name:     "TRIM function with BOTH FROM",
			expected: "LENGTH(TRIM(name)) > 0",
			actual:   "CHECK ((length(TRIM(BOTH FROM name)) > 0))",
			match:    true,
		},
		{
			name:     "Complex AND constraint with TRIM",
			expected: "LENGTH(TRIM(key)) > 0 AND LENGTH(key) <= 128",
			actual:   "CHECK (((length(TRIM(BOTH FROM key)) > 0) AND (length(key) <= 128)))",
			match:    true,
		},
		{
			name:     "Regex pattern with type casting",
			expected: "key ~ '^[a-zA-Z0-9_-]+$'",
			actual:   "CHECK (((key)::text ~ '^[a-zA-Z0-9_-]+$'::text))",
			match:    true,
		},
		{
			name:     "IN clause with type casting",
			expected: "provider IN ('google', 'github', 'microsoft')",
			actual:   "CHECK (((provider)::text = ANY ((ARRAY['google'::character varying, 'github'::character varying, 'microsoft'::character varying])::text[])))",
			match:    true,
		},
		{
			name:     "NOT EQUAL with type casting",
			expected: "provider_user_id != ''",
			actual:   "CHECK (((provider_user_id)::text <> ''::text))",
			match:    true,
		},
		{
			name:     "OR condition with IS NULL",
			expected: "severity IS NULL OR severity IN ('low', 'medium', 'high')",
			actual:   "CHECK (((severity IS NULL) OR ((severity)::text = ANY ((ARRAY['low'::character varying, 'medium'::character varying, 'high'::character varying])::text[]))))",
			match:    true,
		},
		{
			name:     "Date comparison",
			expected: "expires_at > created_at",
			actual:   "CHECK ((expires_at > created_at))",
			match:    true,
		},
		{
			name:     "Non-matching constraint",
			expected: "LENGTH(TRIM(name)) > 5",
			actual:   "CHECK ((length(TRIM(BOTH FROM name)) > 10))",
			match:    false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := checkConstraintMatches(test.expected, test.actual)
			if result != test.match {
				t.Errorf("checkConstraintMatches(%q, %q) = %v, expected %v",
					test.expected, test.actual, result, test.match)
			}
		})
	}
}
