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

.PHONY: infra-db-start infra-db-stop infra-db-clean infra-redis-start infra-redis-stop infra-redis-clean infra-observability-start infra-observability-stop infra-observability-clean

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

infra-observability-start:
	$(call log_info,"Starting observability stack...")
	@if [ -z "$(OBSERVABILITY_COMPOSE_FILE)" ]; then \
		echo "Error: Observability configuration not loaded. Set OBSERVABILITY_COMPOSE_FILE variable."; \
		exit 1; \
	fi
	@if ! docker info >/dev/null 2>&1; then \
		$(call log_error,"Docker is not running. Please start Docker first."); \
		exit 1; \
	fi
	$(call log_info,"Starting services with $(OBSERVABILITY_COMPOSE_FILE)...")
	@docker-compose -f $(OBSERVABILITY_COMPOSE_FILE) up -d
	$(call log_success,"Observability stack started")

infra-observability-stop:
	$(call log_info,"Stopping observability stack...")
	@if [ -z "$(OBSERVABILITY_COMPOSE_FILE)" ]; then \
		echo "Error: Observability configuration not loaded. Set OBSERVABILITY_COMPOSE_FILE variable."; \
		exit 1; \
	fi
	@docker-compose -f $(OBSERVABILITY_COMPOSE_FILE) down
	$(call log_success,"Observability stack stopped")

infra-observability-clean:
	$(call log_warning,"Removing observability stack and data...")
	@if [ -z "$(OBSERVABILITY_COMPOSE_FILE)" ]; then \
		echo "Error: Observability configuration not loaded. Set OBSERVABILITY_COMPOSE_FILE variable."; \
		exit 1; \
	fi
	@docker-compose -f $(OBSERVABILITY_COMPOSE_FILE) down -v --remove-orphans
	$(call log_success,"Observability stack and data removed")

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

.PHONY: process-stop process-wait server-start server-stop observability-wait observability-health

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
	if [ -z "$$LOG_FILE" ]; then LOG_FILE="server.log"; fi; \
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

