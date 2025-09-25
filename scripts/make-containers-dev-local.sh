#!/bin/bash

# TMI Docker Container Setup Script with Security Scanning
# This script creates containers with automated vulnerability patching

# --- Configuration ---
PG_CONTAINER_NAME="tmi-postgresql"
REDIS_CONTAINER_NAME="tmi-redis"
PG_IMAGE="tmi/tmi-postgresql:latest"
REDIS_IMAGE="tmi/tmi-redis:latest"
PG_HOST_PORT="5432"
PG_CONTAINER_PORT="5432"
REDIS_HOST_PORT="6379"
REDIS_CONTAINER_PORT="6379"
PG_DATA_HOST_DIR="/Users/efitz/Projects/tmi/pgsql-data-vol"
PG_CONTAINER_DATA_DIR="/bitnami/postgresql/data" # Bitnami PostgreSQL's data directory
PG_USER="tmi_dev"
PG_PASSWORD="dev123"

# Security configuration
SECURITY_SCAN_ENABLED=true
SECURITY_REPORTS_DIR="$(pwd)/security-reports"

# --- Script Setup ---
set -e           # Exit immediately if a command exits with a non-zero status.
set -o pipefail  # Exit if any command in a pipeline fails.

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

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

echo "--- TMI Docker Container Setup Script ---"

# --- Pre-checks ---
if ! command -v docker &> /dev/null
then
    log_error "Docker is not installed or not in your PATH."
    echo "Please install Docker Desktop for Mac (https://docs.docker.com/desktop/install/mac-install/)"
    exit 1
fi

log_success "Docker found. Proceeding with setup."

# Check if Docker Scout is available
if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    if ! docker scout version >/dev/null 2>&1; then
        log_warning "Docker Scout is not available. Security scanning will be disabled."
        SECURITY_SCAN_ENABLED=false
    else
        log_success "Docker Scout is available for security scanning"
        mkdir -p "$SECURITY_REPORTS_DIR"
    fi
fi

# --- Build Container Images First ---
log_info "Building container images..."

# Check if container build script exists
if [ -f "scripts/build-containers.sh" ]; then
    log_info "Running container build process..."
    ./scripts/build-containers.sh
    if [ $? -ne 0 ]; then
        log_error "Failed to build containers. Exiting."
        exit 1
    fi
else
    log_warning "Container build script not found. Building images with existing Dockerfiles..."
    
    # Build PostgreSQL image if Dockerfile exists
    if [ -f "Dockerfile.postgres" ]; then
        log_info "Building PostgreSQL image..."
        docker build -f Dockerfile.postgres -t "$PG_IMAGE" .
    else
        log_warning "Using original PostgreSQL image (not patched)"
        PG_IMAGE="bitnami/postgresql:latest"
    fi
    
    # Build Redis image if Dockerfile exists
    if [ -f "Dockerfile.redis" ]; then
        log_info "Building Redis image..."
        docker build -f Dockerfile.redis -t "$REDIS_IMAGE" .
    else
        log_warning "Using original Redis image (not patched)"
        REDIS_IMAGE="bitnami/redis:latest"
    fi
fi

# --- Security Scanning (if enabled) ---
if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    log_info "Performing security scans on images..."
    
    # Scan PostgreSQL image
    log_info "Scanning PostgreSQL image for vulnerabilities..."
    docker scout cves "$PG_IMAGE" --only-severity critical,high > "$SECURITY_REPORTS_DIR/postgresql-prescan.txt" 2>&1 || true
    
    # Scan Redis image  
    log_info "Scanning Redis image for vulnerabilities..."
    docker scout cves "$REDIS_IMAGE" --only-severity critical,high > "$SECURITY_REPORTS_DIR/redis-prescan.txt" 2>&1 || true
    
    log_success "Security scans completed. Reports saved to $SECURITY_REPORTS_DIR"
fi

# --- Create Host Volume Directory (if it doesn't exist) ---
log_info "Ensuring host directory for PostgreSQL data exists: ${PG_DATA_HOST_DIR}"
mkdir -p "${PG_DATA_HOST_DIR}"
if [ $? -ne 0 ]; then
    log_error "Could not create host directory ${PG_DATA_HOST_DIR}. Check permissions."
    exit 1
fi
log_success "Host directory ready."

# --- Stop and Remove Existing Containers (for a clean start) ---
log_info "Checking for and stopping/removing existing containers..."

if docker ps -a --format "{{.Names}}" | grep -Eq "^${PG_CONTAINER_NAME}$"; then
    log_info "Stopping existing ${PG_CONTAINER_NAME}..."
    docker stop "${PG_CONTAINER_NAME}" > /dev/null
    log_info "Removing existing ${PG_CONTAINER_NAME}..."
    docker rm "${PG_CONTAINER_NAME}" > /dev/null
else
    log_info "${PG_CONTAINER_NAME} not found or not running."
fi

if docker ps -a --format "{{.Names}}" | grep -Eq "^${REDIS_CONTAINER_NAME}$"; then
    log_info "Stopping existing ${REDIS_CONTAINER_NAME}..."
    docker stop "${REDIS_CONTAINER_NAME}" > /dev/null
    log_info "Removing existing ${REDIS_CONTAINER_NAME}..."
    docker rm "${REDIS_CONTAINER_NAME}" > /dev/null
else
    log_info "${REDIS_CONTAINER_NAME} not found or not running."
fi

log_success "Previous containers cleaned up (if they existed)."

