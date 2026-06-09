package api

import (
	"encoding/json"
	"testing"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func mustJSONBytes(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestParseAdvisory(t *testing.T) {
	raw := mustJSONBytes(t, map[string]interface{}{
		"stream": "TMI_TMI_EXTRACTOR", "consumer": "tmi-extractor",
		"stream_seq": 42, "deliveries": 3,
	})
	adv, err := parseMaxDeliverAdvisory(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if adv.Stream != "TMI_TMI_EXTRACTOR" || adv.StreamSeq != 42 {
		t.Fatalf("unexpected advisory: %+v", adv)
	}
}

func TestIsSelfReferentialStream(t *testing.T) {
	if !isSelfReferentialStream(worker.ResultStream) {
		t.Error("TMI_RESULTS should be self-referential (skip)")
	}
	if !isSelfReferentialStream(worker.DLQStream) {
		t.Error("TMI_DLQ should be self-referential (skip)")
	}
	if !isSelfReferentialStream(worker.DLQAdvisoryStream) {
		t.Error("TMI_DLQ_ADVISORY should be self-referential (skip)")
	}
	if isSelfReferentialStream("TMI_TMI_EXTRACTOR") {
		t.Error("a per-component stream must not be self-referential")
	}
}

func TestDecodeJobForDLQ_ValidJob(t *testing.T) {
	raw := mustJSONBytes(t, jobenvelope.Job{
		JobID: "j1", ContentType: "application/pdf",
		Input: jobenvelope.Input{ObjectRef: "TMI_PAYLOADS/j1/source", ByteSize: 10},
	})
	job, ok := decodeJobForDLQ(raw)
	if !ok {
		t.Fatal("valid job should decode for DLQ")
	}
	if job.JobID != "j1" {
		t.Fatalf("JobID = %q", job.JobID)
	}
}

func TestDecodeJobForDLQ_RejectsResult(t *testing.T) {
	// A Result envelope (no Input) must NOT be treated as a dead-letterable job.
	raw := mustJSONBytes(t, jobenvelope.Result{JobID: "j1", Status: jobenvelope.StatusFailed})
	if _, ok := decodeJobForDLQ(raw); ok {
		t.Fatal("a Result envelope must not decode as a dead-letterable Job")
	}
}

func TestDecodeJobForDLQ_RejectsGarbage(t *testing.T) {
	if _, ok := decodeJobForDLQ([]byte("not json")); ok {
		t.Fatal("garbage must not decode as a Job")
	}
}
