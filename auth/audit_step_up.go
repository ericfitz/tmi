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
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: interface for writing system audit records to a persistent store
type SystemAuditWriter interface {
	WriteSystemAudit(ctx context.Context, entry SystemAuditRecord) error
}

// SystemAuditRecord is a transport struct mapping 1:1 to
// api/models.SystemAuditEntry. Defined here so package auth does not import
// package api (which would create a cycle).
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: transport struct carrying actor identity and change details for a system audit entry (pure)
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
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: fail-open auditor that writes step-up authentication events to the system audit log
type StepUpAuditor struct {
	writer SystemAuditWriter
}

// NewStepUpAuditor returns an auditor. writer may be nil (in which case audit
// calls are no-ops with a debug log; matches the existing fail-open posture).
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: build a StepUpAuditor with the given writer; nil writer makes all calls no-ops (pure)
func NewStepUpAuditor(writer SystemAuditWriter) *StepUpAuditor {
	return &StepUpAuditor{writer: writer}
}

// StepUpActor identifies the user whose step-up event is being recorded.
// All four fields are denormalized into the audit row (matches the
// SystemAuditEntry pattern; rows survive user deletion).
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: value struct identifying the user whose step-up event is being audited (pure)
type StepUpActor struct {
	Email          string
	Provider       string
	ProviderUserID string
	DisplayName    string
}

// LogComplete records a successful step-up. Strength carries strong|weak;
// mode carries round_trip|short_circuit.
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: record a successful step-up authentication event with strength and provider to the audit log
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
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: record a failed step-up attempt, redacting any attempted email in the payload
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
// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: record a step-up attempt rejected before the upstream round-trip, with reason extras
func (a *StepUpAuditor) LogRejected(ctx context.Context, actor StepUpActor, reason string, extras map[string]string) error {
	payload := map[string]string{"reason": reason}
	for k, v := range extras {
		payload[k] = v
	}
	summary := fmt.Sprintf("step-up rejected: %s", reason)
	return a.write(ctx, actor, "auth.step_up_rejected", payload, summary)
}

// SEM@2993ca8c06b610c81da5355fd0a4befd651c08fa: serialize a step-up event payload and write a system audit record; fail-open on write errors (reads DB)
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
// api/admin_audit_redaction.go: a JSON envelope containing the SHA-256
// prefix (8 hex chars) and, when the value is at least 24 chars long, the
// last 6 chars of the original value. Lives in package auth to avoid the
// api → auth import cycle.
// SEM@fdced8cc15afb62a35253491f6797c50eb5428f0: redact an email to a SHA-256 prefix and optional tail envelope for audit storage (pure)
func redactStepUpAttemptedEmail(v string) string {
	sum := sha256.Sum256([]byte(v))
	out := map[string]any{
		"redacted":      true,
		"sha256_prefix": hex.EncodeToString(sum[:4]),
	}
	if len(v) >= 24 {
		out["tail"] = v[len(v)-6:]
	}
	b, err := json.Marshal(out)
	if err != nil {
		// Should be impossible — map[string]any with primitive values.
		return `{"redacted":true,"err":"marshal_failed"}`
	}
	return string(b)
}
