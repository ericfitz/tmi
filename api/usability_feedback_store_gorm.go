package api

import (
	"context"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/dberrors"
	"github.com/ericfitz/tmi/internal/slogging"
	"gorm.io/gorm"
)

// GormUsabilityFeedbackRepository implements UsabilityFeedbackRepository with GORM.
type GormUsabilityFeedbackRepository struct {
	db *gorm.DB
}

// NewGormUsabilityFeedbackRepository constructs a repository.
func NewGormUsabilityFeedbackRepository(db *gorm.DB) *GormUsabilityFeedbackRepository {
	return &GormUsabilityFeedbackRepository{db: db}
}

// Create inserts a new feedback row. The model's BeforeCreate hook generates the
// UUID if none is supplied. CreatedAt is autoCreateTime.
func (r *GormUsabilityFeedbackRepository) Create(ctx context.Context, fb *models.UsabilityFeedback) error {
	if err := r.db.WithContext(ctx).Create(fb).Error; err != nil {
		slogging.Get().Error("UsabilityFeedback Create failed: %v", err)
		return dberrors.Classify(err)
	}
	return nil
}

// Get returns a feedback row by ID, or NotFound if absent.
func (r *GormUsabilityFeedbackRepository) Get(ctx context.Context, id string) (*models.UsabilityFeedback, error) {
	var fb models.UsabilityFeedback
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&fb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, dberrors.ErrNotFound
	}
	if err != nil {
		return nil, dberrors.Classify(err)
	}
	return &fb, nil
}

// List returns feedback rows matching the filter, paginated.
func (r *GormUsabilityFeedbackRepository) List(ctx context.Context, filter UsabilityFeedbackListFilter, offset, limit int) ([]models.UsabilityFeedback, error) {
	q := r.applyFilter(r.db.WithContext(ctx), filter)
	var rows []models.UsabilityFeedback
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	return rows, nil
}

// Count returns the row count for the filter.
func (r *GormUsabilityFeedbackRepository) Count(ctx context.Context, filter UsabilityFeedbackListFilter) (int64, error) {
	q := r.applyFilter(r.db.WithContext(ctx).Model(&models.UsabilityFeedback{}), filter)
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return n, nil
}

func (r *GormUsabilityFeedbackRepository) applyFilter(q *gorm.DB, filter UsabilityFeedbackListFilter) *gorm.DB {
	if filter.Sentiment != "" {
		q = q.Where("sentiment = ?", filter.Sentiment)
	}
	if filter.ClientID != "" {
		q = q.Where("client_id = ?", filter.ClientID)
	}
	if filter.Surface != "" {
		q = q.Where("surface = ?", filter.Surface)
	}
	if !filter.CreatedAfter.IsZero() {
		q = q.Where("created_at >= ?", filter.CreatedAfter)
	}
	if !filter.CreatedBefore.IsZero() {
		q = q.Where("created_at < ?", filter.CreatedBefore)
	}
	_ = time.Time{} // keep `time` referenced if filter is empty
	return q
}
