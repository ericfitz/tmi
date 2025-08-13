#!/bin/bash

# Script to start the TMI service with development configuration
# This script ensures the PostgreSQL and Redis Docker containers are running before starting the service

# Set the script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Change to the project root directory
cd "$PROJECT_ROOT" || { echo "Error: Failed to change to project root directory"; exit 1; }

# Ensure the PostgreSQL Docker container is running
echo "Ensuring PostgreSQL Docker container is running..."
"$SCRIPT_DIR/start-dev-db.sh" || { echo "Error: Failed to start PostgreSQL container"; exit 1; }

# Ensure the Redis Docker container is running
echo "Ensuring Redis Docker container is running..."
"$SCRIPT_DIR/start-dev-redis.sh" || { echo "Error: Failed to start Redis container"; exit 1; }

# Ensure all database migrations are applied
echo "Ensuring database migrations are applied..."
"$SCRIPT_DIR/ensure-migrations.sh" || { echo "Error: Database migrations failed"; exit 1; }

# Check if config-development.yaml exists, generate if not
if [ ! -f config-development.yaml ]; then
    echo "Generating development configuration..."
    go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }
fi

# Start the TMI service with the development configuration and dev build tags
echo "Starting TMI service with development configuration..."
echo "Server output will be logged to server.log"
go run -tags dev cmd/server/main.go --config=config-development.yaml > server.log 2>&1 &
