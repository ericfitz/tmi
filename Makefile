# TMI Refactored Makefile - Atomic Components with Configuration-Driven Composition
# This Makefile uses YAML configuration files and atomic components for maximum reusability.

.PHONY: help list-targets

# Set default target to help (must be before any includes that define targets)
.DEFAULT_GOAL := help

# Include integration test framework targets
-include scripts/test-framework.mk

# Use zsh as the shell with proper PATH
SHELL := /bin/zsh
.SHELLFLAGS := -c

# Export PATH to all submakes and shell recipes
export PATH := /usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:$(PATH)

# Default server port
SERVER_PORT ?= 8080

# ============================================================================
# ATOMIC COMPONENTS - Infrastructure Management
# ============================================================================

.PHONY: start-database stop-database clean-database start-redis stop-redis clean-redis start-db stop-db start-nats stop-nats clean-nats start-workers stop-workers

start-database:
	@uv run scripts/manage-database.py start

stop-database:
	@uv run scripts/manage-database.py stop

start-db: start-database

stop-db: stop-database

clean-database:
	@uv run scripts/manage-database.py clean

start-redis:
	@uv run scripts/manage-redis.py start

stop-redis:
	@uv run scripts/manage-redis.py stop

clean-redis:
	@uv run scripts/manage-redis.py clean

start-nats:  ## Start the NATS JetStream container for local dev
	@uv run scripts/manage-nats.py start

stop-nats:  ## Stop the NATS JetStream container
	@uv run scripts/manage-nats.py stop

clean-nats:  ## Remove the NATS JetStream container
	@uv run scripts/manage-nats.py clean

start-workers:  ## Start async-extraction worker processes (tmi-extractor, tmi-chunk-embed)
	@uv run scripts/manage-workers.py start

stop-workers:  ## Stop async-extraction worker processes
	@uv run scripts/manage-workers.py stop

# Test Infrastructure - Ephemeral containers for integration tests (isolated from dev)
.PHONY: start-test-database stop-test-database clean-test-database start-test-redis stop-test-redis clean-test-redis clean-test-infrastructure

start-test-database:
	@uv run scripts/manage-database.py --test start

stop-test-database:
	@uv run scripts/manage-database.py --test stop

clean-test-database:
	@uv run scripts/manage-database.py --test clean

start-test-redis:
	@uv run scripts/manage-redis.py --test start

stop-test-redis:
	@uv run scripts/manage-redis.py --test stop

clean-test-redis:
	@uv run scripts/manage-redis.py --test clean

clean-test-infrastructure: clean-test-database clean-test-redis


# ============================================================================
# ATOMIC COMPONENTS - Build Management
# ============================================================================

.PHONY: build-server build-migrate build-dbtool build-dbtool-oci build-worker-probe build-genconfig generate-config-example build-genconfigdocs generate-config-docs clean-build generate-api check-unsafe-union-methods check-missing-abort check-direct-http-client check-x-tmi-authz

build-server:
	@uv run scripts/build-server.py

build-migrate:
	@uv run scripts/build-server.py --component migrate

build-dbtool:  ## Build TMI database administration tool (database-agnostic)
	@uv run scripts/build-server.py --component dbtool

build-dbtool-oci:  ## Build TMI database administration tool with Oracle support (requires oci-env.sh)
	@uv run scripts/build-server.py --component dbtool --oci

build-worker-probe:  ## Build the worker-probe stub (proves the #415 worker bootstrap contract)
	@uv run scripts/build-server.py --component worker-probe

build-genconfig:  ## Build the config-example.yml generator
	@uv run scripts/build-server.py --component genconfig

generate-config-example: build-genconfig  ## Regenerate config-example.yml from the classification registry
	@./bin/genconfig

build-genconfigdocs:  ## Build the config-reference.md generator
	@uv run scripts/build-server.py --component genconfigdocs

generate-config-docs: build-genconfigdocs  ## Regenerate config-reference.md from the classification registry
	@./bin/genconfigdocs

clean-build:
	@uv run scripts/clean.py build

generate-api:
	@uv run scripts/generate-api.py

# Check that non-generated code doesn't use unsafe generated From*/Merge* methods
# that corrupt discriminator values (see api/cell_union_helpers.go for details)
check-unsafe-union-methods:
	@uv run scripts/check-unsafe-union-methods.py

# Check that c.JSON(non-2xx, ...) calls are followed by c.Abort() or return.
# See issue #264 — missing aborts let downstream handlers overwrite error
# status codes, masking 4xx/5xx responses as 200 in logs.
check-missing-abort:
	@uv run scripts/check-missing-abort.py

