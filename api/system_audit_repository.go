// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/auth"
	"gorm.io/gorm"
)

// SystemAuditRepository is the data-access surface for system_audit_entries.
// Read methods are minimal in Part 1 of #355 — the full read API is tracked
// in #398. Part 1 needs Create (write path) and ListByActor (test
// verification + lightweight investigator query).
// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: data-access interface for system audit entries with keyset pagination and streaming
type SystemAuditRepository interface {
	Create(ctx context.Context, entry models.SystemAuditEntry) error
	ListByActor(ctx context.Context, actorEmail string, from, to time.Time) ([]models.SystemAuditEntry, error)
	// List returns entries matching the filter, newest first, with bidirectional
	// keyset pagination. Returns (page, total matching the filter, prev cursor,
	// next cursor). Cursors are nil when no further rows exist that direction
	// (#398, #464).
	List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, *string, error)
	// Around returns a page of f.Limit entries centered on anchorID (~half newer,
	// ~half older). Returns errAuditAnchorNotFound when the id is unknown (#464).
	Around(ctx context.Context, f SystemAuditFilter, anchorID string) ([]models.SystemAuditEntry, int, *string, *string, error)
	// StreamFiltered keyset-iterates the entire filtered set newest-first in
	// batches of `batch`, invoking fn per batch until exhausted. Ignores
	// f.Limit/f.Cursor. Used by CSV/NDJSON export (#464).
	StreamFiltered(ctx context.Context, f SystemAuditFilter, batch int, fn func([]models.SystemAuditEntry) error) error
	// GetByID returns the entry or nil when not found.
	GetByID(ctx context.Context, id string) (*models.SystemAuditEntry, error)
}

// SystemAuditFilter carries the admin query dimensions for system audit
// entries (#398). All filter fields are optional and AND-combined.
// SEM@36f789f9e7fb93d8201a7ff2ce6a132a2a6dce47: carry optional AND-combined filter dimensions for querying system audit entries (pure)
type SystemAuditFilter struct {
	ActorEmail    *string
	ActorProvider *string
	CreatedAfter  *time.Time
	CreatedBefore *time.Time
	HTTPMethod    *string
	PathPrefix    *string // matched as LIKE '<escaped>%' ESCAPE '\'
	FieldPath     *string
	Limit         int
	Cursor        *auditCursor
}

// SEM@17032cb417037389f68b10b92286265bafbec4dd: GORM-backed implementation of SystemAuditRepository (reads DB)
type systemAuditRepoGORM struct {
	db *gorm.DB
}

// NewSystemAuditRepository constructs a GORM-backed SystemAuditRepository.
// SEM@17032cb417037389f68b10b92286265bafbec4dd: build a GORM-backed SystemAuditRepository from a database connection (pure)
func NewSystemAuditRepository(db *gorm.DB) SystemAuditRepository {
	return &systemAuditRepoGORM{db: db}
}

// Create inserts a new audit row. The model's BeforeCreate hook generates
// a UUID if ID is empty.
// SEM@17032cb417037389f68b10b92286265bafbec4dd: insert a new system audit entry row into the database (reads DB)
func (r *systemAuditRepoGORM) Create(ctx context.Context, entry models.SystemAuditEntry) error {
	return r.db.WithContext(ctx).Create(&entry).Error
}

// ListByActor returns audit rows for the given actor email within the
// inclusive [from, to] window, ordered by created_at descending.
// SEM@17032cb417037389f68b10b92286265bafbec4dd: fetch audit entries for an actor email within a time window, newest first (reads DB)
func (r *systemAuditRepoGORM) ListByActor(ctx context.Context, actorEmail string, from, to time.Time) ([]models.SystemAuditEntry, error) {
	var rows []models.SystemAuditEntry
	err := r.db.WithContext(ctx).
		Where("actor_email = ? AND created_at >= ? AND created_at <= ?", actorEmail, from, to).
		Order("created_at DESC").
		Find(&rows).Error
	return rows, err
}

