# Embedding-model mismatch detection and self-healing — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When stored Timmy embeddings disagree with the active embedding model or dimension, detect the mismatch on both write (session start) and read (index load) paths, delete the stale rows per-entity, and re-embed transparently within the existing session-start progress flow.

**Architecture:** Two new `TimmyEmbeddingStore` methods (lightweight metadata helper + bulk pruner) + a typed `*ErrEmbeddingModelMismatch` returned by `GetOrLoadIndex` on cache miss + a widened per-entity staleness predicate in `prepareVectorIndex` that surfaces a distinct progress message. No DB migration, no API change.

**Tech Stack:** Go 1.x, GORM, PostgreSQL/Oracle ADB, existing fakes for unit tests.

**Spec:** [docs/superpowers/specs/2026-05-08-embedding-model-mismatch-detection-design.md](../specs/2026-05-08-embedding-model-mismatch-detection-design.md)

---

## File map

**Created:**
- `api/timmy_entity_key.go` — exported `EntityKey` and `EntityEmbeddingMeta` types (~25 lines)

**Modified:**
- `api/timmy_embedding_store.go` — `TimmyEmbeddingStore` interface gains two methods
- `api/timmy_embedding_store_gorm.go` — implements the two new methods
- `api/timmy_embedding_store_test.go` — new tests for the two new methods
- `api/timmy_vector_manager.go` — `ErrEmbeddingModelMismatch` type; `GetOrLoadIndex` signature gains `expectedModel`; mismatch check on cache miss
- `api/timmy_vector_manager_test.go` — update existing call sites to new signature; new mismatch tests
- `api/timmy_session_manager.go` — call new metadata helper; widen staleness predicate; handle `*ErrEmbeddingModelMismatch` in both `prepareVectorIndex` and `vectorSearch`
- `api/timmy_session_manager_test.go` — new tests for session-manager orchestration

---

## Task 1: Add shared `EntityKey` and `EntityEmbeddingMeta` types

**Files:**
- Create: `api/timmy_entity_key.go`

- [ ] **Step 1: Create the file with the new types**

```go
// api/timmy_entity_key.go
package api

// EntityKey identifies a single chunked entity within a threat model. It is
// the natural map key for "one row per entity" lookups (e.g., the freshness
// metadata used by prepareVectorIndex).
type EntityKey struct {
	EntityType string
	EntityID   string
}

// EntityEmbeddingMeta is the per-entity tuple needed to decide whether
// existing embeddings are still usable without loading the vectors.
// Hash, Model, and Dim are taken from any one row of the entity's chunks
// (they are identical across an entity's chunks by construction in
// CreateBatch).
type EntityEmbeddingMeta struct {
	ContentHash    string
	EmbeddingModel string
	EmbeddingDim   int
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `make build-server`
Expected: `bin/tmiserver built`

- [ ] **Step 3: Commit**

```bash
git add api/timmy_entity_key.go
git commit -m "feat(timmy): add EntityKey and EntityEmbeddingMeta shared types"
```

---

## Task 2: Extend `TimmyEmbeddingStore` interface

**Files:**
- Modify: `api/timmy_embedding_store.go`

- [ ] **Step 1: Add the two new methods to the interface**

Replace the existing interface block in `api/timmy_embedding_store.go` with:

```go
// TimmyEmbeddingStore defines operations for persisting vector embeddings
type TimmyEmbeddingStore interface {
	ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
	CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
	DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error)
	DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error)
	DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error)

	// ListEntityMetadataByThreatModelAndIndexType returns one EntityEmbeddingMeta
	// per entity in the (threatModelID, indexType) bucket — without loading the
	// vector blobs. Used by prepareVectorIndex to decide which entities need
	// re-embedding due to content, model, or dimension changes.
	ListEntityMetadataByThreatModelAndIndexType(
		ctx context.Context, threatModelID, indexType string,
	) (map[EntityKey]EntityEmbeddingMeta, error)

	// DeleteEntitiesWithStaleEmbeddingMetadata deletes every row in the
	// (threatModelID, indexType) bucket whose embedding_model or embedding_dim
	// disagrees with (currentModel, currentDim). Returns the number of rows
	// deleted; 0 is not an error.
	DeleteEntitiesWithStaleEmbeddingMetadata(
		ctx context.Context, threatModelID, indexType, currentModel string, currentDim int,
	) (int64, error)
}
```

- [ ] **Step 2: Build to verify it compiles (will fail until Task 3)**

Run: `make build-server`
Expected: FAIL — `*GormTimmyEmbeddingStore` does not implement `TimmyEmbeddingStore` (missing methods).

This is expected. Proceed to Task 3.

- [ ] **Step 3: Do not commit yet — interface extension and implementation must commit together**

---

## Task 3: Implement the two new methods in GORM store

**Files:**
- Modify: `api/timmy_embedding_store_gorm.go`

- [ ] **Step 1: Append the two new methods**

Add at the end of `api/timmy_embedding_store_gorm.go` (after `DeleteByThreatModelAndIndexType`):

```go
// ListEntityMetadataByThreatModelAndIndexType returns one EntityEmbeddingMeta
// per entity. See TimmyEmbeddingStore.ListEntityMetadataByThreatModelAndIndexType.
func (s *GormTimmyEmbeddingStore) ListEntityMetadataByThreatModelAndIndexType(
	ctx context.Context, threatModelID, indexType string,
) (map[EntityKey]EntityEmbeddingMeta, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing embedding metadata for threat model %s index type %s", threatModelID, indexType)

	var rows []struct {
		EntityType     string
		EntityID       string
		ContentHash    string
		EmbeddingModel string
		EmbeddingDim   int
	}
	err := s.db.WithContext(ctx).
		Table(models.TimmyEmbedding{}.TableName()).
		Select("entity_type, entity_id, content_hash, embedding_model, embedding_dim").
		Where(map[string]any{
			"threat_model_id": threatModelID,
			"index_type":      indexType,
		}).
		Find(&rows).Error
	if err != nil {
		logger.Error("Failed to list embedding metadata for threat model %s index type %s: %v",
			threatModelID, indexType, err)
		return nil, dberrors.Classify(err)
	}

	out := make(map[EntityKey]EntityEmbeddingMeta, len(rows))
	for _, r := range rows {
		k := EntityKey{EntityType: r.EntityType, EntityID: r.EntityID}
		// Multiple chunks per entity all carry the same hash/model/dim by
		// construction; last one wins is fine.
		out[k] = EntityEmbeddingMeta{
			ContentHash:    r.ContentHash,
			EmbeddingModel: r.EmbeddingModel,
			EmbeddingDim:   r.EmbeddingDim,
		}
	}
	logger.Debug("Found metadata for %d entities for threat model %s index type %s",
		len(out), threatModelID, indexType)
	return out, nil
}

