-- Migration 005 DOWN: Revert TEXT (JSON) columns back to TEXT[]
-- This reverses the conversion for:
--   - assets.classification
--   - webhook_subscriptions.events

-- ============================================================================
-- ASSETS.CLASSIFICATION
-- ============================================================================

-- Convert JSON back to PostgreSQL array format
ALTER TABLE assets
    ALTER COLUMN classification TYPE TEXT[]
    USING CASE
        WHEN classification IS NULL OR classification = '[]' OR classification = '' THEN
            ARRAY[]::TEXT[]
        ELSE
            (SELECT array_agg(elem::TEXT) FROM json_array_elements_text(classification::JSON) AS elem)
    END;

-- Remove default (original schema had no default)
ALTER TABLE assets ALTER COLUMN classification DROP DEFAULT;

-- ============================================================================
-- WEBHOOK_SUBSCRIPTIONS.EVENTS
-- ============================================================================

-- Convert JSON back to PostgreSQL array format
ALTER TABLE webhook_subscriptions
    ALTER COLUMN events TYPE TEXT[]
    USING CASE
        WHEN events IS NULL OR events = '[]' OR events = '' THEN
            ARRAY[]::TEXT[]
        ELSE
            (SELECT array_agg(elem::TEXT) FROM json_array_elements_text(events::JSON) AS elem)
    END;

-- Remove default (original schema had no default, just NOT NULL)
ALTER TABLE webhook_subscriptions ALTER COLUMN events DROP DEFAULT;

