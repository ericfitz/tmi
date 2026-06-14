package jobenvelope

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
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

func TestValidateResult(t *testing.T) {
	valid := Result{
		JobID:        "job-abc-123",
		Status:       StatusFailed,
		ReasonCode:   "extraction_malformed",
		ReasonDetail: "slide #42",
	}
	cases := []struct {
		name    string
		mutate  func(r *Result)
		wantErr bool
	}{
		{"valid failed result", func(r *Result) {}, false},
		{"valid completed result", func(r *Result) {
			r.Status = StatusCompleted
			r.ReasonCode = ""
			r.ReasonDetail = ""
			r.Output.ResultRef = "TMI_PAYLOADS/job-abc-123/result"
		}, false},
		{"missing job_id", func(r *Result) { r.JobID = "" }, true},
		{"unknown status", func(r *Result) { r.Status = Status("definitely-done") }, true},
		{"empty status", func(r *Result) { r.Status = "" }, true},
		{"reason_code at length limit", func(r *Result) {
			r.ReasonCode = strings.Repeat("a", MaxReasonCodeLen)
		}, false},
		{"oversize reason_code", func(r *Result) {
			r.ReasonCode = strings.Repeat("a", MaxReasonCodeLen+1)
		}, true},
		{"reason_code with escape sequence", func(r *Result) {
			r.ReasonCode = "other\x1b[31m"
		}, true},
		{"reason_code with uppercase", func(r *Result) { r.ReasonCode = "Other" }, true},
		{"oversize result_ref", func(r *Result) {
			r.Output.ResultRef = strings.Repeat("x", MaxResultRefLen+1)
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.mutate(&r)
			err := ValidateResult(r)
			if tc.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
		})
	}
}

func TestSanitizeResultTruncatesOversizeDetail(t *testing.T) {
	r := Result{JobID: "j1", Status: StatusFailed, ReasonCode: "extraction_internal",
		ReasonDetail: strings.Repeat("a", MaxReasonDetailLen+100)}
	out := SanitizeResult(r)
	if len(out.ReasonDetail) != MaxReasonDetailLen {
		t.Fatalf("expected detail truncated to %d bytes, got %d",
			MaxReasonDetailLen, len(out.ReasonDetail))
	}
}

func TestSanitizeResultTruncatesOnRuneBoundary(t *testing.T) {
	// 3-byte runes straddling the byte cap must not be split mid-rune.
	r := Result{ReasonDetail: strings.Repeat("€", MaxReasonDetailLen/3+10)}
	out := SanitizeResult(r)
	if len(out.ReasonDetail) > MaxReasonDetailLen {
		t.Fatalf("detail exceeds cap: %d bytes", len(out.ReasonDetail))
	}
	if !utf8.ValidString(out.ReasonDetail) {
		t.Fatal("truncation produced invalid UTF-8")
	}
}

func TestSanitizeResultStripsControlChars(t *testing.T) {
	r := Result{ReasonDetail: "line1\nline2\tok\x1b[2Jx\x00x\rx"}
	out := SanitizeResult(r)
	want := "line1\nline2\tok[2Jxxx"
	if out.ReasonDetail != want {
		t.Fatalf("got %q want %q", out.ReasonDetail, want)
	}
}

func TestSanitizeResultCoercesInvalidUTF8(t *testing.T) {
	r := Result{ReasonDetail: "ok\xff\xfeok"}
	out := SanitizeResult(r)
	if !utf8.ValidString(out.ReasonDetail) {
		t.Fatalf("detail still invalid UTF-8: %q", out.ReasonDetail)
	}
}
