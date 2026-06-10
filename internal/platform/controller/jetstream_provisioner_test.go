package controller

import (
	"context"
	"os"
	"testing"
	"time"

	platformv1alpha1 "github.com/ericfitz/tmi/api/platform/v1alpha1"
	"github.com/nats-io/nats.go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// natsURL mirrors internal/worker's test helper: CI sets TMI_TEST_NATS_URL,
// locally it defaults to localhost.
func natsURL() string {
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

// TestNATSProvisioner_CreatesConsumerKEDACanFind is the behavioral counterpart
// to the #444 name-agreement unit test: it proves that against a live NATS the
// controller actually creates a durable consumer under the exact name KEDA
// queries (consumerNameFor). Without this consumer, KEDA cannot scale the
// worker from zero. Gated on TMI_RUN_NATS_TESTS (make test-workers).
func TestNATSProvisioner_CreatesConsumerKEDACanFind(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// A distinct component name so this test's stream never collides with the
	// shipped components' streams on a shared NATS.
	comp := &platformv1alpha1.TMIComponent{
		ObjectMeta: metav1.ObjectMeta{Name: "tmi-prov-itest", Namespace: "tmi-platform"},
		Spec: platformv1alpha1.TMIComponentSpec{
			JobSubjects: []string{"jobs.provitest.one", "jobs.provitest.two"},
			Config:      map[string]string{"TMI_JOB_ACK_WAIT": "45s"},
		},
	}
	streamName := streamNameFor(comp)
	consumerName := consumerNameFor(comp)

	prov, err := NewNATSProvisioner(natsURL())
	if err != nil {
		t.Fatalf("NewNATSProvisioner: %v", err)
	}
	defer prov.Close()

	// A separate raw connection to verify what the provisioner created — this
	// is the same StreamInfo/ConsumerInfo lookup KEDA's nats-jetstream scaler
	// performs.
	nc, err := nats.Connect(natsURL())
	if err != nil {
		t.Fatalf("verify connect: %v", err)
	}
	defer nc.Close()
	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("verify jetstream: %v", err)
	}
	// Clean slate + cleanup so reruns are deterministic.
	_ = js.DeleteStream(streamName)
	t.Cleanup(func() { _ = js.DeleteStream(streamName) })

	if err := prov.EnsureStreamAndConsumer(ctx, comp); err != nil {
		t.Fatalf("EnsureStreamAndConsumer: %v", err)
	}

	si, err := js.StreamInfo(streamName, nats.Context(ctx))
	if err != nil {
		t.Fatalf("stream %s not created: %v", streamName, err)
	}
	if si.Config.Retention != nats.WorkQueuePolicy {
		t.Errorf("stream retention = %v, want WorkQueue", si.Config.Retention)
	}

	ci, err := js.ConsumerInfo(streamName, consumerName, nats.Context(ctx))
	if err != nil {
		t.Fatalf("consumer %s/%s not created (KEDA would not find it): %v", streamName, consumerName, err)
	}
	if ci.Config.AckWait != 45*time.Second {
		t.Errorf("consumer AckWait = %v, want 45s (from spec.config)", ci.Config.AckWait)
	}

	// Idempotent: a second reconcile must not error.
	if err := prov.EnsureStreamAndConsumer(ctx, comp); err != nil {
		t.Fatalf("EnsureStreamAndConsumer (second call) must be idempotent: %v", err)
	}
}
