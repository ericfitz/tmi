package api

import (
	"context"
	"time"
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

	// GetAccessReason returns the diagnostic fields (reason_code, reason_detail,
	// access_status_updated_at) for a document. Returns empty strings and nil
	// time if no diagnostic has been set. Does not return an error if the
	// document has no reason (normal state); returns an error only if the
	// document doesn't exist or the DB query fails.
	GetAccessReason(ctx context.Context, id string) (reasonCode string, reasonDetail string, updatedAt *time.Time, err error)

	// ClearPickerMetadataForOwner nulls picker metadata and resets access_status
	// to 'unknown' for every document whose picker_provider_id == providerID and
	// whose parent threat model's owner_internal_uuid == ownerInternalUUID.
	// Used by the content-token un-link cascade: when a user un-links a
	// delegated provider, all documents they picker-attached under that provider
	// revert to a non-picker state and will re-validate via URL-based dispatch
	// on next access attempt.
	// Returns the number of affected rows.
	ClearPickerMetadataForOwner(ctx context.Context, ownerInternalUUID, providerID string) (int64, error)

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
