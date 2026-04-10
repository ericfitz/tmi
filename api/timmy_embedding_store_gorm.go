package api

import (
	"context"
	"fmt"
	"sync"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormTimmyEmbeddingStore implements TimmyEmbeddingStore using GORM
type GormTimmyEmbeddingStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyEmbeddingStore creates a new GORM-backed embedding store
func NewGormTimmyEmbeddingStore(db *gorm.DB) *GormTimmyEmbeddingStore {
	return &GormTimmyEmbeddingStore{db: db}
}

// ListByThreatModelAndIndexType returns all embeddings for a threat model and index type ordered by entity_type, entity_id, chunk_index
func (s *GormTimmyEmbeddingStore) ListByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) ([]models.TimmyEmbedding, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing embeddings for threat model %s index type %s", threatModelID, indexType)

	var embeddings []models.TimmyEmbedding
	err := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
		Order("entity_type ASC, entity_id ASC, chunk_index ASC").
		Find(&embeddings).Error
	if err != nil {
		logger.Error("Failed to list embeddings for threat model %s index type %s: %v", threatModelID, indexType, err)
		return nil, fmt.Errorf("failed to list embeddings: %w", err)
	}

	logger.Debug("Found %d embeddings for threat model %s index type %s", len(embeddings), threatModelID, indexType)
	return embeddings, nil
}

// CreateBatch creates a batch of embeddings
func (s *GormTimmyEmbeddingStore) CreateBatch(ctx context.Context, embeddings []models.TimmyEmbedding) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating batch of %d embeddings", len(embeddings))

	if len(embeddings) == 0 {
		return nil
	}

	if err := s.db.WithContext(ctx).Create(&embeddings).Error; err != nil {
		logger.Error("Failed to create embedding batch: %v", err)
		return fmt.Errorf("failed to create embeddings: %w", err)
	}

	logger.Debug("Successfully created %d embeddings", len(embeddings))
	return nil
}

// DeleteByEntity deletes all embeddings for a specific entity within a threat model
func (s *GormTimmyEmbeddingStore) DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting embeddings for entity %s/%s in threat model %s", entityType, entityID, threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{
			"threat_model_id": threatModelID,
			"entity_type":     entityType,
			"entity_id":       entityID,
		}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for entity %s/%s: %v", entityType, entityID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by entity: %w", result.Error)
	}

	logger.Debug("Deleted %d embeddings for entity %s/%s", result.RowsAffected, entityType, entityID)
	return result.RowsAffected, nil
}

// DeleteByThreatModel deletes all embeddings for a threat model
func (s *GormTimmyEmbeddingStore) DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting all embeddings for threat model %s", threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete embeddings for threat model %s: %v", threatModelID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by threat model: %w", result.Error)
	}

	logger.Debug("Deleted %d embeddings for threat model %s", result.RowsAffected, threatModelID)
	return result.RowsAffected, nil
}

// DeleteByThreatModelAndIndexType deletes all embeddings for a threat model and index type
func (s *GormTimmyEmbeddingStore) DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting %s embeddings for threat model %s", indexType, threatModelID)

	result := s.db.WithContext(ctx).
		Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
		Delete(&models.TimmyEmbedding{})
	if result.Error != nil {
		logger.Error("Failed to delete %s embeddings for threat model %s: %v", indexType, threatModelID, result.Error)
		return 0, fmt.Errorf("failed to delete embeddings by threat model and index type: %w", result.Error)
	}

	logger.Debug("Deleted %d %s embeddings for threat model %s", result.RowsAffected, indexType, threatModelID)
	return result.RowsAffected, nil
}
