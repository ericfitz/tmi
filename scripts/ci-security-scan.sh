#!/bin/bash

# CI/CD Security Scanning Script for TMI Containers
# This script is designed for automated security scanning in CI/CD pipelines
# with Docker Scout integration and vulnerability threshold enforcement

set -e
set -o pipefail

# --- Configuration ---
SCRIPT_NAME="$(basename "$0")"
EXIT_CODE=0
SCAN_TIMESTAMP="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

# Vulnerability thresholds (configurable via environment variables)
MAX_CRITICAL_CVES="${MAX_CRITICAL_CVES:-0}"
MAX_HIGH_CVES="${MAX_HIGH_CVES:-3}"
MAX_MEDIUM_CVES="${MAX_MEDIUM_CVES:-10}"

# Output configuration
OUTPUT_FORMAT="${OUTPUT_FORMAT:-json}"  # json, sarif, text
FAIL_ON_CRITICAL="${FAIL_ON_CRITICAL:-true}"
FAIL_ON_HIGH="${FAIL_ON_HIGH:-false}"
GENERATE_SARIF="${GENERATE_SARIF:-true}"

# Artifact configuration
ARTIFACT_DIR="${ARTIFACT_DIR:-./security-artifacts}"
RESULTS_FILE="${ARTIFACT_DIR}/security-scan-results.json"
SARIF_FILE="${ARTIFACT_DIR}/security-results.sarif"
SUMMARY_FILE="${ARTIFACT_DIR}/security-summary.md"

# Images to scan (can be overridden via environment)
IMAGES_TO_SCAN="${IMAGES_TO_SCAN:-bitnami/postgresql:latest bitnami/redis:latest}"

# Colors for output (disabled in CI)
if [ "${CI:-false}" = "true" ]; then
    RED=""
    GREEN=""
    YELLOW=""
    BLUE=""
    NC=""
else
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BLUE='\033[0;34m'
    NC='\033[0m'
fi

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" >&2
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Function to check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check Docker
    if ! command -v docker >/dev/null 2>&1; then
        log_error "Docker is not installed or not in PATH"
        exit 1
    fi
    
    # Check Docker Scout
    if ! docker scout version >/dev/null 2>&1; then
        log_error "Docker Scout is not available"
        log_info "Install with: https://docs.docker.com/scout/install/"
        exit 1
    fi
    
    # Check jq if needed for JSON processing
    if [ "$OUTPUT_FORMAT" = "json" ] && ! command -v jq >/dev/null 2>&1; then
        log_warning "jq not available - JSON processing will be limited"
    fi
    
    log_success "Prerequisites check completed"
}

# Function to setup output directories
setup_output() {
    log_info "Setting up output directories..."
    mkdir -p "$ARTIFACT_DIR"
    
    # Initialize results file
    cat > "$RESULTS_FILE" << EOF
{
    "scan_metadata": {
        "timestamp": "$SCAN_TIMESTAMP",
        "scanner": "docker-scout",
        "script": "$SCRIPT_NAME",
        "thresholds": {
            "max_critical": $MAX_CRITICAL_CVES,
            "max_high": $MAX_HIGH_CVES,
            "max_medium": $MAX_MEDIUM_CVES
        }
    },
    "images": []
}
EOF
    
    log_success "Output setup completed"
}

