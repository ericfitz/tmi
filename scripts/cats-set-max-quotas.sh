#!/bin/bash
# cats-set-max-quotas.sh
# Sets maximum quotas and rate limits for CATS test user to prevent rate-limit errors
#
# This script sets all quotas to maximum values for the CATS test user (charlie@tmi)
# to eliminate rate-limiting and quota-related errors during intensive fuzzing.
#
# Usage: ./scripts/cats-set-max-quotas.sh

set -e

# Database connection parameters
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-tmi_dev}"
DB_USER="${DB_USER:-tmi_dev}"
DB_PASSWORD="${DB_PASSWORD:-tmi_dev}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-tmi-postgresql}"

echo "🚀 Setting maximum quotas for CATS test user..."
echo "   Target user: charlie@tmi (TMI provider)"
echo "   Database: ${DB_NAME} on ${DB_HOST}:${DB_PORT}"
echo ""

# Execute the SQL script
if docker ps --format "{{.Names}}" | grep -q "^${POSTGRES_CONTAINER}$"; then
    echo "   Using Docker container: ${POSTGRES_CONTAINER}"
    echo ""
    docker exec -i "${POSTGRES_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Set maximum quotas for CATS test user
-- This prevents rate-limiting during intensive fuzzing

DO $$
DECLARE
    v_user_uuid UUID;
    v_quota_count INT;
BEGIN
    -- Get the internal UUID for charlie@tmi
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'tmi'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@tmi) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user already has quotas set
        SELECT COUNT(*) INTO v_quota_count
        FROM user_quotas
        WHERE user_internal_uuid = v_user_uuid;

        IF v_quota_count > 0 THEN
            -- Update existing quotas to maximum values
            UPDATE user_quotas
            SET
                max_requests_per_minute = 10000,
                max_requests_per_hour = 600000,
                max_subscriptions = 100,
                max_events_per_minute = 1000,
                max_subscription_requests_per_minute = 100,
                max_subscription_requests_per_day = 10000,
                max_active_invocations = 10,
                max_invocations_per_hour = 1000,
                updated_at = NOW()
            WHERE user_internal_uuid = v_user_uuid;

            RAISE NOTICE 'Updated quotas for charlie@tmi to maximum values';
        ELSE
            -- Insert maximum quotas
            INSERT INTO user_quotas (
                user_internal_uuid,
                max_requests_per_minute,
                max_requests_per_hour,
                max_subscriptions,
                max_events_per_minute,
                max_subscription_requests_per_minute,
                max_subscription_requests_per_day,
                max_active_invocations,
                max_invocations_per_hour,
                created_at,
                updated_at
            ) VALUES (
                v_user_uuid,
                10000,  -- MaxRequestsPerMinute
                600000, -- MaxRequestsPerHour
                100,    -- MaxSubscriptions
                1000,   -- MaxEventsPerMinute
                100,    -- MaxSubscriptionRequestsPerMinute
                10000,  -- MaxSubscriptionRequestsPerDay
                10,     -- MaxActiveInvocations
                1000,   -- MaxInvocationsPerHour
                NOW(),
                NOW()
            );

            RAISE NOTICE 'Created maximum quotas for charlie@tmi';
        END IF;

        -- Display current quota settings
        RAISE NOTICE 'Current quota settings:';
        RAISE NOTICE '  max_requests_per_minute: 10000';
        RAISE NOTICE '  max_requests_per_hour: 600000';
        RAISE NOTICE '  max_subscriptions: 100';
        RAISE NOTICE '  max_events_per_minute: 1000';
        RAISE NOTICE '  max_subscription_requests_per_minute: 100';
        RAISE NOTICE '  max_subscription_requests_per_day: 10000';
        RAISE NOTICE '  max_active_invocations: 10';
        RAISE NOTICE '  max_invocations_per_hour: 1000';
    END IF;
END $$;
EOF
else
    echo "   Using direct psql connection"
    echo ""
    PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Set maximum quotas for CATS test user
-- This prevents rate-limiting during intensive fuzzing

DO $$
DECLARE
    v_user_uuid UUID;
    v_quota_count INT;
BEGIN
    -- Get the internal UUID for charlie@tmi
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'tmi'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@tmi) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user already has quotas set
        SELECT COUNT(*) INTO v_quota_count
        FROM user_quotas
        WHERE user_internal_uuid = v_user_uuid;

        IF v_quota_count > 0 THEN
            -- Update existing quotas to maximum values
            UPDATE user_quotas
            SET
                max_requests_per_minute = 10000,
                max_requests_per_hour = 600000,
                max_subscriptions = 100,
                max_events_per_minute = 1000,
                max_subscription_requests_per_minute = 100,
                max_subscription_requests_per_day = 10000,
                max_active_invocations = 10,
                max_invocations_per_hour = 1000,
                updated_at = NOW()
            WHERE user_internal_uuid = v_user_uuid;

            RAISE NOTICE 'Updated quotas for charlie@tmi to maximum values';
        ELSE
            -- Insert maximum quotas
            INSERT INTO user_quotas (
                user_internal_uuid,
                max_requests_per_minute,
                max_requests_per_hour,
                max_subscriptions,
                max_events_per_minute,
                max_subscription_requests_per_minute,
                max_subscription_requests_per_day,
                max_active_invocations,
                max_invocations_per_hour,
                created_at,
                updated_at
            ) VALUES (
                v_user_uuid,
                10000,  -- MaxRequestsPerMinute
                600000, -- MaxRequestsPerHour
                100,    -- MaxSubscriptions
                1000,   -- MaxEventsPerMinute
                100,    -- MaxSubscriptionRequestsPerMinute
                10000,  -- MaxSubscriptionRequestsPerDay
                10,     -- MaxActiveInvocations
                1000,   -- MaxInvocationsPerHour
                NOW(),
                NOW()
            );

            RAISE NOTICE 'Created maximum quotas for charlie@tmi';
        END IF;

        -- Display current quota settings
        RAISE NOTICE 'Current quota settings:';
        RAISE NOTICE '  max_requests_per_minute: 10000';
        RAISE NOTICE '  max_requests_per_hour: 600000';
        RAISE NOTICE '  max_subscriptions: 100';
        RAISE NOTICE '  max_events_per_minute: 1000';
        RAISE NOTICE '  max_subscription_requests_per_minute: 100';
        RAISE NOTICE '  max_subscription_requests_per_day: 10000';
        RAISE NOTICE '  max_active_invocations: 10';
        RAISE NOTICE '  max_invocations_per_hour: 1000';
    END IF;
END $$;
EOF
fi

echo ""
echo "✅ Quota configuration complete!"
echo ""
echo "Next steps:"
echo "  1. Clear any existing rate limit state: make clean-redis"
echo "  2. Run CATS fuzzing: make cats-fuzz"
echo ""
