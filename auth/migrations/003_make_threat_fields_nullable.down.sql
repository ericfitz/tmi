-- Rollback migration: Restore threat fields constraints

-- Restore NOT NULL constraint on mitigated (set default for any NULL values first)
UPDATE threats SET mitigated = FALSE WHERE mitigated IS NULL;
ALTER TABLE threats ALTER COLUMN mitigated SET NOT NULL;

-- Restore NOT NULL constraint on status (set default for any NULL values first)
UPDATE threats SET status = 'Active' WHERE status IS NULL;
ALTER TABLE threats ALTER COLUMN status SET NOT NULL;

-- Restore NOT NULL constraint on priority (set default for any NULL values first)
UPDATE threats SET priority = 'Medium' WHERE priority IS NULL;
ALTER TABLE threats ALTER COLUMN priority SET NOT NULL;

-- Restore CHECK constraint on severity (normalize any non-standard values first)
UPDATE threats SET severity = 'Unknown' WHERE severity IS NULL OR severity NOT IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical');
ALTER TABLE threats ADD CONSTRAINT threats_severity_check CHECK (severity IN ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical'));
