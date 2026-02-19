package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormDocumentStore implements DocumentStore using GORM
type GormDocumentStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormDocumentStore creates a new GORM-backed document store with optional caching
func NewGormDocumentStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormDocumentStore {
	return &GormDocumentStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new document
func (s *GormDocumentStore) Create(ctx context.Context, document *Document, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating document: %s in threat model: %s", document.Name, threatModelID)

	// Generate ID if not provided
	if document.Id == nil {
		id := uuid.New()
		document.Id = &id
	}

	now := time.Now().UTC()

	model := models.Document{
		ID:            document.Id.String(),
		ThreatModelID: threatModelID,
		Name:          document.Name,
		URI:           document.Uri,
		Description:   document.Description,
		CreatedAt:     now,
		ModifiedAt:    now,
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		logger.Error("Failed to create document in database: %v", err)
		return fmt.Errorf("failed to create document: %w", err)
	}

	// Update API object with timestamps from database
	document.CreatedAt = &model.CreatedAt
	document.ModifiedAt = &model.ModifiedAt

	// Save metadata if present
	if document.Metadata != nil && len(*document.Metadata) > 0 {
		if err := s.saveMetadata(ctx, document.Id.String(), *document.Metadata); err != nil {
			logger.Error("Failed to save document metadata: %v", err)
			// Don't fail the request if metadata saving fails
		}
	}

	// Cache the new document
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to cache new document: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      document.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created document: %s", document.Id)
	return nil
}

// Get retrieves a document by ID
func (s *GormDocumentStore) Get(ctx context.Context, id string) (*Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting document: %s", id)

	// Try cache first
	if s.cache != nil {
		document, err := s.cache.GetCachedDocument(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting document %s: %v", id, err)
		} else if document != nil {
			logger.Debug("Cache hit for document: %s", id)
			return document, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for document %s, querying database", id)

	var model models.Document
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("document not found: %s", id)
		}
		logger.Error("Failed to get document from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get document: %w", result.Error)
	}

	document := s.modelToAPI(&model)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for document %s: %v", id, err)
		metadata = []Metadata{}
	}
	document.Metadata = &metadata

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to cache document after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved document: %s", id)
	return document, nil
}

// Update updates an existing document
func (s *GormDocumentStore) Update(ctx context.Context, document *Document, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating document: %s", document.Id)

	// Note: Do not include modified_at in updates map as the Document model has
	// autoUpdateTime which GORM handles automatically. Including it manually
	// causes ORA-00957 (duplicate column name) errors in Oracle.
	updates := map[string]interface{}{
		"name":        document.Name,
		"uri":         document.Uri,
		"description": document.Description,
	}

	// Skip hooks to avoid validation errors on empty model struct.
	// Document fields are already validated via OpenAPI middleware before reaching here.
	result := s.db.WithContext(ctx).Session(&gorm.Session{SkipHooks: true}).Model(&models.Document{}).
		Where("id = ? AND threat_model_id = ?", document.Id.String(), threatModelID).
		Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update document in database: %v", result.Error)
		return fmt.Errorf("failed to update document: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("document not found: %s", document.Id)
	}

	// Update metadata if present
	if document.Metadata != nil {
		if err := s.updateMetadata(ctx, document.Id.String(), *document.Metadata); err != nil {
			logger.Error("Failed to update document metadata: %v", err)
		}
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheDocument(ctx, document); cacheErr != nil {
			logger.Error("Failed to update document cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      document.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated document: %s", document.Id)
	return nil
}

// Delete removes a document
func (s *GormDocumentStore) Delete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting document: %s", id)

	// Get threat model ID for cache invalidation
	var model models.Document
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("document not found: %s", id)
		}
		logger.Error("Failed to get threat model ID for document %s: %v", id, err)
		return fmt.Errorf("failed to get document parent: %w", err)
	}

	// Delete from database
	result := s.db.WithContext(ctx).Delete(&models.Document{}, "id = ?", id)
	if result.Error != nil {
		logger.Error("Failed to delete document from database: %v", result.Error)
		return fmt.Errorf("failed to delete document: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("document not found: %s", id)
	}

	// Delete metadata
	s.db.WithContext(ctx).Where("entity_type = ? AND entity_id = ?", "document", id).Delete(&models.Metadata{})

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "document", id); cacheErr != nil {
			logger.Error("Failed to remove document from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "document",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      model.ThreatModelID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after document deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted document: %s", id)
	return nil
}

