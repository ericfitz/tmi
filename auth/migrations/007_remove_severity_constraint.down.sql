-- Migration 007 Rollback: Restore original severity CHECK constraint
--
-- WARNING: This rollback will FAIL if any custom severity values exist in the database
-- that are not in the original enum: ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical')
--
-- Before running this rollback:
-- 1. Verify all severity values conform to the constraint
-- 2. Update any custom values to match the allowed values
-- 3. Consider the impact on existing data

ALTER TABLE threats ADD CONSTRAINT threats_severity_check
  CHECK (severity IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical'));

-- Remove comment
COMMENT ON COLUMN threats.severity IS NULL;
