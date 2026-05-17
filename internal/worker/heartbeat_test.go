package worker

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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

// hbTestNATSURL returns the NATS endpoint for the heartbeat integration test.
// CI sets TMI_TEST_NATS_URL to the service-container address; locally it
// defaults to localhost. Mirrors natsURL in nats_test.go.
func hbTestNATSURL() string {
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

// TestRunHeartbeat_Integration proves the heartbeat liveness signal actually
// reaches subscribers. RunHeartbeat publishes over core NATS (PublishCore);
// no JetStream stream covers components.heartbeat.> so a subscriber that uses
// a plain core NATS subscription must see the message. Gated by
// TMI_RUN_NATS_TESTS; skipped otherwise (consistent with nats_test.go and
// pipeline_integration_test.go).
func TestRunHeartbeat_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The worker connection that runs the heartbeat. ComponentName "hb-test"
	// is what HeartbeatSubject (and the Heartbeat.Component field) will carry.
	conn, err := Connect(ctx, Config{NATSURL: hbTestNATSURL(), ComponentName: "hb-test"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	// Subscribe to the heartbeat subject over core NATS, on an independent
	// connection, before starting the publisher so no tick is missed.
	sub, err := nats.Connect(hbTestNATSURL())
	if err != nil {
		t.Fatalf("nats.Connect (subscriber): %v", err)
	}
	defer sub.Close()

	subject := HeartbeatSubject("hb-test")
	subscription, err := sub.SubscribeSync(subject)
	if err != nil {
		t.Fatalf("SubscribeSync(%q): %v", subject, err)
	}
	defer func() { _ = subscription.Unsubscribe() }()
	// Flush so the subscription interest is registered on the server before
	// the first heartbeat tick fires.
	if err := sub.Flush(); err != nil {
		t.Fatalf("subscriber flush: %v", err)
	}

	// Start the heartbeat on a short interval so the test is fast.
	go RunHeartbeat(ctx, conn, "test-instance", 200*time.Millisecond)

	msg, err := subscription.NextMsg(5 * time.Second)
	if err != nil {
		t.Fatalf("no heartbeat received within timeout: %v", err)
	}

	var hb Heartbeat
	if err := json.Unmarshal(msg.Data, &hb); err != nil {
		t.Fatalf("unmarshal heartbeat: %v", err)
	}
	if hb.Component != "hb-test" {
		t.Fatalf("heartbeat Component: got %q, want %q", hb.Component, "hb-test")
	}
	if hb.InstanceID != "test-instance" {
		t.Fatalf("heartbeat InstanceID: got %q, want %q", hb.InstanceID, "test-instance")
	}
	if hb.SentAt.IsZero() {
		t.Fatal("heartbeat SentAt is zero")
	}
}
