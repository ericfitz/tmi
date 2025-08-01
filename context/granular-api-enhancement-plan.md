# Enhanced TMI API Plan: Granular Operations with Authorization

## Executive Summary

This plan enhances the TMI API to support granular operations on threat model sub-resources while maintaining complete object serialization capabilities. Key additions include PATCH support for cells, individual metadata key access, and comprehensive authorization inheritance for all sub-objects using the existing AccessCheck utility.

## Current Architecture Analysis

**Hierarchical Structure:**
- ThreatModel (top-level) contains: documents, sourceCode, diagrams, threats, metadata, authorization
- Diagrams contain: cells (nodes/edges), metadata
- All entities have metadata arrays
- Authorization is defined at ThreatModel level with owner + authorization list

**Current Limitations:**
1. Only monolithic CRUD operations on complete ThreatModel objects
2. No direct endpoints for sub-resources (threats, documents, etc.)
3. Limited granular access patterns
4. Potential for large payloads when only small changes are needed
5. No PATCH support for individual cells
6. No direct metadata key access

## Enhanced API Endpoints

### 1. **Core Sub-Resource Endpoints**

```
# Threats within a threat model
GET    /threat_models/{id}/threats
POST   /threat_models/{id}/threats
GET    /threat_models/{id}/threats/{threat_id}
PUT    /threat_models/{id}/threats/{threat_id}
PATCH  /threat_models/{id}/threats/{threat_id}
DELETE /threat_models/{id}/threats/{threat_id}

# Documents within a threat model (no PATCH support)
GET    /threat_models/{id}/documents
POST   /threat_models/{id}/documents
GET    /threat_models/{id}/documents/{doc_id}
PUT    /threat_models/{id}/documents/{doc_id}
DELETE /threat_models/{id}/documents/{doc_id}

# Source code within a threat model (no PATCH support)
GET    /threat_models/{id}/source_code
POST   /threat_models/{id}/source_code
GET    /threat_models/{id}/source_code/{source_id}
PUT    /threat_models/{id}/source_code/{source_id}
DELETE /threat_models/{id}/source_code/{source_id}
```

### 2. **Metadata Operations (NEW)**

```
# Metadata operations with individual key access
GET    /threat_models/{id}/metadata
POST   /threat_models/{id}/metadata
GET    /threat_models/{id}/metadata/{key}          # NEW - Individual key access
PUT    /threat_models/{id}/metadata/{key}          # NEW - Set specific key
DELETE /threat_models/{id}/metadata/{key}          # NEW - Delete specific key

# Metadata for threats
GET    /threat_models/{id}/threats/{threat_id}/metadata
POST   /threat_models/{id}/threats/{threat_id}/metadata
GET    /threat_models/{id}/threats/{threat_id}/metadata/{key}
PUT    /threat_models/{id}/threats/{threat_id}/metadata/{key}
DELETE /threat_models/{id}/threats/{threat_id}/metadata/{key}

# Metadata for documents
GET    /threat_models/{id}/documents/{doc_id}/metadata
POST   /threat_models/{id}/documents/{doc_id}/metadata
GET    /threat_models/{id}/documents/{doc_id}/metadata/{key}
PUT    /threat_models/{id}/documents/{doc_id}/metadata/{key}
DELETE /threat_models/{id}/documents/{doc_id}/metadata/{key}

# Metadata for source code
GET    /threat_models/{id}/source_code/{source_id}/metadata
POST   /threat_models/{id}/source_code/{source_id}/metadata
GET    /threat_models/{id}/source_code/{source_id}/metadata/{key}
PUT    /threat_models/{id}/source_code/{source_id}/metadata/{key}
DELETE /threat_models/{id}/source_code/{source_id}/metadata/{key}

# Metadata for diagrams
GET    /threat_models/{id}/diagrams/{diagram_id}/metadata
POST   /threat_models/{id}/diagrams/{diagram_id}/metadata
GET    /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}
PUT    /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}
DELETE /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}

# Metadata for diagram cells
GET    /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}/metadata
POST   /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}/metadata
GET    /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
PUT    /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
DELETE /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}/metadata/{key}
```

### 3. **Enhanced Cell Operations (NEW PATCH Support)**

```
# Diagram cells with PATCH support
GET    /threat_models/{id}/diagrams/{diagram_id}/cells
POST   /threat_models/{id}/diagrams/{diagram_id}/cells
GET    /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}
PUT    /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}
PATCH  /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}    # NEW - Granular cell updates
DELETE /threat_models/{id}/diagrams/{diagram_id}/cells/{cell_id}

# Cell metadata operations (included above in section 2)
```

### 4. **Batch Operations**

