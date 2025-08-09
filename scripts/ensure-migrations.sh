#!/bin/bash

# Script to ensure all database migrations are applied successfully
# This script will fail hard if migrations are not in a clean state

set -e  # Exit on any error

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
CONTAINER_NAME="tmi-postgresql"

echo "🔍 Checking database migration state..."

# Change to project root
cd "$PROJECT_ROOT" || { echo "❌ Error: Failed to change to project root directory"; exit 1; }

# Check if PostgreSQL container is running
if ! docker ps --format '{{.Names}}' | grep -q $CONTAINER_NAME; then
    echo "❌ Error: PostgreSQL container '$CONTAINER_NAME' is not running"
    echo "   Run 'make dev-db' first to start the database"
    exit 1
fi

# Wait for PostgreSQL to be ready
echo "⏳ Waiting for PostgreSQL to be ready..."
MAX_RETRIES=30
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    if docker exec $CONTAINER_NAME pg_isready -U postgres -d tmi -h localhost -p 5432 > /dev/null 2>&1; then
        break
    fi
    RETRY_COUNT=$((RETRY_COUNT + 1))
    sleep 1
done

if [ $RETRY_COUNT -eq $MAX_RETRIES ]; then
    echo "❌ Error: PostgreSQL is not responding after $MAX_RETRIES seconds"
    exit 1
fi

echo "✅ PostgreSQL is ready"

# Run migration-based validation
echo "🔍 Validating migration state..."
cd cmd/check-db
VALIDATION_OUTPUT=$(go run main.go 2>&1)
VALIDATION_EXIT_CODE=$?

echo "$VALIDATION_OUTPUT"

# Check if validation passed
if [ $VALIDATION_EXIT_CODE -ne 0 ]; then
    echo ""
    echo "❌ MIGRATION VALIDATION FAILED!"
    echo ""
    
    # Check if this is a missing migrations issue
    if echo "$VALIDATION_OUTPUT" | grep -q "Missing migration"; then
        echo "🔧 ATTEMPTING AUTOMATIC MIGRATION..."
        echo ""
        
        # Try to run migrations automatically
        cd ../migrate
        echo "📦 Running database migrations..."
        
        if go run main.go up; then
            echo ""
            echo "✅ Migrations applied successfully!"
            echo "🔍 Re-validating migration state..."
            
            # Re-validate after applying migrations
            cd ../check-db
            if go run main.go > /dev/null 2>&1; then
                echo "✅ Migration validation now PASSED!"
                exit 0
            else
                echo "❌ Migration validation still failing after applying migrations"
                exit 1
            fi
        else
            echo ""
            echo "❌ FAILED TO APPLY MIGRATIONS!"
            echo ""
            echo "To fix this issue:"
            echo "1. Check the migration files in auth/migrations/"
            echo "2. Ensure the database is in a clean state"
            echo "3. Run migrations manually: cd cmd/migrate && go run main.go up"
            echo "4. Or reset the database: docker rm -f $CONTAINER_NAME && make dev-db"
            exit 1
        fi
    else
        echo ""
        echo "❌ MIGRATION VALIDATION FAILED WITH NON-MIGRATION ISSUES!"
        echo ""
        echo "The database schema validation failed for reasons other than missing migrations."
        echo "Check the error output above and fix the underlying issues."
        exit 1
    fi
else
    echo ""
    echo "✅ ALL MIGRATIONS ARE PROPERLY APPLIED!"
    echo "✅ Database schema is valid and consistent"
fi