-- Migration 004: Change threat_type from TEXT[] to TEXT (JSON storage)
-- This aligns PostgreSQL schema with Oracle which uses VARCHAR2/CLOB for JSON arrays.
-- Cross-database compatibility: Both databases will store arrays as JSON strings.

-- Step 1: Drop the GIN index that depends on the array type
DROP INDEX IF EXISTS idx_threats_threat_type_gin;

-- Step 2: Drop the array-specific constraints
ALTER TABLE threats DROP CONSTRAINT IF EXISTS threat_type_no_empty_strings;
ALTER TABLE threats DROP CONSTRAINT IF EXISTS threat_type_max_items;

-- Step 3: Convert existing array data to JSON and change column type
-- The array_to_json function converts PostgreSQL arrays to JSON format
ALTER TABLE threats
    ALTER COLUMN threat_type TYPE TEXT
    USING COALESCE(array_to_json(threat_type)::TEXT, '[]');

-- Step 4: Set default to JSON empty array
ALTER TABLE threats ALTER COLUMN threat_type SET DEFAULT '[]';

-- Step 5: Add a constraint to ensure valid JSON array format
-- This validates the JSON is well-formed
ALTER TABLE threats ADD CONSTRAINT threat_type_valid_json
    CHECK (threat_type IS NOT NULL AND threat_type::json IS NOT NULL);

-- Step 6: Create a functional index for JSON array queries (optional, for performance)
-- This allows efficient queries like: WHERE threat_type::jsonb ? 'Tampering'
CREATE INDEX idx_threats_threat_type_jsonb ON threats USING GIN ((threat_type::jsonb));
