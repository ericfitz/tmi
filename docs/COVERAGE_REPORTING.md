# Test Coverage Reporting

This document describes how to generate comprehensive test coverage reports for the TMI project, including both unit test coverage and integration test coverage.

## Overview

The coverage reporting system provides:

- **Unit Test Coverage**: Tests individual functions and modules in isolation
- **Integration Test Coverage**: Tests complete workflows with real databases
- **Combined Coverage**: Merged coverage from both unit and integration tests
- **Multiple Report Formats**: HTML (visual) and text (detailed) reports

## Quick Start

### Generate Full Coverage Report

To generate a comprehensive coverage report including both unit and integration tests:

```bash
make report-coverage
```

This will:
1. Run all unit tests with coverage tracking
2. Set up test databases (PostgreSQL and Redis)
3. Run integration tests with coverage tracking
4. Merge coverage profiles
5. Generate HTML and text reports
6. Create a summary report
7. Clean up test databases

### Generate Unit Tests Only

For faster feedback during development:

```bash
make report-coverage-unit
```

### Generate Integration Tests Only

To test database interactions and full workflows:

```bash
make report-coverage-integration
```

### Generate Reports Without HTML

For CI/CD environments where HTML is not needed:

```bash
make generate-coverage-report
```

## Script Options

The coverage script (`scripts/coverage-report.sh`) supports several options:

```bash
./scripts/coverage-report.sh [OPTIONS]

Options:
  --unit-only          Run only unit tests with coverage
  --integration-only   Run only integration tests with coverage
  --no-html           Skip HTML report generation
  --cleanup-only      Only cleanup test databases
  --help              Show help message
```

## Output Files

Coverage reports are generated in two directories:

### Coverage Directory (`coverage/`)

- `unit_coverage.out` - Raw unit test coverage data
- `integration_coverage.out` - Raw integration test coverage data
- `combined_coverage.out` - Merged coverage data
- `unit_coverage_detailed.txt` - Detailed unit test coverage by function
- `integration_coverage_detailed.txt` - Detailed integration test coverage
- `combined_coverage_detailed.txt` - Detailed combined coverage
- `coverage_summary.txt` - Executive summary with key metrics

### HTML Reports Directory (`coverage_html/`)

- `unit_coverage.html` - Interactive unit test coverage report
- `integration_coverage.html` - Interactive integration test coverage report
- `combined_coverage.html` - Interactive combined coverage report

## Understanding Coverage Reports

### Summary Report

The summary report (`coverage/coverage_summary.txt`) includes:

- Overall coverage percentages
- Top files by coverage
- Files with low coverage (<50%)
- Files with high coverage (>=90%)

### HTML Reports

HTML reports provide:
- Visual coverage highlighting (green = covered, red = not covered)
- Function-by-function coverage breakdown
- Interactive browsing of source code
- Coverage statistics per file

### Detailed Text Reports

Text reports show:
- Coverage percentage per function
- Line-by-line coverage information
- Easy-to-parse format for automation

## Coverage Thresholds

### Current Coverage Levels

The project maintains these coverage targets:

- **Unit Tests**: Target 80%+ coverage for core business logic
- **Integration Tests**: Target 70%+ coverage for API endpoints and workflows
- **Combined**: Target 75%+ overall coverage

### Key Areas of Focus

High priority areas for coverage:

1. **API Handlers** - All HTTP endpoints should be tested
2. **Business Logic** - Core threat modeling functionality
3. **Authentication & Authorization** - Security-critical code
4. **Database Operations** - Data persistence and retrieval
5. **Cache Management** - Performance-critical caching logic

## Prerequisites

The coverage script requires:

- Go 1.19 or later
- Docker (for integration tests)
- `gocovmerge` tool (automatically installed)

## Troubleshooting

### Common Issues

#### Docker Not Available
If Docker is not running, integration tests will fail:
```bash
# Start Docker on macOS
open -a Docker

# Verify Docker is running
docker info
```

#### Database Connection Issues
If test databases fail to start:
```bash
# Clean up any existing containers
make clean-all

# Or manually clean up
docker stop tmi-integration-postgres tmi-integration-redis
docker rm tmi-integration-postgres tmi-integration-redis
```

#### Coverage Tool Missing
If `gocovmerge` is not available:
```bash
go install github.com/wadey/gocovmerge@latest
```

### Test Database Configuration

Integration tests use these database settings:

- **PostgreSQL**: localhost:5434 (user: tmi_integration, db: tmi_integration_test)
- **Redis**: localhost:6381

These ports are chosen to avoid conflicts with development databases.

## CI/CD Integration

### GitHub Actions Example

```yaml
- name: Generate Coverage Report
  run: |
    make generate-coverage-report
    
- name: Upload Coverage
  uses: actions/upload-artifact@v3
  with:
    name: coverage-report
    path: |
      coverage/
      coverage_html/
```

### Coverage Badges

Generate coverage badges using the summary data:

```bash
# Extract combined coverage percentage
COVERAGE=$(grep "Combined Test Coverage:" coverage/coverage_summary.txt | awk '{print $4}' | tr -d '%')
echo "Coverage: ${COVERAGE}%"
```

## Best Practices

### Writing Testable Code

1. **Dependency Injection**: Use interfaces for external dependencies
2. **Small Functions**: Keep functions focused and testable
3. **Error Handling**: Test both success and error paths
4. **Mock External Services**: Don't depend on external APIs in tests

### Coverage Goals

1. **New Code**: All new code should include tests
2. **Bug Fixes**: Add tests that reproduce the bug first
3. **Refactoring**: Maintain or improve coverage during refactoring
4. **Critical Paths**: Ensure 100% coverage for security-critical code

## Monitoring Coverage

### Regular Reports

Generate coverage reports:
- Daily in CI/CD pipeline
- Before each release
- After significant feature additions

### Coverage Trends

Track coverage over time:
- Monitor increases/decreases in coverage
- Identify files with declining coverage
- Set team goals for coverage improvements

## Advanced Usage

### Custom Coverage Profiles

Run tests with custom coverage settings:

```bash
# Test specific packages
go test -coverprofile=custom.out ./api/...

# Test with race detection
go test -race -coverprofile=race.out ./...

# Generate HTML from custom profile
go tool cover -html=custom.out -o custom.html
```

### Coverage Analysis

Analyze coverage data programmatically:

```bash
# Find functions with zero coverage
go tool cover -func=combined_coverage.out | awk '$3 == "0.0%" {print $1}'

# Show files sorted by coverage
go tool cover -func=combined_coverage.out | sort -k3 -n
```