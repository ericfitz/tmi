# External Embedding APIs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add API endpoints for external automation to push pre-computed embeddings into TMI's dual-index vector store, gated by a new `embedding-automation` built-in group.

**Architecture:** OpenAPI-first. New `embedding-automation` group seeded at startup. Layered middleware on `/automation/*` (any automation group) and `/automation/embeddings/*` (embedding-automation only). Three endpoints: config sharing, embedding ingestion, bulk delete. Handlers are methods on the existing `Server` struct. Embedding store gains `DeleteByThreatModelAndIndexType` and all delete methods return row counts.

**Tech Stack:** Go, Gin, GORM, oapi-codegen, OpenAPI 3.0.3, testify, SQLite (unit tests)

**Spec:** `docs/superpowers/specs/2026-04-10-external-embedding-apis-design.md`

---

### Task 1: Add `embedding-automation` Group Constants and Seed

**Files:**
- Modify: `api/validation/validators.go`
- Modify: `api/auth_utils.go`
- Modify: `api/group_membership.go`
- Modify: `api/seed/seed.go`

- [ ] **Step 1: Add UUID constant**

In `api/validation/validators.go`, add after `TMIAutomationGroupUUID`:

```go
	// EmbeddingAutomationGroupUUID is the well-known UUID for the Embedding Automation built-in group.
	EmbeddingAutomationGroupUUID = "00000000-0000-0000-0000-000000000005"
```

- [ ] **Step 2: Add group name constant**

In `api/auth_utils.go`, find the block with `TMIAutomationGroup = "tmi-automation"` and add after it:

```go
	// EmbeddingAutomationGroup is the group_name for the built-in Embedding Automation group.
	// Members can push pre-computed embeddings and read embedding provider config (including API keys).
	EmbeddingAutomationGroup = "embedding-automation"
```

- [ ] **Step 3: Add BuiltInGroup variable**

In `api/group_membership.go`, add after the `GroupTMIAutomation` variable:

```go
	// GroupEmbeddingAutomation is the built-in Embedding Automation group.
	GroupEmbeddingAutomation = BuiltInGroup{Name: EmbeddingAutomationGroup, UUID: uuid.MustParse(EmbeddingAutomationGroupUUID)}
```

- [ ] **Step 4: Add seed function**

In `api/seed/seed.go`, add the seed function (following the exact pattern of `seedTMIAutomationGroup`):

```go
// seedEmbeddingAutomationGroup ensures the "embedding-automation" built-in group exists.
// Members can push pre-computed embeddings and read embedding provider configuration.
func seedEmbeddingAutomationGroup(db *gorm.DB) error {
	log := slogging.Get()

	name := "Embedding Automation"
	group := models.Group{
		InternalUUID: validation.EmbeddingAutomationGroupUUID,
		Provider:     "*",
		GroupName:    "embedding-automation",
		Name:         &name,
		UsageCount:   0,
	}

	result := db.Where(&models.Group{
		Provider:  "*",
		GroupName: "embedding-automation",
	}).FirstOrCreate(&group)

	if result.Error != nil {
		return result.Error
	}

	if result.RowsAffected > 0 {
		log.Info("Created 'embedding-automation' group")
	} else {
		log.Debug("'embedding-automation' group already exists")
	}

	return nil
}
```

- [ ] **Step 5: Call seed function from SeedDatabase**

In `api/seed/seed.go`, in the `SeedDatabase` function, add before the `cleanupOrphanedSurveyResponses` call:

```go
	if err := seedEmbeddingAutomationGroup(db); err != nil {
		log.Error("Failed to seed 'embedding-automation' group: %v", err)
		return err
	}
```

- [ ] **Step 6: Verify build and tests**

Run: `make build-server`
Expected: BUILD SUCCESS

