# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI). It's a server-based web application enabling collaborative threat modeling with real-time diagram editing via WebSockets, role-based access control, OAuth authentication with JWT, and a RESTful API with OpenAPI 3.0 specification.

## Key Files

- docs/TMI-API-v1_0.md - API documentation in Markdown
- tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation
- api/server.go - Main API server with WebSocket support
- api/websocket.go - WebSocket hub for real-time collaboration
- cmd/server/main.go - Server entry point
- Makefile - Build automation with development targets

## Commands

- List targets: `make list` (lists all available make targets)
- Build: `make build` (creates bin/server executable)
- Test: `make test` (runs all tests)
  - `make test passfail=true` (shows only PASS/FAIL results)
  - `make test count1=true` (runs tests with --count=1 to disable caching)
  - `make test passfail=true count1=true` (combines both options)
- Test specific: `make test-one name=TestName`
- Single test with verbose output: `make single-test name=TestName` (runs single test in api package with verbose output)
- Integration tests: `make test-integration` (runs database integration tests with automatic setup/cleanup)
- Integration cleanup: `make test-integration-cleanup` (cleans up integration test containers)
- Lint: `make lint` (runs golangci-lint)
- Generate API: `make gen-api` (uses oapi-codegen with config from oapi-codegen-config.yaml)
- Development: `make dev` (starts full dev environment with DB and Redis)
- Dev DB only: `make dev-db` (starts PostgreSQL container)
- Dev Redis only: `make dev-redis` (starts Redis container)
- Authentication testing: `make test-auth-token` (gets OAuth token from test provider)
- API testing with auth: `make test-with-token` (tests authenticated endpoints)

## Go Style Guidelines

- Format code with `gofmt`
- Group imports by standard lib, external libs, then internal packages
- Use camelCase for variables, PascalCase for exported functions/structs
- Error handling: check errors and return with context
- Prefer interfaces over concrete types for flexibility
- Document all exported functions with godoc comments
- Structure code by domain (auth, diagrams, threats)

## API Design Guidelines

- Follow OpenAPI 3.0.3 specification standards
- Use snake_case for API JSON properties
- Include descriptions for all properties and endpoints
- Document error responses (401, 403, 404)
- Use UUID format for IDs, ISO8601 for timestamps
- Role-based access with reader/writer/owner permissions
- Bearer token auth with JWT
- JSON Patch for partial updates
- WebSocket for real-time collaboration
- Pagination with limit/offset parameters

## Architecture & Code Structure

### Storage Pattern

- Use the generic Store[T] implementation from api/store.go
- Each entity type has its own store instance (DiagramStore, ThreatModelStore)
- Store provides CRUD operations with proper concurrency control
- Entity fields should be properly validated before storage
- Use WithTimestamps interface for entities with created_at/modified_at fields

### Project Structure

- `api/` - API handlers, server implementation, and storage
- `auth/` - Authentication service with OAuth, JWT, and RBAC
- `cmd/` - Command-line executables (server, migrate, check-db)
- `internal/` - Internal packages (logging, dbschema)
- `docs/` - API documentation and architectural diagrams
- `scripts/` - Development setup scripts

### WebSocket Architecture

- Real-time collaboration via WebSocket connections at `/ws/diagrams/{id}`
- WebSocketHub manages active connections and broadcasts updates
- Only diagrams support real-time collaboration, not threat models
- Uses Gorilla WebSocket library

### Database Integration

- PostgreSQL for persistent storage (configured via auth/ package)
- Redis for caching and session management
- Database migrations in auth/migrations/
- Development uses Docker containers
- Dual-mode storage: in-memory for tests, database-backed for dev/prod

## Development Environment

- Copy `.env.example` to `.env.dev` for local development
- Uses PostgreSQL and Redis Docker containers
- Development scripts handle container management automatically
- Server runs on port 8080 by default with configurable TLS support

## User Preferences

- After changing any file, run lint and fix any issues caused by the change
- After changing any executable or test file, run build and fix any issues caused by the change and then run test and fix any issues caused by the change
- Do not disable or skip failing tests, either diagnose to root cause and fix either the test issue or code issue, or ask the user what to do.

## Make Command Memories

- `make list` is useful for quickly discovering and reviewing all available make targets in the project
- `make validate-asyncapi` validates the AsyncAPI specification for the project

## Test Execution Guidelines

- Never create ad hoc commands to run a test - use a make target
- Never create ad hoc commands to run the server - use a make target so we get proper configuration information

## Test Philosophy Memories

- Never disable or skip failing tests - investigate to root cause

### OpenAPI Integration

- API code generated from tmi-openapi.json using oapi-codegen v2
- OpenAPI validation middleware clears security schemes (auth handled by JWT middleware)
- Generated types in api/api.go include Echo server handlers and embedded spec
- Config file: oapi-codegen-config.yaml

## Authentication Memories

- Always use a normal oauth login flow with the "test" provider when performing any development or testing task that requires authentication
- Use `make test-auth-token` to get tokens, then pass them in Authorization header as `Bearer <token>`
- OAuth test provider generates JWT tokens with user claims (email, name, sub)