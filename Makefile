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

# Load configuration if CONFIG_FILE is set
ifdef CONFIG_FILE
ifneq ($(wildcard $(CONFIG_FILE)),)
$(info Loading configuration from $(CONFIG_FILE))
include scripts/load-config.mk
endif
endif

# ============================================================================
# ATOMIC COMPONENTS - Infrastructure Management
# ============================================================================

.PHONY: start-database stop-database clean-database start-redis stop-redis clean-redis

start-database:
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
	if [ -z "$$IMAGE" ]; then IMAGE="tmi/tmi-postgresql:latest"; fi; \
	if ! docker ps -a --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Creating new PostgreSQL container..."; \
		docker run -d \
			--name $$CONTAINER \
			-p 127.0.0.1:$$PORT:5432 \
			-e POSTGRES_USER=$$USER \
			-e POSTGRES_PASSWORD=$$PASSWORD \
			-e POSTGRES_DB=$$DATABASE \
			$$IMAGE; \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Starting existing PostgreSQL container..."; \
		docker start $$CONTAINER; \
	fi; \
	echo "✅ PostgreSQL container is running on port $$PORT"

stop-database:
	$(call log_info,Stopping PostgreSQL container...)
	@CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	docker stop $$CONTAINER 2>/dev/null || true
	$(call log_success,"PostgreSQL container stopped")

clean-database:
	$(call log_warning,"Removing PostgreSQL container and data...")
	@CONTAINER="$(INFRASTRUCTURE_POSTGRES_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-postgresql"; fi; \
	docker rm -f $$CONTAINER 2>/dev/null || true
	$(call log_success,"PostgreSQL container and data removed")

start-redis:
	$(call log_info,Starting Redis container...)
	@CONTAINER="$(INFRASTRUCTURE_REDIS_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-redis"; fi; \
	PORT="$(INFRASTRUCTURE_REDIS_PORT)"; \
	if [ -z "$$PORT" ]; then PORT="6379"; fi; \
	IMAGE="$(INFRASTRUCTURE_REDIS_IMAGE)"; \
	if [ -z "$$IMAGE" ]; then IMAGE="tmi/tmi-redis:latest"; fi; \
	if ! docker ps -a --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Creating new Redis container..."; \
		docker run -d \
			--name $$CONTAINER \
			-p 127.0.0.1:$$PORT:6379 \
			$$IMAGE; \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$$CONTAINER$$"; then \
		echo -e "$(BLUE)[INFO]$(NC) Starting existing Redis container..."; \
		docker start $$CONTAINER; \
	fi; \
	echo "✅ Redis container is running on port $$PORT"

stop-redis:
	$(call log_info,Stopping Redis container...)
	@CONTAINER="$(INFRASTRUCTURE_REDIS_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-redis"; fi; \
	docker stop $$CONTAINER 2>/dev/null || true
	$(call log_success,"Redis container stopped")

clean-redis:
	$(call log_warning,"Removing Redis container and data...")
	@CONTAINER="$(INFRASTRUCTURE_REDIS_CONTAINER)"; \
	if [ -z "$$CONTAINER" ]; then CONTAINER="tmi-redis"; fi; \
	docker rm -f $$CONTAINER 2>/dev/null || true
	$(call log_success,"Redis container and data removed")


# ============================================================================
# ATOMIC COMPONENTS - Build Management
# ============================================================================

.PHONY: build-server build-migrate clean-build generate-api

build-server:
	$(call log_info,Building server binary...)
	@go build -tags="dev" -o bin/server github.com/ericfitz/tmi/cmd/server
	$(call log_success,"Server binary built: bin/server")

build-migrate:
	$(call log_info,Building migration tool...)
	@go build -o bin/migrate github.com/ericfitz/tmi/cmd/migrate
	$(call log_success,"Migration tool built: bin/migrate")

