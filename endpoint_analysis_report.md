# TMI API Endpoint Analysis Report

==================================================

## Summary Statistics

- Total Endpoints: 77
- Implemented: 77 (100.0%)
- Stubbed: 0 (0.0%)
- Placeholder: 0 (0.0%)
- Not Found: 0 (0.0%)

## Authentication Endpoints

### ✅ GET /oauth2/callback

**Summary**: Handle OAuth callback
**Operation ID**: handleOAuthCallback
**Status**: Implemented
**Handler**: `HandleOAuthCallback()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ GET /oauth2/authorize/{provider}

**Summary**: Initiate OAuth authorization flow
**Operation ID**: authorizeOAuthProvider
**Status**: Implemented
**Handler**: `AuthorizeOAuthProvider()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ POST /oauth2/logout

**Summary**: Logout user
**Operation ID**: logoutUser
**Status**: Implemented
**Handler**: `LogoutUser()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ GET /oauth2/me

**Summary**: Get current user information
**Operation ID**: getCurrentUser
**Status**: Implemented
**Handler**: `GetCurrentUser()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ GET /oauth2/providers

**Summary**: List available OAuth providers
**Operation ID**: getAuthProviders
**Status**: Implemented
**Handler**: `GetAuthProviders()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ POST /oauth2/refresh

**Summary**: Refresh JWT token
**Operation ID**: refreshToken
**Status**: Implemented
**Handler**: `RefreshToken()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

### ✅ POST /oauth2/token/{provider}

**Summary**: Exchange OAuth authorization code for JWT tokens
**Operation ID**: exchangeOAuthCode
**Status**: Implemented
**Handler**: `ExchangeOAuthCode()` in `server.go`
**Middleware Stack**: RequestTracing → CORS

## Threat Models Endpoints

### ✅ GET /threat_models

**Summary**: List threat models
**Operation ID**: listThreatModels
**Status**: Implemented
**Handler**: `GetThreatModels()` in `threat_model_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication
**Notes**:

- Database-backed implementation

### ✅ POST /threat_models

**Summary**: Create a threat model
**Operation ID**: createThreatModel
**Status**: Implemented
**Handler**: `CreateThreatModel()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication
**Notes**:

- Database-backed implementation

### ✅ DELETE /threat_models/{threat_model_id}

**Summary**: Delete a threat model
**Operation ID**: deleteThreatModel
**Status**: Implemented
**Handler**: `DeleteThreatModel()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}

**Summary**: Retrieve a threat model
**Operation ID**: getThreatModel
**Status**: Implemented
**Handler**: `GetThreatModel()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PATCH /threat_models/{threat_model_id}

**Summary**: Partially update a threat model
**Operation ID**: patchThreatModel
**Status**: Implemented
**Handler**: `PatchThreatModel()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}

**Summary**: Update a threat model
**Operation ID**: updateThreatModel
**Status**: Implemented
**Handler**: `UpdateThreatModel()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

## Diagrams Endpoints

### ✅ GET /threat_models/{threat_model_id}/diagrams

**Summary**: List threat model diagrams
**Operation ID**: getThreatModelDiagrams
**Status**: Implemented
**Handler**: `GetThreatModelDiagrams()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware
**Notes**:

- WebSocket support

### ✅ POST /threat_models/{threat_model_id}/diagrams

**Summary**: Create a new diagram
**Operation ID**: createThreatModelDiagram
**Status**: Implemented
**Handler**: `CreateThreatModelDiagram()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}

**Summary**: Delete a diagram
**Operation ID**: deleteThreatModelDiagram
**Status**: Implemented
**Handler**: `DeleteThreatModelDiagram()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ GET /threat_models/{threat_model_id}/diagrams/{diagram_id}

**Summary**: Get a specific diagram
**Operation ID**: getThreatModelDiagram
**Status**: Implemented
**Handler**: `GetThreatModelDiagram()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ PATCH /threat_models/{threat_model_id}/diagrams/{diagram_id}

**Summary**: Partially update a diagram
**Operation ID**: patchThreatModelDiagram
**Status**: Implemented
**Handler**: `PatchDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}

**Summary**: Update a diagram
**Operation ID**: updateThreatModelDiagram
**Status**: Implemented
**Handler**: `UpdateThreatModelDiagram()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate

**Summary**: End diagram collaboration session
**Operation ID**: endDiagramCollaborationSession
**Status**: Implemented
**Handler**: `DeleteDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate

**Summary**: Get diagram collaboration session
**Operation ID**: getDiagramCollaborationSession
**Status**: Implemented
**Handler**: `GetDiagrams()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate

**Summary**: Create diagram collaboration session
**Operation ID**: createDiagramCollaborationSession
**Status**: Implemented
**Handler**: `CreateDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate

**Summary**: Join diagram collaboration session
**Operation ID**: joinDiagramCollaborationSession
**Status**: Implemented
**Handler**: `UpdateDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata

