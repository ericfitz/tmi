-- ============================================================================
-- USER API QUOTAS MIGRATION
-- This migration creates the user_api_quotas table for configurable per-user
-- rate limits on resource operations (Tier 3 rate limiting).
-- ============================================================================

-- Create user_api_quotas table for per-user API rate limits
CREATE TABLE IF NOT EXISTS user_api_quotas (
    user_id UUID PRIMARY KEY REFERENCES users(internal_uuid) ON DELETE CASCADE,
    max_requests_per_minute INT NOT NULL DEFAULT 100,
    max_requests_per_hour INT DEFAULT NULL,  -- Optional hourly limit
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create index for faster lookups
CREATE INDEX IF NOT EXISTS idx_user_api_quotas_user_id ON user_api_quotas(user_id);

-- Trigger for automatic timestamp updates
CREATE TRIGGER update_user_api_quotas_modified_at
    BEFORE UPDATE ON user_api_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();

-- Add comment for documentation
COMMENT ON TABLE user_api_quotas IS 'Per-user configurable rate limits for resource operations (Tier 3). Falls back to defaults if no row exists for user.';
COMMENT ON COLUMN user_api_quotas.user_id IS 'User identifier (internal_uuid from users table)';
COMMENT ON COLUMN user_api_quotas.max_requests_per_minute IS 'Maximum API requests per minute (default: 100)';
COMMENT ON COLUMN user_api_quotas.max_requests_per_hour IS 'Optional maximum API requests per hour (default: NULL means no hourly limit)';
