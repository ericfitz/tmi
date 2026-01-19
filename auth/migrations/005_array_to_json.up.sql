-- Migration 005: Convert remaining TEXT[] columns to TEXT (JSON storage)
-- This aligns PostgreSQL schema with Oracle which uses VARCHAR2/CLOB for JSON arrays.
-- Cross-database compatibility: Both databases will store arrays as JSON strings.
-- Affected columns:
--   - assets.classification
--   - webhook_subscriptions.events

-- ============================================================================
-- ASSETS.CLASSIFICATION
-- ============================================================================

-- Convert existing array data to JSON and change column type
ALTER TABLE assets
    ALTER COLUMN classification TYPE TEXT
    USING COALESCE(array_to_json(classification)::TEXT, '[]');

-- Set default to JSON empty array
ALTER TABLE assets ALTER COLUMN classification SET DEFAULT '[]';

-- ============================================================================
-- WEBHOOK_SUBSCRIPTIONS.EVENTS
-- ============================================================================

-- Convert existing array data to JSON and change column type
ALTER TABLE webhook_subscriptions
    ALTER COLUMN events TYPE TEXT
    USING COALESCE(array_to_json(events)::TEXT, '[]');

-- Set default to JSON empty array
ALTER TABLE webhook_subscriptions ALTER COLUMN events SET DEFAULT '[]';

-- Events is NOT NULL, ensure it stays that way
ALTER TABLE webhook_subscriptions ALTER COLUMN events SET NOT NULL;

