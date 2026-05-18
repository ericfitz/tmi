package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// natsURL is the NATS endpoint for integration tests. CI sets TMI_TEST_NATS_URL
// to the service-container address; locally it defaults to localhost.
func natsURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

func TestConnect_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := Connect(ctx, Config{NATSURL: natsURL(t), ComponentName: "test-worker"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	// The payload bucket must be usable: put + get a blob.
	ref, err := conn.PutPayload(ctx, "test-job-1", []byte("hello"))
	if err != nil {
		t.Fatalf("PutPayload: %v", err)
	}
	got, err := conn.GetPayload(ctx, ref)
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("payload round-trip: got %q", got)
	}
}

func TestPayloadName(t *testing.T) {
	ref := PayloadBucket + "/job-1/source"
	name, ok := payloadName(ref)
	if !ok || name != "job-1/source" {
		t.Fatalf("payloadName(%q): got %q ok=%v", ref, name, ok)
	}
	if _, ok := payloadName("no-prefix/x"); ok {
		t.Fatal("payloadName: expected !ok for a ref without the bucket prefix")
	}
	if _, ok := payloadName(PayloadBucket + "/"); ok {
		t.Fatal("payloadName: expected !ok for a ref with empty name")
	}
}
