#!/bin/bash

# Script to run integration tests against a test database
# This script sets up a test database environment and runs the integration tests

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${YELLOW}TMI Integration Test Runner${NC}"
echo "=================================="

# Check if Docker is running
if ! docker info >/dev/null 2>&1; then
    echo -e "${RED}Error: Docker is not running. Please start Docker and try again.${NC}"
    exit 1
fi

# Default test database configuration
TEST_DB_HOST=${TEST_DB_HOST:-localhost}
TEST_DB_PORT=${TEST_DB_PORT:-5433}
TEST_DB_USER=${TEST_DB_USER:-tmi_test}
TEST_DB_PASSWORD=${TEST_DB_PASSWORD:-test123}
TEST_DB_NAME=${TEST_DB_NAME:-tmi_test}

TEST_REDIS_HOST=${TEST_REDIS_HOST:-localhost}
TEST_REDIS_PORT=${TEST_REDIS_PORT:-6380}

# Container names
POSTGRES_CONTAINER="tmi-integration-test-postgres"
REDIS_CONTAINER="tmi-integration-test-redis"

# Function to cleanup containers
cleanup() {
    echo -e "${YELLOW}Cleaning up test containers...${NC}"
    docker stop $POSTGRES_CONTAINER >/dev/null 2>&1 || true
    docker rm $POSTGRES_CONTAINER >/dev/null 2>&1 || true
    docker stop $REDIS_CONTAINER >/dev/null 2>&1 || true
    docker rm $REDIS_CONTAINER >/dev/null 2>&1 || true
}

# Cleanup on exit
trap cleanup EXIT

# Start PostgreSQL test container
echo -e "${YELLOW}Starting PostgreSQL test container...${NC}"
docker run -d \
    --name $POSTGRES_CONTAINER \
    -e POSTGRES_USER=$TEST_DB_USER \
    -e POSTGRES_PASSWORD=$TEST_DB_PASSWORD \
    -e POSTGRES_DB=$TEST_DB_NAME \
    -p $TEST_DB_PORT:5432 \
    postgres:13 >/dev/null

# Start Redis test container
echo -e "${YELLOW}Starting Redis test container...${NC}"
docker run -d \
    --name $REDIS_CONTAINER \
    -p $TEST_REDIS_PORT:6379 \
    redis:6 >/dev/null

# Wait for containers to be ready
echo -e "${YELLOW}Waiting for databases to be ready...${NC}"
sleep 5

# Check PostgreSQL connectivity
echo -e "${YELLOW}Checking PostgreSQL connectivity...${NC}"
max_attempts=30
attempt=1
while [ $attempt -le $max_attempts ]; do
    if docker exec $POSTGRES_CONTAINER pg_isready -U $TEST_DB_USER >/dev/null 2>&1; then
        echo -e "${GREEN}PostgreSQL is ready!${NC}"
        break
    fi
    if [ $attempt -eq $max_attempts ]; then
        echo -e "${RED}PostgreSQL failed to start after $max_attempts attempts${NC}"
        exit 1
    fi
    echo "Waiting for PostgreSQL... (attempt $attempt/$max_attempts)"
    sleep 2
    ((attempt++))
done

# Check Redis connectivity
echo -e "${YELLOW}Checking Redis connectivity...${NC}"
if docker exec $REDIS_CONTAINER redis-cli ping >/dev/null 2>&1; then
    echo -e "${GREEN}Redis is ready!${NC}"
else
    echo -e "${RED}Redis failed to start${NC}"
    exit 1
fi

# Run database migrations if they exist
if [ -f "cmd/migrate/main.go" ]; then
    echo -e "${YELLOW}Running database migrations...${NC}"
    TEST_DB_HOST=$TEST_DB_HOST \
    TEST_DB_PORT=$TEST_DB_PORT \
    TEST_DB_USER=$TEST_DB_USER \
    TEST_DB_PASSWORD=$TEST_DB_PASSWORD \
    TEST_DB_NAME=$TEST_DB_NAME \
    go run cmd/migrate/main.go || {
        echo -e "${YELLOW}Note: Migration failed or not needed. Continuing with tests...${NC}"
    }
fi

# Set environment variables for tests
export TEST_DB_HOST
export TEST_DB_PORT
export TEST_DB_USER
export TEST_DB_PASSWORD
export TEST_DB_NAME
export TEST_REDIS_HOST
export TEST_REDIS_PORT

# Run the integration tests
echo -e "${YELLOW}Running integration tests...${NC}"
echo "Database: postgresql://$TEST_DB_USER:***@$TEST_DB_HOST:$TEST_DB_PORT/$TEST_DB_NAME"
echo "Redis: $TEST_REDIS_HOST:$TEST_REDIS_PORT"
echo ""

# Run tests without the -short flag to include integration tests
if go test -v ./api -run TestIntegration; then
    echo -e "${GREEN}Integration tests passed!${NC}"
    exit 0
else
    echo -e "${RED}Integration tests failed!${NC}"
    exit 1
fi