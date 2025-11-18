-- Migration 007: Remove severity CHECK constraint to allow custom and localized values
--
-- Background:
-- The original CHECK constraint limited severity to only English values:
-- ('Unknown', 'None', 'Low', 'Medium', 'High', 'Critical')
--
-- This migration allows:
--   - Numeric values: "0", "1", "2", "3", "4", "5"
--   - English values: "Unknown", "None", "Low", "Medium", "High", "Critical"
--   - Localized values: "Bajo", "Medio", "Alto" (Spanish), "低", "中", "高" (Chinese)
--   - Custom values with parentheses: "Risk(3)", "Custom-Level"
--
-- OpenAPI Pattern: ^[\u0020-\uFFFF_().-]{1,50}$
-- (Unicode characters, alphanumeric, hyphens, underscores, parentheses, periods)

ALTER TABLE threats DROP CONSTRAINT IF EXISTS threats_severity_check;

-- Add comment explaining the change
COMMENT ON COLUMN threats.severity IS
  'Severity level - accepts numeric strings (0-5), standard values (Unknown, None, Low, Medium, High, Critical), custom values, or localized strings. Supports Unicode characters, alphanumeric, hyphens, underscores, parentheses, periods. Maximum 50 characters.';
