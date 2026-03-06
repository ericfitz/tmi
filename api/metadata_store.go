package api

import (
	"context"
	"fmt"
	"strings"
)

// ErrMetadataKeyExists is returned when a Create operation encounters an existing key.
// ConflictingKeys contains the key name(s) that already exist.
type ErrMetadataKeyExists struct {
	ConflictingKeys []string
}

func (e *ErrMetadataKeyExists) Error() string {
	return fmt.Sprintf("metadata key(s) already exist: %s", strings.Join(e.ConflictingKeys, ", "))
}

// MetadataStore defines the interface for metadata operations with caching support
// Metadata supports POST operations and key-based access per the implementation plan
type MetadataStore interface {
	// CRUD operations
	Create(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Get(ctx context.Context, entityType, entityID, key string) (*Metadata, error)
	Update(ctx context.Context, entityType, entityID string, metadata *Metadata) error
	Delete(ctx context.Context, entityType, entityID, key string) error

	// Collection operations
	List(ctx context.Context, entityType, entityID string) ([]Metadata, error)

	// POST operations - adding metadata without specifying key upfront
	Post(ctx context.Context, entityType, entityID string, metadata *Metadata) error

	// Bulk operations
	BulkCreate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkUpdate(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkReplace(ctx context.Context, entityType, entityID string, metadata []Metadata) error
	BulkDelete(ctx context.Context, entityType, entityID string, keys []string) error

	// Key-based operations
	GetByKey(ctx context.Context, key string) ([]Metadata, error)
	ListKeys(ctx context.Context, entityType, entityID string) ([]string, error)

	// Cache management
	InvalidateCache(ctx context.Context, entityType, entityID string) error
	WarmCache(ctx context.Context, entityType, entityID string) error
}
