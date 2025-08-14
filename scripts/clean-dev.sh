#!/bin/bash
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}🧹 Cleaning development environment${NC}"

echo -e "${YELLOW}1. 🗃️ Cleaning up databases and Redis...${NC}"
make delete-dev-db > /dev/null 2>&1 || true
make delete-dev-redis > /dev/null 2>&1 || true

echo -e "${YELLOW}2. 🛑 Killing any processes on port 8080...${NC}"
lsof -ti:8080 | xargs kill -9 2>/dev/null || true

echo -e "${YELLOW}3. ⏳ Waiting for processes to terminate...${NC}"
sleep 2

echo -e "${YELLOW}4. 🔍 Verifying port 8080 is free...${NC}"
if lsof -i:8080 > /dev/null 2>&1; then
    echo -e "${RED}❌ Port 8080 is still in use:${NC}"
    lsof -i:8080
    exit 1
else
    echo -e "${GREEN}✅ Port 8080 is free${NC}"
fi

echo -e "${GREEN}🎉 Development environment cleaned successfully${NC}"