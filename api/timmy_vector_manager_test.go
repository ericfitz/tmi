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

	idx, err := mgr.GetOrLoadIndex(ctx, tmID, dim)
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
	idx1, err := mgr.GetOrLoadIndex(ctx, tmID, dim)
	require.NoError(t, err)
	require.NotNil(t, idx1)

	// Second load — should return the same index pointer and increment ActiveSessions
	idx2, err := mgr.GetOrLoadIndex(ctx, tmID, dim)
	require.NoError(t, err)
	require.NotNil(t, idx2)

	// Same underlying index
	assert.Same(t, idx1, idx2)

	// ActiveSessions should be 2
	mgr.mu.Lock()
	loaded := mgr.indexes[tmID]
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

	_, err := mgr.GetOrLoadIndex(ctx, tmID, dim)
	require.NoError(t, err)

	// Verify initial ActiveSessions
	mgr.mu.Lock()
	assert.Equal(t, 1, mgr.indexes[tmID].ActiveSessions)
	mgr.mu.Unlock()

	mgr.ReleaseIndex(tmID)

	// ActiveSessions should be 0
	mgr.mu.Lock()
	assert.Equal(t, 0, mgr.indexes[tmID].ActiveSessions)
	mgr.mu.Unlock()

	// Release again — should not go below 0
	mgr.ReleaseIndex(tmID)
	mgr.mu.Lock()
	assert.Equal(t, 0, mgr.indexes[tmID].ActiveSessions)
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
	idx1, err := mgr.GetOrLoadIndex(ctx, tmID1, 3)
	require.NoError(t, err)
	require.NotNil(t, idx1)

	// Mark first index as inactive so eviction can remove it if needed
	mgr.mu.Lock()
	mgr.indexes[tmID1].ActiveSessions = 0
	mgr.mu.Unlock()

	// Manually inflate the memory size of the first index to exceed 90% of budget
	mgr.mu.Lock()
	mgr.indexes[tmID1].MemoryBytes = 100
	mgr.mu.Unlock()

	// Now loading the second index should trigger LRU eviction of the first
	// (since total 100 bytes >= 0.9 * 100 byte budget = 90 bytes)
	idx2, err := mgr.GetOrLoadIndex(ctx, tmID2, 3)
	require.NoError(t, err)
	require.NotNil(t, idx2)

	// First index should have been evicted
	mgr.mu.Lock()
	_, firstStillPresent := mgr.indexes[tmID1]
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
	_, err := mgr.GetOrLoadIndex(ctx, tmID1, 3)
	require.NoError(t, err)

	// Keep first index active so it cannot be evicted, and inflate memory above 90% threshold
	mgr.mu.Lock()
	mgr.indexes[tmID1].ActiveSessions = 1
	mgr.indexes[tmID1].MemoryBytes = 100
	mgr.mu.Unlock()

	// Second load should fail: can't evict (active) and over budget
	_, err = mgr.GetOrLoadIndex(ctx, tmID2, 3)
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

	_, err := mgr.GetOrLoadIndex(ctx, tmID1, dim)
	require.NoError(t, err)
	_, err = mgr.GetOrLoadIndex(ctx, tmID2, dim)
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
