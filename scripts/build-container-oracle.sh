#!/bin/bash
#
# build-container-oracle.sh - Build and push TMI containers for OCI deployment
#
# This script builds TMI container images based on Oracle Linux 9 and pushes
# them to Oracle Cloud Infrastructure (OCI) Container Registry.
#
# Supported components:
#   - server: TMI server with Oracle ADB support (Oracle Instant Client)
#   - redis:  Redis cache server on Oracle Linux
#
# Prerequisites:
#   - OCI CLI installed and configured (oci session authenticate or API key)
#   - Docker installed and running
#   - Access to the target OCI Container Repository
#
# Usage:
#   ./scripts/build-container-oracle.sh [options]
#
# Options:
#   --component COMP      Component to build: server, redis, or all (default: server)
#   --region REGION       OCI region (default: us-ashburn-1, from REGION env var)
#   --repo-ocid OCID      Container repository OCID (required, or set CONTAINER_REPO_OCID env var)
#   --tag TAG             Image tag (default: latest)
#   --version VERSION     Version string for image (default: from .version file)
#   --push                Push to OCI Container Registry after build
#   --no-cache            Build without Docker cache
#   --scan                Run security scan after build
#   --help                Show this help message
#
# Environment Variables:
#   CONTAINER_REPO_OCID   Container repository OCID (alternative to --repo-ocid)
#   OCI_REGION            OCI region (alternative to --region)
#   OCI_TENANCY_NAMESPACE Override tenancy namespace (auto-detected if not set)
#
# Example:
#   ./scripts/build-container-oracle.sh --component server --push
#   ./scripts/build-container-oracle.sh --component redis --push
#   ./scripts/build-container-oracle.sh --component all --push --scan
#

set -euo pipefail

# Script directory for relative paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions - all output to stderr to avoid polluting command substitution
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Default values
COMPONENT="server"
REGION="${OCI_REGION:-us-ashburn-1}"
REPO_OCID="${CONTAINER_REPO_OCID:-}"
TAG="latest"
VERSION=""
PUSH=false
NO_CACHE=false
SCAN=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --component)
            COMPONENT="$2"
            shift 2
            ;;
        --region)
            REGION="$2"
            shift 2
            ;;
        --repo-ocid)
            REPO_OCID="$2"
            shift 2
            ;;
        --tag)
            TAG="$2"
            shift 2
            ;;
        --version)
            VERSION="$2"
            shift 2
            ;;
        --push)
            PUSH=true
            shift
            ;;
        --no-cache)
            NO_CACHE=true
            shift
            ;;
        --scan)
            SCAN=true
            shift
            ;;
        --help)
            sed -n '2,/^$/p' "$0" | sed 's/^# //' | sed 's/^#//'
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Validate required parameters
if [[ -z "$REPO_OCID" ]]; then
    log_error "Container repository OCID is required. Use --repo-ocid or set CONTAINER_REPO_OCID env var"
    exit 1
fi

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    # Check Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed or not in PATH"
        exit 1
    fi

    if ! docker info &> /dev/null; then
        log_error "Docker daemon is not running"
        exit 1
    fi

    # Check OCI CLI
    if ! command -v oci &> /dev/null; then
        log_error "OCI CLI is not installed or not in PATH"
        exit 1
    fi

    # Verify OCI CLI is configured
    if ! oci iam region list --output json &> /dev/null; then
        log_error "OCI CLI is not configured. Run 'oci session authenticate' or configure API keys"
        exit 1
    fi

    log_success "All prerequisites met"
}

# Get tenancy namespace from OCI
get_tenancy_namespace() {
    if [[ -n "${OCI_TENANCY_NAMESPACE:-}" ]]; then
        echo "$OCI_TENANCY_NAMESPACE"
        return
    fi

    log_info "Fetching tenancy namespace from OCI..."
    local namespace
    namespace=$(oci os ns get --query 'data' --raw-output 2>/dev/null)

    if [[ -z "$namespace" ]]; then
        log_error "Failed to get tenancy namespace from OCI"
        exit 1
    fi

    echo "$namespace"
}

# Get repository name from OCID
get_repo_name() {
    log_info "Fetching repository details from OCI..."
    local repo_name
    repo_name=$(oci artifacts container repository get \
        --repository-id "$REPO_OCID" \
        --query 'data."display-name"' \
        --raw-output 2>/dev/null)

    if [[ -z "$repo_name" ]]; then
        log_error "Failed to get repository name from OCID: $REPO_OCID"
        exit 1
    fi

    echo "$repo_name"
}

# Get version from .version file if not specified
get_version() {
    if [[ -n "$VERSION" ]]; then
        echo "$VERSION"
        return
    fi

    local version_file="${PROJECT_ROOT}/.version"
    if [[ -f "$version_file" ]]; then
        local major minor patch
        major=$(jq -r '.major // 1' "$version_file")
        minor=$(jq -r '.minor // 0' "$version_file")
        patch=$(jq -r '.patch // 0' "$version_file")
        echo "${major}.${minor}.${patch}"
    else
        echo "1.0.0"
    fi
}

