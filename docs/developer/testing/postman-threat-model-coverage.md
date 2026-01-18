# TMI Threat Model API - Postman Test Coverage Analysis

This document provides a comprehensive analysis of Postman test coverage for threat model endpoints and all sub-objects.

## Executive Summary

| Metric | Value |
|--------|-------|
| Total Threat Model Paths | 41 |
| Total Operations | 121 |
| Operations with Success Tests | ~85 (70%) |
| Operations with 401 Tests | ~25 (21%) |
| Operations with 403 Tests | ~15 (12%) |
| Operations with 404 Tests | ~35 (29%) |
| Operations with 400 Tests | ~30 (25%) |

**Key Findings:**
- Good coverage for success scenarios (200/201/204)
- Significant gaps in 401 (unauthorized) tests for sub-resources
- Limited 403 (forbidden) tests - mainly at threat model level
- No 429 (rate limit) tests
- No 500 (server error) tests
- Limited 409 (conflict) and 422 (unprocessable) edge case tests

---

## 1. Endpoint Inventory

### 1.1 Threat Models (Core)

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models` | GET | listThreatModels | 200, 400, 401, 429, 500 |
| `/threat_models` | POST | createThreatModel | 201, 400, 401, 429, 500 |
| `/threat_models/{id}` | GET | getThreatModel | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}` | PUT | updateThreatModel | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}` | PATCH | patchThreatModel | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}` | DELETE | deleteThreatModel | 204, 400, 401, 403, 404, 409, 429, 500 |

### 1.2 Threats

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/threats` | GET | getThreatModelThreats | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats` | POST | createThreatModelThreat | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}` | GET | getThreatModelThreat | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}` | PUT | updateThreatModelThreat | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}` | PATCH | patchThreatModelThreat | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}` | DELETE | deleteThreatModelThreat | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/bulk` | POST | bulkCreateThreatModelThreats | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/bulk` | PUT | bulkUpdateThreatModelThreats | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/bulk` | PATCH | bulkPatchThreatModelThreats | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/bulk` | DELETE | bulkDeleteThreatModelThreats | 200, 400, 401, 403, 404, 429, 500 |

### 1.3 Threat Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/threats/{tid}/metadata` | GET | getThreatMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata` | POST | createThreatMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata/{key}` | GET | getThreatMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata/{key}` | PUT | updateThreatMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata/{key}` | DELETE | deleteThreatMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata/bulk` | POST | bulkCreateThreatMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/threats/{tid}/metadata/bulk` | PUT | bulkUpsertThreatMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.4 Documents

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/documents` | GET | getThreatModelDocuments | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents` | POST | createThreatModelDocument | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}` | GET | getThreatModelDocument | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}` | PUT | updateThreatModelDocument | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}` | PATCH | patchThreatModelDocument | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}` | DELETE | deleteThreatModelDocument | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/bulk` | POST | bulkCreateThreatModelDocuments | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/bulk` | PUT | bulkUpsertThreatModelDocuments | 201, 400, 401, 403, 404, 429, 500 |

### 1.5 Document Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/documents/{did}/metadata` | GET | getDocumentMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata` | POST | createDocumentMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata/{key}` | GET | getDocumentMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata/{key}` | PUT | updateDocumentMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata/{key}` | DELETE | deleteDocumentMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata/bulk` | POST | bulkCreateDocumentMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/documents/{did}/metadata/bulk` | PUT | bulkUpsertDocumentMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.6 Diagrams

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/diagrams` | GET | getThreatModelDiagrams | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams` | POST | createThreatModelDiagram | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}` | GET | getThreatModelDiagram | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}` | PUT | updateThreatModelDiagram | 200, 400, 401, 403, 404, **409**, 429, 500 |
| `/threat_models/{id}/diagrams/{did}` | PATCH | patchThreatModelDiagram | 200, 400, 401, 403, 404, **409**, **422**, 429, 500 |
| `/threat_models/{id}/diagrams/{did}` | DELETE | deleteThreatModelDiagram | 204, 400, 401, 403, 404, **409**, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/collaborate` | GET | getDiagramCollaborationSession | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/collaborate` | POST | createDiagramCollaborationSession | 201, 400, 401, 403, 404, **409**, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/collaborate` | DELETE | endDiagramCollaborationSession | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/model` | GET | getDiagramModel | 200, 400, 401, 403, 404, 429, 500 |

