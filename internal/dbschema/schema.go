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
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
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
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "user_id", DataType: "uuid", IsNullable: false},
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
				{Name: "idx_user_providers_user_id_is_primary", Columns: []string{"user_id", "is_primary"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "user_providers_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "user_providers_provider_check",
					Type:       "CHECK",
					Definition: "provider IN ('google', 'github', 'microsoft', 'apple', 'facebook', 'twitter')",
				},
				{
					Name:       "user_providers_provider_user_id_check",
					Type:       "CHECK",
					Definition: "provider_user_id != ''",
				},
			},
		},
		{
			Name: "threat_models",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "owner_email", DataType: "character varying", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "created_by", DataType: "character varying", IsNullable: false},
				{Name: "threat_model_framework", DataType: "character varying", IsNullable: false},
				{Name: "issue_url", DataType: "character varying", IsNullable: true},
				{Name: "document_count", DataType: "integer", IsNullable: false},
				{Name: "source_count", DataType: "integer", IsNullable: false},
				{Name: "diagram_count", DataType: "integer", IsNullable: false},
				{Name: "threat_count", DataType: "integer", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "threat_models_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_threat_models_owner_email", Columns: []string{"owner_email"}, IsUnique: false},
				{Name: "idx_threat_models_framework", Columns: []string{"threat_model_framework"}, IsUnique: false},
				{Name: "idx_threat_models_created_by", Columns: []string{"created_by"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threat_models_owner_email_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"email"},
				},
				{
					Name:       "threat_models_threat_model_framework_check",
					Type:       "CHECK",
					Definition: "threat_model_framework IN ('CIA', 'STRIDE', 'LINDDUN', 'DIE', 'PLOT4ai')",
				},
			},
		},
		{
			Name: "threat_model_access",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
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
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
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
				{Name: "idx_threats_threat_model_id_created_at", Columns: []string{"threat_model_id", "created_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threats_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "threats_severity_check",
					Type:       "CHECK",
					Definition: "severity IS NULL OR severity IN ('low', 'medium', 'high', 'critical')",
				},
				{
					Name:       "threats_likelihood_check",
					Type:       "CHECK",
					Definition: "likelihood IS NULL OR likelihood IN ('low', 'medium', 'high')",
				},
				{
					Name:       "threats_risk_level_check",
					Type:       "CHECK",
					Definition: "risk_level IS NULL OR risk_level IN ('low', 'medium', 'high', 'critical')",
				},
			},
		},
		{
			Name: "diagrams",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "type", DataType: "character varying", IsNullable: true},
				{Name: "content", DataType: "text", IsNullable: true},
				{Name: "metadata", DataType: "jsonb", IsNullable: true},
				{Name: "cells", DataType: "jsonb", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "diagrams_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_diagrams_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_diagrams_type", Columns: []string{"type"}, IsUnique: false},
				{Name: "idx_diagrams_threat_model_id_type", Columns: []string{"threat_model_id", "type"}, IsUnique: false},
				{Name: "idx_diagrams_cells", Columns: []string{"cells"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "diagrams_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "diagrams_type_check",
					Type:       "CHECK",
					Definition: "type IN ('DFD-1.0.0')",
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
		{
			Name: "refresh_tokens",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "user_id", DataType: "uuid", IsNullable: false},
				{Name: "token", DataType: "character varying", IsNullable: false},
				{Name: "expires_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "refresh_tokens_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "refresh_tokens_token_key", Columns: []string{"token"}, IsUnique: true},
				{Name: "idx_refresh_tokens_user_id", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_refresh_tokens_token", Columns: []string{"token"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "refresh_tokens_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "refresh_tokens_expires_at_check",
					Type:       "CHECK",
					Definition: "expires_at > created_at",
				},
			},
		},
		{
			Name: "documents",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "url", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "documents_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_documents_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_documents_name", Columns: []string{"name"}, IsUnique: false},
				{Name: "idx_documents_created_at", Columns: []string{"created_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "documents_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "documents_name_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(name)) > 0",
				},
				{
					Name:       "documents_url_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(url)) > 0",
				},
			},
		},
		{
			Name: "sources",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: true},
				{Name: "url", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "character varying", IsNullable: true},
				{Name: "type", DataType: "character varying", IsNullable: true},
				{Name: "parameters", DataType: "jsonb", IsNullable: true},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "sources_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_sources_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_sources_name", Columns: []string{"name"}, IsUnique: false},
				{Name: "idx_sources_type", Columns: []string{"type"}, IsUnique: false},
				{Name: "idx_sources_created_at", Columns: []string{"created_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "sources_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "sources_type_check",
					Type:       "CHECK",
					Definition: "type IN ('git', 'svn', 'mercurial', 'other')",
				},
				{
					Name:       "sources_url_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(url)) > 0",
				},
			},
		},
		{
			Name: "metadata",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "entity_type", DataType: "character varying", IsNullable: false},
				{Name: "entity_id", DataType: "uuid", IsNullable: false},
				{Name: "key", DataType: "character varying", IsNullable: false},
				{Name: "value", DataType: "text", IsNullable: false},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "updated_at", DataType: "timestamp without time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "metadata_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_metadata_entity_type_id", Columns: []string{"entity_type", "entity_id"}, IsUnique: false},
				{Name: "idx_metadata_key", Columns: []string{"key"}, IsUnique: false},
				{Name: "idx_metadata_entity_id", Columns: []string{"entity_id"}, IsUnique: false},
				{Name: "idx_metadata_unique_key_per_entity", Columns: []string{"entity_type", "entity_id", "key"}, IsUnique: true},
				{Name: "idx_metadata_key_value", Columns: []string{"key", "value"}, IsUnique: false},
				{Name: "idx_metadata_key_only", Columns: []string{"key"}, IsUnique: false},
				{Name: "idx_metadata_entity_key_exists", Columns: []string{"entity_type", "entity_id", "key"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:       "metadata_entity_type_check",
					Type:       "CHECK",
					Definition: "entity_type IN ('threat_model', 'threat', 'diagram', 'document', 'source', 'cell')",
				},
				{
					Name:       "metadata_key_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(key)) > 0 AND LENGTH(key) <= 128",
				},
				{
					Name:       "metadata_key_format",
					Type:       "CHECK",
					Definition: "key ~ '^[a-zA-Z0-9_-]+$'",
				},
				{
					Name:       "metadata_value_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(value)) > 0 AND LENGTH(value) <= 65535",
				},
			},
		},
		{
			Name: "collaboration_sessions",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "diagram_id", DataType: "uuid", IsNullable: false},
				{Name: "websocket_url", DataType: "character varying", IsNullable: false},
				{Name: "created_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "expires_at", DataType: "timestamp without time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "collaboration_sessions_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_collaboration_sessions_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_collaboration_sessions_diagram_id", Columns: []string{"diagram_id"}, IsUnique: false},
				{Name: "idx_collaboration_sessions_expires_at", Columns: []string{"expires_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "collaboration_sessions_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "collaboration_sessions_diagram_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "diagrams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "collaboration_sessions_websocket_url_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(websocket_url)) > 0",
				},
				{
					Name:       "collaboration_sessions_expires_after_created",
					Type:       "CHECK",
					Definition: "expires_at IS NULL OR expires_at > created_at",
				},
			},
		},
		{
			Name: "session_participants",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "session_id", DataType: "uuid", IsNullable: false},
				{Name: "user_id", DataType: "uuid", IsNullable: false},
				{Name: "joined_at", DataType: "timestamp without time zone", IsNullable: false},
				{Name: "left_at", DataType: "timestamp without time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "session_participants_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_session_participants_session_id", Columns: []string{"session_id"}, IsUnique: false},
				{Name: "idx_session_participants_user_id", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_session_participants_joined_at", Columns: []string{"joined_at"}, IsUnique: false},
				{Name: "idx_session_participants_active_unique", Columns: []string{"session_id", "user_id"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "session_participants_session_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "collaboration_sessions",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "session_participants_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"id"},
				},
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
	case dataType == "uuid":
		return "uuid"
	default:
		return dataType
	}
}

// compareDataTypes compares two PostgreSQL data types accounting for variations
func compareDataTypes(expected, actual string) bool {
	return normalizeDataType(expected) == normalizeDataType(actual)
}