// DeleteEntitiesWithStaleEmbeddingMetadata deletes rows where the stored
// embedding_model or embedding_dim disagrees with (currentModel, currentDim).
// See TimmyEmbeddingStore.DeleteEntitiesWithStaleEmbeddingMetadata.
func (s *GormTimmyEmbeddingStore) DeleteEntitiesWithStaleEmbeddingMetadata(
	ctx context.Context, threatModelID, indexType, currentModel string, currentDim int,
) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting stale-model/dim embeddings for threat model %s index type %s (current %s/%d)",
		threatModelID, indexType, currentModel, currentDim)

	var rowsAffected int64
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where("threat_model_id = ? AND index_type = ? AND (embedding_model <> ? OR embedding_dim <> ?)",
				threatModelID, indexType, currentModel, currentDim).
			Delete(&models.TimmyEmbedding{})
		if result.Error != nil {
			logger.Error("Failed to delete stale-model embeddings for %s/%s: %v",
				threatModelID, indexType, result.Error)
			return dberrors.Classify(result.Error)
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}

	logger.Debug("Deleted %d stale-model/dim embeddings for threat model %s index type %s",
		rowsAffected, threatModelID, indexType)
	return rowsAffected, nil
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 3: Commit interface + implementation together**

```bash
git add api/timmy_embedding_store.go api/timmy_embedding_store_gorm.go
git commit -m "feat(timmy): add ListEntityMetadata and DeleteStale-metadata embedding store methods"
```

---

## Task 4: Test `ListEntityMetadataByThreatModelAndIndexType`

**Files:**
- Modify: `api/timmy_embedding_store_test.go`

- [ ] **Step 1: Append the tests**

Add at the end of `api/timmy_embedding_store_test.go`:

```go
func TestTimmyEmbeddingStore_ListEntityMetadataByThreatModelAndIndexType_OneEntryPerEntity(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-meta-001"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h-a", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 1, ContentHash: "h-a", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "y", IndexType: IndexTypeText},
		{ThreatModelID: tmID, EntityType: "threat", EntityID: "t1", ChunkIndex: 0, ContentHash: "h-t", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "z", IndexType: IndexTypeText},
	}))

	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)

	assert.Len(t, meta, 2, "one entry per entity, not per chunk")
	assert.Equal(t, EntityEmbeddingMeta{ContentHash: "h-a", EmbeddingModel: "m1", EmbeddingDim: 8},
		meta[EntityKey{EntityType: "asset", EntityID: "a1"}])
	assert.Equal(t, EntityEmbeddingMeta{ContentHash: "h-t", EmbeddingModel: "m1", EmbeddingDim: 8},
		meta[EntityKey{EntityType: "threat", EntityID: "t1"}])
}

func TestTimmyEmbeddingStore_ListEntityMetadataByThreatModelAndIndexType_ScopesToIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-meta-002"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h-text", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		{ThreatModelID: tmID, EntityType: "repository", EntityID: "r1", ChunkIndex: 0, ContentHash: "h-code", EmbeddingModel: "m2", EmbeddingDim: 16, ChunkText: "y", IndexType: IndexTypeCode},
	}))

	textMeta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, textMeta, 1)
	_, hasAsset := textMeta[EntityKey{EntityType: "asset", EntityID: "a1"}]
	assert.True(t, hasAsset)

	codeMeta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Len(t, codeMeta, 1)
	_, hasRepo := codeMeta[EntityKey{EntityType: "repository", EntityID: "r1"}]
	assert.True(t, hasRepo)
}

func TestTimmyEmbeddingStore_ListEntityMetadataByThreatModelAndIndexType_EmptyReturnsEmptyMap(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, "tm-none", IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, meta)
}
```

- [ ] **Step 2: Run the new tests, expect PASS**

Run: `make test-unit name=TestTimmyEmbeddingStore_ListEntityMetadataByThreatModelAndIndexType`
Expected: 3 tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_embedding_store_test.go
git commit -m "test(timmy): pin ListEntityMetadataByThreatModelAndIndexType behavior"
```

---

## Task 5: Test `DeleteEntitiesWithStaleEmbeddingMetadata`

**Files:**
- Modify: `api/timmy_embedding_store_test.go`

- [ ] **Step 1: Append the tests**

Add at the end of `api/timmy_embedding_store_test.go`:

```go
func TestTimmyEmbeddingStore_DeleteEntitiesWithStaleEmbeddingMetadata_DeletesOnlyMismatchedRows(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-stale-001"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		// fresh: matches current (m-current/8)
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "fresh", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale model
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "stale-model", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale dim
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "stale-dim", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 16, ChunkText: "x", IndexType: IndexTypeText},
	}))

	deleted, err := store.DeleteEntitiesWithStaleEmbeddingMetadata(ctx, tmID, IndexTypeText, "m-current", 8)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

	// Only the fresh entity remains.
	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, meta, 1)
	_, ok := meta[EntityKey{EntityType: "asset", EntityID: "fresh"}]
	assert.True(t, ok)
}

