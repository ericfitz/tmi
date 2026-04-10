package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// TimmyEmbeddingStore defines operations for persisting vector embeddings
type TimmyEmbeddingStore interface {
	ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error)
	CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error
	DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error)
	DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error)
	DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error)
}

// GlobalTimmyEmbeddingStore is the global embedding store instance
var GlobalTimmyEmbeddingStore TimmyEmbeddingStore
