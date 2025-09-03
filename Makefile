# TMI Refactored Makefile - Atomic Components with Configuration-Driven Composition
# This Makefile uses YAML configuration files and atomic components for maximum reusability.

.PHONY: help list-targets

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

# Configuration loading function
define load-config
	$(eval CONFIG_FILE := config/$(1).yml)
	$(eval include scripts/load-config.mk)
endef

# Helper target to load configuration file
.PHONY: load-config-file
load-config-file:
	@if [ -n "$(CONFIG_FILE)" ]; then \
		echo "Loading configuration from $(CONFIG_FILE)"; \
		include scripts/load-config.mk; \
	fi

# ============================================================================
# ATOMIC COMPONENTS - Infrastructure Management
# ============================================================================

.PHONY: infra-db-start infra-db-stop infra-db-clean infra-redis-start infra-redis-stop infra-redis-clean

infra-db-start:
	$(call log_info,Starting PostgreSQL container...)
	@CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	PORT="$(INFRASTRUCTURE_POSTGRES_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="5432"; fi; \
	USER="$(INFRASTRUCTURE_POSTGRES_USER)"; \
	if [ -z "$$USER" ]; then USER="tmi_dev"; fi; \
	PASSWORD="$(INFRASTRUCTURE_POSTGRES_PASSWORD)"; \
	if [ -z "$$PASSWORD" ]; then PASSWORD="dev123"; fi; \
	DATABASE="$(INFRASTRUCTURE_POSTGRES_DATABASE)"; \
	if [ -z "$$DATABASE" ]; then DATABASE="tmi_dev"; fi; \
	IMAGE="$(INFRASTRUCTURE_POSTGRES_IMAGE)"; \
	if [ -z "$$IMAGE" ]; then IMAGE="postgres:14-alpine"; fi; \
	if ! docker ps -a --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Creating new PostgreSQL container..."; \
		docker run -d \
			--name $$CONTAINER \
			-p $$PORT:5432 \
			-e POSTGRES_USER=$$USER \
			-e POSTGRES_PASSWORD=$$PASSWORD \
			-e POSTGRES_DB=$$DATABASE \
			$$IMAGE; \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Starting existing PostgreSQL container..."; \
		docker start $$CONTAINER; \
	fi; \
	echo "✅ PostgreSQL container is running on port $$PORT"

infra-db-stop:
	$(call log_info,Stopping PostgreSQL container: $(INFRASTRUCTURE_POSTGRES_CONTAINER))
	@docker stop $(INFRASTRUCTURE_POSTGRES_CONTAINER) 2>/dev/null || true
	$(call log_success,"PostgreSQL container stopped")

infra-db-clean:
	$(call log_warning,"Removing PostgreSQL container and data: $(INFRASTRUCTURE_POSTGRES_CONTAINER)")
	@docker rm -f $(INFRASTRUCTURE_POSTGRES_CONTAINER) 2>/dev/null || true
	$(call log_success,"PostgreSQL container and data removed")

infra-redis-start:
	$(call log_info,Starting Redis container...)
	@CONTAINER="$(INFRASTRUCTURE_REDIS_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-redis"; fi; \
	PORT="$(INFRASTRUCTURE_REDIS_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="6379"; fi; \
	IMAGE="$(INFRASTRUCTURE_REDIS_IMAGE)"; \
	if [ -z "$$IMAGE" ]; then IMAGE="redis:7-alpine"; fi; \
	if ! docker ps -a --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Creating new Redis container..."; \
		docker run -d \
			--name $$CONTAINER \
			-p $$PORT:6379 \
			$$IMAGE; \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Starting existing Redis container..."; \
		docker start $$CONTAINER; \
	fi; \
	echo "✅ Redis container is running on port $$PORT"

infra-redis-stop:
	$(call log_info,Stopping Redis container: $(INFRASTRUCTURE_REDIS_CONTAINER))
	@docker stop $(INFRASTRUCTURE_REDIS_CONTAINER) 2>/dev/null || true
	$(call log_success,"Redis container stopped")

infra-redis-clean:
	$(call log_warning,"Removing Redis container and data: $(INFRASTRUCTURE_REDIS_CONTAINER)")
	@docker rm -f $(INFRASTRUCTURE_REDIS_CONTAINER) 2>/dev/null || true
	$(call log_success,"Redis container and data removed")


# ============================================================================
# ATOMIC COMPONENTS - Build Management
# ============================================================================

