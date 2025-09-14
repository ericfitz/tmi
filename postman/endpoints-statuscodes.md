
## /
- GET: 200, 400, 500

## /.well-known/openid-configuration
- GET: 200, 400, 500

## /.well-known/oauth-authorization-server
- GET: 200, 400, 500

## /.well-known/jwks.json
- GET: 200, 400, 500

## /.well-known/oauth-protected-resource
- GET: 200, 400, 500

## /oauth2/introspect
- POST: 200, 400

## /oauth2/providers
- GET: 200, 500

## /oauth2/authorize
- GET: 302, 400, 500

## /oauth2/token
- POST: 200, 400, 500

## /oauth2/refresh
- POST: 200, 400, 401, 500

## /oauth2/userinfo
- GET: 200, 401, 500

## /oauth2/callback
- GET: 200, 302, 400, 401, 500

## /oauth2/revoke
- POST: 204, 401, 500

## /collaboration/sessions
- GET: 200, 401, 500

## /threat_models
- GET: 200, 401, 500

## /threat_models
- POST: 201, 400, 401, 500

## /threat_models/{threat_model_id}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}
- PATCH: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}
- PATCH: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata/{key}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/{threat_id}/metadata/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/bulk
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/batch/patch
- POST: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/threats/batch
- DELETE: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata/{key}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/{document_id}/metadata/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/documents/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata/{key}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/{source_id}/metadata/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/sources/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata/{key}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata/{key}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata/{key}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/metadata/bulk
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}
- PATCH: 200, 400, 401, 403, 404, 422, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate
- POST: 201, 400, 401, 403, 404, 409, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/collaborate
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata
- POST: 201, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
- GET: 200, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
- PUT: 200, 400, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/{key}
- DELETE: 204, 401, 403, 404, 500

## /threat_models/{threat_model_id}/diagrams/{diagram_id}/metadata/bulk
- POST: 201, 400, 401, 403, 404, 500