Run: `make test-unit`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/validation/validators.go api/auth_utils.go api/group_membership.go api/seed/seed.go
git commit -m "feat(timmy): add embedding-automation built-in group"
```

---

### Task 2: Update Embedding Store — Delete Return Values and New Method

**Files:**
- Modify: `api/timmy_embedding_store.go`
- Modify: `api/timmy_embedding_store_gorm.go`
- Modify: `api/timmy_embedding_store_test.go`
- Modify: `api/timmy_session_manager.go` (update caller)

- [ ] **Step 1: Write tests for new method and updated signatures**

Add to `api/timmy_embedding_store_test.go`:

```go
func TestTimmyEmbeddingStore_DeleteByThreatModelAndIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-idx-del"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-t1",
			EmbeddingModel: "text-model",
			EmbeddingDim:   1536,
			ChunkText:      "text chunk",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "repository",
			EntityID:       "repo-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeCode,
			ContentHash:    "hash-c1",
			EmbeddingModel: "code-model",
			EmbeddingDim:   768,
			ChunkText:      "code chunk",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "repository",
			EntityID:       "repo-002",
			ChunkIndex:     0,
			IndexType:      IndexTypeCode,
			ContentHash:    "hash-c2",
			EmbeddingModel: "code-model",
			EmbeddingDim:   768,
			ChunkText:      "code chunk 2",
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only code embeddings
	count, err := store.DeleteByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Text embedding should remain
	textResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, textResults, 1)

	// Code embeddings should be gone
	codeResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Empty(t, codeResults)
}

