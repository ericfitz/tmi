#!/bin/bash

# Script to start the TMI service with production configuration
# This script is designed for production deployment and requires external database setup

# Set the script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Change to the project root directory
cd "$PROJECT_ROOT" || { echo "Error: Failed to change to project root directory"; exit 1; }

# Check if config-production.yaml exists
if [ ! -f config-production.yaml ]; then
    echo "Error: config-production.yaml not found"
    echo "Please copy config-example.yaml to config-production.yaml and configure it for production"
    exit 1
fi

# Ensure binary is built
if [ ! -f bin/server ]; then
    echo "Building TMI server..."
    make build || { echo "Error: Failed to build server"; exit 1; }
fi

# Validate critical environment variables are set
if [ -z "$TMI_AUTH_JWT_SECRET" ]; then
    echo "Error: TMI_AUTH_JWT_SECRET environment variable must be set in production"
    exit 1
fi

if [ -z "$TMI_DATABASE_POSTGRES_PASSWORD" ]; then
    echo "Error: TMI_DATABASE_POSTGRES_PASSWORD environment variable must be set"
    exit 1
fi

# Start the TMI service with the production configuration
echo "Starting TMI service with production configuration..."
echo "Configuration file: config-production.yaml"
echo "Log level: WARN"
echo "TLS enabled: Check config-production.yaml"
echo ""

./bin/server --config=config-production.yaml

# Note: The above command will block until the service is stopped
# To stop the service, press Ctrl+C or send SIGTERM signal