# Check that non-helper code in api/ does not construct its own outbound
# http.Client. All outbound HTTP must go through SafeHTTPClient
# (api/safe_http_client.go) so DNS pinning, SSRF blocklist, header timeout,
# and body cap are enforced uniformly. See issue #345 (T3/T26).
check-direct-http-client:
	@uv run scripts/check-direct-http-client.py

# Check that every OpenAPI operation under the covered prefix families carries
# x-tmi-authz. The prefix list grows per slice (#365–#370). Slice 8 (#371)
# removes the prefix list and enforces this on every operation.
check-x-tmi-authz:
	@uv run scripts/check-x-tmi-authz.py


# ============================================================================
# ATOMIC COMPONENTS - Database Operations
# ============================================================================

.PHONY: migrate-database check-database wait-database reset-database dedup-group-members

dedup-group-members:  ## Remove duplicate group_members rows (one-off, run before first migration with unique index)
	@uv run scripts/manage-database.py dedup --config config-development.yml

migrate-database:
	@uv run scripts/manage-database.py migrate

check-database:
	@uv run scripts/manage-database.py check

wait-database:
	@uv run scripts/manage-database.py wait

reset-database:
	@uv run scripts/manage-database.py reset

.PHONY: wait-test-database migrate-test-database

wait-test-database:
	@uv run scripts/manage-database.py --test --config config-test.yml wait

migrate-test-database:
	@uv run scripts/manage-database.py --config config-test.yml migrate

# ============================================================================
# ATOMIC COMPONENTS - Process Management
# ============================================================================

.PHONY: stop-process wait-process start-server start-service stop-server stop-service

stop-process:
	@uv run scripts/manage-server.py --port $(SERVER_PORT) kill-port

start-server:
	@uv run scripts/manage-server.py \
		$(if $(SERVER_CONFIG_FILE),--config $(SERVER_CONFIG_FILE),) \
		$(if $(SERVER_PORT),--port $(SERVER_PORT),) \
		$(if $(SERVER_BINARY),--binary $(SERVER_BINARY),) \
		$(if $(SERVER_LOG_FILE),--log-file $(SERVER_LOG_FILE),) \
		$(if $(SERVER_TAGS),--tags $(SERVER_TAGS),) \
		start

stop-server:
	@uv run scripts/manage-server.py \
		$(if $(SERVER_PORT),--port $(SERVER_PORT),) \
		stop

start-service: start-server

stop-service: stop-server

wait-process:
	@uv run scripts/manage-server.py \
		$(if $(SERVER_PORT),--port $(SERVER_PORT),) \
		$(if $(TIMEOUTS_SERVER_READY),--timeout $(TIMEOUTS_SERVER_READY),) \
		wait


# ============================================================================
# ATOMIC COMPONENTS - Cleanup Operations
# ============================================================================

.PHONY: clean-files clean-logs clean-containers clean-process clean-everything

clean-logs:
	@uv run scripts/clean.py logs

clean-files:
	@uv run scripts/clean.py files

clean-containers:
	@uv run scripts/clean.py containers

clean-process:
	@uv run scripts/clean.py process

clean-everything:
	@uv run scripts/clean.py all

# ============================================================================
# COMPOSITE TARGETS - Main User-Facing Commands
# ============================================================================

.PHONY: test-unit test-integration test-integration-pg test-integration-oci test-api test-api-collection test-api-list start-dev start-dev-oci restart-dev test-coverage test-manual-google-workspace test-corpus-ooxml test-dev-scripts

# Dev-environment Python helpers unit tests
test-dev-scripts:  ## Run unit tests for the dev-environment Python helpers
	@uv run --python ">=3.11" python -m unittest discover -s scripts/lib/tests -v

# Unit Testing - Fast tests with no external dependencies
# Output is summarized: failures show full verbose detail, passes show only counts.
# Raw verbose output is saved to a temp file referenced in the summary.
# Usage: make test-unit                     - Run all unit tests
#        make test-unit name=TestName       - Run specific test by name
#        make test-unit count1=true         - Run with --count=1
test-unit:
	@uv run scripts/run-unit-tests.py $(if $(name),--name $(name),) $(if $(filter true,$(count1)),--count1,)

# Integration Testing - Default to PostgreSQL backend
# Also available: test-integration-oci for Oracle ADB
test-integration: test-integration-pg

# Integration Testing - PostgreSQL backend (Docker container)
# Starts PostgreSQL, Redis, runs migrations, and executes integration tests
# Configuration: config-test.yml + TMI_DATABASE_URL for the PostgreSQL backend
# (run-integration-tests.py builds the URL from the local dev DB parameters)
# Usage: make test-integration-pg
# (Cleanup of dev containers is the responsibility of make stop-dev /
#  make clean-everything, not this target.)
test-integration-pg:
	@uv run scripts/run-integration-tests.py --target pg

