package api

import (
	"context"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestVectorIndexManager_LoadIndex(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-001"
	dim := 3

	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}

	embeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec1, "Asset chunk one", IndexTypeText),
		makeTestEmbedding(tmID, "threat", "threat-001", 0, vec2, "Threat chunk one", IndexTypeText),
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	mgr := NewVectorIndexManager(store, 512, 300)

	idx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	require.NotNil(t, idx)

	// Two vectors should have been loaded
	assert.Equal(t, 2, idx.Count())

	// Search should return the most similar vector to vec1
	results := idx.Search(vec1, 1)
	require.Len(t, results, 1)
	assert.Equal(t, "Asset chunk one", results[0].ChunkText)
	assert.InDelta(t, 1.0, results[0].Similarity, 0.001)
}

func TestVectorIndexManager_CacheHit(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-002"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	embeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "cached chunk", IndexTypeText),
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	mgr := NewVectorIndexManager(store, 512, 300)

	// First load
	idx1, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	require.NotNil(t, idx1)

	// Second load — should return the same index pointer and increment ActiveSessions
	idx2, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	require.NotNil(t, idx2)

	// Same underlying index
	assert.Same(t, idx1, idx2)

	// ActiveSessions should be 2
	mgr.mu.Lock()
	loaded := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	require.NotNil(t, loaded)
	assert.Equal(t, 2, loaded.ActiveSessions)
}

func TestVectorIndexManager_ReleaseIndex(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-003"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	embeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "release chunk", IndexTypeText),
	}
	require.NoError(t, store.CreateBatch(ctx, embeddings))

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)

	// Verify initial ActiveSessions
	mgr.mu.Lock()
	assert.Equal(t, 1, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	mgr.mu.Unlock()

	mgr.ReleaseIndex(tmID, IndexTypeText)

	// ActiveSessions should be 0
	mgr.mu.Lock()
	assert.Equal(t, 0, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	mgr.mu.Unlock()

	// Release again — should not go below 0
	mgr.ReleaseIndex(tmID, IndexTypeText)
	mgr.mu.Lock()
	assert.Equal(t, 0, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	mgr.mu.Unlock()
}

func TestVectorIndexManager_MemoryPressureEviction(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	// First threat model: no embeddings (tiny index)
	tmID1 := "tm-vim-pressure-001"
	// Second threat model: also no embeddings (will succeed or be rejected)
	tmID2 := "tm-vim-pressure-002"

	// Set maxMemoryBytes to 100 bytes — empty indexes (0 bytes) fit within budget,
	// but once we manually inflate the first index to 100 bytes it exceeds the 90%
	// threshold (90 bytes), triggering LRU eviction when the second is loaded.
	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    store,
		maxMemoryBytes:    100,
		inactivityTimeout: 5 * time.Minute,
	}

	// Load first index — it's empty so MemoryBytes == 0, total < 90 byte threshold initially
	idx1, err := mgr.GetOrLoadIndex(ctx, tmID1, IndexTypeText, 3)
	require.NoError(t, err)
	require.NotNil(t, idx1)

	// Mark first index as inactive so eviction can remove it if needed
	mgr.mu.Lock()
	mgr.indexes[tmID1+":"+IndexTypeText].ActiveSessions = 0
	mgr.mu.Unlock()

	// Manually inflate the memory size of the first index to exceed 90% of budget
	mgr.mu.Lock()
	mgr.indexes[tmID1+":"+IndexTypeText].MemoryBytes = 100
	mgr.mu.Unlock()

	// Now loading the second index should trigger LRU eviction of the first
	// (since total 100 bytes >= 0.9 * 100 byte budget = 90 bytes)
	idx2, err := mgr.GetOrLoadIndex(ctx, tmID2, IndexTypeText, 3)
	require.NoError(t, err)
	require.NotNil(t, idx2)

	// First index should have been evicted
	mgr.mu.Lock()
	_, firstStillPresent := mgr.indexes[tmID1+":"+IndexTypeText]
	mgr.mu.Unlock()
	assert.False(t, firstStillPresent, "first index should have been evicted under pressure")

	// Metrics should record the eviction
	assert.GreaterOrEqual(t, mgr.pressureEvictions, int64(1))
	assert.GreaterOrEqual(t, mgr.totalEvictions, int64(1))
}

func TestVectorIndexManager_MemoryPressureRejection(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    store,
		maxMemoryBytes:    100,
		inactivityTimeout: 5 * time.Minute,
	}

	tmID1 := "tm-vim-reject-001"
	tmID2 := "tm-vim-reject-002"

	// Load first index
	_, err := mgr.GetOrLoadIndex(ctx, tmID1, IndexTypeText, 3)
	require.NoError(t, err)

	// Keep first index active so it cannot be evicted, and inflate memory above 90% threshold
	mgr.mu.Lock()
	mgr.indexes[tmID1+":"+IndexTypeText].ActiveSessions = 1
	mgr.indexes[tmID1+":"+IndexTypeText].MemoryBytes = 100
	mgr.mu.Unlock()

	// Second load should fail: can't evict (active) and over budget
	_, err = mgr.GetOrLoadIndex(ctx, tmID2, IndexTypeText, 3)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient memory")
	assert.Equal(t, int64(1), mgr.rejectedSessions)
}

