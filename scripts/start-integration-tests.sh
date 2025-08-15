#!/bin/bash

# TMI Integration Test Runner
# This script automatically sets up test databases, runs integration tests, and cleans up

# Note: We don't use 'set -e' here because we want to handle test failures gracefully
# and still perform cleanup while preserving the correct exit code

# Configuration (matches existing setup scripts)
POSTGRES_TEST_PORT=5433
REDIS_TEST_PORT=6380
POSTGRES_CONTAINER="tmi-integration-postgres"
REDIS_CONTAINER="tmi-integration-redis"
POSTGRES_USER="tmi_dev"
POSTGRES_PASSWORD="dev123"
POSTGRES_DB="tmi_integration_test"
SERVER_PORT=8081

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

# Cleanup function - uses same cleanup logic as Makefile test-integration-cleanup target
cleanup() {
    local exit_code=$?
    log_info "Cleaning up integration test environment..."
    
    # Stop integration server
    if [ -f .integration-server.pid ]; then
        PID=$(cat .integration-server.pid)
        if ps -p $PID > /dev/null 2>&1; then
            log_info "Stopping integration server (PID: $PID)..."
            kill $PID 2>/dev/null || true
            sleep 2
            if ps -p $PID > /dev/null 2>&1; then
                log_info "Force killing integration server (PID: $PID)..."
                kill -9 $PID 2>/dev/null || true
            fi
        fi
        rm -f .integration-server.pid
    fi
    
    # Check for processes listening on port
    PIDS=$(lsof -ti :$SERVER_PORT 2>/dev/null || true)
    if [ -n "$PIDS" ]; then
        log_info "Found processes on port $SERVER_PORT: $PIDS"
        for PID in $PIDS; do
            log_info "Stopping process $PID listening on port $SERVER_PORT..."
            kill $PID 2>/dev/null || true
            sleep 1
            if ps -p $PID > /dev/null 2>&1; then
                log_info "Force killing process $PID..."
                kill -9 $PID 2>/dev/null || true
            fi
        done
    fi
    
    # Stop containers
    log_info "Stopping integration test containers..."
    docker stop $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    docker rm $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    
    # Clean up log files
    rm -f server-integration.log integration-test.log config-integration-test.yaml
    
    log_success "Cleanup completed"
    exit $exit_code
}

# Trap cleanup on script exit, preserving exit code
trap cleanup EXIT

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
    
    # 1. Cleanup any existing environment (same as Makefile)
    log_info "1️⃣  Cleaning up any existing test environment..."
    pkill -f "bin/server.*test" || true
    pkill -f "go run.*server.*test" || true
    docker rm -f $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    sleep 2
    
    # 2. Start test databases using existing script
    log_info "2️⃣  Starting test databases..."
    if ! ./scripts/start-integration-db.sh; then
        log_error "Failed to start integration databases"
        return 1
    fi
    
    # 3. Build server binary
    log_info "3️⃣  Building server binary..."
    if ! make build-server; then
        log_error "Failed to build server"
        return 1
    fi
    
    # 4. Start test server using existing script
    log_info "4️⃣  Starting test server..."
    ./scripts/start-integration-server.sh &
    echo $! > .integration-server.pid
    sleep 2
    
    if [ -f .integration-server.pid ]; then
        log_info "Server started with PID: $(cat .integration-server.pid)"
    else
        log_error "Failed to capture server PID"
        return 1
    fi
    
    # 5. Wait for server to be ready
    log_info "5️⃣  Waiting for server to be ready..."
    for i in 1 2 3 4 5 6 7 8 9 10; do
        if curl -s http://localhost:$SERVER_PORT/ >/dev/null 2>&1; then
            log_success "Server is ready!"
            break
        fi
        log_info "   Waiting for server... (attempt $i/10)"
        sleep 3
    done
    
    if ! curl -s http://localhost:$SERVER_PORT/ >/dev/null 2>&1; then
        log_error "Server failed to start within timeout"
        cat server-integration.log 2>/dev/null || true
        return 1
    fi
    
    # 6. Run integration tests
    log_info "6️⃣  Running integration tests..."
    if [ -n "$TEST_NAME" ]; then
        log_info "Running specific test: $TEST_NAME"
        TEST_PATTERN="$TEST_NAME"
    else
        log_info "Running all integration tests..."
        TEST_PATTERN="Integration"
    fi
    
    # Run tests and capture exit code (matches Makefile environment)
    TEST_EXIT_CODE=0
    TEST_DB_HOST=localhost \
    TEST_DB_PORT=$POSTGRES_TEST_PORT \
    TEST_DB_USER=$POSTGRES_USER \
    TEST_DB_PASSWORD=$POSTGRES_PASSWORD \
    TEST_DB_NAME=$POSTGRES_DB \
    TEST_REDIS_HOST=localhost \
    TEST_REDIS_PORT=$REDIS_TEST_PORT \
    TEST_SERVER_URL=http://localhost:$SERVER_PORT \
        go test -v -timeout=10m ./api/... -run "$TEST_PATTERN" \
        | tee integration-test.log \
        || TEST_EXIT_CODE=$?
    
    echo ""
    if [ $TEST_EXIT_CODE -eq 0 ]; then
        log_success "All integration tests passed!"
    else
        log_error "Integration tests failed with exit code $TEST_EXIT_CODE"
        echo ""
        log_info "📋 Failed test summary:"
        grep -E "FAIL:|--- FAIL" integration-test.log || true
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