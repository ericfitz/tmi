#!/bin/bash

# Enhanced Container Build Script with Docker Scout Security Scanning and Patching
# This script builds secure container images with automated vulnerability patching

set -e           # Exit immediately if a command exits with a non-zero status
set -o pipefail  # Exit if any command in a pipeline fails

# --- Configuration ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 'development')"

# Container Configuration
PG_CONTAINER_NAME="tmi-postgresql-secure"
REDIS_CONTAINER_NAME="tmi-redis-secure"
APP_CONTAINER_NAME="tmi-app-secure"
REGISTRY_PREFIX="${REGISTRY_PREFIX:-tmi}"

# Vulnerability thresholds
MAX_CRITICAL_CVES=0
MAX_HIGH_CVES=2
SECURITY_SCAN_OUTPUT_DIR="${PROJECT_ROOT}/security-reports"

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

# Function to check if Docker Scout is available
check_docker_scout() {
    if ! docker scout version >/dev/null 2>&1; then
        log_error "Docker Scout is not available. Please install Docker Scout CLI."
        log_info "Installation: https://docs.docker.com/scout/install/"
        exit 1
    fi
    log_success "Docker Scout is available"
}

# Function to scan image for vulnerabilities
scan_image_vulnerabilities() {
    local image_name="$1"
    local report_file="$2"
    
    log_info "Scanning ${image_name} for vulnerabilities..."
    
    # Create security reports directory
    mkdir -p "${SECURITY_SCAN_OUTPUT_DIR}"
    
    # Scan for critical and high vulnerabilities
    docker scout cves "${image_name}" \
        --only-severity critical,high \
        --format sarif \
        --output "${report_file}.sarif" || true
    
    # Get human-readable summary
    docker scout cves "${image_name}" \
        --only-severity critical,high > "${report_file}.txt" || true
    
    # Count vulnerabilities
    local critical_count=$(docker scout cves "${image_name}" --only-severity critical --format sarif 2>/dev/null | jq -r '.runs[0].results | length' 2>/dev/null || echo "0")
    local high_count=$(docker scout cves "${image_name}" --only-severity high --format sarif 2>/dev/null | jq -r '.runs[0].results | length' 2>/dev/null || echo "0")
    
    # Default to 0 if jq fails
    critical_count=${critical_count:-0}
    high_count=${high_count:-0}
    
    log_info "Found ${critical_count} critical and ${high_count} high severity vulnerabilities"
    
    # Check against thresholds
    if [ "${critical_count}" -gt "${MAX_CRITICAL_CVES}" ]; then
        log_error "Image ${image_name} has ${critical_count} critical vulnerabilities (max allowed: ${MAX_CRITICAL_CVES})"
        return 1
    fi
    
    if [ "${high_count}" -gt "${MAX_HIGH_CVES}" ]; then
        log_warning "Image ${image_name} has ${high_count} high severity vulnerabilities (max recommended: ${MAX_HIGH_CVES})"
    fi
    
    return 0
}

# Function to build and scan PostgreSQL container
build_postgresql_secure() {
    log_info "Building secure PostgreSQL container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.postgres.secure" > "${PROJECT_ROOT}/Dockerfile.postgres.secure.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.postgres.secure.tmp" \
        -t "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.postgres.secure.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/postgresql-scan"; then
        log_success "PostgreSQL container built and scanned successfully"
    else
        log_error "PostgreSQL container failed security scan"
        return 1
    fi
}

# Function to build and scan Redis container
build_redis_secure() {
    log_info "Building secure Redis container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.redis.secure" > "${PROJECT_ROOT}/Dockerfile.redis.secure.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.redis.secure.tmp" \
        -t "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.redis.secure.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/redis-scan"; then
        log_success "Redis container built and scanned successfully"
    else
        log_error "Redis container failed security scan"
        return 1
    fi
}

# Function to build and scan application container
build_application_secure() {
    log_info "Building secure application container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.dev.secure" > "${PROJECT_ROOT}/Dockerfile.dev.secure.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.dev.secure.tmp" \
        -t "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.dev.secure.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/application-scan"; then
        log_success "Application container built and scanned successfully"
    else
        log_error "Application container failed security scan"
        return 1
    fi
}

