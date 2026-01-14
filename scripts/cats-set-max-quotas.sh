#!/bin/bash
# cats-set-max-quotas.sh
# Sets maximum quotas and rate limits for CATS test user to prevent rate-limit errors
#
# This script sets all quotas to maximum values for the CATS test user (charlie@tmi.local)
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
echo "   Target user: charlie@tmi.local (TMI provider)"
echo "   Database: ${DB_NAME} on ${DB_HOST}:${DB_PORT}"
echo ""

# Execute the SQL script
if docker ps --format "{{.Names}}" | grep -q "^${POSTGRES_CONTAINER}$"; then
    echo "   Using Docker container: ${POSTGRES_CONTAINER}"
    echo ""
    docker exec -i "${POSTGRES_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Set maximum quotas for CATS test user
-- This prevents rate-limiting during intensive fuzzing
-- Uses very high values (100000/min, 1000000/hour) to handle 14000+ CATS tests

DO $$
DECLARE
    v_user_uuid UUID;
    v_api_quota_count INT;
BEGIN
    -- Get the internal UUID for charlie@tmi.local
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'tmi'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@tmi.local) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user already has API quotas set
        SELECT COUNT(*) INTO v_api_quota_count
        FROM user_api_quotas
        WHERE user_internal_uuid = v_user_uuid;

        IF v_api_quota_count > 0 THEN
            -- Update existing API quotas to maximum values
            UPDATE user_api_quotas
            SET
                max_requests_per_minute = 100000,
                max_requests_per_hour = 1000000,
                modified_at = NOW()
            WHERE user_internal_uuid = v_user_uuid;

            RAISE NOTICE 'Updated API quotas for charlie@tmi.local';
        ELSE
            -- Insert maximum API quotas
            INSERT INTO user_api_quotas (
                user_internal_uuid,
                max_requests_per_minute,
                max_requests_per_hour,
                created_at,
                modified_at
            ) VALUES (
                v_user_uuid,
                100000,  -- Very high limit for CATS fuzzing
                1000000, -- Very high limit for CATS fuzzing
                NOW(),
                NOW()
            );

            RAISE NOTICE 'Created maximum API quotas for charlie@tmi.local';
        END IF;

        -- Display current quota settings
        RAISE NOTICE 'Current quota settings:';
        RAISE NOTICE '  max_requests_per_minute: 100000';
        RAISE NOTICE '  max_requests_per_hour: 1000000';
    END IF;
END $$;
EOF
else
    echo "   Using direct psql connection"
    echo ""
    PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Set maximum quotas for CATS test user
-- This prevents rate-limiting during intensive fuzzing
-- Uses very high values (100000/min, 1000000/hour) to handle 14000+ CATS tests

DO $$
DECLARE
    v_user_uuid UUID;
    v_api_quota_count INT;
BEGIN
    -- Get the internal UUID for charlie@tmi.local
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'tmi'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@tmi.local) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user already has API quotas set
        SELECT COUNT(*) INTO v_api_quota_count
        FROM user_api_quotas
        WHERE user_internal_uuid = v_user_uuid;

        IF v_api_quota_count > 0 THEN
            -- Update existing API quotas to maximum values
            UPDATE user_api_quotas
            SET
                max_requests_per_minute = 100000,
                max_requests_per_hour = 1000000,
                modified_at = NOW()
            WHERE user_internal_uuid = v_user_uuid;

            RAISE NOTICE 'Updated API quotas for charlie@tmi.local';
        ELSE
            -- Insert maximum API quotas
            INSERT INTO user_api_quotas (
                user_internal_uuid,
                max_requests_per_minute,
                max_requests_per_hour,
                created_at,
                modified_at
            ) VALUES (
                v_user_uuid,
                100000,  -- Very high limit for CATS fuzzing
                1000000, -- Very high limit for CATS fuzzing
                NOW(),
                NOW()
            );

            RAISE NOTICE 'Created maximum API quotas for charlie@tmi.local';
        END IF;

        -- Display current quota settings
        RAISE NOTICE 'Current quota settings:';
        RAISE NOTICE '  max_requests_per_minute: 100000';
        RAISE NOTICE '  max_requests_per_hour: 1000000';
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
