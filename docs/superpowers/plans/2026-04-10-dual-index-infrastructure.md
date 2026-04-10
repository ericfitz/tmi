# Dual-Index Infrastructure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace Timmy's single-embedding-model architecture with dual vector indexes (text + code) using separate embedding models per index type.

**Architecture:** Bottom-up refactor — config first, then data layer, vector manager, LLM service, and session manager last. Each layer is stable before its consumers change. TDD throughout.

**Tech Stack:** Go, GORM (PostgreSQL + Oracle ADB), LangChainGo, testify, SQLite (unit tests)

**Spec:** `docs/superpowers/specs/2026-04-10-dual-index-infrastructure-design.md`

---

### Task 1: Index Type Constants

**Files:**
- Create: `api/timmy_index_types.go`
- Create: `api/timmy_index_types_test.go`

- [ ] **Step 1: Write the tests**

Create `api/timmy_index_types_test.go`:

```go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEntityTypeToIndexType(t *testing.T) {
	tests := []struct {
		entityType string
		expected   string
	}{
		{"asset", IndexTypeText},
		{"threat", IndexTypeText},
		{"diagram", IndexTypeText},
		{"document", IndexTypeText},
		{"note", IndexTypeText},
		{"repository", IndexTypeCode},
	}

	for _, tt := range tests {
		t.Run(tt.entityType, func(t *testing.T) {
			assert.Equal(t, tt.expected, EntityTypeToIndexType(tt.entityType))
		})
	}
}

func TestIndexTypeConstants(t *testing.T) {
	assert.Equal(t, "text", IndexTypeText)
	assert.Equal(t, "code", IndexTypeCode)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestEntityTypeToIndexType`
Expected: FAIL — `IndexTypeText`, `IndexTypeCode`, `EntityTypeToIndexType` not defined

- [ ] **Step 3: Write the implementation**

Create `api/timmy_index_types.go`:

```go
package api

const (
	// IndexTypeText is the index type for text content (assets, threats, diagrams, documents, notes)
	IndexTypeText = "text"

	// IndexTypeCode is the index type for code content (repositories)
	IndexTypeCode = "code"
)

// EntityTypeToIndexType maps an entity type to its vector index type.
// Repositories go to the code index; everything else goes to the text index.
func EntityTypeToIndexType(entityType string) string {
	if entityType == "repository" {
		return IndexTypeCode
	}
	return IndexTypeText
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestEntityTypeToIndexType`
Expected: PASS

Run: `make test-unit name=TestIndexTypeConstants`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_index_types.go api/timmy_index_types_test.go
git commit -m "feat(timmy): add index type constants and entity-to-index mapping"
```

---

### Task 2: Configuration — Replace Single Embedding with Dual Text/Code

**Files:**
- Modify: `internal/config/timmy.go`
- Modify: `internal/config/timmy_test.go`

- [ ] **Step 1: Write the tests**

Replace the contents of `internal/config/timmy_test.go`:

```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTimmyConfig_IsConfigured(t *testing.T) {
	cfg := TimmyConfig{
		LLMProvider:           "openai",
		LLMModel:              "gpt-4",
		TextEmbeddingProvider: "openai",
		TextEmbeddingModel:    "text-embedding-3-small",
	}
	assert.True(t, cfg.IsConfigured(), "should be configured with LLM + text embedding")

	empty := TimmyConfig{}
	assert.False(t, empty.IsConfigured(), "should not be configured with empty fields")

	noTextEmbed := TimmyConfig{
		LLMProvider: "openai",
		LLMModel:    "gpt-4",
	}
	assert.False(t, noTextEmbed.IsConfigured(), "should not be configured without text embedding")
}

func TestTimmyConfig_IsCodeIndexConfigured(t *testing.T) {
	cfg := TimmyConfig{
		CodeEmbeddingProvider: "openai",
		CodeEmbeddingModel:    "text-embedding-3-small",
	}
	assert.True(t, cfg.IsCodeIndexConfigured(), "should be configured with provider + model")

	noModel := TimmyConfig{
		CodeEmbeddingProvider: "openai",
	}
	assert.False(t, noModel.IsCodeIndexConfigured(), "should not be configured without model")

	empty := TimmyConfig{}
	assert.False(t, empty.IsCodeIndexConfigured(), "should not be configured when empty")
}

func TestTimmyConfig_BaseURLFields(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Empty(t, cfg.LLMBaseURL, "default LLMBaseURL should be empty")
	assert.Empty(t, cfg.TextEmbeddingBaseURL, "default TextEmbeddingBaseURL should be empty")
	assert.Empty(t, cfg.CodeEmbeddingBaseURL, "default CodeEmbeddingBaseURL should be empty")

	cfg.LLMBaseURL = "http://localhost:1234/v1"
	cfg.TextEmbeddingBaseURL = "http://localhost:5678/v1"
	cfg.CodeEmbeddingBaseURL = "http://localhost:9012/v1"
	assert.Equal(t, "http://localhost:1234/v1", cfg.LLMBaseURL)
	assert.Equal(t, "http://localhost:5678/v1", cfg.TextEmbeddingBaseURL)
	assert.Equal(t, "http://localhost:9012/v1", cfg.CodeEmbeddingBaseURL)
}