clean-build:
	$(call log_info,"Cleaning build artifacts...")
	@rm -rf ./bin/*
	@rm -f check-db migrate
	$(call log_success,"Build artifacts cleaned")

generate-api:
	$(call log_info,"Generating API code from OpenAPI specification...")
	@oapi-codegen -config oapi-codegen-config.yml docs/reference/apis/tmi-openapi.json
	$(call log_success,"API code generated: api/api.go")

# Legacy alias for generate-api
gen-api: generate-api

# ============================================================================
# ATOMIC COMPONENTS - Database Operations
# ============================================================================

.PHONY: migrate-database check-database wait-database

migrate-database:
	$(call log_info,"Running database migrations...")
	@if [ -f "./bin/migrate" ]; then \
		./bin/migrate up; \
	elif [ -f "./migrate" ]; then \
		./migrate up; \
	else \
		cd cmd/migrate && go run main.go up; \
	fi
	$(call log_success,"Database migrations completed")

check-database:
	$(call log_info,"Checking database migration status...")
	@if [ -f "./bin/check-db" ]; then \
		./bin/check-db; \
	elif [ -f "./check-db" ]; then \
		./check-db; \
	else \
		cd cmd/check-db && go run main.go; \
	fi

wait-database:
	$(call log_info,"Waiting for database to be ready...")
	@timeout=$${TIMEOUTS_DB_READY:-300}; \
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
		echo -e "$(RED)[ERROR]$(NC) Database failed to start within 300 seconds"; \
		exit 1; \
	fi

# ============================================================================
# ATOMIC COMPONENTS - Process Management
# ============================================================================

.PHONY: stop-process wait-process start-server stop-server

stop-process:
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

start-server:
	$(call log_info,"Starting server on port $(SERVER_PORT)")
	@# Build server first to ensure latest code
	@$(MAKE) build-server
	@LOG_FILE="$(SERVER_LOG_FILE)"; \
	if [ -z "$$LOG_FILE" ]; then LOG_FILE="logs/server.log"; fi; \
	mkdir -p "$$(dirname "$$LOG_FILE")"; \
	CONFIG_FILE="$(SERVER_CONFIG_FILE)"; \
	if [ -z "$$CONFIG_FILE" ]; then CONFIG_FILE="config-development.yml"; fi; \
	BINARY="$(SERVER_BINARY)"; \
	if [ -z "$$BINARY" ]; then BINARY="bin/server"; fi; \
	if [ -n "$(SERVER_TAGS)" ]; then \
		echo "Starting server with build tags: $(SERVER_TAGS)"; \
		echo "Building server with tags: $(SERVER_TAGS)"; \
		go build -tags $(SERVER_TAGS) -o $$BINARY ./cmd/server/; \
	fi; \
	echo "Starting server binary: $$BINARY"; \
	$$BINARY --config=$$CONFIG_FILE > $$LOG_FILE 2>&1 & \
	echo $$! > .server.pid
	$(call log_success,"Server started with PID: $$(cat .server.pid)")

stop-server:
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
	@$(MAKE) stop-process
	$(call log_success,"Server stopped")

wait-process:
	$(call log_info,"Waiting for server to be ready on port $(SERVER_PORT)")
	@timeout=$${TIMEOUTS_SERVER_READY:-300}; \
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
		$(call log_error,"Server failed to start within 300 seconds"); \
		exit 1; \
	fi


# ============================================================================
# ATOMIC COMPONENTS - Test Execution
# ============================================================================

.PHONY: execute-tests-unit execute-tests-integration

execute-tests-unit:
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

execute-tests-integration:
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

.PHONY: clean-files clean-containers clean-process clean-everything

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

clean-process:
	$(call log_info,"Cleaning up processes...")
	@# Kill server using PID file first (if available)
	@if [ -f .server.pid ]; then \
		PID=$$(cat .server.pid 2>/dev/null || true); \
		if [ -n "$$PID" ] && ps -p $$PID > /dev/null 2>&1; then \
			echo -e "$(BLUE)[INFO]$(NC) Killing server process from PID file: $$PID"; \
			kill $$PID 2>/dev/null || true; \
			sleep 2; \
			if ps -p $$PID > /dev/null 2>&1; then \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		fi; \
		rm -f .server.pid; \
	fi
	@# Kill any remaining server processes (bin/server)
	@SERVER_PIDS=$$(ps aux | grep '[b]in/server' | awk '{print $$2}' || true); \
	if [ -n "$$SERVER_PIDS" ]; then \
		for PID in $$SERVER_PIDS; do \
			echo -e "$(BLUE)[INFO]$(NC) Killing server process: $$PID"; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
	fi
	@# Kill OAuth stub processes
	@OAUTH_PIDS=$$(ps aux | grep '[o]auth.*stub' | awk '{print $$2}' || true); \
	if [ -n "$$OAUTH_PIDS" ]; then \
		for PID in $$OAUTH_PIDS; do \
			echo -e "$(BLUE)[INFO]$(NC) Killing OAuth stub process: $$PID"; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
	fi
	@# Kill processes on specified ports (final cleanup)
	@if [ -n "$(CLEANUP_PROCESSES)" ] && [ "$(CLEANUP_PROCESSES)" != "" ]; then \
		for port in $(CLEANUP_PROCESSES); do \
			echo -e "$(BLUE)[INFO]$(NC) Killing any remaining processes on port: $$port"; \
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

clean-everything: clean-process clean-containers clean-files

# ============================================================================
# COMPOSITE TARGETS - Main User-Facing Commands
# ============================================================================

.PHONY: test-unit test-integration test-api start-dev clean-dev test-coverage

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
	trap 'CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) clean-everything' EXIT; \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) clean-everything && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) start-database && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) start-redis && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) build-server && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) migrate-database && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) start-server && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) wait-process && \
	CONFIG_FILE=config/test-integration.yml $(MAKE) -f $(MAKEFILE_LIST) execute-tests-integration

# API Testing - Comprehensive Postman/Newman test suite
test-api:
	$(call log_info,"Running comprehensive API test suite...")
	@if [ ! -f postman/run-tests.sh ]; then \
		echo -e "$(RED)[ERROR]$(NC) API test script not found at postman/run-tests.sh"; \
		exit 1; \
	fi
	@if ! command -v newman >/dev/null 2>&1; then \
		echo -e "$(RED)[ERROR]$(NC) Newman is not installed. Install with: npm install -g newman"; \
		exit 1; \
	fi
	@cd postman && ./run-tests.sh

# Development Environment - Start local dev environment
start-dev:
	@CONFIG_FILE=config/dev-environment.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting development environment: Development Environment Configuration"; \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) start-database && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) start-redis && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	go build -o bin/check-db cmd/check-db/main.go && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) migrate-database && \
	eval $$(uv run scripts/yaml-to-make.py config/dev-environment.yml | grep '^SERVER_CONFIG_FILE := ' | sed 's/SERVER_CONFIG_FILE := /SERVER_CONFIG_FILE=/'); \
	if [ ! -f "$$SERVER_CONFIG_FILE" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Generating development configuration..."; \
		go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }; \
	fi && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) start-server
	@eval $$(uv run scripts/yaml-to-make.py config/dev-environment.yml | grep '^SERVER_PORT := ' | sed 's/SERVER_PORT := /SERVER_PORT=/'); \
	echo -e "$(GREEN)[SUCCESS]$(NC) Development environment started on port $$SERVER_PORT"

# Development Environment Cleanup
clean-dev:
	@CONFIG_FILE=config/dev-environment.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Cleaning development environment: Development Environment Configuration"; \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) clean-everything

# Coverage Report Generation - Comprehensive testing with coverage
test-coverage:
	@CONFIG_FILE=config/coverage-report.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Generating coverage reports: Coverage Report Configuration"; \
	trap 'CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) clean-everything' EXIT; \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) clean-everything && \
	COVERAGE_DIRECTORY=$$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^COVERAGE_DIRECTORY := ' | sed 's/COVERAGE_DIRECTORY := //'); \
	mkdir -p $$COVERAGE_DIRECTORY && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) test-coverage-unit && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) start-database && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) start-redis && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) migrate-database && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) test-coverage-integration && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) merge-coverage && \
	CONFIG_FILE=config/coverage-report.yml $(MAKE) -f $(MAKEFILE_LIST) generate-coverage


# ============================================================================
# SPECIALIZED ATOMIC COMPONENTS - Coverage
# ============================================================================

.PHONY: test-coverage-unit test-coverage-integration merge-coverage generate-coverage

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

merge-coverage:
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

generate-coverage:
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
.PHONY: start-oauth-stub stop-oauth-stub kill-oauth-stub check-oauth-stub
start-oauth-stub:
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

stop-oauth-stub:
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

kill-oauth-stub:
	$(call log_info,"Force killing anything on port 8079...")
	@PIDS=$$(lsof -ti :8079 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo -e "$(YELLOW)[WARNING]$(NC) Found processes on port 8079: $$PIDS"; \
		for PID in $$PIDS; do \
			echo -e "$(BLUE)[INFO]$(NC) Force killing process $$PID with SIGKILL..."; \
			kill -9 $$PID 2>/dev/null || true; \
		done; \
		sleep 1; \
		echo -e "$(GREEN)[SUCCESS]$(NC) All processes on port 8079 killed"; \
	else \
		echo -e "$(GREEN)[SUCCESS]$(NC) No processes found on port 8079"; \
	fi
	@rm -f .oauth-stub.pid

check-oauth-stub:
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

# ============================================================================
# CATS FUZZING - API Security Testing
# ============================================================================

.PHONY: cats-fuzz
cats-fuzz:
	$(call log_info,"Running CATS API fuzzing with OAuth authentication...")
	@if ! command -v cats >/dev/null 2>&1; then \
		$(call log_error,"CATS tool not found. Please install it first."); \
		$(call log_info,"See: https://github.com/Endava/cats"); \
		exit 1; \
	fi
	@./scripts/run-cats-fuzz.sh

cats-fuzz-user:
	$(call log_info,"Running CATS API fuzzing with custom user...")
	@if [ -z "$(USER)" ]; then \
		$(call log_error,"Please specify USER variable: make cats-fuzz-user USER=alice"); \
		exit 1; \
	fi
	@./scripts/run-cats-fuzz.sh -u "$(USER)"

cats-fuzz-server:
	$(call log_info,"Running CATS API fuzzing against custom server...")
	@if [ -z "$(SERVER)" ]; then \
		$(call log_error,"Please specify SERVER variable: make cats-fuzz-server SERVER=http://localhost:8080"); \
		exit 1; \
	fi
	@./scripts/run-cats-fuzz.sh -s "$(SERVER)"

cats-fuzz-custom:
	$(call log_info,"Running CATS API fuzzing with custom user and server...")
	@if [ -z "$(USER)" ] || [ -z "$(SERVER)" ]; then \
		$(call log_error,"Please specify USER and SERVER variables: make cats-fuzz-custom USER=alice SERVER=http://localhost:8080"); \
		exit 1; \
	fi
	@./scripts/run-cats-fuzz.sh -u "$(USER)" -s "$(SERVER)"


# ============================================================================
# CONTAINER SECURITY AND BUILD MANAGEMENT
# ============================================================================

.PHONY: build-containers scan-containers report-containers

# Build containers with vulnerability patching
build-containers:
	$(call log_info,Building containers with vulnerability patching...)
	@./scripts/build-containers.sh
	$(call log_success,Containers built successfully)

# Run security scan on existing containers
scan-containers:
	$(call log_info,Running security scans on container images...)
	@if ! command -v docker scout >/dev/null 2>&1; then \
		$(call log_error,Docker Scout not available. Please install Docker Scout CLI); \
		exit 1; \
	fi
	@mkdir -p security-reports
	@echo "Scanning cgr.dev/chainguard/postgres:latest..."
	@docker scout cves cgr.dev/chainguard/postgres:latest --only-severity critical,high > security-reports/postgresql-scan.txt 2>&1 || true
	@echo "Scanning tmi/tmi-redis:latest..."
	@docker scout cves tmi/tmi-redis:latest --only-severity critical,high > security-reports/redis-scan.txt 2>&1 || true
	@if [ -f "Dockerfile.dev" ]; then \
		echo "Building and scanning application image..."; \
		docker build -f Dockerfile.dev -t tmi-temp-scan:latest . >/dev/null 2>&1 || true; \
		docker scout cves tmi-temp-scan:latest --only-severity critical,high > security-reports/application-scan.txt 2>&1 || true; \
		docker rmi tmi-temp-scan:latest >/dev/null 2>&1 || true; \
	fi
	$(call log_success,Security scans completed. Reports in security-reports/)

# Generate comprehensive security report
report-containers: scan-containers
	$(call log_info,Generating container security report...)
	@mkdir -p security-reports
	@echo "# TMI Container Security Report" > security-reports/security-summary.md
	@echo "" >> security-reports/security-summary.md
	@echo "**Generated:** $$(date)" >> security-reports/security-summary.md
	@echo "**Scanner:** Docker Scout" >> security-reports/security-summary.md
	@echo "" >> security-reports/security-summary.md
	@echo "## Vulnerability Summary" >> security-reports/security-summary.md
	@echo "" >> security-reports/security-summary.md
	@echo "| Image | Critical | High | Status |" >> security-reports/security-summary.md
	@echo "|-------|----------|------|--------|" >> security-reports/security-summary.md
	@for scan in postgresql redis application; do \
		if [ -f "security-reports/$$scan-scan.txt" ]; then \
			critical=$$( (grep -c "CRITICAL" "security-reports/$$scan-scan.txt" 2>/dev/null || echo "0") | tail -1 | tr -d '\n\r ' ); \
			high=$$( (grep -c "HIGH" "security-reports/$$scan-scan.txt" 2>/dev/null || echo "0") | tail -1 | tr -d '\n\r ' ); \
			status="✅ Good"; \
			if [ "$$critical" -gt "0" ] 2>/dev/null; then status="❌ Critical Issues"; \
			elif [ "$$high" -gt "3" ] 2>/dev/null; then status="⚠️ High Issues"; fi; \
			echo "| $$scan | $$critical | $$high | $$status |" >> security-reports/security-summary.md; \
		fi; \
	done
	@echo "" >> security-reports/security-summary.md
	@echo "## Recommendations" >> security-reports/security-summary.md
	@echo "" >> security-reports/security-summary.md
	@echo "1. Use \`make containers-build\` to build patched containers" >> security-reports/security-summary.md
	@echo "2. Regularly update base images" >> security-reports/security-summary.md
	@echo "3. Implement runtime security monitoring" >> security-reports/security-summary.md
	@echo "4. Review detailed scan results in security-reports/" >> security-reports/security-summary.md
	$(call log_success,Security report generated: security-reports/security-summary.md)

# Start development environment with containers (builds containers first)
start-containers-environment:
	$(call log_info,Starting development environment with containers...)
	@./scripts/make-containers-dev-local.sh
	$(call log_success,Development environment started)

# Start server using existing containers (no rebuild)
start-dev-existing:
	@CONFIG_FILE=config/dev-environment-secure.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting development server with containers: Container-based Development Environment Configuration"; \
	if ! docker ps --format "{{.Names}}" | grep -q "^tmi-postgresql$$"; then \
		echo -e "$(RED)[ERROR]$(NC) PostgreSQL container not running. Run 'make start-containers-environment' first."; \
		exit 1; \
	fi; \
	if ! docker ps --format "{{.Names}}" | grep -q "^tmi-redis$$"; then \
		echo -e "$(RED)[ERROR]$(NC) Redis container not running. Run 'make start-containers-environment' first."; \
		exit 1; \
	fi; \
	CONFIG_FILE=config/dev-environment-secure.yml $(MAKE) -f $(MAKEFILE_LIST) wait-database && \
	go build -o bin/check-db cmd/check-db/main.go && \
	CONFIG_FILE=config/dev-environment-secure.yml $(MAKE) -f $(MAKEFILE_LIST) migrate-database && \
	eval $$(uv run scripts/yaml-to-make.py config/dev-environment-secure.yml | grep '^SERVER_CONFIG_FILE := ' | sed 's/SERVER_CONFIG_FILE := /SERVER_CONFIG_FILE=/'); \
	if [ ! -f "$$SERVER_CONFIG_FILE" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Generating development configuration..."; \
		go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }; \
	fi && \
	CONFIG_FILE=config/dev-environment-secure.yml $(MAKE) -f $(MAKEFILE_LIST) start-server
	@eval $$(uv run scripts/yaml-to-make.py config/dev-environment-secure.yml | grep '^SERVER_PORT := ' | sed 's/SERVER_PORT := /SERVER_PORT=/'); \
	echo -e "$(GREEN)[SUCCESS]$(NC) Development server started on port $$SERVER_PORT using containers"

# Shorthand for all container operations
build-containers-all: build-containers report-containers

# ============================================================================
# DISTROLESS CONTAINER MANAGEMENT
# ============================================================================


# ============================================================================
# BACKWARD COMPATIBILITY ALIASES
# ============================================================================

.PHONY: build build-all build-everything test lint clean dev prod infra-db-start infra-redis-start db-migrate dev-start containers-build

# Keep backward compatibility with existing commands
build: build-server
build-all: build-server  # Deprecated: use build-everything
build-everything: build-server
test: test-unit
lint:
	@golangci-lint run
clean: clean-build
dev: start-dev
prod: start-dev  # For now, prod is same as dev

# Deprecated aliases for commonly used targets (will show warning)
infra-db-start:
	@echo "⚠️  WARNING: 'infra-db-start' is deprecated. Use 'start-database' instead."
	@$(MAKE) start-database

infra-redis-start:
	@echo "⚠️  WARNING: 'infra-redis-start' is deprecated. Use 'start-redis' instead."
	@$(MAKE) start-redis

db-migrate:
	@echo "⚠️  WARNING: 'db-migrate' is deprecated. Use 'migrate-database' instead."
	@$(MAKE) migrate-database

dev-start:
	@echo "⚠️  WARNING: 'dev-start' is deprecated. Use 'start-dev' instead."
	@$(MAKE) start-dev

containers-build:
	@echo "⚠️  WARNING: 'containers-build' is deprecated. Use 'build-containers' instead."
	@$(MAKE) build-containers


# ============================================================================
# WEBSOCKET TEST HARNESS
# ============================================================================

.PHONY: build-wstest wstest monitor-wstest clean-wstest

build-wstest:
	$(call log_info,Building WebSocket test harness...)
	@cd ws-test-harness && go mod tidy && go build -o ws-test-harness
	$(call log_success,WebSocket test harness built successfully)

wstest: build-wstest
	$(call log_info,Starting WebSocket test with 3 terminals...)
	@# Check if server is running
	@if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then \
		$(call log_error,Server not running. Please run 'make start-dev' first); \
		exit 1; \
	fi
	@# Terminal 1: Host (alice)
	@if [ "$$TERM_PROGRAM" = "Apple_Terminal" ] || [ "$$TERM_PROGRAM" = "iTerm.app" ]; then \
		osascript -e 'tell app "Terminal" to do script "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user alice --host --participants \"bob,charlie,hobobarbarian@gmail.com\""' > /dev/null; \
	elif command -v gnome-terminal > /dev/null 2>&1; then \
		gnome-terminal -- bash -c "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user alice --host --participants 'bob,charlie,hobobarbarian@gmail.com'; exec bash" & \
	elif command -v xterm > /dev/null 2>&1; then \
		xterm -e "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user alice --host --participants 'bob,charlie,hobobarbarian@gmail.com'" & \
	else \
		echo -e "$(YELLOW)[WARNING]$(NC) Could not detect terminal emulator. Running in background..."; \
		cd ws-test-harness && timeout 30 ./ws-test-harness --user alice --host --participants "bob,charlie,hobobarbarian@gmail.com" > alice.log 2>&1 & \
		echo "Host (alice) running in background, see ws-test-harness/alice.log"; \
	fi
	@# Wait for host to start
	@sleep 3
	@# Terminal 2: Participant (bob)
	@if [ "$$TERM_PROGRAM" = "Apple_Terminal" ] || [ "$$TERM_PROGRAM" = "iTerm.app" ]; then \
		osascript -e 'tell app "Terminal" to do script "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user bob"' > /dev/null; \
	elif command -v gnome-terminal > /dev/null 2>&1; then \
		gnome-terminal -- bash -c "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user bob; exec bash" & \
	elif command -v xterm > /dev/null 2>&1; then \
		xterm -e "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user bob" & \
	else \
		cd ws-test-harness && timeout 30 ./ws-test-harness --user bob > bob.log 2>&1 & \
		echo "Participant (bob) running in background, see ws-test-harness/bob.log"; \
	fi
	@# Terminal 3: Participant (charlie)
	@if [ "$$TERM_PROGRAM" = "Apple_Terminal" ] || [ "$$TERM_PROGRAM" = "iTerm.app" ]; then \
		osascript -e 'tell app "Terminal" to do script "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user charlie"' > /dev/null; \
	elif command -v gnome-terminal > /dev/null 2>&1; then \
		gnome-terminal -- bash -c "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user charlie; exec bash" & \
	elif command -v xterm > /dev/null 2>&1; then \
		xterm -e "cd $(PWD)/ws-test-harness && timeout 30 ./ws-test-harness --user charlie" & \
	else \
		cd ws-test-harness && timeout 30 ./ws-test-harness --user charlie > charlie.log 2>&1 & \
		echo "Participant (charlie) running in background, see ws-test-harness/charlie.log"; \
	fi
	$(call log_success,WebSocket test started with 3 terminals)
	@echo "Watch the terminals for WebSocket activity. Use 'make clean-wstest' to stop all instances."

monitor-wstest: build-wstest
	$(call log_info,Starting WebSocket monitor...)
	@# Check if server is running
	@if ! curl -s http://localhost:8080/health > /dev/null 2>&1; then \
		$(call log_error,Server not running. Please run 'make start-dev' first); \
		exit 1; \
	fi
	@# Run monitor in foreground
	@cd ws-test-harness && ./ws-test-harness --user monitor

clean-wstest:
	$(call log_info,Stopping all WebSocket test harness instances...)
	@# Kill all ws-test-harness processes
	@if pgrep -f "ws-test-harness" > /dev/null 2>&1; then \
		pkill -f "ws-test-harness" && \
		echo -e "$(GREEN)[SUCCESS]$(NC) All WebSocket test harness instances stopped"; \
	else \
		echo -e "$(YELLOW)[WARNING]$(NC) No WebSocket test harness instances found"; \
	fi
	@# Clean up any log files
	@rm -f ws-test-harness/*.log 2>/dev/null || true

# ============================================================================
# VALIDATION TARGETS
# ============================================================================

.PHONY: validate-openapi validate-asyncapi

validate-openapi:
	$(call log_info,Validating OpenAPI specification...)
	@uv run scripts/validate_openapi.py docs/reference/apis/tmi-openapi.json
	$(call log_success,OpenAPI specification is valid)

validate-asyncapi:
	$(call log_info,Validating AsyncAPI specification...)
	@uv run scripts/validate_asyncapi.py docs/reference/apis/tmi-asyncapi.yml
	$(call log_success,AsyncAPI specification is valid)

# ============================================================================
# STATUS CHECKING
# ============================================================================

.PHONY: status

status:
	@echo "TMI Service Status Check"
	@echo "========================"
	@echo ""
	@printf "%-1s %-23s %-6s %-13s %s\n" "S" "SERVICE" "PORT" "STATUS" "PROCESS"
	@printf "%-1s %-23s %-6s %-13s %s\n" "-" "-----------------------" "------" "-------------" "----------------------------"
	@# Check Service (port 8080) - look for actual server process
	@SERVICE_PID=""; \
	for pid in $$(lsof -ti :8080 2>/dev/null || true); do \
		PROC_CMD=$$(ps -p $$pid -o args= 2>/dev/null | head -1 || true); \
		if echo "$$PROC_CMD" | grep -q "bin/server\|server.*--config"; then \
			SERVICE_PID=$$pid; \
			break; \
		fi; \
	done; \
	if [ -n "$$SERVICE_PID" ]; then \
		SERVICE_NAME=$$(ps -p $$SERVICE_PID -o args= 2>/dev/null | head -1 | awk '{print $$1}' | xargs basename 2>/dev/null || echo "unknown"); \
		printf "\033[0;32m✓\033[0m %-23s %-6s %-13s $$SERVICE_PID ($$SERVICE_NAME)\n" "Service" "8080" "Running"; \
	else \
		printf "\033[0;31m✗\033[0m %-23s %-6s %-13s %s\n" "Service" "8080" "Stopped" "-"; \
	fi
	@# Check Database (port 5432)
	@DB_PID=$$(lsof -ti :5432 2>/dev/null | head -1 || true); \
	if [ -n "$$DB_PID" ]; then \
		DB_NAME=$$(ps -p $$DB_PID -o args= 2>/dev/null | head -1 | awk '{print $$1}' | xargs basename 2>/dev/null || echo "unknown"); \
		printf "\033[0;32m✓\033[0m %-23s %-6s %-13s $$DB_PID ($$DB_NAME)\n" "Database" "5432" "Running"; \
	else \
		printf "\033[0;31m✗\033[0m %-23s %-6s %-13s %s\n" "Database" "5432" "Stopped" "-"; \
	fi
	@# Check Redis (port 6379)
	@REDIS_PID=$$(lsof -ti :6379 2>/dev/null | head -1 || true); \
	if [ -n "$$REDIS_PID" ]; then \
		REDIS_NAME=$$(ps -p $$REDIS_PID -o args= 2>/dev/null | head -1 | awk '{print $$1}' | xargs basename 2>/dev/null || echo "unknown"); \
		printf "\033[0;32m✓\033[0m %-23s %-6s %-13s $$REDIS_PID ($$REDIS_NAME)\n" "Redis" "6379" "Running"; \
	else \
		printf "\033[0;31m✗\033[0m %-23s %-6s %-13s %s\n" "Redis" "6379" "Stopped" "-"; \
	fi
	@# Check Application (port 4200)
	@APP_PID=$$(lsof -ti :4200 2>/dev/null | head -1 || true); \
	if [ -n "$$APP_PID" ]; then \
		APP_NAME=$$(ps -p $$APP_PID -o args= 2>/dev/null | head -1 | awk '{print $$1}' | xargs basename 2>/dev/null || echo "unknown"); \
		printf "\033[0;32m✓\033[0m %-23s %-6s %-13s $$APP_PID ($$APP_NAME)\n" "Application" "4200" "Running"; \
	else \
		printf "\033[0;31m✗\033[0m %-23s %-6s %-13s %s\n" "Application" "4200" "Stopped" "-"; \
	fi
	@# Check OAuth Stub (port 8079) - optional
	@OAUTH_PID=$$(lsof -ti :8079 2>/dev/null | head -1 || true); \
	if [ -n "$$OAUTH_PID" ]; then \
		OAUTH_NAME=$$(ps -p $$OAUTH_PID -o args= 2>/dev/null | head -1 | awk '{print $$1}' | xargs basename 2>/dev/null || echo "unknown"); \
		printf "\033[0;32m✓\033[0m %-23s %-6s %-13s $$OAUTH_PID ($$OAUTH_NAME)\n" "OAuth Stub" "8079" "Running"; \
	else \
		printf "\033[1;33m⚬\033[0m %-23s %-6s %-13s %s\n" "OAuth Stub (Optional)" "8079" "Not running"; \
	fi
	@echo ""

# ============================================================================
# HELP AND UTILITIES
# ============================================================================

help:
	@echo "TMI Refactored Makefile - Configuration-Driven Atomic Components"
	@echo ""
	@echo "Usage: make <target> [CONFIG=<config-name>]"
	@echo ""
	@echo "Core Composite Targets (use these):"
	@echo "  status                 - Check status of all services"
	@echo "  test-unit              - Run unit tests"
	@echo "  test-integration       - Run integration tests with full setup"
	@echo "  start-dev              - Start development environment"
	@echo "  start-dev-existing     - Start server using existing containers"
	@echo "  clean-dev              - Clean development environment"
	@echo ""
	@echo "Container Management (Docker Scout Integration):"
	@echo "  build-containers             - Build containers with vulnerability patching"
	@echo "  scan-containers              - Scan existing containers for vulnerabilities"
	@echo "  report-containers            - Generate comprehensive security report"
	@echo "  start-containers-environment - Start development with containers"
	@echo "  build-containers-all         - Run full container build and report"
	@echo ""
	@echo ""
	@echo "Atomic Components (building blocks):"
	@echo "  start-database         - Start PostgreSQL container"
	@echo "  start-redis            - Start Redis container"
	@echo "  build-server           - Build server binary"
	@echo "  migrate-database       - Run database migrations"
	@echo "  start-server           - Start server"
	@echo "  clean-everything       - Clean up everything"
	@echo ""
	@echo "WebSocket Testing:"
	@echo "  build-wstest           - Build WebSocket test harness"
	@echo "  wstest                 - Run WebSocket test with 3 terminals (alice, bob, charlie)"
	@echo "  monitor-wstest         - Run WebSocket test harness with user 'monitor'"
	@echo "  clean-wstest           - Stop all running WebSocket test instances"
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