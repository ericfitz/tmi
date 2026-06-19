package api

import (
	"context"
	"time"
)

// InternalAuditActor holds denormalized user information for audit entries
// in the internal service layer. Uses plain strings (not openapi_types).
// The generated AuditActor type from the OpenAPI spec is used for API responses.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: denormalized user identity for internal audit entries, avoiding generated API types (pure)
type InternalAuditActor struct {
	Email       string `json:"email"`
	Provider    string `json:"provider"`
	ProviderID  string `json:"provider_id"`
	DisplayName string `json:"display_name"`
}

// AuditParams contains the parameters for recording an audit entry.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: parameters for recording a single mutation in the audit trail (pure)
type AuditParams struct {
	ThreatModelID string
	ObjectType    string
	ObjectID      string
	ChangeType    string // "created", "updated", "patched", "deleted", "rolled_back"
	Actor         InternalAuditActor
	PreviousState []byte  // full JSON of entity before mutation; nil for "created"
	CurrentState  []byte  // full JSON of entity after mutation; nil for "deleted"
	ChangeSummary *string // human-readable summary of what changed
}

// AuditFilters defines filtering criteria for querying audit entries.
// SEM@b7db260b5211c371fc74a10f72dfcd61bf7d1090: filtering criteria for querying audit entries by object, actor, or time range (pure)
type AuditFilters struct {
	ObjectType    *string
	ObjectID      *string
	ChangeType    *string
	ActorEmail    *string
	ActorProvider *string // admin cross-TM queries (#398)
	ThreatModelID *string // admin cross-TM queries (#398); per-TM reads still pass the scoped WHERE
	After         *time.Time
	Before        *time.Time
}

// AuditEntryResponse represents an audit entry as returned by the service layer.
// SEM@626c102e7b7f7ceffb64d01a6c51f618862c5f31: audit trail entry returned by the service layer, including actor and change metadata (pure)
type AuditEntryResponse struct {
	ID            string             `json:"id"`
	ThreatModelID string             `json:"threat_model_id"`
	ObjectType    string             `json:"object_type"`
	ObjectID      string             `json:"object_id"`
	Version       *int               `json:"version"` // nil means version snapshot has been pruned
	ChangeType    string             `json:"change_type"`
	Actor         InternalAuditActor `json:"actor"`
	ChangeSummary *string            `json:"change_summary"`
	CreatedAt     time.Time          `json:"created_at"`
}

// AuditServiceInterface defines operations for audit trail and version management.
// SEM@24454e2885191ae61007ef13d2194c563ebe6d37: contract for recording mutations, querying audit trails, and pruning version snapshots
type AuditServiceInterface interface {
	// RecordMutation records a mutation in the audit trail and creates a version snapshot.
	// The service internally computes reverse diffs and determines checkpoint intervals.
	RecordMutation(ctx context.Context, params AuditParams) error

	// GetThreatModelAuditTrailKeyset retrieves audit entries for a threat model and
	// its sub-objects using bidirectional keyset pagination ordered
	// (created_at DESC, id DESC). Returns (rows, total, prev, next) where total is
	// the filtered count ignoring the cursor (#457).
	GetThreatModelAuditTrailKeyset(ctx context.Context, threatModelID string, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error)

	// GetObjectAuditTrail retrieves audit entries for a specific object.
	GetObjectAuditTrail(ctx context.Context, objectType, objectID string, offset, limit int) ([]AuditEntryResponse, int, error)

	// GetAuditEntry retrieves a single audit entry by ID.
	GetAuditEntry(ctx context.Context, entryID string) (*AuditEntryResponse, error)

	// GetSnapshot reconstructs the full entity state at a given audit entry's version.
	// Returns the full JSON by finding the nearest checkpoint and applying diffs.
	// Returns an error if the version snapshot has been pruned.
	GetSnapshot(ctx context.Context, entryID string) ([]byte, error)

	// PruneAuditEntries removes audit entries older than the configured retention period.
	// Returns the number of entries pruned.
	PruneAuditEntries(ctx context.Context) (int, error)

	// PruneVersionSnapshots removes version snapshots outside the configured retention window.
	// Always stops at checkpoint boundaries to ensure remaining diffs can be reconstructed.
	// Audit entries are immutable and keep their version numbers; rollback to a pruned
	// version returns an error (the handler maps it to 410 Gone).
	// Returns the number of snapshots pruned.
	PruneVersionSnapshots(ctx context.Context) (int, error)

	// PruneOrphanedVersionSnapshots removes version snapshots whose referenced
	// entity no longer exists (e.g. children orphaned by the threat-model
	// hard-delete cascade, which removes the rows but not their snapshots, #458).
	// Only snapshots aged past the append-only delete floor are removed; younger
	// orphans are left for a later cycle. Returns the number of snapshots removed.
	PruneOrphanedVersionSnapshots(ctx context.Context) (int, error)

	// PurgeTombstones hard-deletes entities that have been soft-deleted for longer than
	// the tombstone retention period. Also cleans up associated metadata and version snapshots.
	// Audit entries are append-only and are never deleted.
	// Returns the number of entities purged.
	PurgeTombstones(ctx context.Context) (int, error)

	// PruneSystemAuditEntries removes system audit entries older than the
	// configured retention period (SYSTEM_AUDIT_RETENTION_DAYS, default 365,
	// minimum 90). Returns the number of entries pruned.
	PruneSystemAuditEntries(ctx context.Context) (int, error)

	// ListAuditEntriesAdmin lists audit entries across ALL threat models with
	// bidirectional keyset pagination. Returns (rows, total, prev, next) (#464).
	ListAuditEntriesAdmin(ctx context.Context, limit int, cursor *auditCursor, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error)
	// AroundAuditEntriesAdmin returns a page of `limit` entries centered on
	// anchorID. Returns errAuditAnchorNotFound for an unknown id (#464).
	AroundAuditEntriesAdmin(ctx context.Context, limit int, anchorID string, filters *AuditFilters) ([]AuditEntryResponse, int, *string, *string, error)
}