func TestTimmyEmbeddingStore_DeleteEntitiesWithStaleEmbeddingMetadata_NoOpWhenAllFresh(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-stale-002"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
	}))

	deleted, err := store.DeleteEntitiesWithStaleEmbeddingMetadata(ctx, tmID, IndexTypeText, "m-current", 8)
	require.NoError(t, err)
	assert.Equal(t, int64(0), deleted)

	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, meta, 1)
}

func TestTimmyEmbeddingStore_DeleteEntitiesWithStaleEmbeddingMetadata_ScopesToIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-stale-003"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		// stale text
		{ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale code (different model on the code side, must NOT be deleted by a text-side call)
		{ThreatModelID: tmID, EntityType: "repository", EntityID: "r1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old-code", EmbeddingDim: 16, ChunkText: "y", IndexType: IndexTypeCode},
	}))

	deleted, err := store.DeleteEntitiesWithStaleEmbeddingMetadata(ctx, tmID, IndexTypeText, "m-current", 8)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted, "only the text-side stale row should be deleted")

	textMeta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, textMeta)

	codeMeta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Len(t, codeMeta, 1, "code-side row must survive a text-side prune")
}
```

- [ ] **Step 2: Run the new tests, expect PASS**

Run: `make test-unit name=TestTimmyEmbeddingStore_DeleteEntitiesWithStaleEmbeddingMetadata`
Expected: 3 tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_embedding_store_test.go
git commit -m "test(timmy): pin DeleteEntitiesWithStaleEmbeddingMetadata behavior"
```

---

## Task 6: Add `ErrEmbeddingModelMismatch` and extend `GetOrLoadIndex`

**Files:**
- Modify: `api/timmy_vector_manager.go`

- [ ] **Step 1: Add the error type**

Add at the top of `api/timmy_vector_manager.go` (just below the imports, before `LoadedIndex`):

```go
// ErrEmbeddingModelMismatch is returned by GetOrLoadIndex when at least one
// stored embedding row in the (threat_model_id, index_type) bucket disagrees
// with the active embedding model or dimension. The caller is expected to
// delete the stale rows and re-prepare the index.
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

- [ ] **Step 2: Change `GetOrLoadIndex` signature and body**

Replace the existing `GetOrLoadIndex` method in `api/timmy_vector_manager.go` (around line 56) with:

```go
// GetOrLoadIndex returns the index for a threat model and index type, loading
// from DB if needed. expectedModel + dimension are the active embedding model
// and dimension; if any loaded row disagrees, *ErrEmbeddingModelMismatch is
// returned and the caller is responsible for cleanup.
func (m *VectorIndexManager) GetOrLoadIndex(
	ctx context.Context,
	threatModelID, indexType, expectedModel string,
	dimension int,
) (*VectorIndex, error) {
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

	// Pre-flight: refuse to load any row whose embedding_model or
	// embedding_dim disagrees with the active config. The caller deletes
	// stale rows and retries; we do not delete here.
	for _, emb := range embeddings {
		if emb.EmbeddingModel != expectedModel || emb.EmbeddingDim != dimension {
			return nil, &ErrEmbeddingModelMismatch{
				ThreatModelID: threatModelID,
				IndexType:     indexType,
				StaleModel:    emb.EmbeddingModel,
				StaleDim:      emb.EmbeddingDim,
				ExpectedModel: expectedModel,
				ExpectedDim:   dimension,
				EntityType:    emb.EntityType,
				EntityID:      emb.EntityID,
			}
		}
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

	slogging.Get().Debug("Loaded vector index for threat model %s index type %s: %d vectors, %d bytes",
		threatModelID, indexType, idx.Count(), loaded.MemoryBytes)
	return idx, nil
}
```

- [ ] **Step 3: Build — production callers will fail compilation, that's expected**

Run: `make build-server`
Expected: FAIL on `api/timmy_session_manager.go` (two `GetOrLoadIndex` calls with old signature).

This is expected. Proceed to Task 7.

- [ ] **Step 4: Do not commit yet — signature and callers must commit together**

---

## Task 7: Update production callers in `timmy_session_manager.go` (signature only — full handling in Task 9)

**Files:**
- Modify: `api/timmy_session_manager.go:545`, `api/timmy_session_manager.go:688`

- [ ] **Step 1: Update `prepareVectorIndex` call site**

Find the call around line 545 in `api/timmy_session_manager.go`:

```go
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, dim)
```

Replace with (we resolve `expectedModel` two lines up so the rest of the function can also use it):

```go
expectedModel := sm.config.TextEmbeddingModel
if indexType == IndexTypeCode {
	expectedModel = sm.config.CodeEmbeddingModel
}
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
```

- [ ] **Step 2: Update `vectorSearch` call site**

Find the call around line 688 in `api/timmy_session_manager.go`:

```go
dim := len(vectors[0])
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, dim)
```

Replace with:

```go
dim := len(vectors[0])
expectedModel := sm.config.TextEmbeddingModel
if indexType == IndexTypeCode {
	expectedModel = sm.config.CodeEmbeddingModel
}
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
```

- [ ] **Step 3: Build to verify production compiles (tests will still fail until Task 8)**

Run: `make build-server`
Expected: build succeeds. (Test packages will not compile until Task 8.)

- [ ] **Step 4: Do not commit yet — tests must compile first**

---

## Task 8: Update existing `GetOrLoadIndex` test call sites to new signature

**Files:**
- Modify: `api/timmy_vector_manager_test.go`

- [ ] **Step 1: Update every existing call to pass `expectedModel`**

There are roughly 18 existing `GetOrLoadIndex(ctx, tmID, indexType, dim)` calls. Each one currently uses `EmbeddingModel: "test-model"` via `makeTestEmbedding`. Update every call to pass `"test-model"` as the new third argument:

```go
mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
```

becomes

```go
mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, "test-model", dim)
```

For `IndexTypeCode` calls, also use `"test-model"` (the helper sets the same model name for both index types). Apply this mechanical replacement to every existing call site — there are no exceptions.

After the replacement, run `make build-server` to confirm test packages compile.

- [ ] **Step 2: Build and run existing tests, expect PASS**

Run: `make build-server && make test-unit name=TestVectorIndexManager`
Expected: all existing `TestVectorIndexManager_*` tests pass (the signature change is a no-op for matching-model rows).

- [ ] **Step 3: Commit signature change + production update + test sweep together**

```bash
git add api/timmy_vector_manager.go api/timmy_session_manager.go api/timmy_vector_manager_test.go
git commit -m "feat(timmy): GetOrLoadIndex returns ErrEmbeddingModelMismatch on stale model/dim"
```

---

## Task 9: Add mismatch tests for `GetOrLoadIndex`

**Files:**
- Modify: `api/timmy_vector_manager_test.go`

- [ ] **Step 1: Append the new tests**

Add at the end of `api/timmy_vector_manager_test.go`:

```go
func TestVectorIndexManager_GetOrLoadIndex_ReturnsMismatchOnStaleModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-mismatch-model"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "a1", 0, []float32{1, 0, 0}, "x", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	idx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, "different-model", 3)
	require.Error(t, err)
	assert.Nil(t, idx)

	var mismatch *ErrEmbeddingModelMismatch
	require.True(t, errors.As(err, &mismatch))
	assert.Equal(t, tmID, mismatch.ThreatModelID)
	assert.Equal(t, IndexTypeText, mismatch.IndexType)
	assert.Equal(t, "test-model", mismatch.StaleModel)
	assert.Equal(t, 3, mismatch.StaleDim)
	assert.Equal(t, "different-model", mismatch.ExpectedModel)
	assert.Equal(t, 3, mismatch.ExpectedDim)
	assert.Equal(t, "asset", mismatch.EntityType)
	assert.Equal(t, "a1", mismatch.EntityID)
}

