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

	tmID := "tm-embed-001"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			ContentHash:    "hash-001",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 0",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     1,
			ContentHash:    "hash-002",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset description chunk 1",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "threat",
			EntityID:       "threat-001",
			ChunkIndex:     0,
			ContentHash:    "hash-003",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Threat description chunk 0",
			IndexType:      IndexTypeText,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	results, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Len(t, results, 3)

	// Verify ordering: entity_type ASC, entity_id ASC, chunk_index ASC
	// "asset" comes before "threat"
	assert.Equal(t, "asset", results[0].EntityType)
	assert.Equal(t, 0, results[0].ChunkIndex)
	assert.Equal(t, "asset", results[1].EntityType)
	assert.Equal(t, 1, results[1].ChunkIndex)
	assert.Equal(t, "threat", results[2].EntityType)

	// Verify content
	assert.Equal(t, models.DBText("Asset description chunk 0"), results[0].ChunkText)
	assert.Equal(t, "text-embedding-3-small", results[0].EmbeddingModel)
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
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			ContentHash:    "hash-text-001",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Text index asset chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-002",
			ChunkIndex:     0,
			ContentHash:    "hash-text-002",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Another text index chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "repository",
			EntityID:       "repo-001",
			ChunkIndex:     0,
			ContentHash:    "hash-code-001",
			EmbeddingModel: "text-embedding-3-small",
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
		assert.Equal(t, IndexTypeText, r.IndexType)
	}

	// ListByThreatModelAndIndexType with IndexTypeCode returns only code embeddings
	codeResults, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Len(t, codeResults, 1)
	assert.Equal(t, IndexTypeCode, codeResults[0].IndexType)
	assert.Equal(t, "repository", codeResults[0].EntityType)
	assert.Equal(t, "repo-001", codeResults[0].EntityID)

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
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			ContentHash:    "hash-a",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Asset chunk",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "threat",
			EntityID:       "threat-001",
			ChunkIndex:     0,
			ContentHash:    "hash-b",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Threat chunk",
			IndexType:      IndexTypeText,
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only the asset embedding
	err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
	require.NoError(t, err)

	remaining, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, "threat", remaining[0].EntityType)
	assert.Equal(t, "threat-001", remaining[0].EntityID)
}

func TestTimmyEmbeddingStore_DeleteByThreatModel(t *testing.T) {
	db := setupTimmyTestDB(t)
	store := NewGormTimmyEmbeddingStore(db)
	ctx := context.Background()

	tmID := "tm-embed-003"
	embeddings := []models.TimmyEmbedding{
		{
			ThreatModelID:  tmID,
			EntityType:     "asset",
			EntityID:       "asset-001",
			ChunkIndex:     0,
			ContentHash:    "hash-x",
			EmbeddingModel: "text-embedding-3-small",
			EmbeddingDim:   1536,
			ChunkText:      "Chunk text",
			IndexType:      IndexTypeText,
		},
		{
			ThreatModelID:  tmID,
			EntityType:     "threat",
			EntityID:       "threat-001",
			ChunkIndex:     0,
			ContentHash:    "hash-y",
			EmbeddingModel: "text-embedding-3-small",
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
	err = store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)

	afterText, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeText)
	require.NoError(t, err)
	assert.Empty(t, afterText)

	afterCode, err := store.ListByThreatModelAndIndexType(ctx, tmID, IndexTypeCode)
	require.NoError(t, err)
	assert.Empty(t, afterCode)
}