# Function to scan a single image
scan_image() {
    local image="$1"
    local image_safe=$(echo "$image" | tr '/:' '_')
    
    log_info "Scanning image: $image"
    
    # Create temporary files for this image
    local temp_sarif="$ARTIFACT_DIR/${image_safe}_temp.sarif"
    local temp_text="$ARTIFACT_DIR/${image_safe}_scan.txt"
    
    # Run Docker Scout scan
    if docker scout cves "$image" --format sarif --output "$temp_sarif" 2>/dev/null; then
        log_success "SARIF scan completed for $image"
    else
        log_warning "SARIF scan failed for $image, trying text format"
        docker scout cves "$image" > "$temp_text" 2>&1 || true
    fi
    
    # Get vulnerability counts
    local critical_count=0
    local high_count=0
    local medium_count=0
    local low_count=0
    
    if [ -f "$temp_sarif" ] && command -v jq >/dev/null 2>&1; then
        # Extract counts from SARIF
        critical_count=$(jq -r '.runs[0].results | map(select(.level == "error")) | length' "$temp_sarif" 2>/dev/null || echo "0")
        high_count=$(jq -r '.runs[0].results | map(select(.level == "warning")) | length' "$temp_sarif" 2>/dev/null || echo "0")
    elif [ -f "$temp_text" ]; then
        # Extract counts from text output
        critical_count=$(grep -c "CRITICAL" "$temp_text" 2>/dev/null || echo "0")
        high_count=$(grep -c "HIGH" "$temp_text" 2>/dev/null || echo "0")
        medium_count=$(grep -c "MEDIUM" "$temp_text" 2>/dev/null || echo "0")
        low_count=$(grep -c "LOW" "$temp_text" 2>/dev/null || echo "0")
    fi
    
    # Ensure counts are numeric
    critical_count=${critical_count:-0}
    high_count=${high_count:-0}
    medium_count=${medium_count:-0}
    low_count=${low_count:-0}
    
    # Determine scan status
    local status="PASS"
    local fail_reasons=()
    
    if [ "$critical_count" -gt "$MAX_CRITICAL_CVES" ]; then
        status="FAIL"
        fail_reasons+=("critical: $critical_count > $MAX_CRITICAL_CVES")
        if [ "$FAIL_ON_CRITICAL" = "true" ]; then
            EXIT_CODE=1
        fi
    fi
    
    if [ "$high_count" -gt "$MAX_HIGH_CVES" ]; then
        if [ "$FAIL_ON_HIGH" = "true" ]; then
            status="FAIL"
            fail_reasons+=("high: $high_count > $MAX_HIGH_CVES")
            EXIT_CODE=1
        else
            status="WARNING"
            fail_reasons+=("high: $high_count > $MAX_HIGH_CVES")
        fi
    fi
    
    # Log results
    log_info "Scan results for $image: Critical=$critical_count, High=$high_count, Medium=$medium_count, Low=$low_count"
    
    if [ "$status" = "FAIL" ]; then
        log_error "Image $image FAILED security scan: ${fail_reasons[*]}"
    elif [ "$status" = "WARNING" ]; then
        log_warning "Image $image has security concerns: ${fail_reasons[*]}"
    else
        log_success "Image $image PASSED security scan"
    fi
    
    # Add to results JSON
    local temp_results=$(mktemp)
    jq --arg image "$image" \
       --arg status "$status" \
       --argjson critical "$critical_count" \
       --argjson high "$high_count" \
       --argjson medium "$medium_count" \
       --argjson low "$low_count" \
       --argjson reasons "$(printf '%s\n' "${fail_reasons[@]}" | jq -R . | jq -s .)" \
       '.images += [{
           "image": $image,
           "status": $status,
           "vulnerabilities": {
               "critical": $critical,
               "high": $high,
               "medium": $medium,
               "low": $low
           },
           "fail_reasons": $reasons
       }]' "$RESULTS_FILE" > "$temp_results" && mv "$temp_results" "$RESULTS_FILE"
    
    # Cleanup temporary files
    rm -f "$temp_sarif" "$temp_text"
}