```
# Batch operations for efficiency
POST   /threat_models/{id}/threats/batch
PUT    /threat_models/{id}/threats/batch
DELETE /threat_models/{id}/threats/batch
PATCH  /threat_models/{id}/batch                   # Bulk JSON Patch operations

# Batch cell operations
PATCH  /threat_models/{id}/diagrams/{diagram_id}/cells/batch
```

## Authorization Integration with AccessCheck

### Authorization Inheritance Model

All sub-resources inherit authorization from their parent ThreatModel:

```go
// AuthorizationInheritance represents how sub-resources inherit authorization
type AuthorizationInheritance struct {
    ThreatModelID string                 // Parent threat model ID
    AuthData      AuthorizationData      // Inherited from parent
}

// GetInheritedAuthData retrieves authorization data from parent threat model
func GetInheritedAuthData(threatModelID string) (AuthorizationData, error) {
    // Fetch parent threat model
    threatModel, err := ThreatModelStore.Get(threatModelID)
    if err != nil {
        return AuthorizationData{}, err
    }
    
    // Return authorization data from parent
    return AuthorizationData{
        Type:          AuthTypeTMI10,
        Owner:         threatModel.Owner,
        Authorization: threatModel.Authorization,
    }, nil
}
```

### Enhanced Access Control Functions

```go
// CheckSubResourceAccess validates access to sub-resources using inherited authorization
func CheckSubResourceAccess(userName, threatModelID string, requiredRole Role) (bool, error) {
    // Get inherited authorization data
    authData, err := GetInheritedAuthData(threatModelID)
    if err != nil {
        return false, err
    }
    
    // Use existing AccessCheck utility
    return AccessCheck(userName, requiredRole, authData), nil
}

// ValidateSubResourceAccess middleware for sub-resource endpoints
func ValidateSubResourceAccess(requiredRole Role) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Get authenticated user
        userName, _, err := ValidateAuthenticatedUser(c)
        if err != nil {
            HandleRequestError(c, err)
            c.Abort()
            return
        }
        
        // Extract threat model ID from URL path
        threatModelID := c.Param("threat_model_id")
        if threatModelID == "" {
            HandleRequestError(c, BadRequestError("Missing threat model ID"))
            c.Abort()
            return
        }
        
        // Check access using inherited authorization
        hasAccess, err := CheckSubResourceAccess(userName, threatModelID, requiredRole)
        if err != nil {
            HandleRequestError(c, err)
            c.Abort()
            return
        }
        
        if !hasAccess {
            HandleRequestError(c, ForbiddenError("Insufficient permissions for this resource"))
            c.Abort()
            return
        }
        
        // Store threat model auth data in context for handlers
        authData, _ := GetInheritedAuthData(threatModelID)
        c.Set("authData", authData)
        c.Next()
    }
}
```

### Role-Based Access Patterns

**Reader Role:**
- GET operations on all sub-resources
- GET metadata/{key} operations

**Writer Role:**
- All Reader permissions
- POST, PUT, PATCH, DELETE on threats, documents, source_code, cells
- PUT, DELETE metadata/{key} operations

**Owner Role:**
- All Writer permissions
- Authorization management
- Ownership transfer

## Enhanced Data Storage Schema

### Core Tables with Foreign Key Relationships

```sql
-- Core threat models table (existing, enhanced)
CREATE TABLE threat_models (
    id UUID PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    owner VARCHAR(256) NOT NULL,
    created_by VARCHAR(256) NOT NULL,
    threat_model_framework VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL,
    issue_url TEXT
);

-- Sub-resource tables with foreign keys
CREATE TABLE threats (
    id UUID PRIMARY KEY,
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    name VARCHAR(256) NOT NULL,
    description TEXT,
    severity VARCHAR(50),
    score DECIMAL(3,1),
    priority VARCHAR(50),
    mitigated BOOLEAN DEFAULT FALSE,
    status VARCHAR(50),
    threat_type VARCHAR(100),
    diagram_id UUID,
    cell_id UUID,
    issue_url TEXT,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL
);

CREATE TABLE documents (
    id UUID PRIMARY KEY,
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    name VARCHAR(256) NOT NULL,
    url TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL
);

CREATE TABLE sources (
    id UUID PRIMARY KEY,
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    name VARCHAR(256),
    url TEXT NOT NULL,
    type VARCHAR(50) NOT NULL,
    description TEXT,
    parameters JSONB,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL
);

CREATE TABLE diagrams (
    id UUID PRIMARY KEY,
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    name VARCHAR(256) NOT NULL,
    type VARCHAR(50) NOT NULL,
    description TEXT,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL
);

CREATE TABLE diagram_cells (
    id UUID PRIMARY KEY,
    diagram_id UUID NOT NULL REFERENCES diagrams(id) ON DELETE CASCADE,
    shape VARCHAR(100) NOT NULL,
    cell_data JSONB NOT NULL,
    z_index INTEGER DEFAULT 1,
    visible BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL
);

-- Generic metadata table supporting all entity types
CREATE TABLE metadata (
    id UUID PRIMARY KEY,
    entity_type VARCHAR(50) NOT NULL, -- 'threat_model', 'threat', 'document', 'source', 'diagram', 'cell'
    entity_id UUID NOT NULL,
    key VARCHAR(255) NOT NULL,
    value TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    modified_at TIMESTAMP NOT NULL,
    UNIQUE(entity_type, entity_id, key)
);

-- Authorization table (existing, enhanced)
CREATE TABLE authorizations (
    id UUID PRIMARY KEY,
    threat_model_id UUID NOT NULL REFERENCES threat_models(id) ON DELETE CASCADE,
    subject VARCHAR(255) NOT NULL,
    role VARCHAR(50) NOT NULL CHECK (role IN ('reader', 'writer', 'owner')),
    created_at TIMESTAMP NOT NULL,
    UNIQUE(threat_model_id, subject)
);
```