.PHONY: build-server build-migrate build-clean generate-api gen-api

build-server:
	$(call log_info,Building server binary...)
	@go build -tags="dev" -o bin/server github.com/ericfitz/tmi/cmd/server
	$(call log_success,"Server binary built: bin/server")

build-migrate:
	$(call log_info,Building migration tool...)
	@go build -o bin/migrate github.com/ericfitz/tmi/cmd/migrate
	$(call log_success,"Migration tool built: bin/migrate")

build-clean:
	$(call log_info,"Cleaning build artifacts...")
	@rm -rf ./bin/*
	@rm -f check-db migrate
	$(call log_success,"Build artifacts cleaned")

generate-api:
	$(call log_info,"Generating API code from OpenAPI specification...")
	@oapi-codegen -config oapi-codegen-config.yml shared/api-specs/tmi-openapi.json
	$(call log_success,"API code generated: api/api.go")

# Legacy alias for generate-api
gen-api: generate-api

# ============================================================================
# ATOMIC COMPONENTS - Database Operations
# ============================================================================

.PHONY: db-migrate db-check db-wait

db-migrate:
	$(call log_info,"Running database migrations...")
	@if [ -f "./bin/migrate" ]; then \
		./bin/migrate up; \
	elif [ -f "./migrate" ]; then \
		./migrate up; \
	else \
		cd cmd/migrate && go run main.go up; \
	fi
	$(call log_success,"Database migrations completed")

db-check:
	$(call log_info,"Checking database migration status...")
	@if [ -f "./bin/check-db" ]; then \
		./bin/check-db; \
	elif [ -f "./check-db" ]; then \
		./check-db; \
	else \
		cd cmd/check-db && go run main.go; \
	fi

db-wait:
	$(call log_info,"Waiting for database to be ready...")
	@timeout=$${TIMEOUTS_DB_READY:-30}; \
	CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	USER="$(INFRASTRUCTURE_POSTGRES_USER)"; \
	if [ -z "$$USER" ]; then USER="tmi_dev"; fi; \
	while [ $$timeout -gt 0 ]; do \
		if docker exec $$CONTAINER pg_isready -U $$USER >/dev/null 2>&1; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) Database is ready!"; \
			break; \
		fi; \
		echo "⏳ Waiting for database... ($$timeout seconds remaining)"; \
		sleep 2; \
		timeout=$$((timeout - 2)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		echo -e "$(RED)[ERROR]$(NC) Database failed to start within 30 seconds"; \
		exit 1; \
	fi

# ============================================================================
# ATOMIC COMPONENTS - Process Management
# ============================================================================

.PHONY: process-stop process-wait server-start server-stop

process-stop:
	$(call log_info,"Killing processes on port $(SERVER_PORT)")
	@PORT="$(SERVER_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="8080"; fi; \
	PIDS=$$(lsof -ti :$$PORT 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo "Found processes on port $$PORT: $$PIDS"; \
		for PID in $$PIDS; do \
			echo "Stopping process $$PID listening on port $$PORT..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Force killing process $$PID..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
		echo "All processes on port $$PORT have been killed"; \
	else \
		echo "No processes found listening on port $$PORT"; \
	fi

server-start:
	$(call log_info,"Starting server on port $(SERVER_PORT)")
	@LOG_FILE="$(SERVER_LOG_FILE)"; \
	if [ -z "$$LOG_FILE" ]; then LOG_FILE="logs/server.log"; fi; \
	CONFIG_FILE="$(SERVER_CONFIG_FILE)"; \
	if [ -z "$$CONFIG_FILE" ]; then CONFIG_FILE="config-development.yml"; fi; \
	BINARY="$(SERVER_BINARY)"; \
	if [ -z "$$BINARY" ]; then BINARY="bin/server"; fi; \
	if [ -n "$(SERVER_TAGS)" ]; then \
		echo "Starting server with build tags: $(SERVER_TAGS)"; \
		go run -tags $(SERVER_TAGS) cmd/server/main.go --config=$$CONFIG_FILE > $$LOG_FILE 2>&1 & \
	else \
		echo "Starting server binary: $$BINARY"; \
		$$BINARY --config=$$CONFIG_FILE > $$LOG_FILE 2>&1 & \
	fi; \
	echo $$! > .server.pid
	$(call log_success,"Server started with PID: $$(cat .server.pid)")

server-stop:
	$(call log_info,"Stopping server...")
	@if [ -f .server.pid ]; then \
		PID=$$(cat .server.pid); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "Stopping server (PID: $$PID)..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 2; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Force killing server (PID: $$PID)..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		fi; \
		rm -f .server.pid; \
	fi
	@$(MAKE) process-stop
	$(call log_success,"Server stopped")

process-wait:
	$(call log_info,"Waiting for server to be ready on port $(SERVER_PORT)")
	@timeout=$${TIMEOUTS_SERVER_READY:-30}; \
	PORT="$(SERVER_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="8080"; fi; \
	while [ $$timeout -gt 0 ]; do \
		if curl -s http://localhost:$$PORT/ >/dev/null 2>&1; then \
			$(call log_success,"Server is ready!"); \
			break; \
		fi; \
		echo "⏳ Waiting for server... ($$timeout seconds remaining)"; \
		sleep 2; \
		timeout=$$((timeout - 2)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		$(call log_error,"Server failed to start within 30 seconds"); \
		exit 1; \
	fi


# ============================================================================
# ATOMIC COMPONENTS - Test Execution
# ============================================================================

.PHONY: test-unit-execute test-integration-execute

test-unit-execute:
	$(call log_info,"Executing unit tests...")
	@if [ -n "$(TEST_PATTERN)" ] && [ "$(TEST_PATTERN)" != "" ]; then \
		echo "Running specific unit test: $(TEST_PATTERN)"; \
		TEST_CMD="$(ENVIRONMENT_TMI_LOGGING_IS_TEST)=true go test -short ./... -run $(TEST_PATTERN) -v"; \
	else \
		echo "Running all unit tests..."; \
		TEST_CMD="$(ENVIRONMENT_TMI_LOGGING_IS_TEST)=true go test -short ./..."; \
	fi; \
	if [ "$(TEST_COUNT1)" = "true" ]; then \
		TEST_CMD="$$TEST_CMD --count=1"; \
	fi; \
	if [ "$(TEST_TAGS)" != "" ]; then \
		TEST_CMD="$$TEST_CMD -tags='$(TEST_TAGS)'"; \
	fi; \
	if [ "$(TEST_TIMEOUT)" != "" ]; then \
		TEST_CMD="$$TEST_CMD -timeout=$(TEST_TIMEOUT)"; \
	fi; \
	eval $$TEST_CMD
	$(call log_success,"Unit tests completed")

test-integration-execute:
	$(call log_info,"Executing integration tests...")
	@TEST_EXIT_CODE=0; \
	$(ENVIRONMENT_TMI_LOGGING_IS_TEST)=true \
	TEST_DB_HOST=localhost \
	TEST_DB_PORT=$(INFRASTRUCTURE_POSTGRES_PORT) \
	TEST_DB_USER=$(INFRASTRUCTURE_POSTGRES_USER) \
	TEST_DB_PASSWORD=$(INFRASTRUCTURE_POSTGRES_PASSWORD) \
	TEST_DB_NAME=$(INFRASTRUCTURE_POSTGRES_DATABASE) \
	TEST_REDIS_HOST=localhost \
	TEST_REDIS_PORT=$(INFRASTRUCTURE_REDIS_PORT) \
	TEST_SERVER_URL=http://localhost:$(SERVER_PORT) \
		go test -v -timeout=$(TEST_TIMEOUT) $(TEST_PACKAGES) -run "$(TEST_PATTERN)" \
		| tee integration-test.log \
		|| TEST_EXIT_CODE=$$?; \
	if [ $$TEST_EXIT_CODE -eq 0 ]; then \
		$(call log_success,"Integration tests completed successfully"); \
	else \
		$(call log_error,"Integration tests failed with exit code $$TEST_EXIT_CODE"); \
		echo ""; \
		$(call log_info,"Failed test summary:"); \
		grep -E "FAIL:|--- FAIL" integration-test.log || true; \
		exit $$TEST_EXIT_CODE; \
	fi

# ============================================================================
# ATOMIC COMPONENTS - Cleanup Operations
# ============================================================================

.PHONY: clean-files clean-containers clean-processes clean-all

clean-files:
	$(call log_info,"Cleaning up files...")
	@if [ -n "$(CLEANUP_FILES)" ] && [ "$(CLEANUP_FILES)" != "" ]; then \
		for file in $(CLEANUP_FILES); do \
			if [ -f "$$file" ]; then \
				echo -e "$(BLUE)[INFO]$(NC) Removing file: $$file"; \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	@if [ -n "$(ARTIFACTS_LOG_FILES)" ] && [ "$(ARTIFACTS_LOG_FILES)" != "" ]; then \
		for file in $(ARTIFACTS_LOG_FILES); do \
			if [ -f "$$file" ]; then \
				echo -e "$(BLUE)[INFO]$(NC) Removing log file: $$file"; \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	@if [ -n "$(ARTIFACTS_PID_FILES)" ] && [ "$(ARTIFACTS_PID_FILES)" != "" ]; then \
		for file in $(ARTIFACTS_PID_FILES); do \
			if [ -f "$$file" ]; then \
				echo -e "$(BLUE)[INFO]$(NC) Removing PID file: $$file"; \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	$(call log_success,"File cleanup completed")

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

clean-processes:
	$(call log_info,"Cleaning up processes...")
	@if [ -n "$(CLEANUP_PROCESSES)" ] && [ "$(CLEANUP_PROCESSES)" != "" ]; then \
		for port in $(CLEANUP_PROCESSES); do \
			echo -e "$(BLUE)[INFO]$(NC) Killing processes on port: $$port"; \
			PIDS=$$(lsof -ti :$$port 2>/dev/null || true); \
			if [ -n "$$PIDS" ]; then \
				for PID in $$PIDS; do \
					kill $$PID 2>/dev/null || true; \
					sleep 1; \
					if ps -p $$PID > /dev/null 2>&1; then \
						kill -9 $$PID 2>/dev/null || true; \
					fi; \
				done; \
			fi; \
		done; \
	fi
	$(call log_success,"Process cleanup completed")

clean-all: clean-processes clean-containers clean-files

# ============================================================================
# COMPOSITE TARGETS - Main User-Facing Commands
# ============================================================================

.PHONY: test-unit test-integration dev-start dev-clean test-coverage

# Unit Testing - Fast tests with no external dependencies
test-unit:
	@CONFIG_FILE=config/test-unit.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting unit tests..."; \
	TMI_LOGGING_IS_TEST=true go test -short ./... -v; \
	rm -f .config.tmp.mk integration-test.log server.log logs/server.log .server.pid

# Integration Testing - Full environment with database and server
test-integration:
	@CONFIG_FILE=config/test-integration.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting integration tests with configuration: Integration Testing Configuration"; \
	trap 'CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all' EXIT; \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) infra-db-start && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) infra-redis-start && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) db-wait && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) build-server && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) db-migrate && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) server-start && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) process-wait && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) test-integration-execute

# Development Environment - Start local dev environment
dev-start:
	@CONFIG_FILE=config/dev-environment.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting development environment: Development Environment Configuration"; \
	trap 'CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all' EXIT; \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) infra-db-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) infra-redis-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) db-wait && \
	go build -o bin/check-db cmd/check-db/main.go && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) db-migrate && \
	eval $$(uv run scripts/yaml-to-make.py config/dev-environment.yml | grep '^SERVER_CONFIG_FILE := ' | sed 's/SERVER_CONFIG_FILE := /SERVER_CONFIG_FILE=/'); \
	if [ ! -f "$$SERVER_CONFIG_FILE" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Generating development configuration..."; \
		go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }; \
	fi && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) server-start
	@eval $$(uv run scripts/yaml-to-make.py config/dev-environment.yml | grep '^SERVER_PORT := ' | sed 's/SERVER_PORT := /SERVER_PORT=/'); \
	echo -e "$(GREEN)[SUCCESS]$(NC) Development environment started on port $$SERVER_PORT"

# Development Environment Cleanup
dev-clean:
	@CONFIG_FILE=config/dev-environment.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Cleaning development environment: Development Environment Configuration"; \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all

# Coverage Report Generation - Comprehensive testing with coverage
test-coverage:
	@CONFIG_FILE=config/coverage-report.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Generating coverage reports: Coverage Report Configuration"; \
	trap 'CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all' EXIT; \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all && \
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^COVERAGE_DIRECTORY := ' | sed 's/:=/=/'); \
	mkdir -p $$COVERAGE_DIRECTORY && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) test-coverage-unit && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) infra-db-start && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) infra-redis-start && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) db-wait && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) db-migrate && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) test-coverage-integration && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) coverage-merge && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) coverage-reports


# ============================================================================
# SPECIALIZED ATOMIC COMPONENTS - Coverage
# ============================================================================

.PHONY: test-coverage-unit test-coverage-integration coverage-merge coverage-reports

test-coverage-unit:
	$(call log_info,"Running unit tests with coverage...")
	@$(ENVIRONMENT_TMI_LOGGING_IS_TEST)=true go test \
		-coverprofile="$(COVERAGE_DIRECTORY)/$(COVERAGE_UNIT_PROFILE)" \
		-covermode=$(COVERAGE_MODE) \
		-coverpkg=./... \
		$(TEST_UNIT_PACKAGES) \
		-tags="$(TEST_UNIT_TAGS)" \
		-timeout=$(TEST_UNIT_TIMEOUT) \
		-v
	$(call log_success,"Unit test coverage completed")

test-coverage-integration:
	$(call log_info,"Running integration tests with coverage...")
	@$(ENVIRONMENT_TMI_LOGGING_IS_TEST)=true \
	TMI_POSTGRES_HOST=localhost \
	TMI_POSTGRES_PORT=$(INFRASTRUCTURE_POSTGRES_PORT) \
	TMI_POSTGRES_USER=$(INFRASTRUCTURE_POSTGRES_USER) \
	TMI_POSTGRES_PASSWORD=$(INFRASTRUCTURE_POSTGRES_PASSWORD) \
	TMI_POSTGRES_DATABASE=$(INFRASTRUCTURE_POSTGRES_DATABASE) \
	TMI_REDIS_HOST=localhost \
	TMI_REDIS_PORT=$(INFRASTRUCTURE_REDIS_PORT) \
	go test \
		-coverprofile="$(COVERAGE_DIRECTORY)/$(COVERAGE_INTEGRATION_PROFILE)" \
		-covermode=$(COVERAGE_MODE) \
		-coverpkg=./... \
		-tags=$(TEST_INTEGRATION_TAGS) \
		$(TEST_INTEGRATION_PACKAGES) \
		-timeout=$(TEST_INTEGRATION_TIMEOUT) \
		-v
	$(call log_success,"Integration test coverage completed")

coverage-merge:
	$(call log_info,"Merging coverage profiles...")
	@if ! command -v gocovmerge >/dev/null 2>&1; then \
		$(call log_info,"Installing gocovmerge..."); \
		go install $(TOOLS_GOCOVMERGE); \
	fi
	@gocovmerge \
		"$(COVERAGE_DIRECTORY)/$(COVERAGE_UNIT_PROFILE)" \
		"$(COVERAGE_DIRECTORY)/$(COVERAGE_INTEGRATION_PROFILE)" \
		> "$(COVERAGE_DIRECTORY)/$(COVERAGE_COMBINED_PROFILE)"
	$(call log_success,"Coverage profiles merged")

coverage-reports:
	$(call log_info,"Generating coverage reports...")
	@mkdir -p coverage_html
	@if [ "$(OUTPUT_HTML_ENABLED)" = "true" ]; then \
		go tool cover -html="$(COVERAGE_DIRECTORY)/$(COVERAGE_UNIT_PROFILE)" -o "coverage_html/$(COVERAGE_UNIT_HTML_REPORT)"; \
		go tool cover -html="$(COVERAGE_DIRECTORY)/$(COVERAGE_INTEGRATION_PROFILE)" -o "coverage_html/$(COVERAGE_INTEGRATION_HTML_REPORT)"; \
		go tool cover -html="$(COVERAGE_DIRECTORY)/$(COVERAGE_COMBINED_PROFILE)" -o "coverage_html/$(COVERAGE_COMBINED_HTML_REPORT)"; \
	fi
	@if [ "$(OUTPUT_TEXT_ENABLED)" = "true" ]; then \
		go tool cover -func="$(COVERAGE_DIRECTORY)/$(COVERAGE_UNIT_PROFILE)" > "$(COVERAGE_DIRECTORY)/$(COVERAGE_UNIT_DETAILED_REPORT)"; \
		go tool cover -func="$(COVERAGE_DIRECTORY)/$(COVERAGE_INTEGRATION_PROFILE)" > "$(COVERAGE_DIRECTORY)/$(COVERAGE_INTEGRATION_DETAILED_REPORT)"; \
		go tool cover -func="$(COVERAGE_DIRECTORY)/$(COVERAGE_COMBINED_PROFILE)" > "$(COVERAGE_DIRECTORY)/$(COVERAGE_COMBINED_DETAILED_REPORT)"; \
	fi
	@if [ "$(OUTPUT_SUMMARY_ENABLED)" = "true" ]; then \
		$(call log_info,"Generating coverage summary..."); \
		echo "TMI Test Coverage Summary" > "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
		echo "Generated: $$(date)" >> "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
		echo "======================================" >> "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
		echo "" >> "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
		go tool cover -func="$(COVERAGE_DIRECTORY)/$(COVERAGE_COMBINED_PROFILE)" | tail -1 >> "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
		cat "$(COVERAGE_DIRECTORY)/$(COVERAGE_SUMMARY)"; \
	fi
	$(call log_success,"Coverage reports generated in $(COVERAGE_DIRECTORY)/ and coverage_html/")


# OAuth Stub - Development tool for OAuth callback testing
.PHONY: oauth-stub-start oauth-stub-stop oauth-stub-status
oauth-stub-start:
	$(call log_info,"Starting OAuth callback stub on port 8079...")
	@if pgrep -f "oauth-client-callback-stub.py" > /dev/null; then \
		echo -e "$(YELLOW)[WARNING]$(NC) OAuth stub is already running"; \
		echo -e "$(BLUE)[INFO]$(NC) PID: $$(pgrep -f 'oauth-client-callback-stub.py')"; \
	else \
		uv run scripts/oauth-client-callback-stub.py --port 8079 & \
		echo $$! > .oauth-stub.pid; \
		sleep 2; \
		if pgrep -f "oauth-client-callback-stub.py" > /dev/null; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) OAuth stub started on http://localhost:8079/"; \
			echo -e "$(BLUE)[INFO]$(NC) Log file: /tmp/oauth-stub.log"; \
			echo -e "$(BLUE)[INFO]$(NC) PID: $$(cat .oauth-stub.pid)"; \
		else \
			echo -e "$(RED)[ERROR]$(NC) Failed to start OAuth stub"; \
			rm -f .oauth-stub.pid; \
			exit 1; \
		fi; \
	fi

oauth-stub-stop:
	$(call log_info,"Stopping OAuth callback stub...")
	@# Step 1: Send magic exit URL
	@echo -e "$(BLUE)[INFO]$(NC) Sending graceful shutdown request..."
	@curl -s "http://localhost:8079/?code=exit" >/dev/null 2>&1 || true
	@sleep 1
	@# Step 2: Check if anything is still listening on 8079, kill with SIGTERM
	@PIDS=$$(lsof -ti :8079 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Found processes still listening on port 8079: $$PIDS"; \
		for PID in $$PIDS; do \
			echo -e "$(BLUE)[INFO]$(NC) Sending SIGTERM to process $$PID..."; \
			kill $$PID 2>/dev/null || true; \
		done; \
		sleep 2; \
	fi
	@# Step 3: Check again and force kill with SIGKILL if still running
	@PIDS=$$(lsof -ti :8079 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo -e "$(YELLOW)[WARNING]$(NC) Processes still running on port 8079: $$PIDS"; \
		for PID in $$PIDS; do \
			echo -e "$(BLUE)[INFO]$(NC) Force killing process $$PID with SIGKILL..."; \
			kill -9 $$PID 2>/dev/null || true; \
		done; \
		sleep 1; \
	fi
	@# Clean up PID file
	@rm -f .oauth-stub.pid
	@# Final verification
	@PIDS=$$(lsof -ti :8079 2>/dev/null || true); \
	if [ -z "$$PIDS" ]; then \
		echo -e "$(GREEN)[SUCCESS]$(NC) OAuth stub stopped successfully"; \
	else \
		echo -e "$(RED)[ERROR]$(NC) Failed to stop all processes on port 8079: $$PIDS"; \
	fi

oauth-stub-status:
	@if [ -f .oauth-stub.pid ]; then \
		PID=$$(cat .oauth-stub.pid); \
		if kill -0 $$PID 2>/dev/null; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) OAuth stub is running (PID: $$PID)"; \
			echo -e "$(BLUE)[INFO]$(NC) URL: http://localhost:8079/"; \
			echo -e "$(BLUE)[INFO]$(NC) Latest endpoint: http://localhost:8079/latest"; \
		else \
			echo -e "$(YELLOW)[WARNING]$(NC) PID file exists but process $$PID is not running"; \
			rm -f .oauth-stub.pid; \
		fi; \
	else \
		PIDS=$$(pgrep -f "oauth-client-callback-stub.py" || true); \
		if [ -n "$$PIDS" ]; then \
			echo -e "$(YELLOW)[WARNING]$(NC) OAuth stub is running but no PID file found"; \
			echo -e "$(BLUE)[INFO]$(NC) PIDs: $$PIDS"; \
		else \
			echo -e "$(BLUE)[INFO]$(NC) OAuth stub is not running"; \
		fi; \
	fi

 \


# ============================================================================
# BACKWARD COMPATIBILITY ALIASES
# ============================================================================

.PHONY: build build-all test lint clean dev prod test-api

# Keep backward compatibility with existing commands
build: build-server
build-all: build-server
test: test-unit
lint:
	@golangci-lint run
clean: build-clean
dev: dev-start
prod: dev-start  # For now, prod is same as dev



# Legacy test-api target (simplified)
test-api:
	@CONFIG_FILE=config/dev-environment.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Testing API endpoints..."; \
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^SERVER_PORT := ' | sed 's/:=/=/'); \
	if [ -z "$$SERVER_PORT" ]; then SERVER_PORT="8080"; fi; \
	if [ "$(auth)" = "only" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Getting JWT token via OAuth test provider..."; \
		AUTH_REDIRECT=$$(curl -s "http://localhost:$$SERVER_PORT/oauth2/authorize?idp=test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) Token:"; \
			curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null || curl -s "$$AUTH_REDIRECT"; \
		else \
			echo -e "$(RED)[ERROR]$(NC) Failed to get OAuth authorization redirect"; \
		fi; \
	else \
		echo -e "$(BLUE)[INFO]$(NC) Testing authenticated access..."; \
		AUTH_REDIRECT=$$(curl -s "http://localhost:$$SERVER_PORT/oauth2/authorize?idp=test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			TOKEN=$$(curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null); \
			if [ "$$TOKEN" != "null" ] && [ -n "$$TOKEN" ]; then \
				echo -e "$(GREEN)[SUCCESS]$(NC) Got token: $$TOKEN"; \
				echo -e "$(BLUE)[INFO]$(NC) Testing /threat_models endpoint..."; \
				curl -H "Authorization: Bearer $$TOKEN" "http://localhost:$$SERVER_PORT/threat_models" | jq .; \
			fi; \
		fi; \
	fi; \
	rm -f .config.tmp.mk

# ============================================================================
# VALIDATION TARGETS
# ============================================================================

.PHONY: validate-openapi validate-asyncapi

validate-openapi:
	$(call log_info,Validating OpenAPI specification...)
	@uv run scripts/validate_openapi.py shared/api-specs/tmi-openapi.json
	$(call log_success,OpenAPI specification is valid)

validate-asyncapi:
	$(call log_info,Validating AsyncAPI specification...)
	@uv run scripts/validate_asyncapi.py shared/api-specs/tmi-asyncapi.yml
	$(call log_success,AsyncAPI specification is valid)

# ============================================================================
# HELP AND UTILITIES
# ============================================================================

help:
	@echo "TMI Refactored Makefile - Configuration-Driven Atomic Components"
	@echo ""
	@echo "Usage: make <target> [CONFIG=<config-name>]"
	@echo ""
	@echo "Core Composite Targets (use these):"
	@echo "  test-unit              - Run unit tests"
	@echo "  test-integration       - Run integration tests with full setup"
	@echo "  dev-start              - Start development environment"
	@echo "  dev-clean              - Clean development environment"
	@echo ""
	@echo "Atomic Components (building blocks):"
	@echo "  infra-db-start         - Start PostgreSQL container"
	@echo "  infra-redis-start      - Start Redis container"
	@echo "  build-server           - Build server binary"
	@echo "  db-migrate             - Run database migrations"
	@echo "  server-start           - Start server"
	@echo "  clean-all              - Clean up everything"
	@echo ""
	@echo "Validation Targets:"
	@echo "  validate-openapi       - Validate OpenAPI specification"
	@echo "  validate-asyncapi      - Validate AsyncAPI specification"
	@echo ""
	@echo "Configuration Files:"
	@echo "  config/test-unit.yml           - Unit testing configuration"
	@echo "  config/test-integration.yml    - Integration testing configuration"
	@echo "  config/dev-environment.yml     - Development environment configuration"
	@echo "  config/coverage-report.yml     - Coverage reporting configuration"
	@echo ""

list-targets:
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {print $$1}' | grep -v '^Makefile$$' | sort