func TestTimmyConfig_Defaults(t *testing.T) {
	cfg := DefaultTimmyConfig()
	assert.Equal(t, 10, cfg.TextRetrievalTopK)
	assert.Equal(t, 10, cfg.CodeRetrievalTopK)
	assert.Equal(t, 512, cfg.ChunkSize)
	assert.Equal(t, 50, cfg.ChunkOverlap)
	assert.Equal(t, 256, cfg.MaxMemoryMB)
	assert.Equal(t, 120, cfg.LLMTimeoutSeconds)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyConfig`
Expected: FAIL — `TextEmbeddingProvider`, `CodeEmbeddingProvider`, etc. not defined

- [ ] **Step 3: Write the implementation**

Replace the contents of `internal/config/timmy.go`:

```go
package config

// TimmyConfig holds configuration for the Timmy AI assistant feature
type TimmyConfig struct {
	Enabled                   bool   `yaml:"enabled" env:"TMI_TIMMY_ENABLED"`
	LLMProvider               string `yaml:"llm_provider" env:"TMI_TIMMY_LLM_PROVIDER"`
	LLMModel                  string `yaml:"llm_model" env:"TMI_TIMMY_LLM_MODEL"`
	LLMAPIKey                 string `yaml:"llm_api_key" env:"TMI_TIMMY_LLM_API_KEY"`
	LLMBaseURL                string `yaml:"llm_base_url" env:"TMI_TIMMY_LLM_BASE_URL"`
	TextEmbeddingProvider     string `yaml:"text_embedding_provider" env:"TMI_TIMMY_TEXT_EMBEDDING_PROVIDER"`
	TextEmbeddingModel        string `yaml:"text_embedding_model" env:"TMI_TIMMY_TEXT_EMBEDDING_MODEL"`
	TextEmbeddingAPIKey       string `yaml:"text_embedding_api_key" env:"TMI_TIMMY_TEXT_EMBEDDING_API_KEY"`
	TextEmbeddingBaseURL      string `yaml:"text_embedding_base_url" env:"TMI_TIMMY_TEXT_EMBEDDING_BASE_URL"`
	TextRetrievalTopK         int    `yaml:"text_retrieval_top_k" env:"TMI_TIMMY_TEXT_RETRIEVAL_TOP_K"`
	CodeEmbeddingProvider     string `yaml:"code_embedding_provider" env:"TMI_TIMMY_CODE_EMBEDDING_PROVIDER"`
	CodeEmbeddingModel        string `yaml:"code_embedding_model" env:"TMI_TIMMY_CODE_EMBEDDING_MODEL"`
	CodeEmbeddingAPIKey       string `yaml:"code_embedding_api_key" env:"TMI_TIMMY_CODE_EMBEDDING_API_KEY"`
	CodeEmbeddingBaseURL      string `yaml:"code_embedding_base_url" env:"TMI_TIMMY_CODE_EMBEDDING_BASE_URL"`
	CodeRetrievalTopK         int    `yaml:"code_retrieval_top_k" env:"TMI_TIMMY_CODE_RETRIEVAL_TOP_K"`
	MaxConversationHistory    int    `yaml:"max_conversation_history" env:"TMI_TIMMY_MAX_CONVERSATION_HISTORY"`
	OperatorSystemPrompt      string `yaml:"operator_system_prompt" env:"TMI_TIMMY_OPERATOR_SYSTEM_PROMPT"`
	MaxMemoryMB               int    `yaml:"max_memory_mb" env:"TMI_TIMMY_MAX_MEMORY_MB"`
	InactivityTimeoutSeconds  int    `yaml:"inactivity_timeout_seconds" env:"TMI_TIMMY_INACTIVITY_TIMEOUT_SECONDS"`
	MaxMessagesPerUserPerHour int    `yaml:"max_messages_per_user_per_hour" env:"TMI_TIMMY_MAX_MESSAGES_PER_USER_PER_HOUR"`
	MaxSessionsPerThreatModel int    `yaml:"max_sessions_per_threat_model" env:"TMI_TIMMY_MAX_SESSIONS_PER_THREAT_MODEL"`
	MaxConcurrentLLMRequests  int    `yaml:"max_concurrent_llm_requests" env:"TMI_TIMMY_MAX_CONCURRENT_LLM_REQUESTS"`
	ChunkSize                 int    `yaml:"chunk_size" env:"TMI_TIMMY_CHUNK_SIZE"`
	ChunkOverlap              int    `yaml:"chunk_overlap" env:"TMI_TIMMY_CHUNK_OVERLAP"`
	LLMTimeoutSeconds         int    `yaml:"llm_timeout_seconds" env:"TMI_TIMMY_LLM_TIMEOUT_SECONDS"`
}

// DefaultTimmyConfig returns configuration with sensible defaults
func DefaultTimmyConfig() TimmyConfig {
	return TimmyConfig{
		Enabled:                   false,
		TextRetrievalTopK:         10,
		CodeRetrievalTopK:         10,
		MaxConversationHistory:    50,
		MaxMemoryMB:               256,
		InactivityTimeoutSeconds:  3600,
		MaxMessagesPerUserPerHour: 60,
		MaxSessionsPerThreatModel: 50,
		MaxConcurrentLLMRequests:  10,
		ChunkSize:                 512,
		ChunkOverlap:              50,
		LLMTimeoutSeconds:         120,
	}
}

// IsConfigured returns true if the required LLM and text embedding providers are configured
func (tc TimmyConfig) IsConfigured() bool {
	return tc.LLMProvider != "" && tc.LLMModel != "" &&
		tc.TextEmbeddingProvider != "" && tc.TextEmbeddingModel != ""
}

// IsCodeIndexConfigured returns true if the code embedding provider and model are configured
func (tc TimmyConfig) IsCodeIndexConfigured() bool {
	return tc.CodeEmbeddingProvider != "" && tc.CodeEmbeddingModel != ""
}
```

- [ ] **Step 4: Fix all compilation errors**

The old field names (`EmbeddingProvider`, `EmbeddingModel`, `EmbeddingAPIKey`, `EmbeddingBaseURL`, `RetrievalTopK`) are referenced in several files. These will be fixed in subsequent tasks, but to keep the build green, update the references now with temporary stubs. The files that reference old config fields:

1. `api/timmy_llm_service.go` — references `cfg.EmbeddingModel`, `cfg.EmbeddingAPIKey`, `cfg.EmbeddingBaseURL`, `s.config.EmbeddingModel`
2. `api/timmy_session_manager.go` — references `sm.config.EmbeddingModel`, `sm.config.RetrievalTopK`

In `api/timmy_llm_service.go`, replace all references:
- `cfg.EmbeddingModel` → `cfg.TextEmbeddingModel`
- `cfg.EmbeddingAPIKey` → `cfg.TextEmbeddingAPIKey`
- `cfg.EmbeddingBaseURL` → `cfg.TextEmbeddingBaseURL`
- `s.config.EmbeddingModel` → `s.config.TextEmbeddingModel`

In `api/timmy_session_manager.go`, replace:
- `sm.config.EmbeddingModel` → `sm.config.TextEmbeddingModel`
- `sm.config.RetrievalTopK` → `sm.config.TextRetrievalTopK`

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestTimmyConfig`
Expected: PASS

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 6: Update config-development.yml**

In `config-development.yml`, replace the `timmy:` section's embedding fields:

```yaml
timmy:
  enabled: true
  llm_provider: openai
  llm_model: google/gemma-4-26b-a4b
  llm_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  llm_base_url: http://localhost:1234/v1
  text_embedding_provider: openai
  text_embedding_model: text-embedding-nomic-embed-text-v1.5
  text_embedding_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  text_embedding_base_url: http://localhost:1234/v1
```

- [ ] **Step 7: Commit**

```bash
git add internal/config/timmy.go internal/config/timmy_test.go api/timmy_llm_service.go api/timmy_session_manager.go config-development.yml
git commit -m "feat(timmy): replace single embedding config with dual text/code config"
```

---

### Task 3: Data Layer — Add IndexType to TimmyEmbedding Model

**Files:**
- Modify: `api/models/timmy.go`
- Modify: `internal/dbschema/timmy.go`

- [ ] **Step 1: Add IndexType field to TimmyEmbedding model**

In `api/models/timmy.go`, add the `IndexType` field to `TimmyEmbedding` after the `ChunkIndex` field:

```go
	ChunkIndex     int       `gorm:"not null;index:idx_timmy_embeddings_entity,priority:4"`
	IndexType      string    `gorm:"type:varchar(10);not null;default:text;index:idx_timmy_embeddings_entity,priority:5"`
	ContentHash    string    `gorm:"type:varchar(64);not null"`
```

- [ ] **Step 2: Update DB schema definition**

In `internal/dbschema/timmy.go`, in the `timmy_embeddings` table's `Columns` slice, add after the `chunk_index` entry:

```go
{Name: "index_type", DataType: "character varying", IsNullable: false},
```

In the same table's `Indexes` slice, update the composite index to include `index_type`:

```go
{Name: "idx_timmy_embeddings_entity", Columns: []string{"threat_model_id", "entity_type", "entity_id", "chunk_index", "index_type"}, IsUnique: false},
```

- [ ] **Step 3: Verify build**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 4: Run existing tests to ensure no regressions**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: PASS (existing tests should still work — new column has a default value)

- [ ] **Step 5: Commit**

```bash
git add api/models/timmy.go internal/dbschema/timmy.go
git commit -m "feat(timmy): add index_type column to TimmyEmbedding model"
```

---

### Task 4: Embedding Store — Replace ListByThreatModel with ListByThreatModelAndIndexType

**Files:**
- Modify: `api/timmy_embedding_store.go`
- Modify: `api/timmy_embedding_store_gorm.go`
- Modify: `api/timmy_embedding_store_test.go`
- Modify: `api/timmy_vector_manager.go` (update caller)
- Modify: `api/timmy_session_manager.go` (update caller)

- [ ] **Step 1: Write the tests**

Replace `api/timmy_embedding_store_test.go` with tests that use the new method and verify index type isolation:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyEmbeddingStore_CreateAndListByIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-001"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-001",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 0",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     1,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-002",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 1",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "repository",
			EntityID:       "repo-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeCode,
			ContentHash:    "hash-003",
			EmbeddingModel: "code-embedding-model",
			EmbeddingDim:   768,
			ChunkText:      "Repository code chunk 0",
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// List text embeddings only
	textResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, textResults, 2)
	for _, r := range textResults {
		assert.Equal(t, IndexTypeText, r.IndexType)
	}

	// List code embeddings only
	codeResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Len(t, codeResults, 1)
	assert.Equal(t, IndexTypeCode, codeResults[0].IndexType)
	assert.Equal(t, "repository", codeResults[0].EntityType)

	// Listing for a different TM returns empty
	other, err := store.ListByThreatModelAndIndexType(ctx, "tm-other", IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestTimmyEmbeddingStore_DeleteByEntity(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-002"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-a",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset chunk",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "threat",
			EntityID:       "threat-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-b",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Threat chunk",
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only the asset embedding
	err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
	require.NoError(t, err)

	remaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "threat", remaining[0].EntityType)
}

