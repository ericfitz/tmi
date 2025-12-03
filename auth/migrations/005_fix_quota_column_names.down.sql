-- ============================================================================
-- FIX QUOTA COLUMN NAMES MIGRATION ROLLBACK
-- ============================================================================

-- Rename webhook_quotas column back to owner_internal_uuid
ALTER TABLE webhook_quotas RENAME COLUMN owner_id TO owner_internal_uuid;

-- Update index name to match
DROP INDEX IF EXISTS idx_webhook_quotas_owner_id;
CREATE INDEX IF NOT EXISTS idx_webhook_quotas_owner_internal_uuid ON webhook_quotas(owner_internal_uuid);
