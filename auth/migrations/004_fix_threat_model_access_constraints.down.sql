-- Rollback Migration 004: Restore original unique constraints

-- Drop the fixed constraints
ALTER TABLE threat_model_access
DROP CONSTRAINT IF EXISTS threat_model_access_threat_model_id_user_internal_uuid_subject_key;

ALTER TABLE threat_model_access
DROP CONSTRAINT IF EXISTS threat_model_access_threat_model_id_group_internal_uuid_sub_key;

-- Restore original constraints WITH NULLS NOT DISTINCT
ALTER TABLE threat_model_access
ADD CONSTRAINT threat_model_access_threat_model_id_user_internal_uuid_subject_key
UNIQUE NULLS NOT DISTINCT (threat_model_id, user_internal_uuid, subject_type);

ALTER TABLE threat_model_access
ADD CONSTRAINT threat_model_access_threat_model_id_group_internal_uuid_sub_key
UNIQUE NULLS NOT DISTINCT (threat_model_id, group_internal_uuid, subject_type);
