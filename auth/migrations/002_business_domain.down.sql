-- Drop all tables created in 002_business_domain.up.sql in reverse order

-- Drop metadata table
DROP INDEX IF EXISTS idx_metadata_threat_models;
DROP INDEX IF EXISTS idx_metadata_diagrams;
DROP INDEX IF EXISTS idx_metadata_sources;
DROP INDEX IF EXISTS idx_metadata_documents;
DROP INDEX IF EXISTS idx_metadata_threats;
DROP INDEX IF EXISTS idx_metadata_entity_type_modified_at;
DROP INDEX IF EXISTS idx_metadata_entity_type_created_at;
DROP INDEX IF EXISTS idx_metadata_modified_at;
DROP INDEX IF EXISTS idx_metadata_created_at;
DROP INDEX IF EXISTS idx_metadata_entity_key_exists;
DROP INDEX IF EXISTS idx_metadata_key_only;
DROP INDEX IF EXISTS idx_metadata_key_value;
DROP INDEX IF EXISTS idx_metadata_unique_key_per_entity;
DROP INDEX IF EXISTS idx_metadata_entity_id;
DROP INDEX IF EXISTS idx_metadata_key;
DROP INDEX IF EXISTS idx_metadata_entity_type_id;
DROP TABLE IF EXISTS metadata;

-- Drop sources table
DROP INDEX IF EXISTS idx_sources_owner_via_threat_model;
DROP INDEX IF EXISTS idx_sources_threat_model_modified_at;
DROP INDEX IF EXISTS idx_sources_threat_model_created_at;
DROP INDEX IF EXISTS idx_sources_modified_at;
DROP INDEX IF EXISTS idx_sources_created_at;
DROP INDEX IF EXISTS idx_sources_type;
DROP INDEX IF EXISTS idx_sources_name;
DROP INDEX IF EXISTS idx_sources_threat_model_id;
DROP TABLE IF EXISTS sources;

-- Drop documents table
DROP INDEX IF EXISTS idx_documents_owner_via_threat_model;
DROP INDEX IF EXISTS idx_documents_threat_model_modified_at;
DROP INDEX IF EXISTS idx_documents_threat_model_created_at;
DROP INDEX IF EXISTS idx_documents_modified_at;
DROP INDEX IF EXISTS idx_documents_created_at;
DROP INDEX IF EXISTS idx_documents_name;
DROP INDEX IF EXISTS idx_documents_threat_model_id;
DROP TABLE IF EXISTS documents;

-- Drop threat_model_access table
DROP INDEX IF EXISTS idx_threat_model_access_performance;
DROP INDEX IF EXISTS idx_threat_model_access_role;
DROP INDEX IF EXISTS idx_threat_model_access_user_email;
DROP INDEX IF EXISTS idx_threat_model_access_threat_model_id;
DROP TABLE IF EXISTS threat_model_access;

-- Drop threats table
DROP INDEX IF EXISTS idx_threats_owner_via_threat_model;
DROP INDEX IF EXISTS idx_threats_threat_model_modified_at;
DROP INDEX IF EXISTS idx_threats_threat_model_created_at;
DROP INDEX IF EXISTS idx_threats_modified_at;
DROP INDEX IF EXISTS idx_threats_name;
DROP INDEX IF EXISTS idx_threats_metadata_gin;
DROP INDEX IF EXISTS idx_threats_score;
DROP INDEX IF EXISTS idx_threats_threat_type;
DROP INDEX IF EXISTS idx_threats_status;
DROP INDEX IF EXISTS idx_threats_mitigated;
DROP INDEX IF EXISTS idx_threats_priority;
DROP INDEX IF EXISTS idx_threats_cell_id;
DROP INDEX IF EXISTS idx_threats_diagram_id;
DROP INDEX IF EXISTS idx_threats_risk_level;
DROP INDEX IF EXISTS idx_threats_severity;
DROP INDEX IF EXISTS idx_threats_threat_model_id;
DROP TABLE IF EXISTS threats;

-- Drop diagrams table
DROP INDEX IF EXISTS idx_diagrams_threat_model_id_type;
DROP INDEX IF EXISTS idx_diagrams_cells;
DROP INDEX IF EXISTS idx_diagrams_type;
DROP INDEX IF EXISTS idx_diagrams_threat_model_id;
DROP TABLE IF EXISTS diagrams;

-- Drop threat_models table
DROP INDEX IF EXISTS idx_threat_models_owner_created_at;
DROP INDEX IF EXISTS idx_threat_models_created_by;
DROP INDEX IF EXISTS idx_threat_models_framework;
DROP INDEX IF EXISTS idx_threat_models_owner_email;
DROP TABLE IF EXISTS threat_models;