# Integration Testing - Oracle ADB backend (OCI Autonomous Database)
# Requires Oracle Instant Client and wallet configuration
# Configuration: config-test.yml + TMI_DATABASE_URL=oracle://ADMIN@tmiadb_tp (set by scripts/oci-env.sh)
# Usage: make test-integration-oci                   - Run integration tests against OCI ADB
test-integration-oci:
	@uv run scripts/run-integration-tests.py --target oci

# API Testing - Comprehensive Postman/Newman test suite
# Response time multiplier for API tests (default: 1, use higher values for remote databases)
RESPONSE_TIME_MULTIPLIER ?= 1

# Usage: make test-api                          - Expect server running (default)
#        make test-api START_SERVER=true        - Auto-start server if needed
#        make test-api RESPONSE_TIME_MULTIPLIER=4 - Scale response time thresholds (e.g., for OCI)
#        make test-api-collection COLLECTION=name - Run specific collection
test-api:
	@uv run scripts/run-api-tests.py --response-time-multiplier $(RESPONSE_TIME_MULTIPLIER) $(if $(filter true,$(START_SERVER)),--start-server,)

# Run a specific Postman collection
# Usage: make test-api-collection COLLECTION=comprehensive-test-collection
#        make test-api-collection COLLECTION=unauthorized-tests-collection
test-api-collection:
	@uv run scripts/run-api-tests.py --collection $(COLLECTION) --response-time-multiplier $(RESPONSE_TIME_MULTIPLIER)

# List available Postman collections
test-api-list:
	@uv run scripts/run-api-tests.py --list

# Test Database Cleanup - Delete test users, groups, and CATS artifacts via admin API
# Requires: TMI server running (make start-dev), OAuth stub running (make start-oauth-stub)
# Usage: make test-db-cleanup              - Delete all test users, groups, and CATS artifacts
#        make test-db-cleanup ARGS="--dry-run"  - Preview what would be deleted
#        make test-db-cleanup ARGS="--cats-only" - Delete only CATS-seeded artifacts
test-db-cleanup:
	@uv run scripts/delete-test-users.py $(ARGS)

# Development Environment - Start local dev environment
start-dev:
	@uv run scripts/start-dev.py

# Development Environment - Oracle Cloud Infrastructure (OCI) Autonomous Database
# Prerequisites:
#   1. Oracle Instant Client installed
#   2. Wallet extracted to ./wallet directory
#   3. Database user created in OCI ADB
#   4. scripts/start-dev-oci.sh configured with your credentials (gitignored)
start-dev-oci:
	@./scripts/start-dev-oci.sh

# OCI ADB Utility - Drop all tables in OCI Autonomous Database
# Prerequisites: Same as start-dev-oci (Oracle Instant Client, wallet, credentials)
# WARNING: This is destructive and will delete all data in the OCI database
reset-db-oci:
	@./scripts/drop-oracle-tables.sh

# OCI ADB Diagnostic - Verify LOWER(CLOB) LIKE LOWER(?) works on Oracle ADB (#411)
# Prerequisites: Same as start-dev-oci (Oracle Instant Client, wallet, credentials)
# Creates a throwaway probe table, runs the exact TMI description-filter predicate,
# and drops the table. Set PROBE_KEEP=1 to leave the table for inspection.
probe-oracle-clob-like:
	@bash -c "source scripts/oci-env.sh && go run -tags oracle ./scripts/oracle-clob-like-probe/..."

# Development Environment - Restart (stop server, rebuild, clean logs, start dev)
restart-dev:
	@uv run scripts/start-dev.py --restart

# Coverage Report Generation - Comprehensive testing with coverage
test-coverage:
	@uv run scripts/run-coverage.py --full

# Manual Google Workspace Picker Test - Interactive test requiring real Google account
# See test/integration/manual/google_workspace_delegated_test.go for prerequisites.
# Usage: TMI_MANUAL_JWT=<token> TMI_MANUAL_THREAT_MODEL_ID=<uuid> make test-manual-google-workspace
test-manual-google-workspace: ## Run manual Google Workspace picker test (requires real Google account; see test for prerequisites)
	cd test/integration && go test -tags=manual -run TestGoogleWorkspaceDelegatedFlow -v ./manual/...