# Authenticate with OCI Container Registry
authenticate_ocir() {
    log_info "Authenticating with OCI Container Registry..."

    local registry="${REGION}.ocir.io"
    local namespace="$1"

    # Get auth token - use existing OCI session
    # For OCI CLI with session authentication, we need to generate an auth token
    # or use an existing one stored in the config

    # Try to use docker credential helper if available
    if docker-credential-oci-container-registry list &> /dev/null 2>&1; then
        log_info "Using OCI credential helper for Docker authentication"
        # The credential helper handles auth automatically
        return 0
    fi

    # Check if already logged in
    if docker login "${registry}" --get-login &> /dev/null 2>&1; then
        log_info "Already authenticated with ${registry}"
        return 0
    fi

    # For session-based auth, we need to get a temporary token
    # This requires the user to have an auth token configured
    log_warn "Docker login to OCI Container Registry required"
    log_info "To authenticate, you need an OCI Auth Token:"
    log_info "  1. Go to OCI Console > Identity > Users > Your User > Auth Tokens"
    log_info "  2. Generate a new token (save it, shown only once)"
    log_info "  3. Run: docker login ${registry}"
    log_info "     Username: ${namespace}/your-email@example.com"
    log_info "     Password: your-auth-token"
    log_info ""
    log_info "Attempting interactive login..."

    if ! docker login "${registry}"; then
        log_error "Failed to authenticate with OCI Container Registry"
        exit 1
    fi

    log_success "Authenticated with OCI Container Registry"
}

# Build the server container image
build_server_image() {
    local image_name="$1"
    local version="$2"

    log_info "Building TMI server container with Oracle ADB support..."
    log_info "Image: ${image_name}:${TAG}"
    log_info "Version: ${version}"

    cd "$PROJECT_ROOT"

    # Prepare build arguments
    # Note: --platform linux/amd64 is required because OCI Container Instances
    # use CI.Standard.E4.Flex shapes which are AMD64 (x86_64) architecture
    local build_args=(
        --platform linux/amd64
        --file Dockerfile.server-oracle
        --tag "${image_name}:${TAG}"
        --build-arg "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        --build-arg "GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
    )

    # Parse version components
    local major minor patch
    IFS='.' read -r major minor patch <<< "$version"
    build_args+=(
        --build-arg "VERSION_MAJOR=${major:-1}"
        --build-arg "VERSION_MINOR=${minor:-0}"
        --build-arg "VERSION_PATCH=${patch:-0}"
    )

    # Add version tag if different from 'latest'
    if [[ "$TAG" == "latest" ]]; then
        build_args+=(--tag "${image_name}:v${version}")
    fi

    if [[ "$NO_CACHE" == true ]]; then
        build_args+=(--no-cache)
    fi

    # Build the image
    log_info "Running docker build..."
    if ! docker build "${build_args[@]}" .; then
        log_error "Docker build failed"
        exit 1
    fi

    log_success "Server container image built successfully"

    # Show image size
    local image_size
    image_size=$(docker images "${image_name}:${TAG}" --format "{{.Size}}")
    log_info "Image size: ${image_size}"
}

# Build the Redis container image
build_redis_image() {
    local image_name="$1"
    local version="$2"

    log_info "Building Redis container on Oracle Linux..."
    log_info "Image: ${image_name}:${TAG}"

    cd "$PROJECT_ROOT"

    # Prepare build arguments
    # Note: --platform linux/amd64 is required because OCI Container Instances
    # use CI.Standard.E4.Flex shapes which are AMD64 (x86_64) architecture
    local build_args=(
        --platform linux/amd64
        --file Dockerfile.redis-oracle
        --tag "${image_name}:${TAG}"
        --build-arg "BUILD_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
        --build-arg "GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
        --build-arg "REDIS_VERSION=8.4.0"
    )

    # Add version tag if different from 'latest'
    if [[ "$TAG" == "latest" ]]; then
        build_args+=(--tag "${image_name}:v${version}")
    fi

    if [[ "$NO_CACHE" == true ]]; then
        build_args+=(--no-cache)
    fi

    # Build the image
    log_info "Running docker build..."
    if ! docker build "${build_args[@]}" .; then
        log_error "Docker build failed"
        exit 1
    fi

    log_success "Redis container image built successfully"

    # Show image size
    local image_size
    image_size=$(docker images "${image_name}:${TAG}" --format "{{.Size}}")
    log_info "Image size: ${image_size}"
}

