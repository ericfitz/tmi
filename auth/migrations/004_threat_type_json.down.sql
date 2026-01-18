-- Migration 004 DOWN: Revert threat_type from TEXT (JSON) back to TEXT[]

-- Step 1: Drop the JSON index
DROP INDEX IF EXISTS idx_threats_threat_type_jsonb;

-- Step 2: Drop the JSON constraint
ALTER TABLE threats DROP CONSTRAINT IF EXISTS threat_type_valid_json;

-- Step 3: Convert JSON back to array and change column type
-- Parse the JSON array and convert to PostgreSQL array
ALTER TABLE threats
    ALTER COLUMN threat_type TYPE TEXT[]
    USING (
        SELECT COALESCE(
            ARRAY(SELECT jsonb_array_elements_text(threat_type::jsonb)),
            ARRAY[]::TEXT[]
        )
    );

-- Step 4: Restore the default
ALTER TABLE threats ALTER COLUMN threat_type SET DEFAULT '{}'::TEXT[];

-- Step 5: Restore the array constraints
ALTER TABLE threats ADD CONSTRAINT threat_type_no_empty_strings
    CHECK (NOT ('' = ANY(threat_type)));
ALTER TABLE threats ADD CONSTRAINT threat_type_max_items
    CHECK (array_length(threat_type, 1) IS NULL OR array_length(threat_type, 1) <= 20);

-- Step 6: Recreate the GIN index for array queries
CREATE INDEX idx_threats_threat_type_gin ON threats USING GIN (threat_type);
