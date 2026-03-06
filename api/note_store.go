package api

import (
	"context"
)

// NoteStore defines the interface for note operations with caching support
type NoteStore interface {
	// CRUD operations
	Create(ctx context.Context, note *Note, threatModelID string) error
	Get(ctx context.Context, id string) (*Note, error)
	Update(ctx context.Context, note *Note, threatModelID string) error
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Note, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Note, error)
	// Count returns total number of notes for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
