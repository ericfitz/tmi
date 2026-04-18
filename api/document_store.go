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
	// ListByAccessStatus returns documents with the given access status across all threat models.
	ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error)
	// Count returns total number of documents for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, documents []Document, threatModelID string) error

	// UpdateAccessStatus sets the access tracking fields on a document.
	UpdateAccessStatus(ctx context.Context, id string, accessStatus string, contentSource string) error

	// UpdateAccessStatusWithDiagnostics sets the access tracking fields on a document,
	// including the diagnostic reason code and detail. reasonCode may be empty to clear
	// any existing diagnostic. reasonDetail should be empty unless reasonCode == "other".
	// access_status_updated_at is set to NOW().
	UpdateAccessStatusWithDiagnostics(
		ctx context.Context,
		id string,
		accessStatus string,
		contentSource string,
		reasonCode string,
		reasonDetail string,
	) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
