#!/bin/bash

# Container Build Script with Grype Security Scanning
# This script builds container images with automated vulnerability scanning using Grype (Anchore)

set -e           # Exit immediately if a command exits with a non-zero status
set -o pipefail  # Exit if any command in a pipeline fails

# --- Configuration ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
GIT_COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo 'development')"

# Container Configuration
PG_CONTAINER_NAME="tmi-postgresql"
REDIS_CONTAINER_NAME="tmi-redis"
APP_CONTAINER_NAME="tmi-server"
REGISTRY_PREFIX="${REGISTRY_PREFIX:-tmi}"

# Vulnerability thresholds
# Adjusted for Chainguard PostgreSQL (more secure base) and distroless Redis
MAX_CRITICAL_CVES=0
MAX_HIGH_CVES=5
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

# Function to check if Grype is available
check_grype() {
    if ! command -v grype >/dev/null 2>&1; then
        log_warning "Grype is not available. Vulnerability scanning will be skipped."
        log_info "Install: brew install grype"
        log_info "Or: curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin"
        return 1
    fi
    log_success "Grype is available"
    return 0
}

# Function to scan image for vulnerabilities using Grype
scan_image_vulnerabilities() {
    local image_name="$1"
    local report_file="$2"

    log_info "Scanning ${image_name} for vulnerabilities..."

    # Create security reports directory
    mkdir -p "${SECURITY_SCAN_OUTPUT_DIR}"

    # Check if Grype is available
    if ! command -v grype >/dev/null 2>&1; then
        log_warning "Grype not available, skipping vulnerability scan"
        return 0
    fi

    # Generate SARIF report
    grype "${image_name}" -o sarif > "${report_file}.sarif" 2>/dev/null || true

    # Generate human-readable table report
    grype "${image_name}" -o table > "${report_file}.txt" 2>/dev/null || true

    # Count vulnerabilities using JSON output
    local critical_count=$(grype "${image_name}" -o json 2>/dev/null | jq '[.matches[] | select(.vulnerability.severity == "Critical")] | length' 2>/dev/null || echo "0")
    local high_count=$(grype "${image_name}" -o json 2>/dev/null | jq '[.matches[] | select(.vulnerability.severity == "High")] | length' 2>/dev/null || echo "0")

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

# Function to generate SBOM for container image using Syft
generate_container_sbom() {
    local image_name="$1"
    local sbom_prefix="$2"

    log_info "Generating SBOM for ${image_name}..."

    # Check if Syft is available
    if ! command -v syft >/dev/null 2>&1; then
        log_warning "Syft not installed, skipping container SBOM generation"
        log_info "Install: brew install syft"
        return 0
    fi

    mkdir -p "${SECURITY_SCAN_OUTPUT_DIR}/sbom"

    # Generate CycloneDX JSON format
    if syft "${image_name}" -o cyclonedx-json="${SECURITY_SCAN_OUTPUT_DIR}/sbom/${sbom_prefix}-sbom.json" 2>/dev/null; then
        log_success "SBOM JSON generated: ${sbom_prefix}-sbom.json"
    else
        log_warning "SBOM JSON generation failed for ${image_name}"
    fi

    # Generate CycloneDX XML format
    if syft "${image_name}" -o cyclonedx-xml="${SECURITY_SCAN_OUTPUT_DIR}/sbom/${sbom_prefix}-sbom.xml" 2>/dev/null; then
        log_success "SBOM XML generated: ${sbom_prefix}-sbom.xml"
    else
        log_warning "SBOM XML generation failed for ${image_name}"
    fi
}

# Function to generate SBOM for Go application using cyclonedx-gomod
generate_go_application_sbom() {
    log_info "Generating SBOM for Go application..."

    # Check if cyclonedx-gomod is available
    if ! command -v cyclonedx-gomod >/dev/null 2>&1; then
        log_warning "cyclonedx-gomod not installed, skipping Go application SBOM"
        log_info "Install: brew install cyclonedx/cyclonedx/cyclonedx-gomod"
        return 0
    fi

    mkdir -p "${SECURITY_SCAN_OUTPUT_DIR}/sbom"

    # Generate for Go application
    if cyclonedx-gomod app -json -output "${SECURITY_SCAN_OUTPUT_DIR}/sbom/tmi-server-${GIT_COMMIT}-sbom.json" -main cmd/server 2>/dev/null; then
        log_success "Go application SBOM generated: tmi-server-${GIT_COMMIT}-sbom.json"
    fi

    if cyclonedx-gomod app -output "${SECURITY_SCAN_OUTPUT_DIR}/sbom/tmi-server-${GIT_COMMIT}-sbom.xml" -main cmd/server 2>/dev/null; then
        log_success "Go application SBOM generated: tmi-server-${GIT_COMMIT}-sbom.xml"
    fi
}

# Function to build and scan PostgreSQL container
build_postgresql_secure() {
    log_info "Building PostgreSQL container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.postgres" > "${PROJECT_ROOT}/Dockerfile.postgres.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.postgres.tmp" \
        -t "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.postgres.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/postgresql-scan"; then
        log_success "PostgreSQL container built and scanned successfully"
    else
        log_error "PostgreSQL container failed security scan"
        return 1
    fi

    # Generate SBOM for container
    generate_container_sbom "${REGISTRY_PREFIX}/${PG_CONTAINER_NAME}:latest" "tmi-postgresql-${GIT_COMMIT}"
}

# Function to build and scan Redis container
build_redis_secure() {
    log_info "Building Redis container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.redis" > "${PROJECT_ROOT}/Dockerfile.redis.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.redis.tmp" \
        -t "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.redis.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/redis-scan"; then
        log_success "Redis container built and scanned successfully"
    else
        log_error "Redis container failed security scan"
        return 1
    fi

    # Generate SBOM for container
    generate_container_sbom "${REGISTRY_PREFIX}/${REDIS_CONTAINER_NAME}:latest" "tmi-redis-${GIT_COMMIT}"
}

# Function to build and scan application container
build_application_secure() {
    log_info "Building application container..."
    
    # Update scan date in Dockerfile
    sed "s/AUTO_GENERATED/${BUILD_DATE}/g" "${PROJECT_ROOT}/Dockerfile.server" > "${PROJECT_ROOT}/Dockerfile.server.tmp"
    
    # Build the container
    docker build \
        -f "${PROJECT_ROOT}/Dockerfile.server.tmp" \
        -t "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:latest" \
        -t "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:${GIT_COMMIT}" \
        --build-arg BUILD_DATE="${BUILD_DATE}" \
        --build-arg GIT_COMMIT="${GIT_COMMIT}" \
        "${PROJECT_ROOT}"
    
    # Clean up temporary file
    rm -f "${PROJECT_ROOT}/Dockerfile.server.tmp"
    
    # Scan for vulnerabilities
    if scan_image_vulnerabilities "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:latest" "${SECURITY_SCAN_OUTPUT_DIR}/application-scan"; then
        log_success "Application container built and scanned successfully"
    else
        log_error "Application container failed security scan"
        return 1
    fi

    # Generate SBOM for Go application
    generate_go_application_sbom

    # Generate SBOM for container image
    generate_container_sbom "${REGISTRY_PREFIX}/${APP_CONTAINER_NAME}:latest" "tmi-server-${GIT_COMMIT}-container"
}

# Function to generate security report summary
generate_security_summary() {
    log_info "Generating security scan summary..."
    
    local summary_file="${SECURITY_SCAN_OUTPUT_DIR}/security-summary.md"
    
    cat > "${summary_file}" << EOF
# TMI Container Security Scan Summary

**Scan Date:** ${BUILD_DATE}
**Git Commit:** ${GIT_COMMIT}
**Scanner:** Grype (Anchore)

## Images Scanned

| Image | Status | Critical CVEs | High CVEs |
|-------|--------|---------------|-----------|
EOF

    # Add results for each image
    for container in postgresql redis application; do
        local scan_file="${SECURITY_SCAN_OUTPUT_DIR}/${container}-scan.txt"
        if [ -f "${scan_file}" ]; then
            local critical_count=$( (grep -c "CRITICAL" "${scan_file}" 2>/dev/null || echo "0") | tail -1 | tr -d '\n\r ' )
            local high_count=$( (grep -c "HIGH" "${scan_file}" 2>/dev/null || echo "0") | tail -1 | tr -d '\n\r ' )
            local status="✅ PASS"

            if [ "${critical_count}" -gt "${MAX_CRITICAL_CVES}" ] 2>/dev/null; then
                status="❌ FAIL"
            elif [ "${high_count}" -gt "${MAX_HIGH_CVES}" ] 2>/dev/null; then
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

## Software Bill of Materials (SBOM)

### Go Application SBOMs (cyclonedx-gomod)
| Component | JSON | XML |
|-----------|------|-----|
EOF

    # Add Go application SBOM if exists
    if [ -f "${SECURITY_SCAN_OUTPUT_DIR}/sbom/tmi-server-${GIT_COMMIT}-sbom.json" ]; then
        echo "| Go Application | [JSON](sbom/tmi-server-${GIT_COMMIT}-sbom.json) | [XML](sbom/tmi-server-${GIT_COMMIT}-sbom.xml) |" >> "${summary_file}"
    fi

    cat >> "${summary_file}" << EOF

### Container Image SBOMs (Syft - CycloneDX)
| Container | JSON | XML |
|-----------|------|-----|
EOF

    # Add container SBOMs if they exist
    for container in postgresql redis; do
        local sbom_json="${SECURITY_SCAN_OUTPUT_DIR}/sbom/tmi-${container}-${GIT_COMMIT}-sbom.json"
        if [ -f "${sbom_json}" ]; then
            echo "| ${container} | [JSON](sbom/tmi-${container}-${GIT_COMMIT}-sbom.json) | [XML](sbom/tmi-${container}-${GIT_COMMIT}-sbom.xml) |" >> "${summary_file}"
        fi
    done

    # Add server container SBOM
    if [ -f "${SECURITY_SCAN_OUTPUT_DIR}/sbom/tmi-server-${GIT_COMMIT}-container-sbom.json" ]; then
        echo "| Server Container | [JSON](sbom/tmi-server-${GIT_COMMIT}-container-sbom.json) | [XML](sbom/tmi-server-${GIT_COMMIT}-container-sbom.xml) |" >> "${summary_file}"
    fi

    cat >> "${summary_file}" << EOF

## Next Steps

1. Review detailed vulnerability reports
2. Update base images if new patches are available
3. Consider implementing additional security controls
4. Schedule regular re-scans
5. Review SBOMs for license compliance and dependency tracking

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
    echo "--- TMI Container Build Script ---"
    echo "Build Date: ${BUILD_DATE}"
    echo "Git Commit: ${GIT_COMMIT}"
    echo ""
    
    # Pre-checks
    check_grype
    
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
        check_grype
        build_postgresql_secure
        ;;
    redis)
        check_grype
        build_redis_secure
        ;;
    application|app)
        check_grype
        build_application_secure
        ;;
    all)
        main
        ;;
    scan-only)
        check_grype
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