package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
)

// auditAlertEmitter is a narrow interface over EventEmitter.EmitEvent so the
// decorator can be tested without a real Redis connection.
type auditAlertEmitter interface {
	EmitEvent(ctx context.Context, payload EventPayload) error
}

// alertingSystemAuditRepository decorates SystemAuditRepository so every
// successfully persisted system-audit entry also emits a
// system_audit.admin_write webhook event (T7 out-of-band alert, #395).
// Emission is non-fatal: the in-band audit row is the durable record.
type alertingSystemAuditRepository struct {
	SystemAuditRepository
	emitter      auditAlertEmitter
	operatorName string
}

// NewAlertingSystemAuditRepository wraps inner with an alerting decorator that
// emits EventSystemAuditAdminWrite after every successful Create.
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
func (r *alertingSystemAuditRepository) Create(ctx context.Context, entry models.SystemAuditEntry) error {
	if err := r.SystemAuditRepository.Create(ctx, entry); err != nil {
		return err
	}

	payload := EventPayload{
		EventType:  EventSystemAuditAdminWrite,
		ObjectID:   string(entry.ID),
		ObjectType: "system_audit_entry",
		// OwnerID: system audit events are not owned by a threat-model owner;
		// follow the same convention as addon.invoked and leave empty — the
		// webhook consumer matches subscriptions by event type, not owner.
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
