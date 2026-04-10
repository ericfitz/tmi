# Query Decomposition and Cross-Encoder Reranking Design

**Issue:** #241 (sub-spec 3 of 3)
**Date:** 2026-04-10
**Scope:** Optional LLM-driven query decomposition, cross-encoder reranking interface and HTTP implementation, updated query pipeline in session manager, config additions, wiki updates
**Depends on:** Sub-spec 1 (dual-index infrastructure) — completed

## Overview

Enhance Timmy's query pipeline with two optional stages: LLM-driven query decomposition (breaking user questions into index-specific sub-queries) and cross-encoder reranking (rescoring merged results from both indexes for higher-precision context). Both features are opt-in via configuration and degrade gracefully when disabled — the current behavior (same query to both indexes, concatenated results) is preserved as the default.

## Design Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Query decomposition | Optional, default off | Extra LLM call adds latency; uncertain quality improvement; operators opt in |
| Reranker integration | `Reranker` interface + HTTP implementation | Clean abstraction; Cohere/Jina/vLLM all follow same REST pattern |
| Reranker query source | Original user query (not decomposed sub-queries) | Cross-encoder scores relevance to user intent, not retrieval optimization |
| Top-K pipeline | Per-index top-K (recall) → reranker top-K (precision) | Standard two-stage IR pattern; reduces context from ~20 to ~10 high-quality chunks |
| Fallback behavior | nil decomposer → original query; nil reranker → no reranking | Graceful degradation; zero cost when not configured |

## Section 1: Configuration Changes

### New Fields in `internal/config/timmy.go`

**Query decomposition:**

| Field | Env Var | Default |
|-------|---------|---------|
| `QueryDecompositionEnabled` | `TMI_TIMMY_QUERY_DECOMPOSITION_ENABLED` | false |

**Cross-encoder reranking (optional — if absent, results are not reranked):**

| Field | Env Var | Default |
|-------|---------|---------|
| `RerankProvider` | `TMI_TIMMY_RERANK_PROVIDER` | (none) |
| `RerankModel` | `TMI_TIMMY_RERANK_MODEL` | (none) |
| `RerankAPIKey` | `TMI_TIMMY_RERANK_API_KEY` | (none) |
| `RerankBaseURL` | `TMI_TIMMY_RERANK_BASE_URL` | (none) |
| `RerankTopK` | `TMI_TIMMY_RERANK_TOP_K` | 10 |

### New Method

`IsRerankConfigured() bool` — returns true if `RerankProvider` and `RerankModel` are set.

### Defaults

`QueryDecompositionEnabled: false`, `RerankTopK: 10`. `IsConfigured()` unchanged — reranking and decomposition are optional.

### Wiki Updates

Update the following pages in `/Users/efitz/Projects/tmi.wiki`:

**Configuration-Reference.md:**
- Add "Query Decomposition" subsection under Timmy AI Assistant with `TMI_TIMMY_QUERY_DECOMPOSITION_ENABLED`
- Add "Cross-Encoder Reranking" subsection with all `TMI_TIMMY_RERANK_*` variables
- Add reranker config to the YAML example block
- Add minimum config example showing reranker setup

**Timmy-AI-Assistant.md:**
- Update Architecture Decisions to document the optional query decomposition and reranking pipeline stages
- Update In Progress section to reflect completion of #241

Commit and push wiki changes.

## Section 2: Reranker Interface and HTTP Implementation

New file `api/timmy_reranker.go`.

### Interface

```go
type Reranker interface {
    Rerank(ctx context.Context, query string, documents []string) ([]RerankResult, error)
}

type RerankResult struct {
    Index    int     // original index in the documents slice
    Score    float64 // relevance score (higher = more relevant)
    Document string  // the document text
}
```

### HTTP Implementation — `APIReranker`

```go
type APIReranker struct {
    httpClient *http.Client
    baseURL    string
    model      string
    apiKey     string
    topK       int
}
```

- Sends `POST` to `{baseURL}/rerank`. The `baseURL` should include any version prefix (e.g., `http://localhost:1234/v1` for LM Studio, `https://api.jina.ai/v1` for Jina Cloud)
- Request body:

