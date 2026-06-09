package worker_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// testNATSURL returns the NATS endpoint for the integration test. CI sets
// TMI_TEST_NATS_URL to the service-container address; locally it defaults to
// localhost. This mirrors natsURL in nats_test.go (that helper lives in the
// internal package and is not reachable from this external test package).
func testNATSURL() string {
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

// cleanupNATSStreams registers a t.Cleanup that deletes the named JetStream
// streams (and, transitively, their consumers) using a FRESH NATS connection.
//
// A fresh connection is required: t.Cleanup callbacks run AFTER the test
// function's deferred calls, so the test's own `defer conn.Close()` has already
// closed the shared connection by the time cleanup runs — reusing it fails with
// "nats: connection closed" and silently leaks the streams. Leftover durable
// consumers on the shared WorkQueue streams then collide across packages and
// reruns ("filtered consumer not unique on workqueue stream"); see GitHub
// issue #440.
func cleanupNATSStreams(t *testing.T, natsURL string, names ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c, err := worker.Connect(ctx, worker.Config{NATSURL: natsURL, ComponentName: "itest-cleanup"})
		if err != nil {
			return
		}
		defer c.Close()
		js := c.JetStream()
		for _, n := range names {
			_ = js.DeleteStream(ctx, n)
		}
	})
}

// wirePipeline starts the two inline stage handlers (extract, chunkembed) on
// the test's ctx via worker.RunConsumer, sets up a JetStream consumer on the
// result stream, and returns a channel that delivers the result envelope for
// jobID. The RunConsumer goroutines stop when ctx is cancelled. The result
// ConsumeContext is stopped via t.Cleanup.
func wirePipeline(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) <-chan jobenvelope.Result {
	t.Helper()

	// Inline EXTRACT handler: read the source blob, write an "extracted"
	// blob, and forward a chunkembed job. It mimics the real extractor's
	// runtime plumbing without importing cmd/extractor (package main).
	extract := func(ctx context.Context, job jobenvelope.Job) error {
		data, err := conn.GetPayload(ctx, job.Input.ObjectRef)
		if err != nil {
			return err
		}
		ref, err := conn.PutPayload(ctx, job.JobID+"/extracted", data)
		if err != nil {
			return err
		}
		next := jobenvelope.Job{
			JobID:       job.JobID,
			ContentType: job.ContentType,
			Limits:      job.Limits,
			Deadline:    job.Deadline,
			Input: jobenvelope.Input{
				ObjectRef: ref,
				ByteSize:  int64(len(data)),
			},
		}
		b, err := json.Marshal(next)
		if err != nil {
			return err
		}
		return conn.Publish(ctx, worker.ChunkEmbedSubject(job.JobID), b)
	}

	// Inline CHUNKEMBED handler: read the extracted blob, write a "result"
	// blob, and publish the result envelope.
	chunkembed := func(ctx context.Context, job jobenvelope.Job) error {
		data, err := conn.GetPayload(ctx, job.Input.ObjectRef)
		if err != nil {
			return err
		}
		ref, err := conn.PutPayload(ctx, job.JobID+"/result", data)
		if err != nil {
			return err
		}
		res := jobenvelope.Result{
			JobID:  job.JobID,
			Status: jobenvelope.StatusCompleted,
			Output: jobenvelope.Output{ResultRef: ref},
		}
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		return conn.Publish(ctx, worker.ResultSubject(job.JobID), b)
	}

	// Set up the result consumer BEFORE the job is published so no result
	// message is missed. RunConsumer's ensureStream creates the per-stage
	// streams, but the result stream has no RunConsumer behind it here, so
	// create it explicitly.
	js := conn.JetStream()
	resultStream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.ResultStream,
		Subjects:  []string{worker.SubjectResultPrefix + ">"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		t.Fatalf("create result stream: %v", err)
	}
	resultCons, err := resultStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "itest-result",
		FilterSubject: worker.ResultSubject(jobID),
		AckPolicy:     jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create result consumer: %v", err)
	}

	// Delete the streams this test creates so it leaves no durable consumers or
	// subject bindings on the shared NATS server. TMI_RESULTS is a WorkQueue
	// stream; a leftover consumer with a narrow jobs.result.<id> filter collides
	// with the monolith result-consumer's jobs.result.> wildcard when a later
	// package's tests run against the same NATS. The per-stage streams bind
	// jobs.extract.> / jobs.chunkembed.>, whose subject space cannot be re-bound
	// by another stream. Deleting a stream removes its consumers too.
	cleanupNATSStreams(t, testNATSURL(),
		worker.ResultStream,
		worker.StreamNameFor("itest-extract"),
		worker.StreamNameFor("itest-chunkembed"),
	)

	results := make(chan jobenvelope.Result, 1)
	cc, err := resultCons.Consume(func(msg jetstream.Msg) {
		var result jobenvelope.Result
		if err := json.Unmarshal(msg.Data(), &result); err != nil {
			// An undecodable result message can never succeed; ack it
			// so it does not redeliver and the test fails via timeout.
			_ = msg.Ack()
			return
		}
		_ = msg.Ack()
		if result.JobID == jobID {
			select {
			case results <- result:
			default:
			}
		}
	})
	if err != nil {
		t.Fatalf("consume result stream: %v", err)
	}
	t.Cleanup(cc.Stop)

	// Start the two stage consumers. RunConsumer blocks until ctx is
	// cancelled, so run each on its own goroutine bound to the test ctx.
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			StreamName:    worker.StreamNameFor("itest-extract"),
			Durable:       "itest-extract",
			FilterSubject: worker.SubjectExtractPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, extract)
	}()
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			StreamName:    worker.StreamNameFor("itest-chunkembed"),
			Durable:       "itest-chunkembed",
			FilterSubject: worker.SubjectChunkEmbedPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, chunkembed)
	}()

	return results
}

// TestPipeline_Integration exercises the worker runtime end-to-end against a
// real NATS JetStream server: it publishes an extract job and asserts that a
// completed result envelope flows extract -> chunkembed -> result. It uses
// inline handlers (not cmd/extractor or cmd/chunkembed, which are package
// main and cannot be imported) to prove the RunConsumer loop, Object Store,
// and publish chain. Gated by TMI_RUN_NATS_TESTS; skipped otherwise.
func TestPipeline_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := worker.Connect(ctx, worker.Config{
		NATSURL:       testNATSURL(),
		ComponentName: "itest",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	const jobID = "itest-job-1"
	source := []byte("hello integration world")

	srcRef, err := conn.PutPayload(ctx, jobID+"/source", source)
	if err != nil {
		t.Fatalf("PutPayload source: %v", err)
	}

	results := wirePipeline(ctx, t, conn, jobID)

	// Build and publish the extract job.
	deadline := time.Now().Add(25 * time.Second)
	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: "text/plain",
		Limits: jobenvelope.Limits{
			WallClock: jobenvelope.Duration(10 * time.Second),
		},
		Deadline: &deadline,
		Input: jobenvelope.Input{
			ObjectRef: srcRef,
			ByteSize:  int64(len(source)),
		},
	}
	jb, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal job: %v", err)
	}
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"plaintext", jb); err != nil {
		t.Fatalf("publish extract job: %v", err)
	}

	select {
	case result := <-results:
		if result.Status != jobenvelope.StatusCompleted {
			t.Fatalf("result status: got %q, want %q", result.Status, jobenvelope.StatusCompleted)
		}
		if result.Output.ResultRef == "" {
			t.Fatal("result envelope has empty result_ref")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result envelope")
	}
}
