# Database & Data Schemas

This directory contains database schema definitions, data models, and schema evolution documentation for the TMI project.

## Purpose

Authoritative reference for all TMI data schemas including database table definitions, API request/response schemas, WebSocket message formats, and configuration schemas.

## Schema Categories

### Database Schemas
- **PostgreSQL Tables**: Complete table definitions with relationships
- **Constraints**: Primary keys, foreign keys, and business constraints
- **Indexes**: Performance optimization indexes and strategies
- **Migrations**: Schema evolution history and procedures
- **Views**: Computed views and reporting structures

### API Schemas
- **Request Schemas**: HTTP request body validation schemas
- **Response Schemas**: HTTP response structure definitions
- **Error Schemas**: Standardized error response formats
- **Query Parameter Schemas**: URL parameter validation rules
- **Header Schemas**: Required and optional header definitions

### Message Schemas
- **WebSocket Messages**: Real-time collaboration message formats
- **Event Schemas**: Domain event structure definitions
- **Command Schemas**: Command message format specifications
- **Notification Schemas**: System notification message formats
- **Protocol Schemas**: Communication protocol definitions

### Configuration Schemas
- **Application Config**: Server configuration file schemas
- **Database Config**: Database connection and setup schemas
- **OAuth Config**: Authentication provider configuration schemas
- **Environment Config**: Environment-specific configuration schemas
- **Deployment Config**: Container and orchestration configuration schemas

## Core Data Entities

### User Management
```sql
-- Users table with OAuth integration
users (
  id UUID PRIMARY KEY,
  email VARCHAR UNIQUE NOT NULL,
  name VARCHAR NOT NULL,
  oauth_provider VARCHAR NOT NULL,
  oauth_provider_id VARCHAR NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- User sessions for JWT validation
user_sessions (
  id UUID PRIMARY KEY,
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  jwt_token_hash VARCHAR NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ DEFAULT NOW()
);
```

### Threat Modeling
```sql
-- Threat models (top-level container)
threat_models (
  id UUID PRIMARY KEY,
  name VARCHAR NOT NULL,
  description TEXT,
  owner_id UUID REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Diagrams within threat models
diagrams (
  id UUID PRIMARY KEY,
  threat_model_id UUID REFERENCES threat_models(id) ON DELETE CASCADE,
  name VARCHAR NOT NULL,
  diagram_data JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Threats associated with threat models
threats (
  id UUID PRIMARY KEY,
  threat_model_id UUID REFERENCES threat_models(id) ON DELETE CASCADE,
  title VARCHAR NOT NULL,
  description TEXT,
  severity VARCHAR CHECK (severity IN ('low', 'medium', 'high', 'critical')),
  status VARCHAR DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'closed')),
  created_at TIMESTAMPTZ DEFAULT NOW(),
  updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

### Access Control
```sql
-- Role-based access control for threat models
threat_model_permissions (
  id UUID PRIMARY KEY,
  threat_model_id UUID REFERENCES threat_models(id) ON DELETE CASCADE,
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  role VARCHAR NOT NULL CHECK (role IN ('reader', 'writer', 'owner')),
  granted_by UUID REFERENCES users(id),
  granted_at TIMESTAMPTZ DEFAULT NOW(),
  UNIQUE(threat_model_id, user_id)
);
```

### Collaboration
```sql
-- Real-time collaboration sessions
collaboration_sessions (
  id UUID PRIMARY KEY,
  diagram_id UUID REFERENCES diagrams(id) ON DELETE CASCADE,
  host_user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ DEFAULT NOW(),
  expires_at TIMESTAMPTZ NOT NULL
);

-- Session participants
collaboration_participants (
  session_id UUID REFERENCES collaboration_sessions(id) ON DELETE CASCADE,
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  joined_at TIMESTAMPTZ DEFAULT NOW(),
  PRIMARY KEY(session_id, user_id)
);
```

## API Schema Patterns

### Request/Response Schemas
```json
// Standard API response wrapper
{
  "success": boolean,
  "data": object | array | null,
  "error": {
    "code": string,
    "message": string,
    "details": object
  } | null,
  "meta": {
    "timestamp": "ISO 8601 timestamp",
    "request_id": "UUID",
    "pagination": {
      "page": number,
      "limit": number,
      "total": number,
      "pages": number
    } | null
  }
}

// Error response format
{
  "success": false,
  "data": null,
  "error": {
    "code": "VALIDATION_ERROR",
    "message": "Request validation failed",
    "details": {
      "field": "name",
      "reason": "Field is required"
    }
  }
}
```

### Entity Schemas
```json
// Threat Model schema
{
  "id": "UUID",
  "name": "string (1-200 chars)",
  "description": "string (optional, max 2000 chars)",
  "owner_id": "UUID",
  "created_at": "ISO 8601 timestamp",
  "updated_at": "ISO 8601 timestamp",
  "permissions": {
    "current_user_role": "reader|writer|owner",
    "can_edit": boolean,
    "can_delete": boolean,
    "can_share": boolean
  }
}

// Diagram schema
{
  "id": "UUID",
  "threat_model_id": "UUID",
  "name": "string (1-200 chars)",
  "diagram_data": {
    "cells": [
      {
        "id": "string",
        "type": "process|datastore|external_entity|trust_boundary|data_flow",
        "position": {"x": number, "y": number},
        "size": {"width": number, "height": number},
        "properties": {
          "label": "string",
          "description": "string"
        }
      }
    ],
    "metadata": {
      "version": "string",
      "last_modified_by": "UUID",
      "collaboration_session": "UUID | null"
    }
  },
  "created_at": "ISO 8601 timestamp",
  "updated_at": "ISO 8601 timestamp"
}
```

## WebSocket Message Schemas

### Collaboration Messages
```json
// Diagram operation message
{
  "message_type": "diagram_operation",
  "user_id": "string (email)",
  "operation_id": "UUID",
  "sequence_number": number,
  "operation": {
    "type": "patch",
    "cells": [
      {
        "id": "string",
        "operation": "add|update|remove",
        "data": object | null
      }
    ]
  },
  "timestamp": "ISO 8601 timestamp"
}

