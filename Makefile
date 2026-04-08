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

# Default build target
VERSION := 0.9.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "development")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Colors for output
BLUE := \033[0;34m
GREEN := \033[0;32m
YELLOW := \033[1;33m
RED := \033[0;31m
NC := \033[0m

# Logging functions
define log_info
	@echo -e "$(BLUE)[INFO]$(NC) $(1)"
endef

define log_success
	@echo -e "$(GREEN)[SUCCESS]$(NC) $(1)"
endef

define log_warning
	@echo -e "$(YELLOW)[WARNING]$(NC) $(1)"
endef

define log_error
	@echo -e "$(RED)[ERROR]$(NC) $(1)"
endef

# ============================================================================
# REUSABLE MACROS
# ============================================================================

# Kill all processes on a port: SIGTERM all, wait, SIGKILL survivors
# Usage: @$(call kill_port,PORT_NUMBER)
define kill_port
PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
if [ -n "$$PIDS" ]; then \
	for PID in $$PIDS; do \
		kill $$PID 2>/dev/null || true; \
	done; \
	sleep 1; \
	PIDS=$$(lsof -ti :$(1) 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		for PID in $$PIDS; do \
			kill -9 $$PID 2>/dev/null || true; \
		done; \
	fi; \
fi
endef

# ============================================================================
# ATOMIC COMPONENTS - Infrastructure Management
# ============================================================================

.PHONY: start-database stop-database clean-database start-redis stop-redis clean-redis

start-database:
	@uv run scripts/manage-database.py start

stop-database:
	@uv run scripts/manage-database.py stop

clean-database:
	@uv run scripts/manage-database.py clean

start-redis:
	@uv run scripts/manage-redis.py start

stop-redis:
	@uv run scripts/manage-redis.py stop

clean-redis:
	@uv run scripts/manage-redis.py clean

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

.PHONY: build-server build-migrate build-cats-seed build-cats-seed-oci clean-build generate-api check-unsafe-union-methods

build-server:
	@uv run scripts/build-server.py

build-migrate:
	@uv run scripts/build-server.py --component migrate

build-cats-seed:  ## Build CATS database seeding tool (database-agnostic)
	@uv run scripts/build-server.py --component cats-seed

build-cats-seed-oci:  ## Build CATS database seeding tool with Oracle support (requires oci-env.sh)
	@uv run scripts/build-server.py --component cats-seed --oci

clean-build:
	$(call log_info,"Cleaning build artifacts...")
	@rm -rf ./bin/*
	@rm -f migrate
	$(call log_success,"Build artifacts cleaned")

generate-api:
	@uv run scripts/generate-api.py

# Check that non-generated code doesn't use unsafe generated From*/Merge* methods
# that corrupt discriminator values (see api/cell_union_helpers.go for details)
check-unsafe-union-methods:
	@uv run scripts/check-unsafe-union-methods.py


# ============================================================================
# ATOMIC COMPONENTS - Database Operations
# ============================================================================

.PHONY: migrate-database check-database wait-database reset-database dedup-group-members

dedup-group-members:  ## Remove duplicate group_members rows (one-off, run before first migration with unique index)
	@uv run scripts/manage-database.py dedup --config config-development.yml

migrate-database:
	@uv run scripts/manage-database.py migrate

check-database:
	$(call log_info,"Checking database schema...")
	@cd cmd/migrate && go run main.go --config ../../config-development.yml --validate

wait-database:
	@uv run scripts/manage-database.py wait

reset-database:
	@uv run scripts/manage-database.py reset

.PHONY: wait-test-database migrate-test-database

wait-test-database:
	@uv run scripts/manage-database.py --test --config config-test-integration-pg.yml wait

migrate-test-database:
	@uv run scripts/manage-database.py --config config-test-integration-pg.yml migrate

# ============================================================================
# ATOMIC COMPONENTS - Process Management
# ============================================================================

.PHONY: stop-process wait-process start-server start-service stop-server stop-service

stop-process:
	$(call log_info,"Killing processes on port $(SERVER_PORT)")
	@$(call kill_port,$(SERVER_PORT))

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
	$(call log_info,"Cleaning up containers...")
	@if [ -n "$(CLEANUP_CONTAINERS)" ] && [ "$(CLEANUP_CONTAINERS)" != "" ]; then \
		for container in $(CLEANUP_CONTAINERS); do \
			echo -e "$(BLUE)[INFO]$(NC) Stopping and removing container: $$container"; \
			docker stop $$container 2>/dev/null || true; \
			docker rm $$container 2>/dev/null || true; \
		done; \
	fi
	$(call log_success,"Container cleanup completed")

clean-process:
	@uv run scripts/clean.py process

clean-everything:
	@uv run scripts/clean.py all

# ============================================================================
# COMPOSITE TARGETS - Main User-Facing Commands
# ============================================================================

.PHONY: test-unit test-integration test-integration-pg test-integration-oci test-api test-api-collection test-api-list start-dev start-dev-oci restart-dev test-coverage

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
# Configuration: config-test-integration-pg.yml
# Usage: make test-integration-pg                    - Leave server running (default)
#        make test-integration-pg CLEANUP=true       - Stop server and clean containers
test-integration-pg:
	@if [ "$(CLEANUP)" = "true" ]; then \
		./scripts/run-integration-tests-pg.sh --cleanup; \
	else \
		./scripts/run-integration-tests-pg.sh; \
	fi

# Integration Testing - Oracle ADB backend (OCI Autonomous Database)
# Requires Oracle Instant Client and wallet configuration
# Configuration: config-test-integration-oci.yml
# Usage: make test-integration-oci                   - Leave server running (default)
#        make test-integration-oci CLEANUP=true      - Stop server and clean Redis
test-integration-oci:
	@if [ "$(CLEANUP)" = "true" ]; then \
		./scripts/run-integration-tests-oci.sh --cleanup; \
	else \
		./scripts/run-integration-tests-oci.sh; \
	fi

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
	$(call log_info,"Cleaning up test users / groups / CATS artifacts via admin API")
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

# Development Environment - Restart (stop server, rebuild, clean logs, start dev)
restart-dev:
	@uv run scripts/start-dev.py --restart

# Coverage Report Generation - Comprehensive testing with coverage
test-coverage:
	@uv run scripts/run-coverage.py --full


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
.PHONY: start-oauth-stub stop-oauth-stub kill-oauth-stub check-oauth-stub
start-oauth-stub:
	@uv run scripts/manage-oauth-stub.py start

stop-oauth-stub:
	@uv run scripts/manage-oauth-stub.py stop

kill-oauth-stub:
	@uv run scripts/manage-oauth-stub.py kill

check-oauth-stub:
	@uv run scripts/manage-oauth-stub.py status


# ============================================================================
# CATS FUZZING - API Security Testing
# ============================================================================

.PHONY: cats-seed cats-seed-oci cats-fuzz cats-fuzz-oci query-cats-results analyze-cats-results

CATS_CONFIG ?= config-development.yml
CATS_USER ?= charlie
CATS_PROVIDER ?= tmi
CATS_SERVER ?= http://localhost:8080

cats-seed:  ## Seed database for CATS fuzzing
	@uv run scripts/run-cats-seed.py --config=$(CATS_CONFIG) --user=$(CATS_USER) --provider=$(CATS_PROVIDER) --server=$(CATS_SERVER)

cats-seed-oci:  ## Seed database for CATS fuzzing (Oracle ADB)
	@uv run scripts/run-cats-seed.py --oci --user=$(CATS_USER) --provider=$(CATS_PROVIDER)

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

# ---- Backward Compatibility (deprecated - will be removed) ----
.PHONY: build-container-db build-container-redis build-container-tmi build-containers build-containers-all build-container-oracle build-containers-oracle-push containers-dev report-containers
build-container-db: build-db
build-container-redis: build-redis-container
build-container-tmi: build-server-container
build-containers: build-all
build-containers-all: build-all-scan
build-container-oracle: build-app-oci
build-containers-oracle-push: build-app-oci
containers-dev: start-containers-environment
report-containers: scan-containers

# ============================================================================
# OCI FUNCTIONS - Certificate Manager
# ============================================================================

.PHONY: fn-check fn-build-certmgr fn-deploy-certmgr fn-invoke-certmgr fn-logs-certmgr

# Check if Fn CLI is installed
fn-check:
	@command -v fn >/dev/null 2>&1 || { \
		echo -e "$(RED)[ERROR]$(NC) Fn CLI is not installed."; \
		echo -e "$(BLUE)[INFO]$(NC) Install with: brew install fn"; \
		exit 1; \
	}

# Build the certificate manager function
fn-build-certmgr: fn-check  ## Build the certificate manager OCI function
	$(call log_info,Building certificate manager function...)
	@cd functions/certmgr && fn build
	$(call log_success,Certificate manager function built successfully)

# Deploy the certificate manager function to OCI
fn-deploy-certmgr: fn-check  ## Deploy certificate manager function to OCI (requires OCI config)
	$(call log_info,Deploying certificate manager function...)
	@if [ -z "$(FN_APP)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) FN_APP environment variable not set."; \
		echo -e "$(BLUE)[INFO]$(NC) Set FN_APP to the OCI Function Application name"; \
		exit 1; \
	fi
	@cd functions/certmgr && fn deploy --app $(FN_APP)
	$(call log_success,Certificate manager function deployed)

# Invoke the certificate manager function manually (for testing)
fn-invoke-certmgr: fn-check  ## Invoke certificate manager function manually for testing
	$(call log_info,Invoking certificate manager function...)
	@if [ -z "$(FN_APP)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) FN_APP environment variable not set."; \
		exit 1; \
	fi
	@fn invoke $(FN_APP) certmgr
	$(call log_success,Function invoked)

# View certificate manager function logs
fn-logs-certmgr: fn-check  ## View certificate manager function logs
	$(call log_info,Fetching certificate manager function logs...)
	@if [ -z "$(FN_APP)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) FN_APP environment variable not set."; \
		exit 1; \
	fi
	@fn logs $(FN_APP) certmgr

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

.PHONY: build test lint clean dev

# Keep backward compatibility with existing commands
build: build-server
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
	$(call log_info,"Starting Heroku environment configuration...")
	@uv run scripts/setup-heroku-env.py

setup-heroku-dry-run: ## Preview Heroku configuration without applying
	$(call log_info,"Previewing Heroku configuration (dry-run mode)...")
	@uv run scripts/setup-heroku-env.py --dry-run

.PHONY: reset-db-heroku drop-db-heroku
reset-db-heroku: ## Drop and recreate Heroku database schema (DESTRUCTIVE - deletes all data). Use ARGS="--yes" to skip confirmation
	$(call log_warning,"This will DELETE ALL DATA in the Heroku database!")
	@./scripts/heroku-reset-database.sh $(ARGS) tmi-server

drop-db-heroku: ## Drop Heroku database schema leaving it empty (DESTRUCTIVE - deletes all data, no migrations). Use ARGS="--yes" to skip confirmation
	$(call log_warning,"This will DELETE ALL DATA in the Heroku database and leave it EMPTY!")
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
	$(call log_warning,"This will DESTROY all TMI resources in AWS!")
	@./scripts/deploy-aws.sh --destroy $(ARGS)

# ============================================================================
# WEBSOCKET TEST HARNESS
# ============================================================================

.PHONY: build-wstest wstest monitor-wstest clean-wstest

build-wstest:
	$(call log_info,Building WebSocket test harness...)
	@cd wstest && go mod tidy && go build -o wstest
	$(call log_success,WebSocket test harness built successfully)

wstest: build-wstest
	@uv run scripts/run-wstest.py

monitor-wstest: build-wstest
	$(call log_info,Starting WebSocket monitor...)
	@# Check if server is running
	@if ! curl -s http://localhost:8080 > /dev/null 2>&1; then \
		echo -e "$(RED)[ERROR]$(NC) Server not running. Please run 'make start-dev' first"; \
		exit 1; \
	fi
	@# Run monitor in foreground
	@cd wstest && ./wstest --user monitor

clean-wstest:
	$(call log_info,Stopping all WebSocket test harness instances...)
	@# Kill all wstest processes
	@if pgrep -f "wstest" > /dev/null 2>&1; then \
		pkill -f "wstest" && \
		echo -e "$(GREEN)[SUCCESS]$(NC) All WebSocket test harness instances stopped"; \
	else \
		echo -e "$(YELLOW)[WARNING]$(NC) No WebSocket test harness instances found"; \
	fi
	@# Clean up any log files
	@rm -f wstest/*.log 2>/dev/null || true

# ============================================================================
# SBOM GENERATION - Software Bill of Materials
# ============================================================================

.PHONY: check-cyclonedx check-grype generate-sbom build-with-sbom

# Check for cyclonedx-gomod (Go components)
check-cyclonedx:
	@if ! command -v cyclonedx-gomod >/dev/null 2>&1; then \
		$(call log_error,cyclonedx-gomod not found); \
		echo ""; \
		$(call log_info,Install using:); \
		echo "  Homebrew: brew install cyclonedx/cyclonedx/cyclonedx-gomod"; \
		echo "  Go:       go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest"; \
		exit 1; \
	fi
	@$(call log_success,cyclonedx-gomod is available)

# Check for Grype (container vulnerability scanning)
check-grype:
	@if ! command -v grype >/dev/null 2>&1; then \
		$(call log_error,Grype not found); \
		echo ""; \
		$(call log_info,Install using:); \
		echo "  Homebrew: brew install grype"; \
		echo "  Script:   curl -sSfL https://raw.githubusercontent.com/anchore/grype/main/install.sh | sh -s -- -b /usr/local/bin"; \
		exit 1; \
	fi
	@$(call log_success,Grype is available)

# Generate SBOM for Go application only
# Use ALL=true to also generate module SBOMs: make generate-sbom ALL=true
generate-sbom:
	@uv run scripts/generate-sbom.py $(if $(filter true,$(ALL)),--all,)

# Build server with SBOM
build-with-sbom: build-server generate-sbom

# ============================================================================
# VALIDATION TARGETS
# ============================================================================

.PHONY: validate-openapi parse-openapi-validation validate-asyncapi scan-openapi-security arazzo-install arazzo-scaffold arazzo-enhance generate-arazzo validate-arazzo

# ============================================================================
# ARAZZO WORKFLOW GENERATION
# ============================================================================

arazzo-install:
	$(call log_info,Installing Arazzo tooling...)
	@pnpm install
	$(call log_success,Arazzo tools installed)

arazzo-scaffold: arazzo-install
	$(call log_info,Generating base scaffold with Redocly CLI...)
	@bash scripts/generate-arazzo-scaffold.sh
	$(call log_success,Base scaffold generated)

arazzo-enhance:
	$(call log_info,Enhancing with TMI workflow data...)
	@uv run scripts/enhance-arazzo-with-workflows.py
	$(call log_success,"Enhanced Arazzo created at api-schema/tmi.arazzo.yaml and .json")

validate-arazzo:
	$(call log_info,Validating Arazzo specifications...)
	@uv run scripts/validate-arazzo.py \
		api-schema/tmi.arazzo.yaml \
		api-schema/tmi.arazzo.json
	$(call log_success,Arazzo specifications are valid)

generate-arazzo: arazzo-scaffold arazzo-enhance validate-arazzo
	$(call log_success,Arazzo specification generation complete)

# ============================================================================
# OPENAPI/ASYNCAPI VALIDATION
# ============================================================================

OPENAPI_SPEC := api-schema/tmi-openapi.json
OPENAPI_VALIDATION_REPORT := test/outputs/api-validation/openapi-validation-report.json
OPENAPI_VALIDATION_DB := test/outputs/api-validation/openapi-validation.db
ASYNCAPI_SPEC := api-schema/tmi-asyncapi.yml
ASYNCAPI_VALIDATION_REPORT := test/outputs/api-validation/asyncapi-validation-report.json

validate-openapi:
	@uv run scripts/validate-openapi-spec.py --spec $(OPENAPI_SPEC) --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB)

parse-openapi-validation:
	$(call log_info,Parsing OpenAPI validation report into SQLite database...)
	@uv run scripts/parse-openapi-validation.py --report $(OPENAPI_VALIDATION_REPORT) --db $(OPENAPI_VALIDATION_DB) --summary
	$(call log_success,Validation results loaded into: $(OPENAPI_VALIDATION_DB))

validate-asyncapi:
	$(call log_info,Validating AsyncAPI specification with Spectral...)
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC) --format json --output $(ASYNCAPI_VALIDATION_REPORT)
	@uv run scripts/validate-asyncapi.py $(ASYNCAPI_SPEC)
	$(call log_success,AsyncAPI validation complete. Report: $(ASYNCAPI_VALIDATION_REPORT))

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