**Summary**: Get diagram metadata
**Operation ID**: getDiagramMetadata
**Status**: Implemented
**Handler**: `GetDiagrams()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata

**Summary**: Create diagram metadata
**Operation ID**: createDiagramMetadata
**Status**: Implemented
**Handler**: `CreateDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk

**Summary**: Bulk create diagram metadata
**Operation ID**: bulkCreateDiagramMetadata
**Status**: Implemented
**Handler**: `CreateDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}

**Summary**: Delete diagram metadata by key
**Operation ID**: deleteDiagramMetadataByKey
**Status**: Implemented
**Handler**: `DeleteDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}

**Summary**: Get diagram metadata by key
**Operation ID**: getDiagramMetadataByKey
**Status**: Implemented
**Handler**: `GetDiagrams()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

### ✅ PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}

**Summary**: Update diagram metadata by key
**Operation ID**: updateDiagramMetadataByKey
**Status**: Implemented
**Handler**: `UpdateDiagram()` in `threat_model_diagram_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware → DiagramMiddleware

## Threats Endpoints

### ✅ GET /threat_models/{threat_model_id}/threats

**Summary**: List threats in a threat model
**Operation ID**: getThreatModelThreats
**Status**: Implemented
**Handler**: `GetThreatModelThreats()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/threats

**Summary**: Create a new threat
**Operation ID**: createThreatModelThreat
**Status**: Implemented
**Handler**: `CreateThreatModelThreat()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/threats/batch

**Summary**: Batch delete threats
**Operation ID**: batchDeleteThreatModelThreats
**Status**: Implemented
**Handler**: `DeleteThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/threats/batch/patch

**Summary**: Batch patch threats
**Operation ID**: batchPatchThreatModelThreats
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/threats/bulk

**Summary**: Bulk create threats
**Operation ID**: bulkCreateThreatModelThreats
**Status**: Implemented
**Handler**: `BulkCreateThreatModelThreats()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/threats/bulk

**Summary**: Bulk update threats
**Operation ID**: bulkUpdateThreatModelThreats
**Status**: Implemented
**Handler**: `BulkUpdateThreatModelThreats()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/threats/{threat_id}

**Summary**: Delete a threat
**Operation ID**: deleteThreatModelThreat
**Status**: Implemented
**Handler**: `DeleteThreatModelThreat()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/threats/{threat_id}

**Summary**: Get a specific threat
**Operation ID**: getThreatModelThreat
**Status**: Implemented
**Handler**: `GetThreatModelThreat()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PATCH /threat_models/{threat_model_id}/threats/{threat_id}

**Summary**: Partially update a threat
**Operation ID**: patchThreatModelThreat
**Status**: Implemented
**Handler**: `PatchThreatModelThreat()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/threats/{threat_id}

**Summary**: Update a threat
**Operation ID**: updateThreatModelThreat
**Status**: Implemented
**Handler**: `UpdateThreatModelThreat()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata

**Summary**: Get threat metadata
**Operation ID**: getThreatMetadata
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata

**Summary**: Create threat metadata
**Operation ID**: createThreatMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk

**Summary**: Bulk create threat metadata
**Operation ID**: bulkCreateThreatMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}

**Summary**: Delete threat metadata by key
**Operation ID**: deleteThreatMetadataByKey
**Status**: Implemented
**Handler**: `DeleteThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}

**Summary**: Get threat metadata by key
**Operation ID**: getThreatMetadataByKey
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}

**Summary**: Update threat metadata by key
**Operation ID**: updateThreatMetadataByKey
**Status**: Implemented
**Handler**: `UpdateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

## Documents Endpoints

### ✅ GET /threat_models/{threat_model_id}/documents

**Summary**: List documents in a threat model
**Operation ID**: getThreatModelDocuments
**Status**: Implemented
**Handler**: `GetThreatModelDocuments()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/documents

**Summary**: Create a new document
**Operation ID**: createThreatModelDocument
**Status**: Implemented
**Handler**: `CreateThreatModelDocument()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/documents/bulk

**Summary**: Bulk create documents
**Operation ID**: bulkCreateThreatModelDocuments
**Status**: Implemented
**Handler**: `BulkCreateThreatModelDocuments()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/documents/{document_id}

**Summary**: Delete a document
**Operation ID**: deleteThreatModelDocument
**Status**: Implemented
**Handler**: `DeleteThreatModelDocument()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/documents/{document_id}

**Summary**: Get a specific document
**Operation ID**: getThreatModelDocument
**Status**: Implemented
**Handler**: `GetThreatModelDocument()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/documents/{document_id}

