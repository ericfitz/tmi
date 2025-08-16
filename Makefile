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
	$(call log_info,"Starting PostgreSQL container...")
	@if [ -z "$(INFRASTRUCTURE_POSTGRES_CONTAINER)" ]; then \
		echo "Error: PostgreSQL configuration not loaded. Set INFRASTRUCTURE_POSTGRES_CONTAINER variable."; \
		exit 1; \
	fi
	@if ! docker ps -a --format "{{.Names}}" | grep -q "^$(INFRASTRUCTURE_POSTGRES_CONTAINER)$$"; then \
		$(call log_info,"Creating new PostgreSQL container..."); \
		docker run -d \
			--name $(INFRASTRUCTURE_POSTGRES_CONTAINER) \
			-p $(INFRASTRUCTURE_POSTGRES_PORT):5432 \
			-e POSTGRES_USER=$(INFRASTRUCTURE_POSTGRES_USER) \
			-e POSTGRES_PASSWORD=$(INFRASTRUCTURE_POSTGRES_PASSWORD) \
			-e POSTGRES_DB=$(INFRASTRUCTURE_POSTGRES_DATABASE) \
			$(INFRASTRUCTURE_POSTGRES_IMAGE); \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$(INFRASTRUCTURE_POSTGRES_CONTAINER)$$"; then \
		$(call log_info,"Starting existing PostgreSQL container..."); \
		docker start $(INFRASTRUCTURE_POSTGRES_CONTAINER); \
	fi
	$(call log_success,"PostgreSQL container is running on port $(INFRASTRUCTURE_POSTGRES_PORT)")

infra-db-stop:
	$(call log_info,"Stopping PostgreSQL container: $(INFRASTRUCTURE_POSTGRES_CONTAINER)")
	@docker stop $(INFRASTRUCTURE_POSTGRES_CONTAINER) 2>/dev/null || true
	$(call log_success,"PostgreSQL container stopped")

infra-db-clean:
	$(call log_warning,"Removing PostgreSQL container and data: $(INFRASTRUCTURE_POSTGRES_CONTAINER)")
	@docker rm -f $(INFRASTRUCTURE_POSTGRES_CONTAINER) 2>/dev/null || true
	$(call log_success,"PostgreSQL container and data removed")

infra-redis-start:
	$(call log_info,"Starting Redis container...")
	@if [ -z "$(INFRASTRUCTURE_REDIS_CONTAINER)" ]; then \
		echo "Error: Redis configuration not loaded. Set INFRASTRUCTURE_REDIS_CONTAINER variable."; \
		exit 1; \
	fi
	@if ! docker ps -a --format "{{.Names}}" | grep -q "^$(INFRASTRUCTURE_REDIS_CONTAINER)$$"; then \
		$(call log_info,"Creating new Redis container..."); \
		docker run -d \
			--name $(INFRASTRUCTURE_REDIS_CONTAINER) \
			-p $(INFRASTRUCTURE_REDIS_PORT):6379 \
			$(INFRASTRUCTURE_REDIS_IMAGE); \
	elif ! docker ps --format "{{.Names}}" | grep -q "^$(INFRASTRUCTURE_REDIS_CONTAINER)$$"; then \
		$(call log_info,"Starting existing Redis container..."); \
		docker start $(INFRASTRUCTURE_REDIS_CONTAINER); \
	fi
	$(call log_success,"Redis container is running on port $(INFRASTRUCTURE_REDIS_PORT)")

infra-redis-stop:
	$(call log_info,"Stopping Redis container: $(INFRASTRUCTURE_REDIS_CONTAINER)")
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

.PHONY: build-server build-migrate build-clean

build-server:
	$(call log_info,"Building server binary...")
	@go build -o bin/server github.com/ericfitz/tmi/cmd/server
	$(call log_success,"Server binary built: bin/server")

build-migrate:
	$(call log_info,"Building migration tool...")
	@go build -o bin/migrate github.com/ericfitz/tmi/cmd/migrate
	$(call log_success,"Migration tool built: bin/migrate")