# Run security scan
run_security_scan() {
    local image_name="$1"
    local component="$2"

    log_info "Running security scan on ${component} container image..."

    # Check for Docker Scout
    if docker scout version &> /dev/null 2>&1; then
        log_info "Using Docker Scout for security scanning..."

        # Create reports directory
        local reports_dir="${PROJECT_ROOT}/security-reports/oracle-container"
        mkdir -p "$reports_dir"

        # Run CVE scan
        docker scout cves "${image_name}:${TAG}" \
            --format sarif \
            --output "${reports_dir}/${component}-cve-report.sarif.json" 2>/dev/null || true

        # Show summary
        docker scout cves "${image_name}:${TAG}" --format summary 2>/dev/null || true

        log_success "Security scan complete. Report: ${reports_dir}/${component}-cve-report.sarif.json"
    else
        log_warn "Docker Scout not available. Skipping security scan."
        log_info "Install Docker Scout: curl -fsSL https://raw.githubusercontent.com/docker/scout-cli/main/install.sh | sh"
    fi

    # Generate SBOM if syft is available
    if command -v syft &> /dev/null; then
        log_info "Generating SBOM with Syft..."
        local sbom_dir="${PROJECT_ROOT}/security-reports/sbom"
        mkdir -p "$sbom_dir"

        syft "${image_name}:${TAG}" \
            -o cyclonedx-json="${sbom_dir}/tmi-${component}-oracle-sbom.json" 2>/dev/null || true

        log_success "SBOM generated: ${sbom_dir}/tmi-${component}-oracle-sbom.json"
    fi
}

# Push image to OCI Container Registry
push_image() {
    local image_name="$1"
    local version="$2"

    log_info "Pushing image to OCI Container Registry..."

    # Push with tag
    log_info "Pushing ${image_name}:${TAG}..."
    if ! docker push "${image_name}:${TAG}"; then
        log_error "Failed to push ${image_name}:${TAG}"
        exit 1
    fi

    # Push version tag if we created one
    if [[ "$TAG" == "latest" ]]; then
        log_info "Pushing ${image_name}:v${version}..."
        if ! docker push "${image_name}:v${version}"; then
            log_warn "Failed to push version tag (non-fatal)"
        fi
    fi

    log_success "Image pushed successfully to OCI Container Registry"
    log_info "Image URL: ${image_name}:${TAG}"
}

# Build a single component
build_component() {
    local component="$1"
    local base_image_name="$2"
    local version="$3"
    local namespace="$4"

    local image_name
    local image_suffix=""

    case "$component" in
        server)
            image_suffix=""
            ;;
        redis)
            image_suffix="-redis"
            ;;
        *)
            log_error "Unknown component: $component"
            exit 1
            ;;
    esac

    image_name="${base_image_name}${image_suffix}"
    log_info "Building component: ${component}"

    # Build the image
    case "$component" in
        server)
            build_server_image "$image_name" "$version"
            ;;
        redis)
            build_redis_image "$image_name" "$version"
            ;;
    esac

    # Run security scan if requested
    if [[ "$SCAN" == true ]]; then
        run_security_scan "$image_name" "$component"
    fi

    # Push if requested
    if [[ "$PUSH" == true ]]; then
        push_image "$image_name" "$version"
    fi

    # Print run instructions
    echo ""
    case "$component" in
        server)
            log_info "To run the server container locally:"
            echo "  docker run -p 8080:8080 \\"
            echo "    -v /path/to/wallet:/wallet:ro \\"
            echo "    -e TMI_DB_USER=your_user \\"
            echo "    -e TMI_DB_PASSWORD=your_password \\"
            echo "    -e TMI_ORACLE_CONNECT_STRING=your_tns_alias \\"
            echo "    ${image_name}:${TAG}"
            ;;
        redis)
            log_info "To run the Redis container locally:"
            echo "  docker run -p 6379:6379 \\"
            echo "    -e REDIS_PASSWORD=your_password \\"
            echo "    ${image_name}:${TAG}"
            ;;
    esac
}

# Main execution
main() {
    log_info "TMI Oracle Container Build Script"
    log_info "=================================="
    log_info "Component: ${COMPONENT}"

    # Validate component
    case "$COMPONENT" in
        server|redis|all)
            ;;
        *)
            log_error "Invalid component: $COMPONENT. Must be: server, redis, or all"
            exit 1
            ;;
    esac

    # Check prerequisites
    check_prerequisites

    # Get OCI configuration
    local namespace
    namespace=$(get_tenancy_namespace)
    log_info "Tenancy namespace: ${namespace}"

    local repo_name
    repo_name=$(get_repo_name)
    log_info "Repository name: ${repo_name}"

    # Construct base image name
    local registry="${REGION}.ocir.io"
    local base_image_name="${registry}/${namespace}/${repo_name}"
    log_info "Base image path: ${base_image_name}"

    # Get version
    local version
    version=$(get_version)
    log_info "Version: ${version}"

    # Authenticate if pushing
    if [[ "$PUSH" == true ]]; then
        authenticate_ocir "$namespace"
    fi

    # Build requested component(s)
    case "$COMPONENT" in
        server)
            build_component "server" "$base_image_name" "$version" "$namespace"
            ;;
        redis)
            build_component "redis" "$base_image_name" "$version" "$namespace"
            ;;
        all)
            build_component "server" "$base_image_name" "$version" "$namespace"
            echo ""
            log_info "----------------------------------------"
            echo ""
            build_component "redis" "$base_image_name" "$version" "$namespace"
            ;;
    esac

    if [[ "$PUSH" != true ]]; then
        echo ""
        log_info "Image(s) built but not pushed. Use --push to push to OCI Container Registry"
    fi

    log_success "Build complete!"
}

# Run main
main "$@"
