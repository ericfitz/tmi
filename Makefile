.PHONY: build test test-one single-test lint clean dev prod dev-db dev-redis stop-db stop-redis delete-db delete-redis dev-app build-postgres build-redis gen-config dev-observability stop-observability delete-observability test-telemetry benchmark-telemetry validate-otel-config test-integration test-integration-cleanup coverage coverage-unit coverage-integration coverage-report ensure-migrations check-migrations migrate list

# Default build target
VERSION := 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "development")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

build:
	go build -o bin/server github.com/ericfitz/tmi/cmd/server

# List all available make targets
list:
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {print $$1}' | sort

# Run tests with test configuration
test:
	@TEST_CMD="TMI_LOGGING_IS_TEST=true go test ./..."; \
	if [ "$(count1)" = "true" ]; then \
		TEST_CMD="$$TEST_CMD --count=1"; \
	fi; \
	if [ "$(passfail)" = "true" ]; then \
		eval $$TEST_CMD | grep -E "FAIL|PASS"; \
	else \
		eval $$TEST_CMD; \
	fi

# Run specific test
test-one:
	@if [ -z "$(name)" ]; then \
		echo "Usage: make test-one name=TestName"; \
		exit 1; \
	fi
	TMI_LOGGING_IS_TEST=true go test ./... -run $(name)

# Run single test with verbose output in api package
single-test:
	@if [ -z "$(name)" ]; then \
		echo "Usage: make single-test name=TestName"; \
		exit 1; \
	fi
	TMI_LOGGING_IS_TEST=true go test ./api -v -run $(name)

# Run integration tests with automatic database setup
test-integration:
	@echo "Running integration tests with automatic database setup..."
	./scripts/integration-test.sh

# Cleanup integration test containers only
test-integration-cleanup:
	@echo "Cleaning up integration test containers..."
	./scripts/integration-test.sh --cleanup-only

# Run linter
lint:
	golangci-lint run

# Generate API from OpenAPI spec
gen-api:
	oapi-codegen -config oapi-codegen-config.yaml tmi-openapi.json

# Clean build artifacts
clean:
	rm -rf ./bin/*

# Start development environment
dev:
	@echo "Starting TMI development environment..."
	@./scripts/start-dev.sh

# Start production environment
prod:
	@echo "Starting TMI production environment..."
	@./scripts/start-prod.sh

# Development Database and Cache Management

# Start development database only
dev-db:
	@echo "Starting development database..."
	@./scripts/start-dev-db.sh

# Start development Redis only
dev-redis:
	@echo "Starting development Redis..."
	@./scripts/start-dev-redis.sh

# Stop development database (preserves data)
stop-db:
	@echo "Stopping development database..."
	@docker stop tmi-postgresql || true

# Stop development Redis (preserves data)
stop-redis:
	@echo "Stopping development Redis..."
	@docker stop tmi-redis || true

# Delete development database (removes container and data)
delete-db:
	@echo "🗑️  Deleting development database (container and data)..."
	@docker rm -f tmi-postgresql || true
	@echo "✅ Database container removed!"

# Delete development Redis (removes container and data) 
delete-redis:
	@echo "🗑️  Deleting development Redis (container and data)..."
	@docker rm -f tmi-redis || true
	@echo "✅ Redis container removed!"

# Build development Docker container for app
dev-app:
	@echo "Building TMI development Docker container..."
	docker build -f Dockerfile.dev -t tmi-app .

# Build custom PostgreSQL Docker container
build-postgres:
	@echo "Building custom PostgreSQL Docker container..."
	docker build -f Dockerfile.postgres -t tmi-postgres .

# Build custom Redis Docker container
build-redis:
	@echo "Building custom Redis Docker container..."
	docker build -f Dockerfile.redis -t tmi-redis .

# Generate configuration files
gen-config:
	@echo "Generating configuration files..."
	go run github.com/ericfitz/tmi/cmd/server --generate-config

# OpenTelemetry and Observability Stack Management

# Start local observability stack (Grafana, Prometheus, Jaeger, Loki, OpenTelemetry Collector)
dev-observability:
	@echo "Starting TMI Observability Stack..."
	@./scripts/start-observability.sh

# Stop observability stack (preserves data volumes)
stop-observability:
	@echo "Stopping TMI Observability Stack..."
	@./scripts/stop-observability.sh

# Delete observability stack (removes containers, volumes, and networks - destroys all data)
delete-observability:
	@echo "🗑️  Deleting TMI Observability Stack (containers, volumes, networks)..."
	@docker-compose -f docker-compose.observability.yml down -v --remove-orphans
	@echo "✅ Observability stack completely removed!"

# Run telemetry-specific tests
test-telemetry:
	@echo "Running telemetry tests..."
	go test ./internal/telemetry/... -v

# Run telemetry integration tests
test-telemetry-integration:
	@echo "Running telemetry integration tests..."
	go test ./internal/telemetry/... -tags=integration -v

# Run telemetry benchmarks
benchmark-telemetry:
	@echo "Running telemetry benchmarks..."
	go test ./internal/telemetry/... -bench=. -benchmem

# Validate OpenTelemetry configuration
validate-otel-config:
	@echo "Validating OpenTelemetry configuration..."
	go run ./internal/telemetry/cmd/validate-config

# Generate sample telemetry data for testing
generate-telemetry-data:
	@echo "Generating sample telemetry data..."
	go run ./internal/telemetry/cmd/generate-data

# Export telemetry data
export-telemetry:
	@echo "Exporting telemetry data..."
	curl -s http://localhost:8080/metrics > /tmp/tmi-metrics.txt
	@echo "Metrics exported to /tmp/tmi-metrics.txt"

# Clean up telemetry data (alias for delete-observability)
clean-telemetry: delete-observability

# Generate comprehensive test coverage report (unit + integration)
coverage:
	@echo "Generating comprehensive test coverage report..."
	./scripts/coverage-report.sh

# Generate unit test coverage only
coverage-unit:
	@echo "Generating unit test coverage report..."
	./scripts/coverage-report.sh --unit-only

# Generate integration test coverage only
coverage-integration:
	@echo "Generating integration test coverage report..."
	./scripts/coverage-report.sh --integration-only

# Generate coverage report without HTML
coverage-report:
	@echo "Generating coverage report (no HTML)..."
	./scripts/coverage-report.sh --no-html

# Database and Migration Management

# Ensure all database migrations are applied (with auto-fix)
ensure-migrations:
	@echo "Ensuring all database migrations are applied..."
	@./scripts/ensure-migrations.sh

# Check migration state without auto-fix
check-migrations:
	@echo "Checking database migration state..."
	@cd cmd/check-db && go run main.go

# Run database migrations manually
migrate:
	@echo "Running database migrations..."
	@cd cmd/migrate && go run main.go up

# Reset database (interactive confirmation - destroys all data)
reset-db:
	@echo "⚠️  WARNING: This will destroy all database data!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm && [ "$$confirm" = "yes" ] || exit 1
	@$(MAKE) delete-db
	@echo "Database reset. Run 'make dev-db' to create a fresh database."