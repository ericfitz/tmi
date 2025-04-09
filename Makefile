.PHONY: build test lint clean

# Default build target
VERSION := 0.1.0
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "development")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X github.com/ericfitz/tmi/api.VersionMajor=0 -X github.com/ericfitz/tmi/api.VersionMinor=1 -X github.com/ericfitz/tmi/api.VersionPatch=0 -X github.com/ericfitz/tmi/api.GitCommit=$(COMMIT) -X github.com/ericfitz/tmi/api.BuildDate=$(BUILD_DATE)"

build:
	go build $(LDFLAGS) -o bin/server github.com/ericfitz/tmi/cmd/server

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