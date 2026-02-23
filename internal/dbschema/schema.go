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
				{Name: "internal_uuid", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "provider", DataType: "text", IsNullable: false},
				{Name: "provider_user_id", DataType: "text", IsNullable: false},
				{Name: "email", DataType: "text", IsNullable: false},
				{Name: "name", DataType: "text", IsNullable: false},
				{Name: "email_verified", DataType: "boolean", IsNullable: true},
				{Name: "given_name", DataType: "text", IsNullable: true},
				{Name: "family_name", DataType: "text", IsNullable: true},
				{Name: "picture", DataType: "text", IsNullable: true},
				{Name: "locale", DataType: "text", IsNullable: true},
				{Name: "access_token", DataType: "text", IsNullable: true},
				{Name: "refresh_token", DataType: "text", IsNullable: true},
				{Name: "token_expiry", DataType: "timestamp with time zone", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "last_login", DataType: "timestamp with time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "users_pkey", Columns: []string{"internal_uuid"}, IsUnique: true},
				{Name: "users_provider_provider_user_id_key", Columns: []string{"provider", "provider_user_id"}, IsUnique: true},
				{Name: "idx_users_provider_lookup", Columns: []string{"provider", "provider_user_id"}, IsUnique: false},
				{Name: "idx_users_email", Columns: []string{"email"}, IsUnique: false},
				{Name: "idx_users_last_login", Columns: []string{"last_login"}, IsUnique: false},
				{Name: "idx_users_provider", Columns: []string{"provider"}, IsUnique: false},
			},
		},
		{
			Name: "threat_models",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "owner_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "created_by", DataType: "text", IsNullable: false},
				{Name: "threat_model_framework", DataType: "character varying", IsNullable: false},
				{Name: "issue_uri", DataType: "character varying", IsNullable: true},
				{Name: "status", DataType: "ARRAY", IsNullable: true},
				{Name: "status_updated", DataType: "timestamp with time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "threat_models_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_threat_models_owner_internal_uuid", Columns: []string{"owner_internal_uuid"}, IsUnique: false},
				{Name: "idx_threat_models_framework", Columns: []string{"threat_model_framework"}, IsUnique: false},
				{Name: "idx_threat_models_created_by", Columns: []string{"created_by"}, IsUnique: false},
				{Name: "idx_threat_models_status", Columns: []string{"status"}, IsUnique: false},
				{Name: "idx_threat_models_status_updated", Columns: []string{"status_updated"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threat_models_owner_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
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
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "group_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "subject_type", DataType: "text", IsNullable: false},
				{Name: "role", DataType: "text", IsNullable: false},
				{Name: "granted_by_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "threat_model_access_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_threat_model_access_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_threat_model_access_user_internal_uuid", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_threat_model_access_group_internal_uuid", Columns: []string{"group_internal_uuid"}, IsUnique: false},
				{Name: "idx_threat_model_access_subject_type", Columns: []string{"subject_type"}, IsUnique: false},
				{Name: "idx_threat_model_access_role", Columns: []string{"role"}, IsUnique: false},
				{Name: "idx_threat_model_access_performance", Columns: []string{"threat_model_id", "subject_type", "user_internal_uuid", "group_internal_uuid"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "threat_model_access_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "threat_model_access_user_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:           "threat_model_access_group_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "groups",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:           "threat_model_access_granted_by_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:       "threat_model_access_subject_type_check",
					Type:       "CHECK",
					Definition: "subject_type IN ('user', 'group')",
				},
				{
					Name:       "threat_model_access_role_check",
					Type:       "CHECK",
					Definition: "role IN ('owner', 'writer', 'reader')",
				},
				{
					Name:       "exactly_one_subject",
					Type:       "CHECK",
					Definition: "(subject_type = 'user' AND user_internal_uuid IS NOT NULL AND group_internal_uuid IS NULL) OR (subject_type = 'group' AND group_internal_uuid IS NOT NULL AND user_internal_uuid IS NULL)",
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
				{Name: "include_in_report", DataType: "boolean", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
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
					Definition: "severity IS NULL OR severity IN ('Low', 'Medium', 'High', 'Critical', 'Unknown', 'None')",
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
				{Name: "include_in_report", DataType: "boolean", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
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
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "token", DataType: "text", IsNullable: false},
				{Name: "expires_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "refresh_tokens_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "refresh_tokens_token_key", Columns: []string{"token"}, IsUnique: true},
				{Name: "idx_refresh_tokens_user_internal_uuid", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_refresh_tokens_token", Columns: []string{"token"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "refresh_tokens_user_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
			},
		},
		{
			Name: "documents",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "uri", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "character varying", IsNullable: true},
				{Name: "include_in_report", DataType: "boolean", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
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
					Name:       "documents_uri_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(uri)) > 0",
				},
			},
		},
		{
			Name: "repositories",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "uuid", IsNullable: false},
				{Name: "name", DataType: "character varying", IsNullable: true},
				{Name: "uri", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "character varying", IsNullable: true},
				{Name: "type", DataType: "character varying", IsNullable: true},
				{Name: "parameters", DataType: "jsonb", IsNullable: true},
				{Name: "include_in_report", DataType: "boolean", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "repositories_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_repositories_threat_model_id", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_repositories_name", Columns: []string{"name"}, IsUnique: false},
				{Name: "idx_repositories_type", Columns: []string{"type"}, IsUnique: false},
				{Name: "idx_repositories_created_at", Columns: []string{"created_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "repositories_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "repositories_type_check",
					Type:       "CHECK",
					Definition: "type IN ('git', 'svn', 'mercurial', 'other')",
				},
				{
					Name:       "repositories_uri_not_empty",
					Type:       "CHECK",
					Definition: "LENGTH(TRIM(uri)) > 0",
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
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
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
					Definition: "entity_type IN ('threat_model', 'threat', 'diagram', 'document', 'repository', 'cell', 'team', 'project')",
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
				{Name: "websocket_url", DataType: "text", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "expires_at", DataType: "timestamp with time zone", IsNullable: true},
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
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "joined_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "left_at", DataType: "timestamp with time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "session_participants_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_session_participants_session_id", Columns: []string{"session_id"}, IsUnique: false},
				{Name: "idx_session_participants_user_internal_uuid", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_session_participants_joined_at", Columns: []string{"joined_at"}, IsUnique: false},
				{Name: "idx_session_participants_active_unique", Columns: []string{"session_id", "user_internal_uuid"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "session_participants_session_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "collaboration_sessions",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "session_participants_user_internal_uuid_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
			},
		},
		{
			Name: "teams",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "uri", DataType: "character varying", IsNullable: true},
				{Name: "email_address", DataType: "character varying", IsNullable: true},
				{Name: "status", DataType: "character varying", IsNullable: true},
				{Name: "created_by_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "modified_by_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "reviewed_by_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "reviewed_at", DataType: "timestamp with time zone", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "teams_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_team_name", Columns: []string{"name"}, IsUnique: false},
				{Name: "idx_team_status", Columns: []string{"status"}, IsUnique: false},
				{Name: "idx_team_created_at", Columns: []string{"created_at"}, IsUnique: false},
				{Name: "idx_team_reviewed_at", Columns: []string{"reviewed_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "teams_created_by_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
			},
		},
		{
			Name: "team_members",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "team_id", DataType: "uuid", IsNullable: false},
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "custom_role", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "team_members_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_tmem_team", Columns: []string{"team_id"}, IsUnique: false},
				{Name: "idx_tmem_user", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_tmem_team_user", Columns: []string{"team_id", "user_internal_uuid"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "team_members_team_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "teams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "team_members_user_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:       "team_members_role_check",
					Type:       "CHECK",
					Definition: "role IN ('engineering_lead', 'engineer', 'product_manager', 'business_leader', 'security_specialist', 'other')",
				},
			},
		},
		{
			Name: "team_responsible_parties",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "team_id", DataType: "uuid", IsNullable: false},
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "custom_role", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "team_responsible_parties_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_trp_team", Columns: []string{"team_id"}, IsUnique: false},
				{Name: "idx_trp_user", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_trp_team_user", Columns: []string{"team_id", "user_internal_uuid"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "team_responsible_parties_team_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "teams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "team_responsible_parties_user_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:       "team_responsible_parties_role_check",
					Type:       "CHECK",
					Definition: "role IN ('engineering_lead', 'engineer', 'product_manager', 'business_leader', 'security_specialist', 'other')",
				},
			},
		},
		{
			Name: "team_relationships",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "team_id", DataType: "uuid", IsNullable: false},
				{Name: "related_team_id", DataType: "uuid", IsNullable: false},
				{Name: "relationship", DataType: "character varying", IsNullable: false},
				{Name: "custom_relationship", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "team_relationships_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_trel_team", Columns: []string{"team_id"}, IsUnique: false},
				{Name: "idx_trel_related", Columns: []string{"related_team_id"}, IsUnique: false},
				{Name: "idx_trel_team_related", Columns: []string{"team_id", "related_team_id"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "team_relationships_team_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "teams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "team_relationships_related_team_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "teams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "team_relationships_type_check",
					Type:       "CHECK",
					Definition: "relationship IN ('parent', 'child', 'dependency', 'dependent', 'supersedes', 'superseded_by', 'related', 'other')",
				},
			},
		},
		{
			Name: "projects",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "name", DataType: "character varying", IsNullable: false},
				{Name: "description", DataType: "text", IsNullable: true},
				{Name: "team_id", DataType: "uuid", IsNullable: false},
				{Name: "uri", DataType: "character varying", IsNullable: true},
				{Name: "status", DataType: "character varying", IsNullable: true},
				{Name: "created_by_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "modified_by_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "reviewed_by_internal_uuid", DataType: "uuid", IsNullable: true},
				{Name: "reviewed_at", DataType: "timestamp with time zone", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "projects_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_proj_name", Columns: []string{"name"}, IsUnique: false},
				{Name: "idx_proj_team", Columns: []string{"team_id"}, IsUnique: false},
				{Name: "idx_proj_status", Columns: []string{"status"}, IsUnique: false},
				{Name: "idx_proj_created_at", Columns: []string{"created_at"}, IsUnique: false},
				{Name: "idx_proj_reviewed_at", Columns: []string{"reviewed_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "projects_team_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "teams",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "projects_created_by_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
			},
		},
		{
			Name: "project_responsible_parties",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "project_id", DataType: "uuid", IsNullable: false},
				{Name: "user_internal_uuid", DataType: "uuid", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "custom_role", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "project_responsible_parties_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_prp_project", Columns: []string{"project_id"}, IsUnique: false},
				{Name: "idx_prp_user", Columns: []string{"user_internal_uuid"}, IsUnique: false},
				{Name: "idx_prp_project_user", Columns: []string{"project_id", "user_internal_uuid"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "project_responsible_parties_project_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "projects",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "project_responsible_parties_user_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:       "project_responsible_parties_role_check",
					Type:       "CHECK",
					Definition: "role IN ('engineering_lead', 'engineer', 'product_manager', 'business_leader', 'security_specialist', 'other')",
				},
			},
		},
		{
			Name: "project_relationships",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "uuid", IsNullable: false, IsPrimaryKey: true},
				{Name: "project_id", DataType: "uuid", IsNullable: false},
				{Name: "related_project_id", DataType: "uuid", IsNullable: false},
				{Name: "relationship", DataType: "character varying", IsNullable: false},
				{Name: "custom_relationship", DataType: "character varying", IsNullable: true},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "project_relationships_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_prel_project", Columns: []string{"project_id"}, IsUnique: false},
				{Name: "idx_prel_related", Columns: []string{"related_project_id"}, IsUnique: false},
				{Name: "idx_prel_project_related", Columns: []string{"project_id", "related_project_id"}, IsUnique: true},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "project_relationships_project_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "projects",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "project_relationships_related_project_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "projects",
					ForeignColumns: []string{"id"},
				},
				{
					Name:       "project_relationships_type_check",
					Type:       "CHECK",
					Definition: "relationship IN ('parent', 'child', 'dependency', 'dependent', 'supersedes', 'superseded_by', 'related', 'other')",
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
