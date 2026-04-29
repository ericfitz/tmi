# Timmy Integration Tests Design

**Issue:** #214 (feat: backend for timmy)
**Date:** 2026-04-05
**Branch:** dev/1.4.0

## Goal

Add integration tests for the Timmy chat endpoints, covering both structural HTTP behavior (CRUD, auth, pagination, errors) and LLM quality (grounding, multi-turn, embedding retrieval). Tests run against a real LLM provider (LM Studio locally).

## Prerequisite: Base URL Config Fields

The current `TimmyConfig` and `NewTimmyLLMService` hardcode the OpenAI API base URL. To point at LM Studio (or any OpenAI-compatible provider), we need custom base URL support.

### Changes

**`internal/config/timmy.go`** — Add two fields:
- `LLMBaseURL string` (`yaml:"llm_base_url"`, `env:"TMI_TIMMY_LLM_BASE_URL"`)
- `EmbeddingBaseURL string` (`yaml:"embedding_base_url"`, `env:"TMI_TIMMY_EMBEDDING_BASE_URL"`)

**`api/timmy_llm_service.go`** — Conditionally pass `openai.WithBaseURL(cfg.LLMBaseURL)` and `openai.WithBaseURL(cfg.EmbeddingBaseURL)` when the respective fields are non-empty.

No OpenAPI spec changes (internal config only).

### Development Config

Add Timmy block to `config-development.yml` (gitignored, not committed):

```yaml
timmy:
  enabled: true
  llm_provider: openai
  llm_model: google/gemma-4-26b-a4b
  llm_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  llm_base_url: http://localhost:1234/v1
  embedding_provider: openai
  embedding_model: text-embedding-nomic-embed-text-v1.5
  embedding_api_key: sk-lm-tW68Afc9:a3yyq0PHPEiSKx2qyJRJ
  embedding_base_url: http://localhost:1234/v1
```

`make start-dev` picks this up automatically.

## Test File Structure

Two test files under `test/integration/workflows/`:

### File 1: `timmy_crud_test.go` — Structural Tests

**Gate:** `INTEGRATION_TESTS=true` + `TIMMY_INTEGRATION_TESTS=true`

**Setup:** Create a threat model with 2 assets and 1 threat (all with `timmy_enabled` semantics), giving Timmy entities to snapshot during session creation.

| # | Subtest | Verifies |
|---|---------|----------|
| 1 | CreateSession | POST returns 201, valid UUID, correct threat_model_id, status="active", source_snapshot array, timestamps |
| 2 | CreateSessionWithTitle | POST with `{"title": "..."}` returns the title |
| 3 | ListSessions | GET returns paginated response with total/limit/offset, contains created sessions |
| 4 | ListSessionsPagination | GET with `?limit=1&offset=0` returns 1 session, correct total |
| 5 | GetSession | GET by ID returns the specific session with all fields |
| 6 | GetSessionNotFound | GET with fake UUID returns 404 |
| 7 | CreateMessage | POST message returns response with assistant message, non-empty content |
| 8 | ListMessages | GET messages returns paginated list with user + assistant messages, correct sequence ordering |
| 9 | ListMessagesPagination | GET with limit/offset works correctly |
| 10 | DeleteSession | DELETE returns 204, subsequent GET returns 404 |
| 11 | CrossUserIsolation | User B cannot GET/DELETE user A's session (403) |
| 12 | AdminUsage | Admin GET `/admin/timmy/usage` returns 200 |
| 13 | AdminStatus | Admin GET `/admin/timmy/status` returns 200 |
| 14 | AdminEndpointsForbiddenForNonAdmin | Regular user gets 403 on admin endpoints |

### File 2: `timmy_llm_test.go` — LLM Quality Tests

**Gate:** `TIMMY_LLM_TESTS=true` (separate flag, skipped by default)

**Setup:** Create a threat model with richer content:
- 2 assets: "Customer Database", "API Gateway"
- 2 threats: "SQL Injection on Customer Database", "Broken Authentication on API Gateway"
- 1 note with descriptive text about the system architecture

All entities have `timmy_enabled` semantics.

| # | Subtest | Verifies |
|---|---------|----------|
| 1 | ResponseReferencesContext | Ask about assets, response mentions at least one entity name |
| 2 | ThreatAnalysis | Ask about threats, response mentions at least one threat name |
| 3 | MultiTurnConversation | First message + follow-up, second response is non-empty and valid |
| 4 | LongUserMessage | ~2000 char message, no 500 error, non-empty response |
| 5 | EmbeddingRetrieval | Ask about note content, response references information from the note |

**Assertion style:** Loose checks only:
- Response is non-empty, length > 20 chars
- Grounding checks via `strings.Contains` on lowercased response for entity names
- No exact string matching
- 90s HTTP client timeout, 120s per-test timeout

## SSE Handling

`CreateSession` and `CreateMessage` use SSE streaming. A `ParseSSEResponse` helper function (co-located in the test files, not the framework) will:

1. Parse raw response body into `[]SSEEvent{Event string, Data string}`
2. Find the final `complete` or `message` event containing the JSON result
3. Return both parsed events (for progress verification) and the final JSON payload

## Test Environment Requirements

- TMI server running via `make start-dev` with Timmy config in `config-development.yml`
- LM Studio running at `http://localhost:1234` with:
  - Chat model: `google/gemma-4-26b-a4b` (or similar)
  - Embedding model: `text-embedding-nomic-embed-text-v1.5`
- OAuth stub running (`make start-oauth-stub`)
- PostgreSQL and Redis running (handled by `make start-dev`)

## Out of Scope

- CATS fuzzing of Timmy endpoints (separate follow-up)
- CI pipeline integration (LM Studio is local-only for now)
- Performance/load testing
