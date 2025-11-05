-- Revert threat_models.status from TEXT back to TEXT[]

-- Drop the B-tree index
DROP INDEX IF EXISTS idx_threat_models_status;

-- Drop the length constraint
ALTER TABLE threat_models
DROP CONSTRAINT IF EXISTS status_max_length;

-- Convert the single string back to an array
-- Wrap the string value in an array if not null
UPDATE threat_models
SET status = (
    CASE
        WHEN status IS NOT NULL
        THEN ARRAY[status]
        ELSE NULL
    END
)::TEXT[]
WHERE status IS NOT NULL OR status IS NULL;

-- Alter the column type back to TEXT[]
ALTER TABLE threat_models
ALTER COLUMN status TYPE TEXT[] USING (
    CASE
        WHEN status IS NOT NULL
        THEN ARRAY[status]
        ELSE NULL
    END
);

-- Recreate the GIN index for array column
CREATE INDEX IF NOT EXISTS idx_threat_models_status ON threat_models USING GIN (status);