### Enhanced Store Interfaces

```go
// Enhanced stores for sub-resources
type ThreatStore interface {
    GetByThreatModelID(threatModelID string, offset, limit int) ([]Threat, error)
    GetByID(threatModelID, threatID string) (Threat, error)
    Create(threatModelID string, threat Threat) (Threat, error)
    Update(threatModelID, threatID string, threat Threat) error
    Patch(threatModelID, threatID string, operations []PatchOperation) (Threat, error)
    Delete(threatModelID, threatID string) error
    Count(threatModelID string) (int, error)
}

type DocumentStore interface {
    GetByThreatModelID(threatModelID string, offset, limit int) ([]Document, error)
    GetByID(threatModelID, documentID string) (Document, error)
    Create(threatModelID string, document Document) (Document, error)
    Update(threatModelID, documentID string, document Document) error
    Delete(threatModelID, documentID string) error
    Count(threatModelID string) (int, error)
}

type SourceStore interface {
    GetByThreatModelID(threatModelID string, offset, limit int) ([]Source, error)
    GetByID(threatModelID, sourceID string) (Source, error)
    Create(threatModelID string, source Source) (Source, error)
    Update(threatModelID, sourceID string, source Source) error
    Delete(threatModelID, sourceID string) error
    Count(threatModelID string) (int, error)
}

type MetadataStore interface {
    GetByEntity(entityType, entityID string) ([]Metadata, error)
    GetByEntityAndKey(entityType, entityID, key string) (Metadata, error)
    CreateForEntity(entityType, entityID string, metadata Metadata) (Metadata, error) // NEW - POST operation
    SetForEntity(entityType, entityID, key, value string) error
    DeleteByEntityAndKey(entityType, entityID, key string) error
    DeleteByEntity(entityType, entityID string) error
}

type CellStore interface {
    GetByDiagramID(diagramID string, offset, limit int) ([]Cell, error)
    GetByID(diagramID, cellID string) (Cell, error)
    Create(diagramID string, cell Cell) (Cell, error)
    Update(diagramID, cellID string, cell Cell) error
    Patch(diagramID, cellID string, operations []PatchOperation) (Cell, error) // NEW
    Delete(diagramID, cellID string) error
    BatchPatch(diagramID string, cellPatches []CellPatchOperation) ([]Cell, error) // NEW
}
```

## Implementation Plan

### Phase 1: Core Infrastructure
1. **Enhanced Authorization Utilities**
   - Implement `GetInheritedAuthData()` function
   - Create `CheckSubResourceAccess()` utility
   - Build `ValidateSubResourceAccess()` middleware
   - Add authorization caching for performance

2. **Database Schema Updates**
   - Create migration for normalized sub-resource tables
   - Implement foreign key constraints
   - Add indexes for performance optimization
   - Create metadata table with polymorphic associations

### Phase 2: Sub-Resource Stores
1. **Specialized Store Implementations**
   - Implement ThreatStore with full CRUD + PATCH operations
   - Create DocumentStore and SourceStore with CRUD operations (no PATCH)
   - Build MetadataStore with key-based operations including POST
   - Enhance CellStore with PATCH support

### Phase 3: API Handlers
1. **Sub-Resource Handlers**
   - Create ThreatHandler with authorization checks and PATCH support
   - Implement DocumentHandler and SourceHandler (CRUD only, no PATCH)
   - Build MetadataHandler with individual key access and POST operations for all sub-resources
   - Enhance CellHandler with PATCH operations

