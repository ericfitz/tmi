#!/bin/bash

# TMI Integration Test Database Startup Script
# Starts PostgreSQL and Redis containers for integration testing

set -e

# Configuration
POSTGRES_TEST_PORT=5433
REDIS_TEST_PORT=6380
POSTGRES_CONTAINER="tmi-integration-postgres"
REDIS_CONTAINER="tmi-integration-redis"
POSTGRES_USER="tmi_dev"
POSTGRES_PASSWORD="dev123"
POSTGRES_DB="tmi_integration_test"

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

# Function to wait for PostgreSQL to be ready
wait_for_postgres() {
    log_info "Waiting for PostgreSQL to be ready..."
    for i in {1..30}; do
        if docker exec $POSTGRES_CONTAINER pg_isready -U $POSTGRES_USER -d $POSTGRES_DB >/dev/null 2>&1; then
            log_success "PostgreSQL is ready!"
            return 0
        fi
        echo -n "."
        sleep 1
    done
    echo ""
    log_warning "PostgreSQL did not become ready within 30 seconds"
    return 1
}

# Function to wait for Redis to be ready
wait_for_redis() {
    log_info "Waiting for Redis to be ready..."
    for i in {1..30}; do
        if docker exec $REDIS_CONTAINER redis-cli ping >/dev/null 2>&1; then
            log_success "Redis is ready!"
            return 0
        fi
        echo -n "."
        sleep 1
    done
    echo ""
    log_warning "Redis did not become ready within 30 seconds"
    return 1
}

# Start PostgreSQL container
log_info "Starting PostgreSQL test container..."
docker run -d \
    --name $POSTGRES_CONTAINER \
    -e POSTGRES_USER=$POSTGRES_USER \
    -e POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
    -e POSTGRES_DB=$POSTGRES_DB \
    -p $POSTGRES_TEST_PORT:5432 \
    postgres:15-alpine >/dev/null

# Start Redis container
log_info "Starting Redis test container..."
docker run -d \
    --name $REDIS_CONTAINER \
    -p $REDIS_TEST_PORT:6379 \
    redis:7-alpine >/dev/null

# Wait for services to be ready
if ! wait_for_postgres; then
    log_warning "Failed to start PostgreSQL"
    exit 1
fi

if ! wait_for_redis; then
    log_warning "Failed to start Redis"
    exit 1
fi

# Run database migrations
log_info "Running database migrations..."
POSTGRES_HOST=localhost \
POSTGRES_PORT=$POSTGRES_TEST_PORT \
POSTGRES_USER=$POSTGRES_USER \
POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
POSTGRES_DB=$POSTGRES_DB \
    go run cmd/migrate/main.go up

log_success "Integration test databases are ready!"
echo "  PostgreSQL: localhost:$POSTGRES_TEST_PORT"
echo "  Redis: localhost:$REDIS_TEST_PORT"