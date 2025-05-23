package dbschema

import (
	"strings"
)

// TableSchema represents the expected schema for a table
type TableSchema struct {
	Name        string
	Columns     []ColumnSchema
	Indexes     []IndexSchema
	Constraints []ConstraintSchema
}

// ColumnSchema represents the expected schema for a column
type ColumnSchema struct {
	Name         string
	DataType     string
	IsNullable   bool
	DefaultValue *string
	IsPrimaryKey bool
}

// IndexSchema represents the expected schema for an index
type IndexSchema struct {
	Name     string
	Columns  []string
	IsUnique bool
}

// ConstraintSchema represents the expected schema for a constraint
type ConstraintSchema struct {
	Name           string
	Type           string // CHECK, FOREIGN KEY, UNIQUE
	Definition     string
	ForeignTable   string   // For foreign keys
	ForeignColumns []string // For foreign keys
}

// GetExpectedSchema returns the complete expected database schema
func GetExpectedSchema() []TableSchema {
	return []TableSchema{
		{
			Name: "users",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "email", DataType: "character varying", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "last_login", DataType: "timestamp without time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "users_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "users_email_key", Columns: []string{"email"}, IsUnique: true},
				{Name: "idx_users_email", Columns: []string{"email"}, IsUnique: false},
				{Name: "idx_users_last_login", Columns: []string{"last_login"}, IsUnique: false},
			},
		},
		{
			Name: "user_providers",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "user_id", DataType: "character varying", IsNullable: false},
				{Name: "provider", DataType: "character varying", IsNullable: false},
				{Name: "provider_user_id", DataType: "character varying", IsNullable: false},
				{Name: "email", DataType: "character varying", IsNullable: false},
				{Name: "is_primary", DataType: "boolean", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "last_login", DataType: "timestamp without time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "user_providers_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "user_providers_user_id_provider_key", Columns: []string{"user_id", "provider"}, IsUnique: true},
				{Name: "idx_user_providers_user_id", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_user_providers_provider_lookup", Columns: []string{"provider", "provider_user_id"}, IsUnique: false},
				{Name: "idx_user_providers_email", Columns: []string{"email"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "user_providers_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"id"},
				},
			},
		},
		{
			Name: "threat_models",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "owner_email", DataType: "character varying", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "threat_models_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_threat_models_owner_email", Columns: []string{"owner_email"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threat_models_owner_email_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"email"},
				},
			},
		},
		{
			Name: "threat_model_access",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "user_email", DataType: "character varying", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "threat_model_access_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "threat_model_access_threat_model_id_user_email_key", Columns: []string{"threat_model_id", "user_email"}, IsUnique: true},
				{Name: "idx_threat_model_access_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_threat_model_access_user_email", Columns: []string{"user_email"}, IsUnique: false},
				{Name: "idx_threat_model_access_role", Columns: []string{"role"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threat_model_access_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "threat_model_access_user_email_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"email"},
				},
				{
					Name:       "threat_model_access_role_check",
					Type:       "CHECK",
					Definition: "role IN ('owner', 'writer', 'reader')",
				},
			},
		},
		{
			Name: "threats",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "severity", DataType: "character varying", IsNullable: true},
				{Name: "likelihood", DataType: "character varying", IsNullable: true},
				{Name: "risk_level", DataType: "character varying", IsNullable: true},
				{Name: "mitigation", DataType: "text", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "threats_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_threats_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_threats_severity", Columns: []string{"severity"}, IsUnique: false},
				{Name: "idx_threats_risk_level", Columns: []string{"risk_level"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threats_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
			},
		},
		{
			Name: "diagrams",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "type", DataType: "character varying", IsNullable: true},
				{Name: "content", DataType: "text", IsNullable: true},
				{Name: "metadata", DataType: "jsonb", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "diagrams_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_diagrams_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_diagrams_type", Columns: []string{"type"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "diagrams_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
			},
		},
		{
			Name: "schema_migrations",
			Columns: []ColumnSchema{
				{Name: "version", DataType: "bigint", IsNullable: false, IsPrimaryKey: true},
				{Name: "dirty", DataType: "boolean", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "schema_migrations_pkey", Columns: []string{"version"}, IsUnique: true},
			},
		},
	}
}

// normalizeDataType normalizes PostgreSQL data types for comparison
func normalizeDataType(dataType string) string {
	// Normalize common variations
	dataType = strings.ToLower(dataType)

	// Map common variations to canonical forms
	switch {
	case strings.Contains(dataType, "character varying"):
		return "character varying"
	case strings.Contains(dataType, "varchar"):
		return "character varying"
	case strings.Contains(dataType, "timestamp"):
		if strings.Contains(dataType, "with time zone") {
			return "timestamp with time zone"
		}
		return "timestamp without time zone"
	case strings.Contains(dataType, "bool"):
		return "boolean"
	case strings.Contains(dataType, "int8"):
		return "bigint"
	case strings.Contains(dataType, "int4"):
		return "integer"
	default:
		return dataType
	}
}

// compareDataTypes compares two PostgreSQL data types accounting for variations
func compareDataTypes(expected, actual string) bool {
	return normalizeDataType(expected) == normalizeDataType(actual)
}
