.PHONY: build test lint clean dev dev-db dev-redis dev-app build-postgres build-redis gen-config

# Default build target
VERSION := 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "development")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

build:
	go build -o bin/server github.com/ericfitz/tmi/cmd/server

# Run tests
test:
	go test ./...

# Run specific test
test-one:
	@if [ -z "$(name)" ]; then \
		echo "Usage: make test-one name=TestName"; \
		exit 1; \
	fi
	go test ./... -run $(name)

# Run linter
lint:
	golangci-lint run

# Generate API from OpenAPI spec
gen-api:
	oapi-codegen -package api -generate types,server tmi-openapi.json > api/api.go

# Clean build artifacts
clean:
	rm -rf ./bin/*

# Start development environment
dev:
	@echo "Starting TMI development environment..."
	@./scripts/start-dev.sh

# Start development database only
dev-db:
	@echo "Starting development database..."
	@./scripts/start-dev-db.sh

# Start development Redis only
dev-redis:
	@echo "Starting development Redis..."
	@./scripts/start-dev-redis.sh

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