func TestTimmyEmbeddingStore_DeleteByEntity_ReturnsCount(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-del-count"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-a",
			EmbeddingModel: "model",
			EmbeddingDim:   3,
			ChunkText:      "chunk 0",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     1,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-b",
			EmbeddingModel: "model",
			EmbeddingDim:   3,
			ChunkText:      "chunk 1",
		},
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	count, err := store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestTimmyEmbeddingStore_DeleteByThreatModel_ReturnsCount(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-del-tm-count"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-x",
			EmbeddingModel: "model",
			EmbeddingDim:   3,
			ChunkText:      "chunk",
		},
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	count, err := store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestTimmyEmbeddingStore_DeleteByThreatModelAndIndexType`
Expected: FAIL — method not defined

- [ ] **Step 3: Update the interface**

In `api/timmy_embedding_store.go`, replace the interface:

```go
// TimmyEmbeddingStore defines operations for persisting vector embeddings
type TimmyEmbeddingStore interface {
	ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
	CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
	DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error)
	DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error)
	DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error)
}
```

- [ ] **Step 4: Update GORM implementations**

In `api/timmy_embedding_store_gorm.go`:

Update `DeleteByEntity` to return `(int64, error)`:

```go
func (s *GormTimmyEmbeddingStore) DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting embeddings for entity %s/%s in threat model %s", entityType, entityID, threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{
			"threat_model_id": threatModelID,
			"entity_type":     entityType,
			"entity_id":       entityID,
		}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for entity %s/%s: %v", entityType, entityID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by entity: %w", result.Error)
	}

	logger.Debug("Deleted %d embeddings for entity %s/%s", result.RowsAffected, entityType, entityID)
	return result.RowsAffected, nil
}
```

Update `DeleteByThreatModel` to return `(int64, error)`:

```go
func (s *GormTimmyEmbeddingStore) DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting all embeddings for threat model %s", threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for threat model %s: %v", threatModelID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by threat model: %w", result.Error)
	}

	logger.Debug("Deleted %d embeddings for threat model %s", result.RowsAffected, threatModelID)
	return result.RowsAffected, nil
}
```

Add the new method:

```go
// DeleteByThreatModelAndIndexType deletes all embeddings for a threat model and index type
func (s *GormTimmyEmbeddingStore) DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting %s embeddings for threat model %s", indexType, threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete %s embeddings for threat model %s: %v", indexType, threatModelID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by threat model and index type: %w", result.Error)
	}

	logger.Debug("Deleted %d %s embeddings for threat model %s", result.RowsAffected, indexType, threatModelID)
	return result.RowsAffected, nil
}
```

- [ ] **Step 5: Update callers**

In `api/timmy_session_manager.go`, the caller of `DeleteByEntity` (in `prepareVectorIndex`) currently does:

```go
if err := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, src.EntityType, src.EntityID); err != nil {
```

Change to:

```go
if _, err := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, src.EntityType, src.EntityID); err != nil {
```

Search for any other callers of `DeleteByThreatModel` in the codebase and update them similarly to ignore the count with `_`.

- [ ] **Step 6: Update existing delete tests**

The existing `TestTimmyEmbeddingStore_DeleteByEntity` and `TestTimmyEmbeddingStore_DeleteByThreatModel` tests call the old signature. Update them to handle the `(int64, error)` return:

In `TestTimmyEmbeddingStore_DeleteByEntity`:
```go
// Change:
err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
// To:
_, err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
```

In `TestTimmyEmbeddingStore_DeleteByThreatModel`:
```go
// Change:
err = store.DeleteByThreatModel(ctx, tmID)
// To:
_, err = store.DeleteByThreatModel(ctx, tmID)
```

- [ ] **Step 7: Run tests**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: PASS

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 8: Commit**

```bash
git add api/timmy_embedding_store.go api/timmy_embedding_store_gorm.go api/timmy_embedding_store_test.go api/timmy_session_manager.go
git commit -m "feat(timmy): add DeleteByThreatModelAndIndexType and return counts from all deletes"
```

---

### Task 3: Automation Middleware

**Files:**
- Create: `api/automation_middleware.go`
- Create: `api/automation_middleware_test.go`

- [ ] **Step 1: Write tests**

Create `api/automation_middleware_test.go`:

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAutomationMiddleware_AllowsEmbeddingAutomation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tmiIsEmbeddingAutomation", true)
		c.Next()
	})
	r.Use(AutomationMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAutomationMiddleware_AllowsTMIAutomation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tmiIsTMIAutomation", true)
		c.Next()
	})
	r.Use(AutomationMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAutomationMiddleware_RejectsNonMembers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AutomationMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestEmbeddingAutomationMiddleware_AllowsEmbeddingAutomation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tmiIsEmbeddingAutomation", true)
		c.Next()
	})
	r.Use(EmbeddingAutomationMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestEmbeddingAutomationMiddleware_RejectsTMIAutomationOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("tmiIsTMIAutomation", true)
		c.Next()
	})
	r.Use(EmbeddingAutomationMiddleware())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/test", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}
```

Note: The middleware tests use Gin context flags (`tmiIsEmbeddingAutomation`, `tmiIsTMIAutomation`) that will be set by the JWT middleware at authentication time. The actual group membership resolution happens in the JWT middleware before the automation middleware runs. The middleware itself checks these pre-resolved flags. This matches how `tmiIsAdministrator` works in the existing codebase. If the existing JWT middleware doesn't set these flags yet, the middleware implementation should call `IsGroupMemberFromContext` directly instead (and the tests should mock the group membership store). Inspect how the existing admin middleware checks work to determine the correct pattern.

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestAutomationMiddleware`
Expected: FAIL — `AutomationMiddleware` not defined

- [ ] **Step 3: Implement middleware**

Create `api/automation_middleware.go`:

```go
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// AutomationMiddleware gates /automation/* routes to members of any automation group
// (embedding-automation or tmi-automation).
func AutomationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		isEmbeddingAutomation, _ := IsGroupMemberFromContext(c, GroupEmbeddingAutomation)
		isTMIAutomation, _ := IsGroupMemberFromContext(c, GroupTMIAutomation)

		if !isEmbeddingAutomation && !isTMIAutomation {
			logger.Debug("AutomationMiddleware: access denied — not a member of any automation group")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "forbidden",
				"message": "automation group membership required",
			})
			return
		}

		c.Next()
	}
}