func TestVectorIndexManager_GetOrLoadIndex_ReturnsMismatchOnStaleDim(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-mismatch-dim"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "a1", 0, []float32{1, 0, 0}, "x", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	idx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, "test-model", 7) // 3 stored, 7 expected
	require.Error(t, err)
	assert.Nil(t, idx)

	var mismatch *ErrEmbeddingModelMismatch
	require.True(t, errors.As(err, &mismatch))
	assert.Equal(t, 3, mismatch.StaleDim)
	assert.Equal(t, 7, mismatch.ExpectedDim)
}

func TestVectorIndexManager_GetOrLoadIndex_CachedIndexSkipsMismatchCheck(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-cached"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "a1", 0, []float32{1, 0, 0}, "x", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	// First load — caches the index.
	idx1, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, "test-model", 3)
	require.NoError(t, err)
	require.NotNil(t, idx1)

	// Now poison the store with a row of a different model. The cache hit
	// should NOT re-validate — the mismatch check only runs on cache miss.
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "threat", "t1", 0, []float32{0, 1, 0}, "y", IndexTypeText),
	}))
	idx2, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, "test-model", 3)
	require.NoError(t, err)
	assert.Same(t, idx1, idx2, "cached index returned without re-validation")
}
```

Add `"errors"` to the imports if not already present.

- [ ] **Step 2: Run the new tests, expect PASS**

Run: `make test-unit name=TestVectorIndexManager_GetOrLoadIndex_Returns`
Expected: 2 tests pass.

Run: `make test-unit name=TestVectorIndexManager_GetOrLoadIndex_CachedIndex`
Expected: 1 test passes.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_vector_manager_test.go
git commit -m "test(timmy): pin GetOrLoadIndex mismatch detection on cache miss"
```

---

## Task 10: Add `classifyStaleness` helper

**Files:**
- Modify: `api/timmy_session_manager.go`

- [ ] **Step 1: Add the helper near the existing helpers**

Add inside `api/timmy_session_manager.go`, just below the `splitSourcesByIndexType` helper (around line 521):

```go
// classifyStaleness returns a short reason describing why an entity's
// embeddings are stale (or "" when fresh). Used to populate progress
// messages and debug logs. Order is deliberate: dimension before model
// because dimension is what mathematically breaks similarity, and is the
// more diagnostic answer when both differ.
func classifyStaleness(present bool, meta EntityEmbeddingMeta, hash, expModel string, expDim int) string {
	switch {
	case !present:
		return "new entity"
	case meta.EmbeddingDim != expDim:
		return "dimension changed"
	case meta.EmbeddingModel != expModel:
		return "model changed"
	case meta.ContentHash != hash:
		return "content changed"
	default:
		return ""
	}
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_session_manager.go
git commit -m "feat(timmy): add classifyStaleness helper for re-embed reasons"
```

---

## Task 11: Test `classifyStaleness`

**Files:**
- Modify: `api/timmy_session_manager_test.go`

- [ ] **Step 1: Append a table-driven test**

Add at the end of `api/timmy_session_manager_test.go`:

```go
func TestClassifyStaleness_AllReasons(t *testing.T) {
	const expModel = "m-current"
	const expDim = 8
	const hashCurr = "h-current"
	freshMeta := EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: expModel, EmbeddingDim: expDim}

	tests := []struct {
		name     string
		present  bool
		meta     EntityEmbeddingMeta
		hash     string
		expected string
	}{
		{name: "fresh", present: true, meta: freshMeta, hash: hashCurr, expected: ""},
		{name: "new entity", present: false, meta: EntityEmbeddingMeta{}, hash: hashCurr, expected: "new entity"},
		{name: "dim changed", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: expModel, EmbeddingDim: 16}, hash: hashCurr, expected: "dimension changed"},
		{name: "model changed", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: "m-old", EmbeddingDim: expDim}, hash: hashCurr, expected: "model changed"},
		{name: "content changed", present: true, meta: freshMeta, hash: "h-new", expected: "content changed"},
		// dim takes precedence over model (when both differ)
		{name: "dim+model both differ -> dim wins", present: true, meta: EntityEmbeddingMeta{ContentHash: hashCurr, EmbeddingModel: "m-old", EmbeddingDim: 16}, hash: hashCurr, expected: "dimension changed"},
		// model takes precedence over content (when both differ)
		{name: "model+content both differ -> model wins", present: true, meta: EntityEmbeddingMeta{ContentHash: "h-different", EmbeddingModel: "m-old", EmbeddingDim: expDim}, hash: hashCurr, expected: "model changed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyStaleness(tt.present, tt.meta, tt.hash, expModel, expDim)
			assert.Equal(t, tt.expected, got)
		})
	}
}
```

