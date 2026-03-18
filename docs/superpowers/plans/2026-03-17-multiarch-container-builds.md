# Multi-Arch Container Image Build Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable building multi-architecture (amd64 + arm64) container images for TMI server and Redis, producing manifest lists that work on both x86_64 and Ampere/ARM nodes.

**Architecture:** Add a new build script (`scripts/build-containers-multiarch.sh`) that uses `docker buildx` to create multi-platform images. The existing single-arch build script is unchanged. New Makefile targets wrap the new script. Dockerfiles are unchanged — Chainguard base images already provide multi-arch variants, and the Go binary cross-compiles via `GOARCH`.

**Tech Stack:** Docker Buildx, Chainguard multi-arch base images, Go cross-compilation (`CGO_ENABLED=0`)

**Spec:** `docs/superpowers/specs/2026-03-17-terraform-public-private-templates-design.md` (Section 5)

---

### Task 1: Create the multi-arch build script

**Files:**
- Create: `scripts/build-containers-multiarch.sh`

- [ ] **Step 1: Create the script**

The script supports building TMI server, Redis, or both as multi-platform images. It uses `docker buildx` with `--platform linux/amd64,linux/arm64`. The `--push` flag is required for multi-platform builds (buildx can't `--load` multi-arch images to local Docker). A `--local` flag builds for the local platform only (useful for testing without a registry).

```bash
#!/bin/bash
# Multi-Architecture Container Build Script
# Builds amd64 + arm64 manifest lists using docker buildx
#
# Usage:
#   ./build-containers-multiarch.sh [server|redis|all] [--push] [--registry REGISTRY] [--tag TAG] [--local]
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
    exit 1
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
```

- [ ] **Step 2: Make the script executable**

Run: `chmod +x scripts/build-containers-multiarch.sh`

- [ ] **Step 3: Verify the script parses correctly**

Run: `bash -n scripts/build-containers-multiarch.sh`
Expected: No output (syntax OK)

- [ ] **Step 4: Verify help text works**

Run: `scripts/build-containers-multiarch.sh --help`
Expected: Usage message printed, exit 0

- [ ] **Step 5: Commit**

```bash
git add scripts/build-containers-multiarch.sh
git commit -m "feat: add multi-arch container build script for amd64+arm64"
```

---

### Task 2: Add Makefile targets

**Files:**
- Modify: `Makefile` (add targets in the container build section, near line 1098)

- [ ] **Step 1: Add multi-arch targets to Makefile**

Add these targets after the existing `build-containers` target (around line 1161):

```makefile
# Multi-architecture container builds (amd64 + arm64)
build-container-tmi-multiarch:
	@echo "Building TMI server multi-arch image..."
	@./scripts/build-containers-multiarch.sh server --push --registry $(REGISTRY_PREFIX)

build-container-redis-multiarch:
	@echo "Building Redis multi-arch image..."
	@./scripts/build-containers-multiarch.sh redis --push --registry $(REGISTRY_PREFIX)

build-containers-multiarch:
	@echo "Building all multi-arch images..."
	@./scripts/build-containers-multiarch.sh all --push --registry $(REGISTRY_PREFIX)

build-container-tmi-multiarch-local:
	@echo "Building TMI server for local platform..."
	@./scripts/build-containers-multiarch.sh server --local

build-container-redis-multiarch-local:
	@echo "Building Redis for local platform..."
	@./scripts/build-containers-multiarch.sh redis --local

build-containers-multiarch-local:
	@echo "Building all images for local platform..."
	@./scripts/build-containers-multiarch.sh all --local
```

Also add the new targets to the `.PHONY` declaration on line 1098 and to the help text in the `list-targets` section.

- [ ] **Step 2: Verify Makefile syntax**

Run: `make -n build-containers-multiarch-local`
Expected: Shows the command that would run (dry-run), no errors

- [ ] **Step 3: Test local build**

Run: `make build-container-tmi-multiarch-local`
Expected: Builds TMI server image for local platform using buildx. If Docker is not running, expect a clear error from Docker.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "feat: add multi-arch container build Make targets"
```

---

### Task 3: Verify Dockerfiles are multi-arch compatible

**Files:**
- Review (no changes expected): `Dockerfile.server`, `Dockerfile.redis`

- [ ] **Step 1: Verify Dockerfile.server is multi-arch safe**

Check that:
1. Base images (`cgr.dev/chainguard/go:latest`, `cgr.dev/chainguard/static:latest`) provide both amd64 and arm64 variants
2. The build uses `CGO_ENABLED=0` (required for cross-compilation)
3. No architecture-specific instructions (e.g., no hardcoded `GOARCH` or platform-specific paths)

Run: `docker manifest inspect cgr.dev/chainguard/go:latest 2>/dev/null | jq '.manifests[].platform.architecture' 2>/dev/null || echo "Cannot inspect manifest (may need docker login)"`

Expected: Shows `"amd64"` and `"arm64"` (or similar). If manifest inspect fails, this is OK — Chainguard images are documented as multi-arch.

Note: The existing `Dockerfile.server` hardcodes `GOOS=linux` but does NOT set `GOARCH`, which is correct — buildx sets `GOARCH` automatically via the `--platform` flag when using multi-stage builds with a Go builder image.

- [ ] **Step 2: Verify Dockerfile.redis is multi-arch safe**

The Redis Dockerfile uses `cgr.dev/chainguard/redis:latest` with no build stage — it's purely a base image with configuration. Multi-arch support depends entirely on the base image.

Run: `docker manifest inspect cgr.dev/chainguard/redis:latest 2>/dev/null | jq '.manifests[].platform.architecture' 2>/dev/null || echo "Cannot inspect manifest"`

Expected: Shows `"amd64"` and `"arm64"`.

- [ ] **Step 3: Document verification results and commit if any changes were needed**

If both Dockerfiles are multi-arch safe (expected), no changes needed. If any issues found, fix and commit.
