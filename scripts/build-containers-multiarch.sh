#!/bin/bash
# Multi-Architecture Container Build Script
# Builds amd64 + arm64 manifest lists using docker buildx
#
# Usage:
#   ./build-containers-multiarch.sh [server|redis|all] [OPTIONS]
#
# Examples:
#   ./build-containers-multiarch.sh all --push --registry ghcr.io/ericfitz/tmi
#   ./build-containers-multiarch.sh server --push --registry 123456789.dkr.ecr.us-east-1.amazonaws.com
#   ./build-containers-multiarch.sh server --local  # Build for local platform only

set -e
set -o pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 'development')"

# Defaults
PLATFORMS="linux/amd64,linux/arm64"
REGISTRY="${REGISTRY_PREFIX:-tmi}"
TAG="latest"
PUSH=false
LOCAL=false
COMPONENT="all"
BUILDER_NAME="tmi-multiarch"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

usage() {
    echo "Usage: $0 [server|redis|all] [OPTIONS]"
    echo ""
    echo "Components:"
    echo "  server    Build TMI server image only"
    echo "  redis     Build Redis image only"
    echo "  all       Build all images (default)"
    echo ""
    echo "Options:"
    echo "  --push              Push images to registry (required for multi-arch)"
    echo "  --registry REGISTRY Container registry prefix (default: tmi)"
    echo "  --tag TAG           Image tag (default: latest)"
    echo "  --local             Build for local platform only (no --push needed)"
    echo ""
    echo "Examples:"
    echo "  $0 all --push --registry ghcr.io/ericfitz/tmi"
    echo "  $0 server --local"
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        server|redis|all) COMPONENT="$1"; shift ;;
        --push) PUSH=true; shift ;;
        --registry) REGISTRY="$2"; shift 2 ;;
        --tag) TAG="$2"; shift 2 ;;
        --local) LOCAL=true; shift ;;
        --help|-h) usage ;;
        *) log_error "Unknown argument: $1"; usage ;;
    esac
done

# Validate: multi-arch requires --push or --local
if [ "$LOCAL" = false ] && [ "$PUSH" = false ]; then
    log_error "Multi-arch builds require --push (to push to a registry) or --local (to build for local platform only)"
    log_info "Docker buildx cannot load multi-platform images into local Docker daemon"
    exit 1
fi

# If --local, override platforms to current architecture only
if [ "$LOCAL" = true ]; then
    PLATFORMS="linux/$(go env GOARCH 2>/dev/null || uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')"
    log_info "Local build: targeting ${PLATFORMS} only"
fi

# Ensure buildx builder exists
ensure_builder() {
    if ! docker buildx inspect "${BUILDER_NAME}" >/dev/null 2>&1; then
        log_info "Creating buildx builder: ${BUILDER_NAME}"
        docker buildx create --name "${BUILDER_NAME}" --use --bootstrap
    else
        docker buildx use "${BUILDER_NAME}"
    fi
    log_success "Using buildx builder: ${BUILDER_NAME}"
}

# Build TMI server multi-arch
build_server() {
    log_info "Building TMI server for platforms: ${PLATFORMS}"

    local build_args=(
        --platform "${PLATFORMS}"
        -f "${PROJECT_ROOT}/Dockerfile.server"
        -t "${REGISTRY}/tmi-server:${TAG}"
        -t "${REGISTRY}/tmi-server:${GIT_COMMIT}"
        --build-arg "BUILD_DATE=${BUILD_DATE}"
        --build-arg "GIT_COMMIT=${GIT_COMMIT}"
    )

    if [ "$PUSH" = true ]; then
        build_args+=(--push)
    elif [ "$LOCAL" = true ]; then
        build_args+=(--load)
    fi

    docker buildx build "${build_args[@]}" "${PROJECT_ROOT}"

    log_success "TMI server image built: ${REGISTRY}/tmi-server:${TAG}"
}

# Build Redis multi-arch
build_redis() {
    log_info "Building Redis for platforms: ${PLATFORMS}"

    local build_args=(
        --platform "${PLATFORMS}"
        -f "${PROJECT_ROOT}/Dockerfile.redis"
        -t "${REGISTRY}/tmi-redis:${TAG}"
        -t "${REGISTRY}/tmi-redis:${GIT_COMMIT}"
        --build-arg "BUILD_DATE=${BUILD_DATE}"
        --build-arg "GIT_COMMIT=${GIT_COMMIT}"
    )

    if [ "$PUSH" = true ]; then
        build_args+=(--push)
    elif [ "$LOCAL" = true ]; then
        build_args+=(--load)
    fi

    docker buildx build "${build_args[@]}" "${PROJECT_ROOT}"

    log_success "Redis image built: ${REGISTRY}/tmi-redis:${TAG}"
}

# Main
main() {
    echo "--- TMI Multi-Arch Container Build ---"
    echo "Platforms:  ${PLATFORMS}"
    echo "Registry:   ${REGISTRY}"
    echo "Tag:        ${TAG}"
    echo "Git Commit: ${GIT_COMMIT}"
    echo "Push:       ${PUSH}"
    echo ""

    ensure_builder

    case "${COMPONENT}" in
        server) build_server ;;
        redis)  build_redis ;;
        all)    build_server; build_redis ;;
    esac

    log_success "Multi-arch build complete!"
}

main
