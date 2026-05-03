package api

import (
	"context"

	"github.com/ericfitz/tmi/api/models"
)

// ContentFeedbackListFilter controls ContentFeedbackRepository.List.
// Zero/empty fields are ignored.
type ContentFeedbackListFilter struct {
	TargetType          string
	TargetID            string
	Sentiment           string
	FalsePositiveReason string
}

// ContentFeedbackRepository defines persistence for content_feedback rows.
type ContentFeedbackRepository interface {
	Create(ctx context.Context, fb *models.ContentFeedback) error
	Get(ctx context.Context, id string) (*models.ContentFeedback, error)
	List(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter, offset, limit int) ([]models.ContentFeedback, error)
	Count(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter) (int64, error)
}
