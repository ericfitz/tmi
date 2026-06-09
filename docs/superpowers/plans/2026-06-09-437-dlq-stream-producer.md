# TMI_DLQ Stream + Dead-Letter Producer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route extraction jobs whose worker crashes mid-job (redelivery exhausted) to a `TMI_DLQ` JetStream stream and drive the `extraction_jobs` row to a clean `failed` state with an `extraction_dead_lettered` reason code.

**Architecture:** A monolith-hosted `DLQProducer` consumes durably-captured `MAX_DELIVERIES` JetStream advisories, recovers the original `Job` envelope by sequence (`GetMsg`), republishes it to `jobs.dlq`, and deletes the source message. The existing `ResultConsumer` gains a second subscription bound to `TMI_DLQ` that synthesizes a failed `Result`, reuses `handleResult`, and cleans up the crashed job's orphaned input blob.

**Tech Stack:** Go, NATS JetStream (`github.com/nats-io/nats.go/jetstream` v1.52.0), GORM (PostgreSQL + Oracle ADB), `github.com/ericfitz/tmi/internal/slogging`.

**Spec:** `docs/superpowers/specs/2026-06-09-437-dlq-stream-producer-design.md`

---

## File Structure

- **Modify** `internal/worker/names.go` — add `DLQStream`, `DLQAdvisoryStream`, `SubjectMaxDeliverAdvisory` constants (`SubjectDLQ` already exists).
- **Modify** `internal/worker/names_test.go` — assert the new constants.
- **Modify** `api/access_diagnostics.go` — add `ReasonExtractionDeadLettered` constant + `RemediationRetry` case.
- **Create** `api/access_diagnostics_dlq_test.go` — unit test the new diagnostics case.
- **Create** `api/dlq_producer.go` — `DLQProducer` type + the pure `classifyAdvisory`/`decodeJobForDLQ` decision logic.
- **Create** `api/dlq_producer_test.go` — unit tests for the pure decision logic.
- **Modify** `api/result_consumer.go` — add `synthesizeDLQResult`, the `TMI_DLQ` subscription in `Start`, and the DLQ callback with input-blob cleanup.
- **Create** `api/result_consumer_dlq_test.go` — unit test `synthesizeDLQResult`.
- **Modify** `api/server.go` — add `dlqProducer` field, `SetDLQProducer`, `StopDLQProducer`.
- **Modify** `cmd/server/main.go` — wire the producer in `wireExtractionNATS` and stop it in the shutdown path.
- **Create** `api/dlq_integration_test.go` — gated (`TMI_RUN_NATS_TESTS`) full crash→DLQ→failed round-trip.

---

## Task 1: Naming constants for the DLQ + advisory streams

**Files:**
- Modify: `internal/worker/names.go`
- Test: `internal/worker/names_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/worker/names_test.go`:

```go
func TestDLQConstants(t *testing.T) {
	if DLQStream != "TMI_DLQ" {
		t.Errorf("DLQStream = %q; want %q", DLQStream, "TMI_DLQ")
	}
	if DLQAdvisoryStream != "TMI_DLQ_ADVISORY" {
		t.Errorf("DLQAdvisoryStream = %q; want %q", DLQAdvisoryStream, "TMI_DLQ_ADVISORY")
	}
	if SubjectMaxDeliverAdvisory != "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>" {
		t.Errorf("SubjectMaxDeliverAdvisory = %q; want %q",
			SubjectMaxDeliverAdvisory, "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>")
	}
	if SubjectDLQ != "jobs.dlq" {
		t.Errorf("SubjectDLQ = %q; want %q", SubjectDLQ, "jobs.dlq")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestDLQConstants`
Expected: FAIL — `undefined: DLQStream` (compile error).

- [ ] **Step 3: Add the constants**

In `internal/worker/names.go`, add after the `ResultStream` const block:

