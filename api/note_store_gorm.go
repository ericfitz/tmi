package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GormNoteStore implements NoteStore using GORM
type GormNoteStore struct {
	db               *gorm.DB
	cache            *CacheService
	cacheInvalidator *CacheInvalidator
	mutex            sync.RWMutex
}

// NewGormNoteStore creates a new GORM-backed note store with optional caching
func NewGormNoteStore(db *gorm.DB, cache *CacheService, invalidator *CacheInvalidator) *GormNoteStore {
	return &GormNoteStore{
		db:               db,
		cache:            cache,
		cacheInvalidator: invalidator,
	}
}

// Create creates a new note
func (s *GormNoteStore) Create(ctx context.Context, note *Note, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Creating note: %s in threat model: %s", note.Name, threatModelID)

	// Generate ID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}

	now := time.Now().UTC()

	model := models.Note{
		ID:            note.Id.String(),
		ThreatModelID: threatModelID,
		Name:          note.Name,
		Content:       models.DBText(note.Content),
		Description:   note.Description,
		CreatedAt:     now,
		ModifiedAt:    now,
	}

	if err := s.db.WithContext(ctx).Create(&model).Error; err != nil {
		logger.Error("Failed to create note in database: %v", err)
		return fmt.Errorf("failed to create note: %w", err)
	}

	// Save metadata if present
	if note.Metadata != nil && len(*note.Metadata) > 0 {
		if err := s.saveMetadata(ctx, note.Id.String(), *note.Metadata); err != nil {
			logger.Error("Failed to save note metadata: %v", err)
		}
	}

	// Cache the new note
	if s.cache != nil {
		if cacheErr := s.cache.CacheNote(ctx, note); cacheErr != nil {
			logger.Error("Failed to cache new note: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      note.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "create",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note creation: %v", invErr)
		}
	}

	logger.Debug("Successfully created note: %s", note.Id)
	return nil
}

// Get retrieves a note by ID
func (s *GormNoteStore) Get(ctx context.Context, id string) (*Note, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Getting note: %s", id)

	// Try cache first
	if s.cache != nil {
		note, err := s.cache.GetCachedNote(ctx, id)
		if err != nil {
			logger.Error("Cache error when getting note %s: %v", id, err)
		} else if note != nil {
			logger.Debug("Cache hit for note: %s", id)
			return note, nil
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for note %s, querying database", id)

	var model models.Note
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("note not found: %s", id)
		}
		logger.Error("Failed to get note from database: %v", result.Error)
		return nil, fmt.Errorf("failed to get note: %w", result.Error)
	}

	note := s.modelToAPI(&model)

	// Load metadata
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		logger.Error("Failed to load metadata for note %s: %v", id, err)
		metadata = []Metadata{}
	}
	note.Metadata = &metadata

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheNote(ctx, note); cacheErr != nil {
			logger.Error("Failed to cache note after database fetch: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved note: %s", id)
	return note, nil
}