### 1.7 Diagram Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/diagrams/{did}/metadata` | GET | getDiagramMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata` | POST | createDiagramMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata/{key}` | GET | getDiagramMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata/{key}` | PUT | updateDiagramMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata/{key}` | DELETE | deleteDiagramMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata/bulk` | POST | bulkCreateDiagramMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/diagrams/{did}/metadata/bulk` | PUT | bulkUpsertDiagramMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.8 Repositories

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/repositories` | GET | getThreatModelRepositories | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories` | POST | createThreatModelRepository | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}` | GET | getThreatModelRepository | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}` | PUT | updateThreatModelRepository | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}` | PATCH | patchThreatModelRepository | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}` | DELETE | deleteThreatModelRepository | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/bulk` | POST | bulkCreateThreatModelRepositories | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/bulk` | PUT | bulkUpsertThreatModelRepositories | 201, 400, 401, 403, 404, 429, 500 |

### 1.9 Repository Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/repositories/{rid}/metadata` | GET | getRepositoryMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata` | POST | createRepositoryMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata/{key}` | GET | getRepositoryMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata/{key}` | PUT | updateRepositoryMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata/{key}` | DELETE | deleteRepositoryMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata/bulk` | POST | bulkCreateRepositoryMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/repositories/{rid}/metadata/bulk` | PUT | bulkUpsertRepositoryMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.10 Assets

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/assets` | GET | getThreatModelAssets | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets` | POST | createThreatModelAsset | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}` | GET | getThreatModelAsset | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}` | PUT | updateThreatModelAsset | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}` | PATCH | patchThreatModelAsset | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}` | DELETE | deleteThreatModelAsset | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/bulk` | POST | bulkCreateThreatModelAssets | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/bulk` | PUT | bulkUpsertThreatModelAssets | 201, 400, 401, 403, 404, 429, 500 |

### 1.11 Asset Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/assets/{aid}/metadata` | GET | getThreatModelAssetMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata` | POST | createThreatModelAssetMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata/{key}` | GET | getThreatModelAssetMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata/{key}` | PUT | updateThreatModelAssetMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata/{key}` | DELETE | deleteThreatModelAssetMetadata | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata/bulk` | POST | bulkCreateThreatModelAssetMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/assets/{aid}/metadata/bulk` | PUT | bulkUpsertThreatModelAssetMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.12 Notes

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/notes` | GET | getThreatModelNotes | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes` | POST | createThreatModelNote | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}` | GET | getThreatModelNote | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}` | PUT | updateThreatModelNote | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}` | PATCH | patchThreatModelNote | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}` | DELETE | deleteThreatModelNote | 204, 400, 401, 403, 404, 429, 500 |

### 1.13 Note Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/notes/{nid}/metadata` | GET | getNoteMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata` | POST | createNoteMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata/{key}` | GET | getNoteMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata/{key}` | PUT | updateNoteMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata/{key}` | DELETE | deleteNoteMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata/bulk` | POST | bulkCreateNoteMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/notes/{nid}/metadata/bulk` | PUT | bulkUpdateNoteMetadata | 200, 400, 401, 403, 404, 429, 500 |

### 1.14 Threat Model Metadata

