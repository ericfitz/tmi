package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// SystemAuditWriter is the minimal write surface required by step-up audit
// helpers. The concrete implementation in package api wraps GORM/Postgres;
// tests inject a memory implementation.
type SystemAuditWriter interface {
	WriteSystemAudit(ctx context.Context, entry SystemAuditRecord) error
}

// SystemAuditRecord is a transport struct mapping 1:1 to
// api/models.SystemAuditEntry. Defined here so package auth does not import
// package api (which would create a cycle).
type SystemAuditRecord struct {
	ActorEmail       string
	ActorProvider    string
	ActorProviderID  string
	ActorDisplayName string
	HTTPMethod       string
	HTTPPath         string
	FieldPath        string
	OldValueRedacted *string
	NewValueRedacted *string
	ChangeSummary    *string
	CreatedAt        time.Time
}

// StepUpAuditor wraps a SystemAuditWriter with the field shapes specific to
// step-up events. Fail-open: write failures are logged but do not propagate.
type StepUpAuditor struct {
	writer SystemAuditWriter
}

// NewStepUpAuditor returns an auditor. writer may be nil (in which case audit
// calls are no-ops with a debug log; matches the existing fail-open posture).
func NewStepUpAuditor(writer SystemAuditWriter) *StepUpAuditor {
	return &StepUpAuditor{writer: writer}
}

// StepUpActor identifies the user whose step-up event is being recorded.
// All four fields are denormalized into the audit row (matches the
// SystemAuditEntry pattern; rows survive user deletion).
type StepUpActor struct {
	Email          string
	Provider       string
	ProviderUserID string
	DisplayName    string
}

// LogComplete records a successful step-up. Strength carries strong|weak;
// mode carries round_trip|short_circuit.
func (a *StepUpAuditor) LogComplete(ctx context.Context, actor StepUpActor, strength StepUpStrength, providerID, mode string) error {
	payload := map[string]string{
		"provider": providerID,
		"strength": strength.String(),
		"mode":     mode,
	}
	summary := fmt.Sprintf("step-up completed (%s) via %s", strength.String(), providerID)
	if strength == StepUpWeak {
		summary += " — upstream IdP does not honor prompt=login"
	}
	return a.write(ctx, actor, "auth.step_up_complete", payload, summary)
}

// LogFailed records a step-up that did not complete successfully.
// reason is the short stable code (identity_mismatch, access_denied, state_expired, etc.).
// extras are inlined into the payload; values are redacted via redactStepUpAttemptedEmail
// when the key is "attempted_email".
func (a *StepUpAuditor) LogFailed(ctx context.Context, actor StepUpActor, reason string, extras map[string]string) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		if k == "attempted_email" {
			payload[k] = redactStepUpAttemptedEmail(v)
		} else {
			payload[k] = v
		}
	}
	summary := fmt.Sprintf("step-up failed: %s", reason)
	return a.write(ctx, actor, "auth.step_up_failed", payload, summary)
}

// LogRejected records a step-up attempt that was rejected before the upstream
// round-trip began (e.g., CC-grant caller, invalid provider).
func (a *StepUpAuditor) LogRejected(ctx context.Context, actor StepUpActor, reason string, extras map[string]string) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		payload[k] = v
	}
	summary := fmt.Sprintf("step-up rejected: %s", reason)
	return a.write(ctx, actor, "auth.step_up_rejected", payload, summary)
}

func (a *StepUpAuditor) write(ctx context.Context, actor StepUpActor, fieldPath string, payload map[string]string, summary string) error {
	if a == nil || a.writer == nil {
		slogging.Get().Debug("StepUpAuditor: no writer wired; skipping audit row for %s", fieldPath)
		return nil
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		// Defensive — map[string]string cannot fail to marshal in practice.
		slogging.Get().Error("StepUpAuditor: marshal failed for %s: %v", fieldPath, err)
		return nil
	}
	newValStr := string(jsonBody)
	rec := SystemAuditRecord{
		ActorEmail:       actor.Email,
		ActorProvider:    actor.Provider,
		ActorProviderID:  actor.ProviderUserID,
		ActorDisplayName: actor.DisplayName,
		HTTPMethod:       "GET",
		HTTPPath:         "/oauth2/step_up",
		FieldPath:        fieldPath,
		NewValueRedacted: &newValStr,
		ChangeSummary:    &summary,
		CreatedAt:        time.Now().UTC(),
	}
	if err := a.writer.WriteSystemAudit(ctx, rec); err != nil {
		slogging.Get().Error("StepUpAuditor: write failed for %s: %v", fieldPath, err)
		// Fail-open: completion paths should still succeed.
		return nil
	}
	return nil
}

// redactStepUpAttemptedEmail mirrors the Tier-2 redaction shape used by
// api/admin_audit_redaction.go (sha256-prefix-8 + last-6 tail when length >= 24,
// else full sha256-prefix-8). Lives in package auth to avoid the import cycle.
func redactStepUpAttemptedEmail(v string) string {
	sum := sha256.Sum256([]byte(v))
	prefix := hex.EncodeToString(sum[:])[:8]
	if len(v) >= 24 {
		return prefix + "…" + v[len(v)-6:]
	}
	return prefix
}