func TestTimmyEmbeddingStore_DeleteByThreatModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-003"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeText,
			ContentHash:    "hash-x",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Chunk text",
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "repository",
			EntityID:       "repo-001",
			ChunkIndex:     0,
			IndexType:      IndexTypeCode,
			ContentHash:    "hash-y",
			EmbeddingModel: "code-model",
			EmbeddingDim:   768,
			ChunkText:      "Code chunk",
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	err = store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)

	textAfter, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, textAfter)

	codeAfter, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Empty(t, codeAfter)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: FAIL — `ListByThreatModelAndIndexType` not defined

- [ ] **Step 3: Update the interface**

Replace the contents of `api/timmy_embedding_store.go`:

```go
package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmyEmbeddingStore defines operations for persisting vector embeddings
type TimmyEmbeddingStore interface {
	ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
	CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
	DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) error
	DeleteByThreatModel(ctx context.Context, threatModelID string) error
}

// GlobalTimmyEmbeddingStore is the global embedding store instance
var GlobalTimmyEmbeddingStore TimmyEmbeddingStore
```

- [ ] **Step 4: Update the GORM implementation**

In `api/timmy_embedding_store_gorm.go`, rename `ListByThreatModel` to `ListByThreatModelAndIndexType` and add the `indexType` filter:

```go
// ListByThreatModelAndIndexType returns all embeddings for a threat model and index type,
// ordered by entity_type, entity_id, chunk_index
func (s *GormTimmyEmbeddingStore) ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing embeddings for threat model %s, index type %s", threatModelID, indexType)

	var embeddings []models.TimmyEmbedding
	err := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
		Order("entity_type ASC, entity_id ASC, chunk_index ASC").
		Find(&embeddings).Error
	if err != nil {
		logger.Error("Failed to list embeddings for threat model %s, index type %s: %v", threatModelID, indexType, err)
		return nil, fmt.Errorf("failed to list embeddings: %w", err)
	}

	logger.Debug("Found %d embeddings for threat model %s, index type %s", len(embeddings), threatModelID, indexType)
	return embeddings, nil
}
```

- [ ] **Step 5: Update callers**

In `api/timmy_vector_manager.go`, update `GetOrLoadIndex` (line 68):
- Change `m.embeddingStore.ListByThreatModel(ctx, threatModelID)` to `m.embeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, IndexTypeText)`

(This is a temporary placeholder — Task 6 will make the vector manager fully index-type-aware. For now, hardcoding `IndexTypeText` keeps it compiling.)

