# OpenAPI Schema Refactoring Implementation Plan

## Overview
This document outlines all code changes required to implement the OpenAPI schema refactoring:
- Rename `url` → `uri` for properties under `/threat_models` paths only
- Rename `issue_url` → `issue_uri` for threat model and threat resources
- Rename `Source` schema → `Repository` schema
- Rename `/sources` paths → `/repositories` paths
- Rename `source_id` parameter → `repository_id` parameter

**SCOPE LIMITATION**: Only rename URL fields under `/threat_models/*` paths. Do NOT rename:
- `auth_url`, `token_url` (used in `/oauth2/providers`)
- `websocket_url` (used in CollaborationSession)
- `join_url` (used in collaboration conflict responses)

## Change Categories

### 1. Generated API Code (Auto-regenerated)

**File: [api/api.go](api/api.go)**
- **Action**: Run `make generate-api` to regenerate from updated OpenAPI spec
- **Changes**:
  - `type Source struct` → `type Repository struct`
  - Field: `Url string` → `Uri string` (in Source/Repository and Document)
  - Field: `IssueUrl *string` → `IssueUri *string` (in ThreatModel*, Threat*)
  - Handler interface methods: All `Source` → `Repository` in method names
  - Route registration: `/sources` → `/repositories`, `source_id` → `repository_id`
- **Preserved**: `WebsocketUrl`, `auth_url`, `token_url`, `join_url` (not under threat_models)

### 2. Database Schema & Migrations

**File: [auth/migrations/002_business_domain.up.sql](auth/migrations/002_business_domain.up.sql)**
- **Lines**: 10, 49, 74, 85, 194
- **Changes**:
  ```sql
  -- Threat Models table (line ~10)
  issue_url VARCHAR(1024) → issue_uri VARCHAR(1024)

  -- Threats table (line ~49)
  issue_url VARCHAR(1024) → issue_uri VARCHAR(1024)

  -- Sources table (line ~74)
  CREATE TABLE sources → CREATE TABLE repositories
  url VARCHAR(1024) NOT NULL → uri VARCHAR(1024) NOT NULL

  -- Documents table (line ~85)
  url VARCHAR(1024) NOT NULL → uri VARCHAR(1024) NOT NULL

  -- Constraint (line ~194)
  sources_url_not_empty → repositories_uri_not_empty
  ALTER TABLE sources → ALTER TABLE repositories
  ```

**File: [internal/dbschema/schema.go](internal/dbschema/schema.go)**
- **Line 110**: threat_models table column
  ```go
  {Name: "issue_url", ...} → {Name: "issue_uri", ...}
  ```
- **Line 324+**: sources table
  ```go
  Name: "sources" → Name: "repositories"
  {Name: "url", ...} → {Name: "uri", ...}
  ```
- Similar changes for threats table and documents table

### 3. Store Implementations

**File: [api/source_store.go](api/source_store.go)** → Rename to `api/repository_store.go`
- **Type renames**:
  - `SourceStore` interface → `RepositoryStore`
  - `ExtendedSource` struct → `ExtendedRepository`
  - `DatabaseSourceStore` struct → `DatabaseRepositoryStore`
- **Function renames**:
  - `NewDatabaseSourceStore()` → `NewDatabaseRepositoryStore()`
- **Field access**: All `.Url` → `.Uri`
- **SQL queries**:
  - Table: `sources` → `repositories`
  - Column: `url` → `uri`
  - Cache keys: `"source:"` → `"repository:"`

**File: [api/document_store.go](api/document_store.go)**
- **Field access**: `.Url` → `.Uri`
- **SQL column**: `url` → `uri`

**File: [api/threat_store.go](api/threat_store.go)**
- **Field access**: `.IssueUrl` → `.IssueUri`
- **SQL column**: `issue_url` → `issue_uri`