// escapeLikePrefix escapes LIKE metacharacters and appends the wildcard so a
// user-supplied prefix is matched literally. The ESCAPE '\' clause is
// specified explicitly — required on Oracle, harmless on PostgreSQL.
// SEM@36f789f9e7fb93d8201a7ff2ce6a132a2a6dce47: escape LIKE metacharacters in a prefix string and append a wildcard for safe DB pattern matching (pure)
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s) + "%"
}

// SEM@36f789f9e7fb93d8201a7ff2ce6a132a2a6dce47: apply all non-nil SystemAuditFilter fields as AND WHERE clauses to a GORM query (pure)
func (r *systemAuditRepoGORM) applyFilter(q *gorm.DB, f SystemAuditFilter) *gorm.DB {
	if f.ActorEmail != nil {
		q = q.Where("actor_email = ?", *f.ActorEmail)
	}
	if f.ActorProvider != nil {
		q = q.Where("actor_provider = ?", *f.ActorProvider)
	}
	if f.CreatedAfter != nil {
		q = q.Where("created_at >= ?", *f.CreatedAfter)
	}
	if f.CreatedBefore != nil {
		q = q.Where("created_at <= ?", *f.CreatedBefore)
	}
	if f.HTTPMethod != nil {
		q = q.Where("http_method = ?", *f.HTTPMethod)
	}
	if f.PathPrefix != nil {
		q = q.Where(`http_path LIKE ? ESCAPE '\'`, escapeLikePrefix(*f.PathPrefix))
	}
	if f.FieldPath != nil {
		q = q.Where("field_path = ?", *f.FieldPath)
	}
	return q
}

// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: extract the keyset pagination key (created_at, id) from a system audit entry (pure)
func sysAuditKeyOf(e models.SystemAuditEntry) (time.Time, string) {
	return e.CreatedAt, string(e.ID)
}

// List returns system audit entries matching the filter with bidirectional
// keyset pagination ordered (created_at DESC, id DESC).
// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: fetch a keyset-paginated page of system audit entries matching a filter, with total count (reads DB)
func (r *systemAuditRepoGORM) List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, *string, error) {
	total, err := r.countFiltered(ctx, f)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	newQuery := func() *gorm.DB {
		return r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	}
	rows, prev, next, err := fetchKeysetPage(newQuery, f.Cursor, f.Limit, sysAuditKeyOf)
	if err != nil {
		return nil, 0, nil, nil, fmt.Errorf("list system audit entries: %w", err)
	}
	return rows, total, prev, next, nil
}

// Around returns a page centered on anchorID.
// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: fetch a page of audit entries centered on an anchor entry ID, with total count (reads DB)
func (r *systemAuditRepoGORM) Around(ctx context.Context, f SystemAuditFilter, anchorID string) ([]models.SystemAuditEntry, int, *string, *string, error) {
	total, err := r.countFiltered(ctx, f)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	newQuery := func() *gorm.DB {
		return r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	}
	fetchAnchor := func() (*models.SystemAuditEntry, error) {
		return r.GetByID(ctx, anchorID) // by id, ignoring filters
	}
	rows, prev, next, err := fetchAroundPage(newQuery, fetchAnchor, f.Limit, sysAuditKeyOf)
	if err != nil {
		if errors.Is(err, errAuditAnchorNotFound) {
			return nil, 0, nil, nil, err
		}
		return nil, 0, nil, nil, fmt.Errorf("around system audit entries: %w", err)
	}
	return rows, total, prev, next, nil
}

