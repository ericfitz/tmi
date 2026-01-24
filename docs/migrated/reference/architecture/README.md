<!-- Migrated to wiki: Architecture-and-Design.md on 2025-01-24 -->
<!-- This file is kept for archival purposes. The canonical location is now the wiki. -->

<!-- VERIFICATION SUMMARY (2025-01-24):
File references:
- AUTHORIZATION.md: VERIFIED - Previously migrated to docs/migrated/reference/architecture/AUTHORIZATION.md
- oauth-flow-diagrams.md: VERIFIED - Previously migrated to docs/migrated/reference/architecture/oauth-flow-diagrams.md
- ../../developer/setup/development-setup.md: NOT FOUND - Path does not exist (migrated to docs/migrated/developer/setup/development-setup.md)
- ../../operator/deployment/deployment-guide.md: VERIFIED - File exists at /Users/efitz/Projects/tmi/docs/operator/deployment/deployment-guide.md
- ../schemas/: DIRECTORY NOT FOUND - No schemas directory exists at docs/reference/schemas/
- ../apis/: VERIFIED - Directory exists at /Users/efitz/Projects/tmi/docs/reference/apis/
- ../../operator/database/postgresql-operations.md: VERIFIED - File exists
- ../../operator/monitoring/: DIRECTORY NOT FOUND - No monitoring directory exists

Architectural claims verified against source code:
- Go/Gin framework: VERIFIED in api/server.go line 9 (imports github.com/gin-gonic/gin)
- WebSocketHub: VERIFIED in api/websocket.go
- PostgreSQL support: VERIFIED in multiple files (auth/db/gorm.go, api/store.go, etc.)
- Redis support: VERIFIED in auth/db/redis.go and api/cache_service.go
- Chainguard base images: VERIFIED in Dockerfile.server (cgr.dev/chainguard/go:latest and cgr.dev/chainguard/static:latest)
- CGO_ENABLED=0: VERIFIED in Dockerfile.server line 26
- Static binary: VERIFIED in Dockerfile.server
- nonroot execution: VERIFIED in Dockerfile.server line 68 (USER nonroot:nonroot)

Make targets verified:
- observability-start/observability-stop: VERIFIED (as obs-start/obs-stop aliases in Makefile)
- build-containers: VERIFIED in Makefile line 1000
- scan-containers: VERIFIED in Makefile line 1004

CORRECTIONS MADE:
- Fixed wiki Architecture-and-Design.md: Changed "Go/Echo" to "Go/Gin" (line 22)
- Fixed wiki Architecture-and-Design.md: Changed "echo.Context" to "*gin.Context" (line 131)

