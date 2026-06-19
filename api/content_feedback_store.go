package api

import (
	"context"
	"errors"

	"github.com/ericfitz/tmi/api/models"
)

// ErrContentFeedbackTargetNotFound is returned by CreateWithTargetCheck when
// the target row does not exist (or does not belong to the named threat model)
// at the moment of the locked SELECT inside the create transaction.
var ErrContentFeedbackTargetNotFound = errors.New("content feedback target not found")

// ContentFeedbackListFilter controls ContentFeedbackRepository.List.
// Zero/empty fields are ignored.
// SEM@87c89d0c54dceab6486f4f612415b28f2d4b30db: filter parameters for listing content feedback entries (pure)
type ContentFeedbackListFilter struct {
	TargetType          string
	TargetID            string
	Sentiment           string
	FalsePositiveReason string
}

// ContentFeedbackTargetRef identifies the row that a feedback entry refers to.
// Table is the GORM table name (e.g., "threats", "notes", "diagrams").
// SEM@1c63bfe9bdfd225380a2a4e2960fef14b3437996: identify the target row a feedback entry refers to by table, ID, and threat model (pure)
type ContentFeedbackTargetRef struct {
	Table         string
	TargetID      string
	ThreatModelID string
}

// ContentFeedbackRepository defines persistence for content_feedback rows.
// SEM@1c63bfe9bdfd225380a2a4e2960fef14b3437996: persistence interface for creating, fetching, listing, and counting content feedback rows
type ContentFeedbackRepository interface {
	// Create inserts a feedback row without verifying target existence.
	// Prefer CreateWithTargetCheck for handler paths that take user input.
	Create(ctx context.Context, fb *models.ContentFeedback) error
	// CreateWithTargetCheck verifies the target row exists in the threat model
	// and inserts the feedback row in a single transaction with a row lock on
	// the target. This closes the SELECT-then-INSERT TOCTOU window where a
	// concurrent delete of the target could leave a feedback row pointing at a
	// non-existent entity.
	//
	// Returns ErrTargetNotFound if the target row does not exist (or does not
	// belong to the threat model) at the moment of the locked SELECT.
	CreateWithTargetCheck(ctx context.Context, fb *models.ContentFeedback, target ContentFeedbackTargetRef) error
	Get(ctx context.Context, id string) (*models.ContentFeedback, error)
	List(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter, offset, limit int) ([]models.ContentFeedback, error)
	Count(ctx context.Context, threatModelID string, filter ContentFeedbackListFilter) (int64, error)
}
