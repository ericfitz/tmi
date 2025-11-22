-- Drop all core infrastructure tables in reverse dependency order

-- Drop collaboration session tables
DROP TABLE IF EXISTS session_participants CASCADE;
DROP TABLE IF EXISTS collaboration_sessions CASCADE;

-- Drop refresh_tokens table
DROP TABLE IF EXISTS refresh_tokens CASCADE;

-- Drop users table
DROP TABLE IF EXISTS users CASCADE;

-- Drop UUID extension (only if no other tables depend on it)
-- Note: Keeping extension is safer in case other schemas use it
-- DROP EXTENSION IF EXISTS "uuid-ossp";