```go
// DLQStream is the dead-letter JetStream stream bound to SubjectDLQ
// ("jobs.dlq"). The monolith creates it, publishes dead-lettered Job
// envelopes to it, and consumes from it (see api/dlq_producer.go and the
// ResultConsumer DLQ subscription). It is not owned by any per-component
// stream.
const DLQStream = "TMI_DLQ"

// DLQAdvisoryStream is the JetStream stream that durably captures
// MAX_DELIVERIES consumer advisories so the monolith's DLQ producer survives
// restarts (a plain core-NATS subscription would miss advisories fired while
// the monolith is down).
const DLQAdvisoryStream = "TMI_DLQ_ADVISORY"
```

Add to the `const (...)` subject block (next to `SubjectDLQ`):

```go
	// SubjectMaxDeliverAdvisory is the wildcard subject on which JetStream
	// publishes a MAX_DELIVERIES advisory when a message exhausts a consumer's
	// MaxDeliver. The concrete subject is
	// "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.<stream>.<consumer>".
	SubjectMaxDeliverAdvisory = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestDLQConstants`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/names.go internal/worker/names_test.go
git commit -m "feat(worker): add TMI_DLQ + advisory stream naming constants

Refs #437"
```

---

## Task 2: New reason code + retry remediation

**Files:**
- Modify: `api/access_diagnostics.go`
- Test: `api/access_diagnostics_dlq_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/access_diagnostics_dlq_test.go`:

```go
package api

import "testing"

