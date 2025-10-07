#!/usr/bin/env bash
# Build Promtail container with Chainguard static base
# This script builds a local promtail container using the latest release

set -e

# Colors for output
BLUE='\033[0;34m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

# Configuration
CONTAINER_NAME="promtail"
IMAGE_NAME="tmi/promtail:latest"
DOCKERFILE="Dockerfile.promtail"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Dockerfile exists
if [ ! -f "$DOCKERFILE" ]; then
    log_error "Dockerfile not found: $DOCKERFILE"
    exit 1
fi

# Check if config exists
if [ ! -f "promtail/config.yaml" ]; then
    log_error "Promtail config not found: promtail/config.yaml"
    exit 1
fi

log_info "Building Promtail container..."

# Stop and remove existing container if running
if docker ps -a --format "{{.Names}}" | grep -q "^${CONTAINER_NAME}$"; then
    log_warning "Removing existing promtail container..."
    docker stop "$CONTAINER_NAME" 2>/dev/null || true
    docker rm "$CONTAINER_NAME" 2>/dev/null || true
fi

# Build the container
log_info "Building image: $IMAGE_NAME"
docker build -f "$DOCKERFILE" -t "$IMAGE_NAME" .

if [ $? -eq 0 ]; then
    log_success "Promtail container built successfully: $IMAGE_NAME"

    # Display image information
    log_info "Image details:"
    docker images "$IMAGE_NAME" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}\t{{.CreatedAt}}"

    log_info "To start the container, run:"
    echo "  make start-promtail"
    echo ""
    log_info "This will mount:"
    echo "  - ./logs/ -> /logs/ (development logs: tmi.log, server.log)"
    echo "  - /var/log/tmi/ -> /var/log/tmi/ (production logs)"
else
    log_error "Failed to build Promtail container"
    exit 1
fi
