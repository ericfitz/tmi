#!/bin/bash
# start-dev-oci.sh - Start TMI development environment with OCI Autonomous Database
#
# This script sets up the Oracle environment and starts the TMI server.
# It is gitignored because it contains environment-specific paths and credentials.
#
# Prerequisites:
#   1. Oracle Instant Client installed
#   2. Wallet extracted to ./wallet directory
#   3. Database user created in OCI ADB
#
# Usage:
#   ./scripts/start-dev-oci.sh
#
# Configuration:
#   Edit the variables below to match your environment

# ============================================================================
# CONFIGURATION
# ============================================================================
# Source OCI environment variables from oci-env.sh
# This file contains secrets and is gitignored.
# Copy oci-env.sh.example to oci-env.sh and edit with your values.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
OCI_ENV_FILE="$SCRIPT_DIR/oci-env.sh"

if [ -f "$OCI_ENV_FILE" ]; then
    source "$OCI_ENV_FILE"
else
    echo "ERROR: OCI environment file not found: $OCI_ENV_FILE"
    echo ""
    echo "To set up OCI configuration:"
    echo "  cp scripts/oci-env.sh.example scripts/oci-env.sh"
    echo "  # Edit scripts/oci-env.sh with your values"
    exit 1
fi
# ============================================================================
# END CONFIGURATION
# ============================================================================

# Validate configuration
if [ -z "$ORACLE_PASSWORD" ]; then
    echo "ERROR: ORACLE_PASSWORD is not set in oci-env.sh"
    exit 1
fi

if [ ! -d "$DYLD_LIBRARY_PATH" ]; then
    echo "ERROR: Oracle Instant Client not found at: $DYLD_LIBRARY_PATH"
    echo "Edit scripts/oci-env.sh and set DYLD_LIBRARY_PATH to your Instant Client location"
    exit 1
fi

if [ ! -d "$TNS_ADMIN" ]; then
    echo "ERROR: Wallet directory not found at: $TNS_ADMIN"
    echo "Extract your OCI wallet to the ./wallet directory"
    exit 1
fi

# Change to project root
cd "$(dirname "$0")/.."

echo "Starting TMI with OCI Autonomous Database..."
echo "  DYLD_LIBRARY_PATH: $DYLD_LIBRARY_PATH"
echo "  TNS_ADMIN: $TNS_ADMIN"
echo ""

# Build the server with Oracle support if needed
# Always rebuild to ensure Oracle tags are included
echo "Building server with Oracle support..."
go build -tags oracle -o bin/tmiserver ./cmd/server/

# Start Redis
make start-redis

# Clean logs before starting
make clean-logs

# Create logs directory
mkdir -p logs

# Start server directly (not via make, to preserve DYLD_LIBRARY_PATH)
echo "Starting server with OCI configuration..."
./bin/tmiserver --config=config-development-oci.yml > logs/tmi.log 2>&1 &
SERVER_PID=$!
echo "$SERVER_PID" > .server.pid
echo "Server started with PID: $SERVER_PID"

# Wait a moment and check if server is running
sleep 2
if kill -0 $SERVER_PID 2>/dev/null; then
    echo "✅ Server is running on port 8080"
else
    echo "❌ Server failed to start. Check logs/tmi.log for details"
    tail -20 logs/tmi.log
    exit 1
fi
