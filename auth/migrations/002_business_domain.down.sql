-- Drop all tables in reverse dependency order

-- Drop groups table
DROP TABLE IF EXISTS groups CASCADE;

-- Drop quota tables
DROP TABLE IF EXISTS user_api_quotas CASCADE;
DROP TABLE IF EXISTS addon_invocation_quotas CASCADE;

-- Drop administrators table
DROP TABLE IF EXISTS administrators CASCADE;

-- Drop webhook tables
DROP TABLE IF EXISTS webhook_deliveries CASCADE;
DROP TABLE IF EXISTS webhook_subscriptions CASCADE;

-- Drop addon tables
DROP TABLE IF EXISTS addon_invocations CASCADE;

-- Drop metadata table
DROP TABLE IF EXISTS metadata CASCADE;

-- Drop repositories table
DROP TABLE IF EXISTS repositories CASCADE;

-- Drop notes table
DROP TABLE IF EXISTS notes CASCADE;

-- Drop documents table
DROP TABLE IF EXISTS documents CASCADE;

-- Drop threat_model_access table
DROP TABLE IF EXISTS threat_model_access CASCADE;

-- Drop threats table
DROP TABLE IF EXISTS threats CASCADE;

-- Drop assets table
DROP TABLE IF EXISTS assets CASCADE;

-- Drop diagrams table
DROP TABLE IF EXISTS diagrams CASCADE;

-- Drop threat_models table
DROP TABLE IF EXISTS threat_models CASCADE;

-- Remove foreign key constraints from collaboration_sessions (added in this migration)
ALTER TABLE collaboration_sessions DROP CONSTRAINT IF EXISTS collaboration_sessions_threat_model_id_fkey;
ALTER TABLE collaboration_sessions DROP CONSTRAINT IF EXISTS collaboration_sessions_diagram_id_fkey;