# Corpus Testing - Real-document regression tests for OOXML extractors
# Build-tagged "corpus"; corpus dir (testdata/ooxml-corpus/) must contain
# .docx/.pptx/.xlsx files with sibling .expected.md fixtures to run.
# If the directory is empty the test skips cleanly — scaffold only.
# Usage: make test-corpus-ooxml
test-corpus-ooxml: ## Run real-document OOXML extractor corpus tests
	@echo "Running OOXML corpus tests..."
	go test -tags=corpus ./api -run TestOOXMLCorpus -v


# ============================================================================
# SPECIALIZED ATOMIC COMPONENTS - Coverage
# ============================================================================

.PHONY: test-coverage-unit test-coverage-integration merge-coverage generate-coverage

test-coverage-unit:
	@uv run scripts/run-coverage.py --unit-only

test-coverage-integration:
	@uv run scripts/run-coverage.py --integration-only

merge-coverage:
	@uv run scripts/run-coverage.py --merge-only

generate-coverage:
	@uv run scripts/run-coverage.py --generate-only


# OAuth Stub - Development tool for OAuth callback testing
.PHONY: start-oauth-stub stop-oauth-stub kill-oauth-stub check-oauth-stub stop-all
start-oauth-stub:
	@uv run scripts/manage-oauth-stub.py start

stop-oauth-stub:
	@uv run scripts/manage-oauth-stub.py stop

kill-oauth-stub:
	@uv run scripts/manage-oauth-stub.py kill

check-oauth-stub:
	@uv run scripts/manage-oauth-stub.py status

stop-all: stop-server stop-workers stop-nats stop-database stop-redis stop-oauth-stub


# ============================================================================
# CATS FUZZING - API Security Testing
# ============================================================================

.PHONY: cats-seed cats-seed-oci cats-fuzz cats-fuzz-oci query-cats-results analyze-cats-results e2e-seed

CATS_CONFIG ?= config-development.yml
CATS_USER ?= charlie
CATS_PROVIDER ?= tmi
CATS_SERVER ?= http://localhost:8080

cats-seed:  ## Seed database for CATS fuzzing
	@uv run scripts/run-dbtool.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)

e2e-seed:  ## Seed database with E2E test data from tmi-ux seed-spec
	@E2E_SEED=$$(jq -r '."tmi-ux"' .local-projects.json 2>/dev/null)/e2e/seed/seed-spec.json; \
	if [ ! -f "$$E2E_SEED" ]; then echo "Error: seed-spec.json not found at $$E2E_SEED (check .local-projects.json)"; exit 1; fi; \
	uv run scripts/run-dbtool.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER) --input-file=$$E2E_SEED

cats-seed-oci:  ## Seed database for CATS fuzzing (Oracle ADB)
	@uv run scripts/run-dbtool.py --oci --user=$(CATS_USER) --provider=$(CATS_PROVIDER)

cats-fuzz: cats-seed  ## Run CATS API fuzzing (auto-parses results)
	@uv run scripts/run-cats-fuzz.py --skip-seed --user $(CATS_USER) --server $(CATS_SERVER) --config $(CATS_CONFIG) --provider $(CATS_PROVIDER) $(if $(FUZZ_USER),--user $(FUZZ_USER),) $(if $(FUZZ_SERVER),--server $(FUZZ_SERVER),) $(if $(ENDPOINT),--path $(ENDPOINT),) $(if $(filter true,$(BLACKBOX)),--blackbox,)

cats-fuzz-oci: cats-seed-oci  ## Run CATS API fuzzing with OCI ADB (auto-parses results)
	@uv run scripts/run-cats-fuzz.py --oci --skip-seed $(if $(FUZZ_USER),--user $(FUZZ_USER),) $(if $(FUZZ_SERVER),--server $(FUZZ_SERVER),) $(if $(ENDPOINT),--path $(ENDPOINT),) $(if $(filter true,$(BLACKBOX)),--blackbox,)

query-cats-results:  ## Query parsed CATS results
	@uv run scripts/query-cats-results.py --db test/outputs/cats/cats-results.db

analyze-cats-results: query-cats-results  ## Analyze CATS results


# ============================================================================
# CONTAINER SECURITY AND BUILD MANAGEMENT
# ============================================================================

.PHONY: build-app build-app-scan build-app-oci build-app-aws build-app-azure build-app-gcp build-app-heroku build-db build-db-scan build-server-container build-redis-container build-all build-all-scan scan-containers start-containers-environment

# ---- App Container Builds ----
build-app:  ## Build app containers for local development
	@uv run scripts/build-app-containers.py --target local

build-app-scan:  ## Build app containers locally with security scanning
	@uv run scripts/build-app-containers.py --target local --scan

build-app-oci:  ## Build and push app containers for OCI
	@uv run scripts/build-app-containers.py --target oci --push --scan

