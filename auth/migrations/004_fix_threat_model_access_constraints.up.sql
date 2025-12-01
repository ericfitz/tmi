-- Migration 004: Fix threat_model_access unique constraints
-- Problem: NULLS NOT DISTINCT treats NULL values as equal, preventing multiple
-- user entries with NULL group_internal_uuid from being added to the same threat model

-- Drop existing constraints
ALTER TABLE threat_model_access
DROP CONSTRAINT IF EXISTS threat_model_access_threat_model_id_user_internal_uuid_subject_key;

ALTER TABLE threat_model_access
DROP CONSTRAINT IF EXISTS threat_model_access_threat_model_id_group_internal_uuid_sub_key;

-- Recreate constraints WITHOUT NULLS NOT DISTINCT
-- This allows multiple NULL values in the same column
ALTER TABLE threat_model_access
ADD CONSTRAINT threat_model_access_threat_model_id_user_internal_uuid_subject_key
UNIQUE (threat_model_id, user_internal_uuid, subject_type);

ALTER TABLE threat_model_access
ADD CONSTRAINT threat_model_access_threat_model_id_group_internal_uuid_sub_key
UNIQUE (threat_model_id, group_internal_uuid, subject_type);
