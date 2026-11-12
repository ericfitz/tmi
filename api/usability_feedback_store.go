package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
)

// UsabilityFeedbackListFilter controls UsabilityFeedbackRepository.List.
// Zero/empty fields are ignored (no filter applied).
type UsabilityFeedbackListFilter struct {
	Sentiment     string // "up", "down", or "" for any
	ClientID      string
	Surface       string
	CreatedAfter  time.Time // zero value = no lower bound
	CreatedBefore time.Time // zero value = no upper bound
}

// UsabilityFeedbackRepository defines persistence for usability_feedback rows.
type UsabilityFeedbackRepository interface {
	Create(ctx context.Context, fb *models.UsabilityFeedback) error
	Get(ctx context.Context, id string) (*models.UsabilityFeedback, error)
	List(ctx context.Context, filter UsabilityFeedbackListFilter, offset, limit int) ([]models.UsabilityFeedback, error)
	Count(ctx context.Context, filter UsabilityFeedbackListFilter) (int64, error)
}
