// Package api provides storage and HTTP handlers for the TMI service.
package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/gorm"
)

// SystemAuditRepository is the data-access surface for system_audit_entries.
// Read methods are minimal in Part 1 of #355 — the full read API is tracked
// in #398. Part 1 needs Create (write path) and ListByActor (test
// verification + lightweight investigator query).
type SystemAuditRepository interface {
	Create(ctx context.Context, entry models.SystemAuditEntry) error
	ListByActor(ctx context.Context, actorEmail string, from, to time.Time) ([]models.SystemAuditEntry, error)
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