In `api/timmy_session_manager.go`, update `prepareVectorIndex` (line 513):
- Change `GlobalTimmyEmbeddingStore.ListByThreatModel(ctx, threatModelID)` to `GlobalTimmyEmbeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, IndexTypeText)`

(Same temporary measure — Task 8 will parameterize this.)

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test-unit name=TestTimmyEmbeddingStore`
Expected: PASS

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 7: Commit**

```bash
git add api/timmy_embedding_store.go api/timmy_embedding_store_gorm.go api/timmy_embedding_store_test.go api/timmy_vector_manager.go api/timmy_session_manager.go
git commit -m "feat(timmy): replace ListByThreatModel with ListByThreatModelAndIndexType"
```

---

### Task 5: Vector Manager Test Helper — Update makeTestEmbedding

**Files:**
- Modify: `api/timmy_vector_manager_test.go`

- [ ] **Step 1: Update makeTestEmbedding to include IndexType**

In `api/timmy_vector_manager_test.go`, update the `makeTestEmbedding` function to accept and set `indexType`:

```go
func makeTestEmbedding(tmID, entityType, entityID string, chunkIdx int, vector []float32, chunkText string, indexType string) models.TimmyEmbedding {
	return models.TimmyEmbedding{
		ThreatModelID:  tmID,
		EntityType:     entityType,
		EntityID:       entityID,
		ChunkIndex:     chunkIdx,
		IndexType:      indexType,
		ContentHash:    "hash-" + entityID + "-" + string(rune('0'+chunkIdx)),
		EmbeddingModel: "test-model",
		EmbeddingDim:   len(vector),
		VectorData:     float32ToBytes(vector),
		ChunkText:      models.DBText(chunkText),
	}
}
```

- [ ] **Step 2: Update all existing callers of makeTestEmbedding**

Update every call to `makeTestEmbedding` in the file to pass `IndexTypeText` as the last argument. For example:

```go
// Before:
makeTestEmbedding(tmID, "asset", "asset-001", 0, vec1, "Asset chunk one")
// After:
makeTestEmbedding(tmID, "asset", "asset-001", 0, vec1, "Asset chunk one", IndexTypeText)
```

Apply this to all calls in: `TestVectorIndexManager_LoadIndex`, `TestVectorIndexManager_CacheHit`, `TestVectorIndexManager_ReleaseIndex`, `TestVectorIndexManager_MemoryPressureEviction`, `TestVectorIndexManager_MemoryPressureRejection`, `TestVectorIndexManager_GetStatus`.

- [ ] **Step 3: Run tests to verify they pass**

Run: `make test-unit name=TestVectorIndexManager`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/timmy_vector_manager_test.go
git commit -m "refactor(timmy): update makeTestEmbedding to include indexType parameter"
```

---

### Task 6: Vector Manager — Composite Keys and InvalidateIndex

**Files:**
- Modify: `api/timmy_vector_manager.go`
- Modify: `api/timmy_vector_manager_test.go`

- [ ] **Step 1: Write tests for composite key behavior and InvalidateIndex**

Add to `api/timmy_vector_manager_test.go`:

```go
func TestVectorIndexManager_CompositeKey_Isolation(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-composite"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	// Create text and code embeddings
	embeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "text chunk", IndexTypeText),
		makeTestEmbedding(tmID, "repository", "repo-001", 0, vec, "code chunk", IndexTypeCode),
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load text index
	textIdx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	require.NotNil(t, textIdx)
	assert.Equal(t, 1, textIdx.Count(), "text index should have 1 vector")

	// Load code index — should be a separate index
	codeIdx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, dim)
	require.NoError(t, err)
	require.NotNil(t, codeIdx)
	assert.Equal(t, 1, codeIdx.Count(), "code index should have 1 vector")

	// They should be different index instances
	assert.NotSame(t, textIdx, codeIdx)

	// Status should show 2 indexes loaded
	status := mgr.GetStatus()
	assert.Equal(t, 2, status["indexes_loaded"])
}

func TestVectorIndexManager_CompositeKey_IndependentEviction(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-indep-evict"

	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    store,
		maxMemoryBytes:    200,
		inactivityTimeout: 5 * time.Minute,
	}

	// Load both indexes
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, 3)
	require.NoError(t, err)

	// Release both and inflate code index memory
	mgr.ReleaseIndex(tmID, IndexTypeText)
	mgr.ReleaseIndex(tmID, IndexTypeCode)

	mgr.mu.Lock()
	textKey := tmID + ":" + IndexTypeText
	codeKey := tmID + ":" + IndexTypeCode
	mgr.indexes[codeKey].MemoryBytes = 200
	// Make code index older so it gets evicted first
	mgr.indexes[codeKey].LastAccessed = time.Now().Add(-10 * time.Minute)
	mgr.indexes[textKey].LastAccessed = time.Now()
	mgr.mu.Unlock()

	// Load a new index — should evict code but keep text
	_, err = mgr.GetOrLoadIndex(ctx, "tm-other", IndexTypeText, 3)
	require.NoError(t, err)

	mgr.mu.Lock()
	_, textPresent := mgr.indexes[textKey]
	_, codePresent := mgr.indexes[codeKey]
	mgr.mu.Unlock()

	assert.True(t, textPresent, "text index should survive eviction")
	assert.False(t, codePresent, "code index should be evicted (oldest, inactive)")
}

func TestVectorIndexManager_InvalidateIndex(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-invalidate"

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load text index
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)

	// Release so it can be invalidated
	mgr.ReleaseIndex(tmID, IndexTypeText)

	// Invalidate text index
	mgr.InvalidateIndex(tmID, IndexTypeText)

	// Index should be removed
	mgr.mu.Lock()
	_, present := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	assert.False(t, present, "invalidated index should be removed")
}

func TestVectorIndexManager_InvalidateIndex_ActiveSessionsSkipped(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-inv-active"

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load and keep active
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)

	// Try to invalidate — should be skipped because ActiveSessions > 0
	mgr.InvalidateIndex(tmID, IndexTypeText)

	mgr.mu.Lock()
	_, present := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	assert.True(t, present, "index with active sessions should not be invalidated")
}

func TestVectorIndexManager_ReleaseIndex_CompositeKey(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-release-comp"

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, 3)
	require.NoError(t, err)

	// Release text only
	mgr.ReleaseIndex(tmID, IndexTypeText)

	mgr.mu.Lock()
	textKey := tmID + ":" + IndexTypeText
	codeKey := tmID + ":" + IndexTypeCode
	assert.Equal(t, 0, mgr.indexes[textKey].ActiveSessions)
	assert.Equal(t, 1, mgr.indexes[codeKey].ActiveSessions)
	mgr.mu.Unlock()
}

func TestVectorIndexManager_GetStatus_IncludesIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-status-type"

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, 3)
	require.NoError(t, err)

	status := mgr.GetStatus()
	indexes, ok := status["indexes"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, indexes, 2)

	indexTypes := make(map[string]bool)
	for _, entry := range indexes {
		assert.Contains(t, entry, "index_type")
		it, ok := entry["index_type"].(string)
		require.True(t, ok)
		indexTypes[it] = true
	}
	assert.True(t, indexTypes[IndexTypeText])
	assert.True(t, indexTypes[IndexTypeCode])
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestVectorIndexManager_CompositeKey`
Expected: FAIL — `GetOrLoadIndex` doesn't accept `indexType` parameter

