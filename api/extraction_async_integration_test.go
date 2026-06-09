package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// testNATSURLAsync returns the NATS endpoint for the async integration test.
// CI sets TMI_TEST_NATS_URL to the service-container address; locally it
// defaults to localhost. This mirrors testNATSURL in
// internal/worker/pipeline_integration_test.go.
func testNATSURLAsync() string {
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
// consumers on the shared WorkQueue streams (TMI_RESULTS, TMI_DLQ) then collide
// across packages and reruns ("filtered consumer not unique on workqueue
// stream"); see GitHub issue #440.
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

// setupExtractionJobTestDBAsync creates a fresh in-memory SQLite DB with the
// ExtractionJob schema migrated. Separate from setupExtractionJobTestDB in
// extraction_job_store_test.go to avoid duplicate declaration when both files
// are compiled, but functionally identical.
func setupExtractionJobTestDBAsync(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.ExtractionJob{}))
	return db
}

// fakeDocAccessUpdater records UpdateAccessStatusWithDiagnostics calls so the
// integration test can assert the doc-updater was invoked with the right
// access_status. It implements docAccessUpdater.
type fakeDocAccessUpdater struct {
	mu     chan struct{} // used as a one-element mutex-free signal
	id     string
	status string
	called bool
}

func newFakeDocAccessUpdater() *fakeDocAccessUpdater {
	f := &fakeDocAccessUpdater{mu: make(chan struct{}, 1)}
	return f
}

func (f *fakeDocAccessUpdater) UpdateAccessStatusWithDiagnostics(
	_ context.Context,
	id, accessStatus, _, _, _ string,
) error {
	f.id = id
	f.status = accessStatus
	f.called = true
	// Signal (non-blocking; test polls)
	select {
	case f.mu <- struct{}{}:
	default:
	}
	return nil
}

// wirePipelineForMonolith starts the two inline stage handlers (extract,
// chunkembed) on the test ctx via worker.RunConsumer. Unlike wirePipeline in
// internal/worker/pipeline_integration_test.go it does NOT set up its own
// result consumer — the monolith ResultConsumer under test does that. It
// merely drives jobs.extract.> → jobs.chunkembed.> → jobs.result.<jobID>.
func wirePipelineForMonolith(ctx context.Context, t *testing.T, conn *worker.Conn) {
	t.Helper()

	// Inline EXTRACT handler: read the source blob, write an "extracted" blob,
	// and forward a chunkembed job.
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

	// Inline CHUNKEMBED handler: read the extracted blob, write a result blob,
	// and publish the result envelope to jobs.result.<jobID>. The ResultConsumer
	// under test subscribes to the TMI_RESULTS stream which covers that subject.
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

	// Ensure the TMI_RESULTS stream exists BEFORE the chunkembed handler can
	// publish a result. ResultConsumer.Start looks it up via js.Stream(); if it
	// doesn't exist at Start time, Start returns nil (graceful degradation).
	// Creating it here guarantees the stream is live before both the consumer
	// and the publisher.
	js := conn.JetStream()
	_, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.ResultStream,
		Subjects:  []string{worker.SubjectResultPrefix + ">"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	})
	require.NoError(t, err, "create TMI_RESULTS stream")

	// Delete the streams this test creates so it leaves no durable consumers or
	// subject bindings on the shared NATS server (see cleanupNATSStreams and
	// GitHub issue #440). Without this, a rerun — or another package run against
	// the same NATS — collides on the TMI_RESULTS WorkQueue stream or the
	// jobs.extract.> / jobs.chunkembed.> subject space.
	cleanupNATSStreams(t, testNATSURLAsync(),
		worker.ResultStream,
		worker.StreamNameFor("monolith-itest-extract"),
		worker.StreamNameFor("monolith-itest-chunkembed"),
	)

	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			StreamName:    worker.StreamNameFor("monolith-itest-extract"),
			Durable:       "monolith-itest-extract",
			FilterSubject: worker.SubjectExtractPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, extract)
	}()
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			StreamName:    worker.StreamNameFor("monolith-itest-chunkembed"),
			Durable:       "monolith-itest-chunkembed",
			FilterSubject: worker.SubjectChunkEmbedPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, chunkembed)
	}()
}

