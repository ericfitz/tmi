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
OPENAPI_SPEC="api-schema/tmi-openapi.json"
ERROR_KEYWORDS_FILE="cats-error-keywords.txt"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
HTTP_METHODS="POST,PUT,GET,DELETE,PATCH"
# Rate limit to prevent overwhelming slower backends (e.g., Oracle ADB)
# 3000 requests/minute = 50 requests/second - still fast but sustainable
DEFAULT_MAX_REQUESTS_PER_MINUTE=3000

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
    echo "  -r, --rate LIMIT     Max requests per minute (default: ${DEFAULT_MAX_REQUESTS_PER_MINUTE})"
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
    echo "  - TMI server must already be running (make start-dev or make start-dev-oci)"
    echo "  - Database must be clean and ready (make clean-everything before make start-dev)"
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

    # Check if TMI server is running - REQUIRED
    if ! curl -s "${DEFAULT_SERVER}/" &> /dev/null; then
        error "TMI server is not running at ${DEFAULT_SERVER}"
        error "Please start the server first with 'make start-dev' or 'make start-dev-oci'"
        error "For a clean test run: 'make clean-everything && make start-dev'"
        exit 1
    fi

    success "Prerequisites check completed"
}

prepare_test_environment() {
    log "Preparing test environment..."

    # Clear old cats report data (but not test data files created by cats-seed)
    log "Clearing old cats reports..."
    rm -rf "${PROJECT_ROOT}/test/outputs/cats/report/*" || true
    mkdir -p "${PROJECT_ROOT}/test/outputs/cats/report" || true

    # Clear ALL rate limit keys from Redis to avoid 429 errors during testing
    log "Clearing all rate limit keys from Redis..."
    docker exec tmi-redis redis-cli --scan --pattern "*ratelimit*" | xargs -r docker exec -i tmi-redis redis-cli DEL || true

    success "Test environment prepared"
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
    log "Checking OAuth callback stub status..."

    # Check if already running via HTTP request
    if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
        success "OAuth stub already running at ${OAUTH_STUB_URL}"
        return 0
    fi

    # Check if process exists but not responding yet
    if pgrep -f "oauth-client-callback-stub.py" > /dev/null; then
        log "OAuth stub process found, waiting for HTTP response..."
        for i in {1..5}; do
            if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
                success "OAuth stub is ready"
                return 0
            fi
            sleep 1
        done
        warn "OAuth stub process running but not responding, restarting..."
        PATH="$PATH" make -C "${PROJECT_ROOT}" stop-oauth-stub || true
        sleep 1
    fi

    # Start the stub
    log "Starting OAuth callback stub..."
    if ! PATH="$PATH" make -C "${PROJECT_ROOT}" start-oauth-stub; then
        error "Failed to start OAuth stub via make target"
        exit 1
    fi

    # Wait for it to be ready
    for i in {1..10}; do
        if curl -s "${OAUTH_STUB_URL}" &> /dev/null; then
            success "OAuth stub is ready at ${OAUTH_STUB_URL}"
            return 0
        fi
        log "Waiting for OAuth stub to start... (attempt $i/10)"
        sleep 1
    done

    error "OAuth stub failed to start within 10 seconds"
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
    "idp": "tmi",
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

        # Check if tokens are ready (status may be "authorization_completed" or "completed")
        local tokens_ready
        tokens_ready=$(echo "${poll_response}" | jq -r '.tokens_ready // false')

        if [[ "${tokens_ready}" == "true" ]]; then
            log "Flow completed successfully"
            break
        fi

        local status
        status=$(echo "${poll_response}" | jq -r '.status // empty')

        if [[ "${status}" == "error" || "${status}" == "failed" ]]; then
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
    local path="${3:-}"
    local user="${4:-}"
    local max_requests_per_minute="${5:-${DEFAULT_MAX_REQUESTS_PER_MINUTE}}"

    log "Running CATS fuzzing..."
    log "Server: ${server}"
    log "OpenAPI Spec: ${OPENAPI_SPEC}"
    log "Rate limit: ${max_requests_per_minute} requests/minute"
    log "Using CATS default error leak detection keywords"
    log "Skipping UUID format fields to avoid false positives with malformed UUIDs"
    log "Skipping 'offset' field - extreme values return empty results (200), not errors"
    log "Skipping BypassAuthentication fuzzer on endpoints marked x-public-endpoint=true"

    # Export token as environment variable
    export TMI_ACCESS_TOKEN="${token}"

    # Disable rate limits before starting CATS
    if [[ -n "${user}" ]]; then
        disable_rate_limits "${user}"
    fi

    # Construct and run CATS command
    local cats_cmd=(
        "cats"
        "--contract=${PROJECT_ROOT}/${OPENAPI_SPEC}"
        "--server=${server}"
        "--maxRequestsPerMinute=${max_requests_per_minute}"
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
        "--printExecutionStatistics"
        "--refData=${PROJECT_ROOT}/test/outputs/cats/cats-test-data.yml"
        "--output=${PROJECT_ROOT}/test/outputs/cats/report"
        # Skip BypassAuthentication fuzzer on public endpoints marked in OpenAPI spec
        # Public endpoints (OAuth, OIDC, SAML) are marked with x-public-endpoint: true
        # per RFCs 8414, 7517, 6749, and SAML 2.0 specifications
        # See: docs/migrated/developer/testing/cats-public-endpoints.md
        "--skipFuzzersForExtension=x-public-endpoint=true:BypassAuthentication"
        # Skip CheckSecurityHeaders fuzzer on cacheable discovery endpoints
        # Discovery endpoints (OIDC, OAuth metadata, JWKS, provider lists) intentionally use
        # Cache-Control: public, max-age=3600 instead of no-store per RFC 8414/7517
        # CATS expects no-store on all endpoints, but caching discovery metadata is correct
        # See: docs/migrated/developer/testing/cats-public-endpoints.md#cacheable-endpoints
        "--skipFuzzersForExtension=x-cacheable-endpoint=true:CheckSecurityHeaders"
        # Skip CheckDeletedResourcesNotAvailable on /me - users can't delete themselves and expect 404
        "--skipFuzzersForExtension=x-skip-deleted-resource-check=true:CheckDeletedResourcesNotAvailable"
        # Skip InsecureDirectObjectReferences on /oauth2/revoke - accepting different client_ids is valid
        "--skipFuzzersForExtension=x-skip-idor-check=true:InsecureDirectObjectReferences"
        # Skip fuzzers that produce false positives due to valid API behavior:
        # - DuplicateHeaders: TMI ignores duplicate/unknown headers (valid per HTTP spec)
        # - LargeNumberOfRandomAlphanumericHeaders: TMI ignores extra headers (valid behavior)
        # - EnumCaseVariantFields: TMI uses case-sensitive enum validation (stricter is valid)
        # See: docs/developer/testing/cats-false-positives.md
        #
        # Additional fuzzers skipped due to 100% false positive rate with 0 real issues found:
        # - BidirectionalOverrideFields: Unicode BiDi override chars in JSON API don't cause security issues
        # - ResponseHeadersMatchContractHeaders: Flags missing optional headers as errors
        # - PrefixNumbersWithZeroFields: API correctly rejects invalid JSON numbers (leading zeros)
        # - ZalgoTextInFields: Exotic Unicode in JSON API correctly handled
        # - HangulFillerFields: Korean filler chars in JSON API correctly handled
        # - AbugidasInStringFields: Indic script chars in JSON API correctly handled
        # - FullwidthBracketsFields: CJK brackets in JSON API correctly handled
        # - ZeroWidthCharsInValuesFields: Zero-width chars in values correctly handled (not field names)
        #
        # Note: MassAssignmentFuzzer and InsertRandomValuesInBodyFuzzer were previously
        # skipped due to CATS 13.5.0 bugs, now fixed in CATS 13.6.0:
        # - https://github.com/Endava/cats/issues/191 (fixed)
        # - https://github.com/Endava/cats/issues/192 (fixed)
        # - https://github.com/Endava/cats/issues/193 (fixed)
        "--skipFuzzers=DuplicateHeaders,LargeNumberOfRandomAlphanumericHeaders,EnumCaseVariantFields,BidirectionalOverrideFields,ResponseHeadersMatchContractHeaders,PrefixNumbersWithZeroFields,ZalgoTextInFields,HangulFillerFields,AbugidasInStringFields,FullwidthBracketsFields,ZeroWidthCharsInValuesFields"
    )

    # Add path filter if specified
    if [[ -n "${path}" ]]; then
        log "Restricting to endpoint path: ${path}"
        cats_cmd+=("--paths=${path}")
    fi

    # Log the command with token redacted
    local cats_cmd_display="${cats_cmd[*]}"
    cats_cmd_display="${cats_cmd_display//${token}/[REDACTED]}"
    log "Executing: ${cats_cmd_display}"

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
    local max_requests_per_minute="${DEFAULT_MAX_REQUESTS_PER_MINUTE}"

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
            -r|--rate)
                max_requests_per_minute="$2"
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
    prepare_test_environment
    start_oauth_stub

    # Verify reference files exist (created by cats-seed via 'make cats-seed')
    if [[ ! -f "${PROJECT_ROOT}/test/outputs/cats/cats-test-data.json" ]]; then
        error "Test data reference file not found: ${PROJECT_ROOT}/test/outputs/cats/cats-test-data.json"
        error "Run 'make cats-seed' first to create test data"
        exit 1
    fi

    local access_token
    access_token=$(authenticate_user "${user}" "${server}")

    run_cats_fuzz "${access_token}" "${server}" "${path}" "${user}" "${max_requests_per_minute}"

    success "CATS fuzzing completed!"
}

# Only run main if script is executed directly (not sourced)
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    main "$@"
fi