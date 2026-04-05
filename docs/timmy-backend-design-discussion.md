# Timmy Backend Design Discussion

**Date:** 2026-04-04
**Participants:** Eric Fitzgerald, Claude (AI assistant)
**Issue:** ericfitz/tmi#214

This document captures the brainstorming conversation that shaped the Timmy backend architecture. It is preserved as-is because the reasoning and tradeoff analysis were valuable.

---

## Starting Point

Issue #214 describes the server-side work for Timmy:

- Add chat API for clients (endpoint + schema)
- Integrate with LLMs (configs + functionality)
- Integrate with vector store
- Add vector store (integration with vector store database, add vectorization & indexing)

The client-side design (ericfitz/tmi-ux#293) describes Timmy as a NotebookLM-like feature: a separate chat page per threat model, with source management (toggling sub-entities in/out via `timmy_enabled`) and the ability to review or resume previous sessions.

---

## Purpose and Capabilities (Agreed)

Before discussing implementation, we aligned on what Timmy is and the problems it solves.

**What it is:** Timmy is a conversational AI assistant embedded in the threat modeling workflow. It's scoped to a single threat model — you talk to Timmy *about* a specific threat model, and it has deep context on that model's data: its assets, threats, diagrams, documents, repositories, and notes.

**The NotebookLM analogy:** Like Google's NotebookLM, Timmy is a "grounded" chat — it doesn't just answer from general knowledge, it reasons over the specific sources the user has loaded. Users control which sub-entities are included (via the `timmy_enabled` flag), so they can focus the conversation on relevant material.

**Problems it solves:**

1. **Threat models are dense and hard to reason about holistically.** A mature threat model has dozens of assets, threats, data flows, and supporting documents. Humans struggle to hold all of that in their heads simultaneously. Timmy can synthesize across the full model and surface connections, gaps, or inconsistencies a person might miss.

2. **Security review is bottlenecked on expert availability.** Not every team has a senior security reviewer on hand. Timmy acts as an always-available collaborator — it can't replace a human reviewer, but it can help teams self-serve on initial analysis, ask better questions, and arrive at a review better prepared.

3. **Threat modeling artifacts are underutilized after creation.** Teams build threat models and then rarely revisit them conversationally. Timmy makes the model queryable: "What are the highest-risk data flows?", "Which assets lack mitigations?", "Summarize the threats related to authentication."

4. **Onboarding to an existing threat model is slow.** A new team member or reviewer joining a threat model has to read through everything. Timmy can provide guided summaries and answer targeted questions, dramatically reducing ramp-up time.

**How customers use it:** A user navigates to a threat model's chat page, sees their sources (sub-entities) in a sidebar, toggles which ones to include, and has a conversation. They can ask Timmy to analyze threats, identify gaps, explain data flows, suggest mitigations, or just summarize what's there. Previous chat sessions are preserved and can be resumed.

---

## LLM Provider Strategy

**Question:** The codebase currently has no LLM dependencies. Which LLM integration approach?

**Options discussed:**
- A) Claude API (Anthropic) — native Go SDK
- B) OpenAI-compatible API — broader ecosystem
- C) Provider-agnostic abstraction — define an LLM interface, implement for multiple providers
- D) Something else

**Decision:** Provider-agnostic abstraction using LangChainGo. This lets operators choose their LLM provider via configuration.

---

## Vector Store Strategy

**Question:** What's the preference for the vector store backend?

**Options discussed:**
- A) PostgreSQL with pgvector — simplest, no new infrastructure
- B) Dedicated vector database (Pinecone, Weaviate, etc.) — purpose-built, more features
- C) Start with pgvector, design for swappability

**Eric's concern:** TMI customers deploy with PostgreSQL, Oracle ADB, MySQL, or MSSQL. If we only support pgvector, customers on other databases would need a separate Postgres deployment anyway. His aspiration:

> - The vector store is configured separately from the existing database.
> - We use an abstraction layer (preferably LangChainGo) that lets the code just communicate with the abstraction layer, and not have any details about the actual store.
> - In this fashion, the customer can choose the database and vector store that they want. Cost-conscious customers or those with modest requirements can choose to use the same database server (e.g. PostgreSQL + pgvector, or Oracle ADB 23ai) for the database and the vector store.

### Honest Assessment of the Abstracted Vector Store Approach

**Pros:**
- Customers choose what fits their infrastructure
- Single-DB deployments possible (pgvector, Oracle 23ai)
- Clean separation of concerns in the code
- LangChainGo already has vector store abstractions for pgvector, Pinecone, Weaviate, Qdrant, Chroma, etc.

**Cons and sharp edges identified:**

1. **LangChainGo vector store support is uneven.** The Go ecosystem lags behind LangChain Python significantly. Some vector store implementations may be immature, missing features, or poorly maintained. Oracle ADB vector search, for example, almost certainly has no LangChainGo adapter — we'd have to write one.

2. **Embedding model coupling.** The vector store is only half the story — you also need an embedding model to vectorize threat model content. Different vector stores expect different embedding dimensions. If a customer switches vector stores, they may need to re-embed everything. The abstraction needs to encompass both the store *and* the embedder, and they need to be configured as a pair.

