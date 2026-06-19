package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmyEmbeddingStore defines operations for persisting vector embeddings
// SEM@85c2885c496b7031495d6d6c1aa09ecb6d3d45a2: interface for persisting, fetching, and deleting vector embeddings for threat model entities (reads DB)
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

// GlobalTimmyEmbeddingStore is the global embedding store instance
var GlobalTimmyEmbeddingStore TimmyEmbeddingStore
