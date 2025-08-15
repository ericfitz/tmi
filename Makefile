.PHONY: build-server build-check-db run-tests test-unit test-integration test-integration-cleanup run-lint clean-artifacts start-dev start-prod start-dev-db start-dev-redis stop-dev-db stop-dev-redis delete-dev-db delete-dev-redis build-dev-app build-postgres build-redis generate-api generate-config start-observability stop-observability delete-observability test-telemetry benchmark-telemetry validate-otel-config validate-asyncapi validate-openapi list-openapi-endpoints report-coverage report-coverage-unit report-coverage-integration generate-coverage-report ensure-migrations check-migrations run-migrations reset-database test-api test-api-full test-dev test-dev-full test-collaboration-permissions test-collaboration-permissions-v2 clean-dev-env debug-auth-endpoints list-targets sync-shared push-shared subtree-help

# Backward compatibility aliases
.PHONY: build test lint clean dev prod dev-db dev-redis stop-db stop-redis delete-db delete-redis dev-app gen-api gen-config dev-observability coverage coverage-unit coverage-integration coverage-report migrate reset-db dev-test dev-test-full clean-dev openapi-endpoints list

# Default build target
VERSION := 0.9.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "development")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

build-server:
	go build -o bin/server github.com/ericfitz/tmi/cmd/server

# Build check-db executable
build-check-db:
	go build -o check-db cmd/check-db/main.go

# List all available make targets
list-targets:
	@make -qp | awk -F':' '/^[a-zA-Z0-9][^$$#\/\t=]*:([^=]|$$)/ {print $$1}' | sort

# Run unit tests (fast, no external dependencies)
test-unit:
	@if [ -n "$(name)" ]; then \
		echo "Running specific unit test: $(name)"; \
		TMI_LOGGING_IS_TEST=true go test -short ./... -run $(name) -v; \
	else \
		echo "Running all unit tests..."; \
		TEST_CMD="TMI_LOGGING_IS_TEST=true go test -short ./..."; \
		if [ "$(count1)" = "true" ]; then \
			TEST_CMD="$$TEST_CMD --count=1"; \
		fi; \
		if [ "$(passfail)" = "true" ]; then \
			eval $$TEST_CMD | grep -E "FAIL|PASS"; \
		else \
			eval $$TEST_CMD; \
		fi; \
	fi

# Run integration tests with complete environment setup
test-integration:
	@if [ -n "$(name)" ]; then \
		./scripts/start-integration-tests.sh --test-name $(name); \
	else \
		./scripts/start-integration-tests.sh; \
	fi

# Cleanup integration test environment (containers and server)
test-integration-cleanup:
	@echo "🧹 Cleaning up integration test environment..."
	@if [ -f .integration-server.pid ]; then \
		PID=$$(cat .integration-server.pid); \
		if ps -p $$PID > /dev/null 2>&1; then \
			echo "Stopping integration server (PID: $$PID)..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 2; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Force killing integration server (PID: $$PID)..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		fi; \
		rm -f .integration-server.pid; \
	fi
	@echo "Checking for processes listening on port 8081..."
	@PIDS=$$(lsof -ti :8081 2>/dev/null || true); \
	if [ -n "$$PIDS" ]; then \
		echo "Found processes on port 8081: $$PIDS"; \
		for PID in $$PIDS; do \
			echo "Stopping process $$PID listening on port 8081..."; \
			kill $$PID 2>/dev/null || true; \
			sleep 1; \
			if ps -p $$PID > /dev/null 2>&1; then \
				echo "Force killing process $$PID..."; \
				kill -9 $$PID 2>/dev/null || true; \
			fi; \
		done; \
	fi
	@echo "Stopping integration test containers only..."
	@docker stop tmi-integration-postgres tmi-integration-redis 2>/dev/null || true
	@docker rm tmi-integration-postgres tmi-integration-redis 2>/dev/null || true
	@rm -f server-integration.log integration-test.log config-integration-test.yaml
	@echo "✅ Cleanup completed"

