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
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	results, err := store.ListByThreatModel(ctx, tmID)
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
	other, err := store.ListByThreatModel(ctx, "tm-other")
	require.NoError(t, err)
	assert.Empty(t, other)
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
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Delete only the asset embedding
	err = store.DeleteByEntity(ctx, tmID, "asset", "asset-001")
	require.NoError(t, err)

	remaining, err := store.ListByThreatModel(ctx, tmID)
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
		},
	}

	err := store.CreateBatch(ctx, embeddings)
	require.NoError(t, err)

	// Verify records exist before deletion
	before, err := store.ListByThreatModel(ctx, tmID)
	require.NoError(t, err)
	require.Len(t, before, 2)

	// Delete all embeddings for this threat model
	err = store.DeleteByThreatModel(ctx, tmID)
	require.NoError(t, err)

	after, err := store.ListByThreatModel(ctx, tmID)
	require.NoError(t, err)
	assert.Empty(t, after)
}