- [ ] **Step 3: Update VectorIndexManager implementation**

Replace the contents of `api/timmy_vector_manager.go`:

```go
package api

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// LoadedIndex represents an in-memory vector index for a threat model and index type
type LoadedIndex struct {
	ThreatModelID  string
	IndexType      string
	Index          *VectorIndex
	LastAccessed   time.Time
	ActiveSessions int
	MemoryBytes    int64
}

// VectorIndexManager manages in-memory vector indexes keyed by threat model ID and index type
type VectorIndexManager struct {
	mu                sync.Mutex
	indexes           map[string]*LoadedIndex // key: "threatModelID:indexType"
	embeddingStore    TimmyEmbeddingStore
	maxMemoryBytes    int64
	inactivityTimeout time.Duration

	// Metrics
	totalEvictions    int64
	pressureEvictions int64
	rejectedSessions  int64
}

// NewVectorIndexManager creates a new manager with the given memory budget
func NewVectorIndexManager(embeddingStore TimmyEmbeddingStore, maxMemoryMB int, inactivityTimeoutSeconds int) *VectorIndexManager {
	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    embeddingStore,
		maxMemoryBytes:    int64(maxMemoryMB) * 1024 * 1024,
		inactivityTimeout: time.Duration(inactivityTimeoutSeconds) * time.Second,
	}
	go mgr.evictionLoop()
	return mgr
}

// compositeKey builds the map key from threat model ID and index type
func compositeKey(threatModelID, indexType string) string {
	return threatModelID + ":" + indexType
}

// GetOrLoadIndex returns the index for a threat model and index type, loading from DB if needed
func (m *VectorIndexManager) GetOrLoadIndex(ctx context.Context, threatModelID, indexType string, dimension int) (*VectorIndex, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := compositeKey(threatModelID, indexType)

	if loaded, ok := m.indexes[key]; ok {
		loaded.LastAccessed = time.Now()
		loaded.ActiveSessions++
		return loaded.Index, nil
	}

	if !m.canAllocate() {
		m.evictLRU()
		if !m.canAllocate() {
			m.rejectedSessions++
			return nil, fmt.Errorf("insufficient memory to load vector index")
		}
	}

	embeddings, err := m.embeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, indexType)
	if err != nil {
		return nil, fmt.Errorf("failed to load embeddings: %w", err)
	}

	idx := NewVectorIndex(dimension)
	for _, emb := range embeddings {
		vector := bytesToFloat32(emb.VectorData)
		idx.Add(emb.ID, vector, string(emb.ChunkText))
	}

	loaded := &LoadedIndex{
		ThreatModelID:  threatModelID,
		IndexType:      indexType,
		Index:          idx,
		LastAccessed:   time.Now(),
		ActiveSessions: 1,
		MemoryBytes:    idx.MemorySize(),
	}
	m.indexes[key] = loaded

	slogging.Get().Debug("Loaded vector index for threat model %s (type %s): %d vectors, %d bytes",
		threatModelID, indexType, idx.Count(), loaded.MemoryBytes)
	return idx, nil
}

// ReleaseIndex decrements the active session count for a specific index type
func (m *VectorIndexManager) ReleaseIndex(threatModelID, indexType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := compositeKey(threatModelID, indexType)
	if loaded, ok := m.indexes[key]; ok {
		loaded.ActiveSessions--
		if loaded.ActiveSessions < 0 {
			loaded.ActiveSessions = 0
		}
	}
}

// InvalidateIndex removes the in-memory index for a specific threat model and index type,
// forcing a reload from the embedding store on next access.
// Indexes with active sessions are not invalidated.
func (m *VectorIndexManager) InvalidateIndex(threatModelID, indexType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := compositeKey(threatModelID, indexType)
	if loaded, ok := m.indexes[key]; ok {
		if loaded.ActiveSessions > 0 {
			slogging.Get().Debug("Skipping invalidation of index %s: %d active sessions", key, loaded.ActiveSessions)
			return
		}
		delete(m.indexes, key)
		slogging.Get().Debug("Invalidated vector index %s", key)
	}
}

// GetStatus returns current memory and index status for the admin endpoint
func (m *VectorIndexManager) GetStatus() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	var totalMemory int64
	var largestIndex int64
	indexDetails := make([]map[string]any, 0, len(m.indexes))

	for _, loaded := range m.indexes {
		totalMemory += loaded.MemoryBytes
		if loaded.MemoryBytes > largestIndex {
			largestIndex = loaded.MemoryBytes
		}
		indexDetails = append(indexDetails, map[string]any{
			"threat_model_id": loaded.ThreatModelID,
			"index_type":      loaded.IndexType,
			"vectors":         loaded.Index.Count(),
			"memory_bytes":    loaded.MemoryBytes,
			"active_sessions": loaded.ActiveSessions,
			"last_accessed":   loaded.LastAccessed,
		})
	}

	avgSize := int64(0)
	if len(m.indexes) > 0 {
		avgSize = totalMemory / int64(len(m.indexes))
	}

	utilPct := float64(0)
	if m.maxMemoryBytes > 0 {
		utilPct = float64(totalMemory) / float64(m.maxMemoryBytes) * 100
	}

	return map[string]any{
		"memory_used_bytes":      totalMemory,
		"memory_budget_bytes":    m.maxMemoryBytes,
		"memory_utilization_pct": utilPct,
		"indexes_loaded":         len(m.indexes),
		"avg_index_size_bytes":   avgSize,
		"largest_index_bytes":    largestIndex,
		"evictions_total":        m.totalEvictions,
		"evictions_pressure":     m.pressureEvictions,
		"sessions_rejected":      m.rejectedSessions,
		"indexes":                indexDetails,
	}
}

func (m *VectorIndexManager) canAllocate() bool {
	var total int64
	for _, loaded := range m.indexes {
		total += loaded.MemoryBytes
	}
	return total < int64(float64(m.maxMemoryBytes)*0.9)
}

func (m *VectorIndexManager) evictLRU() {
	var oldest *LoadedIndex
	var oldestKey string
	for key, loaded := range m.indexes {
		if loaded.ActiveSessions > 0 {
			continue
		}
		if oldest == nil || loaded.LastAccessed.Before(oldest.LastAccessed) {
			oldest = loaded
			oldestKey = key
		}
	}
	if oldest != nil {
		delete(m.indexes, oldestKey)
		m.totalEvictions++
		m.pressureEvictions++
		slogging.Get().Debug("Pressure-evicted vector index %s", oldestKey)
	}
}

func (m *VectorIndexManager) evictionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for key, loaded := range m.indexes {
			if loaded.ActiveSessions == 0 && now.Sub(loaded.LastAccessed) > m.inactivityTimeout {
				delete(m.indexes, key)
				m.totalEvictions++
				slogging.Get().Debug("Inactivity-evicted vector index %s", key)
			}
		}
		m.mu.Unlock()
	}
}

// bytesToFloat32 converts a byte slice to a float32 slice (little-endian)
func bytesToFloat32(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// float32ToBytes converts a float32 slice to a byte slice (little-endian)
func float32ToBytes(data []float32) []byte {
	result := make([]byte, len(data)*4)
	for i, v := range data {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(result[i*4:(i+1)*4], bits)
	}
	return result
}
```

