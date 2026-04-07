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

Core Targets:
  status                 - Check status of all services
  test-unit              - Run unit tests
  test-integration       - Run integration tests with PostgreSQL (default)
  test-integration-pg    - Run integration tests with PostgreSQL (Docker)
  test-integration-oci   - Run integration tests with Oracle ADB
  test-api               - Run comprehensive Postman/Newman API tests
  start-dev              - Start development environment
  start-dev-oci          - Start dev environment with OCI Autonomous Database
  reset-db-oci           - Drop all tables in OCI ADB (destructive)

CATS Fuzzing:
  cats-fuzz              - Run CATS API fuzzing (PostgreSQL)
  cats-fuzz-oci          - Run CATS API fuzzing (Oracle ADB)
    Variables: FUZZ_USER=alice FUZZ_SERVER=http://host ENDPOINT=/path BLACKBOX=true
  cats-seed              - Seed database for CATS (PostgreSQL)
  cats-seed-oci          - Seed database for CATS (Oracle ADB)
  analyze-cats-results   - Parse and query CATS results

Container Management:
  build-container-db           - Build PostgreSQL container
  build-container-redis        - Build Redis container
  build-container-tmi          - Build TMI server container
  build-container-oracle       - Build TMI container with Oracle ADB support
  build-container-oracle-push  - Build and push Oracle container to OCI
  build-container-redis-oracle - Build Redis container on Oracle Linux
  build-container-redis-oracle-push - Build and push Redis Oracle to OCI
  build-containers-oracle      - Build all Oracle Linux containers
  build-containers-oracle-push - Build and push all Oracle containers
  build-containers             - Build all containers (db, redis, tmi)
  scan-containers              - Scan containers for vulnerabilities
  report-containers            - Generate security report
  start-containers-environment - Start development with containers
  build-containers-all         - Build and report

Multi-Architecture Container Builds (amd64 + arm64):
  build-container-tmi-multiarch       - Build and push TMI server multi-arch image
  build-container-redis-multiarch     - Build and push Redis multi-arch image
  build-containers-multiarch          - Build and push all multi-arch images
  build-container-tmi-multiarch-local - Build TMI server for local platform only
  build-container-redis-multiarch-local - Build Redis for local platform only
  build-containers-multiarch-local    - Build all images for local platform only

OCI Functions (Certificate Manager):
  fn-build-certmgr             - Build the certificate manager function
  fn-deploy-certmgr            - Deploy certificate manager to OCI
  fn-invoke-certmgr            - Invoke certificate manager for testing
  fn-logs-certmgr              - View certificate manager logs

Terraform Infrastructure Management:
  tf-init                      - Initialize Terraform (TF_ENV=oci-public)
  tf-validate                  - Validate Terraform configuration
  tf-fmt                       - Format all Terraform files
  tf-plan                      - Plan infrastructure changes
  tf-apply                     - Apply infrastructure changes
  tf-apply-plan                - Apply from saved plan file
  tf-output                    - Show Terraform outputs
  tf-destroy                   - Destroy infrastructure (DESTRUCTIVE!)
  deploy-oci                   - Deploy TMI to OCI (infra + build + k8s)
  deploy-oci-plan              - Plan TMI OCI deployment (dry run)
  deploy-oci-skip-build        - Deploy TMI to OCI without rebuilding containers
  destroy-oci                  - Destroy TMI OCI infrastructure
  push-oci-info                - Show OCIR push info for external containers
  push-oci-env                 - Output OCIR registry env vars (eval-able)

SBOM Generation (Software Bill of Materials):
  generate-sbom                - Generate SBOM for Go application (cyclonedx-gomod)
    Variables: ALL=true to also generate module SBOMs
  build-with-sbom              - Build server and generate SBOM
  check-cyclonedx              - Verify cyclonedx-gomod is installed

Atomic Components (building blocks):
  start-database         - Start PostgreSQL container
  start-redis            - Start Redis container
  build-server           - Build server binary
  migrate-database       - Run database migrations
  reset-database         - Drop database and run migrations (DESTRUCTIVE)
  start-server           - Start server
  clean-everything       - Clean up everything

WebSocket Testing:
  build-wstest           - Build WebSocket test harness
  wstest                 - Run WebSocket test with 3 terminals
  monitor-wstest         - Run WebSocket monitor
  clean-wstest           - Stop all WebSocket test instances

Arazzo Workflow Generation:
  generate-arazzo        - Generate Arazzo workflow specifications
  validate-arazzo        - Validate generated Arazzo specifications
  arazzo-scaffold        - Generate base scaffold from OpenAPI
  arazzo-enhance         - Enhance scaffold with TMI workflow data
  arazzo-install         - Install Arazzo tooling

Validation Targets:
  validate-openapi       - Validate OpenAPI specification
  validate-asyncapi      - Validate AsyncAPI specification

Configuration Files:
  config/test-unit.yml           - Unit testing configuration
  config/test-integration.yml    - Integration testing configuration
  config/dev-environment.yml     - Development environment configuration
"""


def main() -> None:
    print(HELP_TEXT, end="")


if __name__ == "__main__":
    main()
