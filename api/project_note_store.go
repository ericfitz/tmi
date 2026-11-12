package api

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
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
	// Patch applies operations to the stored note, then calls check (when
	// non-nil) with the stored and patched notes before persisting; check may
	// rewrite the patched note, and an error from it aborts the patch.
	Patch(ctx context.Context, id string, operations []PatchOperation, check func(before, after *ProjectNote) error) (*ProjectNote, error)
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
		ProjectID: models.DBVarchar(projectID),
		Name:      models.DBVarchar(note.Name),
		Content:   models.DBText(note.Content),
	}

	if note.Id != nil {
		record.ID = models.DBVarchar(note.Id.String())
	}
	if note.Description != nil {
		record.Description = models.NewNullableDBText(note.Description)
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
	id := uuid.MustParse(string(record.ID))
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return &ProjectNote{
		Id:           &id,
		Name:         string(record.Name),
		Content:      string(record.Content),
		Description:  record.Description.Ptr(),
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &createdAt,
		ModifiedAt:   &modifiedAt,
	}
}

// projectNoteListItemFromRecord converts a GORM record to an API ProjectNoteListItem
func projectNoteListItemFromRecord(record *models.ProjectNoteRecord) ProjectNoteListItem {
	id := uuid.MustParse(string(record.ID))
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return ProjectNoteListItem{
		Id:           &id,
		Name:         string(record.Name),
		Description:  record.Description.Ptr(),
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
		Where(ColumnMap(s.db.Name(), map[string]any{"id": projectID})).
		Count(&projectCount).Error; err != nil {
		logger.Error("Failed to check project existence: %v", err)
		return nil, dberrors.Classify(err)
	}
	if projectCount == 0 {
		return nil, ErrProjectNotFound
	}

	// Generate ID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}

	record := projectNoteToRecord(note, projectID)
	record.ID = models.DBVarchar(note.Id.String())
	// One explicit microsecond instant for both columns, so the response equals
	// what a later GET returns (#1155).
	now := time.Now().UTC().Truncate(time.Microsecond)
	record.CreatedAt = now
	record.ModifiedAt = now

	// READ COMMITTED: insert-only on freshly keyed rows, so SERIALIZABLE only adds false ORA-08177 (#906, ADR 2026-09-17).
	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(record).Error; err != nil {
			logger.Error("Failed to create project note: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}

	return projectNoteFromRecord(record), nil
}

// Get retrieves a project note by ID
func (s *GormProjectNoteStore) Get(ctx context.Context, id string) (*ProjectNote, error) {
	logger := slogging.Get()

	var record models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(ColumnMap(s.db.Name(), map[string]any{"id": id})).
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNoteNotFound
		}
		logger.Error("Failed to get project note: %v", err)
		return nil, dberrors.Classify(err)
	}

	return projectNoteFromRecord(&record), nil
}

// Update replaces a project note
func (s *GormProjectNoteStore) Update(ctx context.Context, id string, note *ProjectNote, projectID string) (*ProjectNote, error) {
	logger := slogging.Get()

	var existing models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(ColumnMap(s.db.Name(), map[string]any{"id": id})).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProjectNoteNotFound
		}
		logger.Error("Failed to find project note for update: %v", err)
		return nil, dberrors.Classify(err)
	}

	// Verify the note belongs to the specified project
	if string(existing.ProjectID) != projectID {
		return nil, ErrProjectNoteNotFound
	}

	// Update fields
	existing.Name = models.DBVarchar(note.Name)
	existing.Content = models.DBText(note.Content)
	existing.Description = models.NewNullableDBText(note.Description)
	if note.TimmyEnabled != nil {
		existing.TimmyEnabled = models.DBBool(*note.TimmyEnabled)
	}
	if note.Sharable != nil {
		existing.Sharable = models.DBBool(*note.Sharable)
	}
	existing.ModifiedAt = time.Now().UTC().Truncate(time.Microsecond)

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// Updates with an explicit "ModifiedAt" (Go field name) rather than
		// Save: Save lets autoUpdateTime overwrite ModifiedAt with a second
		// instant, so the response would differ from the stored value (#1155).
		res := tx.Model(&existing).Updates(map[string]any{
			"Name":         existing.Name,
			"Content":      existing.Content,
			"Description":  existing.Description,
			"TimmyEnabled": existing.TimmyEnabled,
			"Sharable":     existing.Sharable,
			"ModifiedAt":   existing.ModifiedAt,
		})
		if err := res.Error; err != nil {
			logger.Error("Failed to update project note: %v", err)
			return dberrors.Classify(err)
		}
		if res.RowsAffected == 0 {
			return ErrProjectNoteNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return projectNoteFromRecord(&existing), nil
}

// Delete deletes a project note
func (s *GormProjectNoteStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()

	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where(ColumnMap(tx.Name(), map[string]any{"id": id})).
			Delete(&models.ProjectNoteRecord{})
		if result.Error != nil {
			logger.Error("Failed to delete project note: %v", result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrProjectNoteNotFound
		}
		return nil
	})
}

// Patch applies JSON Patch operations to a project note, runs check on the
// result, and persists it.
func (s *GormProjectNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation, check func(before, after *ProjectNote) error) (*ProjectNote, error) {
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

	// Validate the entity that is about to be persisted (#1013)
	if check != nil {
		if err := check(existing, &patched); err != nil {
			return nil, err
		}
	}

	// Find the record to get the projectID
	var record models.ProjectNoteRecord
	if err := s.db.WithContext(ctx).
		Where(ColumnMap(s.db.Name(), map[string]any{"id": id})).
		First(&record).Error; err != nil {
		logger.Error("Failed to find project note for patch: %v", err)
		return nil, dberrors.Classify(err)
	}

	return s.Update(ctx, id, &patched, string(record.ProjectID))
}

// List returns a paginated list of project notes for a project
func (s *GormProjectNoteStore) List(ctx context.Context, projectID string, offset, limit int, includeNonSharable bool) ([]ProjectNoteListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.ProjectNoteRecord{}).
		Where(ColumnMap(s.db.Name(), map[string]any{"project_id": projectID}))

	if !includeNonSharable {
		query = query.Where(ColumnMap(query.Name(), map[string]any{"sharable": models.DBBool(true)}))
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count project notes: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	// Get paginated results
	var records []models.ProjectNoteRecord
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		logger.Error("Failed to list project notes: %v", err)
		return nil, 0, dberrors.Classify(err)
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
		Where(ColumnMap(s.db.Name(), map[string]any{"project_id": projectID}))

	if !includeNonSharable {
		query = query.Where(ColumnMap(query.Name(), map[string]any{"sharable": models.DBBool(true)}))
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error("Failed to count project notes: %v", err)
		return 0, dberrors.Classify(err)
	}

	return int(count), nil
}
