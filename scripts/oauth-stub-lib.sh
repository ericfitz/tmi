#!/bin/bash
# oauth-stub-lib.sh - Shared OAuth stub lifecycle management
#
# Source this file from any test script that needs the OAuth stub.
# It provides:
#   ensure_oauth_stub  - Start the stub if not already running (uses make targets)
#   cleanup_oauth_stub - No-op (stub is left running across test runs)
#
# Usage:
#   PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
#   source "${PROJECT_ROOT}/scripts/oauth-stub-lib.sh"
#   ensure_oauth_stub
#
# Requires PROJECT_ROOT to be set before sourcing.

ensure_oauth_stub() {
    if [ -z "${PROJECT_ROOT:-}" ]; then
        echo "[ERROR] PROJECT_ROOT must be set before calling ensure_oauth_stub"
        return 1
    fi

    # Check if stub is already running and responding
    if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
        echo "[INFO] OAuth stub already running"
        return 0
    fi

    # Not running — start it via make target
    echo "[INFO] OAuth stub not running, starting it..."
    if make -C "${PROJECT_ROOT}" start-oauth-stub; then
        # Verify it's responding
        for i in 1 2 3 4 5; do
            if curl -s http://127.0.0.1:8079/latest >/dev/null 2>&1; then
                echo "[INFO] OAuth stub started successfully"
                return 0
            fi
            sleep 1
        done
        echo "[ERROR] OAuth stub started but not responding after 5 seconds"
        return 1
    else
        echo "[ERROR] Failed to start OAuth stub"
        return 1
    fi
}

cleanup_oauth_stub() {
    # No-op: OAuth stub is intentionally left running across test runs.
    # Use 'make stop-oauth-stub' to stop it manually if needed.
    :
}
