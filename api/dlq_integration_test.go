package api

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

func dlqTestNATSURL() string {
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

// TestDLQProducer_DeadLettersCrashedJob proves that a job whose worker never
// acks (the crash failure mode) is recovered from its source stream and
// republished to jobs.dlq, and the source message is deleted.
func TestDLQProducer_DeadLettersCrashedJob(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := worker.Connect(ctx, worker.Config{NATSURL: dlqTestNATSURL(), ComponentName: "dlq-it"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	// Start the producer (creates TMI_DLQ + advisory stream + consumer).
	producer := NewDLQProducer(conn)
	if err := producer.Start(ctx); err != nil {
		t.Fatalf("producer Start: %v", err)
	}
	defer producer.Stop()

	js := conn.JetStream()

	// A unique source job stream for this test, WorkQueue like the real ones.
	const srcStream = "TMI_DLQ_IT_SRC"
	const srcSubject = "jobs.dlqit.>"
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name: srcStream, Subjects: []string{srcSubject},
		Retention: jetstream.WorkQueuePolicy, Storage: jetstream.FileStorage,
	}); err != nil {
		t.Fatalf("create source stream: %v", err)
	}
	// Delete this test's source stream plus the DLQ and advisory streams the
	// producer creates. TMI_DLQ is a WorkQueue stream; leaving the dlq-it-observer
	// consumer behind would collide with the monolith DLQ consumer that the
	// round-trip test's ResultConsumer.Start creates on the same stream. Uses a
	// fresh connection; see cleanupNATSStreams (#440).
	cleanupNATSStreams(t, dlqTestNATSURL(), worker.DLQStream, worker.DLQAdvisoryStream, srcStream)

	// Subscribe to jobs.dlq so we can observe the dead-lettered message.
	dlqStream, err := js.Stream(ctx, worker.DLQStream)
	if err != nil {
		t.Fatalf("DLQ stream: %v", err)
	}
	// Purge so a prior run's messages don't bleed in.
	_ = dlqStream.Purge(ctx)
	dlqCons, err := dlqStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable: "dlq-it-observer", FilterSubject: worker.SubjectDLQ,
		AckPolicy: jetstream.AckExplicitPolicy, AckWait: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("DLQ observer consumer: %v", err)
	}

	// Publish a job, then run a consumer that always Naks (the crash model).
	job := jobenvelope.Job{
		JobID: "dlq-it-job-1", ContentType: "text/plain",
		Input: jobenvelope.Input{ObjectRef: "TMI_PAYLOADS/dlq-it-job-1/source", ByteSize: 5},
	}
	jobBytes, _ := json.Marshal(job)
	if _, err := js.Publish(ctx, "jobs.dlqit.dlq-it-job-1", jobBytes); err != nil {
		t.Fatalf("publish job: %v", err)
	}

	naks := make(chan struct{}, 8)
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			StreamName: srcStream, Durable: "dlq-it-worker",
			FilterSubject: srcSubject, AckWait: 1 * time.Second, MaxDeliver: 1,
		}, func(context.Context, jobenvelope.Job) error {
			select {
			case naks <- struct{}{}:
			default:
			}
			return errors.New("simulated crash: never acks")
		})
	}()

	// Expect the dead-lettered Job to arrive on jobs.dlq.
	deadline := time.After(25 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for dead-lettered job on jobs.dlq")
		default:
		}
		batch, err := dlqCons.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
		if err != nil {
			continue
		}
		got := false
		for m := range batch.Messages() {
			var dl jobenvelope.Job
			if err := json.Unmarshal(m.Data(), &dl); err == nil && dl.JobID == job.JobID {
				got = true
			}
			_ = m.Ack()
		}
		if got {
			break
		}
	}

	// The source message should have been deleted by the producer.
	src, err := js.Stream(ctx, srcStream)
	if err != nil {
		t.Fatalf("source stream lookup: %v", err)
	}
	info, err := src.Info(ctx)
	if err != nil {
		t.Fatalf("source stream info: %v", err)
	}
	if info.State.Msgs != 0 {
		t.Fatalf("expected source stream drained after dead-letter, have %d msgs", info.State.Msgs)
	}
}