| Path | Method | Operation ID | Response Codes |
|------|--------|--------------|----------------|
| `/threat_models/{id}/metadata` | GET | getThreatModelMetadata | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata` | POST | createThreatModelMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata/{key}` | GET | getThreatModelMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata/{key}` | PUT | updateThreatModelMetadataByKey | 200, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata/{key}` | DELETE | deleteThreatModelMetadataByKey | 204, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata/bulk` | POST | bulkCreateThreatModelMetadata | 201, 400, 401, 403, 404, 429, 500 |
| `/threat_models/{id}/metadata/bulk` | PUT | bulkUpsertThreatModelMetadata | 200, 400, 401, 403, 404, 429, 500 |

---

## 2. Coverage Matrix by Resource

Legend: ✅ = Covered | ❌ = Gap | ➖ = N/A for this operation

### 2.1 Threat Models (Core)

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /threat_models | ✅ | ❌ | ✅ | ➖ | ➖ | ➖ | ➖ | ❌ | ❌ | comprehensive, unauthorized |
| POST /threat_models | ✅ | ✅ | ✅ | ➖ | ➖ | ➖ | ➖ | ❌ | ❌ | comprehensive, unauthorized |
| GET /threat_models/{id} | ✅ | ❌ | ✅ | ✅ | ✅ | ➖ | ➖ | ❌ | ❌ | comprehensive, permission-matrix |
| PUT /threat_models/{id} | ✅ | ❌ | ✅ | ✅ | ✅ | ➖ | ➖ | ❌ | ❌ | comprehensive |
| PATCH /threat_models/{id} | ✅ | ✅ | ✅ | ✅ | ❌ | ➖ | ✅ | ❌ | ❌ | comprehensive, advanced-error |
| DELETE /threat_models/{id} | ✅ | ❌ | ✅ | ✅ | ✅ | ❌ | ➖ | ❌ | ❌ | comprehensive |

### 2.2 Threats

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /threats | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | threat-crud |
| POST /threats | ✅ | ✅ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | threat-crud |
| GET /threats/{tid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | threat-crud |
| PUT /threats/{tid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | threat-crud |
| PATCH /threats/{tid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | ❌ | threat-crud |
| DELETE /threats/{tid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | threat-crud |
| POST /threats/bulk | ✅ | ✅ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | bulk-operations |
| PUT /threats/bulk | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | bulk-operations |
| PATCH /threats/bulk | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | bulk-operations |
| DELETE /threats/bulk | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | bulk-operations |

### 2.3 Documents

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /documents | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | document-crud |
| POST /documents | ✅ | ✅ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | document-crud |
| GET /documents/{did} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | document-crud |
| PUT /documents/{did} | ✅ | ✅ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | document-crud |
| PATCH /documents/{did} | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | ❌ | - |
| DELETE /documents/{did} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | document-crud |
| POST /documents/bulk | ✅ | ✅ | ✅ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | document-crud |
| PUT /documents/bulk | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |

### 2.4 Diagrams

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /diagrams | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |
| POST /diagrams | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | collaboration |
| GET /diagrams/{did} | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |
| PUT /diagrams/{did} | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | - |
| PATCH /diagrams/{did} | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | - |
| DELETE /diagrams/{did} | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | - |
| GET /collaborate | ✅ | ❌ | ❌ | ✅ | ✅ | ➖ | ➖ | ❌ | ❌ | collaboration |
| POST /collaborate | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ➖ | ❌ | ❌ | collaboration, advanced-error |
| DELETE /collaborate | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | collaboration |
| GET /model | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |

### 2.5 Repositories

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /repositories | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| POST /repositories | ✅ | ✅ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| GET /repositories/{rid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| PUT /repositories/{rid} | ✅ | ✅ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| PATCH /repositories/{rid} | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | ❌ | - |
| DELETE /repositories/{rid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| POST /repositories/bulk | ✅ | ✅ | ✅ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | repository-crud |
| PUT /repositories/bulk | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |

### 2.6 Assets

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /assets | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | assets |
| POST /assets | ✅ | ✅ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | assets |
| GET /assets/{aid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | assets |
| PUT /assets/{aid} | ✅ | ❌ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | assets |
| PATCH /assets/{aid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | ❌ | assets |
| DELETE /assets/{aid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | assets |
| POST /assets/bulk | ✅ | ✅ | ✅ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | assets |
| PUT /assets/bulk | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | assets |

### 2.7 Notes

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /notes | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | notes |
| POST /notes | ✅ | ✅ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | notes |
| GET /notes/{nid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | notes |
| PUT /notes/{nid} | ✅ | ❌ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | notes |
| PATCH /notes/{nid} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ❌ | ❌ | ❌ | notes |
| DELETE /notes/{nid} | ✅ | ❌ | ✅ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | notes |

### 2.8 Threat Model Metadata

| Operation | 200/201/204 | 400 | 401 | 403 | 404 | 409 | 422 | 429 | 500 | Collection |
|-----------|-------------|-----|-----|-----|-----|-----|-----|-----|-----|------------|
| GET /metadata | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | metadata, complete-metadata |
| POST /metadata | ✅ | ✅ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | metadata |
| GET /metadata/{key} | ✅ | ❌ | ❌ | ❌ | ✅ | ➖ | ➖ | ❌ | ❌ | metadata |
| PUT /metadata/{key} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | metadata |
| DELETE /metadata/{key} | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | metadata |
| POST /metadata/bulk | ✅ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | complete-metadata |
| PUT /metadata/bulk | ❌ | ❌ | ❌ | ❌ | ❌ | ➖ | ➖ | ❌ | ❌ | - |

### 2.9 Sub-resource Metadata (Threats, Documents, Diagrams, Repositories, Assets, Notes)

| Resource | GET | POST | GET/key | PUT/key | DELETE/key | POST/bulk | PUT/bulk |
|----------|-----|------|---------|---------|------------|-----------|----------|
| Threat Metadata | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Document Metadata | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Diagram Metadata | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Repository Metadata | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Asset Metadata | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Note Metadata | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |

---

## 3. Authorization Test Matrix

### 3.1 Required Test Scenarios

| ID | Scenario | HTTP Methods | Expected Result |
|----|----------|--------------|-----------------|
| A1 | Owner performs operation | ALL | Success (200/201/204) |
| A2 | Writer performs read | GET | 200 |
| A3 | Writer performs write | POST, PUT, PATCH | Success |
| A4 | Writer performs delete | DELETE | 403 |
| A5 | Reader performs read | GET | 200 |
| A6 | Reader performs write | POST, PUT, PATCH | 403 |
| A7 | Reader performs delete | DELETE | 403 |
| A8 | No access user | ALL | 403 |
| A9 | No authentication | ALL | 401 |

### 3.2 Current Authorization Coverage

| Resource | A1 Owner | A2 Writer Read | A3 Writer Write | A4 Writer Delete | A5 Reader Read | A6 Reader Write | A7 Reader Delete | A8 No Access | A9 No Auth |
|----------|----------|----------------|-----------------|------------------|----------------|-----------------|------------------|--------------|------------|
| Threat Models | ✅ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ | ✅ | ✅ |
| Threats | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Documents | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Diagrams | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Repositories | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Assets | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| Notes | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| TM Metadata | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| Collaboration | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ | ❌ |

---

## 4. Gap Analysis

### 4.1 Priority 1: Critical Gaps (Security)

These gaps represent missing tests for security-critical scenarios.

#### 4.1.1 Missing 401 Tests (No Authentication)

The following operations lack 401 (unauthorized) tests:

| Resource | Operations Missing 401 Tests |
|----------|------------------------------|
| Threats | GET list, GET single, PUT, PATCH, DELETE, all bulk ops |
| Diagrams | ALL operations |
| Diagram Metadata | ALL operations |
| Threat Metadata | ALL operations |
| Document Metadata | ALL operations |
| Repository Metadata | ALL operations |
| TM Metadata | ALL operations |
| Collaboration | GET, POST, DELETE |

**Total: ~50 operations missing 401 tests**

#### 4.1.2 Missing 403 Tests (Forbidden)

The following operations lack 403 (forbidden) tests for authorization scenarios:

| Resource | Operations Missing 403 Tests |
|----------|------------------------------|
| Threats | ALL operations (reader-write, writer-delete, no-access) |
| Documents | ALL operations except create |
| Diagrams | ALL operations |
| Repositories | ALL operations |
| Assets | ALL operations |
| Notes | ALL operations |
| All Metadata | ALL operations |
| Bulk Operations | ALL operations |

**Total: ~100 operations missing one or more 403 scenarios**

### 4.2 Priority 2: Important Gaps (Validation)

#### 4.2.1 Missing 400 Tests (Bad Request)

| Resource | Operations Missing 400 Tests |
|----------|------------------------------|
| GET operations | Malformed IDs, invalid query params |
| PUT operations | Missing required fields, invalid field values |
| PATCH operations | Invalid patch operations |
| Threats | GET, PUT, PATCH, DELETE, bulk update/patch/delete |
| Diagrams | ALL CRUD operations |
| Most Metadata ops | Missing key, invalid value types |

#### 4.2.2 Missing 404 Tests (Not Found)

| Resource | Operations Missing 404 Tests |
|----------|------------------------------|
| Threats | GET single, PUT, PATCH, DELETE |
| Diagrams | ALL CRUD and metadata operations |
| Threat Metadata | ALL operations |
| Document Metadata | ALL operations |
| Diagram Metadata | ALL operations |
| Repository Metadata | ALL operations |
| Bulk operations | Parent resource not found |

### 4.3 Priority 3: Enhancement Gaps (Edge Cases)

#### 4.3.1 Missing 409 Tests (Conflict)

| Resource | Scenario | Status |
|----------|----------|--------|
| Threat Model DELETE | Active collaboration session | ❌ |
| Diagram PUT | Version conflict (update_vector) | ❌ |
| Diagram PATCH | Version conflict | ❌ |
| Diagram DELETE | Active collaboration session | ❌ |
| Collaboration POST | Session already exists | ✅ |

#### 4.3.2 Missing 422 Tests (Unprocessable Entity)

| Resource | Scenario | Status |
|----------|----------|--------|
| Threat Model PATCH | Invalid JSON Patch semantics | ✅ |
| Diagram PATCH | Invalid JSON Patch semantics | ❌ |
| All other PATCH | Invalid JSON Patch semantics | ❌ |

#### 4.3.3 Missing 429 Tests (Rate Limit)

**No rate limit tests exist for any endpoint.**

#### 4.3.4 Missing 500 Tests (Server Error)

**No server error tests exist. Consider adding tests that verify proper error handling for internal failures.**

---

## 5. Recommended Test Cases

### 5.1 Priority 1: Security Tests

#### 5.1.1 Extend `unauthorized-tests-collection.json`

Add 401 tests for all sub-resources:

```
# Threats
POST /threat_models/{id}/threats - Unauthorized (401)
GET /threat_models/{id}/threats - Unauthorized (401)
GET /threat_models/{id}/threats/{tid} - Unauthorized (401)
PUT /threat_models/{id}/threats/{tid} - Unauthorized (401)
PATCH /threat_models/{id}/threats/{tid} - Unauthorized (401)
DELETE /threat_models/{id}/threats/{tid} - Unauthorized (401)