// EmbeddingAutomationMiddleware gates /automation/embeddings/* routes to members
// of the embedding-automation group only.
func EmbeddingAutomationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		logger := slogging.Get().WithContext(c)

		isEmbeddingAutomation, _ := IsGroupMemberFromContext(c, GroupEmbeddingAutomation)

		if !isEmbeddingAutomation {
			logger.Debug("EmbeddingAutomationMiddleware: access denied — not a member of embedding-automation group")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    "forbidden",
				"message": "embedding-automation group membership required",
			})
			return
		}

		c.Next()
	}
}
```

Note: The middleware calls `IsGroupMemberFromContext` which requires the group member store to be initialized. In tests, this will need to be mocked or the test setup must initialize the required globals. The implementer should study how existing middleware tests (e.g., `timmy_middleware.go` tests) handle this and follow the same pattern. If `IsGroupMemberFromContext` doesn't work in unit tests due to uninitialized stores, use Gin context flags as a fallback pattern (check `c.GetBool("tmiIsEmbeddingAutomation")` first, fall back to `IsGroupMemberFromContext` if not set).

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestAutomationMiddleware`
Run: `make test-unit name=TestEmbeddingAutomationMiddleware`
Expected: PASS

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add api/automation_middleware.go api/automation_middleware_test.go
git commit -m "feat(timmy): add automation and embedding-automation middleware"
```

---

### Task 4: OpenAPI Schema — Add Endpoints and Types

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This task adds the three `/automation/embeddings/{threat_model_id}` endpoints and their request/response schemas to the OpenAPI specification. The spec is a large JSON file — use `jq` for surgical updates.

- [ ] **Step 1: Add schema definitions**

Add the following schemas to `components/schemas` in `api-schema/tmi-openapi.json`:

**`EmbeddingProviderConfig`** — config for a single embedding provider:
```json
{
  "type": "object",
  "required": ["provider", "model"],
  "properties": {
    "provider": { "type": "string", "description": "Embedding provider name", "example": "openai" },
    "model": { "type": "string", "description": "Embedding model name", "example": "text-embedding-3-small" },
    "api_key": { "type": "string", "description": "Embedding provider API key" },
    "base_url": { "type": "string", "description": "Custom base URL for self-hosted endpoints", "example": "" }
  }
}
```

**`EmbeddingConfig`** — response for GET config:
```json
{
  "type": "object",
  "required": ["text_embedding"],
  "properties": {
    "text_embedding": { "$ref": "#/components/schemas/EmbeddingProviderConfig" },
    "code_embedding": {
      "allOf": [{ "$ref": "#/components/schemas/EmbeddingProviderConfig" }],
      "nullable": true,
      "description": "Code embedding config. Null if not configured."
    }
  }
}
```

**`EmbeddingIngestionItem`** — a single embedding in the batch:
```json
{
  "type": "object",
  "required": ["entity_type", "entity_id", "chunk_index", "chunk_text", "content_hash", "embedding_model", "embedding_dim", "vector"],
  "properties": {
    "entity_type": { "type": "string", "description": "Entity type (e.g., repository, asset)" },
    "entity_id": { "type": "string", "format": "uuid", "description": "Entity UUID" },
    "chunk_index": { "type": "integer", "minimum": 0, "description": "Sequential chunk number" },
    "chunk_text": { "type": "string", "description": "Original text of the chunk" },
    "content_hash": { "type": "string", "description": "SHA256 hash of the original content" },
    "embedding_model": { "type": "string", "description": "Model used to generate the embedding" },
    "embedding_dim": { "type": "integer", "minimum": 1, "description": "Embedding vector dimension" },
    "vector": {
      "type": "array",
      "items": { "type": "number", "format": "float" },
      "description": "Embedding vector"
    }
  }
}
```

**`EmbeddingIngestionRequest`** — POST request body:
```json
{
  "type": "object",
  "required": ["index_type", "embeddings"],
  "properties": {
    "index_type": { "type": "string", "enum": ["text", "code"], "description": "Target index type" },
    "embeddings": {
      "type": "array",
      "items": { "$ref": "#/components/schemas/EmbeddingIngestionItem" },
      "minItems": 1,
      "description": "Batch of pre-computed embeddings"
    }
  }
}
```

**`EmbeddingIngestionResponse`** — POST response:
```json
{
  "type": "object",
  "required": ["ingested"],
  "properties": {
    "ingested": { "type": "integer", "description": "Number of embeddings ingested" }
  }
}
```

**`EmbeddingDeleteResponse`** — DELETE response:
```json
{
  "type": "object",
  "required": ["deleted"],
  "properties": {
    "deleted": { "type": "integer", "description": "Number of embeddings deleted" }
  }
}
```

- [ ] **Step 2: Add path definitions**

Add the following paths to the `paths` section:

**`/automation/embeddings/{threat_model_id}/config`** with GET operation:
- operationId: `getEmbeddingConfig`
- tags: `["Embedding Automation"]`
- summary: "Get embedding provider configuration"
- description: "Returns embedding model configuration including API keys for automation tools."
- parameters: `threat_model_id` path param (required, string, format uuid)
- security: `[{"bearerAuth": []}]`
- responses: 200 (EmbeddingConfig), 401 (Unauthorized ref), 403 (Forbidden ref), 404 (NotFound ref)
- Mark with `x-public-endpoint: false`

**`/automation/embeddings/{threat_model_id}`** with POST operation:
- operationId: `ingestEmbeddings`
- tags: `["Embedding Automation"]`
- summary: "Ingest pre-computed embeddings"
- description: "Accepts a batch of pre-computed embedding vectors and stores them in the specified index."
- parameters: `threat_model_id` path param
- requestBody: required, EmbeddingIngestionRequest
- security: `[{"bearerAuth": []}]`
- responses: 201 (EmbeddingIngestionResponse), 400 (BadRequest ref), 401 (Unauthorized ref), 403 (Forbidden ref), 404 (NotFound ref), 422 (UnprocessableEntity ref)

**`/automation/embeddings/{threat_model_id}`** with DELETE operation:
- operationId: `deleteEmbeddings`
- tags: `["Embedding Automation"]`
- summary: "Delete embeddings"
- description: "Bulk delete embeddings with query parameter filters."
- parameters: `threat_model_id` path param, plus optional query params `entity_type` (string), `entity_id` (string, format uuid), `index_type` (string, enum ["text", "code"])
- security: `[{"bearerAuth": []}]`
- responses: 200 (EmbeddingDeleteResponse), 400 (BadRequest ref), 401 (Unauthorized ref), 403 (Forbidden ref), 404 (NotFound ref)

- [ ] **Step 3: Validate the OpenAPI spec**

Run: `make validate-openapi`
Expected: PASS (or only pre-existing warnings)

- [ ] **Step 4: Generate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerated with new `ServerInterface` methods

- [ ] **Step 5: Verify build (will fail — new ServerInterface methods not yet implemented)**

Run: `make build-server`
Expected: FAIL — Server does not implement new methods. This is expected and will be fixed in Task 5.

- [ ] **Step 6: Commit OpenAPI spec and generated code**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(timmy): add embedding automation OpenAPI endpoints and schemas"
```

