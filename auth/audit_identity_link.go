package auth

// audit_identity_link.go — identity-link flow audit helpers (#383).
// Follows the exact same shape as audit_step_up.go.

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// IdentityLinkAuditor wraps a SystemAuditWriter with the field shapes specific
// to identity-link events. Fail-open: write failures are logged but do not
// propagate.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: fail-open auditor that records identity link and unlink events to the system audit log
type IdentityLinkAuditor struct {
	writer SystemAuditWriter
}

// NewIdentityLinkAuditor returns an auditor. writer may be nil (in which case
// audit calls are no-ops with a debug log; matches the existing fail-open
// posture).
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: build an IdentityLinkAuditor wrapping the given system audit writer (pure)
func NewIdentityLinkAuditor(writer SystemAuditWriter) *IdentityLinkAuditor {
	return &IdentityLinkAuditor{writer: writer}
}

// IdentityLinkActor identifies the user performing a link operation. All four
// identity fields are denormalized into the audit row (matches SystemAuditEntry
// pattern; rows survive user deletion).
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: denormalized user identity fields for an identity-link audit record (pure)
type IdentityLinkActor struct {
	Email          string
	Provider       string
	ProviderUserID string
	DisplayName    string
	UserUUID       string
}

// LogComplete records a successful identity-link completion. Both sides'
// (provider, sub) are redacted in the audit payload.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: record a successful identity link audit event with redacted provider subject IDs
func (a *IdentityLinkAuditor) LogComplete(
	ctx context.Context,
	actor IdentityLinkActor,
	accountProvider, accountSub, linkedProvider, linkedSub string,
) error {
	payload := map[string]string{
		"account_provider": accountProvider,
		"account_sub":      redactSub(accountSub),
		"linked_provider":  linkedProvider,
		"linked_sub":       redactSub(linkedSub),
	}
	summary := fmt.Sprintf("identity linked: %s sub=%s → account on %s", linkedProvider, redactSub(linkedSub), accountProvider)
	return a.write(ctx, actor, "auth.identity_link_complete", payload, summary)
}

// LogFailed records an identity-link attempt that failed (e.g. upstream error,
// code-exchange failure). reason is a short stable code. extras are inlined
// into the payload.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: record a failed identity-link attempt audit event with reason code
func (a *IdentityLinkAuditor) LogFailed(
	ctx context.Context,
	actor IdentityLinkActor,
	reason string,
	extras map[string]string,
) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		payload[k] = v
	}
	summary := fmt.Sprintf("identity link failed: %s", reason)
	return a.write(ctx, actor, "auth.identity_link_failed", payload, summary)
}

// LogRejected records an identity-link attempt that was rejected before any
// upstream round-trip (e.g. service-account caller, already-bound identity).
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: record a pre-flight-rejected identity-link attempt audit event with reason code
func (a *IdentityLinkAuditor) LogRejected(
	ctx context.Context,
	actor IdentityLinkActor,
	reason string,
	extras map[string]string,
) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		payload[k] = v
	}
	summary := fmt.Sprintf("identity link rejected: %s", reason)
	return a.write(ctx, actor, "auth.identity_link_rejected", payload, summary)
}

// LogUnlink records the removal of a linked identity. Both sides' (provider,
// sub) are redacted in the audit payload.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: record an identity unlink audit event with redacted provider subject IDs
func (a *IdentityLinkAuditor) LogUnlink(
	ctx context.Context,
	actor IdentityLinkActor,
	linkedProvider, linkedSub string,
) error {
	payload := map[string]string{
		"account_provider": actor.Provider,
		"account_sub":      redactSub(actor.ProviderUserID),
		"linked_provider":  linkedProvider,
		"linked_sub":       redactSub(linkedSub),
	}
	summary := fmt.Sprintf("identity unlinked: %s sub=%s from account on %s", linkedProvider, redactSub(linkedSub), actor.Provider)
	return a.write(ctx, actor, "auth.identity_unlink", payload, summary)
}

// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: serialize and store an identity-link audit record via the system audit writer (writes DB)
func (a *IdentityLinkAuditor) write(
	ctx context.Context,
	actor IdentityLinkActor,
	fieldPath string,
	payload map[string]string,
	summary string,
) error {
	if a == nil || a.writer == nil {
		slogging.Get().Debug("IdentityLinkAuditor: no writer wired; skipping audit row for %s", fieldPath)
		return nil
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		// Defensive — map[string]string cannot fail to marshal in practice.
		slogging.Get().Error("IdentityLinkAuditor: marshal failed for %s: %v", fieldPath, err)
		return nil
	}
	newValStr := string(jsonBody)
	rec := SystemAuditRecord{
		ActorEmail:       actor.Email,
		ActorProvider:    actor.Provider,
		ActorProviderID:  actor.ProviderUserID,
		ActorDisplayName: actor.DisplayName,
		HTTPMethod:       "POST",
		HTTPPath:         "/me/identities/link",
		FieldPath:        fieldPath,
		NewValueRedacted: &newValStr,
		ChangeSummary:    &summary,
		CreatedAt:        time.Now().UTC(),
	}
	if err := a.writer.WriteSystemAudit(ctx, rec); err != nil {
		slogging.Get().Error("IdentityLinkAuditor: write failed for %s: %v", fieldPath, err)
		// Fail-open: completion paths should still succeed.
		return nil
	}
	return nil
}

// redactSub mirrors the Tier-2 redaction shape from redactStepUpAttemptedEmail
// but is named for generic subject values (provider_user_id, sub) rather than
// emails. Uses the same SHA-256 prefix pattern.
// SEM@d89a562535e2240eeb7f556a3f619d28fe9c5613: redact an OAuth provider subject value using the SHA-256 prefix pattern (pure)
func redactSub(v string) string {
	return redactStepUpAttemptedEmail(v)
}
