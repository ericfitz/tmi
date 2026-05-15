package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimmyEmbeddingStore_CreateAndList(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmIDStr := "tm-embed-001"
	tmID := models.DBVarchar(tmIDStr)
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-001"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 0",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     1,
			ContentHash:    models.DBVarchar("hash-002"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 1",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     models.DBVarchar("threat"),
			EntityID:       models.DBVarchar("threat-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-003"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Threat description chunk 0",
			IndexType:      IndexTypeText,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	results, err := store.ListByThreatModelAndIndexType(ctx, tmIDStr, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify ordering: entity_type ASC, entity_id ASC, chunk_index ASC
	// "asset" comes before "threat"
	assert.Equal(t, "asset", string(results[0].EntityType))
	assert.Equal(t, 0, results[0].ChunkIndex)
	assert.Equal(t, "asset", string(results[1].EntityType))
	assert.Equal(t, 1, results[1].ChunkIndex)
	assert.Equal(t, "threat", string(results[2].EntityType))

	// Verify content
	assert.Equal(t, models.DBText("Asset description chunk 0"), results[0].ChunkText)
	assert.Equal(t, "text-embedding-3-small", string(results[0].EmbeddingModel))
	assert.Equal(t, 1536, results[0].EmbeddingDim)

	// Listing for a different TM returns empty
	other, err := store.ListByThreatModelAndIndexType(ctx, "tm-other", IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, other)
}

func TestTimmyEmbeddingStore_IndexTypeIsolation(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-idx-iso"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-text-001"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Text index asset chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-002"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-text-002"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Another text index chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("repository"),
			EntityID:       models.DBVarchar("repo-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-code-001"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Code index repository chunk",
			IndexType:      IndexTypeCode,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// ListByThreatModelAndIndexType with IndexTypeText returns only text embeddings
	textResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, textResults, 2)
	for _, r := range textResults {
		assert.Equal(t, IndexTypeText, string(r.IndexType))
	}

	// ListByThreatModelAndIndexType with IndexTypeCode returns only code embeddings
	codeResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Len(t, codeResults, 1)
	assert.Equal(t, IndexTypeCode, string(codeResults[0].IndexType))
	assert.Equal(t, "repository", string(codeResults[0].EntityType))
	assert.Equal(t, "repo-001", string(codeResults[0].EntityID))

	// An unknown index type returns empty
	noneResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, "unknown")
	require.NoError(t, err)
	assert.Empty(t, noneResults)
}

func TestTimmyEmbeddingStore_DeleteByEntity(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-002"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-a"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Asset chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("threat"),
			EntityID:       models.DBVarchar("threat-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-b"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Threat chunk",
			IndexType:      IndexTypeText,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only the asset embedding
	_, err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
	require.NoError(t, err)

	remaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "threat", string(remaining[0].EntityType))
	assert.Equal(t, "threat-001", string(remaining[0].EntityID))
}

func TestTimmyEmbeddingStore_DeleteByThreatModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-003"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-x"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Chunk text",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("threat"),
			EntityID:       models.DBVarchar("threat-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-y"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Another chunk",
			IndexType:      IndexTypeCode,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Verify records exist before deletion (across both index types)
	beforeText, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, beforeText, 1)

	beforeCode, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	require.Len(t, beforeCode, 1)

	// Delete all embeddings for this threat model
	_, err = store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)

	afterText, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, afterText)

	afterCode, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Empty(t, afterCode)
}

func TestTimmyEmbeddingStore_DeleteByThreatModelAndIndexType(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-004"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-text-a"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Text index chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("repository"),
			EntityID:       models.DBVarchar("repo-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-code-a"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Code index chunk",
			IndexType:      IndexTypeCode,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only code embeddings
	count, err := store.DeleteByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Text embeddings should remain
	textRemaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, textRemaining, 1)
	assert.Equal(t, IndexTypeText, string(textRemaining[0].IndexType))

	// Code embeddings should be gone
	codeRemaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Empty(t, codeRemaining)
}

func TestTimmyEmbeddingStore_DeleteByEntity_ReturnsCount(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-005"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       "asset-count-001",
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-count-1"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Chunk 0",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       "asset-count-001",
			ChunkIndex:     1,
			ContentHash:    models.DBVarchar("hash-count-1"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Chunk 1",
			IndexType:      IndexTypeText,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	count, err := store.DeleteByEntity(ctx, tmID, "asset", "asset-count-001")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	remaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestTimmyEmbeddingStore_DeleteByThreatModel_ReturnsCount(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-006"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("asset"),
			EntityID:       models.DBVarchar("asset-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-tm-a"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Text chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  models.DBVarchar(tmID),
			EntityType:     models.DBVarchar("repository"),
			EntityID:       models.DBVarchar("repo-001"),
			ChunkIndex:     0,
			ContentHash:    models.DBVarchar("hash-tm-b"),
			EmbeddingModel: models.DBVarchar("text-embedding-3-small"),
			EmbeddingDim:   1536,
			ChunkText:      "Code chunk",
			IndexType:      IndexTypeCode,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	count, err := store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	remaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, remaining)
}

func TestTimmyEmbeddingStore_ListEntityMetadataByThreatModelAndIndexType_OneEntryPerEntity(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-meta-001"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h-a", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "a1", ChunkIndex: 1, ContentHash: "h-a", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "y", IndexType: IndexTypeText},
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "threat", EntityID: "t1", ChunkIndex: 0, ContentHash: "h-t", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "z", IndexType: IndexTypeText},
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
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h-text", EmbeddingModel: "m1", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "repository", EntityID: "r1", ChunkIndex: 0, ContentHash: "h-code", EmbeddingModel: "m2", EmbeddingDim: 16, ChunkText: "y", IndexType: IndexTypeCode},
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

func TestTimmyEmbeddingStore_DeleteEntitiesWithStaleEmbeddingMetadata_DeletesOnlyMismatchedRows(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-stale-001"
	require.NoError(t, store.CreateBatch(ctx, []models.TimmyEmbedding{
		// fresh: matches current (m-current/8)
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "fresh", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale model
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "stale-model", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale dim
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "stale-dim", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 16, ChunkText: "x", IndexType: IndexTypeText},
	}))

	deleted, err := store.DeleteEntitiesWithStaleEmbeddingMetadata(ctx, tmID, IndexTypeText, "m-current", 8)
	require.NoError(t, err)
	assert.Equal(t, int64(2), deleted)

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
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-current", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
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
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "asset", EntityID: "a1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old", EmbeddingDim: 8, ChunkText: "x", IndexType: IndexTypeText},
		// stale code (different model on the code side, must NOT be deleted by a text-side call)
		{ThreatModelID: models.DBVarchar(tmID), EntityType: "repository", EntityID: "r1", ChunkIndex: 0, ContentHash: "h", EmbeddingModel: "m-old-code", EmbeddingDim: 16, ChunkText: "y", IndexType: IndexTypeCode},
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
