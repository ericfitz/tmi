#!/bin/bash
# drop-oracle-tables.sh - Drop all tables in OCI Autonomous Database
#
# This script sets up the Oracle environment and runs the drop-oracle-tables.go utility.
#
# Prerequisites:
#   1. Oracle Instant Client installed
#   2. Wallet extracted to ./wallet directory
#   3. Database user created in OCI ADB
#
# Usage:
#   ./scripts/drop-oracle-tables.sh
#
# Configuration:
#   Edit scripts/oci-env.sh with your environment values
#   (Copy from scripts/oci-env.sh.example if needed)

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

# Derive ORACLE_CONNECT_STRING from config if not set
if [ -z "$ORACLE_CONNECT_STRING" ]; then
    OCI_CONFIG="config-development-oci.yml"
    if [ -f "$OCI_CONFIG" ]; then
        # Extract TNS alias from database URL (e.g., oracle://ADMIN@tmiadb_tp -> tmiadb_tp)
        TNS_ALIAS=$(grep -E '^\s+url:' "$OCI_CONFIG" | head -1 | sed -E 's/.*@([a-zA-Z0-9_]+).*/\1/')
        if [ -n "$TNS_ALIAS" ]; then
            # Replace service level suffix with _medium for admin operations
            DB_NAME=$(echo "$TNS_ALIAS" | sed -E 's/_(high|medium|low|tp|tpurgent)$//')
            export ORACLE_CONNECT_STRING="${DB_NAME}_medium"
        fi
    fi
fi

if [ -z "$ORACLE_CONNECT_STRING" ]; then
    echo "ERROR: ORACLE_CONNECT_STRING is not set and could not be derived from config"
    echo "Set ORACLE_CONNECT_STRING in scripts/oci-env.sh or ensure config-development-oci.yml exists"
    exit 1
fi

echo "Dropping all tables in OCI Autonomous Database..."
echo "  DYLD_LIBRARY_PATH: $DYLD_LIBRARY_PATH"
echo "  TNS_ADMIN: $TNS_ADMIN"
echo "  ORACLE_CONNECT_STRING: $ORACLE_CONNECT_STRING"
echo ""

# Build the utility first (go run spawns a subprocess which loses DYLD_LIBRARY_PATH on macOS)
echo "Building drop-oracle-tables utility..."
go build -o bin/drop-oracle-tables scripts/drop-oracle-tables.go

# Run the drop tables utility
./bin/drop-oracle-tables
