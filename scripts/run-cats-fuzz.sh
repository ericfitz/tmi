#!/bin/bash

# CATS Fuzzing Script with OAuth Integration
# Automates OAuth authentication and runs CATS fuzzing against TMI API

set -euo pipefail

# Ensure basic commands are available - prepend standard paths if not already there
if [[ ":$PATH:" != *":/usr/bin:"* ]]; then
    export PATH="/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin:$PATH"
fi

# Configuration
DEFAULT_USER="charlie"
DEFAULT_SERVER="http://localhost:8080"
OAUTH_STUB_PORT=8079
OAUTH_STUB_URL="http://localhost:${OAUTH_STUB_PORT}"
OPENAPI_SPEC="docs/reference/apis/tmi-openapi.json"
ERROR_KEYWORDS_FILE="cats-error-keywords.txt"
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
    echo "  -p, --path PATH      Restrict to specific endpoint path (e.g., /addons, /invocations)"
    echo "  -b, --blackbox       Ignore all error codes other than 500"
    echo "  -h, --help           Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                                    # Use defaults (charlie, localhost:8080)"
    echo "  $0 -u alice -s http://localhost:8080  # Custom user and server"
    echo "  $0 -p /addons                         # Only test /addons endpoints"
    echo "  $0 -p /invocations -u alice           # Only test /invocations with user alice"
    echo ""
    echo "Prerequisites:"
    echo "  - TMI server running (make start-dev)"
    echo "  - CATS tool installed"
}

log() {
    echo -e "${BLUE}[$(date '+%Y-%m-%d %H:%M:%S')]${NC} $1" >&2
}

error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
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
    PATH="$PATH" make -C "${PROJECT_ROOT}" stop-server || true

    # Clear log files
    log "Clearing log files..."
    rm -rf "${PROJECT_ROOT}/logs/*" || true
    mkdir -p "${PROJECT_ROOT}/logs" || true

    # Clear old cats report data and test data file
    log "Clearing old cats reports and test data..."
    rm -rf "${PROJECT_ROOT}/cats-report/*" || true
    mkdir -p "${PROJECT_ROOT}/cats-report" || true
    rm -f "${PROJECT_ROOT}/cats-test-data.json" || true

    # Start the server fresh
    log "Starting TMI server..."
    PATH="$PATH" make -C "${PROJECT_ROOT}" start-dev > /dev/null 2>&1 &

    # Wait for server to be ready
    log "Waiting for server to be ready..."
    for i in {1..30}; do
        if curl -s "${server}/" &> /dev/null; then
            success "TMI server is ready and logs are clean"

            # Clear ALL rate limit keys from Redis to avoid 429 errors during testing
            log "Clearing all rate limit keys from Redis..."
            docker exec tmi-redis redis-cli --scan --pattern "*ratelimit*" | xargs -r docker exec -i tmi-redis redis-cli DEL || true

            return 0
        fi
        sleep 1
    done

    error "Server failed to start within 30 seconds"
    exit 1
}

disable_rate_limits() {
    local user="$1"

    log "Disabling rate limits for CATS test user: ${user}..."

    # Clear any existing rate limit entries for this test user
    # IP-based rate limits (match the IP we'll be using - localhost)
    docker exec tmi-redis redis-cli --scan --pattern "ip:ratelimit:*:127.0.0.1" | xargs -r docker exec -i tmi-redis redis-cli DEL || true
    docker exec tmi-redis redis-cli --scan --pattern "ip:ratelimit:*:::1" | xargs -r docker exec -i tmi-redis redis-cli DEL || true

    # Auth flow rate limits (by user identifier)
    docker exec tmi-redis redis-cli --scan --pattern "auth:ratelimit:*:${user}*" | xargs -r docker exec -i tmi-redis redis-cli DEL || true

    # API rate limits (cleared above with general pattern)

    success "Rate limits disabled for user: ${user}"
}

restore_rate_limits() {
    log "Rate limits restored (no action needed - limits apply naturally to new requests)"
}

