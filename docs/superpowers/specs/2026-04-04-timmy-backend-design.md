# Timmy Backend Design Specification

**Date:** 2026-04-04
**Issue:** ericfitz/tmi#214
**Client issue:** ericfitz/tmi-ux#293
**Wiki:** [Timmy AI Assistant](https://github.com/ericfitz/tmi/wiki/Timmy-AI-Assistant)
**Discussion:** [docs/timmy-backend-design-discussion.md](../../timmy-backend-design-discussion.md)

## Overview

Timmy is a conversational AI assistant embedded in the threat modeling workflow. It is scoped to a single threat model and reasons over the model's data — assets, threats, diagrams, documents, repositories, and notes — to help users understand, analyze, and improve their threat models.

Timmy is inspired by Google's NotebookLM: a "grounded" chat that reasons over specific sources rather than answering from general knowledge alone. Users control which sub-entities are included in the conversation via the `timmy_enabled` flag on each sub-entity.

### Problems Solved

1. **Threat models are dense and hard to reason about holistically.** Timmy synthesizes across the full model and surfaces connections, gaps, or inconsistencies.
2. **Security review is bottlenecked on expert availability.** Timmy acts as an always-available collaborator for initial analysis.
3. **Threat modeling artifacts are underutilized after creation.** Timmy makes the model queryable.
4. **Onboarding to an existing threat model is slow.** Timmy provides guided summaries and answers targeted questions.

## Architecture

Five server-side subsystems:

```
+-----------------------------------------------------+
|                   TMI Server                         |
|                                                      |
|  +----------+  +--------------+  +---------------+   |
|  | Chat API |->| Session Mgr  |->|  LLM Service  |  |
|  |(REST+SSE)|  |              |  | (LangChainGo) |  |
|  +----------+  +------+-------+  +-------+-------+  |
|                       |                   |          |
|  +------------------+ |  +---------------+|          |
|  | Vector Index Mgr |<+  |Content Provider|          |
|  | (in-memory HNSW) |    |  (pluggable)  |<+        |
|  +--------+---------+    +---------------+           |
|           |                                          |
|  +--------v-----------------------------------------+|
|  |          Existing GORM Database                  ||
|  |  (embeddings table, sessions table, messages)    ||
|  +--------------------------------------------------+|
+------------------------------------------------------+
```

### 1. Chat API

REST endpoints for session CRUD and message sending. Session creation and message responses stream via SSE (Server-Sent Events). SSE is used instead of WebSocket because Timmy chat is a one-user-to-LLM interaction, not a collaborative multi-user session. The existing WebSocket infrastructure (diagram collaboration) and planned notification WebSocket (Issue #81) remain independent.

### 2. Session Manager

Manages session lifecycle: creation (with source snapshot), context construction (Tier 1 structured data + Tier 2 vector retrieval), conversation history, and session expiry. Sessions are private to their creator.

### 3. LLM Service

Provider-agnostic chat completion via LangChainGo. Handles prompt construction (base system prompt + operator extension + context + conversation history + user message), streaming token delivery, and usage tracking.

### 4. Vector Index Manager

Manages in-memory HNSW indexes per threat model. Handles loading from DB, incremental re-embedding of changed entities, LRU eviction, memory budget enforcement, and write-back on eviction. Embeddings are stored as rows in the existing GORM database — no separate vector database required.

### 5. Content Providers

Pluggable text extraction for converting source entities into plain text for chunking and embedding. Built-in providers handle database content, JSON structures (diagrams), HTTP/HTML pages, and PDFs. OAuth-based providers (Phase 2+) extend the existing auth provider infrastructure for Microsoft 365, Confluence, and Google Workspace. External/webhook integration is available from day one for repositories and other external content.

## Data Model

Four new tables in the existing GORM database.

### `timmy_sessions`

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | Session ID |
| `threat_model_id` | UUID (FK) | Parent threat model |
| `user_id` | UUID (FK to users) | Session creator (private to this user) |
| `title` | string | User-editable session title (auto-generated from first message if not provided) |
| `source_snapshot` | JSON | Frozen list of `{entity_type, entity_id}` for sub-entities that were `timmy_enabled` at session creation |
| `system_prompt_hash` | string | Hash of the system prompt used (base + operator extension), for change detection |
| `status` | string | `active`, `archived` |
| `created_at` | timestamp | |
| `modified_at` | timestamp | |
| `deleted_at` | timestamp | Soft delete |

### `timmy_messages`

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | Message ID |
| `session_id` | UUID (FK) | Parent session |
| `role` | string | `user`, `assistant` |
| `content` | text | Message text |
| `token_count` | int | Tokens used (for usage tracking) |
| `sequence` | int | Message order within session |
| `created_at` | timestamp | |

### `timmy_embeddings`

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | |
| `threat_model_id` | UUID (FK, indexed) | Which threat model this belongs to |
| `entity_type` | string | `asset`, `threat`, `document`, `note`, `diagram`, `repository` |
| `entity_id` | UUID | Source entity |
| `chunk_index` | int | Which chunk of the entity (0 for small entities, 0..N for large ones) |
| `content_hash` | string | Hash of the source content at embedding time (for staleness detection) |
| `embedding_model` | string | Model that produced this embedding (e.g., `text-embedding-3-small`) |
| `embedding_dim` | int | Vector dimension |
| `vector_data` | blob | The embedding vector as serialized bytes |
| `chunk_text` | text | The original text chunk (for injection into LLM context after retrieval) |
| `created_at` | timestamp | |

**Index:** Composite on `(threat_model_id, entity_type, entity_id, chunk_index)` for efficient loading and staleness checks.

### `timmy_usage`

| Column | Type | Description |
|--------|------|-------------|
| `id` | UUID (PK) | |
| `user_id` | UUID (FK) | |
| `session_id` | UUID (FK) | |
| `threat_model_id` | UUID (FK) | |
| `message_count` | int | Messages in this tracking period |
| `prompt_tokens` | int | Total prompt tokens |
| `completion_tokens` | int | Total completion tokens |
| `embedding_tokens` | int | Tokens used for embedding |
| `period_start` | timestamp | Start of tracking window |
| `period_end` | timestamp | End of tracking window |

### Design Notes

- `timmy_embeddings` stores raw vector bytes, not a database-native vector type — portable across all GORM-supported databases
- `chunk_text` is stored alongside the embedding to avoid a second DB round-trip after vector search
- `source_snapshot` in sessions is JSON rather than a join table — it is a frozen-in-time snapshot, not a live relationship
- Usage tracking is per-session with time windows, giving operators both per-user and per-threat-model cost visibility

## Chat API

### Session Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/threat_models/{id}/chat/sessions` | Create a new session (snapshots current `timmy_enabled` sources). Returns SSE stream of preparation progress. |
| `GET` | `/threat_models/{id}/chat/sessions` | List the current user's sessions for this threat model |
| `GET` | `/threat_models/{id}/chat/sessions/{session_id}` | Get session details (metadata + source snapshot) |
| `DELETE` | `/threat_models/{id}/chat/sessions/{session_id}` | Soft-delete a session |

### Message Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/threat_models/{id}/chat/sessions/{session_id}/messages` | Send a message. Returns SSE stream of the assistant's response. |
| `GET` | `/threat_models/{id}/chat/sessions/{session_id}/messages` | List message history (paginated, `limit`/`offset`) |

### Admin Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/admin/timmy/usage` | Aggregated usage stats with filters: `user_id`, `threat_model_id`, date range |
| `GET` | `/admin/timmy/status` | Live memory state: loaded indexes, sizes, active sessions, eviction counts |

### Session Creation SSE Stream

`POST /threat_models/{id}/chat/sessions` returns `Content-Type: text/event-stream`:

```
event: session_created
data: {"session_id": "uuid", "source_count": 12}

event: progress
data: {"phase": "loading", "entity_type": "asset", "entity_name": "User Database", "progress": 0}

event: progress
data: {"phase": "loading", "entity_type": "asset", "entity_name": "User Database", "progress": 100}

event: progress
data: {"phase": "retrieving", "entity_type": "document", "entity_name": "Auth Design Doc", "progress": 0}

event: progress
data: {"phase": "retrieving", "entity_type": "document", "entity_name": "Auth Design Doc", "progress": 100}

event: progress
data: {"phase": "chunking", "entity_type": "document", "entity_name": "Auth Design Doc", "detail": "4 chunks", "progress": 100}

event: progress
data: {"phase": "embedding", "entity_type": "document", "entity_name": "Auth Design Doc", "progress": 50}

event: progress
data: {"phase": "embedding", "entity_type": "document", "entity_name": "Auth Design Doc", "progress": 100}

event: auth_required
data: {"provider": "microsoft365", "entity_name": "Auth Design Doc", "auth_url": "https://..."}

event: ready
data: {"session_id": "uuid", "sources_loaded": 12, "chunks_embedded": 47, "cached_reused": 35, "newly_embedded": 12}
```

**Phases:** `loading` (reading from DB), `retrieving` (fetching external URLs), `chunking`, `embedding`.

**`auth_required` event:** Sent when a document URL requires OAuth consent the server doesn't have. Client opens the auth URL for user consent. After consent, extraction proceeds.

**`ready` event:** Signals the client to switch from preparation UI to chat UI. Includes stats for the client to display.

For sessions resuming with a warm cache, the stream is brief — staleness check, re-embed only what changed, then `ready`.

If the user closes the connection mid-preparation, the server finishes embedding work in the background (useful to cache) but stops sending events.

### Message SSE Stream

`POST /threat_models/{id}/chat/sessions/{session_id}/messages` returns `Content-Type: text/event-stream`:

```
event: message_start
data: {"message_id": "uuid", "role": "assistant"}

event: token
data: {"content": "Based on"}

event: token
data: {"content": " your threat model,"}

event: token
data: {"content": " the authentication"}

...

event: message_end
data: {"message_id": "uuid", "token_count": 347}
```

**Error event:** `event: error` with `data: {"code": "rate_limited", "message": "Hourly message limit reached"}`.

The full assistant message is persisted to `timmy_messages` server-side when the stream completes.

### Authorization

- All endpoints require JWT Bearer authentication
- User must have at least `reader` role on the parent threat model
- Sessions are filtered to the authenticated user only — no user can see another user's sessions
- If the server is under memory pressure and cannot load the vector index, it returns `503 Service Unavailable` with a `Retry-After` header

### Sharing

Sessions are private to the creator. Sharing is a client-side concern: the client (TMI-UX) provides UI to save a message or session as a note in the threat model, at which point the note inherits permissions from the threat model. No server-side sharing model is needed.

## Vector Index Manager

### Lifecycle

```
Idle (no index)
  │
  ├── Session created → Load from DB
  │     ├── Embeddings exist → Deserialize, check staleness, re-embed changed entities
  │     └── First time → Embed all timmy_enabled entities from scratch
  │
  ▼
Active (index in memory)
  │
  ├── Query (similarity search per chat message)
  ├── Entity updated in threat model → Mark stale (re-embed on next session load, not eagerly)
  ├── Inactivity timeout (configurable, default 1 hour) → Write-back to DB, evict
  └── Memory pressure → LRU eviction (write-back, then free)
  │
  ▼
Idle (no index)
```

### Staleness Detection

Each row in `timmy_embeddings` has a `content_hash`. On session load:

1. Load all embedding rows for the threat model
2. For each `timmy_enabled` entity, compute current content hash
3. Hash matches → reuse cached embedding
4. Hash differs → delete old chunks, re-chunk, re-embed
5. New entity (no embedding rows) → chunk and embed
6. Entity removed or `timmy_enabled` set to false → delete embedding rows

### Memory Budget

The manager tracks memory explicitly rather than relying on Go runtime stats:

- Each loaded index has a known size: `num_vectors x embedding_dim x 4 bytes` plus HNSW graph overhead (~1.5x multiplier)
- Operator configures `timmy.max_memory_mb` (default: 256MB)
- When approaching the budget: evict LRU idle indexes (write-back to DB first)
- If over budget after evicting all idle indexes: reject new session creation with `503`
- Indexes with active sessions are never evicted

### Write-Back Strategy

- Write-back on eviction (inactivity timeout or memory pressure)
- Write-back after initial embedding of a new threat model
- Write-back after incremental re-embedding on session load
- No periodic flush — server crash means embeddings are recomputed on next load

### Concurrency

- Multiple users can have simultaneous sessions on the same threat model — they share the same in-memory index (read-only after loading)
- Index loading is synchronized — if two sessions start simultaneously for the same threat model, only one triggers the load; the other waits
- Similarity search is read-only and thread-safe

### Operational Metrics

| Metric | Description |
|--------|-------------|
| `timmy.memory.used_bytes` | Current total memory across all loaded indexes |
| `timmy.memory.budget_bytes` | The configured budget |
| `timmy.memory.utilization_pct` | `used / budget` as percentage |
| `timmy.indexes.loaded_count` | Number of threat model indexes currently in memory |
| `timmy.indexes.avg_size_bytes` | Average index size |
| `timmy.indexes.largest_size_bytes` | Largest currently loaded index |
| `timmy.indexes.evictions_total` | Cumulative LRU evictions |
| `timmy.indexes.evictions_pressure` | Evictions caused by memory pressure (not inactivity) |
| `timmy.sessions.rejected_total` | Sessions denied due to memory pressure (503s) |
| `timmy.indexes.load_time_seconds` | Histogram of index load times |

**Tuning guidance:** Set `timmy.max_memory_mb` to approximately `peak_concurrent_threat_models x avg_index_size x 1.5`. Monitor `timmy.sessions.rejected_total` — if nonzero, increase the budget. Monitor `timmy.indexes.evictions_pressure` — a high rate indicates indexes are being loaded and evicted repeatedly, degrading cold-start performance.

## Content Providers

### Interface

```go
// EntityReference identifies a source entity for content extraction.
// For DB-resident content (notes, assets), URI is empty and the provider
// reads directly from the database using EntityType + EntityID.
// For external content (documents with URLs), URI is the fetch target.
type EntityReference struct {
    EntityType string // "asset", "threat", "document", "note", "diagram", "repository"
    EntityID   string // UUID of the source entity
    URI        string // External URL (empty for DB-resident content)
}

type ContentProvider interface {
    // CanHandle returns true if this provider can extract content from the given entity
    CanHandle(ctx context.Context, ref EntityReference) bool

    // Extract fetches and returns plain text content
    Extract(ctx context.Context, ref EntityReference, userToken *OAuthToken) (ExtractedContent, error)
}

type ExtractedContent struct {
    Text        string            // Extracted plain text
    Title       string            // Document title if available
    ContentType string            // Original content type (e.g., "application/pdf")
    Metadata    map[string]string // Provider-specific metadata
}
```

The server maintains a registry of content providers, matched in priority order. When embedding an entity, the server constructs an `EntityReference` and iterates providers until one returns `CanHandle == true`.

### Built-in Providers (Phase 1)

**Direct Text Provider:**
- Handles entities whose content is already in the database (notes, asset/threat/repository descriptions)
- No HTTP fetching needed, always available

**JSON Provider:**
- Handles diagram entities (DFD JSON)
- Extracts semantic text from structured data: node labels, edge descriptions, annotation text, security boundary names
- Produces readable descriptions like "Process: Auth Service connects to Store: User Database via flow: credentials (crosses trust boundary: External/Internal)"

**HTTP/HTML Provider:**
- Handles `http://` and `https://` URLs returning `text/html` or `text/plain`
- Strips HTML tags, extracts main content
- SSRF protection (see below)
- No authentication — public URLs only

**PDF Provider:**
- Handles URLs returning `application/pdf`
- Fetches via HTTP, extracts text using a Go PDF library
- Same SSRF protection as HTTP provider

### OAuth Content Providers (Phase 2+)

OAuth content providers extend the existing TMI auth provider infrastructure (reuse client IDs, secrets, and token storage) with additional scopes for content access.

**Pattern for each provider:**

1. Operator configures additional content-access scopes in the existing OAuth provider configuration
2. When a document URL matches a provider, the server checks for a stored OAuth token for that user + provider with the required scopes
3. If no token exists, the session creation SSE stream sends an `auth_required` event with an authorization URL
4. Client opens the auth URL, user consents, callback stores the token
5. Extraction proceeds

**Planned providers:**
- Microsoft 365 (SharePoint, OneDrive — DOCX, PPTX, Excel via Graph API)
- Atlassian (Confluence pages via REST API)
- Google Workspace (Google Docs, Sheets, Slides via export API)

### External/Webhook Provider

Available from day one for content TMI cannot fetch directly (repositories, internal systems behind VPNs):

- External tooling extracts text and pushes chunked content into TMI via API
- Document entity stores a flag indicating "externally managed content"
- Vector Index Manager skips these entities during its own extraction pipeline and uses externally-pushed embeddings

### SSRF Protection

All HTTP-based providers share a common URL validator:

- Block RFC 1918 private ranges (10.x, 172.16-31.x, 192.168.x)
- Block loopback (127.x, ::1)
- Block link-local (169.254.x, fe80::)
- Block cloud metadata endpoints (169.254.169.254, etc.)
- Operator-configurable allowlist for internal URLs (e.g., self-hosted Confluence)

## LLM Service

### System Prompt Structure

Three layers, concatenated:

1. **Base prompt (immutable, ships with TMI):**
   - Identity: "You are Timmy, a security analysis assistant for threat modeling."
   - Role: Analyze threats, identify gaps, explain data flows, suggest mitigations, answer questions
   - Guardrails: Don't fabricate threats not grounded in sources, distinguish source material from general knowledge, don't invent CVE numbers or CVSS scores
   - Output style: Cite specific entities by name when referencing threat model content

2. **Operator extension (configurable):**
   - Organizational context, compliance frameworks, internal standards
   - E.g., "All threat assessments must reference our internal risk classification: P0-P4"

3. **Threat model context (per-session, constructed dynamically):**
   - See context construction below

### Context Construction Per Message

```
+-------------------------------------+
| System Prompt (base + operator)     |
+-------------------------------------+
| Tier 1: Structured Overview         |
|  - Threat model name, status, owner |
|  - All entity names + descriptions  |
|  - Threat details (severity, CWEs,  |
|    CVSS, mitigations)               |
|  - Diagram structure (from JSON     |
|    provider: nodes, edges, trust    |
|    boundaries)                      |
|  - Asset/repository metadata        |
+-------------------------------------+
| Tier 2: Retrieved Chunks            |
|  - Top-K chunks from vector search  |
|    on the user's current message    |
|  - Annotated with source entity     |
|    name and type                    |
+-------------------------------------+
| Conversation History                |
|  - All prior messages in session    |
|  - (truncated from the front if     |
|    approaching context limit)       |
+-------------------------------------+
| User Message                        |
+-------------------------------------+
```

**Two-tier rationale:** Tier 1 (structured data) is small, predictable, and gives the LLM the "big picture" — entity names, threat severities, diagram topology, CWEs, CVSS scores. Tier 2 (vector-retrieved chunks) provides targeted detail from large content sources — document text, note text, source code — relevant to the current question.

### Chunking Strategy

Content is split into chunks for embedding. The strategy varies by content type:

- **Structured data (Tier 1):** Not chunked — serialized as a single structured block in the LLM context.
- **Notes and document text:** Sentence-aware chunking with overlap. Target chunk size: ~512 tokens with ~50 token overlap between adjacent chunks. Split on sentence boundaries to preserve semantic coherence.
- **Diagram JSON:** The JSON provider produces semantic text descriptions (not raw JSON). These are typically small enough to be a single chunk per diagram.
- **Large documents (PDF, DOCX):** Same sentence-aware chunking as notes, applied after text extraction.

Chunk size and overlap are configurable but ship with sensible defaults. LangChainGo provides text splitter utilities that handle this.

### Token Budget Management

If the total context exceeds the model's context window:

1. First: reduce top-K chunks
2. Second: truncate conversation history from the front (oldest messages first), preserving the first message (often establishes intent) and the most recent N messages
3. If still over: return an error suggesting the user start a new session

### Streaming

1. Open SSE stream to the client
2. Call LangChainGo with streaming enabled
3. Forward each token as an SSE `token` event
4. On completion: persist the full assembled message to `timmy_messages`, record usage to `timmy_usage`, send `message_end` event

## Rate Limiting and Usage Tracking

### Rate Limits

| Setting | Default | Scope | Response |
|---------|---------|-------|----------|
| `timmy.max_messages_per_user_per_hour` | 60 | Per user | 429 with `Retry-After` |
| `timmy.max_sessions_per_threat_model` | 50 | Per threat model (active sessions across all users) | 429 on session creation |
| `timmy.max_concurrent_llm_requests` | 10 | Server-wide | 503 with `Retry-After` (queued briefly before rejection) |

Rate limit state is tracked in Redis when available (multi-instance consistency), falling back to in-memory counters for single-instance deployments — following the existing Redis/in-memory fallback pattern.

### Usage Tracking

Every LLM interaction records to `timmy_usage`: prompt tokens, completion tokens, embedding tokens, rolled up per session with time windows. The admin usage endpoint supports filtering by user, threat model, and date range to answer: "How much is Timmy costing us?", "Which users are the heaviest consumers?", "Which threat models drive the most usage?"

## Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `timmy.enabled` | `false` | Master switch — endpoints return 404 when disabled |
| `timmy.llm_provider` | (required) | LangChainGo provider name |
| `timmy.llm_model` | (required) | Model identifier |
| `timmy.embedding_provider` | (required) | Embedding provider name |
| `timmy.embedding_model` | (required) | Embedding model identifier |
| `timmy.retrieval_top_k` | 10 | Chunks retrieved per query |
| `timmy.max_conversation_history` | 50 | Max messages before truncation starts |
| `timmy.operator_system_prompt` | `""` | Operator's custom system prompt extension |
| `timmy.max_memory_mb` | 256 | Memory budget for loaded vector indexes |
| `timmy.inactivity_timeout_seconds` | 3600 | Idle index eviction timeout |
| `timmy.max_messages_per_user_per_hour` | 60 | Per-user message rate limit |
| `timmy.max_sessions_per_threat_model` | 50 | Max active sessions per threat model |
| `timmy.max_concurrent_llm_requests` | 10 | Server-wide concurrent LLM call limit |

When `timmy.enabled` is `true` but LLM/embedding providers are not configured, endpoints return `503` with a clear error message. A warning is logged at startup.

## Phasing

### Phase 1: Core Timmy

- Data model (all four tables)
- Chat API (all endpoints, SSE streaming for sessions and messages)
- Session Manager (source snapshot, context construction, history management)
- LLM Service (LangChainGo integration, streaming, system prompt layers)
- Vector Index Manager (in-memory HNSW, DB serialization, staleness detection, memory budget, LRU eviction)
- Content Providers: Direct Text, JSON (diagrams), HTTP/HTML, PDF
- Rate limiting and usage tracking
- Admin endpoints (usage, status/metrics)
- SSRF protection
- Configuration and master switch

### Phase 2: OAuth Content Providers

- Microsoft 365 provider (SharePoint, OneDrive — DOCX, PPTX)
- Extend existing OAuth infrastructure with content-access scopes
- `auth_required` SSE event flow
- Admin UX for provider configuration (TMI-UX)

### Phase 3: Additional Providers

- Atlassian Confluence provider
- Google Workspace provider
- External embedding API for repository content pushed via webhook