// TestExtractionAsyncRoundTrip_Integration exercises the full monolith-side
// async extraction pipeline end-to-end against a real NATS JetStream:
//
//  1. ExtractionPublisher puts the payload blob and publishes the extract job.
//  2. Inline extract + chunkembed stage handlers move the job through the
//     pipeline (mimicking the real workers without importing cmd/extractor or
//     cmd/chunkembed, which are package main).
//  3. ResultConsumer consumes the terminal result on jobs.result.<jobID>,
//     calls MarkTerminal on the ExtractionJobStore (SQLite in-memory), and
//     calls UpdateAccessStatusWithDiagnostics on the fake doc-updater.
//  4. The test polls the extraction_jobs row and asserts it reaches "completed".
//  5. The test asserts the doc-updater was called with access_status "accessible".
//
// DLQ note: a DLQ JetStream stream and consumer for dead-lettered job envelopes
// does not exist yet; that infrastructure is tracked in GitHub issue #437.
//
// Idempotency (TestExtractionAsyncIdempotent_Integration): not added here
// because the monolith doesn't own the job-ID generation path (the publisher
// always generates a new UUID), so true idempotency requires either manual
// row insertion or publisher API changes. Tracked as a follow-up.
//
// Gated by TMI_RUN_NATS_TESTS; skipped otherwise so make test-unit stays green.
func TestExtractionAsyncRoundTrip_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to NATS.
	conn, err := worker.Connect(ctx, worker.Config{
		NATSURL:       testNATSURLAsync(),
		ComponentName: "monolith-itest",
	})
	require.NoError(t, err, "worker.Connect")
	defer conn.Close()

	// Spin up the TMI_RESULTS stream + inline stage handlers.
	// This must happen before ResultConsumer.Start so the stream exists.
	wirePipelineForMonolith(ctx, t, conn)

	// Build SQLite-backed ExtractionJobStore.
	db := setupExtractionJobTestDBAsync(t)
	store := NewExtractionJobStore(db)

	// Fake doc-access updater (records whether the doc-updater was called).
	fakeDoc := newFakeDocAccessUpdater()
	const knownDocRef = "doc-async-int-1"

	// Construct a ResultConsumer with all real machinery except the doc-side
	// dependency. We override lookupDocument to return a known docRef so the
	// access-status update path is exercised without needing a real document DB.
	rc := &ResultConsumer{
		conn:  conn,
		jobs:  store,
		docs:  fakeDoc,
		blobs: conn, // real blob cleanup via Object Store
		emit:  nil,  // GlobalEventEmitter is nil in test; emit guard handles it
		lookupDocument: func(_ context.Context, _ string) (string, string, string, bool) {
			return knownDocRef, "tm-async-1", "owner-async-1", true
		},
	}

	// Start the ResultConsumer. It subscribes to TMI_RESULTS/jobs.result.>
	// and drives MarkTerminal + UpdateAccessStatusWithDiagnostics per result.
	require.NoError(t, rc.Start(ctx), "ResultConsumer.Start")
	defer rc.Stop()

	// Publish via ExtractionPublisher.
	pub := NewExtractionPublisher(conn, store)
	jobID, err := pub.Publish(ctx, ExtractionRequest{
		DocumentID:  knownDocRef,
		ContentType: "text/plain",
		Bytes:       []byte("hello async world"),
	})
	require.NoError(t, err, "ExtractionPublisher.Publish")
	require.NotEmpty(t, jobID)

	// Poll the extraction_jobs row until status == "completed" or timeout.
	deadline := time.Now().Add(28 * time.Second)
	var lastStatus string
	for time.Now().Before(deadline) {
		var row models.ExtractionJob
		result := db.WithContext(ctx).Where("job_id = ?", jobID).First(&row)
		if result.Error == nil {
			lastStatus = string(row.Status)
			if lastStatus == models.ExtractionStatusCompleted {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	assert.Equal(t, models.ExtractionStatusCompleted, lastStatus,
		"extraction_jobs row must reach 'completed'; got %q (jobID=%s)", lastStatus, jobID)

	// Assert the doc-access updater was called with "accessible".
	assert.True(t, fakeDoc.called, "UpdateAccessStatusWithDiagnostics must have been called")
	assert.Equal(t, knownDocRef, fakeDoc.id, "doc-updater must be called with the known doc ref")
	assert.Equal(t, AccessStatusAccessible, fakeDoc.status,
		"doc-updater must be called with access_status 'accessible'")
}
