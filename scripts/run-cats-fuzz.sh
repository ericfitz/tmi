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
    
    # Initiate OAuth flow with proper parameters
    local auth_url="${server}/oauth2/authorize?idp=test&login_hint=${user}&client_callback=${OAUTH_STUB_URL}/&scope=openid"
    log "Initiating OAuth flow: ${auth_url}"
    
    # Follow redirects to complete the OAuth flow
    local response
    if ! response=$(curl -s -L "${auth_url}"); then
        error "Failed to initiate OAuth flow"
        exit 1
    fi
    
    log "OAuth flow initiated, response received"
    
    # Wait for the OAuth callback to be processed
    log "Waiting for OAuth callback to be processed..."
    sleep 2
    
    # Retrieve credentials with retry
    local creds_url="${OAUTH_STUB_URL}/creds?userid=${user}"
    log "Retrieving credentials from: ${creds_url}"
    
    local response
    local retry_count=0
    local max_retries=3
    
    while [ $retry_count -lt $max_retries ]; do
        if response=$(curl -s "${creds_url}"); then
            # Check if we got actual credentials (not an error response)
            if echo "${response}" | grep -q '"access_token"'; then
                break
            elif echo "${response}" | grep -q '"error"'; then
                log "No credentials yet, retrying... (attempt $((retry_count + 1))/${max_retries})"
                sleep 2
                retry_count=$((retry_count + 1))
            else
                error "Unexpected response format"
                error "Response: ${response}"
                exit 1
            fi
        else
            error "Failed to retrieve credentials"
            exit 1
        fi
    done
    
    if [ $retry_count -eq $max_retries ]; then
        error "Failed to retrieve credentials after ${max_retries} attempts"
        error "Last response: ${response}"
        exit 1
    fi
    
    # Extract access token
    local access_token
    if ! access_token=$(echo "${response}" | jq -r '.access_token // empty'); then
        error "Failed to parse credentials response"
        exit 1
    fi
    
    if [[ -z "${access_token}" || "${access_token}" == "null" ]]; then
        error "No access token found in response"
        error "Response: ${response}"
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