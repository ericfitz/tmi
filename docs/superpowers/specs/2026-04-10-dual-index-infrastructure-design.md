# Dual-Index Infrastructure Design

**Issue:** #241 (sub-spec 1 of 3)
**Date:** 2026-04-10
**Scope:** Configuration, data layer, vector manager, LLM service, and session manager changes to support dual vector indexes (text + code)

## Overview

Evolve Timmy's single-embedding-model, single-vector-index architecture to a dual-index system with separate embedding models for text content and code content. This sub-spec covers the internal infrastructure; external embedding ingestion APIs (sub-spec 2) and query decomposition/reranking (sub-spec 3) build on top.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Entity-to-index mapping | Strict: repository -> code, everything else -> text | Simple, deterministic, no config needed |
| Backward compatibility | None required; dev branch not yet merged to main | Clean break, remove old config vars |
| Vector manager key | Composite string `"<threatModelID>:<indexType>"` | Minimal structural change, all existing lifecycle logic works unchanged |
| Embedding store query | Replace `ListByThreatModel` with `ListByThreatModelAndIndexType` | Only consumer always knows the index type; no use case for unfiltered listing |
| Memory budget | Single shared pool for both index types | Most threat models won't have code repos; shared pool naturally allocates to actual usage |
| `prepareVectorIndex` | Parameterized with `indexType`, called twice | Avoids duplicating embedding logic; only the entity filter and embedder differ |

## Section 1: Configuration Changes

### Removed Fields (from `internal/config/timmy.go`)

- `EmbeddingProvider` (`TMI_TIMMY_EMBEDDING_PROVIDER`)
- `EmbeddingModel` (`TMI_TIMMY_EMBEDDING_MODEL`)
- `EmbeddingAPIKey` (`TMI_TIMMY_EMBEDDING_API_KEY`)
- `EmbeddingBaseURL` (`TMI_TIMMY_EMBEDDING_BASE_URL`)
- `RetrievalTopK` (`TMI_TIMMY_RETRIEVAL_TOP_K`)

### Added Fields

**Text embedding (required for Timmy to function):**

| Field | Env Var | Default |
|-------|---------|---------|
| `TextEmbeddingProvider` | `TMI_TIMMY_TEXT_EMBEDDING_PROVIDER` | (none) |
| `TextEmbeddingModel` | `TMI_TIMMY_TEXT_EMBEDDING_MODEL` | (none) |
| `TextEmbeddingAPIKey` | `TMI_TIMMY_TEXT_EMBEDDING_API_KEY` | (none) |
| `TextEmbeddingBaseURL` | `TMI_TIMMY_TEXT_EMBEDDING_BASE_URL` | (none) |
| `TextRetrievalTopK` | `TMI_TIMMY_TEXT_RETRIEVAL_TOP_K` | 10 |

**Code embedding (optional -- if absent, repositories are not vector-searchable):**

| Field | Env Var | Default |
|-------|---------|---------|
| `CodeEmbeddingProvider` | `TMI_TIMMY_CODE_EMBEDDING_PROVIDER` | (none) |
| `CodeEmbeddingModel` | `TMI_TIMMY_CODE_EMBEDDING_MODEL` | (none) |
| `CodeEmbeddingAPIKey` | `TMI_TIMMY_CODE_EMBEDDING_API_KEY` | (none) |
| `CodeEmbeddingBaseURL` | `TMI_TIMMY_CODE_EMBEDDING_BASE_URL` | (none) |
| `CodeRetrievalTopK` | `TMI_TIMMY_CODE_RETRIEVAL_TOP_K` | 10 |

### Method Changes

- `IsConfigured()` -- requires LLM config + text embedding config (provider, model). Code embedding is not required.
- `IsCodeIndexConfigured() bool` -- new method, returns true if the required code embedding fields (provider, model) are set. API key and base URL are optional (same pattern as text embedding -- a locally-hosted model may not need an API key).

## Section 2: Data Layer Changes

### TimmyEmbedding Model

Add `IndexType` field to `api/models/timmy.go`:

```go
IndexType string `gorm:"type:varchar(10);not null;default:text;index:idx_timmy_embeddings_entity,priority:5"`
```

- `varchar(10)` is portable across PostgreSQL and Oracle ADB
- `DEFAULT 'text'` is standard SQL supported by both databases
- Added to the existing composite index at priority 5

### Index Type Constants

New file `api/timmy_index_types.go`:

