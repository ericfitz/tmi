-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- ============================================================================
-- CONSOLIDATED CORE INFRASTRUCTURE MIGRATION
-- This migration consolidates the user authentication and session management
-- tables with the new user identification architecture.
-- ============================================================================

-- Create users table with consolidated provider information
CREATE TABLE IF NOT EXISTS users (
    internal_uuid UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    provider TEXT NOT NULL,                           -- OAuth provider: "tmi", "google", "github", "microsoft", "azure"
    provider_user_id TEXT,                            -- Provider's user ID (from JWT sub claim) - nullable for sparse users
    email TEXT NOT NULL,
    name TEXT NOT NULL,                               -- Display name for UI presentation
    email_verified BOOLEAN DEFAULT FALSE,
    access_token TEXT,                                -- OAuth access token
    refresh_token TEXT,                               -- OAuth refresh token
    token_expiry TIMESTAMPTZ,                         -- Token expiration time
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMPTZ,

    -- Unique constraints:
    -- 1. One user per (provider, provider_user_id) for OAuth-authenticated users
    -- 2. One user per (provider, email) for sparse users (created before first login)
    UNIQUE NULLS NOT DISTINCT (provider, provider_user_id),
    UNIQUE (provider, email)
);

-- Create indexes for users
CREATE INDEX idx_users_provider_lookup ON users(provider, provider_user_id);
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_last_login ON users(last_login);
CREATE INDEX idx_users_provider ON users(provider);

-- Create refresh_tokens table (for additional refresh token tracking if needed)
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_internal_uuid UUID NOT NULL,
    token TEXT UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_internal_uuid) REFERENCES users(internal_uuid) ON DELETE CASCADE
);

-- Create indexes for refresh_tokens
CREATE INDEX idx_refresh_tokens_user_internal_uuid ON refresh_tokens(user_internal_uuid);
CREATE INDEX idx_refresh_tokens_token ON refresh_tokens(token);

-- Create client_credentials table for OAuth 2.0 Client Credentials Grant (RFC 6749 Section 4.4)
-- This table stores service account credentials for machine-to-machine authentication
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

-- Indexes for client_credentials performance
CREATE INDEX IF NOT EXISTS idx_client_credentials_owner ON client_credentials(owner_uuid);
CREATE INDEX IF NOT EXISTS idx_client_credentials_client_id ON client_credentials(client_id);
CREATE INDEX IF NOT EXISTS idx_client_credentials_active ON client_credentials(is_active) WHERE is_active = true;

-- Comments for client_credentials documentation
COMMENT ON TABLE client_credentials IS 'OAuth 2.0 client credentials for machine-to-machine authentication (RFC 6749 Section 4.4)';
COMMENT ON COLUMN client_credentials.client_id IS 'Client identifier in format tmi_cc_{base64url(16_bytes)}';
COMMENT ON COLUMN client_credentials.client_secret_hash IS 'bcrypt hash (cost 10) of client secret';
COMMENT ON COLUMN client_credentials.name IS 'Human-readable name for the credential';
COMMENT ON COLUMN client_credentials.is_active IS 'Whether credential is active (soft delete)';
COMMENT ON COLUMN client_credentials.last_used_at IS 'Last token exchange timestamp';
COMMENT ON COLUMN client_credentials.expires_at IS 'Optional expiration timestamp (NULL = no expiration)';

-- Create collaboration sessions tables for real-time diagram editing
CREATE TABLE collaboration_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    threat_model_id UUID NOT NULL,
    diagram_id UUID NOT NULL,
    websocket_url TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ
);

CREATE TABLE session_participants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES collaboration_sessions(id) ON DELETE CASCADE,
    user_internal_uuid UUID NOT NULL REFERENCES users(internal_uuid) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    left_at TIMESTAMPTZ
);

-- Create indexes for collaboration sessions
CREATE INDEX idx_collaboration_sessions_threat_model_id ON collaboration_sessions(threat_model_id);
CREATE INDEX idx_collaboration_sessions_diagram_id ON collaboration_sessions(diagram_id);
CREATE INDEX idx_collaboration_sessions_expires_at ON collaboration_sessions(expires_at);

CREATE INDEX idx_session_participants_session_id ON session_participants(session_id);
CREATE INDEX idx_session_participants_user_internal_uuid ON session_participants(user_internal_uuid);
CREATE INDEX idx_session_participants_joined_at ON session_participants(joined_at);

-- Ensure unique active participant per session
CREATE UNIQUE INDEX idx_session_participants_active_unique
    ON session_participants(session_id, user_internal_uuid)
    WHERE left_at IS NULL;

-- Add constraints for collaboration sessions
ALTER TABLE collaboration_sessions ADD CONSTRAINT collaboration_sessions_websocket_url_not_empty
    CHECK (LENGTH(TRIM(websocket_url)) > 0);

ALTER TABLE collaboration_sessions ADD CONSTRAINT collaboration_sessions_expires_after_created
    CHECK (expires_at IS NULL OR expires_at > created_at);
