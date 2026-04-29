package api

import (
	"context"
	"sync"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
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
		return nil, dberrors.Classify(err)
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

	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(&embeddings).Error; err != nil {
			logger.Error("Failed to create embedding batch: %v", err)
			return dberrors.Classify(err)
		}
		logger.Debug("Successfully created %d embeddings", len(embeddings))
		return nil
	})
}

// DeleteByEntity deletes all embeddings for a specific entity within a threat model
func (s *GormTimmyEmbeddingStore) DeleteByEntity(ctx context.Context, threatModelID, entityType, entityID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting embeddings for entity %s/%s in threat model %s", entityType, entityID, threatModelID)

	var rowsAffected int64
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where(map[string]any{
				"threat_model_id": threatModelID,
				"entity_type":     entityType,
				"entity_id":       entityID,
			}).
			Delete(&models.TimmyEmbedding{})
		if result.Error != nil {
			logger.Error("Failed to delete embeddings for entity %s/%s: %v", entityType, entityID, result.Error)
			return dberrors.Classify(result.Error)
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}

	logger.Debug("Deleted %d embeddings for entity %s/%s", rowsAffected, entityType, entityID)
	return rowsAffected, nil
}

// DeleteByThreatModel deletes all embeddings for a threat model
func (s *GormTimmyEmbeddingStore) DeleteByThreatModel(ctx context.Context, threatModelID string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting all embeddings for threat model %s", threatModelID)

	var rowsAffected int64
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where(map[string]any{"threat_model_id": threatModelID}).
			Delete(&models.TimmyEmbedding{})
		if result.Error != nil {
			logger.Error("Failed to delete embeddings for threat model %s: %v", threatModelID, result.Error)
			return dberrors.Classify(result.Error)
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}

	logger.Debug("Deleted %d embeddings for threat model %s", rowsAffected, threatModelID)
	return rowsAffected, nil
}

// DeleteByThreatModelAndIndexType deletes all embeddings for a threat model and index type
func (s *GormTimmyEmbeddingStore) DeleteByThreatModelAndIndexType(ctx context.Context, threatModelID, indexType string) (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting %s embeddings for threat model %s", indexType, threatModelID)

	var rowsAffected int64
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where(map[string]any{"threat_model_id": threatModelID, "index_type": indexType}).
			Delete(&models.TimmyEmbedding{})
		if result.Error != nil {
			logger.Error("Failed to delete %s embeddings for threat model %s: %v", indexType, threatModelID, result.Error)
			return dberrors.Classify(result.Error)
		}
		rowsAffected = result.RowsAffected
		return nil
	})
	if err != nil {
		return 0, err
	}

	logger.Debug("Deleted %d %s embeddings for threat model %s", rowsAffected, indexType, threatModelID)
	return rowsAffected, nil
}