// List retrieves documents for a threat model with pagination
func (s *GormDocumentStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing documents for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var documents []Document
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "documents", threatModelID, offset, limit, &documents)
		if err == nil && documents != nil {
			logger.Debug("Cache hit for document list %s [%d:%d]", threatModelID, offset, limit)
			return documents, nil
		}
		if err != nil {
			logger.Error("Cache error when getting document list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for document list, querying database")

	var modelList []models.Document
	result := s.db.WithContext(ctx).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query documents from database: %v", result.Error)
		return nil, fmt.Errorf("failed to list documents: %w", result.Error)
	}

	documents = make([]Document, 0, len(modelList))
	for _, model := range modelList {
		doc := s.modelToAPI(&model)

		// Load metadata for this document
		metadata, metaErr := s.loadMetadata(ctx, model.ID)
		if metaErr != nil {
			logger.Error("Failed to load metadata for document %s: %v", model.ID, metaErr)
			metadata = []Metadata{}
		}
		doc.Metadata = &metadata

		documents = append(documents, *doc)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "documents", threatModelID, offset, limit, documents); cacheErr != nil {
			logger.Error("Failed to cache document list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d documents", len(documents))
	return documents, nil
}

// BulkCreate creates multiple documents in a single transaction
func (s *GormDocumentStore) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Bulk creating %d documents", len(documents))

	if len(documents) == 0 {
		return nil
	}

	now := time.Now().UTC()

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range documents {
			document := &documents[i]

			// Generate ID if not provided
			if document.Id == nil {
				id := uuid.New()
				document.Id = &id
			}

			model := models.Document{
				ID:            document.Id.String(),
				ThreatModelID: threatModelID,
				Name:          document.Name,
				URI:           document.Uri,
				Description:   document.Description,
				CreatedAt:     now,
				ModifiedAt:    now,
			}

			if err := tx.Create(&model).Error; err != nil {
				logger.Error("Failed to bulk create document %d: %v", i, err)
				return fmt.Errorf("failed to insert document %d: %w", i, err)
			}
		}

		return nil
	})
}

// Patch applies JSON patch operations to a document
func (s *GormDocumentStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Document, error) {
	logger := slogging.Get()
	logger.Debug("Patching document %s with %d operations", id, len(operations))

	// Get current document
	document, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(document, op); err != nil {
			logger.Error("Failed to apply patch operation %s to document %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getDocumentThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the document
	if err := s.Update(ctx, document, threatModelID); err != nil {
		return nil, err
	}

	return document, nil
}

// Count returns the total number of documents for a threat model
func (s *GormDocumentStore) Count(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Counting documents for threat model %s", threatModelID)

	var count int64
	result := s.db.WithContext(ctx).Model(&models.Document{}).
		Where("threat_model_id = ?", threatModelID).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count documents: %v", result.Error)
		return 0, fmt.Errorf("failed to count documents: %w", result.Error)
	}

	return int(count), nil
}

// InvalidateCache removes document-related cache entries
func (s *GormDocumentStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "document", id)
}

// WarmCache preloads documents for a threat model into cache
func (s *GormDocumentStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model documents: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of documents
	documents, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache with %d documents for threat model %s", len(documents), threatModelID)
	return nil
}

// modelToAPI converts a GORM Document model to the API Document type
func (s *GormDocumentStore) modelToAPI(model *models.Document) *Document {
	id, _ := uuid.Parse(model.ID)
	doc := &Document{
		Id:          &id,
		Name:        model.Name,
		Uri:         model.URI,
		Description: model.Description,
	}

	// Include timestamps
	if !model.CreatedAt.IsZero() {
		doc.CreatedAt = &model.CreatedAt
	}
	if !model.ModifiedAt.IsZero() {
		doc.ModifiedAt = &model.ModifiedAt
	}

	return doc
}

// loadMetadata loads metadata for a document
func (s *GormDocumentStore) loadMetadata(ctx context.Context, documentID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "document", documentID)
}

// saveMetadata saves metadata for a document
func (s *GormDocumentStore) saveMetadata(ctx context.Context, documentID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "document", documentID, metadata)
}

// updateMetadata updates metadata for a document
func (s *GormDocumentStore) updateMetadata(ctx context.Context, documentID string, metadata []Metadata) error {
	return deleteAndSaveEntityMetadata(s.db.WithContext(ctx), "document", documentID, metadata)
}

// applyPatchOperation applies a single patch operation to a document
func (s *GormDocumentStore) applyPatchOperation(document *Document, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == string(Replace) {
			if name, ok := op.Value.(string); ok {
				document.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/uri":
		if op.Op == string(Replace) {
			if uri, ok := op.Value.(string); ok {
				document.Uri = uri
			} else {
				return fmt.Errorf("invalid value type for uri: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case string(Replace), string(Add):
			if desc, ok := op.Value.(string); ok {
				document.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case string(Remove):
			document.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getDocumentThreatModelID retrieves the threat model ID for a document
func (s *GormDocumentStore) getDocumentThreatModelID(ctx context.Context, documentID string) (string, error) {
	var model models.Document
	err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", documentID).Error
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for document: %w", err)
	}
	return model.ThreatModelID, nil
}