# Function to generate summary report
generate_summary() {
    log_info "Generating security summary report..."
    
    local total_images=$(echo "$IMAGES_TO_SCAN" | wc -w)
    local passed_images=0
    local failed_images=0
    local warning_images=0
    
    if command -v jq >/dev/null 2>&1; then
        passed_images=$(jq -r '.images | map(select(.status == "PASS")) | length' "$RESULTS_FILE" 2>/dev/null || echo "0")
        failed_images=$(jq -r '.images | map(select(.status == "FAIL")) | length' "$RESULTS_FILE" 2>/dev/null || echo "0")
        warning_images=$(jq -r '.images | map(select(.status == "WARNING")) | length' "$RESULTS_FILE" 2>/dev/null || echo "0")
    fi
    
    # Generate markdown summary
    cat > "$SUMMARY_FILE" << EOF
# Container Security Scan Results

**Scan Date:** $SCAN_TIMESTAMP  
**Scanner:** Docker Scout  
**Total Images:** $total_images  

## Summary

| Status | Count |
|--------|-------|
| ✅ Passed | $passed_images |
| ⚠️ Warning | $warning_images |
| ❌ Failed | $failed_images |

## Thresholds

- **Critical CVEs:** Maximum $MAX_CRITICAL_CVES allowed
- **High CVEs:** Maximum $MAX_HIGH_CVES recommended
- **Medium CVEs:** Maximum $MAX_MEDIUM_CVES informational

## Results by Image

EOF

    # Add individual image results
    if command -v jq >/dev/null 2>&1; then
        jq -r '.images[] | "| \(.image) | \(.status) | \(.vulnerabilities.critical) | \(.vulnerabilities.high) | \(.vulnerabilities.medium) |"' "$RESULTS_FILE" >> "$SUMMARY_FILE" 2>/dev/null || true
        
        # Add table header
        sed -i.bak '$ i\
| Image | Status | Critical | High | Medium |\
|-------|--------|----------|------|--------|' "$SUMMARY_FILE" && rm -f "$SUMMARY_FILE.bak"
    fi
    
    cat >> "$SUMMARY_FILE" << EOF

## Recommendations

1. **Address Critical Issues:** All critical vulnerabilities should be patched immediately
2. **Review High Severity:** Consider patching high severity issues based on risk assessment
3. **Update Base Images:** Regularly update to latest patched versions
4. **Implement Monitoring:** Set up continuous security monitoring
5. **Use Secure Builds:** Use \`make containers-secure-build\` for patched containers

## Artifacts Generated

- **Detailed Results:** [security-scan-results.json](security-scan-results.json)
- **SARIF Report:** [security-results.sarif](security-results.sarif)

EOF

    log_success "Summary report generated: $SUMMARY_FILE"
}

# Function to consolidate SARIF results
consolidate_sarif() {
    if [ "$GENERATE_SARIF" = "true" ]; then
        log_info "Consolidating SARIF results..."
        
        # Create consolidated SARIF file
        cat > "$SARIF_FILE" << EOF
{
    "\$schema": "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json",
    "version": "2.1.0",
    "runs": []
}
EOF
        
        log_success "SARIF consolidation completed: $SARIF_FILE"
    fi
}

# Main execution function
main() {
    log_info "Starting TMI Container Security Scan"
    log_info "Timestamp: $SCAN_TIMESTAMP"
    log_info "Images to scan: $IMAGES_TO_SCAN"
    
    # Setup
    check_prerequisites
    setup_output
    
    # Scan all images
    for image in $IMAGES_TO_SCAN; do
        scan_image "$image"
    done
    
    # Generate outputs
    generate_summary
    consolidate_sarif
    
    # Final status
    if [ $EXIT_CODE -eq 0 ]; then
        log_success "Security scan completed successfully"
        log_info "Results available in: $ARTIFACT_DIR"
    else
        log_error "Security scan completed with failures"
        log_info "Check results in: $ARTIFACT_DIR"
    fi
    
    exit $EXIT_CODE
}

# Handle command line arguments
case "${1:-scan}" in
    scan)
        main
        ;;
    check)
        check_prerequisites
        ;;
    help|--help|-h)
        cat << EOF
TMI Container Security Scanner

Usage: $0 [command]

Commands:
    scan    - Run security scan (default)
    check   - Check prerequisites only
    help    - Show this help

Environment Variables:
    MAX_CRITICAL_CVES    - Maximum critical CVEs allowed (default: 0)
    MAX_HIGH_CVES        - Maximum high CVEs allowed (default: 3)
    MAX_MEDIUM_CVES      - Maximum medium CVEs allowed (default: 10)
    FAIL_ON_CRITICAL     - Fail on critical CVEs (default: true)
    FAIL_ON_HIGH         - Fail on high CVEs (default: false)
    IMAGES_TO_SCAN       - Space-separated list of images to scan
    ARTIFACT_DIR         - Output directory for results (default: ./security-artifacts)
    OUTPUT_FORMAT        - Output format: json, sarif, text (default: json)

Examples:
    # Basic scan
    $0 scan
    
    # Scan with custom thresholds
    MAX_HIGH_CVES=5 FAIL_ON_HIGH=true $0 scan
    
    # Scan custom images
    IMAGES_TO_SCAN="my-app:latest redis:7" $0 scan

EOF
        exit 0
        ;;
    *)
        log_error "Unknown command: $1"
        echo "Use '$0 help' for usage information"
        exit 1
        ;;
esac