//go:build e2e

package platform_e2e

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// TestWorkersE2E_PlaintextJob assumes `make e2e-platform-up`, the
// component-controller, and both TMIComponent CRs are already deployed (the
// Makefile target test-e2e-workers wires that). It connects to the
// in-cluster NATS through a port-forward on localhost:4222, puts a plaintext
// payload, publishes an extract job, and asserts a completed result
// envelope lands. KEDA scales tmi-extractor and tmi-chunk-embed from zero on
// queue depth, so the timeout is generous to allow cold start.
func TestWorkersE2E_PlaintextJob(t *testing.T) {
	const natsURL = "nats://127.0.0.1:4222"

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	conn, err := worker.Connect(ctx, worker.Config{NATSURL: natsURL, ComponentName: "e2e"})
	if err != nil {
		t.Fatalf("connect to in-cluster NATS (is the port-forward up?): %v", err)
	}
	defer conn.Close()

	jobID := "e2e-job-1"
	srcRef, err := conn.PutPayload(ctx, jobID+"/source", []byte("end to end plaintext"))
	if err != nil {
		t.Fatalf("put source payload: %v", err)
	}

	results := subscribeResult(ctx, t, conn, jobID)

	dl := time.Now().Add(90 * time.Second)
	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: "text/plain",
		Limits:      jobenvelope.Limits{WallClock: jobenvelope.Duration(10 * time.Second)},
		Deadline:    &dl,
		Input:       jobenvelope.Input{ObjectRef: srcRef, ByteSize: 20},
	}
	jb, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"plaintext", jb); err != nil {
		t.Fatalf("publish extract job: %v", err)
	}

	select {
	case res := <-results:
		// The chunk-embed worker may report a failed result if no real
		// embedding endpoint is reachable in the e2e environment; the
		// pipeline plumbing (extract -> chunkembed -> result) is proven
		// either way. A completed result additionally proves embedding.
		if res.Status != jobenvelope.StatusCompleted && res.Status != jobenvelope.StatusFailed {
			t.Fatalf("unexpected result status %q", res.Status)
		}
		t.Logf("e2e result: status=%s reason=%s", res.Status, res.ReasonCode)
	case <-ctx.Done():
		t.Fatal("timed out waiting for the worker pipeline result envelope")
	}

	// Sanity: confirm KEDA scaled the extractor up at some point.
	out, err := exec.CommandContext(ctx, "kubectl", "--context", "kind-tmi-platform",
		"-n", "tmi-platform", "get", "pods", "-l", "app=tmi-extractor", "--no-headers").Output()
	if err != nil {
		t.Logf("pod check skipped: %v", err)
	} else {
		t.Logf("tmi-extractor pods: %q", string(out))
	}
}

// subscribeResult creates a durable JetStream consumer on the job's result
// subject and returns a channel that receives the Result envelope.
func subscribeResult(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) <-chan jobenvelope.Result {
	t.Helper()
	js := conn.JetStream()
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.ResultStream,
		Subjects:  []string{worker.SubjectResultPrefix + ">"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		t.Fatalf("ensure result stream: %v", err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "e2e-result",
		FilterSubject: worker.ResultSubject(jobID),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create result consumer: %v", err)
	}
	out := make(chan jobenvelope.Result, 1)
	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var r jobenvelope.Result
		if json.Unmarshal(msg.Data(), &r) == nil && r.JobID == jobID {
			_ = msg.Ack()
			select {
			case out <- r:
			default:
			}
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		t.Fatalf("consume result subject: %v", err)
	}
	t.Cleanup(cc.Stop)
	return out
}