# Diagrams
POST /threat_models/{id}/diagrams - Unauthorized (401)
GET /threat_models/{id}/diagrams - Unauthorized (401)
GET /threat_models/{id}/diagrams/{did} - Unauthorized (401)
PUT /threat_models/{id}/diagrams/{did} - Unauthorized (401)
PATCH /threat_models/{id}/diagrams/{did} - Unauthorized (401)
DELETE /threat_models/{id}/diagrams/{did} - Unauthorized (401)

# Collaboration
GET /threat_models/{id}/diagrams/{did}/collaborate - Unauthorized (401)
POST /threat_models/{id}/diagrams/{did}/collaborate - Unauthorized (401)
DELETE /threat_models/{id}/diagrams/{did}/collaborate - Unauthorized (401)

# All Metadata endpoints (TM, Threat, Document, Diagram, Repository, Asset, Note)
GET /threat_models/{id}/metadata - Unauthorized (401)
POST /threat_models/{id}/metadata - Unauthorized (401)
# ... (repeat for all metadata paths)

# Bulk operations
POST /threat_models/{id}/threats/bulk - Unauthorized (401)
# ... (repeat for all bulk paths)
```

**Estimated: 40-50 new test cases**

#### 5.1.2 Extend `permission-matrix-tests-collection.json`

Add 403 tests for sub-resources with three scenarios each:
1. Reader attempting write operation
2. Reader attempting delete operation
3. User with no access to parent threat model

For each sub-resource (Threats, Documents, Diagrams, Repositories, Assets, Notes):

```
# Reader Write (403)
POST /threat_models/{id}/threats as reader - Forbidden (403)
PUT /threat_models/{id}/threats/{tid} as reader - Forbidden (403)
PATCH /threat_models/{id}/threats/{tid} as reader - Forbidden (403)

