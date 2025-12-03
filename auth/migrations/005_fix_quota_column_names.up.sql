-- ============================================================================
-- FIX QUOTA COLUMN NAMES MIGRATION
-- This migration renames quota table columns to match the code expectations.
-- The code uses simpler names (owner_id, user_id) while migration 002 used
-- the more verbose (owner_internal_uuid, user_internal_uuid).
-- ============================================================================

-- Rename webhook_quotas column from owner_internal_uuid to owner_id
ALTER TABLE webhook_quotas RENAME COLUMN owner_internal_uuid TO owner_id;

-- Update index name to match
DROP INDEX IF EXISTS idx_webhook_quotas_owner_internal_uuid;
CREATE INDEX IF NOT EXISTS idx_webhook_quotas_owner_id ON webhook_quotas(owner_id);
