#!/bin/bash

# TMI Coverage Report Generator
# This script generates comprehensive test coverage reports including unit tests and integration tests

set -e  # Exit on any error

# Configuration
COVERAGE_DIR="coverage"
UNIT_COVERAGE_FILE="unit_coverage.out"
INTEGRATION_COVERAGE_FILE="integration_coverage.out"
COMBINED_COVERAGE_FILE="combined_coverage.out"
HTML_REPORT_DIR="coverage_html"
POSTGRES_TEST_PORT=5434
REDIS_TEST_PORT=6381
POSTGRES_CONTAINER="tmi-integration-postgres"
REDIS_CONTAINER="tmi-integration-redis"
POSTGRES_USER="tmi_integration"
POSTGRES_PASSWORD="integration_test_123"
POSTGRES_DB="tmi_integration_test"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
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

log_header() {
    echo -e "${CYAN}================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}================================${NC}"
}

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check prerequisites
check_prerequisites() {
    log_header "Checking Prerequisites"
    
    # Check if Go is installed
    if ! command_exists go; then
        log_error "Go is not installed or not in PATH"
        exit 1
    fi
    
    # Check if Docker is installed and running
    if ! command_exists docker; then
        log_error "Docker is not installed or not in PATH"
        exit 1
    fi
    
    if ! docker info >/dev/null 2>&1; then
        log_error "Docker is not running"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

# Function to setup coverage directory
setup_coverage_dir() {
    log_info "Setting up coverage directory..."
    mkdir -p "$COVERAGE_DIR"
    mkdir -p "$HTML_REPORT_DIR"
    
    # Clean up old coverage files
    rm -f "$COVERAGE_DIR"/*.out
    rm -rf "$HTML_REPORT_DIR"/*
    
    log_success "Coverage directory prepared"
}

# Function to run unit tests with coverage
run_unit_tests() {
    log_header "Running Unit Tests with Coverage"
    
    local unit_coverage_path="$COVERAGE_DIR/$UNIT_COVERAGE_FILE"
    
    log_info "Running unit tests (excluding integration tests)..."
    
    # Run unit tests with coverage, excluding integration tests
    TMI_LOGGING_IS_TEST=true go test \
        -coverprofile="$unit_coverage_path" \
        -covermode=atomic \
        -coverpkg=./... \
        $(go list ./... | grep -v integration) \
        -tags="!integration" \
        -v
    
    if [[ -f "$unit_coverage_path" ]]; then
        log_success "Unit tests completed successfully"
        
        # Generate unit test coverage summary
        local unit_coverage=$(go tool cover -func="$unit_coverage_path" | tail -1 | awk '{print $3}')
        log_info "Unit test coverage: $unit_coverage"
    else
        log_warning "Unit test coverage file not generated"
    fi
}

# Function to setup test databases for integration tests
setup_test_databases() {
    log_info "Setting up test databases..."
    
    # Stop and remove existing containers if they exist
    docker stop $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    docker rm $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    
    # Start PostgreSQL for integration tests
    log_info "Starting PostgreSQL container for integration tests..."
    docker run -d \
        --name $POSTGRES_CONTAINER \
        -p $POSTGRES_TEST_PORT:5432 \
        -e POSTGRES_USER=$POSTGRES_USER \
        -e POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
        -e POSTGRES_DB=$POSTGRES_DB \
        postgres:14-alpine
    
    # Start Redis for integration tests
    log_info "Starting Redis container for integration tests..."
    docker run -d \
        --name $REDIS_CONTAINER \
        -p $REDIS_TEST_PORT:6379 \
        redis:7-alpine
    
    # Wait for databases to be ready
    log_info "Waiting for databases to be ready..."
    sleep 10
    
    # Verify PostgreSQL connection
    local postgres_ready=false
    for i in {1..30}; do
        if docker exec $POSTGRES_CONTAINER pg_isready -U $POSTGRES_USER >/dev/null 2>&1; then
            postgres_ready=true
            break
        fi
        sleep 1
    done
    
    if [[ "$postgres_ready" == false ]]; then
        log_error "PostgreSQL failed to start"
        exit 1
    fi
    
    # Verify Redis connection
    local redis_ready=false
    for i in {1..30}; do
        if docker exec $REDIS_CONTAINER redis-cli ping >/dev/null 2>&1; then
            redis_ready=true
            break
        fi
        sleep 1
    done
    
    if [[ "$redis_ready" == false ]]; then
        log_error "Redis failed to start"
        exit 1
    fi
    
    log_success "Test databases are ready"
}

# Function to run database migrations for integration tests
run_integration_migrations() {
    log_info "Running database migrations for integration tests..."
    
    # Set environment variables for integration tests
    export TMI_POSTGRES_HOST=localhost
    export TMI_POSTGRES_PORT=$POSTGRES_TEST_PORT
    export TMI_POSTGRES_USER=$POSTGRES_USER
    export TMI_POSTGRES_PASSWORD=$POSTGRES_PASSWORD
    export TMI_POSTGRES_DATABASE=$POSTGRES_DB
    export TMI_REDIS_HOST=localhost
    export TMI_REDIS_PORT=$REDIS_TEST_PORT
    export TMI_LOGGING_IS_TEST=true
    
    # Build and run migrations
    if [[ -f "./migrate" ]]; then
        ./migrate
    elif [[ -f "./bin/migrate" ]]; then
        ./bin/migrate
    else
        # Try to build migrate tool
        go build -o ./bin/migrate ./cmd/migrate
        ./bin/migrate
    fi
    
    log_success "Database migrations completed"
}

# Function to run integration tests with coverage
run_integration_tests() {
    log_header "Running Integration Tests with Coverage"
    
    local integration_coverage_path="$COVERAGE_DIR/$INTEGRATION_COVERAGE_FILE"
    
    # Setup test databases
    setup_test_databases
    
    # Run migrations
    run_integration_migrations
    
    log_info "Running integration tests with coverage..."
    
    # Run integration tests with coverage
    TMI_LOGGING_IS_TEST=true \
    TMI_POSTGRES_HOST=localhost \
    TMI_POSTGRES_PORT=$POSTGRES_TEST_PORT \
    TMI_POSTGRES_USER=$POSTGRES_USER \
    TMI_POSTGRES_PASSWORD=$POSTGRES_PASSWORD \
    TMI_POSTGRES_DATABASE=$POSTGRES_DB \
    TMI_REDIS_HOST=localhost \
    TMI_REDIS_PORT=$REDIS_TEST_PORT \
    go test \
        -coverprofile="$integration_coverage_path" \
        -covermode=atomic \
        -coverpkg=./... \
        -tags=integration \
        $(go list ./... | grep -E "(integration|_test\.go)" | head -10) \
        -v
    
    if [[ -f "$integration_coverage_path" ]]; then
        log_success "Integration tests completed successfully"
        
        # Generate integration test coverage summary
        local integration_coverage=$(go tool cover -func="$integration_coverage_path" | tail -1 | awk '{print $3}')
        log_info "Integration test coverage: $integration_coverage"
    else
        log_warning "Integration test coverage file not generated"
    fi
}

# Function to cleanup test databases
cleanup_test_databases() {
    log_info "Cleaning up test databases..."
    docker stop $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    docker rm $POSTGRES_CONTAINER $REDIS_CONTAINER 2>/dev/null || true
    log_success "Test databases cleaned up"
}

# Function to merge coverage profiles
merge_coverage_profiles() {
    log_header "Merging Coverage Profiles"
    
    local unit_coverage_path="$COVERAGE_DIR/$UNIT_COVERAGE_FILE"
    local integration_coverage_path="$COVERAGE_DIR/$INTEGRATION_COVERAGE_FILE"
    local combined_coverage_path="$COVERAGE_DIR/$COMBINED_COVERAGE_FILE"
    
    # Check if gocovmerge is installed
    if ! command_exists gocovmerge; then
        log_info "Installing gocovmerge..."
        go install github.com/wadey/gocovmerge@latest
    fi
    
    # Merge coverage profiles
    log_info "Merging coverage profiles..."
    
    local profiles_to_merge=""
    
    if [[ -f "$unit_coverage_path" ]]; then
        profiles_to_merge="$profiles_to_merge $unit_coverage_path"
        log_info "Including unit test coverage"
    fi
    
    if [[ -f "$integration_coverage_path" ]]; then
        profiles_to_merge="$profiles_to_merge $integration_coverage_path"
        log_info "Including integration test coverage"
    fi
    
    if [[ -n "$profiles_to_merge" ]]; then
        gocovmerge $profiles_to_merge > "$combined_coverage_path"
        log_success "Coverage profiles merged successfully"
        
        # Generate combined coverage summary
        local combined_coverage=$(go tool cover -func="$combined_coverage_path" | tail -1 | awk '{print $3}')
        log_info "Combined test coverage: $combined_coverage"
    else
        log_warning "No coverage profiles to merge"
    fi
}

# Function to generate HTML reports
generate_html_reports() {
    log_header "Generating HTML Coverage Reports"
    
    local unit_coverage_path="$COVERAGE_DIR/$UNIT_COVERAGE_FILE"
    local integration_coverage_path="$COVERAGE_DIR/$INTEGRATION_COVERAGE_FILE"
    local combined_coverage_path="$COVERAGE_DIR/$COMBINED_COVERAGE_FILE"
    
    # Generate unit test HTML report
    if [[ -f "$unit_coverage_path" ]]; then
        log_info "Generating unit test HTML report..."
        go tool cover -html="$unit_coverage_path" -o "$HTML_REPORT_DIR/unit_coverage.html"
        log_success "Unit test HTML report: $HTML_REPORT_DIR/unit_coverage.html"
    fi
    
    # Generate integration test HTML report
    if [[ -f "$integration_coverage_path" ]]; then
        log_info "Generating integration test HTML report..."
        go tool cover -html="$integration_coverage_path" -o "$HTML_REPORT_DIR/integration_coverage.html"
        log_success "Integration test HTML report: $HTML_REPORT_DIR/integration_coverage.html"
    fi
    
    # Generate combined HTML report
    if [[ -f "$combined_coverage_path" ]]; then
        log_info "Generating combined coverage HTML report..."
        go tool cover -html="$combined_coverage_path" -o "$HTML_REPORT_DIR/combined_coverage.html"
        log_success "Combined coverage HTML report: $HTML_REPORT_DIR/combined_coverage.html"
    fi
}

# Function to generate detailed text reports
generate_text_reports() {
    log_header "Generating Detailed Text Reports"
    
    local unit_coverage_path="$COVERAGE_DIR/$UNIT_COVERAGE_FILE"
    local integration_coverage_path="$COVERAGE_DIR/$INTEGRATION_COVERAGE_FILE"
    local combined_coverage_path="$COVERAGE_DIR/$COMBINED_COVERAGE_FILE"
    
    # Generate detailed unit test report
    if [[ -f "$unit_coverage_path" ]]; then
        log_info "Generating detailed unit test report..."
        go tool cover -func="$unit_coverage_path" > "$COVERAGE_DIR/unit_coverage_detailed.txt"
        log_success "Unit test detailed report: $COVERAGE_DIR/unit_coverage_detailed.txt"
    fi
    
    # Generate detailed integration test report
    if [[ -f "$integration_coverage_path" ]]; then
        log_info "Generating detailed integration test report..."
        go tool cover -func="$integration_coverage_path" > "$COVERAGE_DIR/integration_coverage_detailed.txt"
        log_success "Integration test detailed report: $COVERAGE_DIR/integration_coverage_detailed.txt"
    fi
    
    # Generate detailed combined report
    if [[ -f "$combined_coverage_path" ]]; then
        log_info "Generating detailed combined coverage report..."
        go tool cover -func="$combined_coverage_path" > "$COVERAGE_DIR/combined_coverage_detailed.txt"
        log_success "Combined coverage detailed report: $COVERAGE_DIR/combined_coverage_detailed.txt"
    fi
}

# Function to generate summary report
generate_summary_report() {
    log_header "Generating Coverage Summary"
    
    local summary_file="$COVERAGE_DIR/coverage_summary.txt"
    local unit_coverage_path="$COVERAGE_DIR/$UNIT_COVERAGE_FILE"
    local integration_coverage_path="$COVERAGE_DIR/$INTEGRATION_COVERAGE_FILE"
    local combined_coverage_path="$COVERAGE_DIR/$COMBINED_COVERAGE_FILE"
    
    echo "TMI Test Coverage Summary" > "$summary_file"
    echo "Generated: $(date)" >> "$summary_file"
    echo "======================================" >> "$summary_file"
    echo "" >> "$summary_file"
    
    # Unit test coverage
    if [[ -f "$unit_coverage_path" ]]; then
        local unit_coverage=$(go tool cover -func="$unit_coverage_path" | tail -1 | awk '{print $3}')
        echo "Unit Test Coverage: $unit_coverage" >> "$summary_file"
        
        # Top files by coverage
        echo "" >> "$summary_file"
        echo "Unit Test Coverage by File (Top 10):" >> "$summary_file"
        go tool cover -func="$unit_coverage_path" | sort -k3 -nr | head -10 >> "$summary_file"
    else
        echo "Unit Test Coverage: Not available" >> "$summary_file"
    fi
    
    echo "" >> "$summary_file"
    
    # Integration test coverage
    if [[ -f "$integration_coverage_path" ]]; then
        local integration_coverage=$(go tool cover -func="$integration_coverage_path" | tail -1 | awk '{print $3}')
        echo "Integration Test Coverage: $integration_coverage" >> "$summary_file"
    else
        echo "Integration Test Coverage: Not available" >> "$summary_file"
    fi
    
    echo "" >> "$summary_file"
    
    # Combined coverage
    if [[ -f "$combined_coverage_path" ]]; then
        local combined_coverage=$(go tool cover -func="$combined_coverage_path" | tail -1 | awk '{print $3}')
        echo "Combined Test Coverage: $combined_coverage" >> "$summary_file"
        
        # Files with low coverage
        echo "" >> "$summary_file"
        echo "Files with Low Coverage (<50%):" >> "$summary_file"
        go tool cover -func="$combined_coverage_path" | awk '$3 < 50 && NF == 3 {print $1, $3}' | sort -k2 -n >> "$summary_file"
        
        # Files with high coverage
        echo "" >> "$summary_file"
        echo "Files with High Coverage (>=90%):" >> "$summary_file"
        go tool cover -func="$combined_coverage_path" | awk '$3 >= 90 && NF == 3 {print $1, $3}' | sort -k2 -nr >> "$summary_file"
    else
        echo "Combined Test Coverage: Not available" >> "$summary_file"
    fi
    
    # Display summary
    cat "$summary_file"
    
    log_success "Coverage summary saved to: $summary_file"
}

# Function to display usage
usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  --unit-only          Run only unit tests with coverage"
    echo "  --integration-only   Run only integration tests with coverage"
    echo "  --no-html           Skip HTML report generation"
    echo "  --cleanup-only      Only cleanup test databases"
    echo "  --help              Show this help message"
    echo ""
    echo "Default: Run both unit and integration tests with full reports"
}

# Main function
main() {
    local run_unit=true
    local run_integration=true
    local generate_html=true
    local cleanup_only=false
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --unit-only)
                run_integration=false
                shift
                ;;
            --integration-only)
                run_unit=false
                shift
                ;;
            --no-html)
                generate_html=false
                shift
                ;;
            --cleanup-only)
                cleanup_only=true
                shift
                ;;
            --help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
    
    # Cleanup only mode
    if [[ "$cleanup_only" == true ]]; then
        cleanup_test_databases
        exit 0
    fi
    
    log_header "TMI Coverage Report Generator"
    
    # Setup cleanup on exit
    trap cleanup_test_databases EXIT
    
    # Check prerequisites
    check_prerequisites
    
    # Setup coverage directory
    setup_coverage_dir
    
    # Run tests based on options
    if [[ "$run_unit" == true ]]; then
        run_unit_tests
    fi
    
    if [[ "$run_integration" == true ]]; then
        run_integration_tests
    fi
    
    # Merge coverage profiles if both were run
    if [[ "$run_unit" == true && "$run_integration" == true ]]; then
        merge_coverage_profiles
    fi
    
    # Generate reports
    generate_text_reports
    
    if [[ "$generate_html" == true ]]; then
        generate_html_reports
    fi
    
    # Generate summary
    generate_summary_report
    
    log_header "Coverage Report Generation Complete"
    log_success "Coverage files are available in the '$COVERAGE_DIR' directory"
    
    if [[ "$generate_html" == true ]]; then
        log_success "HTML reports are available in the '$HTML_REPORT_DIR' directory"
    fi
}

# Run main function with all arguments
main "$@"