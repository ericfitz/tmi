#!/bin/bash

# CATS Fuzzing Script with OAuth Integration
# Automates OAuth authentication and runs CATS fuzzing against TMI API

set -euo pipefail

# Configuration
DEFAULT_USER="charlie"
DEFAULT_SERVER="http://localhost:8080"
OAUTH_STUB_PORT=8079
OAUTH_STUB_URL="http://localhost:${OAUTH_STUB_PORT}"
OPENAPI_SPEC="docs/reference/apis/tmi-openapi.json"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HTTP_METHODS="POST,PUT,GET,DELETE,PATCH"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -u, --user USER      OAuth user login hint (default: ${DEFAULT_USER})"
    echo "  -s, --server URL     TMI server URL (default: ${DEFAULT_SERVER})"
    echo "  -h, --help           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                                    # Use defaults (charlie, localhost:8080)"
    echo "  $0 -u alice -s http://localhost:8080  # Custom user and server"
    echo ""
    echo "Prerequisites:"
    echo "  - TMI server running (make start-dev)"
    echo "  - CATS tool installed"
}

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

cleanup() {
    log "Cleaning up..."
    if pgrep -f "oauth-client-callback-stub" > /dev/null; then
        log "Stopping OAuth stub..."
        make -C "${PROJECT_ROOT}" stop-oauth-stub || true
    fi
}

check_prerequisites() {
    log "Checking prerequisites..."
    
    # Check if we're in the right directory
    if [[ ! -f "${PROJECT_ROOT}/${OPENAPI_SPEC}" ]]; then
        error "OpenAPI spec not found at ${PROJECT_ROOT}/${OPENAPI_SPEC}"
        error "Please run this script from the TMI project root or ensure the OpenAPI spec exists"
        exit 1
    fi
    
    # Check if CATS is installed
    if ! command -v cats &> /dev/null; then
        error "CATS tool not found. Please install it first."
        error "See: https://github.com/Endava/cats"
        error "On MacOS with Homebrew, use 'brew install cats'."
        exit 1
    fi
    
    # Check if TMI server is running
    if ! curl -s "${DEFAULT_SERVER}/" &> /dev/null; then
        warn "TMI server might not be running at ${DEFAULT_SERVER}"
        warn "Consider running 'make start-dev' first"
    fi
    
    success "Prerequisites check completed"
}

restart_server_clean() {
    local server="$1"
    
    log "Restarting server with clean logs for CATS fuzzing..."
    
    # Stop the server if it's running
    log "Stopping TMI server..."
    make -C "${PROJECT_ROOT}" stop-server || true
    
    # Clear log files
    log "Clearing log files..."
    rm -f "${PROJECT_ROOT}/server.log" "${PROJECT_ROOT}/logs/server.log" "${PROJECT_ROOT}/integration-test.log" || true
    mkdir -p "${PROJECT_ROOT}/logs" || true
    
    # Start the server fresh
    log "Starting TMI server..."
    make -C "${PROJECT_ROOT}" start-dev > /dev/null 2>&1 &
    
    # Wait for server to be ready
    log "Waiting for server to be ready..."
    for i in {1..30}; do
        if curl -s "${server}/" &> /dev/null; then
            success "TMI server is ready and logs are clean"
            return 0
        fi
        sleep 1
    done
    
    error "Server failed to start within 30 seconds"
    exit 1
}

start_oauth_stub() {
    log "Starting OAuth callback stub..."
    
    # Check if already running
    if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
        log "OAuth stub already running"
        return 0
    fi
    
    # Start the stub
    make -C "${PROJECT_ROOT}" start-oauth-stub
    
    # Wait for it to be ready
    for i in {1..10}; do
        if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
            success "OAuth stub is ready"
            return 0
        fi
        log "Waiting for OAuth stub to start... (attempt $i/10)"
        sleep 1
    done
    
    error "OAuth stub failed to start"
    exit 1
}

