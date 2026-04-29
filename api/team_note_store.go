package api

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TeamNoteStoreInterface defines the store interface for team notes
type TeamNoteStoreInterface interface {
	Create(ctx context.Context, note *TeamNote, teamID string) (*TeamNote, error)
	Get(ctx context.Context, id string) (*TeamNote, error)
	Update(ctx context.Context, id string, note *TeamNote, teamID string) (*TeamNote, error)
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*TeamNote, error)
	List(ctx context.Context, teamID string, offset, limit int, includeNonSharable bool) ([]TeamNoteListItem, int, error)
	Count(ctx context.Context, teamID string, includeNonSharable bool) (int, error)
}

// GormTeamNoteStore implements TeamNoteStoreInterface using GORM
type GormTeamNoteStore struct {
	db *gorm.DB
}

// NewGormTeamNoteStore creates a new GORM-backed team note store
func NewGormTeamNoteStore(db *gorm.DB) *GormTeamNoteStore {
	return &GormTeamNoteStore{db: db}
}

// teamNoteToRecord converts an API TeamNote to a GORM record
func teamNoteToRecord(note *TeamNote, teamID string) *models.TeamNoteRecord {
	record := &models.TeamNoteRecord{
		TeamID:  teamID,
		Name:    note.Name,
		Content: models.DBText(note.Content),
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

// teamNoteFromRecord converts a GORM record to an API TeamNote
func teamNoteFromRecord(record *models.TeamNoteRecord) *TeamNote {
	id := uuid.MustParse(record.ID)
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return &TeamNote{
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

// teamNoteListItemFromRecord converts a GORM record to an API TeamNoteListItem
func teamNoteListItemFromRecord(record *models.TeamNoteRecord) TeamNoteListItem {
	id := uuid.MustParse(record.ID)
	sharable := bool(record.Sharable)
	timmyEnabled := bool(record.TimmyEnabled)
	createdAt := record.CreatedAt
	modifiedAt := record.ModifiedAt

	return TeamNoteListItem{
		Id:           &id,
		Name:         record.Name,
		Description:  record.Description,
		Sharable:     &sharable,
		TimmyEnabled: &timmyEnabled,
		CreatedAt:    &createdAt,
		ModifiedAt:   &modifiedAt,
	}
}

// Create creates a new team note
func (s *GormTeamNoteStore) Create(ctx context.Context, note *TeamNote, teamID string) (*TeamNote, error) {
	logger := slogging.Get()

	// Verify parent team exists
	var teamCount int64
	if err := s.db.WithContext(ctx).Model(&models.TeamRecord{}).
		Where(map[string]any{"id": teamID}).
		Count(&teamCount).Error; err != nil {
		logger.Error("Failed to check team existence: %v", err)
		return nil, dberrors.Classify(err)
	}
	if teamCount == 0 {
		return nil, ErrTeamNotFound
	}

	// Generate ID if not provided
	if note.Id == nil {
		id := uuid.New()
		note.Id = &id
	}

	record := teamNoteToRecord(note, teamID)
	record.ID = note.Id.String()

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Create(record).Error; err != nil {
			logger.Error("Failed to create team note: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return teamNoteFromRecord(record), nil
}

// Get retrieves a team note by ID
func (s *GormTeamNoteStore) Get(ctx context.Context, id string) (*TeamNote, error) {
	logger := slogging.Get()

	var record models.TeamNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTeamNoteNotFound
		}
		logger.Error("Failed to get team note: %v", err)
		return nil, dberrors.Classify(err)
	}

	return teamNoteFromRecord(&record), nil
}

// Update replaces a team note
func (s *GormTeamNoteStore) Update(ctx context.Context, id string, note *TeamNote, teamID string) (*TeamNote, error) {
	logger := slogging.Get()

	var existing models.TeamNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTeamNoteNotFound
		}
		logger.Error("Failed to find team note for update: %v", err)
		return nil, dberrors.Classify(err)
	}

	// Verify the note belongs to the specified team
	if existing.TeamID != teamID {
		return nil, ErrTeamNoteNotFound
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

	err := authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		if err := tx.Save(&existing).Error; err != nil {
			logger.Error("Failed to update team note: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return teamNoteFromRecord(&existing), nil
}

// Delete deletes a team note
func (s *GormTeamNoteStore) Delete(ctx context.Context, id string) error {
	logger := slogging.Get()

	return authdb.WithRetryableGormTransaction(ctx, s.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		result := tx.
			Where(map[string]any{"id": id}).
			Delete(&models.TeamNoteRecord{})
		if result.Error != nil {
			logger.Error("Failed to delete team note: %v", result.Error)
			return dberrors.Classify(result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrTeamNoteNotFound
		}
		return nil
	})
}

// Patch applies JSON Patch operations to a team note
func (s *GormTeamNoteStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*TeamNote, error) {
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

	// Find the record to get the teamID
	var record models.TeamNoteRecord
	if err := s.db.WithContext(ctx).
		Where(map[string]any{"id": id}).
		First(&record).Error; err != nil {
		logger.Error("Failed to find team note for patch: %v", err)
		return nil, dberrors.Classify(err)
	}

	return s.Update(ctx, id, &patched, record.TeamID)
}

// List returns a paginated list of team notes for a team
func (s *GormTeamNoteStore) List(ctx context.Context, teamID string, offset, limit int, includeNonSharable bool) ([]TeamNoteListItem, int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.TeamNoteRecord{}).
		Where(map[string]any{"team_id": teamID})

	if !includeNonSharable {
		query = query.Where(map[string]any{"sharable": models.DBBool(true)})
	}

	// Get total count
	var total int64
	if err := query.Count(&total).Error; err != nil {
		logger.Error("Failed to count team notes: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	// Get paginated results
	var records []models.TeamNoteRecord
	if err := query.Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&records).Error; err != nil {
		logger.Error("Failed to list team notes: %v", err)
		return nil, 0, dberrors.Classify(err)
	}

	items := make([]TeamNoteListItem, len(records))
	for i, record := range records {
		items[i] = teamNoteListItemFromRecord(&record)
	}

	return items, int(total), nil
}

// Count returns the number of team notes for a team
func (s *GormTeamNoteStore) Count(ctx context.Context, teamID string, includeNonSharable bool) (int, error) {
	logger := slogging.Get()

	query := s.db.WithContext(ctx).Model(&models.TeamNoteRecord{}).
		Where(map[string]any{"team_id": teamID})

	if !includeNonSharable {
		query = query.Where(map[string]any{"sharable": models.DBBool(true)})
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error("Failed to count team notes: %v", err)
		return 0, dberrors.Classify(err)
	}

	return int(count), nil
}
