package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExtractionJobStore persists ExtractionJob rows. The result-consumer is the
// sole writer of terminal states; the publish-side callers only InsertQueued.
type ExtractionJobStore struct {
	db *gorm.DB
}

// NewExtractionJobStore constructs the store.
func NewExtractionJobStore(db *gorm.DB) *ExtractionJobStore {
	return &ExtractionJobStore{db: db}
}

// InsertQueued inserts a queued row for job_id. Idempotent: an existing row
// is left unchanged (OnConflict DoNothing). Portable across PG and Oracle.
// Uses Col()/ColumnName() so the Oracle GORM driver receives uppercase column
// identifiers when emitting MERGE INTO.
func (s *ExtractionJobStore) InsertQueued(ctx context.Context, jobID, documentRef string) error {
	dialect := s.db.Name()
	row := models.ExtractionJob{
		JobID:       models.DBVarchar(jobID),
		DocumentRef: models.DBVarchar(documentRef),
		Status:      models.DBVarchar(models.ExtractionStatusQueued),
	}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{Col(dialect, "job_id")},
			DoNothing: true,
		}).
		Create(&row).Error
	if err != nil {
		return fmt.Errorf("extraction job store: insert queued %s: %w", jobID, err)
	}
	return nil
}

// MarkTerminal upserts the terminal state for job_id. If the queued row is
// missing it is created. OnConflict on job_id updates status, reason_code,
// completed_at, updated_at. Portable across PG and Oracle.
// Uses Col()/ColumnName() so the Oracle GORM driver receives uppercase column
// identifiers when emitting MERGE INTO.
//
// When no prior queued row exists (bare-upsert-insert path), document_ref is
// set to empty string because the column is NOT NULL and no FK enforces it.
// An empty reasonCode string is stored as SQL NULL (NullableDBVarchar{Valid: false}).
func (s *ExtractionJobStore) MarkTerminal(ctx context.Context, jobID, status, reasonCode string) error {
	dialect := s.db.Name()
	now := time.Now().UTC()

	reasonCodeVal := models.NullableDBVarchar{Valid: false}
	if reasonCode != "" {
		reasonCodeVal = models.NullableDBVarchar{String: reasonCode, Valid: true}
	}

	row := models.ExtractionJob{
		JobID:       models.DBVarchar(jobID),
		DocumentRef: models.DBVarchar(""), // empty string on bare-upsert-insert; existing rows keep their value
		Status:      models.DBVarchar(status),
		ReasonCode:  reasonCodeVal,
		CompletedAt: &now,
	}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{Col(dialect, "job_id")},
			DoUpdates: clause.AssignmentColumns([]string{
				ColumnName(dialect, "status"),
				ColumnName(dialect, "reason_code"),
				ColumnName(dialect, "completed_at"),
				ColumnName(dialect, "updated_at"),
			}),
		}).
		Create(&row).Error
	if err != nil {
		return fmt.Errorf("extraction job store: mark terminal %s: %w", jobID, err)
	}
	return nil
}