```go
const (
    IndexTypeText = "text"
    IndexTypeCode = "code"
)

func EntityTypeToIndexType(entityType string) string {
    if entityType == "repository" {
        return IndexTypeCode
    }
    return IndexTypeText
}
```

### Embedding Store Interface

Replace `ListByThreatModel` in `TimmyEmbeddingStore`:

```go
// Before:
ListByThreatModel(ctx context.Context, threatModelID string) ([]models.TimmyEmbedding, error)

// After:
ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
```

`CreateBatch` and `DeleteByEntity` signatures are unchanged -- the `IndexType` field on the model struct is persisted automatically.

### DB Migration

1. Add column: `index_type varchar(10) NOT NULL DEFAULT 'text'` to `timmy_embeddings`
2. Drop index: `idx_timmy_embeddings_entity`
3. Create index: `idx_timmy_embeddings_entity` on `(threat_model_id, entity_type, entity_id, chunk_index, index_type)`

### DB Schema (`internal/dbschema/timmy.go`)

Update `timmy_embeddings` table schema:
- Add column: `{Name: "index_type", DataType: "character varying", IsNullable: false}`
- Update composite index to include `index_type`

## Section 3: Vector Index Manager Changes

### Composite Key

The `indexes` map key changes from `threatModelID` to `"<threatModelID>:<indexType>"` (e.g., `"abc-123:text"`, `"abc-123:code"`).

### LoadedIndex Struct

Add `IndexType string` field for status reporting.

### Method Signature Changes

```go
// Before:
GetOrLoadIndex(ctx context.Context, threatModelID string, dimension int) (*VectorIndex, error)
ReleaseIndex(threatModelID string)

// After:
GetOrLoadIndex(ctx context.Context, threatModelID, indexType string, dimension int) (*VectorIndex, error)
ReleaseIndex(threatModelID, indexType string)
```

### New Method

```go
InvalidateIndex(threatModelID, indexType string)
```

Removes the in-memory index for a specific composite key, forcing a reload from the embedding store on next access. Needed by sub-spec 2 (external embedding ingestion) but added now to complete the manager API.

### Eviction Behavior

- Each composite key tracks its own `ActiveSessions` and `LastAccessed` independently
- LRU eviction treats each composite key as an independent entry
- Evicting `"abc-123:code"` does not affect `"abc-123:text"`

### Status Endpoint

`GetStatus()` includes `index_type` in each index detail entry alongside the existing fields.

## Section 4: LLM Service Changes

### Struct Changes

Replace single embedder with dual embedders:

```go
// Before:
embedder embeddings.Embedder

// After:
textEmbedder embeddings.Embedder
codeEmbedder embeddings.Embedder  // nil if code embedding not configured
```

### Constructor

`NewTimmyLLMService` creates both embedders from config:
- Text embedder: from `TextEmbeddingProvider/Model/APIKey/BaseURL` (required)
- Code embedder: from `CodeEmbeddingProvider/Model/APIKey/BaseURL` (only if `IsCodeIndexConfigured()` is true; nil otherwise)

### Method Signature Changes

```go
// Before:
EmbedTexts(ctx context.Context, texts []string) ([][]float32, error)
EmbeddingDimension(ctx context.Context) (int, error)

// After:
EmbedTexts(ctx context.Context, texts []string, indexType string) ([][]float32, error)
EmbeddingDimension(ctx context.Context, indexType string) (int, error)
```

- Routes to `textEmbedder` or `codeEmbedder` based on `indexType`
- Returns error if `indexType` is `"code"` but `codeEmbedder` is nil (code embedding not configured)
- OpenTelemetry span attributes include `index_type` and the specific model used

### Unchanged

`GenerateStreamingResponse` and `GetBasePrompt` are unaffected.

## Section 5: Session Manager Changes

### prepareVectorIndex

Signature changes:

```go
// Before:
prepareVectorIndex(ctx, threatModelID string, sources []SourceSnapshotEntry, progress SessionProgressCallback) error

// After:
prepareVectorIndex(ctx context.Context, threatModelID, indexType string, sources []SourceSnapshotEntry, progress SessionProgressCallback) error
```

Internal changes:
1. `sm.llmService.EmbeddingDimension(ctx, indexType)` for dimension
2. `sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, dim)` for index access
3. `GlobalTimmyEmbeddingStore.ListByThreatModelAndIndexType(ctx, threatModelID, indexType)` for hash comparison
4. `sm.llmService.EmbedTexts(ctx, chunks, indexType)` for embedding
5. Sets `IndexType` on each `TimmyEmbedding` record before persisting

