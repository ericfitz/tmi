package jobenvelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJobRoundTrip(t *testing.T) {
	in := Job{
		JobID:       "job-abc-123",
		ContentType: "application/pdf",
		Limits:      Limits{MaxBytes: 50 << 20, WallClock: 60 * time.Second},
		Deadline:    time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
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
}
