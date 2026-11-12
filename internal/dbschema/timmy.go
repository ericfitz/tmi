package dbschema

// GetTimmySchema returns the expected schema for the Timmy AI assistant tables
func GetTimmySchema() []TableSchema {
	return []TableSchema{
		{
			Name: "timmy_sessions",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "user_id", DataType: "character varying", IsNullable: false},
				{Name: "title", DataType: "character varying", IsNullable: true},
				{Name: "source_snapshot", DataType: "text", IsNullable: true},
				{Name: "system_prompt_hash", DataType: "character varying", IsNullable: true},
				{Name: "status", DataType: "character varying", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "modified_at", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "deleted_at", DataType: "timestamp with time zone", IsNullable: true},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_sessions_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_sessions_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_sessions_user", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_timmy_sessions_status", Columns: []string{"status"}, IsUnique: false},
				{Name: "idx_timmy_sessions_deleted_at", Columns: []string{"deleted_at"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "timmy_sessions_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "timmy_sessions_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
			},
		},
		{
			Name: "timmy_messages",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "session_id", DataType: "character varying", IsNullable: false},
				{Name: "role", DataType: "character varying", IsNullable: false},
				{Name: "content", DataType: "text", IsNullable: false},
				{Name: "token_count", DataType: "integer", IsNullable: true},
				{Name: "sequence", DataType: "integer", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_messages_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_messages_session", Columns: []string{"session_id"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "timmy_messages_session_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "timmy_sessions",
					ForeignColumns: []string{"id"},
				},
			},
		},
		{
			Name: "timmy_embeddings",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "entity_type", DataType: "character varying", IsNullable: false},
				{Name: "entity_id", DataType: "character varying", IsNullable: false},
				{Name: "chunk_index", DataType: "integer", IsNullable: false},
				{Name: "index_type", DataType: "character varying", IsNullable: false},
				{Name: "content_hash", DataType: "character varying", IsNullable: false},
				{Name: "embedding_model", DataType: "character varying", IsNullable: false},
				{Name: "embedding_dim", DataType: "integer", IsNullable: false},
				{Name: "vector_data", DataType: "bytea", IsNullable: true},
				{Name: "chunk_text", DataType: "text", IsNullable: false},
				{Name: "created_at", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_embeddings_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_embeddings_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_embeddings_entity", Columns: []string{"threat_model_id", "entity_type", "entity_id", "chunk_index", "index_type"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "timmy_embeddings_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
			},
		},
		{
			Name: "timmy_usage",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "character varying", IsNullable: false, IsPrimaryKey: true},
				{Name: "user_id", DataType: "character varying", IsNullable: false},
				{Name: "session_id", DataType: "character varying", IsNullable: false},
				{Name: "threat_model_id", DataType: "character varying", IsNullable: false},
				{Name: "message_count", DataType: "integer", IsNullable: true},
				{Name: "prompt_tokens", DataType: "integer", IsNullable: true},
				{Name: "completion_tokens", DataType: "integer", IsNullable: true},
				{Name: "embedding_tokens", DataType: "integer", IsNullable: true},
				{Name: "period_start", DataType: "timestamp with time zone", IsNullable: false},
				{Name: "period_end", DataType: "timestamp with time zone", IsNullable: false},
			},
			Indexes: []IndexSchema{
				{Name: "timmy_usage_pkey", Columns: []string{"id"}, IsUnique: true},
				{Name: "idx_timmy_usage_user", Columns: []string{"user_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_session", Columns: []string{"session_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_tm", Columns: []string{"threat_model_id"}, IsUnique: false},
				{Name: "idx_timmy_usage_period", Columns: []string{"period_start", "period_end"}, IsUnique: false},
			},
			Constraints: []ConstraintSchema{
				{
					Name:           "timmy_usage_threat_model_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "threat_models",
					ForeignColumns: []string{"id"},
				},
				{
					Name:           "timmy_usage_user_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "users",
					ForeignColumns: []string{"internal_uuid"},
				},
				{
					Name:           "timmy_usage_session_id_fkey",
					Type:           "FOREIGN KEY",
					ForeignTable:   "timmy_sessions",
					ForeignColumns: []string{"id"},
				},
			},
		},
	}
}
