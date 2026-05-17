package worker

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatPayload(t *testing.T) {
	hb := Heartbeat{Component: "tmi-extractor", InstanceID: "pod-xyz"}
	b, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Heartbeat
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Component != "tmi-extractor" || out.InstanceID != "pod-xyz" {
		t.Fatalf("heartbeat round-trip mismatch: %+v", out)
	}
}

func TestHeartbeatInterval(t *testing.T) {
	// A zero interval falls back to the default.
	if got := heartbeatInterval(0); got != defaultHeartbeatInterval {
		t.Fatalf("interval fallback: got %v", got)
	}
	if got := heartbeatInterval(5 * time.Second); got != 5*time.Second {
		t.Fatalf("interval passthrough: got %v", got)
	}
}
