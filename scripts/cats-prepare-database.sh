#!/bin/bash
# cats-prepare-database.sh
# Prepares the database for CATS fuzzing by granting admin privileges to the test user
#
# This script grants administrator privileges to the CATS test user (charlie@test.tmi)
# to eliminate 86% of CATS fuzzing errors related to 401/403 authorization failures.
#
# Usage: ./scripts/cats-prepare-database.sh

set -e

# Database connection parameters
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-tmi_dev}"
DB_USER="${DB_USER:-tmi_dev}"
DB_PASSWORD="${DB_PASSWORD:-tmi_dev}"
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-tmi-postgresql}"

echo "🔧 Preparing database for CATS fuzzing..."
echo "   Target user: charlie@test.tmi (test provider)"
echo "   Database: ${DB_NAME} on ${DB_HOST}:${DB_PORT}"
echo ""

# Execute the SQL script
if docker ps --format "{{.Names}}" | grep -q "^${POSTGRES_CONTAINER}$"; then
    echo "   Using Docker container: ${POSTGRES_CONTAINER}"
    echo ""
    docker exec -i "${POSTGRES_CONTAINER}" psql -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Grant admin privileges to CATS test user
-- This allows the test user to access all endpoints during fuzzing

DO $$
DECLARE
    v_user_uuid UUID;
    v_admin_count INT;
BEGIN
    -- Get the internal UUID for charlie@test.tmi
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'test'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@test.tmi) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user is already an administrator
        SELECT COUNT(*) INTO v_admin_count
        FROM administrators
        WHERE user_internal_uuid = v_user_uuid
          AND subject_type = 'user';

        IF v_admin_count > 0 THEN
            RAISE NOTICE 'User charlie@test.tmi is already an administrator.';
        ELSE
            -- Grant admin privileges
            INSERT INTO administrators (
                user_internal_uuid,
                subject_type,
                provider,
                notes
            ) VALUES (
                v_user_uuid,
                'user',
                'test',
                'Auto-granted for CATS fuzzing - allows comprehensive API testing'
            );

            RAISE NOTICE 'Successfully granted admin privileges to charlie@test.tmi';
        END IF;

        -- Display the user info
        RAISE NOTICE 'User internal_uuid: %', v_user_uuid;
    END IF;
END $$;
EOF
else
    echo "   Using direct psql connection"
    echo ""
    PGPASSWORD="${DB_PASSWORD}" psql -h "${DB_HOST}" -p "${DB_PORT}" -U "${DB_USER}" -d "${DB_NAME}" <<'EOF'
-- Grant admin privileges to CATS test user
-- This allows the test user to access all endpoints during fuzzing

DO $$
DECLARE
    v_user_uuid UUID;
    v_admin_count INT;
BEGIN
    -- Get the internal UUID for charlie@test.tmi
    SELECT internal_uuid INTO v_user_uuid
    FROM users
    WHERE provider_user_id = 'charlie'
      AND provider = 'test'
    LIMIT 1;

    IF v_user_uuid IS NULL THEN
        RAISE NOTICE 'CATS test user (charlie@test.tmi) not found in database.';
        RAISE NOTICE 'The user will be created during the first OAuth login.';
        RAISE NOTICE 'Run CATS fuzzing once, then run this script again.';
    ELSE
        -- Check if user is already an administrator
        SELECT COUNT(*) INTO v_admin_count
        FROM administrators
        WHERE user_internal_uuid = v_user_uuid
          AND subject_type = 'user';

        IF v_admin_count > 0 THEN
            RAISE NOTICE 'User charlie@test.tmi is already an administrator.';
        ELSE
            -- Grant admin privileges
            INSERT INTO administrators (
                user_internal_uuid,
                subject_type,
                provider,
                notes
            ) VALUES (
                v_user_uuid,
                'user',
                'test',
                'Auto-granted for CATS fuzzing - allows comprehensive API testing'
            );

            RAISE NOTICE 'Successfully granted admin privileges to charlie@test.tmi';
        END IF;

        -- Display the user info
        RAISE NOTICE 'User internal_uuid: %', v_user_uuid;
    END IF;
END $$;
EOF
fi

echo ""
echo "✅ Database preparation complete!"
echo ""
echo "Next steps:"
echo "  1. Run CATS fuzzing: make cats-fuzz"
echo "  2. Expected improvement: ~16,367 fewer errors (86% reduction)"
echo ""