**File: [api/database_store.go](api/database_store.go)**
- **Field access**:
  - `.Url` → `.Uri` (for Document, Source/Repository)
  - `.IssueUrl` → `.IssueUri` (for ThreatModel*, Threat*)
- **SQL columns**: `url` → `uri`, `issue_url` → `issue_uri`

### 4. Handler Implementations

**File: [api/source_sub_resource_handlers.go](api/source_sub_resource_handlers.go)** → Rename to `api/repository_sub_resource_handlers.go`
- **Function names**: All containing `Source` → `Repository`
- **Type references**: `Source` → `Repository`
- **Field access**: `.Url` → `.Uri`

**File: [api/source_metadata_handlers.go](api/source_metadata_handlers.go)** → Rename to `api/repository_metadata_handlers.go`
- **Function names**: `Source` → `Repository`
- **Comments**: Update references

**File: [api/document_sub_resource_handlers.go](api/document_sub_resource_handlers.go)**
- **Field access**: `.Url` → `.Uri`

**File: [api/threat_model_handlers.go](api/threat_model_handlers.go)**
- **Field access**: `.IssueUrl` → `.IssueUri`

**File: [api/threat_sub_resource_handlers.go](api/threat_sub_resource_handlers.go)**
- **Field access**: `.IssueUrl` → `.IssueUri`

### 5. Server & Main

**File: [api/server.go](api/server.go)**
- **Store initialization**:
  ```go
  sourceStore := NewDatabaseSourceStore(...) → repositoryStore := NewDatabaseRepositoryStore(...)
  ```
- **ServerImpl fields**: `sourceStore` → `repositoryStore`
- **Handler implementations**: Update all Source → Repository method calls

**File: [cmd/server/main.go](cmd/server/main.go)**
- Update any `Source` type references if present

**File: [api/middleware.go](api/middleware.go)**
- **Path parameter**: Context key for `source_id` → `repository_id`

### 6. Internal Models & Validation

**File: [api/internal_models.go](api/internal_models.go)**
- **Field access**: `.Url` → `.Uri`, `.IssueUrl` → `.IssueUri`
- Update any internal model conversions

**File: [api/validation_config.go](api/validation_config.go)**
- Update field name references in validation rules if any

### 7. Cache Service

**File: [api/cache_service.go](api/cache_service.go)**
- **Cache key patterns**:
  ```go
  "source:{id}" → "repository:{id}"
  "tm:{id}:sources" → "tm:{id}:repositories"
  ```
- **Function names**: Any containing `Source` → `Repository`

### 8. Test Files (21 files)

**Integration Tests**:
- **[api/source_integration_test.go](api/source_integration_test.go)** → Rename to `api/repository_integration_test.go`
  - Type: `Source` → `Repository`
  - Field: `.Url` → `.Uri`
  - Paths: `/sources` → `/repositories`

**Handler Tests**:
- **[api/source_sub_resource_handlers_test.go](api/source_sub_resource_handlers_test.go)** → Rename to `api/repository_sub_resource_handlers_test.go`
  - All `Source` → `Repository`

**Sub-resource Tests**:
- **[api/sub_resource_integration_test.go](api/sub_resource_integration_test.go)**
  - Source → Repository references
  - Path updates

- **[api/sub_resource_test_fixtures.go](api/sub_resource_test_fixtures.go)**
  - Source → Repository
  - `.Url` → `.Uri`

**Document Tests**:
- **[api/document_integration_test.go](api/document_integration_test.go)**
  - `.Url` → `.Uri`

- **[api/document_sub_resource_handlers_test.go](api/document_sub_resource_handlers_test.go)**
  - `.Url` → `.Uri`

**Database & Cache Tests**:
- **[api/database_store_test.go](api/database_store_test.go)**
  - `.Url` → `.Uri`, `.IssueUrl` → `.IssueUri`

- **[api/cache_service_test.go](api/cache_service_test.go)**
  - Source → Repository
  - Cache key patterns

