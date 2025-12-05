-- Migration 003 Rollback: Remove client credentials support

-- Drop trigger
DROP TRIGGER IF EXISTS update_client_credentials_modified_at ON client_credentials;

-- Drop indexes
DROP INDEX IF EXISTS idx_client_credentials_active;
DROP INDEX IF EXISTS idx_client_credentials_client_id;
DROP INDEX IF EXISTS idx_client_credentials_owner;

-- Drop table
DROP TABLE IF EXISTS client_credentials;
