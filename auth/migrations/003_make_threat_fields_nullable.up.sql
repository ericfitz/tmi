-- Migration: Make threat fields (severity, priority, status, mitigated) nullable and remove enum constraints
-- This migration supports flexible threat modeling by allowing free-form values for these fields

-- Remove CHECK constraint on severity to allow any string value
ALTER TABLE threats DROP CONSTRAINT IF EXISTS threats_severity_check;

-- Make severity accept any string value (keeping same VARCHAR length)
-- No need to alter the column type, just removing the constraint is sufficient

-- Make priority nullable (remove NOT NULL constraint)
ALTER TABLE threats ALTER COLUMN priority DROP NOT NULL;

-- Make status nullable (remove NOT NULL constraint)
ALTER TABLE threats ALTER COLUMN status DROP NOT NULL;

-- Make mitigated nullable (remove NOT NULL constraint)
ALTER TABLE threats ALTER COLUMN mitigated DROP NOT NULL;

-- Note: We keep the default values for backward compatibility with existing code
-- that might not provide these fields. New code can explicitly set NULL if desired.