**Summary**: Update a document
**Operation ID**: updateThreatModelDocument
**Status**: Implemented
**Handler**: `UpdateThreatModelDocument()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/documents/{document_id}/metadata

**Summary**: Get document metadata
**Operation ID**: getDocumentMetadata
**Status**: Implemented
**Handler**: `GetDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/documents/{document_id}/metadata

**Summary**: Create document metadata
**Operation ID**: createDocumentMetadata
**Status**: Implemented
**Handler**: `CreateDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk

**Summary**: Bulk create document metadata
**Operation ID**: bulkCreateDocumentMetadata
**Status**: Implemented
**Handler**: `CreateDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}

**Summary**: Delete document metadata by key
**Operation ID**: deleteDocumentMetadataByKey
**Status**: Implemented
**Handler**: `DeleteDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}

**Summary**: Get document metadata by key
**Operation ID**: getDocumentMetadataByKey
**Status**: Implemented
**Handler**: `GetDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}

**Summary**: Update document metadata by key
**Operation ID**: updateDocumentMetadataByKey
**Status**: Implemented
**Handler**: `UpdateDocument()` in `document_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

## Sources Endpoints

### ✅ GET /threat_models/{threat_model_id}/sources

**Summary**: List sources in a threat model
**Operation ID**: getThreatModelSources
**Status**: Implemented
**Handler**: `GetThreatModelSources()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/sources

**Summary**: Create a new source reference
**Operation ID**: createThreatModelSource
**Status**: Implemented
**Handler**: `CreateThreatModelSource()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/sources/bulk

**Summary**: Bulk create sources
**Operation ID**: bulkCreateThreatModelSources
**Status**: Implemented
**Handler**: `BulkCreateThreatModelSources()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/sources/{source_id}

**Summary**: Delete a source reference
**Operation ID**: deleteThreatModelSource
**Status**: Implemented
**Handler**: `DeleteThreatModelSource()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/sources/{source_id}

**Summary**: Get a specific source reference
**Operation ID**: getThreatModelSource
**Status**: Implemented
**Handler**: `GetThreatModelSource()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/sources/{source_id}

**Summary**: Update a source reference
**Operation ID**: updateThreatModelSource
**Status**: Implemented
**Handler**: `UpdateThreatModelSource()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/sources/{source_id}/metadata

**Summary**: Get source metadata
**Operation ID**: getSourceMetadata
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/sources/{source_id}/metadata

**Summary**: Create source metadata
**Operation ID**: createSourceMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/sources/{source_id}/metadata/bulk

**Summary**: Bulk create source metadata
**Operation ID**: bulkCreateSourceMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}

**Summary**: Delete source metadata by key
**Operation ID**: deleteSourceMetadataByKey
**Status**: Implemented
**Handler**: `DeleteThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}

**Summary**: Get source metadata by key
**Operation ID**: getSourceMetadataByKey
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}

**Summary**: Update source metadata by key
**Operation ID**: updateSourceMetadataByKey
**Status**: Implemented
**Handler**: `UpdateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

## Metadata Endpoints

### ✅ GET /threat_models/{threat_model_id}/metadata

**Summary**: Get threat model metadata
**Operation ID**: getThreatModelMetadata
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/metadata

**Summary**: Create threat model metadata
**Operation ID**: createThreatModelMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ POST /threat_models/{threat_model_id}/metadata/bulk

**Summary**: Bulk create threat model metadata
**Operation ID**: bulkCreateThreatModelMetadata
**Status**: Implemented
**Handler**: `CreateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ DELETE /threat_models/{threat_model_id}/metadata/{key}

**Summary**: Delete threat model metadata by key
**Operation ID**: deleteThreatModelMetadataByKey
**Status**: Implemented
**Handler**: `DeleteThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ GET /threat_models/{threat_model_id}/metadata/{key}

**Summary**: Get threat model metadata by key
**Operation ID**: getThreatModelMetadataByKey
**Status**: Implemented
**Handler**: `GetThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

### ✅ PUT /threat_models/{threat_model_id}/metadata/{key}

**Summary**: Update threat model metadata by key
**Operation ID**: updateThreatModelMetadataByKey
**Status**: Implemented
**Handler**: `UpdateThreat()` in `threat_sub_resource_handlers.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication → ThreatModelMiddleware

## Collaboration Endpoints

### ✅ GET /collaboration/sessions

**Summary**: List active collaboration sessions
**Operation ID**: getCollaborationSessions
**Status**: Implemented
**Handler**: `GetCollaborationSessions()` in `server.go`
**Middleware Stack**: RequestTracing → CORS → OpenAPIValidation → JWTAuthentication

## System Endpoints

### ✅ GET /

**Summary**: Get API information
**Operation ID**: getApiInfo
**Status**: Implemented
**Handler**: `GetApiInfo()` in `server.go`
**Middleware Stack**: RequestTracing → CORS
