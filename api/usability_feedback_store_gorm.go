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
// SEM@e6358cd2f50a1221d6e8c0f0fe40c31b27feb5d2: GORM-backed repository for persisting and querying usability feedback records
type GormUsabilityFeedbackRepository struct {
	db *gorm.DB
}

// NewGormUsabilityFeedbackRepository constructs a repository.
// SEM@e6358cd2f50a1221d6e8c0f0fe40c31b27feb5d2: build a GormUsabilityFeedbackRepository backed by the given DB connection (pure)
func NewGormUsabilityFeedbackRepository(db *gorm.DB) *GormUsabilityFeedbackRepository {
	return &GormUsabilityFeedbackRepository{db: db}
}

// Create inserts a new feedback row. The model's BeforeCreate hook generates the
// UUID if none is supplied. CreatedAt is set explicitly here (not via GORM's
// autoCreateTime) for Oracle compatibility — the Threat model uses the same
// pattern to avoid gorm-oracle's RETURNING INTO interaction on high-volume
// inserts (see #380).
// SEM@f0dc4a18f547f807534300d1d5683b959965e3ab: store a new usability feedback row, setting created_at explicitly for Oracle compatibility (reads DB)
func (r *GormUsabilityFeedbackRepository) Create(ctx context.Context, fb *models.UsabilityFeedback) error {
	if fb.CreatedAt.IsZero() {
		fb.CreatedAt = time.Now().UTC()
	}
	if err := r.db.WithContext(ctx).Create(fb).Error; err != nil {
		slogging.Get().Error("UsabilityFeedback Create failed: %v", err)
		return dberrors.Classify(err)
	}
	return nil
}

// Get returns a feedback row by ID, or NotFound if absent.
// SEM@e6358cd2f50a1221d6e8c0f0fe40c31b27feb5d2: fetch a usability feedback record by ID, returning NotFound if absent (reads DB)
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
// SEM@e6358cd2f50a1221d6e8c0f0fe40c31b27feb5d2: list usability feedback records matching a filter with pagination, ordered by created_at desc (reads DB)
func (r *GormUsabilityFeedbackRepository) List(ctx context.Context, filter UsabilityFeedbackListFilter, offset, limit int) ([]models.UsabilityFeedback, error) {
	q := r.applyFilter(r.db.WithContext(ctx), filter)
	var rows []models.UsabilityFeedback
	if err := q.Order("created_at DESC").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		return nil, dberrors.Classify(err)
	}
	return rows, nil
}

// Count returns the row count for the filter.
// SEM@e6358cd2f50a1221d6e8c0f0fe40c31b27feb5d2: count usability feedback records matching a filter (reads DB)
func (r *GormUsabilityFeedbackRepository) Count(ctx context.Context, filter UsabilityFeedbackListFilter) (int64, error) {
	q := r.applyFilter(r.db.WithContext(ctx).Model(&models.UsabilityFeedback{}), filter)
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return 0, dberrors.Classify(err)
	}
	return n, nil
}

// SEM@f0dc4a18f547f807534300d1d5683b959965e3ab: apply sentiment, client, surface, and date range constraints to a GORM query (pure)
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
	return q
}