---

### Task 5: Implement Handlers

**Files:**
- Create: `api/timmy_embedding_automation_handlers.go`
- Modify: `api/server.go` (add Server method stubs that delegate to handler functions)

- [ ] **Step 1: Create handler file**

Create `api/timmy_embedding_automation_handlers.go` with the three handler methods. These are methods on `*Server` (matching the generated `ServerInterface`):

```go
package api

import (
	"net/http"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
)

// GetEmbeddingConfig returns embedding provider configuration including API keys.
// Endpoint: GET /automation/embeddings/{threat_model_id}/config
func (s *Server) handleGetEmbeddingConfig(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	threatModelID := c.Param("threat_model_id")

	// Verify threat model exists
	_, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	cfg := s.timmySessionManager.config

	textConfig := gin.H{
		"provider": cfg.TextEmbeddingProvider,
		"model":    cfg.TextEmbeddingModel,
		"api_key":  cfg.TextEmbeddingAPIKey,
		"base_url": cfg.TextEmbeddingBaseURL,
	}

	response := gin.H{
		"text_embedding": textConfig,
	}

	if cfg.IsCodeIndexConfigured() {
		response["code_embedding"] = gin.H{
			"provider": cfg.CodeEmbeddingProvider,
			"model":    cfg.CodeEmbeddingModel,
			"api_key":  cfg.CodeEmbeddingAPIKey,
			"base_url": cfg.CodeEmbeddingBaseURL,
		}
	} else {
		response["code_embedding"] = nil
	}

	logger.Debug("Returned embedding config for threat model %s", threatModelID)
	c.JSON(http.StatusOK, response)
}

// IngestEmbeddings accepts a batch of pre-computed embedding vectors.
// Endpoint: POST /automation/embeddings/{threat_model_id}
func (s *Server) handleIngestEmbeddings(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	threatModelID := c.Param("threat_model_id")

	// Verify threat model exists
	_, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	// Bind request
	var req struct {
		IndexType  string `json:"index_type" binding:"required"`
		Embeddings []struct {
			EntityType     string    `json:"entity_type" binding:"required"`
			EntityID       string    `json:"entity_id" binding:"required"`
			ChunkIndex     int       `json:"chunk_index"`
			ChunkText      string    `json:"chunk_text" binding:"required"`
			ContentHash    string    `json:"content_hash" binding:"required"`
			EmbeddingModel string    `json:"embedding_model" binding:"required"`
			EmbeddingDim   int       `json:"embedding_dim" binding:"required"`
			Vector         []float32 `json:"vector" binding:"required"`
		} `json:"embeddings" binding:"required,min=1"`
	}

	if bindErr := c.ShouldBindJSON(&req); bindErr != nil {
		HandleRequestError(c, InvalidInputError("invalid request body: "+bindErr.Error()))
		return
	}

	// Validate index_type
	if req.IndexType != IndexTypeText && req.IndexType != IndexTypeCode {
		HandleRequestError(c, InvalidInputError("index_type must be 'text' or 'code'"))
		return
	}

	// Validate entity type / index type consistency and dimension consistency
	var expectedDim int
	for i, emb := range req.Embeddings {
		expectedIndexType := EntityTypeToIndexType(emb.EntityType)
		if expectedIndexType != req.IndexType {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "entity_index_type_mismatch",
				Message: "entity type '" + emb.EntityType + "' is not compatible with index type '" + req.IndexType + "'",
			})
			return
		}

		if i == 0 {
			expectedDim = emb.EmbeddingDim
		} else if emb.EmbeddingDim != expectedDim {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "dimension_mismatch",
				Message: "all embeddings must have the same dimension",
			})
			return
		}

		if len(emb.Vector) != emb.EmbeddingDim {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "vector_dimension_mismatch",
				Message: "vector length does not match embedding_dim",
			})
			return
		}
	}

	// Convert to model objects
	embeddingRecords := make([]models.TimmyEmbedding, 0, len(req.Embeddings))
	for _, emb := range req.Embeddings {
		embeddingRecords = append(embeddingRecords, models.TimmyEmbedding{
			ThreatModelID:  threatModelID,
			EntityType:     emb.EntityType,
			EntityID:       emb.EntityID,
			ChunkIndex:     emb.ChunkIndex,
			IndexType:      req.IndexType,
			ContentHash:    emb.ContentHash,
			EmbeddingModel: emb.EmbeddingModel,
			EmbeddingDim:   emb.EmbeddingDim,
			VectorData:     float32ToBytes(emb.Vector),
			ChunkText:      models.DBText(emb.ChunkText),
		})
	}

	if storeErr := GlobalTimmyEmbeddingStore.CreateBatch(c.Request.Context(), embeddingRecords); storeErr != nil {
		logger.Error("Failed to store embeddings: %v", storeErr)
		HandleRequestError(c, InternalError("failed to store embeddings"))
		return
	}

	// Invalidate in-memory index
	if s.vectorManager != nil {
		s.vectorManager.InvalidateIndex(threatModelID, req.IndexType)
	}

	logger.Info("Ingested %d %s embeddings for threat model %s", len(embeddingRecords), req.IndexType, threatModelID)
	c.JSON(http.StatusCreated, gin.H{"ingested": len(embeddingRecords)})
}

// DeleteEmbeddings bulk deletes embeddings with query parameter filters.
// Endpoint: DELETE /automation/embeddings/{threat_model_id}
func (s *Server) handleDeleteEmbeddings(c *gin.Context) {
	logger := slogging.Get().WithContext(c)

	threatModelID := c.Param("threat_model_id")

	// Verify threat model exists
	_, err := ThreatModelStore.Get(threatModelID)
	if err != nil {
		HandleRequestError(c, NotFoundError("Threat model not found"))
		return
	}

	entityType := c.Query("entity_type")
	entityID := c.Query("entity_id")
	indexType := c.Query("index_type")

	// Require at least one filter
	if entityType == "" && entityID == "" && indexType == "" {
		HandleRequestError(c, InvalidInputError("at least one filter parameter is required (entity_type, entity_id, index_type)"))
		return
	}

	// entity_id requires entity_type
	if entityID != "" && entityType == "" {
		HandleRequestError(c, InvalidInputError("entity_id requires entity_type"))
		return
	}

	// Validate index_type if provided
	if indexType != "" && indexType != IndexTypeText && indexType != IndexTypeCode {
		HandleRequestError(c, InvalidInputError("index_type must be 'text' or 'code'"))
		return
	}

	ctx := c.Request.Context()
	var totalDeleted int64

	if entityType != "" && entityID != "" {
		// Delete by specific entity
		count, delErr := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, entityType, entityID)
		if delErr != nil {
			logger.Error("Failed to delete embeddings by entity: %v", delErr)
			HandleRequestError(c, InternalError("failed to delete embeddings"))
			return
		}
		totalDeleted = count

		// Invalidate the index type for this entity
		affectedIndexType := EntityTypeToIndexType(entityType)
		if s.vectorManager != nil {
			s.vectorManager.InvalidateIndex(threatModelID, affectedIndexType)
		}
	} else if indexType != "" {
		// Delete by index type
		count, delErr := GlobalTimmyEmbeddingStore.DeleteByThreatModelAndIndexType(ctx, threatModelID, indexType)
		if delErr != nil {
			logger.Error("Failed to delete embeddings by index type: %v", delErr)
			HandleRequestError(c, InternalError("failed to delete embeddings"))
			return
		}
		totalDeleted = count

		if s.vectorManager != nil {
			s.vectorManager.InvalidateIndex(threatModelID, indexType)
		}
	} else if entityType != "" {
		// Delete by entity type (all entities of this type)
		// Use DeleteByThreatModelAndIndexType won't work here — need to filter by entity_type.
		// The store doesn't have a DeleteByEntityType method. Use the existing
		// DeleteByThreatModelAndIndexType with the mapped index type as an approximation,
		// but this is inexact. Better: add a targeted query.
		// For now, the simplest approach: list embeddings of this entity type and delete them.
		// Actually, since entity_type maps 1:1 to index_type for the supported types,
		// and entity_type is more specific, we should add a store method or use raw deletion.
		// The cleanest solution: entity_type filtering via the embedding store.
		// Since we can't add a new store method in this step (scope), we'll document this
		// as a limitation and return 400 for entity_type-only without entity_id.
		HandleRequestError(c, InvalidInputError("entity_type filter requires entity_id; use index_type filter to delete all embeddings of a type"))
		return
	}

	logger.Info("Deleted %d embeddings for threat model %s", totalDeleted, threatModelID)
	c.JSON(http.StatusOK, gin.H{"deleted": totalDeleted})
}
```

