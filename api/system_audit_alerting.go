package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/google/uuid"
)

// auditAlertEmitter is a narrow interface over EventEmitter.EmitEvent so the
// decorator can be tested without a real Redis connection.
// SEM@2f7a2f21d458c7aaf4b361657372e56563f72390: interface for emitting a webhook event payload (pure)
type auditAlertEmitter interface {
	EmitEvent(ctx context.Context, payload EventPayload) error
}

// alertingSystemAuditRepository decorates SystemAuditRepository so every
// successfully persisted system-audit entry also emits a
// system_audit.admin_write webhook event (T7 out-of-band alert, #395).
// Emission is non-fatal: the in-band audit row is the durable record.
// SEM@2f7a2f21d458c7aaf4b361657372e56563f72390: decorator that emits a webhook alert after each successful system audit entry creation (pure)
type alertingSystemAuditRepository struct {
	SystemAuditRepository
	emitter      auditAlertEmitter
	operatorName string
}

// NewAlertingSystemAuditRepository wraps inner with an alerting decorator that
// emits EventSystemAuditAdminWrite after every successful Create.
// SEM@2f7a2f21d458c7aaf4b361657372e56563f72390: build a SystemAuditRepository decorator that emits admin-write alerts after Create (pure)
func NewAlertingSystemAuditRepository(
	inner SystemAuditRepository,
	emitter auditAlertEmitter,
	operatorName string,
) SystemAuditRepository {
	return &alertingSystemAuditRepository{
		SystemAuditRepository: inner,
		emitter:               emitter,
		operatorName:          operatorName,
	}
}

// Create persists the entry via the inner repository and, on success, emits a
// system_audit.admin_write webhook event. Emit errors are logged but never
// propagate — the in-band audit row is the durable record.
//
// The entry ID is generated here (if absent) before delegation so that the
// emitted payload carries the same UUID the inner repo will persist.
// models.SystemAuditEntry.BeforeCreate also generates the ID, but only on
// the pointer receiver inside the GORM transaction — the decorator's copy
// would remain empty, breaking deep-links and the emitter dedup key
// (EventType + ObjectID + 1 s window).
// SEM@c13f85301f7c723dfb20f687cb8fddc4ed77e703: persist a system audit entry then emit a non-fatal webhook alert event (reads DB)
func (r *alertingSystemAuditRepository) Create(ctx context.Context, entry models.SystemAuditEntry) error {
	// Pre-assign the ID so the emitted payload matches what the inner repo
	// will persist. BeforeCreate only sets it when empty, so this is safe.
	if entry.ID == "" {
		entry.ID = models.DBVarchar(uuid.New().String())
	}

	if err := r.SystemAuditRepository.Create(ctx, entry); err != nil {
		return err
	}

	payload := EventPayload{
		EventType:  EventSystemAuditAdminWrite,
		ObjectID:   string(entry.ID),
		ObjectType: "system_audit_entry",
		// OwnerID is intentionally empty: system_audit.* events are
		// broadcast-matched by event type in the webhook consumer — there is
		// no single owner for a system-level audit event.
		Timestamp: time.Now().UTC(),
		Data: map[string]any{
			"entry_id":           string(entry.ID),
			"actor_email":        string(entry.ActorEmail),
			"actor_provider":     string(entry.ActorProvider),
			"actor_display_name": string(entry.ActorDisplayName),
			"http_method":        string(entry.HTTPMethod),
			"http_path":          string(entry.HTTPPath),
			"field_path":         string(entry.FieldPath),
			"old_value_redacted": entry.OldValueRedacted.Ptr(),
			"new_value_redacted": entry.NewValueRedacted.Ptr(),
			"change_summary":     entry.ChangeSummary.Ptr(),
			"operator_name":      r.operatorName,
		},
	}

	if err := r.emitter.EmitEvent(ctx, payload); err != nil {
		slogging.Get().Error(
			"system audit alert emit failed (in-band audit row persisted): %v", err,
		)
	}
	return nil
}
