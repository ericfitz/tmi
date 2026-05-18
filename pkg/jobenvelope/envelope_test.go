package jobenvelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJobRoundTrip(t *testing.T) {
	dl := time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC)
	in := Job{
		JobID:       "job-abc-123",
		ContentType: "application/pdf",
		Limits:      Limits{MaxBytes: 50 << 20, WallClock: Duration(60 * time.Second)},
		Deadline:    &dl,
		Input:       Input{ObjectRef: "TMI_PAYLOADS/job-abc-123/source", ByteSize: 1234},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Job
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.JobID != in.JobID || out.ContentType != in.ContentType {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
	if out.Input.ObjectRef != in.Input.ObjectRef || out.Limits.MaxBytes != in.Limits.MaxBytes {
		t.Fatalf("nested round-trip mismatch: got %+v", out)
	}
	if out.Deadline == nil || !out.Deadline.Equal(*in.Deadline) {
		t.Fatalf("deadline round-trip mismatch: got %v want %v", out.Deadline, in.Deadline)
	}
	if out.Limits.WallClock != in.Limits.WallClock {
		t.Fatalf("WallClock round-trip mismatch: got %v want %v", out.Limits.WallClock, in.Limits.WallClock)
	}
}

func TestResultRoundTrip(t *testing.T) {
	in := Result{
		JobID:      "job-abc-123",
		Status:     StatusFailed,
		ReasonCode: "extraction_malformed",
		Output:     Output{ResultRef: ""},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Status != StatusFailed || out.ReasonCode != "extraction_malformed" {
		t.Fatalf("result round-trip mismatch: got %+v", out)
	}
	if out.Output.ResultRef != "" {
		t.Fatalf("expected empty ResultRef on failure, got %q", out.Output.ResultRef)
	}
}

func TestValidateContentRefOK(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "application/pdf",
		Input: Input{ObjectRef: "b/k", ByteSize: 10}}
	if err := Validate(j); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateRejectsMissingJobID(t *testing.T) {
	j := Job{ContentType: "text/plain", Input: Input{ObjectRef: "b/k"}}
	if err := Validate(j); err == nil {
		t.Fatal("expected error for missing job_id")
	}
}

func TestValidateRejectsNoInput(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "text/plain"}
	if err := Validate(j); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidateRejectsBothInputModes(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "text/plain",
		Input: Input{ObjectRef: "b/k", SourceURL: "https://x"}}
	if err := Validate(j); err == nil {
		t.Fatal("expected error: content-ref and source-locator both set")
	}
}
