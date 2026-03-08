package api

import (
	"context"
)

// DocumentStore defines the interface for document operations with caching support
type DocumentStore interface {
	// CRUD operations
	Create(ctx context.Context, document *Document, threatModelID string) error
	Get(ctx context.Context, id string) (*Document, error)
	Update(ctx context.Context, document *Document, threatModelID string) error
	Delete(ctx context.Context, id string) error
	SoftDelete(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error
	GetIncludingDeleted(ctx context.Context, id string) (*Document, error)
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Document, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error)
	// Count returns total number of documents for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, documents []Document, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
