package api

import (
	"context"
)

// TriageNoteStore defines the interface for triage note operations.
// Triage notes are append-only (create + read), so no Update/Delete/Patch methods.
type TriageNoteStore interface {
	// Create creates a new triage note with an auto-assigned sequential ID
	Create(ctx context.Context, note *TriageNote, surveyResponseID string, creatorInternalUUID string) error
	// Get retrieves a specific triage note by survey response ID and note ID
	Get(ctx context.Context, surveyResponseID string, noteID int) (*TriageNote, error)
	// List returns triage notes for a survey response with pagination
	List(ctx context.Context, surveyResponseID string, offset, limit int) ([]TriageNote, error)
	// Count returns the total number of triage notes for a survey response
	Count(ctx context.Context, surveyResponseID string) (int, error)
}