- [ ] **Step 4: Update existing tests that used old signatures**

In `api/timmy_vector_manager_test.go`, update all existing test functions to use the new `GetOrLoadIndex` and `ReleaseIndex` signatures:

- `TestVectorIndexManager_LoadIndex`: `mgr.GetOrLoadIndex(ctx, tmID, dim)` → `mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)`
- `TestVectorIndexManager_CacheHit`: same change, plus update `mgr.indexes[tmID]` → `mgr.indexes[tmID+":"+IndexTypeText]`
- `TestVectorIndexManager_ReleaseIndex`: same changes, plus `mgr.ReleaseIndex(tmID)` → `mgr.ReleaseIndex(tmID, IndexTypeText)`
- `TestVectorIndexManager_MemoryPressureEviction`: `mgr.GetOrLoadIndex(ctx, tmIDN, dim)` → `mgr.GetOrLoadIndex(ctx, tmIDN, IndexTypeText, dim)`, update map key access to use composite keys
- `TestVectorIndexManager_MemoryPressureRejection`: same pattern
- `TestVectorIndexManager_GetStatus`: same pattern

- [ ] **Step 5: Update callers in session manager**

In `api/timmy_session_manager.go`:
- `buildTier2Context` (line 641): `mgr.GetOrLoadIndex(ctx, threatModelID, dim)` → `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, IndexTypeText, dim)`
- `buildTier2Context` (line 648): `sm.vectorManager.ReleaseIndex(threatModelID)` → `sm.vectorManager.ReleaseIndex(threatModelID, IndexTypeText)`
- `prepareVectorIndex` (line 507): `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, dim)` → `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, IndexTypeText, dim)`

- [ ] **Step 6: Run all tests**

Run: `make test-unit name=TestVectorIndexManager`
Expected: PASS (all old + new tests)

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 7: Commit**

```bash
git add api/timmy_vector_manager.go api/timmy_vector_manager_test.go api/timmy_session_manager.go
git commit -m "feat(timmy): composite keys and InvalidateIndex in vector manager"
```

---

### Task 7: LLM Service — Dual Embedders

**Files:**
- Modify: `api/timmy_llm_service.go`

- [ ] **Step 1: Update TimmyLLMService struct and constructor for dual embedders**

Replace the struct, constructor, `EmbedTexts`, and `EmbeddingDimension` in `api/timmy_llm_service.go`. Keep `timmyBasePrompt`, `GenerateStreamingResponse`, and `GetBasePrompt` unchanged.

Update the struct:

```go
type TimmyLLMService struct {
	chatModel    llms.Model
	textEmbedder embeddings.Embedder
	codeEmbedder embeddings.Embedder // nil if code embedding not configured
	config       config.TimmyConfig
	basePrompt   string
}
```

Update `NewTimmyLLMService` to create both embedders:

```go
func NewTimmyLLMService(cfg config.TimmyConfig) (*TimmyLLMService, error) {
	if !cfg.IsConfigured() {
		return nil, fmt.Errorf("timmy LLM/embedding providers not configured")
	}

	timeoutSec := cfg.LLMTimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	httpClient := &http.Client{
		Timeout:   time.Duration(timeoutSec) * time.Second,
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}

	// Create chat model
	chatOpts := []openai.Option{
		openai.WithModel(cfg.LLMModel),
		openai.WithToken(cfg.LLMAPIKey),
		openai.WithHTTPClient(httpClient),
	}
	if cfg.LLMBaseURL != "" {
		chatOpts = append(chatOpts, openai.WithBaseURL(cfg.LLMBaseURL))
	}
	chatModel, err := openai.New(chatOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create LLM chat model: %w", err)
	}

	// Create text embedder (required)
	textEmbedder, err := createEmbedder(cfg.TextEmbeddingModel, cfg.TextEmbeddingAPIKey, cfg.TextEmbeddingBaseURL, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create text embedder: %w", err)
	}

	// Create code embedder (optional)
	var codeEmbedder embeddings.Embedder
	if cfg.IsCodeIndexConfigured() {
		codeEmbedder, err = createEmbedder(cfg.CodeEmbeddingModel, cfg.CodeEmbeddingAPIKey, cfg.CodeEmbeddingBaseURL, httpClient)
		if err != nil {
			return nil, fmt.Errorf("failed to create code embedder: %w", err)
		}
	}

	prompt := timmyBasePrompt
	if cfg.OperatorSystemPrompt != "" {
		prompt = prompt + "\n\n" + cfg.OperatorSystemPrompt
	}

	return &TimmyLLMService{
		chatModel:    chatModel,
		textEmbedder: textEmbedder,
		codeEmbedder: codeEmbedder,
		config:       cfg,
		basePrompt:   prompt,
	}, nil
}

// createEmbedder creates an embeddings.Embedder from model config
func createEmbedder(model, apiKey, baseURL string, httpClient *http.Client) (embeddings.Embedder, error) {
	embOpts := []openai.Option{
		openai.WithModel(model),
		openai.WithToken(apiKey),
		openai.WithEmbeddingModel(model),
		openai.WithHTTPClient(httpClient),
	}
	if baseURL != "" {
		embOpts = append(embOpts, openai.WithBaseURL(baseURL))
	}
	embLLM, err := openai.New(embOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding model: %w", err)
	}
	embedder, err := embeddings.NewEmbedder(embLLM)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedder: %w", err)
	}
	return embedder, nil
}
```

