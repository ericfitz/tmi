package api

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ThreatFilter defines filtering criteria for threats
// SEM@cd9bd2528bd9cff1ec072491f5cbcbe1cb9219ac: filter criteria for threat list queries including scores, dates, and pagination (pure)
type ThreatFilter struct {
	// Basic filters
	Name        *string
	Description *string
	ThreatType  []string
	Severity    []string
	Priority    []string
	Status      []string
	Mitigated   *bool
	DiagramID   *uuid.UUID
	CellID      *uuid.UUID

	// Score comparison filters
	ScoreGT *float32
	ScoreLT *float32
	ScoreEQ *float32
	ScoreGE *float32
	ScoreLE *float32

	// Date filters
	CreatedAfter   *time.Time
	CreatedBefore  *time.Time
	ModifiedAfter  *time.Time
	ModifiedBefore *time.Time

	// Sorting and pagination
	Sort   *string
	Offset int
	Limit  int
}

// normalizeSeverity is a no-op that returns severity as-is without modification
// Severity is now a free-form string field and should not be normalized
// SEM@b110c1a606d320e3e927019adfe2962042680f18: return severity string unchanged; severity is free-form and requires no normalization (pure)
func normalizeSeverity(severity string) string {
	return severity
}

// ThreatRepository defines the interface for threat operations with caching support
// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: store interface for threat CRUD, soft-delete, restore, bulk, patch, and cache operations
type ThreatRepository interface {
	// CRUD operations
	Create(ctx context.Context, threat *Threat) error
	Get(ctx context.Context, id string) (*Threat, error)
	Update(ctx context.Context, threat *Threat) error
	Delete(ctx context.Context, id string) error
	SoftDelete(ctx context.Context, id string) error
	Restore(ctx context.Context, id string) error
	HardDelete(ctx context.Context, id string) error
	GetIncludingDeleted(ctx context.Context, id string) (*Threat, error)

	// List operations with filtering, sorting and pagination
	// Returns: items, total count (before pagination), error
	List(ctx context.Context, threatModelID string, filter ThreatFilter) ([]Threat, int, error)

	// PATCH operations for granular updates
	Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error)

	// Bulk operations
	BulkCreate(ctx context.Context, threats []Threat) error
	BulkUpdate(ctx context.Context, threats []Threat) error

	// Cache management
	InvalidateCache(ctx context.Context, id string) error
	WarmCache(ctx context.Context, threatModelID string) error
}
