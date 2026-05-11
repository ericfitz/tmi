package auth

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

type memorySystemAuditWriter struct {
	mu      sync.Mutex
	entries []SystemAuditRecord
}

func (m *memorySystemAuditWriter) WriteSystemAudit(ctx context.Context, e SystemAuditRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

func TestStepUpAuditor_StrongSuccess(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "alice@example.com", Provider: "google", ProviderUserID: "u-123", DisplayName: "Alice"}

	err := aud.LogComplete(context.Background(), actor, StepUpStrong, "google", "round_trip")
	if err != nil {
		t.Fatalf("LogComplete: %v", err)
	}
	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	e := w.entries[0]
	if e.HTTPPath != "/oauth2/step_up" || e.HTTPMethod != "GET" {
		t.Errorf("wrong method/path: %s %s", e.HTTPMethod, e.HTTPPath)
	}
	if e.FieldPath != "auth.step_up_complete" {
		t.Errorf("wrong FieldPath: %s", e.FieldPath)
	}
	if e.NewValueRedacted == nil {
		t.Fatal("NewValueRedacted nil")
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(*e.NewValueRedacted), &payload); err != nil {
		t.Fatalf("json: %v", err)
	}
	if payload["strength"] != "strong" || payload["provider"] != "google" || payload["mode"] != "round_trip" {
		t.Errorf("wrong payload: %v", payload)
	}
}

func TestStepUpAuditor_WeakShortCircuit(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "bob@example.com", Provider: "github", ProviderUserID: "u-456", DisplayName: "Bob"}

	_ = aud.LogComplete(context.Background(), actor, StepUpWeak, "github", "short_circuit")

	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	if !strings.Contains(*w.entries[0].NewValueRedacted, `"strength":"weak"`) {
		t.Errorf("missing weak marker: %s", *w.entries[0].NewValueRedacted)
	}
	if !strings.Contains(*w.entries[0].ChangeSummary, "weak") {
		t.Errorf("summary missing weak: %s", *w.entries[0].ChangeSummary)
	}
}

func TestStepUpAuditor_IdentityMismatchRedactsAttemptedEmail(t *testing.T) {
	w := &memorySystemAuditWriter{}
	aud := NewStepUpAuditor(w)
	actor := StepUpActor{Email: "alice@example.com", Provider: "google", ProviderUserID: "u-123", DisplayName: "Alice"}

	_ = aud.LogFailed(context.Background(), actor, "identity_mismatch", map[string]string{
		"attempted_email": "eve-the-attacker@evil.example",
	})

	if len(w.entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(w.entries))
	}
	body := *w.entries[0].NewValueRedacted
	if strings.Contains(body, "eve-the-attacker@evil.example") {
		t.Errorf("attempted_email leaked verbatim: %s", body)
	}
	if !strings.Contains(body, `"reason":"identity_mismatch"`) {
		t.Errorf("missing reason: %s", body)
	}
	// The envelope is JSON-encoded as a string inside the outer JSON object, so
	// the key appears in its escaped form.
	if !strings.Contains(body, `sha256_prefix`) {
		t.Errorf("missing sha256_prefix envelope: %s", body)
	}
}

func TestStepUpAuditor_NilWriterIsNoOp(t *testing.T) {
	aud := NewStepUpAuditor(nil)
	if err := aud.LogRejected(context.Background(), StepUpActor{}, "unsupported_grant_type", nil); err != nil {
		t.Fatalf("nil-writer should not error: %v", err)
	}
}