```json
{
  "model": "jinaai/jina-reranker-v3-mlx",
  "query": "user question",
  "documents": ["chunk 1 text", "chunk 2 text", ...],
  "top_n": 10
}
```

- Response body (standard Cohere/Jina format):

```json
{
  "results": [
    {"index": 3, "relevance_score": 0.95},
    {"index": 0, "relevance_score": 0.87},
    ...
  ]
}
```

- Authorization: `Authorization: Bearer {apiKey}` header
- OpenTelemetry instrumented
- Timeout: uses the same HTTP client as the LLM service (configurable via `LLMTimeoutSeconds`)

### Factory Function

```go
func NewReranker(cfg config.TimmyConfig, httpClient *http.Client) Reranker
```

Returns `*APIReranker` if `IsRerankConfigured()` is true, `nil` otherwise. The session manager checks for nil and skips reranking.

## Section 3: Query Decomposer

New file `api/timmy_query_decomposer.go`.

### Interface

```go
type QueryDecomposer interface {
    Decompose(ctx context.Context, query string, hasCodeIndex bool) (*DecomposedQuery, error)
}

type DecomposedQuery struct {
    TextQuery string // query optimized for text index
    CodeQuery string // query optimized for code index — empty if no code index
    Strategy  string // "parallel" or "sequential" (informational)
}
```

### LLM Implementation — `LLMQueryDecomposer`

- Uses the existing inference LLM via `TimmyLLMService.GenerateStreamingResponse`
- Structured prompt:

```
You are a query decomposition assistant. Given a user question about a threat model,
generate optimized sub-queries for searching different indexes.

Text index contains: assets, threats, diagrams, documents, notes
Code index contains: repository source code

Respond with JSON only:
{"text_query": "...", "code_query": "...", "strategy": "parallel"}

If the question is only about text content, set code_query to empty string.
If the question is only about code, set text_query to empty string.

User question: {query}
```

- When `hasCodeIndex` is false, the prompt omits the code index instruction and code_query is always empty
- Graceful fallback: on LLM error or unparseable JSON output, returns the original query for both sub-queries (logs a warning)
- Uses `onToken: nil` (no streaming — just the final result)
- OpenTelemetry instrumented

### Nil Fallback

When `QueryDecompositionEnabled` is false, no `QueryDecomposer` is created (nil on the session manager). The session manager uses the original query for both indexes — current behavior.

## Section 4: Updated Query Pipeline in Session Manager

### New Fields on `TimmySessionManager`

```go
reranker   Reranker        // nil if not configured
decomposer QueryDecomposer // nil if not enabled
```

Set during construction in `NewTimmySessionManager`.

### Updated `buildTier2Context` Flow

```
1. Determine sub-queries:
   If decomposer != nil:
     decomposed = decomposer.Decompose(ctx, query, config.IsCodeIndexConfigured())
     textQuery = decomposed.TextQuery (or original query if empty)
     codeQuery = decomposed.CodeQuery
   Else:
     textQuery = query
     codeQuery = query

2. Search text index with textQuery → textResults ([]VectorSearchResult)

3. If code index configured:
     Search code index with codeQuery → codeResults

4. Merge all results into one candidates list

5. If reranker != nil:
     Extract document texts from candidates
     reranked = reranker.Rerank(ctx, originalQuery, documentTexts)
     Format reranked results as tier 2 context
   Else:
     Format merged results as tier 2 context (current behavior)
```

### Changes to `searchIndex`

Currently returns a formatted string. Needs to return raw results for reranking:

**New method:** `searchIndexRaw(ctx, tmID, indexType, query string, topK int) []VectorSearchResult`
- Same logic as current `searchIndex` but returns `[]VectorSearchResult` instead of formatted text

**Existing `searchIndex`** can be removed or kept as a convenience wrapper.

### Changes to `ContextBuilder`

**New method:** `BuildTier2ContextFromResults(results []VectorSearchResult) string`
- Formats pre-searched (and optionally reranked) results
- Same formatting as current `BuildTier2Context` but accepts results directly instead of performing the search

The existing `BuildTier2Context(index, queryVector, topK)` remains for backward compatibility but is no longer called from the session manager.

## Section 5: Testing

### Unit Tests