ISSUES IDENTIFIED (marked with NEEDS-REVIEW in content):
- Reference to ../../developer/setup/development-setup.md broken (file migrated)
- Reference to ../schemas/ broken (directory doesn't exist)
- Reference to ../../operator/monitoring/ broken (directory doesn't exist)
-->

# System Architecture & Design

This directory contains high-level system design, architectural decisions, and design patterns for the TMI project.

## Purpose

Authoritative architectural documentation that serves as the foundation for system understanding, design decisions, and implementation guidance across the TMI platform.

## Files in this Directory

### [AUTHORIZATION.md](AUTHORIZATION.md)
**Authorization and access control architecture** for TMI.

**Content includes:**
- Role-based access control (RBAC) design
- Permission model and inheritance
- Resource-level authorization
- Authorization middleware implementation

### [oauth-flow-diagrams.md](oauth-flow-diagrams.md)
**Comprehensive OAuth flow diagrams** for all supported authentication scenarios.

**Content includes:**
- OAuth 2.0 authorization code flow diagrams
- PKCE flow for public clients
- Multi-provider authentication flows (Google, GitHub, Microsoft)
- Token refresh and revocation flows
- Error handling scenarios

## Architectural Overview

### TMI System Architecture

```
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│   Web Clients   │◄──►│   Load Balancer  │◄──►│  TMI Server(s)  │
│  (React/Vue/etc)│    │   (Nginx/HAProxy)│    │   (Go/Gin)      │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                                        │
                                                        ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  OAuth Providers│◄──►│  Authentication  │◄──►│   PostgreSQL    │
│ (Google/GitHub) │    │   & JWT Layer    │    │   Primary DB    │
└─────────────────┘    └──────────────────┘    └─────────────────┘
                                                        │
                                                        ▼
┌─────────────────┐    ┌──────────────────┐    ┌─────────────────┐
│  WebSocket Hub  │◄──►│   Redis Cache    │◄──►│   Monitoring    │
│ (Collaboration) │    │  (Sessions/RT)   │    │   & Logging     │
└─────────────────┘    └──────────────────┘    └─────────────────┘
```

### Core Components

#### TMI Server (Go Application)

- **HTTP API**: RESTful endpoints for CRUD operations
- **WebSocket Hub**: Real-time collaboration coordination
- **Authentication**: OAuth integration with JWT tokens
- **Authorization**: Role-based access control (RBAC)
- **Business Logic**: Threat modeling and diagram management

#### Data Layer

- **PostgreSQL**: Primary data storage for persistent entities
- **Redis**: Session storage and real-time coordination
- **File Storage**: Static assets and uploaded content

#### External Integrations

- **OAuth Providers**: Google, GitHub, Microsoft authentication
- **Monitoring Stack**: Observability and alerting systems
- **Client Applications**: Web and mobile client integrations

## Architectural Patterns

### 1. Domain-Driven Design (DDD)

- **Entities**: Core business objects (ThreatModel, Diagram, User)
- **Value Objects**: Immutable data containers
- **Aggregates**: Consistency boundaries for business operations
- **Repositories**: Data access abstraction layer
- **Services**: Business logic coordination

### 2. Hexagonal Architecture (Ports & Adapters)

- **Core Domain**: Business logic independent of external concerns
- **Ports**: Interfaces for external communication
- **Adapters**: Implementations of external integrations
- **Dependency Inversion**: Core depends on abstractions, not implementations

### 3. Event-Driven Architecture

- **Command-Query Separation**: Clear separation of reads and writes
- **Domain Events**: Business event notifications
- **Event Sourcing**: Audit trail through event storage
- **Real-time Events**: WebSocket-based real-time updates

### 4. Microservice Patterns (Future)

- **Service Boundaries**: Clear service responsibilities
- **API Gateway**: Unified client interface
- **Service Discovery**: Dynamic service location
- **Circuit Breakers**: Fault tolerance and resilience

## Security Architecture

### Authentication & Authorization

```
[Client] → [OAuth Provider] → [TMI Auth] → [JWT Token] → [Protected Resources]
    ↑           ↑                ↑            ↑              ↑
  Login    Provider Auth    Token Exchange  Bearer Auth   Resource Access
```

#### OAuth 2.0 Flow

1. **Client Initiation**: Client redirects to TMI OAuth endpoint
2. **Provider Selection**: User selects OAuth provider (Google/GitHub)
3. **Provider Authentication**: User authenticates with chosen provider
4. **Authorization Grant**: Provider returns authorization to TMI
5. **Token Exchange**: TMI exchanges authorization for user info and generates JWT
6. **Client Access**: Client receives JWT token for API access

#### Role-Based Access Control (RBAC)

- **Roles**: Reader, Writer, Owner for each threat model
- **Permissions**: Fine-grained access control per operation
- **Inheritance**: Role hierarchy with permission inheritance
- **Context**: Resource-specific role assignments

### Data Security

- **Encryption in Transit**: TLS 1.3 for all communications
- **Encryption at Rest**: Database and file storage encryption
- **Data Validation**: Input sanitization and validation
- **Audit Logging**: Comprehensive audit trail for all operations

## Collaboration Architecture

### Real-time Collaboration

```
[User A] ──WebSocket──┐
                       ├── [TMI WebSocket Hub] ──Redis── [Session State]
[User B] ──WebSocket──┘           │
                                  ├── [Operation Log] ──PostgreSQL
                                  └── [Conflict Resolution]
```

#### WebSocket Protocol

- **Connection Management**: Authenticated WebSocket connections
- **Message Routing**: Hub-based message distribution
- **Operation Ordering**: Conflict-free replicated data types (CRDTs)
- **State Synchronization**: Automatic state correction and resync

#### Collaboration Features

- **Multi-user Editing**: Simultaneous diagram editing
- **Presenter Mode**: Designated presenter with cursor sharing
- **Conflict Resolution**: Operational transformation for consistency
- **Undo/Redo**: Collaborative undo/redo with history management

## Scalability Architecture

### Horizontal Scaling

- **Stateless Servers**: TMI servers designed for horizontal scaling
- **Load Balancing**: Round-robin or least-connections load balancing
- **Session Affinity**: Redis-based session storage for multi-server deployment
- **Database Scaling**: Read replicas and connection pooling

### Performance Optimization

- **Caching Strategy**: Multi-layer caching with Redis and application cache
- **Connection Pooling**: Database and Redis connection pooling
- **Lazy Loading**: On-demand resource loading and pagination
- **CDN Integration**: Static asset delivery through CDN

### Resource Management

- **Memory Management**: Efficient Go garbage collection tuning
- **Connection Limits**: Appropriate limits for concurrent connections
- **Rate Limiting**: API and WebSocket rate limiting
- **Resource Quotas**: Per-user and per-organization resource limits

## Data Architecture

### Data Storage Strategy

```
┌─── Application Layer ───┐
│  Go Structs & Interfaces │
├─── Business Logic ──────┤
│   Domain Models & Rules  │
├─── Data Access Layer ───┤
│    Repository Pattern    │
├─── Storage Layer ───────┤
│ PostgreSQL │   Redis     │
│ (Persistent)│ (Temporary) │
└─────────────────────────┘
```

#### PostgreSQL (Primary Storage)

- **ACID Compliance**: Strong consistency for business data
- **Relational Integrity**: Foreign key constraints and referential integrity
- **Schema Migrations**: Versioned schema evolution
- **Performance**: Optimized indexes and query patterns

#### Redis (Cache & Sessions)

- **Session Storage**: JWT session validation and user state
- **Real-time Coordination**: WebSocket connection coordination
- **Caching**: Frequently accessed data caching
- **Rate Limiting**: Request rate limiting and throttling

### Data Flow Patterns

- **Command Query Responsibility Segregation (CQRS)**: Separate read/write models
- **Event Sourcing**: Audit trail through event storage
- **Cache-Aside Pattern**: Application-managed caching
- **Write-Through Pattern**: Consistent cache updates

## Integration Architecture

### API Design Principles

- **RESTful Design**: Resource-based API design with HTTP verbs
- **OpenAPI Specification**: Complete API specification documentation
- **Versioning Strategy**: API versioning for backward compatibility
- **Error Handling**: Consistent error response format

### WebSocket Integration

- **Protocol Design**: Message-based protocol with JSON payloads
- **Connection Management**: Automatic reconnection and heartbeat
- **Message Ordering**: Guaranteed message delivery and ordering
- **Scalability**: Hub-based architecture for multi-server deployment

### External Service Integration

- **OAuth Providers**: Standardized OAuth 2.0 integration pattern
- **Monitoring Services**: OpenTelemetry-based observability
- **Email Services**: SMTP integration for notifications
- **File Storage**: Cloud storage integration for assets

## Deployment Architecture

### Container Strategy

- **Docker Images**: Multi-stage builds with Chainguard base images
  - Builder: `cgr.dev/chainguard/go:latest` for secure Go compilation
  - Runtime: `cgr.dev/chainguard/static:latest` for minimal attack surface (~57MB)
- **Static Binaries**: Built with `CGO_ENABLED=0` for maximum portability
- **Security Hardening**: Non-root execution (nonroot:nonroot), no shell in runtime
- **Configuration Management**: Environment-based configuration
- **Health Checks**: Comprehensive health check endpoints
- **Database Support**: Container builds support PostgreSQL, MySQL, SQLServer, SQLite (Oracle excluded due to CGO requirement)

### Orchestration Patterns

- **Kubernetes Deployment**: Cloud-native orchestration
- **Service Mesh**: Advanced networking and security
- **GitOps**: Infrastructure and application deployment automation
- **Blue-Green Deployment**: Zero-downtime deployment strategy

## Future Architecture Considerations

### Microservice Evolution

- **Service Decomposition**: Breaking monolith into focused services
- **API Gateway**: Unified client interface and cross-cutting concerns
- **Service Discovery**: Dynamic service registration and discovery
- **Distributed Tracing**: End-to-end request tracing

### Advanced Collaboration

- **Operational Transformation**: Advanced conflict resolution algorithms
- **Branch and Merge**: Version control patterns for threat models
- **Real-time Analytics**: Live collaboration analytics
- **Multi-tenancy**: Organization-based data isolation

### Performance Enhancements

- **GraphQL API**: Flexible client data requirements
- **CDN Integration**: Global content delivery
- **Edge Computing**: Regional deployment for low latency
- **Advanced Caching**: Multi-level caching strategies

## Related Documentation

### Implementation Guides

- [Developer Setup](../../developer/setup/development-setup.md) - Local architecture setup <!-- NEEDS-REVIEW: File migrated to docs/migrated/developer/setup/development-setup.md -->
- [Deployment Guide](../../operator/deployment/deployment-guide.md) - Production architecture deployment

### Technical Specifications

- [Database Schema](../schemas/) - Data architecture specifications <!-- NEEDS-REVIEW: Directory does not exist -->
- [API Specifications](../apis/) - API architecture documentation

### Operational Guidance

- [Database Operations](../../operator/database/postgresql-operations.md) - Data layer operations
- [Monitoring Guide](../../operator/monitoring/) - Observability architecture <!-- NEEDS-REVIEW: Directory does not exist -->

This architectural documentation serves as the foundational reference for understanding TMI system design and guiding implementation decisions across the platform.
