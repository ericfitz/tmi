package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Tombstone store methods for soft delete, restore, hard delete, and get-including-deleted.
// These methods are added to existing GORM store types to implement tombstoning (issue #126).

// contextKeyIncludeDeleted is used to pass include_deleted flag through context
type contextKeyIncludeDeleted struct{}

// ContextWithIncludeDeleted returns a context with include_deleted set
func ContextWithIncludeDeleted(ctx context.Context) context.Context {
	return context.WithValue(ctx, contextKeyIncludeDeleted{}, true)
}

// includeDeletedFromContext returns whether include_deleted is set in the context
func includeDeletedFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(contextKeyIncludeDeleted{}).(bool)
	return v
}

// AuthorizeIncludeDeleted checks whether the authenticated user has owner or admin role,
// which is required to use the include_deleted query parameter. Returns true if authorized.
// If not authorized, sends a 403 response and returns false.
func AuthorizeIncludeDeleted(c *gin.Context) bool {
	// Check if user is an administrator
	isAdmin, _ := IsUserAdministrator(c)
	if isAdmin {
		return true
	}

	// Check if user has owner role on the resource (set by ThreatModelMiddleware)
	if role, exists := c.Get("userRole"); exists {
		if r, ok := role.(Role); ok && r == RoleOwner {
			return true
		}
	}

	HandleRequestError(c, &RequestError{
		Status:  http.StatusForbidden,
		Code:    "forbidden",
		Message: "The include_deleted parameter requires owner or admin role",
	})
	return false
}

// --- GormThreatModelStore tombstone methods ---

// SoftDelete sets deleted_at on a threat model and all its children
func (s *GormThreatModelStore) SoftDelete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()
	now := time.Now().UTC()

	return s.db.Transaction(func(tx *gorm.DB) error {
		// Verify the threat model exists and is not already deleted
		var tm models.ThreatModel
		if err := tx.First(&tm, "id = ? AND deleted_at IS NULL", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("threat model with ID %s not found", id)
			}
			return fmt.Errorf("failed to get threat model: %w", err)
		}

		// Soft-delete the threat model
		// Use Model with primary key set to satisfy Oracle GORM driver's WHERE clause check
		if err := tx.Model(&models.ThreatModel{ID: id}).UpdateColumn("deleted_at", now).Error; err != nil {
			return fmt.Errorf("failed to soft-delete threat model: %w", err)
		}

		// Cascade soft-delete to all children
		// Use UpdateColumn to skip model hooks (BeforeSave/BeforeUpdate) which would
		// validate fields on the empty model struct and fail (e.g., Document.Name required)
		// Note: Oracle's GORM driver returns "WHERE conditions required" when an
		// UpdateColumn matches zero rows. We check RowsAffected to distinguish
		// between a genuine error and a no-op (no children to soft-delete).
		for _, model := range []struct {
			table string
			m     any
		}{
			{"threats", &models.Threat{}},
			{"diagrams", &models.Diagram{}},
			{"documents", &models.Document{}},
			{"assets", &models.Asset{}},
			{"notes", &models.Note{}},
			{"repositories", &models.Repository{}},
		} {
			result := tx.Model(model.m).Where("threat_model_id = ? AND deleted_at IS NULL", id).UpdateColumn("deleted_at", now)
			if result.Error != nil && result.RowsAffected > 0 {
				return fmt.Errorf("failed to soft-delete %s: %w", model.table, result.Error)
			}
		}

		logger.Info("Soft-deleted threat model %s and all children", id)
		return nil
	})
}