**`api/timmy_reranker_test.go`:**
- `APIReranker` with `httptest.NewServer`: valid rerank response returns results sorted by score, error handling (HTTP error, malformed JSON), empty documents slice

**`api/timmy_query_decomposer_test.go`:**
- `LLMQueryDecomposer` with mocked LLM response: valid decomposition returns distinct text/code queries, graceful fallback on LLM error returns original query, graceful fallback on unparseable JSON, `hasCodeIndex=false` produces empty code query

**`api/timmy_context_builder_test.go`** (update):
- `BuildTier2ContextFromResults` with results, empty results, nil results

**`api/timmy_session_manager_test.go`** (update):
- `buildTier2Context` with nil decomposer uses original query
- `buildTier2Context` with nil reranker returns unranked results

**Config tests** (`internal/config/timmy_test.go`):
- `IsRerankConfigured()` true/false
- `DefaultTimmyConfig()` has `QueryDecompositionEnabled: false` and `RerankTopK: 10`

### Integration Tests

**`test/integration/workflows/timmy_query_pipeline_test.go`:**

Skipped automatically when env vars are not set (`t.Skip`).

**Config env vars:**
```bash
TMI_TEST_RERANK_BASE_URL=http://localhost:1234/v1
TMI_TEST_RERANK_MODEL=jinaai/jina-reranker-v3-mlx
TMI_TEST_RERANK_API_KEY=<provided at runtime>
TMI_TEST_DECOMPOSITION_BASE_URL=http://localhost:1234/v1
TMI_TEST_DECOMPOSITION_MODEL=<inference model loaded in LM Studio>
```

**Test cases:**
- Reranker integration: send real query + documents to local reranker, verify scored results return in descending order
- Decomposer integration: send mixed question (e.g., "What authentication vulnerabilities exist in the login handler code?"), verify distinct text and code sub-queries
- Full pipeline: decompose → search (mocked indexes with pre-loaded vectors) → rerank → verify final context is well-ordered

**Local dev setup:** LM Studio at `http://localhost:1234` with `jinaai/jina-reranker-v3-mlx` loaded.

## Section 6: Architecture Diagram

Create a Mermaid diagram of the Timmy server-side query pipeline showing the full flow from user message through decomposition, dual-index search, reranking, context building, and LLM synthesis. Add the diagram to the Timmy-AI-Assistant wiki page.

## Files Changed

| File | Change Type |
|------|-------------|
| `internal/config/timmy.go` | Modified: add reranker and decomposition config fields |
| `internal/config/timmy_test.go` | Modified: test new config fields |
| `api/timmy_reranker.go` | New: `Reranker` interface + `APIReranker` HTTP implementation |
| `api/timmy_reranker_test.go` | New: unit tests with mock HTTP server |
| `api/timmy_query_decomposer.go` | New: `QueryDecomposer` interface + `LLMQueryDecomposer` |
| `api/timmy_query_decomposer_test.go` | New: unit tests with mocked LLM |
| `api/timmy_session_manager.go` | Modified: wire decomposer + reranker into `buildTier2Context` |
| `api/timmy_session_manager_test.go` | Modified: test nil decomposer/reranker fallback |
| `api/timmy_context_builder.go` | Modified: add `BuildTier2ContextFromResults` |
| `api/timmy_context_builder_test.go` | Modified: test new method |
| `cmd/server/main.go` | Modified: create and wire decomposer + reranker |
| `test/integration/workflows/timmy_query_pipeline_test.go` | New: integration tests (env-gated) |

## Wiki Changes

| Page | Changes |
|------|---------|
| `Configuration-Reference.md` | Add query decomposition and reranker config sections |
| `Timmy-AI-Assistant.md` | Update architecture decisions, add pipeline diagram, update implementation status |

## Relationship to Other Sub-Specs

- **Sub-spec 1** (Dual-Index Infrastructure): Provides `searchIndex`, `IndexTypeText`/`IndexTypeCode`, `VectorSearchResult`, dual-embedder `EmbedTexts`. This sub-spec extends the search pipeline that sub-spec 1 built.
- **Sub-spec 2** (External Embedding APIs): Independent. External embedding ingestion works regardless of how queries are processed.
