package api

import (
	"context"
)

// RepositoryRepository defines the interface for repository operations with caching support.
// (Yes, the doubled name is awkward; the entity is named "Repository" in the API spec.)
// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: interface for CRUD, soft-delete, patch, pagination, bulk create, and cache operations on repository entities
type RepositoryRepository interface {
	// CRUD operations
	Create(ctx context.Context, repository *Repository, threatModelID string) error
	Get(ctx context.Context, id string) (*Repository, error)
	Update(ctx context.Context, repository *Repository, threatModelID string) error
	Delete(ctx context.Context, id string) error
	SoftDelete(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error
	GetIncludingDeleted(ctx context.Context, id string) (*Repository, error)
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Repository, error)

	// List operations with pagination
	List(ctx context.Context, threatModelID string, offset, limit int) ([]Repository, error)
	// Count returns total number of repositories for a threat model
	Count(ctx context.Context, threatModelID string) (int, error)

	// Bulk operations
	BulkCreate(ctx context.Context, repositorys []Repository, threatModelID string) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
