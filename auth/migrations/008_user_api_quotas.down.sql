-- Drop user_api_quotas table
DROP TRIGGER IF EXISTS update_user_api_quotas_modified_at ON user_api_quotas;
DROP INDEX IF EXISTS idx_user_api_quotas_user_id;
DROP TABLE IF EXISTS user_api_quotas;