Update `EmbedTexts` to accept `indexType`:

```go
// EmbedTexts returns embeddings for the given texts using the embedder for the specified index type
func (s *TimmyLLMService) EmbedTexts(ctx context.Context, texts []string, indexType string) ([][]float32, error) {
	embedder, modelName, err := s.getEmbedder(indexType)
	if err != nil {
		return nil, err
	}

	tracer := otel.Tracer("tmi.timmy")
	ctx, embedSpan := tracer.Start(ctx, "timmy.embedding.generate",
		trace.WithAttributes(
			attribute.String("tmi.timmy.embedding_model", modelName),
			attribute.String("tmi.timmy.index_type", indexType),
			attribute.Int("tmi.timmy.text_count", len(texts)),
		),
	)
	defer embedSpan.End()

	embedStart := time.Now()
	vectors, err := embedder.EmbedDocuments(ctx, texts)
	embedDuration := time.Since(embedStart)
	if err != nil {
		return nil, fmt.Errorf("embedding failed: %w", err)
	}

	if m := tmiotel.GlobalMetrics; m != nil {
		m.TimmyEmbedDuration.Record(ctx, embedDuration.Seconds())
	}

	return vectors, nil
}
```

Update `EmbeddingDimension` to accept `indexType`:

```go
// EmbeddingDimension returns the dimension by embedding a test string with the specified index type
func (s *TimmyLLMService) EmbeddingDimension(ctx context.Context, indexType string) (int, error) {
	vectors, err := s.EmbedTexts(ctx, []string{"dimension test"}, indexType)
	if err != nil {
		return 0, err
	}
	if len(vectors) == 0 {
		return 0, fmt.Errorf("no embedding returned")
	}
	return len(vectors[0]), nil
}
```

Add the helper to select the right embedder:

```go
// getEmbedder returns the embedder and model name for the specified index type
func (s *TimmyLLMService) getEmbedder(indexType string) (embeddings.Embedder, string, error) {
	switch indexType {
	case IndexTypeText:
		return s.textEmbedder, s.config.TextEmbeddingModel, nil
	case IndexTypeCode:
		if s.codeEmbedder == nil {
			return nil, "", fmt.Errorf("code embedding not configured")
		}
		return s.codeEmbedder, s.config.CodeEmbeddingModel, nil
	default:
		return nil, "", fmt.Errorf("unknown index type: %s", indexType)
	}
}
```

- [ ] **Step 2: Update callers of EmbedTexts and EmbeddingDimension**

In `api/timmy_session_manager.go`:
- `prepareVectorIndex` (line ~501): `sm.llmService.EmbeddingDimension(ctx)` → `sm.llmService.EmbeddingDimension(ctx, IndexTypeText)`
- `prepareVectorIndex` (line ~580): `sm.llmService.EmbedTexts(ctx, chunks)` → `sm.llmService.EmbedTexts(ctx, chunks, IndexTypeText)`
- `buildTier2Context` (line ~629): `sm.llmService.EmbedTexts(ctx, []string{query})` → `sm.llmService.EmbedTexts(ctx, []string{query}, IndexTypeText)`

- [ ] **Step 3: Verify build and tests**

Run: `make build-server`
Expected: BUILD SUCCESS

Run: `make test-unit name=TestTimmy`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/timmy_llm_service.go api/timmy_session_manager.go
git commit -m "feat(timmy): dual embedders in LLM service with index-type routing"
```

---

### Task 8: Session Manager — Parameterized prepareVectorIndex and Dual-Index buildTier2Context

**Files:**
- Modify: `api/timmy_session_manager.go`
- Modify: `api/timmy_session_manager_test.go`

- [ ] **Step 1: Write tests for splitSourcesByIndexType**

Add to `api/timmy_session_manager_test.go`:

```go
func TestSplitSourcesByIndexType(t *testing.T) {
	sources := []SourceSnapshotEntry{
		{EntityType: "asset", EntityID: "a1", Name: "Database"},
		{EntityType: "threat", EntityID: "t1", Name: "SQL Injection"},
		{EntityType: "repository", EntityID: "r1", Name: "Backend Repo"},
		{EntityType: "diagram", EntityID: "d1", Name: "DFD"},
		{EntityType: "repository", EntityID: "r2", Name: "Frontend Repo"},
		{EntityType: "note", EntityID: "n1", Name: "Design Note"},
		{EntityType: "document", EntityID: "doc1", Name: "RFC Doc"},
	}

	textSources, codeSources := splitSourcesByIndexType(sources)

	assert.Len(t, textSources, 5, "should have 5 text sources")
	assert.Len(t, codeSources, 2, "should have 2 code sources")

	for _, s := range textSources {
		assert.NotEqual(t, "repository", s.EntityType)
	}
	for _, s := range codeSources {
		assert.Equal(t, "repository", s.EntityType)
	}
}

func TestSplitSourcesByIndexType_Empty(t *testing.T) {
	textSources, codeSources := splitSourcesByIndexType(nil)
	assert.Empty(t, textSources)
	assert.Empty(t, codeSources)
}