// StreamFiltered keyset-iterates the entire filtered set newest-first.
// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: iterate all filtered audit entries newest-first in batches, invoking a callback per batch (reads DB)
func (r *systemAuditRepoGORM) StreamFiltered(ctx context.Context, f SystemAuditFilter, batch int, fn func([]models.SystemAuditEntry) error) error {
	if batch <= 0 {
		batch = 1000
	}
	var cursor *auditCursor
	for {
		q := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
		if cursor != nil {
			q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
				cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
		}
		var rows []models.SystemAuditEntry
		if err := q.Order("created_at DESC, id DESC").Limit(batch).Find(&rows).Error; err != nil {
			return fmt.Errorf("stream system audit entries: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		if err := fn(rows); err != nil {
			return err
		}
		if len(rows) < batch {
			return nil
		}
		last := rows[len(rows)-1]
		cursor = &auditCursor{CreatedAt: last.CreatedAt, ID: string(last.ID), Dir: dirForward}
	}
}

// countFiltered returns the total rows matching the filter (ignoring cursor).
// SEM@f12e77c453fd57852ba9396c20645e34e8c16784: count total system audit entries matching a filter, ignoring pagination cursor (reads DB)
func (r *systemAuditRepoGORM) countFiltered(ctx context.Context, f SystemAuditFilter) (int, error) {
	var total int64
	if err := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f).Count(&total).Error; err != nil {
		return 0, fmt.Errorf("count system audit entries: %w", err)
	}
	return int(total), nil
}

// GetByID returns the system audit entry with the given ID, or nil if not found.
// SEM@36f789f9e7fb93d8201a7ff2ce6a132a2a6dce47: fetch a single system audit entry by ID, returning nil if not found (reads DB)
func (r *systemAuditRepoGORM) GetByID(ctx context.Context, id string) (*models.SystemAuditEntry, error) {
	var row models.SystemAuditEntry
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get system audit entry: %w", err)
	}
	return &row, nil
}

// StepUpAuditAdapter adapts a SystemAuditRepository to the auth package's
// SystemAuditWriter interface, mapping auth.SystemAuditRecord to
// models.SystemAuditEntry. Used exclusively by /oauth2/step_up (#397) so
// package auth can write system audit rows without importing package api.
// SEM@dd66d35bda6952fa6d623976b1adb6177685fe6d: adapter bridging auth.SystemAuditWriter to a SystemAuditRepository (pure)
type StepUpAuditAdapter struct {
	repo SystemAuditRepository
}

// NewStepUpAuditAdapter constructs the adapter.
// SEM@13ccacf4852fae0cb6fc35e8197cb04b2a0df1e9: build a StepUpAuditAdapter wrapping a SystemAuditRepository (pure)
func NewStepUpAuditAdapter(repo SystemAuditRepository) *StepUpAuditAdapter {
	return &StepUpAuditAdapter{repo: repo}
}

// WriteSystemAudit implements auth.SystemAuditWriter.
// SEM@87d6f75bc3aecf3edd6c4103567546955c1afadf: convert an auth audit record to a DB model and persist it (writes DB)
func (a *StepUpAuditAdapter) WriteSystemAudit(ctx context.Context, rec auth.SystemAuditRecord) error {
	entry := models.SystemAuditEntry{
		ActorEmail:       models.DBVarchar(rec.ActorEmail),
		ActorProvider:    models.DBVarchar(rec.ActorProvider),
		ActorProviderID:  models.DBVarchar(rec.ActorProviderID),
		ActorDisplayName: models.DBVarchar(rec.ActorDisplayName),
		HTTPMethod:       models.DBVarchar(rec.HTTPMethod),
		HTTPPath:         models.DBText(rec.HTTPPath),
		FieldPath:        models.DBVarchar(rec.FieldPath),
		OldValueRedacted: models.NewNullableDBText(rec.OldValueRedacted),
		NewValueRedacted: models.NewNullableDBText(rec.NewValueRedacted),
		ChangeSummary:    models.NewNullableDBText(rec.ChangeSummary),
	}
	if !rec.CreatedAt.IsZero() {
		entry.CreatedAt = rec.CreatedAt
	}
	return a.repo.Create(ctx, entry)
}