func TestVectorIndexManager_GetStatus(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID1 := "tm-vim-status-001"
	tmID2 := "tm-vim-status-002"
	dim := 3

	vec := []float32{1.0, 0.0, 0.0}
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID1, "asset", "asset-001", 0, vec, "status chunk 1", IndexTypeText),
		makeTestEmbedding(tmID2, "threat", "threat-001", 0, vec, "status chunk 2", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID1, IndexTypeText, dim)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID2, IndexTypeText, dim)
	require.NoError(t, err)

	status := mgr.GetStatus()
	require.NotNil(t, status)

	// Verify expected keys exist
	assert.Contains(t, status, "memory_used_bytes")
	assert.Contains(t, status, "memory_budget_bytes")
	assert.Contains(t, status, "memory_utilization_pct")
	assert.Contains(t, status, "indexes_loaded")
	assert.Contains(t, status, "avg_index_size_bytes")
	assert.Contains(t, status, "largest_index_bytes")
	assert.Contains(t, status, "evictions_total")
	assert.Contains(t, status, "evictions_pressure")
	assert.Contains(t, status, "sessions_rejected")
	assert.Contains(t, status, "indexes")

	assert.Equal(t, 2, status["indexes_loaded"])
	assert.Equal(t, int64(512*1024*1024), status["memory_budget_bytes"])

	indexes, ok := status["indexes"].([]map[string]any)
	require.True(t, ok)
	assert.Len(t, indexes, 2)

	// Each index entry should have the expected keys
	for _, entry := range indexes {
		assert.Contains(t, entry, "threat_model_id")
		assert.Contains(t, entry, "vectors")
		assert.Contains(t, entry, "memory_bytes")
		assert.Contains(t, entry, "active_sessions")
		assert.Contains(t, entry, "last_accessed")
	}
}

// TestVectorIndexManager_CompositeKey_Isolation verifies that text and code indexes
// for the same threat model are stored as separate instances.
func TestVectorIndexManager_CompositeKey_Isolation(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-composite-001"
	dim := 3

	vecText := []float32{1.0, 0.0, 0.0}
	vecCode := []float32{0.0, 1.0, 0.0}

	textEmbeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vecText, "text chunk", IndexTypeText),
	}
	codeEmbeddings := []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "repository", "repo-001", 0, vecCode, "code chunk", IndexTypeCode),
	}
	require.NoError(t, store.CreateBatch(ctx, textEmbeddings))
	require.NoError(t, store.CreateBatch(ctx, codeEmbeddings))

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load text index
	textIdx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	require.NotNil(t, textIdx)

	// Load code index
	codeIdx, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, dim)
	require.NoError(t, err)
	require.NotNil(t, codeIdx)

	// They must be different instances
	assert.NotSame(t, textIdx, codeIdx)

	// Each index should have exactly one vector
	assert.Equal(t, 1, textIdx.Count())
	assert.Equal(t, 1, codeIdx.Count())

	// Two separate map entries should exist
	mgr.mu.Lock()
	textLoaded := mgr.indexes[tmID+":"+IndexTypeText]
	codeLoaded := mgr.indexes[tmID+":"+IndexTypeCode]
	mgr.mu.Unlock()

	require.NotNil(t, textLoaded)
	require.NotNil(t, codeLoaded)

	assert.Equal(t, 1, textLoaded.ActiveSessions)
	assert.Equal(t, 1, codeLoaded.ActiveSessions)

	assert.Equal(t, IndexTypeText, textLoaded.IndexType)
	assert.Equal(t, IndexTypeCode, codeLoaded.IndexType)
}