- **[api/cache_warming_test.go](api/cache_warming_test.go)**
  - Source → Repository

- **[api/cache_test_helpers.go](api/cache_test_helpers.go)**
  - Source references

**Test Helpers & Fixtures**:
- **[api/integration_test_helpers_test.go](api/integration_test_helpers_test.go)**
  - Source → Repository, `.Url` → `.Uri`

- **[api/test_fixtures.go](api/test_fixtures.go)**
  - Source → Repository, `.Url` → `.Uri`

**Other Integration Tests**:
- **[api/sub_entities_integration_test.go](api/sub_entities_integration_test.go)** - Source references
- **[api/batch_integration_test.go](api/batch_integration_test.go)** - Source → Repository
- **[api/simple_performance_test.go](api/simple_performance_test.go)** - Source → Repository
- **[api/metadata_handlers_test.go](api/metadata_handlers_test.go)** - Source metadata
- **[api/metadata_integration_test.go](api/metadata_integration_test.go)** - Source metadata
- **[api/api_specification_compliance_test.go](api/api_specification_compliance_test.go)** - `/sources` paths
- **[api/integration_collaboration_test.go](api/integration_collaboration_test.go)** - Check for url references
- **[api/websocket_test.go](api/websocket_test.go)** - Check for url field references (NOT websocket_url)

### 9. Documentation Files (5 files)

**Testing Documentation**:
- **[docs/developer/testing/endpoints-status-codes.md](docs/developer/testing/endpoints-status-codes.md)**
  - `/sources` → `/repositories`
  - `source_id` → `repository_id`
  - `url` → `uri`, `issue_url` → `issue_uri` (in examples)

- **[docs/developer/testing/comprehensive-test-plan.md](docs/developer/testing/comprehensive-test-plan.md)**
  - Source → Repository references
  - Field name updates

- **[docs/developer/testing/integration-testing.md](docs/developer/testing/integration-testing.md)**
  - Source → Repository
  - Path and field updates

**Database Documentation**:
- **[docs/operator/database/postgresql-schema.md](docs/operator/database/postgresql-schema.md)**
  - Table: `sources` → `repositories`
  - Columns: `url` → `uri`, `issue_url` → `issue_uri`

- **[docs/operator/database/redis-schema.md](docs/operator/database/redis-schema.md)**
  - Cache key patterns: `source:` → `repository:`

### 10. Scripts (3 files)

**Test Scripts**:
- **[scripts/test_source_metadata.sh](scripts/test_source_metadata.sh)**
  - `/sources` → `/repositories`
  - `source_id` → `repository_id`
  - Variable names: `SOURCE_ID` → `REPOSITORY_ID`

**Analysis Scripts**:
- **[scripts/analyze_endpoints.py](scripts/analyze_endpoints.py)**
  - `/sources` → `/repositories` in path analysis

- **[scripts/validate_openapi.py](scripts/validate_openapi.py)**
  - Update references to Source schema (now Repository)

### 11. Postman Collections (5+ files)

**Source Collections**:
- **[postman/source-crud-tests-collection.json](postman/source-crud-tests-collection.json)**
  - Rename file to `repository-crud-tests-collection.json`
  - All `/sources` → `/repositories`
  - `source_id` → `repository_id`
  - `"url"` → `"uri"` in request bodies

**Metadata Collections**:
- **[postman/complete-metadata-tests-collection.json](postman/complete-metadata-tests-collection.json)**
  - Source metadata endpoints → Repository metadata endpoints

**Bulk Operations**:
- **[postman/bulk-operations-tests-collection.json](postman/bulk-operations-tests-collection.json)**
  - Bulk source operations → Repository
  - Field names in JSON payloads

**Multi-user Auth**:
- **[postman/multi-user-auth.js](postman/multi-user-auth.js)**
  - Check for url/issue_url references

**Other Collections**:
- Review all collections for `url`/`issue_url` field references