build-app-aws:  ## Build and push app containers for AWS
	@uv run scripts/build-app-containers.py --target aws --push --scan

build-app-azure:  ## Build and push app containers for Azure
	@uv run scripts/build-app-containers.py --target azure --push --scan

build-app-gcp:  ## Build and push app containers for GCP
	@uv run scripts/build-app-containers.py --target gcp --push --scan

build-app-heroku:  ## Build and push server container for Heroku
	@uv run scripts/build-app-containers.py --target heroku --component server --push

# ---- DB Container Builds ----
build-db:  ## Build database containers for local development
	@uv run scripts/build-db-containers.py --target local

build-db-scan:  ## Build database containers locally with security scanning
	@uv run scripts/build-db-containers.py --target local --scan

# ---- Individual Component Builds (convenience) ----
build-server-container:  ## Build only the TMI server container locally
	@uv run scripts/build-app-containers.py --target local --component server

build-redis-container:  ## Build only the Redis container locally
	@uv run scripts/build-app-containers.py --target local --component redis

# ---- Combined Builds ----
build-all: build-db build-app  ## Build all containers for local development

build-all-scan: build-db-scan build-app-scan  ## Build all containers with scanning

# ---- Scanning ----
scan-containers:  ## Scan existing container images for vulnerabilities
	@uv run scripts/build-app-containers.py --scan-only $(if $(TARGET),--target $(TARGET),)

# ---- Dev Environment ----
start-containers-environment: build-all  ## Build containers then start dev environment
	@$(MAKE) start-database
	@$(MAKE) start-redis

# ============================================================================
# OCI FUNCTIONS - Certificate Manager
# ============================================================================

.PHONY: fn-build-certmgr fn-deploy-certmgr fn-invoke-certmgr fn-logs-certmgr

fn-build-certmgr:  ## Build the certificate manager OCI function
	@uv run scripts/manage-oci-functions.py build

fn-deploy-certmgr:  ## Deploy certificate manager function to OCI
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) deploy

fn-invoke-certmgr:  ## Invoke certificate manager function manually
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) invoke

fn-logs-certmgr:  ## View certificate manager function logs
	@uv run scripts/manage-oci-functions.py --app $(FN_APP) logs

# ============================================================================
# TERRAFORM INFRASTRUCTURE MANAGEMENT
# ============================================================================

TF_ENV ?= oci-public

.PHONY: tf-init tf-plan tf-apply tf-apply-plan tf-validate tf-fmt tf-output tf-destroy

tf-init:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) init

tf-plan:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) plan

tf-apply:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) $(if $(AUTO_APPROVE),--auto-approve,) apply

tf-apply-plan:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) --from-plan apply

tf-validate:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) validate

tf-fmt:
	@uv run scripts/manage-terraform.py fmt

tf-output:
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) output

tf-destroy:  ## Destroy Terraform infrastructure (DESTRUCTIVE!)
	@uv run scripts/manage-terraform.py --environment $(TF_ENV) $(if $(AUTO_APPROVE),--auto-approve,) destroy

# OCI-specific deployment shortcuts
.PHONY: deploy-oci deploy-oci-plan deploy-oci-skip-build destroy-oci push-oci-info push-oci-env

deploy-oci:  ## Deploy TMI to OCI (two-phase: infra, build containers, then K8s resources)
	@scripts/deploy-oci.sh $(if $(AUTO_APPROVE),--auto-approve,)

deploy-oci-plan:  ## Plan TMI OCI deployment (dry run)
	@scripts/deploy-oci.sh --dry-run

deploy-oci-skip-build:  ## Deploy TMI to OCI without rebuilding containers
	@scripts/deploy-oci.sh --skip-build $(if $(AUTO_APPROVE),--auto-approve,)

destroy-oci:  ## Destroy TMI OCI infrastructure (DESTRUCTIVE!)
	@scripts/deploy-oci.sh --destroy $(if $(AUTO_APPROVE),--auto-approve,)

push-oci-info:  ## Show OCIR push instructions for external containers (tmi-ux)
	@scripts/deploy-oci.sh --push-info

push-oci-env:  ## Output OCIR registry info as env vars (use: eval $$(make push-oci-env))
	@scripts/deploy-oci.sh --push-env


# ============================================================================
# BACKWARD COMPATIBILITY ALIASES
# ============================================================================

.PHONY: build build-containers test lint clean dev

# Keep backward compatibility with existing commands
build: build-server
build-containers: build-all
test: test-unit
lint:
	@uv run scripts/lint.py
clean: clean-everything
dev: start-dev

# ============================================================================
# Heroku Configuration
# ============================================================================

