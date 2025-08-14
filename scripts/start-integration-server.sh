#!/bin/bash

# TMI Integration Test Server Startup Script
# Starts the TMI server with integration test configuration

set -e

# Configuration
SERVER_PORT=8081
LOG_FILE="server-integration.log"

# Environment variables for integration test server
export POSTGRES_HOST=localhost
export POSTGRES_PORT=5433
export POSTGRES_USER=tmi_dev
export POSTGRES_PASSWORD=dev123
export POSTGRES_DB=tmi_integration_test
export REDIS_HOST=localhost
export REDIS_PORT=6380
export REDIS_DB=0
export JWT_SECRET=integration-test-jwt-secret-key-for-testing-only
export SERVER_PORT=$SERVER_PORT
export LOG_LEVEL=INFO
export ENVIRONMENT=integration_test

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

# Ensure we have a server binary
if [ ! -f "bin/server" ]; then
    echo "❌ Server binary not found at bin/server"
    echo "   Run 'make build-server' first"
    exit 1
fi

# Create integration test configuration based on existing test config
if [ ! -f "config-integration-test.yaml" ] || [ "config-test.yaml" -nt "config-integration-test.yaml" ]; then
    log_info "Creating integration test configuration..."
    # Copy the test config and modify it for integration testing
    sed "s/port: \"0\"/port: \"$SERVER_PORT\"/" config-test.yaml | \
    sed "s/port: \"5432\"/port: \"5433\"/" | \
    sed "s/port: \"6379\"/port: \"6380\"/" | \
    sed "s/database: \"tmi_test\"/database: \"tmi_integration_test\"/" | \
    sed "s/callback_url: \"http:\/\/localhost:8080\"/callback_url: \"http:\/\/localhost:8081\"/" | \
    sed "s/authorization_url: \"http:\/\/localhost:8080\"/authorization_url: \"http:\/\/localhost:8081\"/" | \
    sed "s/token_url: \"http:\/\/localhost:8080\"/token_url: \"http:\/\/localhost:8081\"/" \
    > config-integration-test.yaml
fi

log_info "Starting TMI server for integration testing..."
log_info "Server will log to: $LOG_FILE"
log_info "Server will run on: http://localhost:$SERVER_PORT"

# Start the server
exec ./bin/server --config=config-integration-test.yaml > $LOG_FILE 2>&1