- [ ] **Step 2: Add ServerInterface method stubs**

The generated `ServerInterface` will have methods matching the operationIds. Add methods to `Server` that delegate to the handler methods. The exact signatures depend on what `make generate-api` produces. Typical pattern:

```go
// In a new file api/server_embedding_automation.go or inline:

func (s *Server) GetEmbeddingConfig(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.handleGetEmbeddingConfig(c)
}

func (s *Server) IngestEmbeddings(c *gin.Context, threatModelId openapi_types.UUID) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	s.handleIngestEmbeddings(c)
}

func (s *Server) DeleteEmbeddings(c *gin.Context, threatModelId openapi_types.UUID, params DeleteEmbeddingsParams) {
	c.Params = append(c.Params, gin.Param{Key: "threat_model_id", Value: threatModelId.String()})
	// Set query params from generated params struct if not already in URL
	if params.EntityType != nil {
		c.Request.URL.RawQuery = c.Request.URL.RawQuery // params already in query string
	}
	s.handleDeleteEmbeddings(c)
}
```

The exact parameter types depend on the generated code. The implementer must check `api/api.go` after generation and match the `ServerInterface` signatures exactly.

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: BUILD SUCCESS

Run: `make lint`
Expected: 0 new issues

- [ ] **Step 4: Commit**