### 12. SDK Examples

**Python SDK**:
- **[docs/sdk-examples/python-sdk/tmi_client/client.py](docs/sdk-examples/python-sdk/tmi_client/client.py)**
  - Method names: `get_sources()` → `get_repositories()`
  - Paths: `/sources` → `/repositories`
  - Field names: `url` → `uri`, `issue_url` → `issue_uri`

### 13. API Workflow Reference

**Workflow Documentation**:
- **[docs/reference/apis/api-workflows.json](docs/reference/apis/api-workflows.json)**
  - `/sources` → `/repositories` in workflow examples
  - Field name updates in sample payloads

---

## Implementation Order

### Phase 1: Database & Schema (Breaking Changes)
1. Create new migration file `003_rename_url_to_uri_and_sources_to_repositories.up.sql`
2. Update `internal/dbschema/schema.go`

### Phase 2: Regenerate API Code
3. Ensure OpenAPI spec is updated (already done)
4. Run `make generate-api` to regenerate `api/api.go`

### Phase 3: Rename Store Files
5. Rename `api/source_store.go` → `api/repository_store.go`
6. Rename `api/source_metadata_handlers.go` → `api/repository_metadata_handlers.go`
7. Rename `api/source_sub_resource_handlers.go` → `api/repository_sub_resource_handlers.go`
8. Update all Source → Repository type references in renamed files

### Phase 4: Update Store Implementations
9. Update `api/repository_store.go` (formerly source_store.go)
10. Update `api/document_store.go`
11. Update `api/threat_store.go`
12. Update `api/database_store.go`
13. Update `api/cache_service.go`

### Phase 5: Update Handlers
14. Update `api/repository_sub_resource_handlers.go`
15. Update `api/repository_metadata_handlers.go`
16. Update `api/document_sub_resource_handlers.go`
17. Update `api/threat_model_handlers.go`
18. Update `api/threat_sub_resource_handlers.go`

### Phase 6: Update Server & Infrastructure
19. Update `api/server.go`
20. Update `api/middleware.go`
21. Update `cmd/server/main.go`
22. Update `api/internal_models.go`
23. Update `api/validation_config.go`

### Phase 7: Update Tests
24. Rename test files (source_*_test.go → repository_*_test.go)
25. Update all test files with type and field changes
26. Run `make test-unit` and fix issues
27. Run `make test-integration` and fix issues

### Phase 8: Update Documentation & Scripts
28. Update documentation files
29. Update scripts
30. Update Postman collections
31. Update SDK examples

### Phase 9: Validation
32. Run `make validate-openapi` - ensure spec is valid
33. Run `make lint` - ensure code quality
34. Run `make build-server` - ensure compilation
35. Run `make test-unit` - ensure unit tests pass
36. Run `make test-integration` - ensure integration tests pass

---

## Summary Statistics

**Total Files Requiring Updates**: ~75 files

**Breakdown by Type**:
- Go source files: ~35 files
- Go test files: ~20 files
- Documentation: ~5 files
- Scripts: ~3 files
- Postman collections: ~5 files
- SDK examples: ~1 file
- Database migrations: ~2 files
- Workflow definitions: ~1 file

**Property Renames**:
- `url` → `uri`: 25 occurrences (Document, Repository schemas only)
- `issue_url` → `issue_uri`: 8 occurrences (ThreatModel*, Threat* schemas)
- **Preserved**: `auth_url`, `token_url`, `websocket_url`, `join_url`

**Type/Schema Renames**:
- `Source` → `Repository`: All Go types, structs, interfaces, functions
- Table: `sources` → `repositories`
- Paths: `/sources` → `/repositories`
- Parameter: `source_id` → `repository_id`

**Cache Key Patterns**:
- `"source:{id}"` → `"repository:{id}"`
- `"tm:{id}:sources"` → `"tm:{id}:repositories"`
