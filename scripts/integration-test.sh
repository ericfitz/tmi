#!/bin/bash

# TMI Integration Test Runner
# This script automatically sets up test databases, runs integration tests, and cleans up

# Note: We don't use 'set -e' here because we want to handle test failures gracefully
# and still perform cleanup while preserving the correct exit code

# Configuration
POSTGRES_TEST_PORT=5433
REDIS_TEST_PORT=6380
POSTGRES_CONTAINER="tmi-integration-postgres"
REDIS_CONTAINER="tmi-integration-redis"
POSTGRES_USER="tmi_dev"
POSTGRES_PASSWORD="dev123"
POSTGRES_DB="tmi_integration_test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Cleanup function
cleanup() {
    local exit_code=$?
    log_info "Cleaning up test containers..."
    docker stop $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    docker rm $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    log_success "Cleanup completed"
    exit $exit_code
}

# Trap cleanup on script exit, preserving exit code
trap cleanup EXIT

# Function to wait for PostgreSQL to be ready
wait_for_postgres() {
    log_info "Waiting for PostgreSQL to be ready..."
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if docker exec $POSTGRES_CONTAINER pg_isready -h localhost -p 5432 -U $POSTGRES_USER >/dev/null 2>&1; then
            log_success "PostgreSQL is ready!"
            return 0
        fi
        
        echo -n "."
        sleep 1
        attempt=$((attempt + 1))
    done
    
    log_error "PostgreSQL failed to start within $max_attempts seconds"
    return 1
}

# Function to wait for Redis to be ready
wait_for_redis() {
    log_info "Waiting for Redis to be ready..."
    local max_attempts=30
    local attempt=1
    
    while [ $attempt -le $max_attempts ]; do
        if docker exec $REDIS_CONTAINER redis-cli ping >/dev/null 2>&1; then
            log_success "Redis is ready!"
            return 0
        fi
        
        echo -n "."
        sleep 1
        attempt=$((attempt + 1))
    done
    
    log_error "Redis failed to start within $max_attempts seconds"
    return 1
}

# Function to check if container is running
is_container_running() {
    docker ps --format '{{.Names}}' | grep -q "^$1$"
}

# Main execution
main() {
    echo -e "${BLUE}================================${NC}"
    echo -e "${BLUE}TMI Integration Test Runner${NC}"
    echo -e "${BLUE}================================${NC}"
    
    # Check if Docker is running
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not running. Please start Docker and try again."
        exit 1
    fi
    
    # Stop and remove existing containers if they exist
    log_info "Cleaning up any existing test containers..."
    docker stop $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    docker rm $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    
    # Start PostgreSQL container
    log_info "Starting PostgreSQL test container..."
    docker run -d \
        --name $POSTGRES_CONTAINER \
        -p $POSTGRES_TEST_PORT:5432 \
        -e POSTGRES_USER=$POSTGRES_USER \
        -e POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
        -e POSTGRES_DB=$POSTGRES_DB \
        postgres:13 >/dev/null
    
    # Start Redis container
    log_info "Starting Redis test container..."
    docker run -d \
        --name $REDIS_CONTAINER \
        -p $REDIS_TEST_PORT:6379 \
        redis:7 >/dev/null
    
    # Wait for databases to be ready
    if ! wait_for_postgres; then
        log_error "Failed to start PostgreSQL"
        return 1
    fi
    
    if ! wait_for_redis; then
        log_error "Failed to start Redis"
        return 1
    fi
    
    # Run database migrations (single source of truth)
    log_info "Running database migrations..."
    if ! POSTGRES_HOST=localhost \
    POSTGRES_PORT=$POSTGRES_TEST_PORT \
    POSTGRES_USER=$POSTGRES_USER \
    POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
    POSTGRES_DB=$POSTGRES_DB \
        go run cmd/migrate/main.go up; then
        log_error "Database migration failed!"
        return 1
    fi
    
    # Validate migration state
    log_info "Validating database migration state..."
    if ! POSTGRES_HOST=localhost \
    POSTGRES_PORT=$POSTGRES_TEST_PORT \
    POSTGRES_USER=$POSTGRES_USER \
    POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
    POSTGRES_DB=$POSTGRES_DB \
        go run cmd/check-db/main.go; then
        log_error "Migration validation failed in integration test setup!"
        return 1
    fi
    
    # Run integration tests
    if [ -n "$TEST_NAME" ]; then
        log_info "Running specific integration test: $TEST_NAME"
        TEST_PATTERN="$TEST_NAME"
    else
        log_info "Running all integration tests..."
        TEST_PATTERN="(TestDatabase.*Integration|Test.*Integration|TestIntegrationWithRedis|TestRedisConsistency|TestPerformanceWithAndWithoutRedis)"
    fi
    
    # Run tests and capture exit code
    TEST_EXIT_CODE=0
    TEST_DB_HOST=localhost \
    TEST_DB_PORT=$POSTGRES_TEST_PORT \
    TEST_DB_USER=$POSTGRES_USER \
    TEST_DB_PASSWORD=$POSTGRES_PASSWORD \
    TEST_DB_NAME=$POSTGRES_DB \
    TEST_REDIS_HOST=localhost \
    TEST_REDIS_PORT=$REDIS_TEST_PORT \
        go test -v ./api -run "$TEST_PATTERN" || TEST_EXIT_CODE=$?
    
    if [ $TEST_EXIT_CODE -eq 0 ]; then
        log_success "Integration tests completed successfully!"
    else
        log_error "Integration tests failed with exit code $TEST_EXIT_CODE"
    fi
    
    return $TEST_EXIT_CODE
}

# Show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -h, --help              Show this help message"
    echo "  --cleanup-only          Only cleanup existing containers and exit"
    echo "  --test-name <name>      Run only the specified test by name"
    echo ""
    echo "This script will:"
    echo "  1. Start PostgreSQL on port $POSTGRES_TEST_PORT"
    echo "  2. Start Redis on port $REDIS_TEST_PORT"
    echo "  3. Run database migrations"
    echo "  4. Run integration tests"
    echo "  5. Clean up containers on exit"
}

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -h|--help)
            show_usage
            exit 0
            ;;
        --cleanup-only)
            cleanup
            exit 0
            ;;
        --test-name)
            TEST_NAME="$2"
            shift 2
            ;;
        *)
            log_error "Unknown option: $1"
            show_usage
            exit 1
            ;;
    esac
done

# Run main function and preserve its exit code
main
exit $?