// Restore clears deleted_at on a threat model and all its children
func (s *GormThreatModelStore) Restore(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	logger := slogging.Get()

	return s.db.Transaction(func(tx *gorm.DB) error {
		// Verify the threat model exists and IS deleted
		var tm models.ThreatModel
		if err := tx.First(&tm, "id = ? AND deleted_at IS NOT NULL", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("threat model with ID %s not found or not deleted", id)
			}
			return fmt.Errorf("failed to get threat model: %w", err)
		}

		// Restore the threat model
		if err := tx.Model(&models.ThreatModel{ID: id}).UpdateColumn("deleted_at", nil).Error; err != nil {
			return fmt.Errorf("failed to restore threat model: %w", err)
		}

		// Restore all children
		// Use UpdateColumn to skip model hooks (same reason as SoftDelete above)
		// See SoftDelete for explanation of RowsAffected check (Oracle compatibility).
		for _, model := range []struct {
			table string
			m     any
		}{
			{"threats", &models.Threat{}},
			{"diagrams", &models.Diagram{}},
			{"documents", &models.Document{}},
			{"assets", &models.Asset{}},
			{"notes", &models.Note{}},
			{"repositories", &models.Repository{}},
		} {
			result := tx.Model(model.m).Where("threat_model_id = ? AND deleted_at IS NOT NULL", id).UpdateColumn("deleted_at", nil)
			if result.Error != nil && result.RowsAffected > 0 {
				return fmt.Errorf("failed to restore %s: %w", model.table, result.Error)
			}
		}

		logger.Info("Restored threat model %s and all children", id)
		return nil
	})
}

// HardDelete permanently removes a threat model and all its children (the original Delete behavior)
func (s *GormThreatModelStore) HardDelete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	return s.hardDeleteTx(s.db, id)
}

// hardDeleteTx performs the hard delete within a given transaction or DB handle
func (s *GormThreatModelStore) hardDeleteTx(db *gorm.DB, id string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. Get all child entity IDs for metadata cleanup
		var threatIDs, diagramIDs, documentIDs, assetIDs, noteIDs, repositoryIDs []string

		tx.Model(&models.Threat{}).Where("threat_model_id = ?", id).Pluck("id", &threatIDs)
		tx.Model(&models.Diagram{}).Where("threat_model_id = ?", id).Pluck("id", &diagramIDs)
		tx.Model(&models.Document{}).Where("threat_model_id = ?", id).Pluck("id", &documentIDs)
		tx.Model(&models.Asset{}).Where("threat_model_id = ?", id).Pluck("id", &assetIDs)
		tx.Model(&models.Note{}).Where("threat_model_id = ?", id).Pluck("id", &noteIDs)
		tx.Model(&models.Repository{}).Where("threat_model_id = ?", id).Pluck("id", &repositoryIDs)

		// 2. Delete metadata for all child entities
		for _, pair := range []struct {
			entityType string
			ids        []string
		}{
			{"threat", threatIDs},
			{"diagram", diagramIDs},
			{"document", documentIDs},
			{"asset", assetIDs},
			{"note", noteIDs},
			{"repository", repositoryIDs},
		} {
			if len(pair.ids) > 0 {
				if err := tx.Where("entity_type = ? AND entity_id IN ?", pair.entityType, pair.ids).Delete(&models.Metadata{}).Error; err != nil {
					return fmt.Errorf("failed to delete %s metadata: %w", pair.entityType, err)
				}
			}
		}

		// 3. Delete collaboration sessions
		if err := tx.Where("threat_model_id = ?", id).Delete(&models.CollaborationSession{}).Error; err != nil {
			return fmt.Errorf("failed to delete collaboration sessions: %w", err)
		}

		// 4. Delete child entities
		for _, model := range []struct {
			name string
			m    any
		}{
			{"threats", &models.Threat{}},
			{"diagrams", &models.Diagram{}},
			{"documents", &models.Document{}},
			{"assets", &models.Asset{}},
			{"notes", &models.Note{}},
			{"repositories", &models.Repository{}},
		} {
			if err := tx.Where("threat_model_id = ?", id).Delete(model.m).Error; err != nil {
				return fmt.Errorf("failed to delete %s: %w", model.name, err)
			}
		}

		// 5. Delete threat model metadata
		if err := tx.Where("entity_type = 'threat_model' AND entity_id = ?", id).Delete(&models.Metadata{}).Error; err != nil {
			return fmt.Errorf("failed to delete threat model metadata: %w", err)
		}

		// 6. Delete access records
		if err := tx.Where("threat_model_id = ?", id).Delete(&models.ThreatModelAccess{}).Error; err != nil {
			return fmt.Errorf("failed to delete threat model access records: %w", err)
		}

		// 7. Delete the threat model
		result := tx.Delete(&models.ThreatModel{}, "id = ?", id)
		if result.Error != nil {
			return fmt.Errorf("failed to delete threat model: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("threat model with ID %s not found", id)
		}

		return nil
	})
}