```bash
git add api/timmy_embedding_automation_handlers.go api/server_embedding_automation.go
git commit -m "feat(timmy): implement embedding automation handlers"
```

---

### Task 6: Route Registration — Apply Middleware

**Files:**
- Modify: `cmd/server/main.go` or `api/server.go` (wherever middleware is applied to routes)

- [ ] **Step 1: Apply automation middleware to route groups**

Find where `TimmyEnabledMiddleware` is applied in the router setup. After the OpenAPI handler registration, add middleware for the automation routes:

```go
// Automation middleware — layered access control
automationRoutes := r.Group("/automation")
automationRoutes.Use(AutomationMiddleware())
{
	embeddingRoutes := automationRoutes.Group("/embeddings")
	embeddingRoutes.Use(EmbeddingAutomationMiddleware())
}
```

Note: Since the routes are registered by the OpenAPI router, the middleware groups need to match the path patterns. The implementer should verify that applying middleware via `r.Group("/automation")` works correctly with OpenAPI-registered routes — the middleware must intercept the request before it reaches the OpenAPI handler. If the OpenAPI router registers routes directly on the root router, middleware groups applied before `RegisterHandlersWithOptions` will work. If not, the middleware may need to be applied as global middleware with path checks (similar to `TimmyEnabledMiddleware`).

