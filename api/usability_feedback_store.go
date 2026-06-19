package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
)

// UsabilityFeedbackListFilter controls UsabilityFeedbackRepository.List.
// Zero/empty fields are ignored (no filter applied).
// SEM@c02756463276ea096f49221b581d44cd5b21ea12: filter criteria for listing usability feedback by sentiment, surface, or date range (pure)
type UsabilityFeedbackListFilter struct {
	Sentiment     string // "up", "down", or "" for any
	ClientID      string
	Surface       string
	CreatedAfter  time.Time // zero value = no lower bound
	CreatedBefore time.Time // zero value = no upper bound
}

// UsabilityFeedbackRepository defines persistence for usability_feedback rows.
// SEM@c02756463276ea096f49221b581d44cd5b21ea12: interface for persisting and querying usability feedback records (reads DB)
type UsabilityFeedbackRepository interface {
	Create(ctx context.Context, fb *models.UsabilityFeedback) error
	Get(ctx context.Context, id string) (*models.UsabilityFeedback, error)
	List(ctx context.Context, filter UsabilityFeedbackListFilter, offset, limit int) ([]models.UsabilityFeedback, error)
	Count(ctx context.Context, filter UsabilityFeedbackListFilter) (int64, error)
}
