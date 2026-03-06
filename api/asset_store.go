package api

import (
	"context"
)

// AssetStore defines the interface for asset operations with caching support
type AssetStore interface {
	// CRUD operations
	Create(ctx context.Context, asset *Asset, threatModelID string) error
	Get(ctx context.Context, id string) (*Asset, error)
	Update(ctx context.Context, asset *Asset, threatModelID string) error
	Delete(ctx context.Context, id string) error
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Asset, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Asset, error)
	// Count returns total number of assets for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, assets []Asset, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