.PHONY: setup-heroku setup-heroku-dry-run

setup-heroku: ## Configure Heroku environment variables interactively
	@uv run scripts/setup-heroku-env.py

setup-heroku-dry-run: ## Preview Heroku configuration without applying
	@uv run scripts/setup-heroku-env.py --dry-run

.PHONY: reset-db-heroku drop-db-heroku
reset-db-heroku: ## Drop and recreate Heroku database schema (DESTRUCTIVE - deletes all data). Use ARGS="--yes" to skip confirmation
	@./scripts/heroku-reset-database.sh $(ARGS) tmi-server

drop-db-heroku: ## Drop Heroku database schema leaving it empty (DESTRUCTIVE - deletes all data, no migrations). Use ARGS="--yes" to skip confirmation
	@./scripts/heroku-drop-database.sh $(ARGS) tmi-server

# ============================================================================
# Heroku Deployment
# ============================================================================

# Deploy to Heroku production
# This target builds the server, commits changes, and deploys to Heroku
deploy-heroku:
	@uv run scripts/deploy-heroku.py


# ============================================================================
# AWS Deployment
# ============================================================================

.PHONY: deploy-aws deploy-aws-dry-run destroy-aws

deploy-aws: ## Deploy TMI to AWS (EKS + RDS + Secrets Manager). Use ARGS for options (e.g., ARGS="--domain tmi.example.com --zone-id Z123")
	@./scripts/deploy-aws.sh $(ARGS)

deploy-aws-dry-run: ## Preview AWS deployment changes without applying
	@./scripts/deploy-aws.sh --dry-run $(ARGS)

destroy-aws: ## Destroy TMI AWS deployment (DESTRUCTIVE - removes all AWS resources)
	@./scripts/deploy-aws.sh --destroy $(ARGS)

# ============================================================================
# WEBSOCKET TEST HARNESS
# ============================================================================

.PHONY: wstest monitor-wstest

wstest:
	@uv run scripts/run-wstest.py

monitor-wstest:
	@uv run scripts/run-wstest.py --monitor

# ============================================================================
# SBOM GENERATION - Software Bill of Materials
# ============================================================================

.PHONY: generate-sbom build-with-sbom

# Generate SBOM for Go application only
# Use ALL=true to also generate module SBOMs: make generate-sbom ALL=true
generate-sbom:
	@uv run scripts/generate-sbom.py $(if $(filter true,$(ALL)),--all,)

# Build server with SBOM
build-with-sbom: build-server generate-sbom

# ============================================================================
# VALIDATION TARGETS
# ============================================================================

.PHONY: validate-openapi parse-openapi-validation validate-asyncapi arazzo-install arazzo-scaffold arazzo-enhance generate-arazzo validate-arazzo

# ============================================================================
# ARAZZO WORKFLOW GENERATION
# ============================================================================

arazzo-install:
	@uv run scripts/manage-arazzo.py install

arazzo-scaffold: arazzo-install
	@uv run scripts/manage-arazzo.py scaffold

arazzo-enhance:
	@uv run scripts/manage-arazzo.py enhance

generate-arazzo:
	@uv run scripts/manage-arazzo.py generate

validate-arazzo:
	@uv run scripts/validate-arazzo.py api-schema/tmi.arazzo.yaml api-schema/tmi.arazzo.json

# ============================================================================
# OPENAPI/ASYNCAPI VALIDATION
# ============================================================================

OPENAPI_SPEC := api-schema/tmi-openapi.json
OPENAPI_VALIDATION_REPORT := test/outputs/api-validation/openapi-validation-report.json
OPENAPI_VALIDATION_DB := test/outputs/api-validation/openapi-validation.db
ASYNCAPI_SPEC := api-schema/tmi-asyncapi.yml
ASYNCAPI_VALIDATION_REPORT := test/outputs/api-validation/asyncapi-validation-report.json

validate-openapi: check-x-tmi-authz
	@uv run scripts/validate-openapi-spec.py --spec $(OPENAPI_SPEC) --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB)

parse-openapi-validation:
	@uv run scripts/parse-openapi-validation.py --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB) --summary

validate-asyncapi:
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC) --format json --output $(ASYNCAPI_VALIDATION_REPORT)
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC)

# ============================================================================
# STATUS CHECKING
# ============================================================================

.PHONY: status

status:
	@uv run scripts/status.py

# ============================================================================
# HELP AND UTILITIES
# ============================================================================

help:
	@uv run scripts/help.py

list-targets:
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {print $$1}' | grep -v '^Makefile$$' | sort

## --- TMI Component Platform (Plan 1: foundation) ---

GOPATH_BIN := $(shell go env GOPATH)/bin