authenticate_user() {
    local user="$1"
    local server="$2"

    log "Authenticating user: ${user}"

    # Use the new /flows/start endpoint for automated e2e OAuth flow
    local start_flow_url="${OAUTH_STUB_URL}/flows/start"
    local flow_request=$(cat <<EOF
{
    "userid": "${user}",
    "idp": "test",
    "tmi_server": "${server}"
}
EOF
)

    log "Starting automated OAuth flow via ${start_flow_url}"

    # Start the OAuth flow
    local start_response
    if ! start_response=$(curl -s -X POST "${start_flow_url}" \
        -H "Content-Type: application/json" \
        -d "${flow_request}"); then
        error "Failed to start OAuth flow"
        exit 1
    fi

    # Extract flow_id from response
    local flow_id
    if ! flow_id=$(echo "${start_response}" | jq -r '.flow_id // empty'); then
        error "Failed to parse flow_id from response"
        error "Response: ${start_response}"
        exit 1
    fi

    if [[ -z "${flow_id}" || "${flow_id}" == "null" ]]; then
        error "No flow_id found in response"
        error "Response: ${start_response}"
        exit 1
    fi

    log "Flow started with ID: ${flow_id}"

    # Poll for flow completion
    local poll_url="${OAUTH_STUB_URL}/flows/${flow_id}"
    local max_attempts=10
    local attempt=0
    local poll_response

    while [ $attempt -lt $max_attempts ]; do
        attempt=$((attempt + 1))
        log "Polling flow status (attempt ${attempt}/${max_attempts})..."

        if ! poll_response=$(curl -s "${poll_url}"); then
            error "Failed to poll flow status"
            exit 1
        fi

        # Check flow status
        local status
        status=$(echo "${poll_response}" | jq -r '.status // empty')

        if [[ "${status}" == "completed" ]]; then
            log "Flow completed successfully"
            break
        elif [[ "${status}" == "error" ]]; then
            local flow_error
            flow_error=$(echo "${poll_response}" | jq -r '.error // "Unknown error"')
            error "Flow failed: ${flow_error}"
            exit 1
        fi

        # Wait before next poll
        sleep 2
    done

    if [ $attempt -eq $max_attempts ]; then
        error "Flow did not complete within ${max_attempts} attempts"
        error "Last status: $(echo "${poll_response}" | jq -r '.status')"
        exit 1
    fi

    # Extract access token from completed flow
    local access_token
    if ! access_token=$(echo "${poll_response}" | jq -r '.tokens.access_token // empty'); then
        error "Failed to extract access token from flow response"
        error "Response: ${poll_response}"
        exit 1
    fi

    if [[ -z "${access_token}" || "${access_token}" == "null" ]]; then
        error "No access token found in flow response"
        error "Response: ${poll_response}"
        exit 1
    fi

    success "Authentication successful for user: ${user}"
    echo "${access_token}"
}

run_cats_fuzz() {
    local token="$1"
    local server="$2"
    
    log "Running CATS fuzzing..."
    log "Server: ${server}"
    log "OpenAPI Spec: ${OPENAPI_SPEC}"
    
    # Export token as environment variable
    export TMI_ACCESS_TOKEN="${token}"
    
    # Construct and run CATS command
    local cats_cmd=(
        "cats"
        "--contract=${PROJECT_ROOT}/${OPENAPI_SPEC}"
        "--server=${server}"
        "-H" "Authorization=Bearer ${token}"
        "-X=${HTTP_METHODS}" 
    )
    
    log "Executing: ${cats_cmd[*]}"
    
    # Run CATS with the token
    "${cats_cmd[@]}"
}

main() {
    local user="${DEFAULT_USER}"
    local server="${DEFAULT_SERVER}"
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -u|--user)
                user="$2"
                shift 2
                ;;
            -s|--server)
                server="$2"
                shift 2
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
    
    # Set up cleanup trap
    trap cleanup EXIT
    
    log "Starting CATS fuzzing with OAuth integration"
    log "User: ${user}"
    log "Server: ${server}"
    
    check_prerequisites
    restart_server_clean "${server}"
    start_oauth_stub
    
    local access_token
    access_token=$(authenticate_user "${user}" "${server}")
    
    run_cats_fuzz "${access_token}" "${server}"
    
    success "CATS fuzzing completed!"
}

# Only run main if script is executed directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi