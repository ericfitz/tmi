# Embedding-model mismatch detection and self-healing

**Date:** 2026-05-08
**Status:** Draft — pending user review
**Related issue:** TBD (file after spec approval)

## Problem

TMI persists per-entity vector embeddings in `timmy_embeddings`. Each row records the `embedding_model` (e.g. `text-embedding-3-large`) and `embedding_dim` (e.g. `3072`) used to produce its vector, but nothing in the load or query path checks that those values still match the active configuration.

When an operator switches `timmy.text_embedding_model` (or `code_embedding_model`) — for example, from a self-hosted `text-embedding-nomic-embed-text-v1.5` (768-dim) to OpenAI's `text-embedding-3-large` (3072-dim) — older embeddings remain on disk. The vector index manager loads them anyway. At query time, `cosineSimilarity` compares the 3072-dim query vector to 768-dim stored vectors, hits its `len(a) != len(b)` guard, and returns 0. Every chunk scores 0; top-K returns whichever rows happen to be first; the LLM sees a fraction of the corpus and confidently hallucinates a partial answer.

The bug was found in production: a 43-page document whose extracted text contains 26 numbered threats (T1–T26) was reduced to "3 threats" in Timmy's response because the model switch had silenced the similarity scores.

Embeddings are never intended to live forever — TMI already has automation to expire them on inactivity and on issue close. We need a third reason to invalidate them: **the embeddings disagree with the active model.**

## Goals

1. Detect at session-start (write path) and at index-load (read path) when stored embeddings' `(embedding_model, embedding_dim)` disagree with the active config.
2. Delete the offending rows surgically (per-entity).
3. Re-extract, re-chunk, and re-embed those entities transparently within the existing session-start progress flow.
4. Surface a distinct progress message ("embedding model changed — re-indexing") so the cause is visible in logs and to the user.
5. Apply uniformly to text and code index types.

## Non-goals

- No background sweeper (current detection points cover the lifecycle).
- No fallback to "use the stale embeddings anyway." Silent degradation is the bug we're fixing.
- No partial / heuristic re-embedding ("only re-embed if >50% are stale"). The existing per-entity stale path is already proportional.
- No async / background re-embed. The session waits with the existing progress channel.
- No new HTTP surface, no API or schema change. The fix is entirely in internal Timmy paths.

## Design summary

Three small additions, no new components.

1. **Embedding store:** new lightweight metadata helper `ListEntityMetadataByThreatModelAndIndexType(ctx, threatModelID, indexType) (map[entityKey]EntityEmbeddingMeta, error)` returning `(content_hash, embedding_model, embedding_dim)` per entity — *not* the vector blobs. Replaces today's "load all rows just to extract content_hash" call inside `prepareVectorIndex`.
2. **Embedding store:** new bulk pruner `DeleteEntitiesWithStaleEmbeddingMetadata(ctx, threatModelID, indexType, currentModel string, currentDim int) (int64, error)` issuing one round-trip:

   ```sql
   DELETE FROM timmy_embeddings
   WHERE threat_model_id = ?
     AND index_type      = ?
     AND (embedding_model <> ? OR embedding_dim <> ?)
   ```

   Returns affected row count; 0 is not an error.
3. **Vector index manager:** `GetOrLoadIndex` signature gains `expectedModel string`; on cache miss it scans the loaded rows and returns a typed `*ErrEmbeddingModelMismatch` if any row's `(model, dim)` disagrees. Cached-index path is unchanged (memoization remains correct because cached entries were validated at load time).
4. **Session manager:** `prepareVectorIndex` widens its per-entity staleness predicate from `hash != current` to `hash != current OR model != current OR dim != current`, and on `ErrEmbeddingModelMismatch` from `GetOrLoadIndex` it (a) logs at WARN with the mismatch fields, (b) emits a distinct progress message, (c) calls the bulk pruner, (d) calls `vectorManager.InvalidateIndex` to drop any partially-populated cache, (e) retries `GetOrLoadIndex` exactly once. A second mismatch on retry is propagated as a programming error.

## Architecture

### New types

```go
// in api/timmy_embedding_store.go

// EntityEmbeddingMeta is the per-entity tuple needed to decide whether
// existing embeddings are still usable without loading the vectors.
type EntityEmbeddingMeta struct {
    ContentHash    string
    EmbeddingModel string
    EmbeddingDim   int
}

// TimmyEmbeddingStore (interface) — new methods:
ListEntityMetadataByThreatModelAndIndexType(
    ctx context.Context, threatModelID, indexType string,
) (map[EntityKey]EntityEmbeddingMeta, error)

DeleteEntitiesWithStaleEmbeddingMetadata(
    ctx context.Context, threatModelID, indexType, currentModel string, currentDim int,
) (int64, error)
```

`EntityKey` is a small exported `{EntityType, EntityID string}` struct so callers don't redefine it. The session manager today defines an unexported `entityKey` inline (in `prepareVectorIndex`); the implementation will lift it to package scope as exported `EntityKey` and update the in-function map type.

```go
// in api/timmy_vector_manager.go

// ErrEmbeddingModelMismatch is returned by GetOrLoadIndex when stored
// embeddings disagree with the active embedding model or dimension.
// The caller is expected to delete the stale rows and re-prepare the index.
type ErrEmbeddingModelMismatch struct {
    ThreatModelID string
    IndexType     string
    StaleModel    string
    StaleDim      int
    ExpectedModel string
    ExpectedDim   int
    EntityType    string // first mismatched row, for diagnostics
    EntityID      string
}

func (e *ErrEmbeddingModelMismatch) Error() string {
    return fmt.Sprintf(
        "embedding model mismatch for tm=%s index=%s: stored %s/%d, expected %s/%d (first mismatched entity %s/%s)",
        e.ThreatModelID, e.IndexType, e.StaleModel, e.StaleDim,
        e.ExpectedModel, e.ExpectedDim, e.EntityType, e.EntityID,
    )
}
```

### Changed signatures

```go
// api/timmy_vector_manager.go
func (m *VectorIndexManager) GetOrLoadIndex(
    ctx context.Context,
    threatModelID, indexType, expectedModel string, // expectedModel is new
    expectedDim int,
) (*VectorIndex, error)
```

The cache key remains `(threatModelID, indexType)`; `expectedModel` is only used on cache miss for the validation pass. A cache hit returns the previously-loaded index unchanged. Existing callers in `api/` are updated; tests are updated. There are no external callers.

### `classifyStaleness` helper

Tiny pure function used to populate progress messages and debug logs:

```go
func classifyStaleness(present bool, meta EntityEmbeddingMeta, hash, expModel string, expDim int) string {
    switch {
    case !present:                        return "new entity"
    case meta.EmbeddingDim != expDim:     return "dimension changed"
    case meta.EmbeddingModel != expModel: return "model changed"
    case meta.ContentHash != hash:        return "content changed"
    default:                              return ""
    }
}
```

Empty return = fresh, no work needed. Order is deliberate: dimension before model because dimension is what mathematically breaks similarity and is the more diagnostic answer when both differ.

## Data flow

### Write path (`prepareVectorIndex`, session start)

```
session.Start
  └─ prepareVectorIndex(ctx, tmID, indexType, sources, progress)
       1. expectedDim   ← llmService.EmbeddingDimension(ctx, indexType)
       2. expectedModel ← config.TextEmbeddingModel  (or CodeEmbeddingModel)
       3. idx, err ← vectorManager.GetOrLoadIndex(ctx, tmID, indexType,
                                                  expectedModel, expectedDim)
          if errors.As(err, &mismatchErr):
              logger.Warn(...)
              progress("indexing", "", "", 0,
                       "embedding model changed — re-indexing")
              store.DeleteEntitiesWithStaleEmbeddingMetadata(
                  ctx, tmID, indexType, expectedModel, expectedDim)
              vectorManager.InvalidateIndex(tmID, indexType)
              idx, err ← vectorManager.GetOrLoadIndex(...)  // single retry
              if errors.As(err, &mismatchErr):
                  return fmt.Errorf("…store did not honor purge: %w", err)
       4. existingMeta ← store.ListEntityMetadataByThreatModelAndIndexType(
                              ctx, tmID, indexType)
       5. for each src in sources:
              content ← provider.Extract(ctx, ref)
              hash    ← sha256(content.Text)
              meta, present ← existingMeta[entityKey]
              reason  ← classifyStaleness(present, meta, hash,
                                          expectedModel, expectedDim)
              if reason == "": continue                 // fresh
              progress("indexing", entityType, name, pct,
                       fmt.Sprintf("re-embedding (%s)", reason))
              store.DeleteByEntity(ctx, tmID, entityType, entityID)
              // existing path: chunk → embed → CreateBatch → idx.Add
```

### Read path (`GetOrLoadIndex`, on cache miss)

```
GetOrLoadIndex(ctx, tmID, indexType, expectedModel, expectedDim)
  if cached: return cached.Index   // unchanged
  embeddings ← store.ListByThreatModelAndIndexType(ctx, tmID, indexType)
  for each emb in embeddings:
      if emb.EmbeddingModel != expectedModel
         OR emb.EmbeddingDim != expectedDim:
          return nil, &ErrEmbeddingModelMismatch{
              ThreatModelID: tmID,
              IndexType:     indexType,
              StaleModel:    emb.EmbeddingModel,
              StaleDim:      emb.EmbeddingDim,
              ExpectedModel: expectedModel,
              ExpectedDim:   expectedDim,
              EntityType:    emb.EntityType,
              EntityID:      emb.EntityID,
          }
  // …existing index build (Add each vector)…
```

The check short-circuits on the first mismatched row. The session manager handles cleanup; the manager itself does not delete.

### Concurrency

- `prepareVectorIndex` is per-session. `GetOrLoadIndex` is mutex-guarded.
- Two near-simultaneous session starts on the same threat model with stale embeddings: both observe the mismatch, both call the deleter (idempotent — second one finds 0 rows), both retry, both succeed. The first writer of new embeddings wins via the existing per-entity hash check inside the loop.
- No new locking required.

## Error handling