// Update updates an existing note
func (s *GormNoteStore) Update(ctx context.Context, note *Note, threatModelID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Updating note: %s", note.Id)

	// Note: modified_at is handled automatically by GORM's autoUpdateTime tag
	updates := map[string]interface{}{
		"name":        note.Name,
		"content":     note.Content,
		"description": note.Description,
	}

	result := s.db.WithContext(ctx).Model(&models.Note{}).
		Where("id = ? AND threat_model_id = ?", note.Id.String(), threatModelID).
		Updates(updates)

	if result.Error != nil {
		logger.Error("Failed to update note in database: %v", result.Error)
		return fmt.Errorf("failed to update note: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("note not found: %s", note.Id)
	}

	// Update metadata if present
	if note.Metadata != nil {
		if err := s.updateMetadata(ctx, note.Id.String(), *note.Metadata); err != nil {
			logger.Error("Failed to update note metadata: %v", err)
		}
	}

	// Update cache
	if s.cache != nil {
		if cacheErr := s.cache.CacheNote(ctx, note); cacheErr != nil {
			logger.Error("Failed to update note cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      note.Id.String(),
			ParentType:    "threat_model",
			ParentID:      threatModelID,
			OperationType: "update",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note update: %v", invErr)
		}
	}

	logger.Debug("Successfully updated note: %s", note.Id)
	return nil
}

// Delete removes a note
func (s *GormNoteStore) Delete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	logger.Debug("Deleting note: %s", id)

	// Get threat model ID for cache invalidation
	var model models.Note
	if err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return fmt.Errorf("note not found: %s", id)
		}
		logger.Error("Failed to get threat model ID for note %s: %v", id, err)
		return fmt.Errorf("failed to get note parent: %w", err)
	}

	// Delete from database
	result := s.db.WithContext(ctx).Delete(&models.Note{}, "id = ?", id)
	if result.Error != nil {
		logger.Error("Failed to delete note from database: %v", result.Error)
		return fmt.Errorf("failed to delete note: %w", result.Error)
	}

	if result.RowsAffected == 0 {
		return fmt.Errorf("note not found: %s", id)
	}

	// Delete metadata
	s.db.WithContext(ctx).Where("entity_type = ? AND entity_id = ?", "note", id).Delete(&models.Metadata{})

	// Remove from cache
	if s.cache != nil {
		if cacheErr := s.cache.InvalidateEntity(ctx, "note", id); cacheErr != nil {
			logger.Error("Failed to remove note from cache: %v", cacheErr)
		}
	}

	// Invalidate related caches
	if s.cacheInvalidator != nil {
		event := InvalidationEvent{
			EntityType:    "note",
			EntityID:      id,
			ParentType:    "threat_model",
			ParentID:      model.ThreatModelID,
			OperationType: "delete",
			Strategy:      InvalidateImmediately,
		}
		if invErr := s.cacheInvalidator.InvalidateSubResourceChange(ctx, event); invErr != nil {
			logger.Error("Failed to invalidate caches after note deletion: %v", invErr)
		}
	}

	logger.Debug("Successfully deleted note: %s", id)
	return nil
}

// List retrieves notes for a threat model with pagination
func (s *GormNoteStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Note, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Listing notes for threat model %s (offset: %d, limit: %d)", threatModelID, offset, limit)

	// Try cache first
	var notes []Note
	if s.cache != nil {
		err := s.cache.GetCachedList(ctx, "notes", threatModelID, offset, limit, &notes)
		if err == nil && notes != nil {
			logger.Debug("Cache hit for note list %s [%d:%d]", threatModelID, offset, limit)
			return notes, nil
		}
		if err != nil {
			logger.Error("Cache error when getting note list: %v", err)
		}
	}

	// Cache miss - get from database
	logger.Debug("Cache miss for note list, querying database")

	var modelList []models.Note
	result := s.db.WithContext(ctx).
		Where("threat_model_id = ?", threatModelID).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&modelList)

	if result.Error != nil {
		logger.Error("Failed to query notes from database: %v", result.Error)
		return nil, fmt.Errorf("failed to list notes: %w", result.Error)
	}

	notes = make([]Note, 0, len(modelList))
	for _, model := range modelList {
		note := s.modelToAPI(&model)

		// Load metadata for this note
		metadata, metaErr := s.loadMetadata(ctx, model.ID)
		if metaErr != nil {
			logger.Error("Failed to load metadata for note %s: %v", model.ID, metaErr)
			metadata = []Metadata{}
		}
		note.Metadata = &metadata

		notes = append(notes, *note)
	}

	// Cache the result
	if s.cache != nil {
		if cacheErr := s.cache.CacheList(ctx, "notes", threatModelID, offset, limit, notes); cacheErr != nil {
			logger.Error("Failed to cache note list: %v", cacheErr)
		}
	}

	logger.Debug("Successfully retrieved %d notes", len(notes))
	return notes, nil
}

// Patch applies JSON patch operations to a note
func (s *GormNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error) {
	logger := slogging.Get()
	logger.Debug("Patching note %s with %d operations", id, len(operations))

	// Get current note
	note, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	for _, op := range operations {
		if err := s.applyPatchOperation(note, op); err != nil {
			logger.Error("Failed to apply patch operation %s to note %s: %v", op.Op, id, err)
			return nil, fmt.Errorf("failed to apply patch operation: %w", err)
		}
	}

	// Get threat model ID for update
	threatModelID, err := s.getNoteThreatModelID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get threat model ID: %w", err)
	}

	// Update the note
	if err := s.Update(ctx, note, threatModelID); err != nil {
		return nil, err
	}

	return note, nil
}