// TestBuildAccessDiagnostics_DeadLettered verifies a dead-lettered extraction
// surfaces a retry remediation (a crashed worker is a transient condition).
func TestBuildAccessDiagnostics_DeadLettered(t *testing.T) {
	d := BuildAccessDiagnostics(BuilderContext{ReasonCode: ReasonExtractionDeadLettered})
	if d == nil {
		t.Fatal("expected non-nil diagnostics for extraction_dead_lettered")
	}
	if d.ReasonCode != "extraction_dead_lettered" {
		t.Errorf("ReasonCode = %q; want %q", d.ReasonCode, "extraction_dead_lettered")
	}
	if len(d.Remediations) != 1 || d.Remediations[0].Action != RemediationRetry {
		t.Fatalf("expected a single retry remediation, got %+v", d.Remediations)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestBuildAccessDiagnostics_DeadLettered`
Expected: FAIL — `undefined: ReasonExtractionDeadLettered`.

- [ ] **Step 3: Add the constant and the diagnostics case**

In `api/access_diagnostics.go`, add to the extraction reason-code const block (after `ReasonExtractionInternal`):

```go
	// ReasonExtractionDeadLettered is emitted when an extraction job's worker
	// exhausted redelivery without ever publishing a result (e.g. the worker
	// crashed/OOMed mid-job). The job was dead-lettered to jobs.dlq and the
	// monolith marked it failed. Retry may succeed.
	ReasonExtractionDeadLettered = "extraction_dead_lettered"
```

In `BuildAccessDiagnostics`, extend the existing transient-retry case so it reads:

```go
	case ReasonTokenTransientFailure, ReasonFetchError, ReasonExtractionDeadLettered:
		d.Remediations = append(d.Remediations, AccessRemediationDiag{
			Action: RemediationRetry,
			Params: map[string]interface{}{},
		})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestBuildAccessDiagnostics_DeadLettered`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/access_diagnostics.go api/access_diagnostics_dlq_test.go
git commit -m "feat(api): add extraction_dead_lettered reason code with retry remediation

Refs #437"
```

---

## Task 3: DLQ producer pure decision logic

The producer's testable core: given an advisory payload and the recovered source-message bytes, decide whether to dead-letter and produce the `Job` to forward. No NATS needed.

**Files:**
- Create: `api/dlq_producer.go`
- Test: `api/dlq_producer_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/dlq_producer_test.go`:

```go
package api

import (
	"encoding/json"
	"testing"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestParseAdvisory(t *testing.T) {
	raw := mustJSON(t, map[string]interface{}{
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
	if isSelfReferentialStream("TMI_TMI_EXTRACTOR") {
		t.Error("a per-component stream must not be self-referential")
	}
}

func TestDecodeJobForDLQ_ValidJob(t *testing.T) {
	raw := mustJSON(t, jobenvelope.Job{
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
	raw := mustJSON(t, jobenvelope.Result{JobID: "j1", Status: jobenvelope.StatusFailed})
	if _, ok := decodeJobForDLQ(raw); ok {
		t.Fatal("a Result envelope must not decode as a dead-letterable Job")
	}
}

func TestDecodeJobForDLQ_RejectsGarbage(t *testing.T) {
	if _, ok := decodeJobForDLQ([]byte("not json")); ok {
		t.Fatal("garbage must not decode as a Job")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name='TestParseAdvisory|TestIsSelfReferentialStream|TestDecodeJobForDLQ'`
Expected: FAIL — `undefined: parseMaxDeliverAdvisory` (compile error).

- [ ] **Step 3: Write the pure logic (header + functions only)**

Create `api/dlq_producer.go` with the imports, the advisory struct, and the three pure functions (the `DLQProducer` type itself is added in Task 4):

```go
package api

import (
	"encoding/json"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// maxDeliverAdvisory is the subset of a JetStream MAX_DELIVERIES consumer
// advisory the DLQ producer needs. Parsed from a local struct rather than a
// nats-server type to avoid a server dependency. The advisory carries the
// source stream + sequence but NOT the original payload, which is why the
// producer recovers it via GetMsg.
type maxDeliverAdvisory struct {
	Stream     string `json:"stream"`
	Consumer   string `json:"consumer"`
	StreamSeq  uint64 `json:"stream_seq"`
	Deliveries uint64 `json:"deliveries"`
}

// parseMaxDeliverAdvisory decodes a MAX_DELIVERIES advisory payload.
func parseMaxDeliverAdvisory(data []byte) (maxDeliverAdvisory, error) {
	var adv maxDeliverAdvisory
	if err := json.Unmarshal(data, &adv); err != nil {
		return maxDeliverAdvisory{}, err
	}
	return adv, nil
}

// isSelfReferentialStream reports whether advisories for the given source
// stream must be ignored to avoid dead-letter loops: the result stream and
// the DLQ stream are consumed by the monolith itself, never dead-lettered.
func isSelfReferentialStream(stream string) bool {
	return stream == worker.ResultStream || stream == worker.DLQStream
}

// decodeJobForDLQ decodes recovered source bytes as a Job envelope and returns
// it only when it is a valid job. This scopes dead-lettering to job streams
// without hardcoding component names: a Result envelope or any non-job message
// fails jobenvelope.Validate and is skipped.
func decodeJobForDLQ(data []byte) (jobenvelope.Job, bool) {
	var job jobenvelope.Job
	if err := json.Unmarshal(data, &job); err != nil {
		return jobenvelope.Job{}, false
	}
	if err := jobenvelope.Validate(job); err != nil {
		return jobenvelope.Job{}, false
	}
	return job, true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name='TestParseAdvisory|TestIsSelfReferentialStream|TestDecodeJobForDLQ'`
Expected: PASS (all 5 subtests).

- [ ] **Step 5: Commit**

```bash
git add api/dlq_producer.go api/dlq_producer_test.go
git commit -m "feat(api): DLQ producer advisory-decode + job-validation logic

Refs #437"
```

---

## Task 4: DLQProducer type — stream bootstrap + advisory consume loop

This wires the pure logic from Task 3 to NATS. It is exercised end-to-end by the gated integration test in Task 7 (it needs a live JetStream server); there is no unit test for the NATS-bound `Start`.

**Files:**
- Modify: `api/dlq_producer.go`

- [ ] **Step 1: Add the DLQProducer type, constructor, stream bootstrap, Start, and Stop**

Append to `api/dlq_producer.go`. Add `context`, `errors`, `time`, `slogging`, and the jetstream import to the import block so it reads:

```go
import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)
```

Then append:

```go
// DLQProducer turns JetStream MAX_DELIVERIES advisories into dead-letter
// messages. For each advisory on a per-component job stream it recovers the
// original Job envelope by sequence, republishes it to jobs.dlq, and deletes
// the source message (reclaiming the WorkQueue slot). It is the only durable
// path by which a worker that crashed mid-job (and thus never published a
// result) reaches a clean terminal state.
type DLQProducer struct {
	conn   *worker.Conn
	cancel context.CancelFunc
}

// NewDLQProducer constructs a DLQProducer bound to the monolith's NATS conn.
func NewDLQProducer(conn *worker.Conn) *DLQProducer {
	return &DLQProducer{conn: conn}
}

// ensureStreams creates (or updates) the DLQ stream and the advisory-capture
// stream. Idempotent: safe to call on every startup.
func (p *DLQProducer) ensureStreams(ctx context.Context) error {
	js := p.conn.JetStream()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.DLQStream,
		Subjects:  []string{worker.SubjectDLQ},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		return err
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      worker.DLQAdvisoryStream,
		Subjects:  []string{worker.SubjectMaxDeliverAdvisory},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    24 * time.Hour,
	}); err != nil {
		return err
	}
	return nil
}

// Start ensures the streams exist, creates a durable consumer on the
// advisory-capture stream, and begins processing advisories in the background.
// It returns after the consumer is created. Call Stop to release resources.
func (p *DLQProducer) Start(ctx context.Context) error {
	logger := slogging.Get()
	ctx, p.cancel = context.WithCancel(ctx)

	if err := p.ensureStreams(ctx); err != nil {
		return err
	}

	js := p.conn.JetStream()
	advStream, err := js.Stream(ctx, worker.DLQAdvisoryStream)
	if err != nil {
		return err
	}
	cons, err := advStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "monolith-dlq-producer",
		FilterSubject: worker.SubjectMaxDeliverAdvisory,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	})
	if err != nil {
		return err
	}

	cc, err := cons.Consume(p.makeCallback(ctx))
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		cc.Stop()
		logger.Info("dlq-producer: shut down")
	}()
	logger.Info("dlq-producer: subscribed to %s", worker.SubjectMaxDeliverAdvisory)
	return nil
}

// Stop cancels the producer's context. Safe to call when Start was never run.
func (p *DLQProducer) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
}

// makeCallback returns the advisory handler. It must never panic — a panic
// here would crash the monolith — so it is guarded.
func (p *DLQProducer) makeCallback(ctx context.Context) func(jetstream.Msg) {
	logger := slogging.Get()
	js := p.conn.JetStream()

	return func(msg jetstream.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("dlq-producer: panic on %s: %v — terminating", msg.Subject(), r)
				_ = msg.Term()
			}
		}()

		adv, err := parseMaxDeliverAdvisory(msg.Data())
		if err != nil {
			logger.Error("dlq-producer: undecodable advisory on %s: %v — terminating", msg.Subject(), err)
			_ = msg.Term()
			return
		}

		// Never dead-letter the result or DLQ streams themselves (loop guard).
		if isSelfReferentialStream(adv.Stream) {
			_ = msg.Ack()
			return
		}

		// Recover the original message by sequence from its source stream.
		srcStream, err := js.Stream(ctx, adv.Stream)
		if err != nil {
			logger.Warn("dlq-producer: source stream %s lookup failed: %v — nak", adv.Stream, err)
			_ = msg.Nak()
			return
		}
		raw, err := srcStream.GetMsg(ctx, adv.StreamSeq)
		if err != nil {
			// Already deleted by a prior advisory delivery — idempotent ack.
			if errors.Is(err, jetstream.ErrMsgNotFound) {
				_ = msg.Ack()
				return
			}
			logger.Warn("dlq-producer: GetMsg seq=%d on %s failed: %v — nak", adv.StreamSeq, adv.Stream, err)
			_ = msg.Nak()
			return
		}

		// Only dead-letter valid Job envelopes (skips stray Results, etc.).
		if _, ok := decodeJobForDLQ(raw.Data); !ok {
			logger.Warn("dlq-producer: seq=%d on %s is not a valid job — acking advisory without dead-lettering", adv.StreamSeq, adv.Stream)
			_ = msg.Ack()
			return
		}

		// Publish-then-delete: publish to jobs.dlq first so nothing is lost on
		// publish failure; then delete the source to reclaim the WorkQueue slot.
		if _, err := js.Publish(ctx, worker.SubjectDLQ, raw.Data); err != nil {
			logger.Warn("dlq-producer: publish to %s failed: %v — nak", worker.SubjectDLQ, err)
			_ = msg.Nak()
			return
		}
		if err := srcStream.DeleteMsg(ctx, adv.StreamSeq); err != nil && !errors.Is(err, jetstream.ErrMsgNotFound) {
			// The DLQ message is already published; a failed source delete only
			// leaves a dead slot, not a correctness bug. Log and ack.
			logger.Warn("dlq-producer: DeleteMsg seq=%d on %s failed: %v", adv.StreamSeq, adv.Stream, err)
		}
		logger.Info("dlq-producer: dead-lettered seq=%d from %s to %s", adv.StreamSeq, adv.Stream, worker.SubjectDLQ)
		_ = msg.Ack()
	}
}
```

Note: `jobenvelope` is still imported because `decodeJobForDLQ` (Task 3) references it; keep the import.

- [ ] **Step 2: Build to verify it compiles**

Run: `make build-server`
Expected: builds cleanly. (`jetstream.ErrMsgNotFound` exists in nats.go v1.52.0.)

- [ ] **Step 3: Run the package unit tests**

Run: `make test-unit name='TestParseAdvisory|TestDecodeJobForDLQ'`
Expected: PASS (still green — no behavior change to the pure functions).

- [ ] **Step 4: Commit**

```bash
git add api/dlq_producer.go
git commit -m "feat(api): DLQProducer advisory consumer with publish-then-delete

Refs #437"
```

---

## Task 5: ResultConsumer DLQ subscription + input-blob cleanup

**Files:**
- Modify: `api/result_consumer.go`
- Test: `api/result_consumer_dlq_test.go`

- [ ] **Step 1: Write the failing test**

Create `api/result_consumer_dlq_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestSynthesizeDLQResult`
Expected: FAIL — `undefined: synthesizeDLQResult`.

- [ ] **Step 3: Add `synthesizeDLQResult`**

In `api/result_consumer.go`, add near `handleResult`:

```go
// synthesizeDLQResult builds the failed Result for a dead-lettered job. A
// dead-lettered job is one whose worker exhausted redelivery without ever
// publishing a result (e.g. it crashed mid-job), so we manufacture the
// terminal failure here.
func synthesizeDLQResult(job jobenvelope.Job) jobenvelope.Result {
	return jobenvelope.Result{
		JobID:        job.JobID,
		Status:       jobenvelope.StatusFailed,
		ReasonCode:   ReasonExtractionDeadLettered,
		ReasonDetail: "worker exhausted redelivery (dead-letter)",
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestSynthesizeDLQResult`
Expected: PASS.

- [ ] **Step 5: Add the DLQ subscription to `Start`**

In `api/result_consumer.go`, inside `Start`, after the existing `cc, err := cons.Consume(...)` block and its `go func() { <-ctx.Done(); cc.Stop() ... }()` goroutine, and BEFORE the final `logger.Info("result-consumer: subscribed ...")` / `return nil`, insert:

```go
	// Also consume the dead-letter stream. Each message there is the original
	// Job envelope (republished by the DLQ producer), not a Result; the DLQ
	// callback synthesizes a failed Result and reuses handleResult. Bind
	// skip-if-absent, mirroring the TMI_RESULTS handling above.
	if dlqStream, derr := js.Stream(ctx, worker.DLQStream); derr != nil {
		if errors.Is(derr, jetstream.ErrStreamNotFound) {
			logger.Warn("result-consumer: stream %s not found; dead-letter processing unavailable until the DLQ producer creates it", worker.DLQStream)
		} else {
			return derr
		}
	} else {
		dlqCons, derr := dlqStream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       "monolith-dlq-consumer",
			FilterSubject: worker.SubjectDLQ,
			AckPolicy:     jetstream.AckExplicitPolicy,
			AckWait:       30 * time.Second,
			MaxDeliver:    5,
		})
		if derr != nil {
			return derr
		}
		dcc, derr := dlqCons.Consume(rc.makeDLQCallback(ctx))
		if derr != nil {
			return derr
		}
		go func() {
			<-ctx.Done()
			dcc.Stop()
		}()
		logger.Info("result-consumer: subscribed to %s/%s", worker.DLQStream, worker.SubjectDLQ)
	}
```

- [ ] **Step 6: Add the DLQ callback**

In `api/result_consumer.go`, add after `makeCallback`:

```go
// makeDLQCallback returns the handler for dead-letter messages. The payload is
// the original Job envelope; the handler synthesizes a failed Result, runs the
// shared handleResult, and additionally deletes the crashed job's orphaned
// input blob (handleResult only cleans Output.ResultRef, which is empty for a
// synthesized DLQ result). It MUST NOT panic.
func (rc *ResultConsumer) makeDLQCallback(ctx context.Context) func(jetstream.Msg) {
	logger := slogging.Get()

	return func(msg jetstream.Msg) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("result-consumer(dlq): panic on %s: %v — terminating", msg.Subject(), r)
				_ = msg.Term()
			}
		}()

		var job jobenvelope.Job
		if err := json.Unmarshal(msg.Data(), &job); err != nil {
			logger.Error("result-consumer(dlq): undecodable job on %s: %v — terminating", msg.Subject(), err)
			_ = msg.Term()
			return
		}

		if err := rc.handleResult(ctx, synthesizeDLQResult(job)); err != nil {
			logger.Warn("result-consumer(dlq): transient failure for job %s: %v — redelivering", job.JobID, err)
			_ = msg.Nak()
			return
		}

		// Clean up the crashed job's orphaned input payload blob (best-effort).
		if rc.blobs != nil && job.Input.ObjectRef != "" {
			if err := rc.blobs.DeletePayload(ctx, job.Input.ObjectRef); err != nil {
				logger.Warn("result-consumer(dlq): input blob cleanup for job %s failed: %v", job.JobID, err)
			}
		}

		_ = msg.Ack()
	}
}
```

- [ ] **Step 7: Build and run package tests**

Run: `make build-server`
Expected: builds cleanly.

Run: `make test-unit name='TestSynthesizeDLQResult'`
Expected: PASS.

- [ ] **Step 8: Update the stale DLQ note**

In `api/result_consumer.go`, the `ResultConsumer` type doc comment currently says the DLQ "would be required ... tracked as a follow-up". Replace that paragraph with:

```go
// DLQ note: the dead-letter path is now wired. The DLQ producer
// (api/dlq_producer.go) republishes the original Job envelope of any job that
// exhausted redelivery to SubjectDLQ ("jobs.dlq"); this consumer binds the
// TMI_DLQ stream (see makeDLQCallback) and turns each dead-lettered Job into a
// failed terminal transition.
```

Also delete the now-incorrect comment block in `Start` near the durable consumer that says "The DLQ subject (jobs.dlq) is not bound to this stream; see the type-level comment for the follow-up note." Replace with:

```go
	// Create (or bind to) a durable consumer that filters to jobs.result.>
	// only. The dead-letter subject is handled by a separate consumer on the
	// TMI_DLQ stream, added below.
```

- [ ] **Step 9: Commit**

```bash
git add api/result_consumer.go api/result_consumer_dlq_test.go
git commit -m "feat(api): consume TMI_DLQ and fail dead-lettered extraction jobs

Refs #437"
```

---

## Task 6: Wire the producer into server startup + shutdown

**Files:**
- Modify: `api/server.go`
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add the server field + stop hook**

In `api/server.go`, add a field next to `resultConsumer`:

```go
	dlqProducer    *DLQProducer
```

Add after `StopResultConsumer`:

```go
// SetDLQProducer injects the dead-letter producer for orderly shutdown. The
// producer must already have been started.
func (s *Server) SetDLQProducer(p *DLQProducer) { s.dlqProducer = p }

// StopDLQProducer gracefully stops the DLQ producer if one is wired. Safe to
// call when none is set (no-op). Call before CloseExtractionNATS.
func (s *Server) StopDLQProducer() {
	if s.dlqProducer != nil {
		s.dlqProducer.Stop()
	}
}
```

- [ ] **Step 2: Wire startup in `wireExtractionNATS`**

In `cmd/server/main.go`, inside `wireExtractionNATS`, BEFORE the `rc := api.NewResultConsumer(...)` line, insert (so the DLQ + advisory streams exist before the result-consumer binds `TMI_DLQ`):

```go
	// Start the DLQ producer first so the TMI_DLQ stream exists before the
	// result-consumer binds it. Non-fatal on failure: dead-lettering is simply
	// unavailable, and stuck rows still get the access-poller timeout backstop.
	dlqProducer := api.NewDLQProducer(conn)
	if dlqErr := dlqProducer.Start(context.Background()); dlqErr != nil {
		logger.Warn("dlq-producer failed to start (non-fatal): %v", dlqErr)
	} else {
		apiServer.SetDLQProducer(dlqProducer)
		logger.Info("dlq-producer started")
	}
```

- [ ] **Step 3: Wire shutdown**

In `cmd/server/main.go`, in the shutdown path, immediately AFTER `apiServer.StopResultConsumer()` and BEFORE `apiServer.CloseExtractionNATS()`, insert:

```go
	// Stop the DLQ producer before closing NATS so in-flight acks complete.
	apiServer.StopDLQProducer()
```

- [ ] **Step 4: Build and run unit tests**

Run: `make build-server`
Expected: builds cleanly.

Run: `make test-unit`
Expected: PASS (full suite; no regressions).

- [ ] **Step 5: Commit**

```bash
git add api/server.go cmd/server/main.go
git commit -m "feat(server): start/stop the DLQ producer with the extraction pipeline

Refs #437"
```

---

## Task 7: Gated integration test — crash → DLQ → failed

Simulates a crashed worker with a real consumer whose handler always Naks (never publishes a result), `MaxDeliver=1`, short `AckWait`. Asserts the job reaches `jobs.dlq` and the source message is deleted. Gated by `TMI_RUN_NATS_TESTS`, consistent with `internal/worker/pipeline_integration_test.go`.

**Files:**
- Create: `api/dlq_integration_test.go`

- [ ] **Step 1: Write the gated integration test**

Create `api/dlq_integration_test.go`:

```go
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
	t.Cleanup(func() { _ = js.DeleteStream(context.Background(), srcStream) })

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
	if _, err := dlqStream.Info(ctx); err != nil {
		t.Fatalf("DLQ stream info: %v", err)
	}
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
```

- [ ] **Step 2: Build the test (compile-only without NATS)**

Run: `make test-unit name=TestDLQProducer_DeadLettersCrashedJob`
Expected: the test compiles and SKIPs (no `TMI_RUN_NATS_TESTS`). Confirms no compile errors.

- [ ] **Step 3: Run against live NATS**

Run: `make start-dev` (brings up NATS), then in another shell:
`TMI_RUN_NATS_TESTS=1 make test-unit name=TestDLQProducer_DeadLettersCrashedJob count1=true`
Expected: PASS — the dead-lettered job appears on `jobs.dlq` and the source stream drains.

(If `make test-unit` does not forward `TMI_RUN_NATS_TESTS`, run the equivalent the worker integration tests use — check `internal/worker/pipeline_integration_test.go`'s make invocation, e.g. `make test-workers`, and mirror it.)

- [ ] **Step 4: Commit**

```bash
git add api/dlq_integration_test.go
git commit -m "test(api): gated integration test for DLQ dead-lettering of crashed jobs

Refs #437"
```

---

## Task 8: Quality gates, Oracle review, follow-up, close-out

**Files:** none (verification + bookkeeping).

- [ ] **Step 1: Lint, build, full unit suite**

Run: `make lint`
Expected: 0 issues.

Run: `make build-server`
Expected: builds cleanly.

Run: `make test-unit`
Expected: full suite PASS.

- [ ] **Step 2: Oracle DB compatibility review**

The change calls into `ExtractionJobStore.MarkTerminal` and `UpdateAccessStatusWithDiagnostics` but introduces no new SQL or schema. Per CLAUDE.md, dispatch the `oracle-db-admin` subagent anyway (repository-layer call paths). Address any BLOCKING findings; fold APPROVED-WITH-NOTES items in or file follow-ups.

Invoke the `oracle-db-admin` skill.

- [ ] **Step 3: Security regression scan**

Invoke the `security-regression` skill (the change adds outbound JetStream publish + a new consumer; confirm no regression of fixed vulns). Expected: PASS.

- [ ] **Step 4: Integration tests (PostgreSQL)**

Run: `make test-integration`
Expected: PASS (no DB-path regressions).

- [ ] **Step 5: File the tmi-ux localization follow-up**

Use the `file-client-bug` skill (or `gh issue create` in `ericfitz/tmi-ux`) to file: "Localize new `extraction_dead_lettered` access-diagnostics reason code." Reference TMI #437.

- [ ] **Step 6: Close out #437**

Per CLAUDE.md, commits on `dev/1.4.0` do not auto-close. After the work is pushed:

```bash
git branch --show-current   # confirm dev/1.4.0
gh issue comment 437 --body "Resolved on dev/1.4.0: TMI_DLQ stream + advisory-driven dead-letter producer wired; crashed-worker jobs now transition cleanly to failed with reason extraction_dead_lettered. Integration test added (gated). tmi-ux localization follow-up filed."
gh issue close 437
```

- [ ] **Step 7: Push**

```bash
git pull --rebase
git push
git status   # MUST show up to date with origin
```

---

## Self-Review Notes

- **Spec coverage:** TMI_DLQ stream (Task 4), advisory-capture stream + producer (Tasks 3–4), GetMsg→publish→delete (Task 4), validate-as-Job scoping (Task 3), ResultConsumer DLQ subscription + synthesized failed Result (Task 5), input-blob cleanup (Task 5), new reason code + retry remediation (Task 2), monolith wiring + shutdown (Task 6), gated crash integration test (Task 7), oracle review + tmi-ux follow-up (Task 8). All spec sections mapped.
- **Loop guard:** `isSelfReferentialStream` skips `TMI_RESULTS`/`TMI_DLQ` advisories (Task 3) so a stuck DLQ message can never re-dead-letter itself.
- **Type consistency:** `synthesizeDLQResult`, `decodeJobForDLQ`, `parseMaxDeliverAdvisory`, `isSelfReferentialStream`, `makeDLQCallback`, `NewDLQProducer`, `SetDLQProducer`/`StopDLQProducer` names are used identically across tasks. `jobenvelope.Job.Input.ObjectRef`, `jobenvelope.Result{JobID,Status,ReasonCode,ReasonDetail}`, `jetstream.ErrMsgNotFound`, `jetstream.ErrStreamNotFound` match the verified library/source surface.