- [ ] **Step 2: Run the new test, expect PASS**

Run: `make test-unit name=TestClassifyStaleness_AllReasons`
Expected: 7 sub-tests pass.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_session_manager_test.go
git commit -m "test(timmy): pin classifyStaleness reason ordering"
```

---

## Task 12: Wire `prepareVectorIndex` to the new metadata helper, widened predicate, and mismatch retry

**Files:**
- Modify: `api/timmy_session_manager.go:526-668`

- [ ] **Step 1: Replace the body of `prepareVectorIndex`**

Replace the entire `prepareVectorIndex` function in `api/timmy_session_manager.go` with:

```go
// prepareVectorIndex ensures the vector index is loaded and up-to-date for
// the threat model. For each source entity, it checks cached metadata
// (content_hash + embedding_model + embedding_dim) against the active
// embedder, and re-embeds stale or new content. If the in-memory index
// cannot be loaded because stored embeddings disagree with the active model
// or dimension, the stale rows are pruned and the load is retried once.
func (sm *TimmySessionManager) prepareVectorIndex(
	ctx context.Context,
	threatModelID, indexType string,
	sources []SourceSnapshotEntry,
	progress SessionProgressCallback,
) error {
	logger := slogging.Get()

	if progress != nil {
		progress("indexing", "", "", 0, "loading vector index")
	}

	// Determine embedding dimension and the expected model name for this index type.
	dim, err := sm.llmService.EmbeddingDimension(ctx, indexType)
	if err != nil {
		return fmt.Errorf("failed to determine embedding dimension: %w", err)
	}
	expectedModel := sm.config.TextEmbeddingModel
	if indexType == IndexTypeCode {
		expectedModel = sm.config.CodeEmbeddingModel
	}

	// Get or load the index. If stored rows disagree with (expectedModel, dim),
	// purge the stale rows and retry once.
	idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
	var mismatch *ErrEmbeddingModelMismatch
	if errors.As(err, &mismatch) {
		logger.Warn("Embedding model mismatch for tm=%s index=%s (stored %s/%d, expected %s/%d) — purging stale rows",
			threatModelID, indexType, mismatch.StaleModel, mismatch.StaleDim,
			expectedModel, dim)
		if progress != nil {
			progress("indexing", "", "", 0, "embedding model changed — re-indexing")
		}
		if _, perr := GlobalTimmyEmbeddingStore.DeleteEntitiesWithStaleEmbeddingMetadata(
			ctx, threatModelID, indexType, expectedModel, dim,
		); perr != nil {
			return fmt.Errorf("purge stale embeddings: %w", perr)
		}
		sm.vectorManager.InvalidateIndex(threatModelID, indexType)
		idx, err = sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
		if errors.As(err, &mismatch) {
			return fmt.Errorf("embedding store did not honor purge: %w", err)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to load vector index: %w", err)
	}

	// Load existing per-entity metadata (hash + model + dim) — not vectors.
	existingMeta, err := GlobalTimmyEmbeddingStore.ListEntityMetadataByThreatModelAndIndexType(ctx, threatModelID, indexType)
	if err != nil {
		return fmt.Errorf("failed to load embedding metadata: %w", err)
	}

	total := len(sources)
	for i, src := range sources {
		if progress != nil {
			pct := 0
			if total > 0 {
				pct = (i * 100) / total
			}
			progress("indexing", src.EntityType, src.Name, pct, "processing")
		}

		// Extract content
		ref := EntityReference{
			EntityType: src.EntityType,
			EntityID:   src.EntityID,
			Name:       src.Name,
			URI:        src.URI,
		}
		content, err := sm.providerRegistry.Extract(ctx, ref)
		if err != nil {
			logger.Warn("Failed to extract content for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}
		if content.Text == "" {
			continue
		}

		hash := fmt.Sprintf("%x", sha256.Sum256([]byte(content.Text)))
		key := EntityKey{EntityType: src.EntityType, EntityID: src.EntityID}
		meta, present := existingMeta[key]

		reason := classifyStaleness(present, meta, hash, expectedModel, dim)
		if reason == "" {
			// Fresh — embeddings still valid.
			continue
		}

		logger.Debug("Re-embedding %s/%s (%s)", src.EntityType, src.EntityID, reason)
		if progress != nil {
			pct := 0
			if total > 0 {
				pct = (i * 100) / total
			}
			progress("indexing", src.EntityType, src.Name, pct, fmt.Sprintf("re-embedding (%s)", reason))
		}

		// Delete old embeddings for this entity.
		if _, err := GlobalTimmyEmbeddingStore.DeleteByEntity(ctx, threatModelID, src.EntityType, src.EntityID); err != nil {
			logger.Warn("Failed to delete old embeddings for %s/%s: %v", src.EntityType, src.EntityID, err)
		}

		// Chunk the content.
		chunks := sm.chunker.Chunk(content.Text)
		if len(chunks) == 0 {
			continue
		}

		// Embed all chunks.
		vectors, err := sm.llmService.EmbedTexts(ctx, chunks, indexType)
		if err != nil {
			logger.Warn("Failed to embed chunks for %s/%s: %v", src.EntityType, src.EntityID, err)
			continue
		}

		// Persist embeddings and add to in-memory index.
		var embeddingRecords []models.TimmyEmbedding
		for j, chunk := range chunks {
			if j >= len(vectors) {
				break
			}
			emb := models.TimmyEmbedding{
				ThreatModelID:  threatModelID,
				EntityType:     src.EntityType,
				EntityID:       src.EntityID,
				ChunkIndex:     j,
				ContentHash:    hash,
				IndexType:      indexType,
				EmbeddingModel: expectedModel,
				EmbeddingDim:   len(vectors[j]),
				VectorData:     float32ToBytes(vectors[j]),
				ChunkText:      models.DBText(chunk),
			}
			embeddingRecords = append(embeddingRecords, emb)

			entryID := fmt.Sprintf("%s:%s:%d", src.EntityType, src.EntityID, j)
			idx.Add(entryID, vectors[j], chunk)
		}

		if len(embeddingRecords) > 0 {
			if err := GlobalTimmyEmbeddingStore.CreateBatch(ctx, embeddingRecords); err != nil {
				logger.Warn("Failed to persist embeddings for %s/%s: %v", src.EntityType, src.EntityID, err)
			}
		}
	}

	if progress != nil {
		progress("indexing", "", "", 100, "vector index ready")
	}

	return nil
}
```

Note: this replaces the previous body that re-derived `embeddingModel` inside the loop and used `existingHashes`. The previous unexported `entityKey` struct and `existingHashes` map are gone.

Add `"errors"` to the imports if not already present.

- [ ] **Step 2: Build to verify**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 3: Run existing session-manager tests, expect PASS**

Run: `make test-unit name=TestTimmySessionManager`
Expected: all existing tests pass (the staleness predicate widened but matches the previous behavior on hash-equal/fresh paths).

- [ ] **Step 4: Commit**

```bash
git add api/timmy_session_manager.go
git commit -m "feat(timmy): widen prepareVectorIndex staleness check to model+dim and self-heal on mismatch"
```

---

## Task 13: Make `vectorSearch` resilient to mid-session mismatch

**Files:**
- Modify: `api/timmy_session_manager.go:687-696`

When a query path runs after `prepareVectorIndex`, a `*ErrEmbeddingModelMismatch` is unexpected (the prepare step would have healed it). But the existing search code returns `nil` on any error and logs a warning — we want to make the warning specifically diagnose the mismatch case so it doesn't get lost in noise. We do **not** retry from the search path: the session has already paid for prepare; if a mid-session mismatch happens (e.g., manual DB tampering), failing the query is the right answer.

- [ ] **Step 1: Update `vectorSearch` to log mismatch distinctly**

Find the block in `api/timmy_session_manager.go` around line 688:

```go
dim := len(vectors[0])
expectedModel := sm.config.TextEmbeddingModel
if indexType == IndexTypeCode {
	expectedModel = sm.config.CodeEmbeddingModel
}
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
if err != nil {
	logger.Warn("Failed to get %s vector index for search: %v", indexType, err)
	return nil
}
```

Replace with:

```go
dim := len(vectors[0])
expectedModel := sm.config.TextEmbeddingModel
if indexType == IndexTypeCode {
	expectedModel = sm.config.CodeEmbeddingModel
}
idx, err := sm.vectorManager.GetOrLoadIndex(ctx, threatModelID, indexType, expectedModel, dim)
if err != nil {
	var mismatch *ErrEmbeddingModelMismatch
	if errors.As(err, &mismatch) {
		logger.Warn("Embedding model mismatch during search for tm=%s index=%s (stored %s/%d, expected %s/%d) — session was not properly prepared; failing query",
			threatModelID, indexType, mismatch.StaleModel, mismatch.StaleDim,
			expectedModel, dim)
	} else {
		logger.Warn("Failed to get %s vector index for search: %v", indexType, err)
	}
	return nil
}
```

- [ ] **Step 2: Build to verify**

Run: `make build-server`
Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_session_manager.go
git commit -m "feat(timmy): distinct log for mid-session embedding model mismatch in vectorSearch"
```

---

## Task 14: Integration-style test for `prepareVectorIndex` self-healing on stale model

**Files:**
- Modify: `api/timmy_session_manager_test.go`

We test `prepareVectorIndex` end-to-end against a real `GormTimmyEmbeddingStore` (sqlite via `setupTimmyTestDB`) and a fake `llmService` so we can pin embed-call counts. This is the most important behavioral test for the feature.

- [ ] **Step 1: Locate or create a fake LLM service / chunker setup used by other session-manager tests**

Run: `rg -n "func.*EmbedTexts" api/ --type go | head` — confirm whether an existing fake exists. Pattern-match on whichever fake the existing `TestTimmySessionManager_*` tests use. If none uses one, see the next step for a minimal one to drop in.

- [ ] **Step 2: Add a minimal in-test fake `llmService` if one is not already in scope**

If the test file doesn't already define one, add at the top of `api/timmy_session_manager_test.go` (after imports):

```go
type fakeEmbedderForReembed struct {
	dim         int
	embedCalls  int
	dimCalls    int
	chunksSeen  []string
}

func (f *fakeEmbedderForReembed) EmbedTexts(_ context.Context, chunks []string, _ string) ([][]float32, error) {
	f.embedCalls++
	f.chunksSeen = append(f.chunksSeen, chunks...)
	out := make([][]float32, len(chunks))
	for i := range chunks {
		v := make([]float32, f.dim)
		v[0] = 1
		out[i] = v
	}
	return out, nil
}
func (f *fakeEmbedderForReembed) EmbeddingDimension(_ context.Context, _ string) (int, error) {
	f.dimCalls++
	return f.dim, nil
}
// (Add no-op stubs for any other methods on TimmyLLMService that the
// session manager calls during prepareVectorIndex; copy the shape from
// any pre-existing fake in this test file.)
```

If the existing fake is sufficient (it usually is), skip this step.

- [ ] **Step 3: Add the stale-model self-heal test**

Add at the end of `api/timmy_session_manager_test.go`:

```go
func TestPrepareVectorIndex_StaleModel_SelfHeals(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	GlobalTimmyEmbeddingStore = store
	t.Cleanup(func() { GlobalTimmyEmbeddingStore = nil })

	ctx := context.Background()
	tmID := "tm-selfheal-001"

	// Seed stale-model embeddings for one entity.
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "a1", 0, []float32{1, 0, 0}, "old chunk", IndexTypeText),
	}))

	// Build session manager with active model = "current-model", dim 3.
	embedder := &fakeEmbedderForReembed{dim: 3}
	chunker := NewSimpleChunker(/* parameters per existing helper, e.g. */ 256, 0)
	mgr := NewVectorIndexManager(store, 512, 300)

	sm := &TimmySessionManager{
		config: config.TimmyConfig{
			TextEmbeddingModel: "current-model",
		},
		llmService:       embedder,
		vectorManager:    mgr,
		chunker:          chunker,
		providerRegistry: stubProviderRegistry(t, "asset", "a1", "fresh content"),
	}

	var progressMessages []string
	progress := func(_ string, _ string, _ string, _ int, msg string) {
		progressMessages = append(progressMessages, msg)
	}

	err := sm.prepareVectorIndex(ctx, tmID, IndexTypeText,
		[]SourceSnapshotEntry{{EntityType: "asset", EntityID: "a1", Name: "Asset 1"}},
		progress)
	require.NoError(t, err)

	// Expectations:
	// 1. The "embedding model changed — re-indexing" message was emitted.
	assert.Contains(t, progressMessages, "embedding model changed — re-indexing")

	// 2. Stored rows now use the active model.
	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, meta, 1)
	assert.Equal(t, "current-model", meta[EntityKey{EntityType: "asset", EntityID: "a1"}].EmbeddingModel)

	// 3. The embedder was invoked exactly once for the re-embed.
	assert.Equal(t, 1, embedder.embedCalls)
}
```

`stubProviderRegistry` is a helper to build a `ContentProviderRegistry` (or whatever the session manager actually depends on for `Extract`) that returns `"fresh content"` for the named entity. Use whatever pattern existing session-manager tests already use; if none exists, the simplest path is to construct a real `ContentProviderRegistry` and register a fake `ContentProvider` for the asset entity that returns the canned text.

- [ ] **Step 4: Run the new test, expect PASS**

Run: `make test-unit name=TestPrepareVectorIndex_StaleModel_SelfHeals`
Expected: 1 test passes.

- [ ] **Step 5: Commit**

```bash
git add api/timmy_session_manager_test.go
git commit -m "test(timmy): pin prepareVectorIndex self-heal on stale embedding model"
```

---

## Task 15: Test stale-dim self-heal and fresh no-op paths

**Files:**
- Modify: `api/timmy_session_manager_test.go`

- [ ] **Step 1: Append two more tests covering dim-mismatch and the fresh path**

```go
func TestPrepareVectorIndex_StaleDim_SelfHeals(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	GlobalTimmyEmbeddingStore = store
	t.Cleanup(func() { GlobalTimmyEmbeddingStore = nil })

	ctx := context.Background()
	tmID := "tm-selfheal-002"

	// Seed embeddings of dim 3 — but the active embedder will report dim 5.
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "a1", 0, []float32{1, 0, 0}, "old", IndexTypeText),
	}))

	embedder := &fakeEmbedderForReembed{dim: 5}
	mgr := NewVectorIndexManager(store, 512, 300)
	sm := &TimmySessionManager{
		config: config.TimmyConfig{TextEmbeddingModel: "test-model"},
		llmService: embedder,
		vectorManager: mgr,
		chunker: NewSimpleChunker(256, 0),
		providerRegistry: stubProviderRegistry(t, "asset", "a1", "fresh content"),
	}

	err := sm.prepareVectorIndex(ctx, tmID, IndexTypeText,
		[]SourceSnapshotEntry{{EntityType: "asset", EntityID: "a1", Name: "Asset 1"}},
		nil)
	require.NoError(t, err)

	meta, err := store.ListEntityMetadataByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, meta, 1)
	assert.Equal(t, 5, meta[EntityKey{EntityType: "asset", EntityID: "a1"}].EmbeddingDim)
}

func TestPrepareVectorIndex_AllFresh_NoReembed(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	GlobalTimmyEmbeddingStore = store
	t.Cleanup(func() { GlobalTimmyEmbeddingStore = nil })

	ctx := context.Background()
	tmID := "tm-selfheal-003"

	// Seed embeddings produced by the active embedder.
	freshHash := fmt.Sprintf("%x", sha256.Sum256([]byte("fresh content")))
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		{
			ThreatModelID: tmID, EntityType: "asset", EntityID: "a1", ChunkIndex: 0,
			ContentHash: freshHash, EmbeddingModel: "test-model", EmbeddingDim: 3,
			VectorData: float32ToBytes([]float32{1, 0, 0}), ChunkText: "fresh content",
			IndexType: IndexTypeText,
		},
	}))

	embedder := &fakeEmbedderForReembed{dim: 3}
	mgr := NewVectorIndexManager(store, 512, 300)
	sm := &TimmySessionManager{
		config: config.TimmyConfig{TextEmbeddingModel: "test-model"},
		llmService: embedder,
		vectorManager: mgr,
		chunker: NewSimpleChunker(256, 0),
		providerRegistry: stubProviderRegistry(t, "asset", "a1", "fresh content"),
	}

	err := sm.prepareVectorIndex(ctx, tmID, IndexTypeText,
		[]SourceSnapshotEntry{{EntityType: "asset", EntityID: "a1", Name: "Asset 1"}},
		nil)
	require.NoError(t, err)

	// embedder.EmbedTexts MUST NOT have been called — entity was fresh.
	assert.Equal(t, 0, embedder.embedCalls)
}
```

- [ ] **Step 2: Run the new tests, expect PASS**

Run: `make test-unit name=TestPrepareVectorIndex_StaleDim_SelfHeals`
Run: `make test-unit name=TestPrepareVectorIndex_AllFresh_NoReembed`
Expected: both pass.

- [ ] **Step 3: Commit**

```bash
git add api/timmy_session_manager_test.go
git commit -m "test(timmy): pin prepareVectorIndex stale-dim heal and fresh no-op paths"
```

---

## Task 16: End-to-end self-review — lint, build, full test sweep

**Files:** none (tooling only)

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: `Lint passed`. Fix any issues inline (most likely `goimports` on the test file).

- [ ] **Step 2: Build**

Run: `make build-server`
Expected: `Server binary built: bin/tmiserver`.

- [ ] **Step 3: Full unit test suite**

Run: `make test-unit`
Expected: all packages pass; total test count increases by ~14 (3 metadata, 3 stale-meta, 3 mismatch, 1 cached, 1 helper, 3 self-heal session tests).

- [ ] **Step 4: Confirm the original-bug repro is now self-healing (manual verification — optional)**

Optional: against the dev DB,

```bash
docker exec tmi-postgresql psql -U tmi_dev -d tmi_dev -c "SELECT embedding_model, embedding_dim, COUNT(*) FROM timmy_embeddings GROUP BY 1,2;"
```

Restart the server (`make stop-server && make start-dev`), open a Timmy session against threat model `e8970c41-d053-4215-a4d2-93dceaab787f`, and confirm:
- Server log emits `"Embedding model mismatch for tm=e8970c41-… — purging stale rows"`.
- The session emits the `"embedding model changed — re-indexing"` progress message.
- After re-embed, `SELECT embedding_model, COUNT(*) ...` shows only `text-embedding-3-large` rows.
- Asking Timmy about the document returns 26 threats, not 3.

- [ ] **Step 5: Dispatch oracle-db-admin subagent for the SQL change**

Per CLAUDE.md, any DB-touching change must be reviewed before completion. The new `DELETE … WHERE … (embedding_model <> ? OR embedding_dim <> ?)` is the load-bearing piece.

Dispatch the `oracle-db-admin` subagent with a short prompt summarizing:
- The new method signature `DeleteEntitiesWithStaleEmbeddingMetadata`.
- The SQL form (parameterized, against `timmy_embeddings`, with two `<>` predicates inside an OR).
- The new metadata helper which is a `SELECT entity_type, entity_id, content_hash, embedding_model, embedding_dim WHERE threat_model_id = ? AND index_type = ?`.
- That no migration, no FK change, no new index.

Address every BLOCKING finding before proceeding; fold APPROVED WITH NOTES items into a follow-up issue if non-trivial.

- [ ] **Step 6: Run security-regression scan**

Per CLAUDE.md, before any commit completes for a security-adjacent area:

Run the `security-regression` skill against the change set. Expected verdict: PASS (no SSRF, no auth, no PATCH, no error-classification, no redirect surface).

- [ ] **Step 7: Final commit and push**

After all checks pass, the prior task commits represent the full implementation. Push:

```bash
git pull --rebase
git push
git status   # MUST show "up to date with origin"
```

- [ ] **Step 8: File and close the implementation issue**

```bash
gh issue create --title "feat(timmy): detect embedding-model mismatch and self-heal" \
  --body "Implements the design at docs/superpowers/specs/2026-05-08-embedding-model-mismatch-detection-design.md. Closed by [tag commit ref]."
```

After push, close the issue from the commit body or by hand if not auto-closed.

---

## Self-review checklist

**Spec coverage:**

| Spec section | Implementing task |
|---|---|
| `EntityKey` and `EntityEmbeddingMeta` types | Task 1 |
| Interface gains `ListEntityMetadataByThreatModelAndIndexType` | Task 2, 3, 4 |
| Interface gains `DeleteEntitiesWithStaleEmbeddingMetadata` | Task 2, 3, 5 |
| `*ErrEmbeddingModelMismatch` returned by `GetOrLoadIndex` | Task 6, 9 |
| `GetOrLoadIndex` signature gains `expectedModel` | Task 6, 7, 8 |
| `prepareVectorIndex` widened staleness predicate | Task 12, 14, 15 |
| Distinct progress message ("embedding model changed — re-indexing") | Task 12, 14 |
| `vectorSearch` logs distinctly on mid-session mismatch | Task 13 |
| `classifyStaleness` helper | Task 10, 11 |
| Single-retry contract / propagate on second mismatch | Task 12 (the second `errors.As` returns the wrapped error) |
| Cached-index path skips mismatch check | Task 9 (`TestVectorIndexManager_GetOrLoadIndex_CachedIndexSkipsMismatchCheck`) |
| Per-entity scope (not whole TM) | Task 12 (`DeleteByEntity` per stale entity inside the loop) |
| Both text and code index types | All tasks treat `indexType` symmetrically |

No spec section is unimplemented.

**Type consistency:**

- `EntityKey.EntityType` / `EntityID` used identically across Tasks 1, 4, 5, 12, 14, 15.
- `EntityEmbeddingMeta.{ContentHash, EmbeddingModel, EmbeddingDim}` used identically across Tasks 1, 4, 12.
- `ErrEmbeddingModelMismatch.{StaleModel, StaleDim, ExpectedModel, ExpectedDim}` used identically in Tasks 6, 9, 12, 13.
- `DeleteEntitiesWithStaleEmbeddingMetadata` signature `(ctx, tmID, indexType, currentModel, currentDim)` used identically in Tasks 2, 3, 5, 12.
- `GetOrLoadIndex` new signature `(ctx, tmID, indexType, expectedModel, dim)` used identically in Tasks 6, 7, 8, 9, 12, 13.

**Placeholder scan:** clean — no TBD, no "fill in", no "similar to". Two tasks (14 & 15) reference `stubProviderRegistry` and an embedder fake whose exact shape depends on existing test patterns in the file; that's a documented inspection step, not a placeholder.