// Count returns the total number of notes for a threat model
func (s *GormNoteStore) Count(ctx context.Context, threatModelID string) (int, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	logger := slogging.Get()
	logger.Debug("Counting notes for threat model %s", threatModelID)

	var count int64
	result := s.db.WithContext(ctx).Model(&models.Note{}).
		Where("threat_model_id = ?", threatModelID).
		Count(&count)

	if result.Error != nil {
		logger.Error("Failed to count notes: %v", result.Error)
		return 0, fmt.Errorf("failed to count notes: %w", result.Error)
	}

	return int(count), nil
}

// InvalidateCache removes note-related cache entries
func (s *GormNoteStore) InvalidateCache(ctx context.Context, id string) error {
	if s.cache == nil {
		return nil
	}
	return s.cache.InvalidateEntity(ctx, "note", id)
}

// WarmCache preloads notes for a threat model into cache
func (s *GormNoteStore) WarmCache(ctx context.Context, threatModelID string) error {
	logger := slogging.Get()
	logger.Debug("Warming cache for threat model notes: %s", threatModelID)

	if s.cache == nil {
		return nil
	}

	// Load first page of notes
	notes, err := s.List(ctx, threatModelID, 0, 50)
	if err != nil {
		return fmt.Errorf("failed to warm cache: %w", err)
	}

	logger.Debug("Warmed cache with %d notes for threat model %s", len(notes), threatModelID)
	return nil
}

// modelToAPI converts a GORM Note model to the API Note type
func (s *GormNoteStore) modelToAPI(model *models.Note) *Note {
	id, _ := uuid.Parse(model.ID)
	return &Note{
		Id:          &id,
		Name:        model.Name,
		Content:     string(model.Content), // Convert DBText to string
		Description: model.Description,
	}
}

// loadMetadata loads metadata for a note
func (s *GormNoteStore) loadMetadata(ctx context.Context, noteID string) ([]Metadata, error) {
	return loadEntityMetadata(s.db.WithContext(ctx), "note", noteID)
}

// saveMetadata saves metadata for a note
func (s *GormNoteStore) saveMetadata(ctx context.Context, noteID string, metadata []Metadata) error {
	return saveEntityMetadata(s.db.WithContext(ctx), "note", noteID, metadata)
}

// updateMetadata updates metadata for a note
func (s *GormNoteStore) updateMetadata(ctx context.Context, noteID string, metadata []Metadata) error {
	return deleteAndSaveEntityMetadata(s.db.WithContext(ctx), "note", noteID, metadata)
}

// applyPatchOperation applies a single patch operation to a note
func (s *GormNoteStore) applyPatchOperation(note *Note, op PatchOperation) error {
	switch op.Path {
	case "/name":
		if op.Op == "replace" {
			if name, ok := op.Value.(string); ok {
				note.Name = name
			} else {
				return fmt.Errorf("invalid value type for name: expected string")
			}
		}
	case "/content":
		if op.Op == "replace" {
			if content, ok := op.Value.(string); ok {
				note.Content = content
			} else {
				return fmt.Errorf("invalid value type for content: expected string")
			}
		}
	case "/description":
		switch op.Op {
		case "replace", "add":
			if desc, ok := op.Value.(string); ok {
				note.Description = &desc
			} else {
				return fmt.Errorf("invalid value type for description: expected string")
			}
		case "remove":
			note.Description = nil
		}
	default:
		return fmt.Errorf("unsupported patch path: %s", op.Path)
	}
	return nil
}

// getNoteThreatModelID retrieves the threat model ID for a note
func (s *GormNoteStore) getNoteThreatModelID(ctx context.Context, noteID string) (string, error) {
	var model models.Note
	err := s.db.WithContext(ctx).Select("threat_model_id").First(&model, "id = ?", noteID).Error
	if err != nil {
		return "", fmt.Errorf("failed to get threat model ID for note: %w", err)
	}
	return model.ThreatModelID, nil
}