# Reader Delete (403)
DELETE /threat_models/{id}/threats/{tid} as reader - Forbidden (403)

# No Access (403)
GET /threat_models/{id}/threats as no-access user - Forbidden (403)
POST /threat_models/{id}/threats as no-access user - Forbidden (403)
```

**Estimated: 60-80 new test cases**

### 5.2 Priority 2: Validation Tests

#### 5.2.1 Create/Extend validation test collections

Add 400 tests for:

```
# Threats validation
POST /threats - Empty name (400)
POST /threats - Invalid threat_type enum (400)
POST /threats - Invalid severity enum (400)
POST /threats - Score out of range (400)
PUT /threats/{tid} - Missing required fields (400)
PATCH /threats/{tid} - Invalid patch operation (400)

# Diagrams validation
POST /diagrams - Missing name (400)
POST /diagrams - Invalid cells format (400)
PUT /diagrams/{did} - Missing required fields (400)
PATCH /diagrams/{did} - Invalid JSON Patch (400)

# Metadata validation
POST /metadata - Missing key (400)
POST /metadata - Invalid value type (400)
PUT /metadata/{key} - Empty value (400)
```

**Estimated: 30-40 new test cases**

#### 5.2.2 Add 404 tests for nested resources

```
# Threats 404
GET /threat_models/{invalid}/threats - Parent Not Found (404)
GET /threat_models/{id}/threats/{invalid} - Threat Not Found (404)
PUT /threat_models/{id}/threats/{invalid} - Threat Not Found (404)
DELETE /threat_models/{id}/threats/{invalid} - Threat Not Found (404)