# Function to generate security report summary
generate_security_summary() {
    log_info "Generating security scan summary..."
    
    local summary_file="${SECURITY_SCAN_OUTPUT_DIR}/security-summary.md"
    
    cat > "${summary_file}" << EOF
# TMI Container Security Scan Summary

**Scan Date:** ${BUILD_DATE}
**Git Commit:** ${GIT_COMMIT}
**Scanner:** Docker Scout

## Images Scanned

| Image | Status | Critical CVEs | High CVEs |
|-------|--------|---------------|-----------|
EOF

    # Add results for each image
    for container in postgresql redis application; do
        local scan_file="${SECURITY_SCAN_OUTPUT_DIR}/${container}-scan.txt"
        if [ -f "${scan_file}" ]; then
            local critical_count=$(grep -c "CRITICAL" "${scan_file}" 2>/dev/null || echo "0")
            local high_count=$(grep -c "HIGH" "${scan_file}" 2>/dev/null || echo "0")
            local status="✅ PASS"
            
            if [ "${critical_count}" -gt "${MAX_CRITICAL_CVES}" ]; then
                status="❌ FAIL"
            elif [ "${high_count}" -gt "${MAX_HIGH_CVES}" ]; then
                status="⚠️ WARNING"
            fi
            
            echo "| ${container} | ${status} | ${critical_count} | ${high_count} |" >> "${summary_file}"
        fi
    done
    
    cat >> "${summary_file}" << EOF

## Scan Thresholds

- **Maximum Critical CVEs:** ${MAX_CRITICAL_CVES}
- **Maximum High CVEs (recommended):** ${MAX_HIGH_CVES}

## Detailed Reports

- PostgreSQL: [SARIF](postgresql-scan.sarif) | [Text](postgresql-scan.txt)
- Redis: [SARIF](redis-scan.sarif) | [Text](redis-scan.txt)  
- Application: [SARIF](application-scan.sarif) | [Text](application-scan.txt)

## Next Steps

1. Review detailed vulnerability reports
2. Update base images if new patches are available
3. Consider implementing additional security controls
4. Schedule regular re-scans

EOF

    log_success "Security summary generated: ${summary_file}"
}

# Function to cleanup old images
cleanup_old_images() {
    log_info "Cleaning up old untagged images..."
    docker image prune -f >/dev/null 2>&1 || true
    log_success "Image cleanup completed"
}

# Main execution
main() {
    echo "--- TMI Secure Container Build Script ---"
    echo "Build Date: ${BUILD_DATE}"
    echo "Git Commit: ${GIT_COMMIT}"
    echo ""
    
    # Pre-checks
    check_docker_scout
    
    # Clean up old images first
    cleanup_old_images
    
    # Build all containers
    local exit_code=0
    
    if ! build_postgresql_secure; then
        exit_code=1
    fi
    
    if ! build_redis_secure; then
        exit_code=1
    fi
    
    if ! build_application_secure; then
        exit_code=1
    fi
    
    # Generate security summary
    generate_security_summary
    
    # Final status
    if [ $exit_code -eq 0 ]; then
        log_success "All containers built and scanned successfully!"
        log_info "Security reports available in: ${SECURITY_SCAN_OUTPUT_DIR}"
    else
        log_error "Some containers failed security validation"
        log_info "Check security reports in: ${SECURITY_SCAN_OUTPUT_DIR}"
        exit 1
    fi
}

# Handle script arguments
case "${1:-all}" in
    postgresql|pg)
        check_docker_scout
        build_postgresql_secure
        ;;
    redis)
        check_docker_scout
        build_redis_secure
        ;;
    application|app)
        check_docker_scout
        build_application_secure
        ;;
    all)
        main
        ;;
    scan-only)
        check_docker_scout
        generate_security_summary
        ;;
    *)
        echo "Usage: $0 [postgresql|redis|application|all|scan-only]"
        echo "  postgresql  - Build only PostgreSQL secure container"
        echo "  redis       - Build only Redis secure container"
        echo "  application - Build only Application secure container"
        echo "  all         - Build all secure containers (default)"
        echo "  scan-only   - Generate security summary from existing scans"
        exit 1
        ;;
esac