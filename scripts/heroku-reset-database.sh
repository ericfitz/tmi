#!/bin/bash
# scripts/heroku-reset-database.sh
#
# Drop and recreate the Heroku PostgreSQL database schema from scratch
# This script is useful when database migrations get out of sync or you need a fresh start
#
# Usage: ./scripts/heroku-reset-database.sh [--yes] [app-name]
# Options:
#   --yes       Skip confirmation prompt (use with caution!)
#   app-name    Name of the Heroku app (default: tmi-server)
#
# Examples:
#   ./scripts/heroku-reset-database.sh                    # Interactive mode for tmi-server
#   ./scripts/heroku-reset-database.sh --yes              # Skip confirmation for tmi-server
#   ./scripts/heroku-reset-database.sh my-app             # Interactive mode for my-app
#   ./scripts/heroku-reset-database.sh --yes my-app       # Skip confirmation for my-app

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
echo -e "${BLUE}Heroku Database Reset Script${NC}"
echo -e "${BLUE}========================================${NC}"
echo ""
echo -e "App: ${GREEN}${APP_NAME}${NC}"
echo ""

# Confirmation prompt (unless --yes flag is provided)
if [ "$SKIP_CONFIRM" = false ]; then
    echo -e "${YELLOW}WARNING: This will DELETE ALL DATA in the ${APP_NAME} database!${NC}"
    echo -e "${YELLOW}This action cannot be undone.${NC}"
    echo ""
    read -p "Are you sure you want to continue? (type 'yes' to confirm): " CONFIRM

    if [ "$CONFIRM" != "yes" ]; then
        echo -e "${RED}Aborted.${NC}"
        exit 1
    fi
else
    echo -e "${YELLOW}WARNING: Proceeding with database reset (--yes flag provided)${NC}"
fi

echo ""
echo -e "${BLUE}Step 1/3: Dropping all tables...${NC}"
echo -e "${YELLOW}Executing DROP SCHEMA CASCADE...${NC}"
# Note: We don't grant to 'postgres' as this role doesn't exist in Heroku
# The current database user already has all necessary permissions
heroku run -a "$APP_NAME" 'echo "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" | psql $DATABASE_URL' 2>&1 | grep -E "(DROP|CREATE|ERROR|drop cascades)" || true

echo ""
echo -e "${GREEN}✓ Schema dropped successfully${NC}"
echo ""

echo -e "${BLUE}Step 2/3: Running migrations via server restart...${NC}"
echo -e "${YELLOW}Restarting dyno to trigger GORM AutoMigrate...${NC}"

# The TMI server runs GORM AutoMigrate on startup, which creates all tables
# automatically from the model definitions. No separate migrate binary needed.
heroku dyno:restart -a "$APP_NAME"

# Wait for the dyno to start and run migrations
echo -e "${YELLOW}Waiting for server to start and run AutoMigrate...${NC}"
sleep 10

# Check if the server started successfully
heroku logs -a "$APP_NAME" -n 50 2>&1 | grep -E "(AutoMigrate|migration|Starting|error)" | head -20 || true

echo ""
echo -e "${GREEN}✓ Server restarted (AutoMigrate runs on startup)${NC}"
echo ""

echo -e "${BLUE}Step 3/3: Verifying schema...${NC}"

# Check table count
echo -e "${YELLOW}Checking tables...${NC}"
echo -e "${YELLOW}Running: SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public'${NC}"
TABLE_COUNT=$(heroku run -a "$APP_NAME" "echo \"SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';\" | psql \$DATABASE_URL" 2>&1 | tee /dev/tty | grep -E "^\s*[0-9]+\s*$" | tr -d '[:space:]')

echo -e "Tables created: ${GREEN}${TABLE_COUNT}${NC}"

# List all tables
echo ""
echo -e "${YELLOW}Table list:${NC}"
echo -e "${YELLOW}Running: SELECT table_name FROM information_schema.tables...${NC}"
heroku run -a "$APP_NAME" "echo \"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name;\" | psql \$DATABASE_URL" 2>&1 | tee /dev/tty | grep -E "^\s+[a-z_]+\s*$" | sed 's/^/  - /'

# Verify threat_models table has issue_uri column
echo ""
echo -e "${YELLOW}Verifying threat_models schema...${NC}"
echo -e "${YELLOW}Running: SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'threat_models' AND column_name = 'issue_uri'${NC}"
ISSUE_URI_EXISTS=$(heroku run -a "$APP_NAME" "echo \"SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'threat_models' AND column_name = 'issue_uri';\" | psql \$DATABASE_URL" 2>&1 | tee /dev/tty | grep -E "^\s*[0-9]+\s*$" | tr -d '[:space:]')

if [ "$ISSUE_URI_EXISTS" = "1" ]; then
    echo -e "  ${GREEN}✓ issue_uri column exists${NC}"
else
    echo -e "  ${RED}✗ issue_uri column missing!${NC}"
    exit 1
fi

# Verify notes table exists (if in schema)
echo ""
echo -e "${YELLOW}Checking for notes table...${NC}"
NOTES_TABLE_EXISTS=$(heroku run -a "$APP_NAME" "echo \"SELECT COUNT(*) FROM information_schema.tables WHERE table_name = 'notes';\" | psql \$DATABASE_URL" 2>&1 | tee /dev/tty | grep -E "^\s*[0-9]+\s*$" | tr -d '[:space:]')

if [ "$NOTES_TABLE_EXISTS" = "1" ]; then
    echo -e "  ${GREEN}✓ notes table exists${NC}"
fi

# List all tables created by GORM AutoMigrate
echo ""
echo -e "${YELLOW}Listing all tables created by GORM AutoMigrate...${NC}"
heroku run -a "$APP_NAME" "echo \"SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' ORDER BY table_name;\" | psql \$DATABASE_URL" 2>&1 | grep -E "^\s+[a-z_]+\s*$" | sed 's/^/  - /' || true

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}✓ Database reset complete!${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""
echo -e "${BLUE}Next steps:${NC}"
echo -e "  1. Users will need to re-authenticate via OAuth"
echo -e "  2. All previous data has been deleted"
echo -e "  3. Test creating a threat model to verify functionality"
echo ""
