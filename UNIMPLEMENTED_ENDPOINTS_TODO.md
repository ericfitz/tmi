# Unimplemented API Endpoints Todo List

## Priority 1: Individual Sub-Entity CRUD + Metadata Lists (22 endpoints)

### Threat Management
- [x] `GET /threat_models/{id}/threats` - ✅ **IMPLEMENTED**
- [x] `POST /threat_models/{id}/threats` - ✅ **IMPLEMENTED**
- [x] `GET /threat_models/{id}/threats/{threat_id}` - ✅ **IMPLEMENTED**
- [x] `PUT /threat_models/{id}/threats/{threat_id}` - ✅ **IMPLEMENTED**
- [x] `DELETE /threat_models/{id}/threats/{threat_id}` - ✅ **IMPLEMENTED**

### Document Management
- [x] `GET /threat_models/{id}/documents` - ✅ **IMPLEMENTED**
- [x] `POST /threat_models/{id}/documents` - ✅ **IMPLEMENTED**
- [x] `GET /threat_models/{id}/documents/{document_id}` - ✅ **IMPLEMENTED**
- [x] `PUT /threat_models/{id}/documents/{document_id}` - ✅ **IMPLEMENTED**
- [x] `DELETE /threat_models/{id}/documents/{document_id}` - ✅ **IMPLEMENTED**

### Source Management
- [x] `GET /threat_models/{id}/sources` - ✅ **IMPLEMENTED**
- [x] `POST /threat_models/{id}/sources` - ✅ **IMPLEMENTED**
- [x] `GET /threat_models/{id}/sources/{source_id}` - ✅ **IMPLEMENTED**
- [x] `PUT /threat_models/{id}/sources/{source_id}` - ✅ **IMPLEMENTED**
- [x] `DELETE /threat_models/{id}/sources/{source_id}` - ✅ **IMPLEMENTED**

### Metadata Lists
- [ ] `GET /diagrams/{id}/metadata` - List diagram metadata
- [ ] `GET /diagrams/{id}/cells/{cell_id}/metadata` - List cell metadata
- [ ] `GET /threat_models/{id}/diagrams/{diagram_id}/metadata` - List diagram metadata
- [ ] `GET /threat_models/{id}/threats/{threat_id}/metadata` - List threat metadata
- [ ] `GET /threat_models/{id}/documents/{document_id}/metadata` - List document metadata
- [ ] `GET /threat_models/{id}/sources/{source_id}/metadata` - List source metadata

## Priority 2: Individual Metadata CRUD (24 endpoints)

### Diagram Metadata
- [ ] `POST /diagrams/{id}/metadata` - Create diagram metadata
- [ ] `GET /diagrams/{id}/metadata/{key}` - Get specific metadata  
- [ ] `PUT /diagrams/{id}/metadata/{key}` - Update metadata
- [ ] `DELETE /diagrams/{id}/metadata/{key}` - Delete metadata

### Diagram Cell Metadata
- [ ] `POST /diagrams/{id}/cells/{cell_id}/metadata` - Create cell metadata
- [ ] `GET /diagrams/{id}/cells/{cell_id}/metadata/{key}` - Get cell metadata
- [ ] `PUT /diagrams/{id}/cells/{cell_id}/metadata/{key}` - Update cell metadata  
- [ ] `DELETE /diagrams/{id}/cells/{cell_id}/metadata/{key}` - Delete cell metadata

### Threat Model Diagram Metadata
- [ ] `POST /threat_models/{id}/diagrams/{diagram_id}/metadata` - Create metadata
- [ ] `GET /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}` - Get metadata
- [ ] `PUT /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}` - Update metadata
- [ ] `DELETE /threat_models/{id}/diagrams/{diagram_id}/metadata/{key}` - Delete metadata

### Threat Metadata
- [ ] `POST /threat_models/{id}/threats/{threat_id}/metadata` - Create metadata
- [ ] `GET /threat_models/{id}/threats/{threat_id}/metadata/{key}` - Get metadata
- [ ] `PUT /threat_models/{id}/threats/{threat_id}/metadata/{key}` - Update metadata
- [ ] `DELETE /threat_models/{id}/threats/{threat_id}/metadata/{key}` - Delete metadata

### Document Metadata
- [ ] `POST /threat_models/{id}/documents/{document_id}/metadata` - Create metadata  
- [ ] `GET /threat_models/{id}/documents/{document_id}/metadata/{key}` - Get metadata
- [ ] `PUT /threat_models/{id}/documents/{document_id}/metadata/{key}` - Update metadata
- [ ] `DELETE /threat_models/{id}/documents/{document_id}/metadata/{key}` - Delete metadata

### Source Metadata
- [ ] `POST /threat_models/{id}/sources/{source_id}/metadata` - Create metadata
- [ ] `GET /threat_models/{id}/sources/{source_id}/metadata/{key}` - Get metadata  
- [ ] `PUT /threat_models/{id}/sources/{source_id}/metadata/{key}` - Update metadata
- [ ] `DELETE /threat_models/{id}/sources/{source_id}/metadata/{key}` - Delete metadata

## Priority 3: PATCH Operations (2 endpoints)

- [ ] `PATCH /threat_models/{id}/threats/{threat_id}` - Patch threat
- [ ] `PATCH /diagrams/{id}/cells/{cell_id}` - Patch diagram cell

## Priority 4: Bulk Operations (9 endpoints)

- [ ] `POST /threat_models/{id}/threats/bulk` - Bulk create threats
- [ ] `PUT /threat_models/{id}/threats/bulk` - Bulk update threats
- [ ] `POST /threat_models/{id}/documents/bulk` - Bulk create documents
- [ ] `POST /threat_models/{id}/sources/bulk` - Bulk create sources
- [ ] `POST /diagrams/{id}/metadata/bulk` - Bulk metadata operations
- [ ] `POST /threat_models/{id}/diagrams/{diagram_id}/metadata/bulk` - Bulk operations
- [ ] `POST /threat_models/{id}/threats/{threat_id}/metadata/bulk` - Bulk operations
- [ ] `POST /threat_models/{id}/documents/{document_id}/metadata/bulk` - Bulk operations
- [ ] `POST /threat_models/{id}/sources/{source_id}/metadata/bulk` - Bulk operations

## Priority 5: Everything Else (3 endpoints)

- [ ] `POST /diagrams/{id}/cells/batch/patch` - Batch patch cells
- [ ] `POST /threat_models/{id}/threats/batch/patch` - Batch patch threats
- [ ] `DELETE /threat_models/{id}/threats/batch` - Batch delete threats

## Summary
- **Priority 1**: 22 endpoints (Individual CRUD + Metadata Lists)
- **Priority 2**: 24 endpoints (Individual Metadata CRUD) 
- **Priority 3**: 2 endpoints (PATCH operations)
- **Priority 4**: 9 endpoints (Bulk operations)
- **Priority 5**: 3 endpoints (Batch operations)
- **Already Implemented**: 2 endpoints ✅

**Total Remaining**: 60 endpoints