start_oauth_stub() {
    log "Starting OAuth callback stub..."
    
    # Check if already running
    if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
        log "OAuth stub already running"
        return 0
    fi
    
    # Start the stub
    PATH="$PATH" make -C "${PROJECT_ROOT}" start-oauth-stub
    
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

create_test_data() {
    local token="$1"
    local server="$2"
    local user="${3:-}"

    log "Creating test data for CATS fuzzing..."

    # Check if cats-create-test-data.sh exists
    local test_data_script="${PROJECT_ROOT}/scripts/cats-create-test-data.sh"
    if [[ ! -f "${test_data_script}" ]]; then
        error "Test data script not found: ${test_data_script}"
        return 1
    fi

    # Check if script is executable
    if [[ ! -x "${test_data_script}" ]]; then
        log "Making test data script executable..."
        chmod +x "${test_data_script}"
    fi

    # Run test data creation script
    if ! "${test_data_script}" --token="${token}" --server="${server}" --user="${user}"; then
        error "Failed to create test data"
        return 1
    fi

    # Verify reference file was created
    if [[ ! -f "${PROJECT_ROOT}/cats-test-data.json" ]]; then
        error "Test data reference file not found: ${PROJECT_ROOT}/cats-test-data.json"
        return 1
    fi

    success "Test data created successfully"
    return 0
}

run_cats_fuzz() {
    local token="$1"
    local server="$2"
    local path="${3:-}"
    local user="${4:-}"

    log "Running CATS fuzzing..."
    log "Server: ${server}"
    log "OpenAPI Spec: ${OPENAPI_SPEC}"
    log "Using CATS default error leak detection keywords"
    log "Skipping UUID format fields to avoid false positives with malformed UUIDs"
    log "Skipping 'offset' field - extreme values return empty results (200), not errors"

    # Export token as environment variable
    export TMI_ACCESS_TOKEN="${token}"

    # Disable rate limits before starting CATS
    if [[ -n "${user}" ]]; then
        disable_rate_limits "${user}"
    fi

    # Public endpoints that must be accessible without authentication per RFCs
    # These endpoints have security:[] in the OpenAPI spec but CATS doesn't
    # respect this marker for the BypassAuthentication fuzzer, causing false positives.
    # See: docs/developer/testing/cats-public-endpoints.md
    local public_paths=(
        "/"
        "/.well-known/jwks.json"
        "/.well-known/oauth-authorization-server"
        "/.well-known/oauth-protected-resource"
        "/.well-known/openid-configuration"
        "/oauth2/authorize"
        "/oauth2/callback"
        "/oauth2/introspect"
        "/oauth2/providers"
        "/oauth2/refresh"
        "/oauth2/token"
        "/saml/acs"
        "/saml/providers"
        "/saml/slo"
        "/saml/{provider}/login"
        "/saml/{provider}/metadata"
    )

    # Construct and run CATS command
    local cats_cmd=(
        "cats"
        "--contract=${PROJECT_ROOT}/${OPENAPI_SPEC}"
        "--server=${server}"
    )

    # Add blackbox flag if set
    if [[ -n "${blackbox}" ]]; then
        cats_cmd+=("${blackbox}")
    fi

    cats_cmd+=(
        "-H" "Authorization=Bearer ${token}"
        "-X=${HTTP_METHODS}"
        "--skipFieldFormat=uuid"
        "--skipField=offset"
        "--refData=${PROJECT_ROOT}/cats-test-data.json"
    )

    # Add path filter if specified
    if [[ -n "${path}" ]]; then
        log "Restricting to endpoint path: ${path}"
        cats_cmd+=("--paths=${path}")
    else
        # When testing all endpoints, skip BypassAuthentication fuzzer on public paths
        # to avoid false positives. These endpoints are intentionally public per RFCs:
        # - RFC 8414: OAuth 2.0 Authorization Server Metadata (.well-known/*)
        # - RFC 8693: OAuth 2.0 Token Exchange
        # - RFC 7517: JSON Web Key (JWK) (jwks.json)
        log "Skipping BypassAuthentication fuzzer on ${#public_paths[@]} public endpoints"
        local skip_paths_arg
        skip_paths_arg=$(IFS=','; echo "${public_paths[*]}")
        cats_cmd+=("--skipPaths=${skip_paths_arg}")
    fi

    log "Executing: ${cats_cmd[*]}"

    # Run CATS with the token
    "${cats_cmd[@]}"

    local cats_exit_code=$?

    # Restore rate limits after CATS completes
    restore_rate_limits

    return ${cats_exit_code}
}

main() {
    local user="${DEFAULT_USER}"
    local server="${DEFAULT_SERVER}"
    local path=""
    local blackbox=""

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
            -p|--path)
                path="$2"
                shift 2
                ;;
            -b|--blackbox)
                blackbox="-b"
                shift 1
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
    if [[ -n "${path}" ]]; then
        log "Path filter: ${path}"
    fi

    check_prerequisites
    restart_server_clean "${server}"
    start_oauth_stub

    local access_token
    access_token=$(authenticate_user "${user}" "${server}")

    # Create test data before running CATS
    create_test_data "${access_token}" "${server}" "${user}"

    run_cats_fuzz "${access_token}" "${server}" "${path}" "${user}"

    success "CATS fuzzing completed!"
}

# Only run main if script is executed directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi