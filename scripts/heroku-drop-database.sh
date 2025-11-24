#!/bin/bash
# scripts/heroku-drop-database.sh
#
# Drop the Heroku PostgreSQL database schema, leaving the database in an empty state
# Unlike heroku-reset-database.sh, this does NOT run migrations after dropping
#
# Usage: ./scripts/heroku-drop-database.sh [--yes] [app-name]
# Options:
#   --yes       Skip confirmation prompt (use with caution!)
#   app-name    Name of the Heroku app (default: tmi-server)
#
# Examples:
#   ./scripts/heroku-drop-database.sh                    # Interactive mode for tmi-server
#   ./scripts/heroku-drop-database.sh --yes              # Skip confirmation for tmi-server
#   ./scripts/heroku-drop-database.sh my-app             # Interactive mode for my-app
#   ./scripts/heroku-drop-database.sh --yes my-app       # Skip confirmation for my-app

set -e  # Exit on error

# Color codes for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Parse command line arguments
SKIP_CONFIRM=false
APP_NAME="tmi-server"

for arg in "$@"; do
    case $arg in
        --yes)
            SKIP_CONFIRM=true
            shift
            ;;
        *)
            APP_NAME="$arg"
            shift
            ;;
    esac
done

echo -e "${BLUE}========================================${NC}"
echo -e "${BLUE}Heroku Database Drop Script${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "App: ${GREEN}${APP_NAME}${NC}"
echo ""

# Confirmation prompt (unless --yes flag is provided)
if [ "$SKIP_CONFIRM" = false ]; then
    echo -e "${YELLOW}WARNING: This will DELETE ALL DATA in the ${APP_NAME} database!${NC}"
    echo -e "${YELLOW}This action cannot be undone.${NC}"
    echo -e "${YELLOW}The database will be left in an EMPTY state with no tables.${NC}"
    echo ""
    read -p "Are you sure you want to continue? (type 'yes' to confirm): " CONFIRM

    if [ "$CONFIRM" != "yes" ]; then
        echo -e "${RED}Aborted.${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}WARNING: Proceeding with database drop (--yes flag provided)${NC}"
fi

echo ""
echo -e "${BLUE}Dropping all tables...${NC}"
echo -e "${YELLOW}Executing DROP SCHEMA CASCADE...${NC}"
# Note: We don't grant to 'postgres' as this role doesn't exist in Heroku
# The current database user already has all necessary permissions
heroku run -a "$APP_NAME" 'echo "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" | psql $DATABASE_URL' 2>&1 | grep -E "(DROP|CREATE|ERROR|drop cascades)" || true

echo ""
echo -e "${GREEN}✓ Schema dropped successfully${NC}"
echo ""

# Verify the database is empty
echo -e "${BLUE}Verifying database state...${NC}"
TABLE_COUNT=$(heroku run -a "$APP_NAME" "echo \"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';\" | psql \$DATABASE_URL" 2>&1 | grep -E "^\s*[0-9]+\s*$" | tr -d '[:space:]')

echo -e "Tables in database: ${GREEN}${TABLE_COUNT}${NC}"

if [ "$TABLE_COUNT" = "0" ]; then
    echo -e "  ${GREEN}✓ Database is empty${NC}"
else
    echo -e "  ${YELLOW}⚠ Database has ${TABLE_COUNT} tables (expected 0)${NC}"
fi

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Database drop complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Database state:${NC}"
echo -e "  - All tables have been deleted"
echo -e "  - Schema 'public' exists but is empty"
echo -e "  - Ready for migrations or manual schema creation"
echo ""
echo -e "${BLUE}To restore the database:${NC}"
echo -e "  1. Run 'make heroku-reset-db' to drop and run migrations"
echo -e "  2. Or restart the Heroku app to trigger automatic migrations"
echo -e "  3. Or manually run: heroku run -a ${APP_NAME} '/app/bin/server migrate'"
echo ""
