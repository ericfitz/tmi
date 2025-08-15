# TMI Documentation

This directory contains comprehensive documentation for the TMI (Threat Modeling Interface) project. The documents are organized by functional area and provide detailed guidance for developers, operators, and users.

## Implementation Plans

### [ADMIN_INTERFACE_IMPLEMENTATION_PLAN.md](ADMIN_INTERFACE_IMPLEMENTATION_PLAN.md)

Detailed 5-phase implementation plan for adding an admin web interface to TMI. Covers session management, system configuration viewing, authentication mechanisms, security considerations, and timeline with deliverables for building a secure admin dashboard.

## Client Integration Guides

### [OAUTH_INTEGRATION.md](OAUTH_INTEGRATION.md)

Web application integration guide for OAuth authentication with multiple providers (Google, GitHub, Microsoft). Includes provider configuration, token exchange patterns, security best practices, and troubleshooting for client applications.

## Testing & Quality Assurance

### [COVERAGE_REPORTING.md](COVERAGE_REPORTING.md)

Guide for generating comprehensive test coverage reports including unit and integration tests. Covers coverage thresholds, report formats (HTML/text), CI/CD integration, and testing best practices with automated coverage analysis tools.

### [INTEGRATION_TESTING.md](INTEGRATION_TESTING.md)

Comprehensive guide for running integration tests with real PostgreSQL and Redis databases. Includes automated setup/cleanup, testing methodology, database verification patterns, and best practices for testing API endpoints with authentication.

## Database Documentation

### [POSTGRESQL_DATABASE_OPS.md](POSTGRESQL_DATABASE_OPS.md)

Complete PostgreSQL operations guide covering deployment, migration management, schema validation, performance optimization, backup/recovery procedures, and operational commands with troubleshooting for database administration.

### [POSTGRESQL_DATABASE_SCHEMA.md](POSTGRESQL_DATABASE_SCHEMA.md)

Detailed database schema documentation with entity relationships, table definitions, migration history, constraints, and design patterns. Includes comprehensive indexing strategy and data integrity rules for the TMI platform.

### [REDIS_SCHEMA.md](REDIS_SCHEMA.md)

Redis key patterns, data structures, and caching strategies for TMI. Covers authentication/session management, performance monitoring, TTL strategies, cache invalidation patterns, and security considerations for the Redis caching layer.

## Observability & Operations

### [observability/README.md](observability/README.md)

Overview of TMI's OpenTelemetry-based observability implementation. Covers distributed tracing, metrics collection, structured logging, security filtering, architecture overview, and getting started guide for monitoring and performance analysis.

### [observability/performance-tuning.md](observability/performance-tuning.md)

Performance optimization guide for OpenTelemetry implementation. Includes sampling optimization, batch processing tuning, memory management, network optimization, database performance, and automated optimization strategies with monitoring dashboards.

### [observability/runbooks/incident-response.md](observability/runbooks/incident-response.md)

Step-by-step incident response procedures for TMI application issues. Covers service availability problems, performance degradation, database issues, security incidents, escalation procedures, and post-incident analysis with monitoring tools and commands.

### [observability/runbooks/performance-issues.md](observability/runbooks/performance-issues.md)

Specific runbook for diagnosing and resolving performance issues including high response times, CPU/memory usage, database performance problems, cache issues, network latency, and goroutine leaks with optimization strategies.

## Historical Documents

### [Prompts to generate a collaborative editing ws api.md](Prompts%20to%20generate%20a%20collaborative%20editing%20ws%20api.md)

Historical documentation showing the iterative prompt engineering process used to develop the collaborative editing implementation plan. Contains the actual prompts and evolution of requirements that shaped the WebSocket API design and implementation strategy.