### CreateSession

Splits source snapshot by index type, calls `prepareVectorIndex` twice:

```go
textSources, codeSources := splitSourcesByIndexType(sources)

// Always prepare text index
sm.prepareVectorIndex(ctx, threatModelID, IndexTypeText, textSources, progress)

// Prepare code index only if configured and there are repositories
if sm.config.IsCodeIndexConfigured() && len(codeSources) > 0 {
    sm.prepareVectorIndex(ctx, threatModelID, IndexTypeCode, codeSources, progress)
}
```

`splitSourcesByIndexType` uses `EntityTypeToIndexType()` to partition the snapshot entries.

### buildTier2Context

Searches both indexes and merges results:

1. Embed query with text model, search text index, get `TextRetrievalTopK` results
2. If code index is configured: embed query with code model, search code index, get `CodeRetrievalTopK` results
3. Concatenate results (text first, then code) and pass to `ContextBuilder.BuildTier2Context`

Per-index top-K is controlled by `TextRetrievalTopK` and `CodeRetrievalTopK`.

### Index Release

Session cleanup releases both index types:

```go
sm.vectorManager.ReleaseIndex(threatModelID, IndexTypeText)
sm.vectorManager.ReleaseIndex(threatModelID, IndexTypeCode)
```

## Section 6: Migration and Testing

### Config File Updates

- Update `config-development.yml` with `TMI_TIMMY_TEXT_EMBEDDING_*` env vars
- Update `.env.example` with new env var names
- Remove all references to old `TMI_TIMMY_EMBEDDING_*` env vars

### Unit Tests

| Area | Test Cases |
|------|------------|
| Config | `IsConfigured()` requires text embedding fields; `IsCodeIndexConfigured()` returns false when code fields are empty |
| Embedding store | `ListByThreatModelAndIndexType` filters by index type; embeddings with different `IndexType` values are isolated |
| Vector manager | Composite keys work correctly; evicting `"uuid:code"` does not affect `"uuid:text"`; `InvalidateIndex` removes the correct entry; `ReleaseIndex` decrements the correct composite key's session count |
| LLM service | Routes `EmbedTexts` to correct embedder by index type; returns error for code index when not configured |
| Session manager | `splitSourcesByIndexType` partitions correctly; `prepareVectorIndex` skips code index when not configured; `buildTier2Context` merges results from both indexes |

### Integration Tests

- Existing Timmy integration tests updated to use new config var names
- No new integration tests needed for this sub-spec (behavior is the same single-model flow with renamed config)

### No OpenAPI Changes

All changes in this sub-spec are internal. API endpoints for external embedding ingestion are in sub-spec 2.

## Files Changed

| File | Change Type |
|------|-------------|
| `internal/config/timmy.go` | Modified: replace single embedding config with text/code pairs |
| `api/models/timmy.go` | Modified: add `IndexType` field to `TimmyEmbedding` |
| `api/timmy_index_types.go` | New: index type constants and `EntityTypeToIndexType` |
| `api/timmy_embedding_store.go` | Modified: `ListByThreatModel` -> `ListByThreatModelAndIndexType` |
| `api/timmy_embedding_store_gorm.go` | Modified: implement new interface method |
| `api/timmy_vector_manager.go` | Modified: composite keys, `InvalidateIndex`, index-type-aware signatures |
| `api/timmy_llm_service.go` | Modified: dual embedders, index-type-aware `EmbedTexts`/`EmbeddingDimension` |
| `api/timmy_session_manager.go` | Modified: parameterized `prepareVectorIndex`, dual-index `buildTier2Context`, `splitSourcesByIndexType` |
| `internal/dbschema/timmy.go` | Modified: add `index_type` column and updated index to schema |
| `auth/migrations/*.go` | New: migration to add `index_type` column |
| `config-development.yml` | Modified: new env var names |
| `.env.example` | Modified: new env var names |

## Relationship to Other Sub-Specs

- **Sub-spec 2** (External Embedding APIs): Builds on the `IndexType` field, `InvalidateIndex` method, and dual-index store. Adds new authorization group, config-sharing endpoint, and embedding ingestion endpoint.
- **Sub-spec 3** (Query Decomposition + Reranking): Builds on the dual-index search in `buildTier2Context`. Adds LLM-driven query decomposition, cross-encoder reranking config, and the reranker pipeline stage.
