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
//
// Oracle note: with OnConflict the gorm-oracle driver emits MERGE INTO, not
// INSERT ... RETURNING. RETURNING-from-MERGE is fragile, so the store methods
// must not rely on struct fields populated by Create after the call — re-read
// via GetDocumentRef instead.
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

// MarkTerminal records the terminal state for job_id and reports whether this
// call performed the *first* terminal transition (true) or hit a row that was
// already terminal (false). The boolean is the durable, restart-safe emit-once
// signal the result-consumer uses to fire the document.extraction_* webhook
// exactly once under JetStream at-least-once redelivery (#438).
//
// It is implemented as a guarded UPDATE followed, only when that matches no
// row, by an OnConflict-DoNothing insert:
//
//  1. UPDATE ... WHERE job_id = ? AND status NOT IN ('completed','failed').
//     A plain UPDATE reports reliable RowsAffected on PG, Oracle, and SQLite
//     (unlike MERGE/RETURNING). RowsAffected == 1 means we moved an existing
//     non-terminal row to terminal — the first transition → return true.
//     Under READ COMMITTED on both PG and Oracle the WHERE is re-evaluated
//     after the row lock is taken, so two concurrent deliveries cannot both
//     match: the second sees the now-terminal status and matches 0 rows.
//  2. RowsAffected == 0 means the row is absent (a terminal result arrived with
//     no prior queued row — possible under at-least-once delivery) OR it is
//     already terminal (a redelivery). Attempt a bare insert with the
//     unknownDocumentRef sentinel; OnConflict DoNothing makes a concurrent
//     insert or an existing terminal row a no-op. Insert RowsAffected == 1
//     means we created the terminal row → first transition (true); == 0 means a
//     row already existed and, per step 1, is already terminal → false.
//
// document_ref is only ever written on the bare-insert path and is set to the
// unknownDocumentRef sentinel (NOT empty string; see the constant doc for the
// Oracle ORA-01400 rationale). The guarded UPDATE never touches document_ref,
// so an existing queued row keeps its real document_ref. An empty reasonCode
// string is stored as SQL NULL (NullableDBVarchar{Valid: false}).
//
// Portable across PG, Oracle, and SQLite. Uses Col()/ColumnName()/AssignmentMap
// so the Oracle GORM driver receives uppercase column identifiers.
func (s *ExtractionJobStore) MarkTerminal(ctx context.Context, jobID, status, reasonCode string) (bool, error) {
	dialect := s.db.Name()
	now := time.Now().UTC()

	reasonCodeVal := models.NullableDBVarchar{Valid: false}
	if reasonCode != "" {
		reasonCodeVal = models.NullableDBVarchar{String: reasonCode, Valid: true}
	}

	// Step 1: guarded UPDATE — transition only a row that is not already terminal.
	assignments := AssignmentMap(dialect, map[string]any{
		"status":       status,
		"reason_code":  reasonCodeVal,
		"completed_at": &now,
		"updated_at":   now,
	})
	upd := s.db.WithContext(ctx).
		Model(&models.ExtractionJob{}).
		Where("job_id = ?", jobID).
		Where("status NOT IN (?, ?)", models.ExtractionStatusCompleted, models.ExtractionStatusFailed).
		Updates(assignments)
	if upd.Error != nil {
		return false, fmt.Errorf("extraction job store: mark terminal (update) %s: %w", jobID, upd.Error)
	}
	if upd.RowsAffected > 0 {
		return true, nil
	}

	// Step 2: no non-terminal row matched — insert a bare terminal row, racing
	// safely against any concurrent insert via OnConflict DoNothing.
	row := models.ExtractionJob{
		JobID:       models.DBVarchar(jobID),
		DocumentRef: models.DBVarchar(unknownDocumentRef), // bare-insert path only
		Status:      models.DBVarchar(status),
		ReasonCode:  reasonCodeVal,
		CompletedAt: &now,
	}
	ins := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{Col(dialect, "job_id")},
			DoNothing: true,
		}).
		Create(&row)
	if ins.Error != nil {
		return false, fmt.Errorf("extraction job store: mark terminal (insert) %s: %w", jobID, ins.Error)
	}
	return ins.RowsAffected > 0, nil
}
