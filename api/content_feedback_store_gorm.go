package api

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	authdb "github.com/ericfitz/tmi/auth/db"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormContentFeedbackRepository implements ContentFeedbackRepository with GORM.
// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: GORM-backed repository for content feedback records (reads DB)
type GormContentFeedbackRepository struct {
	db *gorm.DB
}

// NewGormContentFeedbackRepository constructs a repository.
// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: build a content feedback repository backed by the provided GORM database (pure)
func NewGormContentFeedbackRepository(db *gorm.DB) *GormContentFeedbackRepository {
	return &GormContentFeedbackRepository{db: db}
}

// Create inserts a feedback row. CreatedAt is set explicitly here (not via
// GORM's autoCreateTime) for Oracle compatibility — matches the Threat-model
// pattern (see #380).
// SEM@f0dc4a18f547f807534300d1d5683b959965e3ab: insert a content feedback row with an explicit timestamp for Oracle compatibility (reads DB)
func (r *GormContentFeedbackRepository) Create(ctx context.Context, fb *models.ContentFeedback) error {
	if fb.CreatedAt.IsZero() {
		fb.CreatedAt = time.Now().UTC()
	}
	if err := r.db.WithContext(ctx).Create(fb).Error; err != nil {
		slogging.Get().Error("ContentFeedback Create failed: %v", err)
		return dberrors.Classify(err)
	}
	return nil
}

// CreateWithTargetCheck verifies the target row exists in the named threat
// model and inserts the feedback row inside a single transaction. The target
// row is acquired with SELECT ... FOR UPDATE so a concurrent DELETE of the
// target either waits for this transaction to commit or finds the row gone.
//
// On Oracle and PostgreSQL this is a real row lock; on SQLite (used in unit
// tests) GORM's clause.Locking is silently ignored and the check still serializes
// via the surrounding transaction's default isolation.
// SEM@d0742bff5d3b93b3ab7b22df0377398a720a8d9c: insert content feedback only if its target row exists in the threat model, using a row lock (reads DB)
func (r *GormContentFeedbackRepository) CreateWithTargetCheck(ctx context.Context, fb *models.ContentFeedback, target ContentFeedbackTargetRef) error {
	if fb.CreatedAt.IsZero() {
		fb.CreatedAt = time.Now().UTC()
	}
	return authdb.WithRetryableGormTransaction(ctx, r.db, authdb.DefaultRetryConfig(), func(tx *gorm.DB) error {
		// SEM@1c63bfe9bdfd225380a2a4e2960fef14b3437996: local struct holding a single ID column for targeted SELECT queries (pure)
		type idRow struct{ ID string }
		var got idRow
		err := tx.Table(target.Table).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("id").
			Where("id = ? AND threat_model_id = ?", target.TargetID, target.ThreatModelID).
			First(&got).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrContentFeedbackTargetNotFound
		}
		if err != nil {
			return dberrors.Classify(err)
		}
		if err := tx.Create(fb).Error; err != nil {
			slogging.Get().Error("ContentFeedback CreateWithTargetCheck insert failed: %v", err)
			return dberrors.Classify(err)
		}
		return nil
	})
}

// Get returns a feedback row by ID, or NotFound if absent.
// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: fetch a content feedback row by ID, returning not-found if absent (reads DB)
func (r *GormContentFeedbackRepository) Get(ctx context.Context, id string) (*models.ContentFeedback, error) {
	var fb models.ContentFeedback
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&fb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dberrors.ErrNotFound
	}
	if err != nil {
		return nil, dberrors.Classify(err)
	}
	return &fb, nil
}

// List returns feedback for a threat model matching the filter.
// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: list content feedback rows for a threat model matching a filter with pagination (reads DB)
func (r *GormContentFeedbackRepository) List(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter, offset, limit int) ([]models.ContentFeedback, error) {
	q := r.db.WithContext(ctx).Where("threat_model_id = ?", threatModelID)
	q = r.applyFilter(q, filter)
	var rows []models.ContentFeedback
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	return rows, nil
}

// Count returns the row count for a threat model and filter.
// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: count content feedback rows for a threat model matching a filter (reads DB)
func (r *GormContentFeedbackRepository) Count(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter) (int64, error) {
	q := r.db.WithContext(ctx).Model(&models.ContentFeedback{}).Where("threat_model_id = ?", threatModelID)
	q = r.applyFilter(q, filter)
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return n, nil
}

// SEM@7189df9e563ddcdb55d93d17322355f4ee6c57d0: append WHERE clauses to a query based on content feedback filter fields (pure)
func (r *GormContentFeedbackRepository) applyFilter(q *gorm.DB, filter ContentFeedbackListFilter) *gorm.DB {
	if filter.TargetType != "" {
		q = q.Where("target_type = ?", filter.TargetType)
	}
	if filter.TargetID != "" {
		q = q.Where("target_id = ?", filter.TargetID)
	}
	if filter.Sentiment != "" {
		q = q.Where("sentiment = ?", filter.Sentiment)
	}
	if filter.FalsePositiveReason != "" {
		q = q.Where("false_positive_reason = ?", filter.FalsePositiveReason)
	}
	return q
}
