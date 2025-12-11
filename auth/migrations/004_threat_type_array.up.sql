-- Migration 004: Convert threat_type from TEXT to TEXT[]
-- Enables multiple threat type classifications per threat

-- Step 1: Add new TEXT[] column
ALTER TABLE threats ADD COLUMN threat_type_array TEXT[];

-- Step 2: Migrate existing data (TEXT → single-element array)
UPDATE threats
SET threat_type_array = CASE
    WHEN threat_type IS NULL OR threat_type = '' THEN '{}'::TEXT[]
    ELSE ARRAY[threat_type]::TEXT[]
END;

-- Step 3: Drop old column
ALTER TABLE threats DROP COLUMN threat_type;

-- Step 4: Rename new column
ALTER TABLE threats RENAME COLUMN threat_type_array TO threat_type;

-- Step 5: Set default to empty array
ALTER TABLE threats ALTER COLUMN threat_type SET DEFAULT '{}'::TEXT[];

-- Step 6: Set NOT NULL constraint (empty array is valid)
ALTER TABLE threats ALTER COLUMN threat_type SET NOT NULL;

-- Step 7: Drop old B-tree index
DROP INDEX IF EXISTS idx_threats_threat_type;

-- Step 8: Create GIN index for efficient array containment queries
CREATE INDEX idx_threats_threat_type_gin ON threats USING GIN (threat_type);

-- Step 9: Add constraints
ALTER TABLE threats ADD CONSTRAINT threat_type_no_empty_strings
CHECK (NOT ('' = ANY(threat_type)));

ALTER TABLE threats ADD CONSTRAINT threat_type_max_items
CHECK (array_length(threat_type, 1) IS NULL OR array_length(threat_type, 1) <= 20);

ALTER TABLE threats ADD CONSTRAINT threat_type_item_max_length
CHECK (NOT EXISTS (
    SELECT 1 FROM unnest(threat_type) AS item
    WHERE length(item) > 256
));