// GetIncludingDeleted retrieves a threat model by ID without filtering on deleted_at
func (s *GormThreatModelStore) GetIncludingDeleted(id string) (ThreatModel, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var tm models.ThreatModel
	result := s.db.Preload("Owner").Preload("CreatedBy").Preload("SecurityReviewer").First(&tm, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return ThreatModel{}, fmt.Errorf("threat model with ID %s not found", id)
		}
		return ThreatModel{}, fmt.Errorf("failed to get threat model: %w", result.Error)
	}

	return s.convertToAPIModel(&tm)
}

// --- GormDiagramStore tombstone methods ---

// SoftDelete sets deleted_at on a diagram and nullifies diagram_id/cell_id on related threats
func (s *GormDiagramStore) SoftDelete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now().UTC()

	return s.db.Transaction(func(tx *gorm.DB) error {
		// 1. Soft-delete the diagram
		result := tx.Model(&models.Diagram{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
		if result.Error != nil {
			return fmt.Errorf("failed to soft-delete diagram: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("diagram not found: %s", id)
		}

		// 2. Nullify diagram_id and cell_id on threats referencing this diagram
		if err := tx.Model(&models.Threat{}).
			Where("diagram_id = ?", id).
			Updates(map[string]any{
				"diagram_id": nil,
				"cell_id":    nil,
			}).Error; err != nil {
			return fmt.Errorf("failed to nullify diagram references on threats: %w", err)
		}

		return nil
	})
}

// Restore clears deleted_at on a diagram
func (s *GormDiagramStore) Restore(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.Model(&models.Diagram{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore diagram: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("diagram not found or not deleted: %s", id)
	}
	return nil
}

// HardDelete permanently removes a diagram (original Delete behavior with FK cleanup)
func (s *GormDiagramStore) HardDelete(id string) error {
	return s.hardDeleteDiagram(id)
}

// GetIncludingDeleted retrieves a diagram by ID without filtering on deleted_at
func (s *GormDiagramStore) GetIncludingDeleted(id string) (DfdDiagram, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var diagram models.Diagram
	result := s.db.First(&diagram, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return DfdDiagram{}, fmt.Errorf("diagram not found: %s", id)
		}
		return DfdDiagram{}, fmt.Errorf("failed to get diagram: %w", result.Error)
	}

	return s.convertToAPIDiagram(&diagram)
}

// --- Sub-resource tombstone methods (generic pattern) ---

// GormDocumentRepository tombstone methods

func (s *GormDocumentRepository) SoftDelete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now().UTC()
	// Use Model with primary key set to satisfy Oracle GORM driver's WHERE clause check.
	// Oracle's GORM driver rejects UpdateColumn with Model(&empty{}).Where(...) as
	// "WHERE conditions required" even though a WHERE clause is present.
	result := s.db.WithContext(ctx).Model(&models.Document{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to soft-delete document: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("document not found: %s", id)
	}

	// Invalidate cache
	if s.cache != nil {
		if err := s.cache.InvalidateEntity(ctx, "document", id); err != nil {
			slogging.Get().Error("Failed to invalidate document cache after soft-delete: %v", err)
		}
	}
	return nil
}

func (s *GormDocumentRepository) Restore(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).Model(&models.Document{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore document: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("document not found or not deleted: %s", id)
	}
	return nil
}

func (s *GormDocumentRepository) HardDelete(ctx context.Context, id string) error {
	return s.hardDeleteDocument(ctx, id)
}

func (s *GormDocumentRepository) GetIncludingDeleted(ctx context.Context, id string) (*Document, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var model models.Document
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("document not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get document: %w", result.Error)
	}

	document := s.modelToAPI(&model)
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		metadata = []Metadata{}
	}
	document.Metadata = &metadata
	return document, nil
}

// GormNoteRepository tombstone methods

func (s *GormNoteRepository) SoftDelete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.Note{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to soft-delete note: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("note not found: %s", id)
	}

	if s.cache != nil {
		if err := s.cache.InvalidateEntity(ctx, "note", id); err != nil {
			slogging.Get().Error("Failed to invalidate note cache after soft-delete: %v", err)
		}
	}
	return nil
}

