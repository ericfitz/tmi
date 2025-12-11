-- Migration 004 Rollback: Revert threat_type from TEXT[] to TEXT

-- Step 1: Drop GIN index
DROP INDEX IF EXISTS idx_threats_threat_type_gin;

-- Step 2: Drop constraints
DROP CONSTRAINT IF EXISTS threat_type_no_empty_strings;
DROP CONSTRAINT IF EXISTS threat_type_max_items;
DROP CONSTRAINT IF EXISTS threat_type_item_max_length;

-- Step 3: Add temporary TEXT column
ALTER TABLE threats ADD COLUMN threat_type_text TEXT;

-- Step 4: Convert arrays back to TEXT (takes first element or empty string)
UPDATE threats
SET threat_type_text = COALESCE(threat_type[1], '');

-- Step 5: Drop array column
ALTER TABLE threats DROP COLUMN threat_type;

-- Step 6: Rename text column back to threat_type
ALTER TABLE threats RENAME COLUMN threat_type_text TO threat_type;

-- Step 7: Restore original default
ALTER TABLE threats ALTER COLUMN threat_type SET DEFAULT 'Unspecified';

-- Step 8: Recreate original B-tree index
CREATE INDEX idx_threats_threat_type ON threats(threat_type);
