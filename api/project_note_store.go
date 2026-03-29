package api

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ProjectNoteStoreInterface defines the store interface for project notes
type ProjectNoteStoreInterface interface {
	Create(ctx context.Context, note *ProjectNote, projectID string) (*ProjectNote, error)
	Get(ctx context.Context, id string) (*ProjectNote, error)
	Update(ctx context.Context, id string, note *ProjectNote, projectID string) (*ProjectNote, error)
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*ProjectNote, error)
	List(ctx context.Context, projectID string, offset, limit int, includeNonSharable bool) ([]ProjectNoteListItem, int, error)
	Count(ctx context.Context, projectID string, includeNonSharable bool) (int, error)
}

// GormProjectNoteStore implements ProjectNoteStoreInterface using GORM
type GormProjectNoteStore struct {
	db *gorm.DB
}

// NewGormProjectNoteStore creates a new GORM-backed project note store
func NewGormProjectNoteStore(db *gorm.DB) *GormProjectNoteStore {
	return &GormProjectNoteStore{db: db}
}

// projectNoteToRecord converts an API ProjectNote to a GORM record
func projectNoteToRecord(note *ProjectNote, projectID string) *models.ProjectNoteRecord {
	record := &models.ProjectNoteRecord{
		ProjectID: projectID,
		Name:      note.Name,
		Content:   models.DBText(note.Content),
	}

	if note.Id != nil {
		record.ID = note.Id.String()
	}
	if note.Description != nil {
		record.Description = note.Description
	}
	if note.TimmyEnabled != nil {
		record.TimmyEnabled = models.DBBool(*note.TimmyEnabled)
	} else {
		record.TimmyEnabled = models.DBBool(true)
	}
	if note.Sharable != nil {
		record.Sharable = models.DBBool(*note.Sharable)
	} else {
		record.Sharable = models.DBBool(true)
	}

	return record
}

// projectNoteFromRecord converts a GORM record to an API ProjectNote
func projectNoteFromRecord(record *models.ProjectNoteRecord) *ProjectNote {
	id := uuid.MustParse(record.ID)
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return &ProjectNote{
		Id:           &id,
		Name:         record.Name,
		Content:      string(record.Content),
		Description:  record.Description,
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &createdAt,
		ModifiedAt:   &modifiedAt,
	}
}

// projectNoteListItemFromRecord converts a GORM record to an API ProjectNoteListItem
func projectNoteListItemFromRecord(record *models.ProjectNoteRecord) ProjectNoteListItem {
	id := uuid.MustParse(record.ID)
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return ProjectNoteListItem{
		Id:           &id,
		Name:         record.Name,
		Description:  record.Description,
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &createdAt,
		ModifiedAt:   &modifiedAt,
	}
}

// Create creates a new project note
func (s *GormProjectNoteStore) Create(ctx context.Context, note *ProjectNote, projectID string) (*ProjectNote, error) {
	logger := slogging.Get()

	// Verify parent project exists
	var projectCount int64
	if err := s.db.WithContext(ctx).Model(&models.ProjectRecord{}).
		Where(map[string]any{"id": projectID}).
		Count(&projectCount).Error; err != nil {
		logger.Error("Failed to check project existence: %v", err)
		return nil, ServerError("Failed to validate project")
	}
	if projectCount == 0 {
		return nil, NotFoundError(fmt.Sprintf("Project not found: %s", projectID))
	}

	// Generate ID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}

	record := projectNoteToRecord(note, projectID)
	record.ID = note.Id.String()

	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		logger.Error("Failed to create project note: %v", err)
		return nil, ServerError("Failed to create project note")
	}

	return projectNoteFromRecord(record), nil
}

// Get retrieves a project note by ID
func (s *GormProjectNoteStore) Get(ctx context.Context, id string) (*ProjectNote, error) {
	logger := slogging.Get()

	var record models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFoundError("Project note not found")
		}
		logger.Error("Failed to get project note: %v", err)
		return nil, ServerError("Failed to retrieve project note")
	}

	return projectNoteFromRecord(&record), nil
}

// Update replaces a project note
func (s *GormProjectNoteStore) Update(ctx context.Context, id string, note *ProjectNote, projectID string) (*ProjectNote, error) {
	logger := slogging.Get()

	var existing models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFoundError("Project note not found")
		}
		logger.Error("Failed to find project note for update: %v", err)
		return nil, ServerError("Failed to update project note")
	}

	// Verify the note belongs to the specified project
	if existing.ProjectID != projectID {
		return nil, NotFoundError("Project note not found")
	}

	// Update fields
	existing.Name = note.Name
	existing.Content = models.DBText(note.Content)
	existing.Description = note.Description
	if note.TimmyEnabled != nil {
		existing.TimmyEnabled = models.DBBool(*note.TimmyEnabled)
	}
	if note.Sharable != nil {
		existing.Sharable = models.DBBool(*note.Sharable)
	}
	existing.ModifiedAt = time.Now().UTC()

	if err := s.db.WithContext(ctx).Save(&existing).Error; err != nil {
		logger.Error("Failed to update project note: %v", err)
		return nil, ServerError("Failed to update project note")
	}

	return projectNoteFromRecord(&existing), nil
}

// Delete deletes a project note
func (s *GormProjectNoteStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()

	result := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		Delete(&models.ProjectNoteRecord{})
	if result.Error != nil {
		logger.Error("Failed to delete project note: %v", result.Error)
		return ServerError("Failed to delete project note")
	}
	if result.RowsAffected == 0 {
		return NotFoundError("Project note not found")
	}

	return nil
}

// Patch applies JSON Patch operations to a project note
func (s *GormProjectNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*ProjectNote, error) {
	logger := slogging.Get()

	// Get existing note
	existing, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply patch operations
	patched, err := ApplyPatchOperations(*existing, operations)
	if err != nil {
		return nil, err
	}

	// Preserve immutable fields
	patched.Id = existing.Id
	patched.CreatedAt = existing.CreatedAt

	// Find the record to get the projectID
	var record models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&record).Error; err != nil {
		logger.Error("Failed to find project note for patch: %v", err)
		return nil, ServerError("Failed to patch project note")
	}

	return s.Update(ctx, id, &patched, record.ProjectID)
}

// List returns a paginated list of project notes for a project
func (s *GormProjectNoteStore) List(ctx context.Context, projectID string, offset, limit int, includeNonSharable bool) ([]ProjectNoteListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.ProjectNoteRecord{}).
		Where(map[string]any{"project_id": projectID})

	if !includeNonSharable {
		query = query.Where(map[string]any{"sharable": models.DBBool(true)})
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count project notes: %v", err)
		return nil, 0, ServerError("Failed to list project notes")
	}

	// Get paginated results
	var records []models.ProjectNoteRecord
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		logger.Error("Failed to list project notes: %v", err)
		return nil, 0, ServerError("Failed to list project notes")
	}

	items := make([]ProjectNoteListItem, len(records))
	for i, record := range records {
		items[i] = projectNoteListItemFromRecord(&record)
	}

	return items, int(total), nil
}

// Count returns the number of project notes for a project
func (s *GormProjectNoteStore) Count(ctx context.Context, projectID string, includeNonSharable bool) (int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.ProjectNoteRecord{}).
		Where(map[string]any{"project_id": projectID})

	if !includeNonSharable {
		query = query.Where(map[string]any{"sharable": models.DBBool(true)})
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error("Failed to count project notes: %v", err)
		return 0, ServerError("Failed to count project notes")
	}

	return int(count), nil
}