observability-wait:
	$(call log_info,"Waiting for observability services to be ready...")
	@timeout=$${TIMEOUTS_STACK_READY:-60}; \
	echo "⏳ Waiting for services to start..."; \
	sleep 10; \
	while [ $$timeout -gt 0 ]; do \
		if $(MAKE) observability-health >/dev/null 2>&1; then \
			$(call log_success,"Observability services are ready!"); \
			break; \
		fi; \
		echo "⏳ Services starting... ($$timeout seconds remaining)"; \
		sleep 5; \
		timeout=$$((timeout - 5)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		$(call log_error,"Observability services failed to start within $${TIMEOUTS_STACK_READY:-60} seconds"); \
		exit 1; \
	fi

observability-health:
	$(call log_info,"Checking observability service health...")
	@HEALTH_OK=true; \
	if curl -f http://localhost:$(JAEGER_PORT)/api/services >/dev/null 2>&1; then \
		echo "✅ Jaeger UI available at http://localhost:$(JAEGER_PORT)"; \
	else \
		echo "⚠️  Jaeger not ready at port $(JAEGER_PORT)"; \
		HEALTH_OK=false; \
	fi; \
	if curl -f http://localhost:$(PROMETHEUS_PORT)/-/ready >/dev/null 2>&1; then \
		echo "✅ Prometheus available at http://localhost:$(PROMETHEUS_PORT)"; \
	else \
		echo "⚠️  Prometheus not ready at port $(PROMETHEUS_PORT)"; \
		HEALTH_OK=false; \
	fi; \
	if curl -f http://localhost:$(GRAFANA_PORT)/api/health >/dev/null 2>&1; then \
		echo "✅ Grafana available at http://localhost:$(GRAFANA_PORT)"; \
	else \
		echo "⚠️  Grafana not ready at port $(GRAFANA_PORT)"; \
		HEALTH_OK=false; \
	fi; \
	if curl -f http://localhost:$(OTEL_COLLECTOR_PORT)/v1/traces >/dev/null 2>&1; then \
		echo "✅ OpenTelemetry Collector ready at http://localhost:$(OTEL_COLLECTOR_PORT)"; \
	else \
		echo "⚠️  OpenTelemetry Collector not ready at port $(OTEL_COLLECTOR_PORT)"; \
		HEALTH_OK=false; \
	fi; \
	if [ "$$HEALTH_OK" = "false" ]; then \
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

.PHONY: test-unit test-integration dev-start dev-clean test-coverage stepci-full observability-start observability-stop observability-clean test-telemetry

# Unit Testing - Fast tests with no external dependencies
test-unit:
	@CONFIG_FILE=config/test-unit.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Starting unit tests..."; \
	TMI_LOGGING_IS_TEST=true go test -short ./... -v; \
	rm -f .config.tmp.mk integration-test.log server.log .server.pid

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

# StepCI Full Integration Testing
stepci-full:
	@CONFIG_FILE=config/stepci-full.yml; \
	echo -e "$(BLUE)[INFO]$(NC) Loading configuration from $$CONFIG_FILE"; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	echo -e "$(BLUE)[INFO]$(NC) Running StepCI full integration tests: StepCI Full Integration Testing"; \
	trap 'eval $$(cat .config.tmp.mk) && $(MAKE) clean-all; $(MAKE) oauth-stub-stop; rm -f .config.tmp.mk' EXIT; \
	$(MAKE) oauth-stub-stop && \
	eval $$(cat .config.tmp.mk) && $(MAKE) clean-processes && \
	eval $$(cat .config.tmp.mk) && $(MAKE) infra-db-start && \
	eval $$(cat .config.tmp.mk) && $(MAKE) infra-redis-start && \
	eval $$(cat .config.tmp.mk) && $(MAKE) db-wait && \
	eval $$(cat .config.tmp.mk) && $(MAKE) build-server && \
	eval $$(cat .config.tmp.mk) && $(MAKE) db-migrate && \
	$(MAKE) oauth-stub-start && \
	eval $$(cat .config.tmp.mk) && $(MAKE) server-start && \
	eval $$(cat .config.tmp.mk) && $(MAKE) process-wait && \
	eval $$(cat .config.tmp.mk) && $(MAKE) stepci-execute

# ============================================================================
# SPECIALIZED ATOMIC COMPONENTS - Coverage and StepCI
# ============================================================================

.PHONY: test-coverage-unit test-coverage-integration coverage-merge coverage-reports stepci-execute

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

stepci-execute:
	$(call log_info,Executing StepCI tests...)
	@TEST_DIR="$(STEPCI_TEST_DIRECTORY)"; \
	if [ -z "$$TEST_DIR" ]; then TEST_DIR="stepci"; fi; \
	if [ -n "$(TEST_PATTERN)" ] && [ "$(TEST_PATTERN)" != "" ]; then \
		echo -e "\033[0;34m[INFO]\033[0m Running specific StepCI test: $(TEST_PATTERN)"; \
		if [ ! -f "$$TEST_DIR/$(TEST_PATTERN)" ]; then \
			echo -e "\033[0;31m[ERROR]\033[0m Test file not found: $$TEST_DIR/$(TEST_PATTERN)"; \
			echo -e "\033[0;34m[INFO]\033[0m Available tests:"; \
			find "$$TEST_DIR" -name "*.yml" | grep -v "/utils/" | sed "s|$$TEST_DIR/||" | sort; \
			exit 1; \
		fi; \
		stepci run "$$TEST_DIR/$(TEST_PATTERN)"; \
	else \
		echo -e "\033[0;34m[INFO]\033[0m Running all StepCI integration tests in series (OAuth stub requires serial execution)..."; \
		for test_file in $$(find "$$TEST_DIR" -name "*.yml" | grep -v "/utils/" | sort); do \
			echo -e "\033[0;34m[INFO]\033[0m Running: $$test_file"; \
			stepci run "$$test_file" || echo "❌ Test failed: $$test_file"; \
			echo ""; \
			sleep 1; \
		done; \
	fi
	$(call log_success,"StepCI tests completed")

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

# Observability Stack - Start monitoring and telemetry services
observability-start:
	@CONFIG_FILE=config/observability.yml; \
	$(call log_info,"Loading configuration from $$CONFIG_FILE"); \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	$(call log_info,"Starting observability stack..."); \
	if ! docker info >/dev/null 2>&1; then \
		echo "Error: Docker is not running. Please start Docker first."; \
		exit 1; \
	fi; \
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^OBSERVABILITY_COMPOSE_FILE := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^GRAFANA_PORT := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^PROMETHEUS_PORT := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^JAEGER_PORT := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^OTEL_COLLECTOR_PORT := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^GRAFANA_URL := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^PROMETHEUS_URL := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^JAEGER_URL := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^LOKI_URL := ' | sed 's/:=/=/');
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^OTEL_COLLECTOR_GRPC_PORT := ' | sed 's/:=/=/');
	echo "Starting services with $$OBSERVABILITY_COMPOSE_FILE..."; \
	docker compose -f $$OBSERVABILITY_COMPOSE_FILE up -d; \
	echo "⏳ Waiting for services to start..."; \
	sleep 10; \
	timeout=60; \
	while [ $$timeout -gt 0 ]; do \
		HEALTH_OK=true; \
		if ! curl -f http://localhost:$$JAEGER_PORT/api/services >/dev/null 2>&1; then HEALTH_OK=false; fi; \
		if ! curl -f http://localhost:$$PROMETHEUS_PORT/-/ready >/dev/null 2>&1; then HEALTH_OK=false; fi; \
		if ! curl -f http://localhost:$$GRAFANA_PORT/api/health >/dev/null 2>&1; then HEALTH_OK=false; fi; \
		if ! curl -f http://localhost:$$OTEL_COLLECTOR_PORT/v1/traces >/dev/null 2>&1; then HEALTH_OK=false; fi; \
		if [ "$$HEALTH_OK" = "true" ]; then \
			echo "✅ All services are ready!"; \
			break; \
		fi; \
		echo "⏳ Services starting... ($$timeout seconds remaining)"; \
		sleep 5; \
		timeout=$$((timeout - 5)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		echo "Services failed to start within 60 seconds"; \
		exit 1; \
	fi; \
	echo ""; \
	echo "🎉 Observability stack started!"; \
	echo ""; \
	echo "Services:"; \
	echo "  📊 Grafana:     $$GRAFANA_URL (admin/admin)"; \
	echo "  🔍 Jaeger:      $$JAEGER_URL"; \
	echo "  📈 Prometheus:  $$PROMETHEUS_URL"; \
	echo "  📋 Loki:       $$LOKI_URL"; \
	echo "  📡 OTel:       http://localhost:$$OTEL_COLLECTOR_PORT (HTTP) / :$$OTEL_COLLECTOR_GRPC_PORT (gRPC)"; \
	echo ""; \
	echo "To stop: make observability-stop"; \
	echo "To view logs: docker compose -f $$OBSERVABILITY_COMPOSE_FILE logs -f [service]"; \
	rm -f .config.tmp.mk

# Observability Stack - Stop monitoring services
observability-stop:
	@CONFIG_FILE=config/observability.yml; \
	echo "🛑 Stopping observability stack..."; \
	COMPOSE_FILE=$$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^OBSERVABILITY_COMPOSE_FILE := ' | sed 's/^OBSERVABILITY_COMPOSE_FILE := //'); \
	docker compose -f $$COMPOSE_FILE down; \
	echo "✅ Observability stack stopped!"

# Observability Stack - Clean up with data removal
observability-clean:
	@CONFIG_FILE=config/observability.yml; \
	echo "🧹 Cleaning observability stack - this will delete all observability data"; \
	COMPOSE_FILE=$$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^OBSERVABILITY_COMPOSE_FILE := ' | sed 's/^OBSERVABILITY_COMPOSE_FILE := //'); \
	docker compose -f $$COMPOSE_FILE down -v --remove-orphans; \
	echo "✅ Observability stack and data removed!"

# Telemetry Testing - Test telemetry components with observability stack
test-telemetry:
	@CONFIG_FILE=config/observability.yml; \
	echo "🧪 Testing telemetry components..."; \
	trap 'make observability-clean' EXIT; \
	make observability-start; \
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^TELEMETRY_TEST_PACKAGES := ' | sed 's/:=/=/'); \
	eval $$(uv run scripts/yaml-to-make.py $$CONFIG_FILE | grep '^TELEMETRY_INTEGRATION_TAGS := ' | sed 's/:=/=/'); \
	echo "Running telemetry tests..."; \
	TMI_LOGGING_IS_TEST=true go test \
		$$TELEMETRY_TEST_PACKAGES \
		-tags="$$TELEMETRY_INTEGRATION_TAGS" \
		-timeout=5m \
		-v; \
	echo "✅ Telemetry tests completed"

# ============================================================================
# STEPCI PREPARATION - Environment Setup for API Testing
# ============================================================================

.PHONY: stepci-cleanup stepci-setup stepci-auth-user stepci-prep run-stepci-test run-stepci-tests

# Complete environment cleanup for StepCI preparation
stepci-cleanup:
	$(call log_info,Cleaning up environment for StepCI preparation...)
	@$(MAKE) server-stop 2>/dev/null || true
	@$(MAKE) oauth-stub-stop 2>/dev/null || true
	@# Stop and clean containers with default names
	@echo -e "$(BLUE)[INFO]$(NC) Stopping and cleaning PostgreSQL container..."
	@docker stop tmi-postgresql 2>/dev/null || true
	@docker rm -f tmi-postgresql 2>/dev/null || true
	@echo -e "$(BLUE)[INFO]$(NC) Stopping and cleaning Redis container..."
	@docker stop tmi-redis 2>/dev/null || true
	@docker rm -f tmi-redis 2>/dev/null || true
	@# Kill any remaining processes on port 8080
	@echo -e "$(BLUE)[INFO]$(NC) Killing any remaining processes on port 8080..."
	@PIDS=$$(lsof -ti :8080 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		for PID in $$PIDS; do \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
	fi
	@rm -f .server.pid .oauth-stub.pid server.log
	@rm -rf tmp/alice.json tmp/bob.json tmp/chuck.json
	$(call log_success,"Environment cleanup completed")

# Setup clean environment for StepCI testing
stepci-setup:
	$(call log_info,Setting up clean environment for StepCI testing...)
	@CONFIG_FILE=config/dev-environment.yml; \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) infra-db-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) infra-redis-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) db-wait && \
	$(MAKE) build-server && \
	go build -o bin/check-db cmd/check-db/main.go && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) db-migrate && \
	eval $$(uv run scripts/yaml-to-make.py config/dev-environment.yml | grep '^SERVER_CONFIG_FILE := ' | sed 's/SERVER_CONFIG_FILE := /SERVER_CONFIG_FILE=/'); \
	if [ ! -f "$$SERVER_CONFIG_FILE" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Generating development configuration..."; \
		go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }; \
	fi && \
	$(MAKE) oauth-stub-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) server-start && \
	CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) process-wait; \
	rm -f .config.tmp.mk
	$(call log_success,"Environment setup completed")

# Authenticate a user and save credentials to JSON file
# Usage: make stepci-auth-user user=alice
stepci-auth-user:
	$(call log_info,Authenticating user: $(user))
	@if [ -z "$(user)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) Usage: make stepci-auth-user user=<username>"; \
		echo -e "$(BLUE)[INFO]$(NC) Example: make stepci-auth-user user=alice"; \
		exit 1; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Initiating OAuth flow for user: $(user)..."
	@curl -sL "http://localhost:8080/oauth2/authorize/test?user_hint=$(user)&client_callback=http://localhost:8079/" > /dev/null || { \
		echo -e "$(RED)[ERROR]$(NC) Failed to initiate OAuth flow for $(user)"; \
		exit 1; \
	}
	@sleep 2
	@echo -e "$(BLUE)[INFO]$(NC) Retrieving credentials for user: $(user)..."
	@CREDS=$$(curl -s "http://localhost:8079/creds?userid=$(user)" 2>/dev/null); \
	if [ -z "$$CREDS" ] || echo "$$CREDS" | grep -q '"error"'; then \
		echo -e "$(RED)[ERROR]$(NC) Failed to retrieve credentials for user $(user)"; \
		echo -e "$(BLUE)[INFO]$(NC) Response: $$CREDS"; \
		exit 1; \
	fi; \
	mkdir -p tmp; \
	echo "$$CREDS" > "tmp/$(user).json"; \
	echo -e "$(GREEN)[SUCCESS]$(NC) Credentials saved to tmp/$(user).json"
	@if [ -f "tmp/$(user).json" ]; then \
		echo -e "$(BLUE)[INFO]$(NC) Credential summary for $(user):"; \
		jq -r '"User: " + (.email // "unknown") + ", Token expires: " + (.expires_in // "unknown") + "s"' "tmp/$(user).json" 2>/dev/null || echo "Raw credentials saved"; \
	fi

# Complete StepCI preparation - cleanup, setup, and authenticate test users
stepci-prep:
	$(call log_info,Preparing complete environment for StepCI API tests...)
	@echo -e "$(BLUE)[INFO]$(NC) Step 1: Cleaning up any existing environment..."
	@$(MAKE) stepci-cleanup
	@echo -e "$(BLUE)[INFO]$(NC) Step 2: Setting up fresh environment..."
	@$(MAKE) stepci-setup
	@echo -e "$(BLUE)[INFO]$(NC) Step 3: Authenticating test users..."
	@$(MAKE) stepci-auth-user user=alice
	@$(MAKE) stepci-auth-user user=bob
	@$(MAKE) stepci-auth-user user=chuck
	@echo -e "$(GREEN)[SUCCESS]$(NC) StepCI environment preparation completed!"
	@echo -e "$(BLUE)[INFO]$(NC) Environment ready:"
	@echo -e "$(BLUE)[INFO]$(NC)   - Server running on http://localhost:8080"
	@echo -e "$(BLUE)[INFO]$(NC)   - OAuth stub running on http://localhost:8079"
	@echo -e "$(BLUE)[INFO]$(NC)   - Database and Redis containers running"
	@echo -e "$(BLUE)[INFO]$(NC)   - Test user credentials: tmp/alice.json, tmp/bob.json, tmp/chuck.json"
	@echo -e "$(BLUE)[INFO]$(NC) To clean up when done: make stepci-cleanup"

# Run StepCI tests using pre-generated credentials  
# Usage: make run-stepci-test file=stepci/workflow.yml
run-stepci-test:
	$(call log_info,Running StepCI test with pre-generated credentials...)
	@if [ -z "$(file)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) Usage: make run-stepci-test file=<test-file>"; \
		echo -e "$(BLUE)[INFO]$(NC) Example: make run-stepci-test file=stepci/workflow.yml"; \
		exit 1; \
	fi
	@if [ ! -f "$(file)" ]; then \
		echo -e "$(RED)[ERROR]$(NC) Test file not found: $(file)"; \
		exit 1; \
	fi
	@echo -e "$(BLUE)[INFO]$(NC) Running StepCI test: $(file)"
	@./scripts/run-stepci-with-creds.sh run "$(file)"
	$(call log_success,"StepCI test completed: $(file)")

# Run all modified StepCI tests (uses pre-generated credentials)
run-stepci-tests:
	$(call log_info,Running all modified StepCI tests with pre-generated credentials...)
	@echo -e "$(BLUE)[INFO]$(NC) Running StepCI tests with pre-generated credentials..."
	@for test_file in \
		stepci/workflow.yml \
		stepci/threat-models/crud-operations.yml \
		stepci/threat-models/search-filtering.yml \
		stepci/threat-models/validation-failures.yml \
		stepci/threats/crud-operations.yml \
		stepci/threats/bulk-operations.yml \
		stepci/diagrams/collaboration.yml \
		stepci/integration/full-workflow.yml \
		stepci/integration/rbac-permissions.yml \
		stepci/auth/oauth-env-test.yml \
		stepci/auth/user-operations.yml; do \
		echo -e "$(BLUE)[INFO]$(NC) Running: $$test_file"; \
		if ./scripts/run-stepci-with-creds.sh run "$$test_file"; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) $$test_file passed"; \
		else \
			echo -e "$(RED)[ERROR]$(NC) $$test_file failed"; \
		fi; \
		echo ""; \
		sleep 1; \
	done
	$(call log_success,"All StepCI tests completed")

# ============================================================================
# BACKWARD COMPATIBILITY ALIASES
# ============================================================================

.PHONY: build build-all test lint clean dev prod test-api obs-start obs-stop obs-clean obs-health obs-wait

# Keep backward compatibility with existing commands
build: build-server
build-all: build-server
test: test-unit
lint:
	@golangci-lint run
clean: build-clean
dev: dev-start
prod: dev-start  # For now, prod is same as dev

# Observability aliases (obs-* shortcuts)
obs-start: observability-start
obs-stop: observability-stop
obs-clean: observability-clean
obs-health: observability-health
obs-wait: observability-wait

# StepCI alias
test-stepci: stepci-full

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
		AUTH_REDIRECT=$$(curl -s "http://localhost:$$SERVER_PORT/oauth2/authorize/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			echo -e "$(GREEN)[SUCCESS]$(NC) Token:"; \
			curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null || curl -s "$$AUTH_REDIRECT"; \
		else \
			echo -e "$(RED)[ERROR]$(NC) Failed to get OAuth authorization redirect"; \
		fi; \
	else \
		echo -e "$(BLUE)[INFO]$(NC) Testing authenticated access..."; \
		AUTH_REDIRECT=$$(curl -s "http://localhost:$$SERVER_PORT/oauth2/authorize/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
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
	@echo "  observability-start    - Start OpenTelemetry monitoring stack (alias: obs-start)"
	@echo "  observability-stop     - Stop monitoring services (alias: obs-stop)"
	@echo "  observability-clean    - Clean monitoring stack with data removal (alias: obs-clean)"
	@echo "  test-telemetry         - Test telemetry components with monitoring stack"
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
	@echo "  config/stepci-full.yml         - StepCI testing configuration"
	@echo "  config/observability.yml       - OpenTelemetry observability stack configuration"
	@echo ""

list-targets:
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {print $$1}' | grep -v '^Makefile$$' | sort