// TestVectorIndexManager_CompositeKey_IndependentEviction verifies that evicting one
// index type does not affect the other index type for the same threat model.
func TestVectorIndexManager_CompositeKey_IndependentEviction(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-composite-002"

	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    store,
		maxMemoryBytes:    200,
		inactivityTimeout: 5 * time.Minute,
	}

	// Load text index
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, 3)
	require.NoError(t, err)

	// Load code index
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, 3)
	require.NoError(t, err)

	// Mark text index as inactive and inflate its memory to trigger eviction
	mgr.mu.Lock()
	mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions = 0
	mgr.indexes[tmID+":"+IndexTypeText].MemoryBytes = 180
	mgr.mu.Unlock()

	// Load a third index to trigger LRU eviction of the text index
	tmID2 := "tm-vim-composite-evict"
	_, err = mgr.GetOrLoadIndex(ctx, tmID2, IndexTypeText, 3)
	require.NoError(t, err)

	// Text index for tmID should be evicted
	mgr.mu.Lock()
	_, textPresent := mgr.indexes[tmID+":"+IndexTypeText]
	_, codePresent := mgr.indexes[tmID+":"+IndexTypeCode]
	mgr.mu.Unlock()

	assert.False(t, textPresent, "text index should have been evicted")
	assert.True(t, codePresent, "code index should still be present")
}

// TestVectorIndexManager_InvalidateIndex verifies that InvalidateIndex removes
// the index when there are no active sessions.
func TestVectorIndexManager_InvalidateIndex(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-invalidate-001"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "invalidate chunk", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load and then release
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	mgr.ReleaseIndex(tmID, IndexTypeText)

	// Verify index is present
	mgr.mu.Lock()
	_, present := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	require.True(t, present)

	// Invalidate should remove it (no active sessions)
	mgr.InvalidateIndex(tmID, IndexTypeText)

	mgr.mu.Lock()
	_, stillPresent := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	assert.False(t, stillPresent, "index should have been removed by InvalidateIndex")
}

// TestVectorIndexManager_InvalidateIndex_ActiveSessionsSkipped verifies that
// InvalidateIndex does NOT remove the index when active sessions are present.
func TestVectorIndexManager_InvalidateIndex_ActiveSessionsSkipped(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-invalidate-002"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "active chunk", IndexTypeText),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	// Load without releasing — active session remains
	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)

	mgr.mu.Lock()
	assert.Equal(t, 1, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	mgr.mu.Unlock()

	// InvalidateIndex should skip because there is an active session
	mgr.InvalidateIndex(tmID, IndexTypeText)

	mgr.mu.Lock()
	_, stillPresent := mgr.indexes[tmID+":"+IndexTypeText]
	mgr.mu.Unlock()
	assert.True(t, stillPresent, "index with active sessions should NOT be removed")
}

// TestVectorIndexManager_ReleaseIndex_CompositeKey verifies that releasing the text
// index does not affect the active session count of the code index.
func TestVectorIndexManager_ReleaseIndex_CompositeKey(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-release-composite-001"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "text chunk", IndexTypeText),
		makeTestEmbedding(tmID, "repository", "repo-001", 0, vec, "code chunk", IndexTypeCode),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, dim)
	require.NoError(t, err)

	// Both have 1 active session
	mgr.mu.Lock()
	assert.Equal(t, 1, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	assert.Equal(t, 1, mgr.indexes[tmID+":"+IndexTypeCode].ActiveSessions)
	mgr.mu.Unlock()

	// Release text index
	mgr.ReleaseIndex(tmID, IndexTypeText)

	// Text should be 0, code should still be 1
	mgr.mu.Lock()
	assert.Equal(t, 0, mgr.indexes[tmID+":"+IndexTypeText].ActiveSessions)
	assert.Equal(t, 1, mgr.indexes[tmID+":"+IndexTypeCode].ActiveSessions)
	mgr.mu.Unlock()
}

// TestVectorIndexManager_GetStatus_IncludesIndexType verifies that the status
// response includes the index_type field for each loaded index.
func TestVectorIndexManager_GetStatus_IncludesIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-vim-status-indextype-001"
	dim := 3
	vec := []float32{1.0, 0.0, 0.0}

	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		makeTestEmbedding(tmID, "asset", "asset-001", 0, vec, "text chunk", IndexTypeText),
		makeTestEmbedding(tmID, "repository", "repo-001", 0, vec, "code chunk", IndexTypeCode),
	}))

	mgr := NewVectorIndexManager(store, 512, 300)

	_, err := mgr.GetOrLoadIndex(ctx, tmID, IndexTypeText, dim)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID, IndexTypeCode, dim)
	require.NoError(t, err)

	status := mgr.GetStatus()

	indexes, ok := status["indexes"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, indexes, 2)

	// Collect the index_type values reported
	indexTypes := make(map[string]bool)
	for _, entry := range indexes {
		assert.Contains(t, entry, "index_type", "each index entry should include index_type")
		if it, ok := entry["index_type"].(string); ok {
			indexTypes[it] = true
		}
	}

	assert.True(t, indexTypes[IndexTypeText], "status should include text index type")
	assert.True(t, indexTypes[IndexTypeCode], "status should include code index type")
}
