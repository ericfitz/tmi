# TMI Endpoints, methods and http response codes

## Authentication & OAuth

### Root & Discovery

- GET / → 200, 400, 500
- GET /.well-known/openid-configuration → 200, 400, 500
- GET /.well-known/oauth-authorization-server → 200, 400, 500
- GET /.well-known/jwks.json → 200, 400, 500

### OAuth2 Operations

- GET /oauth2/providers → 200, 500
- GET /oauth2/authorize → 302, 400, 500
- POST /oauth2/token → 200, 400, 500
- POST /oauth2/refresh → 200, 400, 401, 500
- GET /oauth2/userinfo → 200, 401, 500
- GET /oauth2/callback → 200, 302, 400, 401, 500
- POST /oauth2/introspect → 200, 400
- POST /oauth2/revoke → 204, 401, 500

## Collaboration

- GET /collaboration/sessions → 200, 401, 500

## Threat Models

### Basic CRUD

- GET /threat_models → 200, 401, 500
- POST /threat_models → 201, 400, 401, 500
- GET /threat_models/{threat_model_id} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id} → 200, 400, 401, 403, 404, 500
- PATCH /threat_models/{threat_model_id} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id} → 204, 401, 403, 404, 500

### Metadata

- GET /threat_models/{threat_model_id}/metadata → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/metadata → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/metadata/{key} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/metadata/{key} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/metadata/{key} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/metadata/bulk → 201, 400, 401, 403, 404, 500

## Threats

### CRUD Operations

- GET /threat_models/{threat_model_id}/threats → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/threats → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/threats/{threat_id} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/threats/{threat_id} → 200, 400, 401, 403, 404, 500
- PATCH /threat_models/{threat_model_id}/threats/{threat_id} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/threats/{threat_id} → 204, 401, 403, 404, 500

### Bulk Operations

- POST /threat_models/{threat_model_id}/threats/bulk → 201, 400, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/threats/bulk → 200, 400, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/threats/batch/patch → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/threats/batch → 200, 400, 401, 403, 404, 500

### Threat Metadata

- GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk → 201, 400, 401, 403, 404, 500

## Documents

### CRUD Operations

- GET /threat_models/{threat_model_id}/documents → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/documents → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/documents/{document_id} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/documents/{document_id} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/documents/{document_id} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/documents/bulk → 201, 400, 401, 403, 404, 500

### Document Metadata

- GET /threat_models/{threat_model_id}/documents/{document_id}/metadata → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/documents/{document_id}/metadata → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk → 201, 400, 401, 403, 404, 500

## Sources

### CRUD Operations

- GET /threat_models/{threat_model_id}/sources → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/sources → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/sources/{source_id} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/sources/{source_id} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/sources/{source_id} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/sources/bulk → 201, 400, 401, 403, 404, 500

### Source Metadata

- GET /threat_models/{threat_model_id}/sources/{source_id}/metadata → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/sources/{source_id}/metadata → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/sources/{source_id}/metadata/bulk → 201, 400, 401, 403, 404, 500

## Diagrams

### CRUD Operations

- GET /threat_models/{threat_model_id}/diagrams → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/diagrams → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/diagrams/{diagram_id} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/diagrams/{diagram_id} → 200, 400, 401, 403, 404, 500
- PATCH /threat_models/{threat_model_id}/diagrams/{diagram_id} → 200, 400, 401, 403, 404, 422, 500
- DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id} → 204, 401, 403, 404, 500

### Collaboration

- GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate → 201, 400, 401, 403, 404, 409, 500
- PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate → 200, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate → 204, 401, 403, 404, 500

### Diagram Metadata

- GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata → 200, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata → 201, 400, 401, 403, 404, 500
- GET /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} → 200, 401, 403, 404, 500
- PUT /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} → 200, 400, 401, 403, 404, 500
- DELETE /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key} → 204, 401, 403, 404, 500
- POST /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk → 201, 400, 401, 403, 404, 500