# Alias for backward compatibility
run-tests: test-unit

# Run linter
run-lint:
	golangci-lint run

# Generate API from OpenAPI spec
generate-api:
	oapi-codegen -config oapi-codegen-config.yaml shared/api-specs/tmi-openapi.json

# Note: AsyncAPI Go types are manually implemented in api/asyncapi_types.go
# due to asyncapi-codegen parsing issues with AsyncAPI v3.0 specifications

# Validate AsyncAPI WebSocket specification
validate-asyncapi:
	uv run scripts/validate_asyncapi.py tmi-asyncapi.yaml

# Validate OpenAPI specification with comprehensive analysis
validate-openapi:
	@FILE=$${file:-shared/api-specs/tmi-openapi.json}; \
	if [ ! -f "$$FILE" ]; then \
		echo "❌ File not found: $$FILE"; \
		echo "Usage: make validate-openapi [file=path/to/openapi.json]"; \
		exit 1; \
	fi; \
	echo "🔍 Validating OpenAPI specification: $$FILE"; \
	echo "1️⃣  Checking JSON syntax..."; \
	python -m json.tool "$$FILE" > /dev/null && echo "✅ JSON syntax is valid"; \
	echo "2️⃣  Running detailed OpenAPI analysis..."; \
	uv run scripts/validate_openapi.py "$$FILE"

# List OpenAPI endpoints with HTTP methods and response codes
list-openapi-endpoints:
	@FILE=$${file:-shared/api-specs/tmi-openapi.json}; \
	if [ ! -f "$$FILE" ]; then \
		echo "❌ File not found: $$FILE"; \
		echo "Usage: make list-openapi-endpoints [file=path/to/openapi.json]"; \
		exit 1; \
	fi; \
	echo "📋 OpenAPI Endpoints from $$FILE:"; \
	echo ""; \
	jq -r ' \
		.paths \
		| with_entries(select(.value | type == "object")) \
		| to_entries[] \
		| .key as $$path \
		| .value \
		| with_entries(select(.value | type == "object")) \
		| to_entries[] \
		| select(.key | ascii_downcase | IN("get", "post", "put", "delete", "patch", "options", "head", "trace")) \
		| select(.value.responses | type == "object") \
		| "\($$path) \(.key | ascii_upcase): \( [.value.responses | keys_unsorted[] // "none"] | join(", ") )" \
	' "$$FILE" | sort