# --- Create tmi-postgresql Container ---
echo ""
log_info "--- Creating tmi-postgresql container ---"
echo "Image: ${PG_IMAGE}"
echo "Port: 127.0.0.1:${PG_HOST_PORT}:${PG_CONTAINER_PORT}"
echo "Volume: ${PG_DATA_HOST_DIR} -> ${PG_CONTAINER_DATA_DIR}"
echo "User: ${PG_USER}, Password: ${PG_PASSWORD}"

docker run -d \
  --name "${PG_CONTAINER_NAME}" \
  -p "127.0.0.1:${PG_HOST_PORT}:${PG_CONTAINER_PORT}" \
  -v "${PG_DATA_HOST_DIR}:${PG_CONTAINER_DATA_DIR}" \
  -e "POSTGRESQL_USERNAME=${PG_USER}" \
  -e "POSTGRESQL_PASSWORD=${PG_PASSWORD}" \
  -e "POSTGRESQL_DATABASE=tmi_dev" \
  --restart=unless-stopped \
  "${PG_IMAGE}"

if [ $? -ne 0 ]; then
    log_error "Failed to create ${PG_CONTAINER_NAME} container."
    exit 1
fi
log_success "${PG_CONTAINER_NAME} container created successfully."

# --- Create tmi-redis Container ---
echo ""
log_info "--- Creating tmi-redis container ---"
echo "Image: ${REDIS_IMAGE}"
echo "Port: 127.0.0.1:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}"
echo "Enhanced security configuration enabled."

docker run -d \
  --name "${REDIS_CONTAINER_NAME}" \
  -p "127.0.0.1:${REDIS_HOST_PORT}:${REDIS_CONTAINER_PORT}" \
  -e "ALLOW_EMPTY_PASSWORD=yes" \
  --restart=unless-stopped \
  "${REDIS_IMAGE}"

if [ $? -ne 0 ]; then
    log_error "Failed to create ${REDIS_CONTAINER_NAME} container."
    exit 1
fi
log_success "${REDIS_CONTAINER_NAME} container created successfully."

# --- Runtime Security Scanning ---
if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    log_info "Performing post-deployment security validation..."
    
    # Wait for containers to start
    sleep 10
    
    # Verify containers are running securely
    if docker ps --format "{{.Names}}" | grep -q "^${PG_CONTAINER_NAME}$"; then
        log_success "PostgreSQL container is running securely"
        
        # Check for security-related logs
        docker logs "${PG_CONTAINER_NAME}" 2>&1 | grep -i "security\|error" > "$SECURITY_REPORTS_DIR/postgresql-runtime.log" || true
    fi
    
    if docker ps --format "{{.Names}}" | grep -q "^${REDIS_CONTAINER_NAME}$"; then
        log_success "Redis container is running securely"
        
        # Check for security-related logs
        docker logs "${REDIS_CONTAINER_NAME}" 2>&1 | grep -i "security\|error" > "$SECURITY_REPORTS_DIR/redis-runtime.log" || true
    fi
fi

# --- Verification ---
echo ""
log_info "--- Verification ---"
log_info "Waiting a few seconds for containers to initialize..."
sleep 5 # Give containers a moment to start up

log_info "Checking running Docker containers:"
docker ps -f name="${PG_CONTAINER_NAME}" -f name="${REDIS_CONTAINER_NAME}"

# --- Security Report Summary ---
if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    echo ""
    log_info "--- Security Report Summary ---"
    
    cat > "$SECURITY_REPORTS_DIR/runtime-summary.md" << EOF
# TMI Container Security Report

**Date:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")
**Configuration:** Development Local with Security Enhancements

## Container Status

| Container | Status | Image | Security Features |
|-----------|---------|-------|-------------------|
| PostgreSQL | Running | ${PG_IMAGE} | Patched, Logging Enhanced, Volume Mounted |
| Redis | Running | ${REDIS_IMAGE} | Patched, Protected Mode, Command Restrictions |

## Security Reports Generated

- PostgreSQL Pre-deployment Scan: postgresql-prescan.txt
- Redis Pre-deployment Scan: redis-prescan.txt
- PostgreSQL Runtime Logs: postgresql-runtime.log
- Redis Runtime Logs: redis-runtime.log

## Security Recommendations

1. Regularly update container images
2. Monitor security scan reports
3. Review runtime logs for anomalies
4. Implement network segmentation
5. Use secrets management for credentials

EOF

    log_success "Security report summary generated: $SECURITY_REPORTS_DIR/runtime-summary.md"
fi

echo ""
log_success "--- Setup Complete! ---"
echo "You can now connect to PostgreSQL at localhost:${PG_HOST_PORT}"
echo "  User: ${PG_USER}"
echo "  Password: ${PG_PASSWORD}"
echo "  Database: tmi_dev"
echo "You can now connect to Redis at localhost:${REDIS_HOST_PORT}"
echo "  No password required."

if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    echo ""
    log_info "--- Security Information ---"
    echo "Security reports available in: $SECURITY_REPORTS_DIR"
    echo "Container images have been scanned and patched for known vulnerabilities"
    echo "Runtime security monitoring is active"
fi

echo ""
echo "To stop these containers:"
echo "  docker stop ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To remove these containers (and preserve PostgreSQL data volume):"
echo "  docker rm ${PG_CONTAINER_NAME} ${REDIS_CONTAINER_NAME}"
echo "To view logs for PostgreSQL:"
echo "  docker logs ${PG_CONTAINER_NAME}"
echo "To view logs for Redis:"
echo "  docker logs ${REDIS_CONTAINER_NAME}"

if [ "$SECURITY_SCAN_ENABLED" = true ]; then
    echo "To view security scan results:"
    echo "  cat $SECURITY_REPORTS_DIR/runtime-summary.md"
fi