3. **Hybrid search inconsistency.** A key capability for Timmy is likely hybrid search (vector similarity + keyword/metadata filtering). Each vector store implements filtering differently — pgvector uses SQL WHERE clauses, Pinecone uses its own filter syntax, Qdrant uses payload filters. The abstraction layer can hide this, but the *quality* of results may vary across backends.

4. **Testing surface area.** TMI already has the multi-database testing challenge with GORM. Adding a second axis of database variation (vector store backend) multiplies the test matrix.

5. **Operational complexity for customers.** "Choose your own vector store" means documentation, configuration, and troubleshooting for each backend.

6. **Schema/index management.** Vector stores need indexes tuned for embedding dimensions and distance metric. GORM auto-migration doesn't cover vector indexes. Each backend has its own index creation story (IVFFlat vs HNSW for pgvector, etc.).

7. **MySQL and MSSQL have no native vector support.** Customers on those databases *must* deploy a separate vector store service. "Same database for both" is only possible for Postgres and Oracle.

**Alternative raised:** Embed a lightweight vector store in the server process itself (in-process SQLite+vector extension, or Go-native HNSW library). Threat models are small — this would eliminate the separate-service requirement entirely, making deployment trivially simple, at the cost of no shared state across multiple TMI server instances.

---

## The Breakthrough: In-Memory Vector Store with Database Serialization

**Eric's idea:**

> What if we went with the in-memory store idea, BUT we serialized it to the threat model object in our normal database. When we first get a request for Timmy with a threat model, we check if we have serialized vector data. If so, we read it from the database into the vector store, and update anything that has changed since the last time. If we've never used the vector store for this threat model, we compute all the embeddings. After some period of inactivity (e.g. an hour), we purge the in-memory data. We guard carefully against memory overuse, push things out more quickly under memory pressure, and deny new Timmy sessions under severe memory pressure.

### Analysis

**What's good about this:**

- **Zero additional infrastructure.** No vector database to deploy, configure, or maintain. Timmy "just works" with whatever database you already have.
- **Naturally scoped.** One threat model = one vector index. Even a large threat model might have a few hundred entities. Fits comfortably in memory.
- **Lazy computation.** You only pay the embedding cost when someone actually uses Timmy, not on every write. Most threat models may never use it.
- **Portable.** Works with every database TMI supports since the serialized vector data is just a blob column. No pgvector, no Oracle 23ai vector search, no special extensions required.

**Concerns and things to design carefully:**

1. **Embedding computation cost at load time.** If a threat model has 200 entities and it's the first Timmy session, you need to embed all of them before the user gets a response. Depending on the embedding model (local vs API), this could be seconds to tens of seconds of cold-start delay. Need a UX strategy (progress indicator, "Timmy is reading your threat model..." state).

2. **Serialization format matters a lot.** The blob needs to store vectors + metadata + a version/hash per entity so you can detect what changed. If the format isn't forward-compatible, a TMI server upgrade could invalidate all cached embeddings. Should include a version, embedding model name, and dimension so you know when a full re-embed is required.

3. **Consistency on multi-instance deployments.** If two TMI server instances both load the same threat model's Timmy data, they each have independent in-memory copies. Edits on instance A won't be reflected in instance B's vector store until it reloads. Probably fine — Timmy sessions are per-user, and staleness is bounded by the inactivity timeout — but should be documented.

4. **Serialized blob size.** A 768-dimension float32 embedding is ~3KB per vector. 200 entities = ~600KB. Even 1000 entities at 1536 dimensions is ~6MB. Non-issue at threat-model scale.

5. **Memory pressure detection.** "Deny new sessions under severe memory pressure" is the right idea but hard to implement well in Go. `runtime.MemStats` gives you heap info, but Go's GC and OS memory reporting don't always agree. Better approach: explicit memory budget — track the allocated size of loaded vector stores, set a budget, evict LRU when approaching the budget.

6. **Write-back strategy.** When to serialize back to the database? Options:
   - On every embedding update (safest, most writes)
   - On session end / inactivity eviction (fewer writes, risk of data loss on crash)
   - Periodic dirty-flush (compromise)

   Since re-computing embeddings is the expensive part and you'd just redo it on next load anyway, write-back on eviction seems fine — a server crash just means a cold start next time.

7. **Conversation history is separate and straightforward.** Chat sessions and messages go in normal relational tables, not blob serialization. Clean and queryable.

### Enhancement: Rows Per Embedding vs. Single Blob

Instead of serializing all vector data into one blob, store embeddings as rows in a regular table (entity_id, embedding_model, vector_bytes, content_hash). This gives you:

- Incremental updates (re-embed only changed entities) without deserializing the whole blob
- Ability to query "which threat models have stale embeddings" across the system
- Standard database backup/restore without blob concerns

The in-memory HNSW index gets rebuilt from these rows on load, which is fast for hundreds of vectors.

---

## Decisions Made

1. **LLM integration:** Provider-agnostic via LangChainGo
2. **Vector store:** In-memory with database-serialized embeddings (rows-per-embedding approach preferred over single blob)
3. **Conversation storage:** Normal relational tables in the existing threat model database
4. **Memory management:** Explicit budget with LRU eviction and session admission control under pressure
5. **Scope:** One vector index per threat model, loaded on demand, evicted after inactivity
