-- Migration 003: Client Credentials for OAuth 2.0 Client Credentials Grant
-- This migration adds support for service account authentication via OAuth 2.0 CCG (RFC 6749 Section 4.4)

-- Client credentials table (OAuth 2.0 CCG)
CREATE TABLE IF NOT EXISTS client_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    client_id TEXT NOT NULL UNIQUE,
    client_secret_hash TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT,
    is_active BOOLEAN NOT NULL DEFAULT true,
    last_used_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMP
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_client_credentials_owner ON client_credentials(owner_uuid);
CREATE INDEX IF NOT EXISTS idx_client_credentials_client_id ON client_credentials(client_id);
CREATE INDEX IF NOT EXISTS idx_client_credentials_active ON client_credentials(is_active) WHERE is_active = true;

-- Trigger for modified_at timestamp
CREATE TRIGGER update_client_credentials_modified_at
    BEFORE UPDATE ON client_credentials
    FOR EACH ROW
    EXECUTE FUNCTION update_modified_at_column();

-- Comments for documentation
COMMENT ON TABLE client_credentials IS 'OAuth 2.0 client credentials for machine-to-machine authentication (RFC 6749 Section 4.4)';
COMMENT ON COLUMN client_credentials.client_id IS 'Client identifier in format tmi_cc_{base64url(16_bytes)}';
COMMENT ON COLUMN client_credentials.client_secret_hash IS 'bcrypt hash (cost 10) of client secret';
COMMENT ON COLUMN client_credentials.name IS 'Human-readable name for the credential';
COMMENT ON COLUMN client_credentials.is_active IS 'Whether credential is active (soft delete)';
COMMENT ON COLUMN client_credentials.last_used_at IS 'Last token exchange timestamp';
COMMENT ON COLUMN client_credentials.expires_at IS 'Optional expiration timestamp (NULL = no expiration)';

