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
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: GORM-backed store for Timmy vector embedding records with a read-write mutex
type GormTimmyEmbeddingStore struct {
	db    *gorm.DB
	mutex sync.RWMutex
}

// NewGormTimmyEmbeddingStore creates a new GORM-backed embedding store
// SEM@38c9cd78ea6f81a7cfa5891e34a980915566378b: build a new GORM-backed Timmy embedding store (pure)
func NewGormTimmyEmbeddingStore(db *gorm.DB) *GormTimmyEmbeddingStore {
	return &GormTimmyEmbeddingStore{db: db}
}

// ListByThreatModelAndIndexType returns all embeddings for a threat model and index type ordered by entity_type, entity_id, chunk_index
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: fetch all embeddings for a threat model and index type ordered by entity and chunk (reads DB)
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: store a batch of embedding records in a single retryable transaction (reads DB)
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: delete all embeddings for a specific entity within a threat model and return count removed (reads DB)
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: delete all embeddings for a threat model and return count removed (reads DB)
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
// SEM@fb2f7a7145abd513579b00a314e93717693bf60d: delete all embeddings for a threat model and index type and return count removed (reads DB)
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

// ListEntityMetadataByThreatModelAndIndexType returns one EntityEmbeddingMeta
// per entity. See TimmyEmbeddingStore.ListEntityMetadataByThreatModelAndIndexType.
// SEM@85c2885c496b7031495d6d6c1aa09ecb6d3d45a2: fetch a map of entity keys to embedding metadata for a threat model and index type (reads DB)
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
// SEM@6081f52fc388ecc8072369db4a8490b7b7f499c6: delete embeddings whose model or dimension disagrees with the current configuration (reads DB)
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
		// Use COALESCE around embedding_model so the predicate behaves
		// identically on Oracle (where '' is NULL) and PostgreSQL. Without it,
		// passing currentModel == "" on Oracle would silently delete nothing
		// for the model branch (NULL <> NULL is UNKNOWN), masking misconfig.
		result := tx.
			Where("threat_model_id = ? AND index_type = ? AND (COALESCE(embedding_model, '') <> ? OR embedding_dim <> ?)",
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
		rowsAffected, indexType, threatModelID)
	return rowsAffected, nil
}