func TestSplitSourcesByIndexType_NoRepositories(t *testing.T) {
	sources := []SourceSnapshotEntry{
		{EntityType: "asset", EntityID: "a1", Name: "DB"},
		{EntityType: "threat", EntityID: "t1", Name: "XSS"},
	}

	textSources, codeSources := splitSourcesByIndexType(sources)
	assert.Len(t, textSources, 2)
	assert.Empty(t, codeSources)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestSplitSourcesByIndexType`
Expected: FAIL — `splitSourcesByIndexType` not defined

- [ ] **Step 3: Implement splitSourcesByIndexType and update prepareVectorIndex**

Add `splitSourcesByIndexType` to `api/timmy_session_manager.go`:

```go
// splitSourcesByIndexType partitions source snapshot entries into text and code sources
func splitSourcesByIndexType(sources []SourceSnapshotEntry) (textSources, codeSources []SourceSnapshotEntry) {
	for _, src := range sources {
		if EntityTypeToIndexType(src.EntityType) == IndexTypeCode {
			codeSources = append(codeSources, src)
		} else {
			textSources = append(textSources, src)
		}
	}
	return textSources, codeSources
}
```

Update `prepareVectorIndex` signature to accept `indexType`:

```go
func (sm *TimmySessionManager) prepareVectorIndex(
	ctx context.Context,
	threatModelID, indexType string,
	sources []SourceSnapshotEntry,
	progress SessionProgressCallback,
) error {
```

Inside `prepareVectorIndex`, update:
- `sm.llmService.EmbeddingDimension(ctx, IndexTypeText)` → `sm.llmService.EmbeddingDimension(ctx, indexType)`
- `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, IndexTypeText, dim)` → `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, dim)`
- `GlobalTimmyEmbeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, IndexTypeText)` → `GlobalTimmyEmbeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, indexType)`
- `sm.llmService.EmbedTexts(ctx, chunks, IndexTypeText)` → `sm.llmService.EmbedTexts(ctx, chunks, indexType)`
- `EmbeddingModel: sm.config.TextEmbeddingModel` → use a helper to select the right model:

```go
			embeddingModel := sm.config.TextEmbeddingModel
			if indexType == IndexTypeCode {
				embeddingModel = sm.config.CodeEmbeddingModel
			}
```

- Set `IndexType` on each embedding record: `IndexType: indexType`

Update `CreateSession` to split sources and call `prepareVectorIndex` twice:

```go
	if sm.llmService != nil && sm.vectorManager != nil {
		textSources, codeSources := splitSourcesByIndexType(sources)

		ctx, indexSpan := tracer.Start(ctx, "timmy.session.index_prepare")
		indexErr := sm.prepareVectorIndex(ctx, threatModelID, IndexTypeText, textSources, progress)
		indexSpan.End()
		if indexErr != nil {
			logger.Warn("Failed to prepare text vector index for session %s: %v", session.ID, indexErr)
		}

		if sm.config.IsCodeIndexConfigured() && len(codeSources) > 0 {
			ctx, codeIndexSpan := tracer.Start(ctx, "timmy.session.code_index_prepare")
			codeIndexErr := sm.prepareVectorIndex(ctx, threatModelID, IndexTypeCode, codeSources, progress)
			codeIndexSpan.End()
			if codeIndexErr != nil {
				logger.Warn("Failed to prepare code vector index for session %s: %v", session.ID, codeIndexErr)
			}
		}
	}
```

Update `buildTier2Context` to search both indexes:

```go
func (sm *TimmySessionManager) buildTier2Context(ctx context.Context, threatModelID, query string) string {
	// Search text index
	textTier2 := sm.searchIndex(ctx, threatModelID, IndexTypeText, query, sm.config.TextRetrievalTopK)

	// Search code index if configured
	codeTier2 := ""
	if sm.config.IsCodeIndexConfigured() {
		codeTier2 = sm.searchIndex(ctx, threatModelID, IndexTypeCode, query, sm.config.CodeRetrievalTopK)
	}

	// Merge results
	if textTier2 == "" && codeTier2 == "" {
		return ""
	}

	var sb strings.Builder
	if textTier2 != "" {
		sb.WriteString(textTier2)
	}
	if codeTier2 != "" {
		if textTier2 != "" {
			sb.WriteString("\n")
		}
		sb.WriteString(codeTier2)
	}
	return sb.String()
}

// searchIndex embeds the query and searches a single index, returning formatted tier 2 context
func (sm *TimmySessionManager) searchIndex(ctx context.Context, threatModelID, indexType, query string, topK int) string {
	logger := slogging.Get()

	vectors, err := sm.llmService.EmbedTexts(ctx, []string{query}, indexType)
	if err != nil {
		logger.Warn("Failed to embed query for %s vector search: %v", indexType, err)
		return ""
	}
	if len(vectors) == 0 {
		return ""
	}

	dim := len(vectors[0])
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, dim)
	if err != nil {
		logger.Warn("Failed to get %s vector index for search: %v", indexType, err)
		return ""
	}
	defer sm.vectorManager.ReleaseIndex(threatModelID, indexType)

	return sm.contextBuilder.BuildTier2Context(idx, vectors[0], topK)
}
```

Add `"strings"` to the imports if not already present.

- [ ] **Step 4: Run tests**

Run: `make test-unit name=TestSplitSourcesByIndexType`
Expected: PASS

Run: `make test-unit name=TestTimmySessionManager`
Expected: PASS

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_session_manager.go api/timmy_session_manager_test.go
git commit -m "feat(timmy): parameterized prepareVectorIndex and dual-index buildTier2Context"
```

---

### Task 9: Full Test Suite and Lint

**Files:**
- All modified files

- [ ] **Step 1: Run lint**

Run: `make lint`
Expected: PASS (or only pre-existing warnings in `api/api.go`)

Fix any new lint issues.

- [ ] **Step 2: Run full unit test suite**

Run: `make test-unit`
Expected: PASS

- [ ] **Step 3: Build server**

Run: `make build-server`
Expected: BUILD SUCCESS

- [ ] **Step 4: Fix any failures, then commit if there were fixes**

```bash
git add -A
git commit -m "fix(timmy): address lint and test issues from dual-index refactor"
```

(Skip this commit if there were no fixes needed.)

---

### Task 10: Final Commit and Push

- [ ] **Step 1: Review all changes**

Run: `git log --oneline main..HEAD | head -20`

Verify the commit history shows the incremental dual-index infrastructure changes.

- [ ] **Step 2: Push**

```bash
git pull --rebase
git push
git status
```

Expected: "up to date with origin"
