package api

import "testing"

// TestBuildAccessDiagnostics_DeadLettered verifies a dead-lettered extraction
// surfaces a retry remediation (a crashed worker is a transient condition).
func TestBuildAccessDiagnostics_DeadLettered(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonExtractionDeadLettered})
	if d == nil {
		t.Fatal("expected non-nil diagnostics for extraction_dead_lettered")
	}
	if d.ReasonCode != "extraction_dead_lettered" {
		t.Errorf("ReasonCode = %q; want %q", d.ReasonCode, "extraction_dead_lettered")
	}
	if len(d.Remediations) != 1 || d.Remediations[0].Action != RemediationRetry {
		t.Fatalf("expected a single retry remediation, got %+v", d.Remediations)
	}
}