build-clean:
	$(call log_info,"Cleaning build artifacts...")
	@rm -rf ./bin/*
	@rm -f check-db migrate
	$(call log_success,"Build artifacts cleaned")

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
	while [ $$timeout -gt 0 ]; do \
		if docker exec $(INFRASTRUCTURE_POSTGRES_CONTAINER) pg_isready -U $(INFRASTRUCTURE_POSTGRES_USER) >/dev/null 2>&1; then \
			$(call log_success,"Database is ready!"); \
			break; \
		fi; \
		echo "⏳ Waiting for database... ($$timeout seconds remaining)"; \
		sleep 2; \
		timeout=$$((timeout - 2)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		$(call log_error,"Database failed to start within $${TIMEOUTS_DB_READY:-30} seconds"); \
		exit 1; \
	fi

# ============================================================================
# ATOMIC COMPONENTS - Process Management
# ============================================================================

.PHONY: process-stop process-wait server-start server-stop observability-wait observability-health

process-stop:
	$(call log_info,"Killing processes on port $(SERVER_PORT)")
	@if [ -z "$(SERVER_PORT)" ]; then \
		echo "Error: SERVER_PORT not configured"; \
		exit 1; \
	fi; \
	PIDS=$$(lsof -ti :$(SERVER_PORT) 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo "Found processes on port $(SERVER_PORT): $$PIDS"; \
		for PID in $$PIDS; do \
			echo "Stopping process $$PID listening on port $(SERVER_PORT)..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Force killing process $$PID..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
		echo "All processes on port $(SERVER_PORT) have been killed"; \
	else \
		echo "No processes found listening on port $(SERVER_PORT)"; \
	fi

server-start:
	$(call log_info,"Starting server on port $(SERVER_PORT)")
	@if [ -z "$(SERVER_BINARY)" ]; then \
		echo "Error: SERVER_BINARY not configured"; \
		exit 1; \
	fi; \
	if [ -n "$(SERVER_TAGS)" ]; then \
		echo "Starting server with build tags: $(SERVER_TAGS)"; \
		go run -tags $(SERVER_TAGS) cmd/server/main.go --config=$(SERVER_CONFIG_FILE) > $(SERVER_LOG_FILE) 2>&1 & \
	else \
		echo "Starting server binary: $(SERVER_BINARY)"; \
		$(SERVER_BINARY) --config=$(SERVER_CONFIG_FILE) > $(SERVER_LOG_FILE) 2>&1 & \
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
	while [ $$timeout -gt 0 ]; do \
		if curl -s http://localhost:$(SERVER_PORT)/ >/dev/null 2>&1; then \
			$(call log_success,"Server is ready!"); \
			break; \
		fi; \
		echo "⏳ Waiting for server... ($$timeout seconds remaining)"; \
		sleep 2; \
		timeout=$$((timeout - 2)); \
	done; \
	if [ $$timeout -le 0 ]; then \
		$(call log_error,"Server failed to start within $${TIMEOUTS_SERVER_READY:-30} seconds"); \
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
				$(call log_info,"Removing file: $$file"); \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	@if [ -n "$(ARTIFACTS_LOG_FILES)" ] && [ "$(ARTIFACTS_LOG_FILES)" != "" ]; then \
		for file in $(ARTIFACTS_LOG_FILES); do \
			if [ -f "$$file" ]; then \
				$(call log_info,"Removing log file: $$file"); \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	@if [ -n "$(ARTIFACTS_PID_FILES)" ] && [ "$(ARTIFACTS_PID_FILES)" != "" ]; then \
		for file in $(ARTIFACTS_PID_FILES); do \
			if [ -f "$$file" ]; then \
				$(call log_info,"Removing PID file: $$file"); \
				rm -f "$$file"; \
			fi; \
		done; \
	fi
	$(call log_success,"File cleanup completed")

clean-containers:
	$(call log_info,"Cleaning up containers...")
	@if [ -n "$(CLEANUP_CONTAINERS)" ] && [ "$(CLEANUP_CONTAINERS)" != "" ]; then \
		for container in $(CLEANUP_CONTAINERS); do \
			$(call log_info,"Stopping and removing container: $$container"); \
			docker stop $$container 2>/dev/null || true; \
			docker rm $$container 2>/dev/null || true; \
		done; \
	fi
	$(call log_success,"Container cleanup completed")

clean-processes:
	$(call log_info,"Cleaning up processes...")
	@if [ -n "$(CLEANUP_PROCESSES)" ] && [ "$(CLEANUP_PROCESSES)" != "" ]; then \
		for port in $(CLEANUP_PROCESSES); do \
			$(call log_info,"Killing processes on port: $$port"); \
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
	$(call log_info,"Loading configuration from $$CONFIG_FILE"); \
	uv run scripts/yaml-to-make.py $$CONFIG_FILE > .config.tmp.mk; \
	$(call log_info,"Starting unit tests..."); \
	TMI_LOGGING_IS_TEST=true go test -short ./... -v; \
	rm -f .config.tmp.mk integration-test.log server.log .server.pid

# Integration Testing - Full environment with database and server
test-integration:
	$(call load-config,test-integration)
	$(call log_info,"Starting integration tests with configuration: $(NAME)")
	@trap 'make -f $(MAKEFILE_LIST) clean-all CONFIG_FILE=config/test-integration.yml' EXIT; \
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
	$(call load-config,dev-environment)
	$(call log_info,"Starting development environment: $(NAME)")
	@CONFIG_FILE=config/dev-environment.yml INFRASTRUCTURE_POSTGRES_CONTAINER=$(INFRASTRUCTURE_POSTGRES_CONTAINER) INFRASTRUCTURE_POSTGRES_PORT=$(INFRASTRUCTURE_POSTGRES_PORT) INFRASTRUCTURE_POSTGRES_USER=$(INFRASTRUCTURE_POSTGRES_USER) INFRASTRUCTURE_POSTGRES_PASSWORD=$(INFRASTRUCTURE_POSTGRES_PASSWORD) INFRASTRUCTURE_POSTGRES_DATABASE=$(INFRASTRUCTURE_POSTGRES_DATABASE) INFRASTRUCTURE_POSTGRES_IMAGE=$(INFRASTRUCTURE_POSTGRES_IMAGE) $(MAKE) -f $(MAKEFILE_LIST) infra-db-start
	@CONFIG_FILE=config/dev-environment.yml INFRASTRUCTURE_REDIS_CONTAINER=$(INFRASTRUCTURE_REDIS_CONTAINER) INFRASTRUCTURE_REDIS_PORT=$(INFRASTRUCTURE_REDIS_PORT) INFRASTRUCTURE_REDIS_IMAGE=$(INFRASTRUCTURE_REDIS_IMAGE) $(MAKE) -f $(MAKEFILE_LIST) infra-redis-start
	@CONFIG_FILE=config/dev-environment.yml INFRASTRUCTURE_POSTGRES_CONTAINER=$(INFRASTRUCTURE_POSTGRES_CONTAINER) INFRASTRUCTURE_POSTGRES_USER=$(INFRASTRUCTURE_POSTGRES_USER) TIMEOUTS_DB_READY=$(TIMEOUTS_DB_READY) $(MAKE) -f $(MAKEFILE_LIST) db-wait
	@go build -o bin/check-db cmd/check-db/main.go
	@CONFIG_FILE=config/dev-environment.yml POSTGRES_URL=$(POSTGRES_URL) $(MAKE) -f $(MAKEFILE_LIST) db-migrate
	@if [ ! -f $(SERVER_CONFIG_FILE) ]; then \
		$(call log_info,"Generating development configuration..."); \
		go run cmd/server/main.go --generate-config || { echo "Error: Failed to generate config files"; exit 1; }; \
	fi
	@CONFIG_FILE=config/dev-environment.yml SERVER_PORT=$(SERVER_PORT) SERVER_BINARY=$(SERVER_BINARY) SERVER_CONFIG_FILE=$(SERVER_CONFIG_FILE) $(MAKE) -f $(MAKEFILE_LIST) server-start
	$(call log_success,"Development environment started on port $(SERVER_PORT)")

# Development Environment Cleanup
dev-clean:
	$(call load-config,dev-environment)
	$(call log_info,"Cleaning development environment: $(NAME)")
	@CONFIG_FILE=config/dev-environment.yml $(MAKE) -f $(MAKEFILE_LIST) clean-all

# Coverage Report Generation - Comprehensive testing with coverage
test-coverage:
	$(call load-config,coverage-report)
	$(call log_info,"Generating coverage reports: $(NAME)")
	@trap 'make clean-all CONFIG_FILE=$(CONFIG_FILE)' EXIT; \
	$(MAKE) clean-all && \
	mkdir -p $(COVERAGE_DIRECTORY) && \
	$(MAKE) test-coverage-unit && \
	$(MAKE) infra-db-start && \
	$(MAKE) infra-redis-start && \
	$(MAKE) db-wait && \
	$(MAKE) db-migrate && \
	$(MAKE) test-coverage-integration && \
	$(MAKE) coverage-merge && \
	$(MAKE) coverage-reports

# StepCI Full Integration Testing
stepci-full:
	$(call load-config,stepci-full)
	$(call log_info,"Running StepCI full integration tests: $(NAME)")
	@trap 'make clean-all CONFIG_FILE=$(CONFIG_FILE)' EXIT; \
	$(MAKE) clean-processes && \
	$(MAKE) infra-db-start && \
	$(MAKE) infra-redis-start && \
	$(MAKE) db-wait && \
	$(MAKE) build-server && \
	$(MAKE) db-migrate && \
	$(MAKE) server-start && \
	$(MAKE) process-wait && \
	$(MAKE) stepci-execute

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
	$(call log_info,"Executing StepCI tests...")
	@if [ -n "$(TEST_PATTERN)" ] && [ "$(TEST_PATTERN)" != "" ]; then \
		$(call log_info,"Running specific StepCI test: $(TEST_PATTERN)"); \
		if [ ! -f "$(STEPCI_TEST_DIRECTORY)/$(TEST_PATTERN)" ]; then \
			$(call log_error,"Test file not found: $(STEPCI_TEST_DIRECTORY)/$(TEST_PATTERN)"); \
			$(call log_info,"Available tests:"); \
			find $(STEPCI_TEST_DIRECTORY) -name "*.yml" -not -path "$(STEPCI_EXCLUDE_PATHS)" | sed 's|$(STEPCI_TEST_DIRECTORY)/||' | sort; \
			exit 1; \
		fi; \
		stepci run "$(STEPCI_TEST_DIRECTORY)/$(TEST_PATTERN)"; \
	else \
		$(call log_info,"Running all StepCI integration tests..."); \
		for test_file in $$(find $(STEPCI_TEST_DIRECTORY) -name "*.yml" -not -path "$(STEPCI_EXCLUDE_PATHS)" | sort); do \
			$(call log_info,"Running: $$test_file"); \
			stepci run "$$test_file" || echo "❌ Test failed: $$test_file"; \
			echo ""; \
		done; \
	fi
	$(call log_success,"StepCI tests completed")

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
	@echo ""
	@echo "🎉 Observability stack started!"
	@echo ""
	@echo "Services:"
	@echo "  📊 Grafana:     $(GRAFANA_URL) (admin/admin)"
	@echo "  🔍 Jaeger:      $(JAEGER_URL)"
	@echo "  📈 Prometheus:  $(PROMETHEUS_URL)"
	@echo "  📋 Loki:       $(LOKI_URL)"
	@echo "  📡 OTel:       http://localhost:$(OTEL_COLLECTOR_PORT) (HTTP) / :$(OTEL_COLLECTOR_GRPC_PORT) (gRPC)"
	@echo ""
	@echo "To stop: make observability-stop"
	@echo "To view logs: docker-compose -f $(OBSERVABILITY_COMPOSE_FILE) logs -f [service]"

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

# Legacy test-api target (simplified)
test-api:
	$(call load-config,dev-environment)
	@$(call log_info,"Testing API endpoints...")
	@if [ "$(auth)" = "only" ]; then \
		$(call log_info,"Getting JWT token via OAuth test provider..."); \
		AUTH_REDIRECT=$$(curl -s "http://localhost:$(SERVER_PORT)/auth/login/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			$(call log_success,"Token:"); \
			curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null || curl -s "$$AUTH_REDIRECT"; \
		else \
			$(call log_error,"Failed to get OAuth authorization redirect"); \
		fi; \
	else \
		$(call log_info,"Testing authenticated access..."); \
		AUTH_REDIRECT=$$(curl -s "http://localhost:$(SERVER_PORT)/auth/login/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			TOKEN=$$(curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null); \
			if [ "$$TOKEN" != "null" ] && [ -n "$$TOKEN" ]; then \
				$(call log_success,"Got token: $$TOKEN"); \
				$(call log_info,"Testing /threat_models endpoint..."); \
				curl -H "Authorization: Bearer $$TOKEN" "http://localhost:$(SERVER_PORT)/threat_models" | jq .; \
			fi; \
		fi; \
	fi

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