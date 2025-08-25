-- Drop all tables created in 001_core_infrastructure.up.sql in reverse order

-- Drop collaboration sessions tables
DROP INDEX IF EXISTS idx_session_participants_active_unique;
DROP INDEX IF EXISTS idx_session_participants_joined_at;
DROP INDEX IF EXISTS idx_session_participants_user_id;
DROP INDEX IF EXISTS idx_session_participants_session_id;
DROP INDEX IF EXISTS idx_collaboration_sessions_expires_at;
DROP INDEX IF EXISTS idx_collaboration_sessions_diagram_id;
DROP INDEX IF EXISTS idx_collaboration_sessions_threat_model_id;

DROP TABLE IF EXISTS session_participants;
DROP TABLE IF EXISTS collaboration_sessions;

-- Drop refresh_tokens table
DROP INDEX IF EXISTS idx_refresh_tokens_token;
DROP INDEX IF EXISTS idx_refresh_tokens_user_id;
DROP TABLE IF EXISTS refresh_tokens;

-- Drop user_providers table
DROP INDEX IF EXISTS idx_user_providers_email;
DROP INDEX IF EXISTS idx_user_providers_provider_lookup;
DROP INDEX IF EXISTS idx_user_providers_user_id;
DROP TABLE IF EXISTS user_providers;

-- Drop users table
DROP INDEX IF EXISTS idx_users_last_login;
DROP INDEX IF EXISTS idx_users_email;
DROP TABLE IF EXISTS users;

-- Drop UUID extension
DROP EXTENSION IF EXISTS "uuid-ossp";