func (s *GormNoteRepository) Restore(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).Model(&models.Note{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore note: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("note not found or not deleted: %s", id)
	}
	return nil
}

func (s *GormNoteRepository) HardDelete(ctx context.Context, id string) error {
	return s.hardDeleteNote(ctx, id)
}

func (s *GormNoteRepository) GetIncludingDeleted(ctx context.Context, id string) (*Note, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var model models.Note
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("note not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get note: %w", result.Error)
	}

	note := s.modelToAPI(&model)
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		metadata = []Metadata{}
	}
	note.Metadata = &metadata
	return note, nil
}

// GormRepositoryRepository tombstone methods

func (s *GormRepositoryRepository) SoftDelete(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.Repository{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to soft-delete repository: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("repository not found: %s", id)
	}

	if s.cache != nil {
		if err := s.cache.InvalidateEntity(ctx, "repository", id); err != nil {
			slogging.Get().Error("Failed to invalidate repository cache after soft-delete: %v", err)
		}
	}
	return nil
}

func (s *GormRepositoryRepository) Restore(ctx context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	result := s.db.WithContext(ctx).Model(&models.Repository{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore repository: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("repository not found or not deleted: %s", id)
	}
	return nil
}

func (s *GormRepositoryRepository) HardDelete(ctx context.Context, id string) error {
	return s.hardDeleteRepository(ctx, id)
}

func (s *GormRepositoryRepository) GetIncludingDeleted(ctx context.Context, id string) (*Repository, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var model models.Repository
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("repository not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get repository: %w", result.Error)
	}

	repository := s.modelToAPI(&model)
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		metadata = []Metadata{}
	}
	repository.Metadata = &metadata
	return repository, nil
}

// GormAssetRepository tombstone methods

func (s *GormAssetRepository) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.Asset{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to soft-delete asset: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("asset not found: %s", id)
	}

	if s.cache != nil {
		if err := s.cache.InvalidateEntity(ctx, "asset", id); err != nil {
			slogging.Get().Error("Failed to invalidate asset cache after soft-delete: %v", err)
		}
	}
	return nil
}

func (s *GormAssetRepository) Restore(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Model(&models.Asset{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore asset: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("asset not found or not deleted: %s", id)
	}
	return nil
}

func (s *GormAssetRepository) HardDelete(ctx context.Context, id string) error {
	return s.hardDeleteAsset(ctx, id)
}

func (s *GormAssetRepository) GetIncludingDeleted(ctx context.Context, id string) (*Asset, error) {
	var model models.Asset
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("asset not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get asset: %w", result.Error)
	}

	asset := s.toAPIModel(&model)
	metadata, err := s.loadMetadata(ctx, id)
	if err != nil {
		metadata = []Metadata{}
	}
	asset.Metadata = &metadata
	return asset, nil
}

// GormThreatRepository tombstone methods

func (s *GormThreatRepository) SoftDelete(ctx context.Context, id string) error {
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&models.Threat{ID: id}).Where("deleted_at IS NULL").UpdateColumn("deleted_at", now)
	if result.Error != nil {
		return fmt.Errorf("failed to soft-delete threat: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("threat not found: %s", id)
	}

	if s.cache != nil {
		if err := s.cache.InvalidateEntity(ctx, "threat", id); err != nil {
			slogging.Get().Error("Failed to invalidate threat cache after soft-delete: %v", err)
		}
	}
	return nil
}

func (s *GormThreatRepository) Restore(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Model(&models.Threat{ID: id}).Where("deleted_at IS NOT NULL").UpdateColumn("deleted_at", nil)
	if result.Error != nil {
		return fmt.Errorf("failed to restore threat: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("threat not found or not deleted: %s", id)
	}
	return nil
}

func (s *GormThreatRepository) HardDelete(ctx context.Context, id string) error {
	return s.hardDeleteThreat(ctx, id)
}

func (s *GormThreatRepository) GetIncludingDeleted(ctx context.Context, id string) (*Threat, error) {
	var model models.Threat
	result := s.db.WithContext(ctx).First(&model, "id = ?", id)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("threat not found: %s", id)
		}
		return nil, fmt.Errorf("failed to get threat: %w", result.Error)
	}

	threat := s.toAPIModel(&model)
	return threat, nil
}
