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
type SystemAuditRepository interface {
	Create(ctx context.Context, entry models.SystemAuditEntry) error
	ListByActor(ctx context.Context, actorEmail string, from, to time.Time) ([]models.SystemAuditEntry, error)
	// List returns entries matching the filter, newest first, with keyset
	// pagination. Returns (page, total matching the filter, encoded next
	// cursor or nil) (#398).
	List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, error)
	// GetByID returns the entry or nil when not found.
	GetByID(ctx context.Context, id string) (*models.SystemAuditEntry, error)
}

// SystemAuditFilter carries the admin query dimensions for system audit
// entries (#398). All filter fields are optional and AND-combined.
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

type systemAuditRepoGORM struct {
	db *gorm.DB
}

// NewSystemAuditRepository constructs a GORM-backed SystemAuditRepository.
func NewSystemAuditRepository(db *gorm.DB) SystemAuditRepository {
	return &systemAuditRepoGORM{db: db}
}

// Create inserts a new audit row. The model's BeforeCreate hook generates
// a UUID if ID is empty.
func (r *systemAuditRepoGORM) Create(ctx context.Context, entry models.SystemAuditEntry) error {
	return r.db.WithContext(ctx).Create(&entry).Error
}

// ListByActor returns audit rows for the given actor email within the
// inclusive [from, to] window, ordered by created_at descending.
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
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s) + "%"
}

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

// List returns system audit entries matching the filter, newest first, with
// keyset pagination ordered (created_at DESC, id DESC). Uses the expanded
// cursor comparison form because Oracle has no row-value comparison.
func (r *systemAuditRepoGORM) List(ctx context.Context, f SystemAuditFilter) ([]models.SystemAuditEntry, int, *string, error) {
	var total int64
	if err := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f).Count(&total).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("count system audit entries: %w", err)
	}

	q := r.applyFilter(r.db.WithContext(ctx).Model(&models.SystemAuditEntry{}), f)
	if f.Cursor != nil {
		q = q.Where("created_at < ? OR (created_at = ? AND id < ?)",
			f.Cursor.CreatedAt, f.Cursor.CreatedAt, f.Cursor.ID)
	}
	var rows []models.SystemAuditEntry
	if err := q.Order("created_at DESC, id DESC").Limit(f.Limit).Find(&rows).Error; err != nil {
		return nil, 0, nil, fmt.Errorf("list system audit entries: %w", err)
	}

	var next *string
	if f.Limit > 0 && len(rows) == f.Limit {
		last := rows[len(rows)-1]
		enc := encodeAuditCursor(last.CreatedAt, string(last.ID))
		next = &enc
	}
	return rows, int(total), next, nil
}

// GetByID returns the system audit entry with the given ID, or nil if not found.
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
type StepUpAuditAdapter struct {
	repo SystemAuditRepository
}

// NewStepUpAuditAdapter constructs the adapter.
func NewStepUpAuditAdapter(repo SystemAuditRepository) *StepUpAuditAdapter {
	return &StepUpAuditAdapter{repo: repo}
}

// WriteSystemAudit implements auth.SystemAuditWriter.
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
