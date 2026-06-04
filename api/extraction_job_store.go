package api

import (
	"context"
	"errors"
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

// GetDocumentRef returns the document_ref stored for the given jobID.
// Returns ("", nil) when the job row does not exist (e.g. the row was never
// inserted, or the document was hard-deleted before the result arrived).
func (s *ExtractionJobStore) GetDocumentRef(ctx context.Context, jobID string) (string, error) {
	var row models.ExtractionJob
	result := s.db.WithContext(ctx).
		Select("document_ref").
		Where("job_id = ?", jobID).
		First(&row)
	if result.Error != nil {
		if errors.Is(result.Error, gorm.ErrRecordNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("extraction job store: get document_ref for %s: %w", jobID, result.Error)
	}
	return string(row.DocumentRef), nil
}

// unknownDocumentRef is the sentinel written to the NOT NULL document_ref
// column on the bare-upsert-insert path (a terminal result arriving with no
// prior queued row, possible under at-least-once delivery). The real
// document_ref is unknown there, and an empty string must NOT be used: on
// Oracle an empty string is indistinguishable from NULL, so inserting one into
// the NOT NULL column raises ORA-01400 and the result message would redeliver
// forever. A non-empty sentinel preserves the NOT NULL invariant on both
// dialects.
const unknownDocumentRef = "__unknown__"

// MarkTerminal upserts the terminal state for job_id. If the queued row is
// missing it is created. OnConflict on job_id updates status, reason_code,
// completed_at, updated_at. Portable across PG and Oracle.
// Uses Col()/ColumnName() so the Oracle GORM driver receives uppercase column
// identifiers when emitting MERGE INTO.
//
// When no prior queued row exists (bare-upsert-insert path), document_ref is
// set to the unknownDocumentRef sentinel (NOT empty string; see the constant
// doc for the Oracle ORA-01400 rationale). The DoUpdates list omits
// document_ref, so an existing queued row keeps its real document_ref.
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
		DocumentRef: models.DBVarchar(unknownDocumentRef), // bare-insert path only; existing rows keep their value (omitted from DoUpdates)
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
