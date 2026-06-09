package api

import (
	"testing"

	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func TestSynthesizeDLQResult(t *testing.T) {
	job := jobenvelope.Job{
		JobID: "job-9", ContentType: "application/pdf",
		Input: jobenvelope.Input{ObjectRef: "TMI_PAYLOADS/job-9/source"},
	}
	res := synthesizeDLQResult(job)
	if res.JobID != "job-9" {
		t.Errorf("JobID = %q; want job-9", res.JobID)
	}
	if res.Status != jobenvelope.StatusFailed {
		t.Errorf("Status = %q; want failed", res.Status)
	}
	if res.ReasonCode != ReasonExtractionDeadLettered {
		t.Errorf("ReasonCode = %q; want %q", res.ReasonCode, ReasonExtractionDeadLettered)
	}
	if res.ReasonDetail == "" {
		t.Error("ReasonDetail should be set")
	}
}