- [ ] **Step 2: Verify middleware is hit**

Run: `make build-server`
Expected: BUILD SUCCESS

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat(timmy): register automation middleware on /automation routes"
```

---

### Task 7: Handler Tests

**Files:**
- Create: `api/timmy_embedding_automation_handlers_test.go`

- [ ] **Step 1: Write handler tests**

Create `api/timmy_embedding_automation_handlers_test.go` with tests for:

1. **GetEmbeddingConfig**: returns text config; returns text + code config when configured; 404 for missing threat model
2. **IngestEmbeddings**: valid batch returns 201 with count; rejects invalid index_type (400); rejects entity/index type mismatch (422); rejects inconsistent dimensions (422); rejects vector/dim mismatch (422); 404 for missing threat model
3. **DeleteEmbeddings**: deletes by entity_type + entity_id returns count; deletes by index_type returns count; 400 when no filters; 400 for entity_id without entity_type; 404 for missing threat model

The tests should set up:
- A test Gin router with the handlers
- A mock or SQLite-backed ThreatModelStore
- A SQLite-backed TimmyEmbeddingStore
- A VectorIndexManager (for invalidation verification)

Follow the test patterns from `api/timmy_handlers_test.go` and `api/timmy_session_manager_test.go` for store setup.

- [ ] **Step 2: Run tests**

Run: `make test-unit name=TestEmbeddingAutomation`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add api/timmy_embedding_automation_handlers_test.go
git commit -m "test(timmy): add embedding automation handler tests"
```

---

### Task 8: Full Test Suite, Lint, and Push

**Files:**
- All modified files

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Build server**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 4: Fix any failures and commit fixes**

- [ ] **Step 5: Review commit history**

Run: `git log --oneline HEAD~10..HEAD`

- [ ] **Step 6: Push**

```bash
git pull --rebase
git push
git status
```

Expected: "up to date with origin"