# Clean build artifacts
clean-artifacts:
	rm -rf ./bin/*
	rm -f check-db

# Start development environment
start-dev: build-check-db
	@echo "Starting TMI development environment..."
	@./scripts/start-dev.sh

# Start production environment
start-prod:
	@echo "Starting TMI production environment..."
	@./scripts/start-prod.sh

# Development Database and Cache Management

# Start development database only
start-dev-db:
	@echo "Starting development database..."
	@./scripts/start-dev-db.sh
	@echo "Applying database migrations..."
	@cd cmd/migrate && go run main.go up

# Start development Redis only
start-dev-redis:
	@echo "Starting development Redis..."
	@./scripts/start-dev-redis.sh

# Stop development database (preserves data)
stop-dev-db:
	@echo "Stopping development database..."
	@docker stop tmi-postgresql || true

# Stop development Redis (preserves data)
stop-dev-redis:
	@echo "Stopping development Redis..."
	@docker stop tmi-redis || true

# Delete development database (removes container and data)
delete-dev-db:
	@echo "🗑️  Deleting development database (container and data)..."
	@docker rm -f tmi-postgresql || true
	@echo "✅ Database container removed!"

# Delete development Redis (removes container and data) 
delete-dev-redis:
	@echo "🗑️  Deleting development Redis (container and data)..."
	@docker rm -f tmi-redis || true
	@echo "✅ Redis container removed!"

# Build development Docker container for app
build-dev-app:
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
generate-config:
	@echo "Generating configuration files..."
	go run github.com/ericfitz/tmi/cmd/server --generate-config

# OpenTelemetry and Observability Stack Management

# Start local observability stack (Grafana, Prometheus, Jaeger, Loki, OpenTelemetry Collector)
start-observability:
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

# Run telemetry tests (unit and integration)
test-telemetry:
	@if [ "$(integration)" = "true" ]; then \
		echo "Running telemetry integration tests..."; \
		go test ./internal/telemetry/... -tags=integration -v; \
	else \
		echo "Running telemetry unit tests..."; \
		go test ./internal/telemetry/... -v; \
	fi

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
report-coverage:
	@echo "Generating comprehensive test coverage report..."
	./scripts/coverage-report.sh

# Generate unit test coverage only
report-coverage-unit:
	@echo "Generating unit test coverage report..."
	./scripts/coverage-report.sh --unit-only

# Generate integration test coverage only
report-coverage-integration:
	@echo "Generating integration test coverage report..."
	./scripts/coverage-report.sh --integration-only

# Generate coverage report without HTML
generate-coverage-report:
	@echo "Generating coverage report (no HTML)..."
	./scripts/coverage-report.sh --no-html

# Database and Migration Management

# Ensure all database migrations are applied (with auto-fix)
ensure-migrations: build-check-db
	@echo "Ensuring all database migrations are applied..."
	@./scripts/ensure-migrations.sh

# Check migration state without auto-fix
check-migrations:
	@echo "Checking database migration state..."
	@cd cmd/check-db && go run main.go

# Run database migrations manually
run-migrations:
	@echo "Running database migrations..."
	@cd cmd/migrate && go run main.go up

# Reset database (interactive confirmation - destroys all data)
reset-database:
	@echo "⚠️  WARNING: This will destroy all database data!"
	@read -p "Are you sure? Type 'yes' to continue: " confirm && [ "$$confirm" = "yes" ] || exit 1
	@$(MAKE) delete-dev-db
	@echo "Database reset. Run 'make start-dev-db' to create a fresh database."

# Testing and Development Authentication Targets

# Test live API endpoints (requires running server)
test-api:
	@if [ "$(auth)" = "only" ]; then \
		echo "🔐 Getting JWT token via OAuth test provider..."; \
		AUTH_REDIRECT=$$(curl -s "http://localhost:8080/auth/login/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			echo "✅ Token:"; \
			curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null || curl -s "$$AUTH_REDIRECT"; \
		else \
			echo "❌ Failed to get OAuth authorization redirect"; \
		fi; \
	elif [ "$(noauth)" = "true" ]; then \
		echo "🚫 Testing unauthenticated access (should return 401)..."; \
		curl -v "http://localhost:8080/threat_models" 2>&1 | grep -E "(401|unauthorized)" || echo "❌ Expected 401 Unauthorized"; \
	else \
		echo "🧪 Testing API endpoints..."; \
		echo "1. Testing unauthenticated access (should fail)..."; \
		curl -v "http://localhost:8080/threat_models" 2>&1 | grep -E "(401|unauthorized)" || echo "❌ Expected 401 Unauthorized"; \
		echo ""; \
		echo "2. Testing authenticated access..."; \
		AUTH_REDIRECT=$$(curl -s "http://localhost:8080/auth/login/test" | grep -oE 'href="[^"]*"' | sed 's/href="//; s/"//' | sed 's/&amp;/\&/g'); \
		if [ -n "$$AUTH_REDIRECT" ]; then \
			TOKEN=$$(curl -s "$$AUTH_REDIRECT" | jq -r '.access_token' 2>/dev/null); \
			if [ "$$TOKEN" != "null" ] && [ -n "$$TOKEN" ]; then \
				echo "✅ Got token: $$TOKEN"; \
				echo "3. Testing /threat_models endpoint..."; \
				curl -H "Authorization: Bearer $$TOKEN" "http://localhost:8080/threat_models" | jq .; \
			else \
				echo "❌ Failed to get token from OAuth callback"; \
			fi; \
		else \
			echo "❌ Failed to get OAuth authorization redirect"; \
		fi; \
	fi

# Full API test with environment setup - kills existing server, sets up fresh environment, and runs tests
test-api-full:
	@echo "🚀 Setting up full development environment for API testing..."
	
	@echo "1. 🛑 Stopping any running server processes..."
	@pkill -f "cmd/server/main.go" || true
	@pkill -f "bin/server" || true
	@sleep 2
	
	@echo "2. 🗑️  Cleaning up existing containers..."
	@docker rm -f tmi-postgresql tmi-redis || true
	
	@echo "3. 🗃️  Starting development database..."
	@$(MAKE) start-dev-db
	
	@echo "4. 📦 Starting development Redis..."
	@$(MAKE) start-dev-redis
	
	@echo "5. ⏳ Waiting for services to be ready..."
	@sleep 5
	
	@echo "6. 🔨 Building server..."
	@$(MAKE) build-server
	
	@echo "7. 📝 Ensuring configuration files exist..."
	@if [ ! -f config-development.yaml ]; then \
		echo "   Generating development configuration..."; \
		go run cmd/server/main.go --generate-config || exit 1; \
	fi
	
	@echo "8. 🚀 Starting development server in background..."
	@nohup go run -tags dev cmd/server/main.go --config=config-development.yaml > server.log 2>&1 & \
	SERVER_PID=$$!; \
	echo "Server started with PID: $$SERVER_PID"; \
	echo "9. ⏳ Waiting for server to start..."; \
	for i in 1 2 3 4 5 6 7 8 9 10; do \
		if curl -s http://localhost:8080/health >/dev/null 2>&1; then \
			echo "✅ Server is ready!"; \
			break; \
		fi; \
		echo "   Waiting for server... (attempt $$i/10)"; \
		sleep 3; \
	done; \
	if ! curl -s http://localhost:8080/health >/dev/null 2>&1; then \
		echo "❌ Server failed to start within timeout"; \
		echo "Server log:"; \
		tail -20 server.log || true; \
		pkill -f "cmd/server/main.go" || true; \
		exit 1; \
	fi
	
	@echo "10. 🧪 Running API tests..."
	@$(MAKE) test-api || (echo "❌ API tests failed"; pkill -f "cmd/server/main.go" || true; pkill -f "bin/server" || true; exit 1)
	
	@echo "11. 🛑 Cleaning up server..."
	@pkill -f "cmd/server/main.go" || true
	@pkill -f "bin/server" || true
	
	@echo "✅ Full API test completed successfully!"

# Quick development test - builds and tests key endpoints
test-dev: build-server
	@echo "🚀 Running quick development test..."
	@if ! pgrep -f "bin/server" > /dev/null; then \
		echo "❌ Server not running. Start with 'make start-dev' first"; \
		exit 1; \
	fi
	@$(MAKE) test-api

# Full development test - sets up environment and runs development tests
test-dev-full: test-api-full

# Clean development environment (kill processes, clean DBs)
clean-dev-env:
	@echo "🧹 Cleaning development environment..."
	@./scripts/clean-dev.sh

# Test collaboration session permissions end-to-end (original version)
test-collaboration-permissions:
	@echo "🧪 Running comprehensive collaboration session permissions test..."
	@./scripts/test-collaboration-permissions.sh

# Test collaboration session permissions end-to-end (v2 - improved version)
test-collaboration-permissions-v2:
	@echo "🧪 Running comprehensive collaboration session permissions test (v2)..."
	@./scripts/test-collaboration-permissions-v2.sh

# Test session creation and join integration (OAuth flow, threat models, collaboration)
test-session-join-integration:
	@echo "🧪 Running session creation and join integration test..."
	@./scripts/test-session-join-integration.sh

# Debug auth endpoints - check what's available
debug-auth-endpoints:
	@echo "🔍 Checking available auth endpoints..."
	@echo "1. Testing /auth/login (should show dev login page):"
	@curl -s -w "\n  Status: %{http_code}\n" "http://localhost:8080/auth/login" | head -5
	@echo ""
	@echo "2. Testing /auth/callback directly:"
	@curl -s -w "\n  Status: %{http_code}\n" "http://localhost:8080/auth/callback?username=test@example.com&role=owner"
	@echo ""
	@echo "3. Testing /auth/providers:"
	@curl -s -w "\n  Status: %{http_code}\n" "http://localhost:8080/auth/providers" | head -3
	@echo ""
	@echo "4. Testing test provider auth URL:"
	@curl -s -w "\n  Status: %{http_code}\n" "http://localhost:8080/auth/login/test"
	@echo ""
	@echo "5. Getting redirect headers from test provider:"
	@curl -s -I "http://localhost:8080/auth/login/test" | grep -i location

# Backward compatibility aliases for common targets
build: build-server
test: run-tests
lint: run-lint
clean: clean-artifacts
dev: start-dev
prod: start-prod
dev-db: start-dev-db
dev-redis: start-dev-redis
stop-db: stop-dev-db
stop-redis: stop-dev-redis
delete-db: delete-dev-db
delete-redis: delete-dev-redis
dev-app: build-dev-app
gen-api: generate-api
gen-config: generate-config
dev-observability: start-observability
coverage: report-coverage
coverage-unit: report-coverage-unit
coverage-integration: report-coverage-integration
coverage-report: generate-coverage-report
migrate: run-migrations
reset-db: reset-database
dev-test: test-dev
dev-test-full: test-dev-full
clean-dev: clean-dev-env
openapi-endpoints: list-openapi-endpoints
list: list-targets

# Git Subtree Management for Shared Resources
.PHONY: sync-shared push-shared subtree-help

# Update shared directory from source files
sync-shared:
	@echo "🔄 Syncing shared directory from source files..."
	@echo "Copying API specifications..."
	@echo "⚠️  Note: tmi-openapi.json is now maintained in shared/api-specs/ (authoritative version)"
	@cp tmi-asyncapi.yaml shared/api-specs/
	@echo "Copying documentation..."
	@cp docs/CLIENT_INTEGRATION_GUIDE.md shared/docs/
	@cp docs/TMI-API-v1_0.md shared/docs/
	@cp docs/CLIENT_OAUTH_INTEGRATION.md shared/docs/
	@cp docs/AUTHORIZATION.md shared/docs/
	@cp docs/COLLABORATIVE_EDITING_PLAN.md shared/docs/
	@cp docs/*.png shared/docs/ 2>/dev/null || true
	@echo "Copying SDK examples..."
	@rm -rf shared/sdk-examples/python-sdk
	@cp -r python-sdk shared/sdk-examples/
	@echo "✅ Shared directory synchronized"

# Push shared branch to GitHub for subtree consumption
push-shared:
	@echo "🚀 Pushing shared branch for subtree consumption..."
	@git add shared/
	@git commit -m "Update shared resources for client consumption" || echo "No changes to commit"
	@git subtree push --prefix=shared origin shared
	@echo "✅ Shared branch pushed to GitHub"

# Show help for subtree operations
subtree-help:
	@echo "📖 TMI Git Subtree Management"
	@echo ""
	@echo "Available targets:"
	@echo "  sync-shared   - Update shared/ directory from source files"
	@echo "  push-shared   - Push shared branch to GitHub for client consumption"
	@echo "  subtree-help  - Show this help message"
	@echo ""
	@echo "For TMI-UX client repo to consume:"
	@echo "  git subtree add --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash"
	@echo ""
	@echo "For TMI-UX to pull updates:"
	@echo "  git subtree pull --prefix=shared-api https://github.com/yourusername/tmi.git shared --squash"