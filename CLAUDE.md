# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview
This repository contains API documentation and Go implementation for a Collaborative Threat Modeling Interface (TMI).

## Key Files
- tmi-api-v1_0.md - API documentation in Markdown
- tmi-openapi.json - OpenAPI specification
- api/store.go - Generic typed map storage implementation

## Commands
- Build: `go build ./...`
- Test: `go test ./...`
- Test specific: `go test ./path/to/package -run TestName`
- Lint: `golangci-lint run`
- Generate API: `oapi-codegen -package api -generate types,server tmi-openapi.json > api/api.go`

## Go Style Guidelines
- Format code with `gofmt`
- Group imports by standard lib, external libs, then internal packages
- Use camelCase for variables, PascalCase for exported functions/structs
- Error handling: check errors and return with context
- Prefer interfaces over concrete types for flexibility
- Document all exported functions with godoc comments
- Structure code by domain (auth, diagrams, threats)

## API Design Guidelines
- Follow OpenAPI 3.1 specification standards
- Use snake_case for API JSON properties
- Include descriptions for all properties and endpoints
- Document error responses (401, 403, 404)
- Use UUID format for IDs, ISO8601 for timestamps
- Role-based access with reader/writer/owner permissions
- Bearer token auth with JWT
- JSON Patch for partial updates
- WebSocket for real-time collaboration
- Pagination with limit/offset parameters

## Storage Pattern
- Use the generic Store[T] implementation from api/store.go
- Each entity type has its own store instance (DiagramStore, ThreatModelStore)
- Store provides CRUD operations with proper concurrency control
- Entity fields should be properly validated before storage
- Use WithTimestamps interface for entities with created_at/modified_at fields