.PHONY: build-component-controller generate-platform-crd test-platform e2e-platform-up e2e-platform-down test-e2e-platform

build-component-controller:  ## Build the component-controller binary
	go build -o bin/component-controller ./cmd/component-controller/

generate-platform-crd:  ## Regenerate TMIComponent DeepCopy methods and CRD YAML
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.17.3
	$(GOPATH_BIN)/controller-gen object paths=./api/platform/v1alpha1/...
	$(GOPATH_BIN)/controller-gen crd paths=./api/platform/v1alpha1/... output:crd:dir=config/crd/bases

test-platform:  ## Run platform controller unit tests (downloads envtest assets if needed)
	go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest
	@if [ -f $(CURDIR)/bin/k8s/1.30.0-darwin-arm64/kube-apiserver ] || [ -f $(CURDIR)/bin/k8s/1.30.0-linux-amd64/kube-apiserver ] || [ -f $(CURDIR)/bin/k8s/1.30.0-linux-arm64/kube-apiserver ]; then \
		ASSETS=$$(ls -d $(CURDIR)/bin/k8s/1.30.0-* 2>/dev/null | head -1); \
		echo "Using cached envtest binaries at $$ASSETS"; \
		KUBEBUILDER_ASSETS="$$ASSETS" go test ./internal/platform/... ./api/platform/...; \
	else \
		KUBEBUILDER_ASSETS="$$($(GOPATH_BIN)/setup-envtest use 1.30.0 --bin-dir $(CURDIR)/bin/k8s -p path)" \
		go test ./internal/platform/... ./api/platform/...; \
	fi

e2e-platform-up:  ## Create the kind cluster and install platform dependencies (NATS, KEDA, CRD)
	kind create cluster --config deployments/k8s/platform/kind-cluster.yml
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/calico.yml
	kubectl --context kind-tmi-platform wait --for=condition=Ready nodes --all --timeout=180s
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/nats.yml
	kubectl --context kind-tmi-platform apply --server-side -f deployments/k8s/platform/keda.yml
	kubectl --context kind-tmi-platform apply -f config/crd/bases/tmi.dev_tmicomponents.yaml

e2e-platform-down:  ## Delete the kind platform cluster
	kind delete cluster --name tmi-platform

test-e2e-platform:  ## Run the platform e2e tests (requires e2e-platform-up + controller deployed)
	go test -tags e2e ./test/e2e/platform/ -v

.PHONY: build-extractor build-chunkembed build-workers test-workers \
        stage-worker-docker-deps build-extractor-container build-chunkembed-container

build-extractor:  ## Build the tmi-extractor worker binary
	go build -o bin/tmi-extractor ./cmd/extractor/

build-chunkembed:  ## Build the tmi-chunk-embed worker binary
	go build -o bin/tmi-chunk-embed ./cmd/chunkembed/

build-workers: build-extractor build-chunkembed  ## Build both worker binaries

test-workers:  ## Run worker + extract + envelope + async-extraction tests (starts a NATS JetStream container)
	@docker run -d --rm --name tmi-nats-test -p 4222:4222 nats:2.10-alpine -js >/dev/null
	@sleep 2
	@TMI_RUN_NATS_TESTS=1 TMI_TEST_NATS_URL=nats://127.0.0.1:4222 \
		go test -p 1 ./internal/worker/... ./internal/platform/controller/... \
		./pkg/extract/... ./pkg/jobenvelope/... \
		./cmd/extractor/... ./cmd/chunkembed/... ./api/...; \
		rc=$$?; docker stop tmi-nats-test >/dev/null; exit $$rc

# Stage the tmi-client Go module into .docker-deps/tmi-client/ so the worker
# Dockerfiles can COPY it (the repo's go.mod uses a relative replace directive
# that a naive `COPY . .` build cannot resolve). The source path is derived
# from the replace directive in go.mod itself, then resolved relative to the
# repo root, so it works from a worktree as well as the main checkout.
stage-worker-docker-deps:  ## Stage tmi-client dependency for worker container builds
	@rel=$$(sed -n 's|.*=> \(\.\./tmi-clients/go-client-generated/[^ ]*\).*|\1|p' go.mod); \
	if [ -z "$$rel" ]; then \
		echo "go.mod has no tmi-clients replace directive; nothing to stage"; \
		exit 0; \
	fi; \
	src=$$(cd "$$(dirname go.mod)" && cd "$$rel" 2>/dev/null && pwd); \
	if [ -z "$$src" ] || [ ! -d "$$src" ]; then \
		echo "ERROR: tmi-client source not found via go.mod replace path '$$rel'"; \
		echo "       Ensure the tmi-clients repo is checked out as a sibling of the repo root."; \
		exit 1; \
	fi; \
	rm -rf .docker-deps/tmi-client; \
	mkdir -p .docker-deps; \
	cp -R "$$src" .docker-deps/tmi-client; \
	echo "Staged tmi-client dependency: $$src -> .docker-deps/tmi-client"