// Presenter mode messages
{
  "message_type": "current_presenter",
  "current_presenter": "string (email)",
  "timestamp": "ISO 8601 timestamp"
}

{
  "message_type": "presenter_cursor",
  "user_id": "string (email)",
  "cursor_position": {"x": number, "y": number},
  "timestamp": "ISO 8601 timestamp"
}

// Session management messages
{
  "event": "join",
  "user_id": "string (email)",
  "timestamp": "ISO 8601 timestamp"
}

{
  "event": "leave",
  "user_id": "string (email)",
  "timestamp": "ISO 8601 timestamp"
}

{
  "event": "session_ended",
  "user_id": "string (email)",
  "message": "string (reason)",
  "timestamp": "ISO 8601 timestamp"
}
```

## Configuration Schemas

### Application Configuration
```yaml
# TMI server configuration schema
server:
  port: "8080"                    # HTTP port
  interface: "0.0.0.0"           # Bind interface
  read_timeout: "30s"            # Request read timeout
  write_timeout: "30s"           # Response write timeout
  idle_timeout: "120s"           # Connection idle timeout
  tls_enabled: false             # Enable HTTPS
  tls_cert_file: ""              # TLS certificate file
  tls_key_file: ""               # TLS private key file

database:
  postgres:
    host: "localhost"            # PostgreSQL host
    port: "5432"                # PostgreSQL port
    user: "postgres"            # Database user
    password: ""                # Database password (env var)
    database: "tmi"             # Database name
    sslmode: "disable"          # SSL mode
  redis:
    host: "localhost"           # Redis host
    port: "6379"               # Redis port
    password: ""               # Redis password (env var)
    db: 0                      # Redis database number

auth:
  jwt:
    secret: ""                 # JWT signing secret (env var)
    expiration_seconds: 3600   # Token expiration
    signing_method: "HS256"    # JWT signing algorithm
  oauth:
    callback_url: "http://localhost:8080/oauth2/callback"
    providers:
      google:
        enabled: true
        client_id: ""          # Google OAuth client ID
        client_secret: ""      # Google OAuth client secret
      github:
        enabled: true
        client_id: ""          # GitHub OAuth client ID
        client_secret: ""      # GitHub OAuth client secret

logging:
  level: "info"              # Log level (debug|info|warn|error)
  is_dev: false             # Development mode logging
  log_dir: "/var/log/tmi"   # Log file directory
  max_age_days: 30          # Log retention days
  max_size_mb: 100          # Log file size limit
  max_backups: 10           # Number of log files to keep
  also_log_to_console: true # Console logging
```

## Redis Schema Patterns

### Key Naming Conventions
```
# Session storage
session:<user_id>:<session_id> = {jwt_hash, expires_at, user_data}

# WebSocket connections
ws:connections:<diagram_id> = SET of user_ids
ws:presenter:<diagram_id> = presenter_user_id

# Rate limiting
rate_limit:<user_id>:<endpoint> = request_count (TTL: window_duration)

# Collaboration coordination
collab:session:<session_id> = {host_user_id, diagram_id, participants}
collab:operations:<session_id> = LIST of operation_events
```

### Data Structure Usage
- **Strings**: Simple key-value pairs for configuration and flags
- **Hashes**: Complex objects like user sessions and collaboration state
- **Sets**: Collections like active connections and participant lists
- **Lists**: Ordered data like operation logs and message queues
- **Sorted Sets**: Time-ordered data like leaderboards and activity feeds

## Schema Validation

### JSON Schema Validation
- **OpenAPI Integration**: API schemas validated with OpenAPI 3.0
- **Request Validation**: Incoming request body validation
- **Response Validation**: Outgoing response structure validation
- **Schema Testing**: Automated schema compliance testing
- **Version Compatibility**: Schema evolution and compatibility checks

### Database Constraints
- **Data Integrity**: Foreign key constraints and referential integrity
- **Business Rules**: CHECK constraints for business logic validation
- **Performance**: Strategic indexing for query optimization
- **Security**: Row-level security and access control constraints

## Schema Evolution

### Migration Strategy
- **Versioned Migrations**: Sequential database schema changes
- **Rollback Capability**: Safe rollback procedures for failed migrations
- **Data Migration**: Data transformation during schema changes
- **Zero-Downtime**: Online schema changes for production systems
- **Testing**: Migration testing in staging environments

### API Versioning
- **Backward Compatibility**: Maintaining API compatibility across versions
- **Deprecation Strategy**: Gradual deprecation of old API versions
- **Schema Registry**: Centralized schema version management
- **Client Migration**: Guidance for client application updates

## Related Documentation

### Implementation References
- [Database Operations](../../operator/database/postgresql-operations.md) - Database management
- [API Documentation](../apis/) - API implementation using these schemas

### Development Integration
- [Integration Testing](../../developer/testing/integration-testing.md) - Schema testing procedures
- [Client Integration](../../developer/integration/client-integration-guide.md) - Client schema usage

### Architecture Context
- [System Architecture](../architecture/) - Schema design decisions and patterns

This schema documentation serves as the definitive reference for all data structures and formats used throughout the TMI system.