# Metadata 404
GET /threat_models/{id}/threats/{invalid}/metadata - Parent Not Found (404)
GET /threat_models/{id}/metadata/{invalid} - Key Not Found (404)
```

**Estimated: 25-35 new test cases**

### 5.3 Priority 3: Edge Case Tests

#### 5.3.1 Conflict (409) Tests

```
# Diagram conflicts
PUT /diagrams/{did} with stale update_vector - Conflict (409)
DELETE /diagrams/{did} with active session - Conflict (409)
DELETE /threat_models/{id} with active collaboration - Conflict (409)
```

#### 5.3.2 Unprocessable Entity (422) Tests

```
# JSON Patch semantic errors
PATCH /diagrams/{did} - Test path does not exist (422)
PATCH /diagrams/{did} - Copy from non-existent source (422)
```

**Estimated: 5-10 new test cases**

---

## 6. Implementation Recommendations

### 6.1 Collection Organization

| Collection | Purpose | New Tests |
|------------|---------|-----------|
| `unauthorized-tests-collection.json` | Extend with all 401 tests | +45 tests |
| `permission-matrix-tests-collection.json` | Extend with sub-resource 403 tests | +70 tests |
| `validation-tests-collection.json` | NEW: Centralize 400/422 tests | +40 tests |
| `not-found-tests-collection.json` | NEW: Centralize 404 tests | +30 tests |
| `advanced-error-scenarios-collection.json` | Extend with 409 tests | +8 tests |

### 6.2 Testing Order

1. **Phase 1**: Add all 401 tests (highest security impact)
2. **Phase 2**: Add 403 tests for sub-resources (authorization gaps)
3. **Phase 3**: Add 400/404 tests (validation and resource existence)
4. **Phase 4**: Add 409/422 edge case tests

### 6.3 Maintenance

- Update this document when new endpoints are added to the OpenAPI spec
- Run coverage analysis quarterly to identify drift
- Consider automated coverage tracking via CI/CD

---

## Appendix A: Collection File Reference

| Collection File | Primary Focus |
|-----------------|---------------|
| `threat-crud-tests-collection.json` | Threat entity CRUD |
| `permission-matrix-tests-collection.json` | Multi-user authorization |
| `repository-crud-tests-collection.json` | Repository CRUD |
| `document-crud-tests-collection.json` | Document CRUD |
| `assets-tests-collection.json` | Asset CRUD + metadata |
| `notes-tests-collection.json` | Note CRUD + metadata |
| `metadata-tests-collection.json` | TM-level metadata |
| `complete-metadata-tests-collection.json` | Enhanced metadata tests |
| `bulk-operations-tests-collection.json` | Batch operations |
| `collaboration-tests-collection.json` | WebSocket collaboration |
| `unauthorized-tests-collection.json` | 401 authentication tests |
| `comprehensive-test-collection.json` | Full workflow tests |
| `advanced-error-scenarios-collection.json` | 409, 422 edge cases |

---

## Appendix B: Response Code Reference

| Code | Meaning | When Used |
|------|---------|-----------|
| 200 | OK | Successful GET, PUT, PATCH, bulk operations |
| 201 | Created | Successful POST (resource creation) |
| 204 | No Content | Successful DELETE |
| 400 | Bad Request | Validation errors, malformed requests |
| 401 | Unauthorized | Missing or invalid authentication |
| 403 | Forbidden | Authenticated but lacks permission |
| 404 | Not Found | Resource does not exist |
| 409 | Conflict | Resource conflict (e.g., active session, version) |
| 422 | Unprocessable Entity | Semantic errors in PATCH operations |
| 429 | Too Many Requests | Rate limit exceeded |
| 500 | Internal Server Error | Server-side failures |