- `*ErrEmbeddingModelMismatch` is the only new error type, in-band only — *not* a `dberrors.Classify` case.
- Detection by `errors.As`, never by string match.
- Single-retry contract: a second mismatch after `DeleteEntitiesWithStaleEmbeddingMetadata` succeeds means the SQL didn't honor the predicate. Treated as a programming error and propagated.
- `DeleteEntitiesWithStaleEmbeddingMetadata` returning 0 deleted is normal (race lost to another session); not an error.
- All other failures (DB error from delete, embedder failure during re-embed) flow through the existing `prepareVectorIndex` error returns.
- No silent fallback: if the re-embed fails for an entity, that entity's embeddings end up empty, the existing per-entity error log fires, and Timmy's reply will reflect the missing context — same behavior as today's hash-stale failure path.

## Testing

### Unit — embedding store (`api/timmy_embedding_store_gorm_test.go`)

- `TestListEntityMetadataByThreatModelAndIndexType_ReturnsLatestPerEntity` — multi-entity, multi-chunk; helper returns one row per entity.
- `TestListEntityMetadataByThreatModelAndIndexType_ScopesToIndexType` — text + code on the same threat model; helper returns only the requested type.
- `TestDeleteEntitiesWithStaleEmbeddingMetadata_DeletesMismatchedRowsOnly` — three entities (fresh, stale model, stale dim); only the two stale ones are deleted.
- `TestDeleteEntitiesWithStaleEmbeddingMetadata_NoOpWhenAllFresh` — pinning the 0-row case.
- `TestDeleteEntitiesWithStaleEmbeddingMetadata_ScopesToIndexType` — deletes stale text rows, leaves code rows.

### Unit — vector manager (`api/timmy_vector_manager_test.go`)

- `TestGetOrLoadIndex_ReturnsErrEmbeddingModelMismatch_ForStaleModel`
- `TestGetOrLoadIndex_ReturnsErrEmbeddingModelMismatch_ForStaleDim`
- `TestGetOrLoadIndex_NoMismatch_LoadsAsBefore` — pins behavior unchanged when everything matches.
- `TestGetOrLoadIndex_CachedIndex_SkipsMismatchCheck` — second call with poisoned store returns the cached index; check only runs on miss.

### Unit — session manager (`api/timmy_session_manager_test.go`)

- `TestPrepareVectorIndex_StaleModel_DeletesAndReembeds` — fakes assert (a) `DeleteEntitiesWithStaleEmbeddingMetadata` called with `(currentModel, currentDim)`, (b) `CreateBatch` writes new rows with current model, (c) progress emits "embedding model changed — re-indexing".
- `TestPrepareVectorIndex_StaleDim_DeletesAndReembeds` — same as above for dim.
- `TestPrepareVectorIndex_PerEntityClassifyStaleness` — three entities (fresh, hash-stale, model-stale); progress messages reflect correct reason; only stale ones re-embed.
- `TestPrepareVectorIndex_RetryAfterMismatchSucceeds` — single-retry contract.
- `TestPrepareVectorIndex_DoubleMismatch_PropagatesError` — second mismatch propagates, no infinite retry.

### Helper

- `TestClassifyStaleness_AllReasons` — table-driven over `(present, hashEq, modelEq, dimEq)` → expected reason string.

### What's not tested

- No CATS / API fuzzing impact (no HTTP surface change).
- No integration-level test: all SQL is straightforward CRUD already covered by the GORM repo's existing tests; the orchestration is best tested with fakes.

## Migration / rollout

- No DB migration; both new methods use existing columns.
- First deploy with this fix will, on each session start, observe model mismatches for any historical embeddings created with a different model and re-embed them. The session start latency for the first session per threat model after deploy will increase by the cost of re-extraction + re-embedding for stale entities. After that first pass, subsequent sessions return to current latency.
- Operators who want to pre-emptively flush old embeddings can run, ahead of deploy, the equivalent SQL: `DELETE FROM timmy_embeddings WHERE embedding_model <> '<current>' OR embedding_dim <> <current>;`. This is optional and the system self-heals.

## Risks and open questions

- **Risk: a buggy operator config that names a model but mis-configures dimension.** The mismatch loop would delete and re-embed every session forever. Mitigation: the WARN log lets an operator notice; we don't attempt to silently mask it.
- **Risk: the read-path check pays O(N) cost per cache miss across all rows of the index.** N is bounded by the rows for one threat model + index type; in practice ≤ a few thousand. Acceptable.
- **No open questions** — clarifying questions resolved during brainstorming, captured here.

## References

- Production observation: document `cf1b5255-3f24-47fc-860d-e703590df72b` on threat model `e8970c41-d053-4215-a4d2-93dceaab787f` produced 28 chunks of 768-dim `nomic-embed-text-v1.5` vectors (2026-05-07) but is queried with 3072-dim `text-embedding-3-large` (2026-05-08+); cosine similarity returned 0 across the board.
- `api/timmy_session_manager.go::prepareVectorIndex` — primary hook.
- `api/timmy_vector_manager.go::GetOrLoadIndex` — secondary hook.
- `api/timmy_embedding_store.go` — interface gains two methods.
