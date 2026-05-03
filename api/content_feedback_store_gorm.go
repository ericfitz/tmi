package api

import (
	"context"
	"errors"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormContentFeedbackRepository implements ContentFeedbackRepository with GORM.
type GormContentFeedbackRepository struct {
	db *gorm.DB
}

// NewGormContentFeedbackRepository constructs a repository.
func NewGormContentFeedbackRepository(db *gorm.DB) *GormContentFeedbackRepository {
	return &GormContentFeedbackRepository{db: db}
}

// Create inserts a feedback row.
func (r *GormContentFeedbackRepository) Create(ctx context.Context, fb *models.ContentFeedback) error {
	if err := r.db.WithContext(ctx).Create(fb).Error; err != nil {
		slogging.Get().Error("ContentFeedback Create failed: %v", err)
		return dberrors.Classify(err)
	}
	return nil
}

// Get returns a feedback row by ID, or NotFound if absent.
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
func (r *GormContentFeedbackRepository) Count(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter) (int64, error) {
	q := r.db.WithContext(ctx).Model(&models.ContentFeedback{}).Where("threat_model_id = ?", threatModelID)
	q = r.applyFilter(q, filter)
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return n, nil
}

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