2. **Authorization Integration**
   - Apply ValidateSubResourceAccess middleware to all sub-resource endpoints
   - Implement role-based access patterns
   - Add audit logging for authorization decisions

### Phase 4: Enhanced Operations
1. **PATCH Support for Cells**
   - Implement JSON Patch for cell properties
   - Support partial updates to cell data
   - Add validation for cell-specific patch operations
   - Enable batch cell updates

2. **Metadata Key Operations**
   - Individual metadata key GET/PUT/DELETE endpoints
   - Metadata inheritance patterns
   - Key-based access control

### Phase 5: Optimization & Compatibility
1. **Performance Enhancements**
   - Add response caching for authorization data
   - Implement database query optimization
   - Add pagination for large collections
   - Optimize JSON serialization/deserialization

2. **Backward Compatibility**
   - Maintain existing monolithic endpoints
   - Add response transformation utilities
   - Support both granular and complete object patterns
   - Provide migration utilities for existing data

## OpenAPI Schema Extensions

### New Schemas for Enhanced Operations

```json
{
  "MetadataItem": {
    "type": "object",
    "properties": {
      "key": {"type": "string", "maxLength": 255},
      "value": {"type": "string"},
      "created_at": {"type": "string", "format": "date-time"},
      "modified_at": {"type": "string", "format": "date-time"}
    },
    "required": ["key", "value"]
  },
  "MetadataKeyValue": {
    "type": "object",
    "properties": {
      "value": {"type": "string"}
    },
    "required": ["value"]
  },
  "CellPatchOperation": {
    "allOf": [
      {"$ref": "#/components/schemas/PatchOperation"},
      {
        "type": "object",
        "properties": {
          "cell_id": {"type": "string", "format": "uuid"}
        }
      }
    ]
  },
  "BatchCellPatchRequest": {
    "type": "object",
    "properties": {
      "operations": {
        "type": "array",
        "items": {"$ref": "#/components/schemas/CellPatchOperation"}
      }
    }
  },
  "ThreatList": {
    "allOf": [
      {"$ref": "#/components/schemas/PaginatedResponse"},
      {
        "properties": {
          "data": {
            "type": "array",
            "items": {"$ref": "#/components/schemas/Threat"}
          }
        }
      }
    ]
  },
  "AuthorizationInheritance": {
    "type": "object",
    "properties": {
      "threat_model_id": {"type": "string", "format": "uuid"},
      "owner": {"type": "string"},
      "authorization": {
        "type": "array",
        "items": {"$ref": "#/components/schemas/Authorization"}
      }
    }
  }
}
```

## Benefits of Enhanced Approach

### 1. **Granular Operations**
- Individual sub-resource CRUD operations
- Targeted metadata key management
- Efficient cell-level PATCH operations
- Reduced network overhead

### 2. **Comprehensive Authorization**
- Consistent authorization inheritance
- Integration with existing AccessCheck utility
- Role-based access control at all levels
- Audit trail for security compliance

### 3. **Performance & Scalability**
- Normalized database design
- Optimized queries for specific operations
- Reduced payload sizes
- Better caching opportunities

### 4. **Developer Experience**
- Intuitive RESTful patterns
- Fine-grained control over resources
- Backward compatibility maintained
- Clear authorization model

### 5. **Future-Proof Architecture**
- Extensible to new resource types
- Supports complex authorization patterns
- Scalable database design
- Maintainable codebase structure

## Summary of Key API Operations

### Complete Metadata Operations Matrix

| Resource Type | GET Collection | POST | GET Key | PUT Key | DELETE Key |
|---------------|----------------|------|---------|---------|------------|
| ThreatModel | ✓ | ✓ | ✓ | ✓ | ✓ |
| Threat | ✓ | ✓ | ✓ | ✓ | ✓ |
| Document | ✓ | ✓ | ✓ | ✓ | ✓ |
| Source Code | ✓ | ✓ | ✓ | ✓ | ✓ |
| Diagram | ✓ | ✓ | ✓ | ✓ | ✓ |
| Cell | ✓ | ✓ | ✓ | ✓ | ✓ |

### PATCH Support Matrix

| Resource Type | PATCH Support |
|---------------|---------------|
| ThreatModel | ✓ (existing) |
| Threat | ✓ |
| Document | ✗ (CRUD only) |
| Source Code | ✗ (CRUD only) |
| Diagram | ✓ (existing) |
| Cell | ✓ (NEW) |

### Authorization Inheritance

All sub-resources inherit authorization from their parent ThreatModel using the existing `AccessCheck` utility function, ensuring consistent security across all API operations.

This enhanced plan provides a comprehensive approach to granular API operations while maintaining security, performance, and compatibility with the existing TMI architecture.