-- Enable UUID extension if not already enabled
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Create users table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    email_verified BOOLEAN DEFAULT FALSE,
    given_name VARCHAR(255),
    family_name VARCHAR(255),
    picture VARCHAR(1024),
    locale VARCHAR(10) DEFAULT 'en-US',
    identity_provider VARCHAR(100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    modified_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMPTZ
);

-- Create user_providers table for OAuth provider linking
CREATE TABLE IF NOT EXISTS user_providers (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL,
    provider VARCHAR(50) NOT NULL,
    provider_user_id VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL,
    is_primary BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login TIMESTAMPTZ,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    UNIQUE(user_id, provider)
);

-- Create refresh_tokens table
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID NOT NULL,
    token VARCHAR(255) UNIQUE NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Create collaboration sessions tables for real-time diagram editing
CREATE TABLE collaboration_sessions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    threat_model_id UUID NOT NULL,
    diagram_id UUID NOT NULL,
    websocket_url VARCHAR(1024) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ
);

CREATE TABLE session_participants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    session_id UUID NOT NULL REFERENCES collaboration_sessions(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    left_at TIMESTAMPTZ
);

-- Create indexes for users
CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_last_login ON users(last_login);
CREATE INDEX IF NOT EXISTS idx_users_identity_provider ON users(identity_provider);

-- Create indexes for user_providers
CREATE INDEX IF NOT EXISTS idx_user_providers_user_id ON user_providers(user_id);
CREATE INDEX IF NOT EXISTS idx_user_providers_provider_lookup ON user_providers(provider, provider_user_id);
CREATE INDEX IF NOT EXISTS idx_user_providers_email ON user_providers(email);

-- Create indexes for refresh_tokens
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON refresh_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_token ON refresh_tokens(token);

-- Create indexes for collaboration sessions
CREATE INDEX idx_collaboration_sessions_threat_model_id ON collaboration_sessions(threat_model_id);
CREATE INDEX idx_collaboration_sessions_diagram_id ON collaboration_sessions(diagram_id);
CREATE INDEX idx_collaboration_sessions_expires_at ON collaboration_sessions(expires_at);

CREATE INDEX idx_session_participants_session_id ON session_participants(session_id);
CREATE INDEX idx_session_participants_user_id ON session_participants(user_id);
CREATE INDEX idx_session_participants_joined_at ON session_participants(joined_at);

-- Ensure unique active participant per session
CREATE UNIQUE INDEX idx_session_participants_active_unique 
    ON session_participants(session_id, user_id) 
    WHERE left_at IS NULL;

-- Add constraints for collaboration sessions
ALTER TABLE collaboration_sessions ADD CONSTRAINT collaboration_sessions_websocket_url_not_empty 
    CHECK (LENGTH(TRIM(websocket_url)) > 0);

ALTER TABLE collaboration_sessions ADD CONSTRAINT collaboration_sessions_expires_after_created 
    CHECK (expires_at IS NULL OR expires_at > created_at);