# Each container build stages the tmi-client dependency itself (as a recipe
# step, NOT a shared prerequisite) so that building both in one `make`
# invocation works: a prerequisite is satisfied only once per run, but each
# recipe ends by removing .docker-deps, so a shared prerequisite would leave
# the second build without its staged dependency. Staging per-recipe keeps
# each target independent whether run alone or together.
build-extractor-container:  ## Build the tmi-extractor container image
	$(MAKE) stage-worker-docker-deps
	docker build -f Dockerfile.extractor -t tmi-extractor:dev .
	@rm -rf .docker-deps

build-chunkembed-container:  ## Build the tmi-chunk-embed container image
	$(MAKE) stage-worker-docker-deps
	docker build -f Dockerfile.chunkembed -t tmi-chunk-embed:dev .
	@rm -rf .docker-deps

.PHONY: test-e2e-workers

test-e2e-workers:  ## Build worker images, load into kind, deploy CRs, run the worker e2e
	@echo ">> assumes 'make e2e-platform-up' has run and the controller is deployed"
	$(MAKE) build-extractor-container build-chunkembed-container
	kind load docker-image tmi-extractor:dev --name tmi-platform
	kind load docker-image tmi-chunk-embed:dev --name tmi-platform
	kubectl --context kind-tmi-platform -n tmi-platform create secret generic tmi-embedding \
		--from-literal=api-key=$${TMI_EMBEDDING_API_KEY:-sk-e2e-placeholder} \
		--dry-run=client -o yaml | kubectl --context kind-tmi-platform apply -f -
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/components/
	@echo ">> port-forwarding NATS to localhost:4222 for the test"
	kubectl --context kind-tmi-platform -n tmi-platform port-forward svc/nats 4222:4222 & \
		PF_PID=$$!; \
		for i in $$(seq 1 60); do \
			nc -z 127.0.0.1 4222 2>/dev/null && break; \
			if ! kill -0 $$PF_PID 2>/dev/null; then \
				echo "ERROR: port-forward exited before NATS became reachable" >&2; \
				exit 1; \
			fi; \
			sleep 0.5; \
		done; \
		if ! nc -z 127.0.0.1 4222 2>/dev/null; then \
			echo "ERROR: NATS not reachable on localhost:4222 after 30s" >&2; \
			kill $$PF_PID 2>/dev/null; exit 1; \
		fi; \
		go test -tags e2e ./test/e2e/platform/ -run TestWorkersE2E -v; \
		rc=$$?; kill $$PF_PID 2>/dev/null; exit $$rc

.PHONY: test-e2e-build load-probe-image test-e2e-acceptance

test-e2e-build:  ## Compile the e2e tests without running them (fast tag-build check)
	go test -tags e2e -run xxxNONExxx -count=1 ./test/e2e/platform/ -o /dev/null -c

load-probe-image:  ## Pull busybox and load it into kind for the egress/OOM probe pods
	docker pull busybox:1.36
	kind load docker-image busybox:1.36 --name tmi-platform

test-e2e-acceptance: load-probe-image  ## Run the #347 Plan 4 acceptance suite (requires e2e-platform-up + controller + workers deployed)
	@echo ">> assumes 'make e2e-platform-up', the controller, and 'make test-e2e-workers' (CRs deployed) have run"
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/components/
	@echo ">> port-forwarding NATS to localhost:4222 for the acceptance tests"
	kubectl --context kind-tmi-platform -n tmi-platform port-forward svc/nats 4222:4222 & \
		PF_PID=$$!; \
		for i in $$(seq 1 60); do \
			nc -z 127.0.0.1 4222 2>/dev/null && break; \
			if ! kill -0 $$PF_PID 2>/dev/null; then \
				echo "ERROR: port-forward exited before NATS became reachable" >&2; \
				exit 1; \
			fi; \
			sleep 0.5; \
		done; \
		if ! nc -z 127.0.0.1 4222 2>/dev/null; then \
			echo "ERROR: NATS not reachable on localhost:4222 after 30s" >&2; \
			kill $$PF_PID 2>/dev/null; exit 1; \
		fi; \
		go test -tags e2e ./test/e2e/platform/ -run TestAcceptance -v -timeout 20m; \
		rc=$$?; kill $$PF_PID 2>/dev/null; exit $$rc
