-- Change threat_models.status from TEXT[] to TEXT (nullable string)

-- Drop the GIN index on the array column
DROP INDEX IF EXISTS idx_threat_models_status;

-- Alter the column type from TEXT[] to TEXT
-- The USING clause converts existing array data to a single string (first element)
ALTER TABLE threat_models
ALTER COLUMN status TYPE TEXT USING (
    CASE
        WHEN status IS NOT NULL AND array_length(status, 1) > 0
        THEN status[1]
        ELSE NULL
    END
);

-- Ensure the column is nullable (it should already be, but make it explicit)
ALTER TABLE threat_models
ALTER COLUMN status DROP NOT NULL;

-- Add a length constraint to match the OpenAPI spec (max 128 characters)
ALTER TABLE threat_models
ADD CONSTRAINT status_max_length CHECK (status IS NULL OR length(status) <= 128);

-- Create a standard B-tree index on the new string column
CREATE INDEX IF NOT EXISTS idx_threat_models_status ON threat_models(status);
