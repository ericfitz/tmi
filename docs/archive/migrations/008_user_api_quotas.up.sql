-- Create user_api_quotas table for per-user API rate limits
CREATE TABLE IF NOT EXISTS user_api_quotas (
    user_id UUID PRIMARY KEY,
    max_requests_per_minute INT NOT NULL DEFAULT 100,
    max_requests_per_hour INT DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create index for user_id lookups
CREATE INDEX idx_user_api_quotas_user_id ON user_api_quotas(user_id);

-- Create trigger for automatic modified_at updates
CREATE TRIGGER update_user_api_quotas_modified_at
    BEFORE UPDATE ON user_api_quotas
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();
