# /// script
# requires-python = ">=3.11"
# ///
"""Print help text for available Make targets.

Usage:
    uv run scripts/help.py
"""

HELP_TEXT = """\
TMI Makefile

Usage: make <target> [VARIABLE=value ...]

Development Environment:
  start-dev              - Start local dev environment (PostgreSQL + Redis + server)
  start-dev-oci          - Start dev environment with OCI Autonomous Database
  restart-dev            - Stop server, rebuild, clean logs, restart dev
  status                 - Check status of all services
  start-oauth-stub       - Start OAuth callback stub for testing
  stop-oauth-stub        - Stop OAuth callback stub
  check-oauth-stub       - Check OAuth stub status

Building:
  build-server           - Build server binary (bin/tmiserver)
  build-migrate          - Build migration binary (bin/migrate)
  build-dbtool           - Build database administration tool (bin/tmi-dbtool)
  build-dbtool-oci       - Build dbtool with Oracle support (requires oci-env.sh)
  generate-api           - Regenerate API code from OpenAPI spec (oapi-codegen)
  lint                   - Run golangci-lint
  check-unsafe-union-methods - Check for unsafe discriminator methods in non-generated code

Container Builds (Local):
  build-server-container - Build TMI server container
  build-redis-container  - Build Redis container
  build-db               - Build PostgreSQL container
  build-app              - Build server + Redis containers
  build-all              - Build all containers (db + server + redis)
  build-containers       - Alias for build-all
  build-app-scan         - Build app containers with security scanning
  build-db-scan          - Build database container with security scanning
  build-all-scan         - Build all containers with scanning
  scan-containers        - Scan existing container images for vulnerabilities
  start-containers-environment - Build all containers then start database + Redis

Container Builds (Cloud):
  build-app-oci          - Build and push app containers for OCI
  build-app-aws          - Build and push app containers for AWS
  build-app-azure        - Build and push app containers for Azure
  build-app-gcp          - Build and push app containers for GCP
  build-app-heroku       - Build and push server container for Heroku

Testing:
  test-unit              - Run unit tests
    Variables: name=TestName count1=true
  test-integration       - Run integration tests with PostgreSQL (default)
  test-integration-pg    - Run integration tests with PostgreSQL (Docker)
    Variables: CLEANUP=true to stop server and clean containers after
  test-integration-oci   - Run integration tests with Oracle ADB
    Variables: CLEANUP=true to stop server and clean Redis after
  test-api               - Run Postman/Newman API tests
    Variables: START_SERVER=true RESPONSE_TIME_MULTIPLIER=4
  test-api-collection    - Run specific Postman collection
    Variables: COLLECTION=name RESPONSE_TIME_MULTIPLIER=4
  test-api-list          - List available Postman collections
  test-db-cleanup        - Delete test users, groups, and CATS artifacts
    Variables: ARGS="--dry-run" ARGS="--cats-only"
  test-help              - Show integration test framework help

Integration Test Framework (Tier-Based):
  test-integration-tier1 - Tier 1: Core workflows (< 2 min, CI/CD)
  test-integration-tier2 - Tier 2: Feature tests (< 10 min, nightly)
  test-integration-tier3 - Tier 3: Edge cases & admin (< 15 min, weekly)
  test-integration-all   - Run all tiers (< 27 min, 100% coverage)
  test-integration-new   - Run all integration tests (new framework)
  test-integration-quick - Run quick example test
  test-integration-full  - Full setup + run all tests
  test-integration-workflow - Run specific workflow
    Variables: WORKFLOW=name
  check-test-framework   - Validate integration test framework setup

Coverage:
  test-coverage          - Full coverage report (unit + integration)
  test-coverage-unit     - Unit test coverage only
  test-coverage-integration - Integration test coverage only
  merge-coverage         - Merge coverage profiles
  generate-coverage      - Generate coverage report from merged profiles

CATS API Fuzzing:
  cats-fuzz              - Run CATS API fuzzing (seeds + fuzzes + parses results)
  cats-fuzz-oci          - Run CATS API fuzzing with Oracle ADB
    Variables: FUZZ_USER=alice FUZZ_SERVER=http://host ENDPOINT=/path BLACKBOX=true
  cats-seed              - Seed database for CATS (PostgreSQL)
  cats-seed-oci          - Seed database for CATS (Oracle ADB)
  analyze-cats-results   - Parse and query CATS results
  query-cats-results     - Query parsed CATS results database

SBOM Generation:
  generate-sbom          - Generate SBOM for Go application (cyclonedx-gomod)
    Variables: ALL=true to also generate module SBOMs
  build-with-sbom        - Build server and generate SBOM

Validation:
  validate-openapi       - Validate OpenAPI specification
  parse-openapi-validation - Parse OpenAPI validation report
  validate-asyncapi      - Validate AsyncAPI specification

Arazzo Workflow Generation:
  generate-arazzo        - Generate Arazzo workflow specifications
  validate-arazzo        - Validate generated Arazzo specifications
  arazzo-scaffold        - Generate base scaffold from OpenAPI
  arazzo-enhance         - Enhance scaffold with TMI workflow data
  arazzo-install         - Install Arazzo tooling

WebSocket Testing:
  wstest                 - Run WebSocket test with 3 terminals
  monitor-wstest         - Run WebSocket monitor

Infrastructure Management:
  start-database         - Start PostgreSQL container
  stop-database          - Stop PostgreSQL container
  clean-database         - Remove PostgreSQL container and data
  start-redis            - Start Redis container
  stop-redis             - Stop Redis container
  clean-redis            - Remove Redis container and data
  migrate-database       - Run database migrations
  check-database         - Check database connectivity
  wait-database          - Wait for database to be ready
  reset-database         - Drop database and run migrations (DESTRUCTIVE)
  dedup-group-members    - Remove duplicate group_members rows

Test Infrastructure:
  start-test-database    - Start test PostgreSQL container (isolated)
  stop-test-database     - Stop test PostgreSQL container
  clean-test-database    - Remove test PostgreSQL container
  start-test-redis       - Start test Redis container (isolated)
  stop-test-redis        - Stop test Redis container
  clean-test-redis       - Remove test Redis container
  clean-test-infrastructure - Clean all test containers
  clean-test-outputs     - Clean all test output files

Process Management:
  start-server           - Start TMI server
  stop-server            - Stop TMI server
  stop-process           - Kill process on server port
  wait-process           - Wait for server to be ready

OCI Functions (Certificate Manager):
  fn-build-certmgr      - Build the certificate manager function
  fn-deploy-certmgr     - Deploy certificate manager to OCI
  fn-invoke-certmgr     - Invoke certificate manager for testing
  fn-logs-certmgr       - View certificate manager logs

Terraform:
  tf-init                - Initialize Terraform (TF_ENV=oci-public)
  tf-validate            - Validate Terraform configuration
  tf-fmt                 - Format all Terraform files
  tf-plan                - Plan infrastructure changes
  tf-apply               - Apply infrastructure changes
  tf-apply-plan          - Apply from saved plan file
  tf-output              - Show Terraform outputs
  tf-destroy             - Destroy infrastructure (DESTRUCTIVE!)

Deployment:
  deploy-oci             - Deploy TMI to OCI (infra + build + K8s)
  deploy-oci-plan        - Plan TMI OCI deployment (dry run)
  deploy-oci-skip-build  - Deploy TMI to OCI without rebuilding containers
  destroy-oci            - Destroy TMI OCI infrastructure (DESTRUCTIVE!)
  push-oci-info          - Show OCIR push info for external containers
  push-oci-env           - Output OCIR registry env vars (eval-able)
  deploy-heroku          - Deploy TMI to Heroku
  setup-heroku           - Configure Heroku environment variables
  setup-heroku-dry-run   - Preview Heroku configuration without applying
  reset-db-heroku        - Drop and recreate Heroku database (DESTRUCTIVE)
  drop-db-heroku         - Drop Heroku database schema (DESTRUCTIVE)
  reset-db-oci           - Drop all tables in OCI ADB (DESTRUCTIVE)
  deploy-aws             - Deploy TMI to AWS (EKS + RDS + Secrets Manager)
  deploy-aws-dry-run     - Preview AWS deployment changes
  destroy-aws            - Destroy TMI AWS deployment (DESTRUCTIVE!)

Cleanup:
  clean-build            - Clean build artifacts
  clean-logs             - Clean log files
  clean-files            - Clean build artifacts, logs, and temp files
  clean-containers       - Stop and remove dev containers
  clean-process          - Stop server and OAuth stub processes
  clean-everything       - Stop everything, clean all artifacts and containers

Backward Compatibility Aliases:
  build                  - Alias for build-server
  build-containers       - Alias for build-all
  test                   - Alias for test-unit
  clean                  - Alias for clean-everything
  dev                    - Alias for start-dev

Utilities:
  help                   - Show this help message
  list-targets           - List all available Make targets
"""


def main() -> None:
    print(HELP_TEXT, end="")


if __name__ == "__main__":
    main()
