package api

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// terminalMarker can persist the terminal state of an extraction job. It
// returns whether this call performed the first terminal transition, which the
// consumer uses to emit the webhook exactly once under at-least-once redelivery.
type terminalMarker interface {
	MarkTerminal(ctx context.Context, jobID, status, reasonCode string) (bool, error)
}

// docAccessUpdater can update a document's access status with diagnostic fields.
type docAccessUpdater interface {
	UpdateAccessStatusWithDiagnostics(
		ctx context.Context,
		id, accessStatus, contentSource, reasonCode, reasonDetail string,
	) error
}

// blobDeleter can delete a payload blob from the Object Store by object_ref.
type blobDeleter interface {
	DeletePayload(ctx context.Context, ref string) error
}

// emitFunc is the signature of the event-emission closure used by ResultConsumer.
type emitFunc func(ctx context.Context, eventType, documentID, threatModelID, ownerID string)

// ResultConsumer subscribes to the TMI_RESULTS JetStream stream and, per
// result message, upserts the extraction_jobs terminal state, updates the
// document's access_status, emits a webhook event, and deletes the result
// blob. It is safe to use concurrently; the JetStream callback is invoked
// serially (single goroutine) by the nats.go library.
//
// DLQ note: the dead-letter path is now wired. The DLQ producer
// (api/dlq_producer.go) republishes the original Job envelope of any job that
// exhausted redelivery to SubjectDLQ ("jobs.dlq"); this consumer binds the
// TMI_DLQ stream (see makeDLQCallback) and turns each dead-lettered Job into a
// failed terminal transition.
type ResultConsumer struct {
	conn           *worker.Conn
	jobs           terminalMarker
	docs           docAccessUpdater
	blobs          blobDeleter
	emit           emitFunc
	lookupDocument func(ctx context.Context, jobID string) (docRef, threatModelID, ownerID string, ok bool)
	cancel         context.CancelFunc
}

// handleResult is the pure, testable core of the result consumer. It
// classifies the outcome, upserts the extraction_jobs terminal state, updates
// the document's access_status, emits a webhook event, and cleans up blobs.
// Returning a non-nil error signals a transient failure; the caller should Nak
// the message for redelivery.
func (rc *ResultConsumer) handleResult(ctx context.Context, res jobenvelope.Result) error {
	logger := slogging.Get()

	// Classify outcome.
	status := models.ExtractionStatusCompleted
	accessStatus := AccessStatusAccessible
	eventType := EventDocumentExtractionCompleted
	reasonCode := ""
	reasonDetail := ""
	if res.Status == jobenvelope.StatusFailed {
		status = models.ExtractionStatusFailed
		accessStatus = AccessStatusExtractionFailed
		eventType = EventDocumentExtractionFailed
		reasonCode = res.ReasonCode
		reasonDetail = res.ReasonDetail
	}

	// Look up the document associated with this job. Done before the terminal
	// flip so the idempotent access-status update below can fail and redeliver
	// without having consumed the one-shot emit signal (see MarkTerminal note).
	docRef, tmID, ownerID, ok := rc.lookupDocument(ctx, res.JobID)
	if !ok {
		// Document was deleted before the result arrived; no document to update
		// and no webhook to emit, but we still record the terminal state below.
		logger.Warn("result-consumer: document for job %s no longer exists; dropping", res.JobID)
	} else {
		// Update document access status (transient failure → redeliver).
		// Idempotent, so a redelivery re-running it is harmless.
		if err := rc.docs.UpdateAccessStatusWithDiagnostics(
			ctx, docRef, accessStatus, "", reasonCode, reasonDetail,
		); err != nil {
			return err
		}
	}

	// Flip the durable terminal state. transitioned is true only on the first
	// delivery that moves the row from non-terminal to terminal — the emit-once
	// signal (#438). A transient failure redelivers without emitting.
	transitioned, err := rc.jobs.MarkTerminal(ctx, res.JobID, status, reasonCode)
	if err != nil {
		return err
	}

	// Emit the webhook exactly once, on the first terminal transition only.
	// Nothing between MarkTerminal and here can error or redeliver (emit is
	// best-effort and never Naks), so a transition always reaches this emit in
	// the same delivery — the only residual loss window is a process crash in
	// this sub-millisecond gap, far smaller than the prior at-least-once dupes.
	if transitioned && ok && rc.emit != nil {
		rc.emit(ctx, eventType, docRef, tmID, ownerID)
	}

	// Delete the result blob from the Object Store (best-effort; log on failure).
	if rc.blobs != nil && res.Output.ResultRef != "" {
		if err := rc.blobs.DeletePayload(ctx, res.Output.ResultRef); err != nil {
			logger.Warn("result-consumer: blob cleanup for job %s failed: %v", res.JobID, err)
		}
	}

	return nil
}

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

// Start subscribes to the TMI_RESULTS stream and begins processing result
// messages in the background. It returns nil immediately after the JetStream
// consumer is created; actual message processing happens in the consume
// callback goroutine managed by the nats.go library.
//
// If the TMI_RESULTS stream does not yet exist (e.g. no workers have run),
// Start logs a warning and returns nil — the async result path is simply
// unavailable until a worker creates the stream.
//
// The provided ctx controls the lifetime of the consumer; call Stop() to
// release resources explicitly.
func (rc *ResultConsumer) Start(ctx context.Context) error {
	logger := slogging.Get()

	ctx, rc.cancel = context.WithCancel(ctx)

	js := rc.conn.JetStream()

	// Look up the result stream. It is created by workers when they first
	// publish a result; we do not create it here because we are the consumer
	// side only.
	stream, err := js.Stream(ctx, worker.ResultStream)
	if err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			logger.Warn("result-consumer: stream %s not found; async result processing unavailable until a worker publishes its first result", worker.ResultStream)
			return nil
		}
		return err
	}

	// Create (or bind to) a durable consumer that filters to jobs.result.>
	// only. The dead-letter subject is handled by a separate consumer on the
	// TMI_DLQ stream, added below.
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "monolith-result-consumer",
		FilterSubject: worker.SubjectResultPrefix + ">",
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       30 * time.Second,
		MaxDeliver:    5,
	})
	if err != nil {
		return err
	}

	cc, err := cons.Consume(rc.makeCallback(ctx))
	if err != nil {
		return err
	}

	// Stop the consume context when our context is cancelled.
	go func() {
		<-ctx.Done()
		cc.Stop()
		logger.Info("result-consumer: shut down")
	}()

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

	logger.Info("result-consumer: subscribed to %s/%s", worker.ResultStream, worker.SubjectResultPrefix+">")
	return nil
}

// makeCallback returns the JetStream message handler. It is extracted from
// Start so the recover() guard and the decode/dispatch logic are easy to read.
// The callback MUST NOT panic — a panic here would crash the monolith.
func (rc *ResultConsumer) makeCallback(ctx context.Context) func(jetstream.Msg) {
	logger := slogging.Get()

	return func(msg jetstream.Msg) {
		// Panic guard: the monolith must never crash from a bad result message.
		defer func() {
			if r := recover(); r != nil {
				logger.Error("result-consumer: panic processing message on %s: %v — terminating message", msg.Subject(), r)
				_ = msg.Term()
			}
		}()

		var res jobenvelope.Result
		if err := json.Unmarshal(msg.Data(), &res); err != nil {
			logger.Error("result-consumer: undecodable message on %s: %v — terminating", msg.Subject(), err)
			_ = msg.Term()
			return
		}

		if err := rc.handleResult(ctx, res); err != nil {
			logger.Warn("result-consumer: transient failure for job %s: %v — redelivering", res.JobID, err)
			_ = msg.Nak()
			return
		}

		_ = msg.Ack()
	}
}

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

// Stop cancels the consumer's context, which causes the background goroutine
// to call cc.Stop() and release JetStream resources. Safe to call when Start
// was never called or when the stream was not found (no-op).
func (rc *ResultConsumer) Stop() {
	if rc.cancel != nil {
		rc.cancel()
	}
}

// NewResultConsumer constructs a ResultConsumer wired to real dependencies.
// Pass conn, the ExtractionJobStore, and the DocumentRepository; the
// constructor wires the emit closure and the lookupDocument function.
//
// lookupDocument enrichment: the function queries extraction_jobs for the
// document_ref, then fetches the document's ThreatModelID via
// DocumentRepository.GetThreatModelID and the owner via
// DocumentRepository.GetPickerDispatch. If either secondary lookup fails the
// function degrades gracefully: the access_status update (which only needs
// document_ref) still proceeds; the webhook's threat_model_id / owner_id
// fields are left empty.
func NewResultConsumer(
	conn *worker.Conn,
	jobStore *ExtractionJobStore,
	docs DocumentRepository,
) *ResultConsumer {
	rc := &ResultConsumer{
		conn:  conn,
		jobs:  jobStore,
		docs:  docs,
		blobs: conn,
	}

	// Emit closure: guard against a nil GlobalEventEmitter (e.g. in tests or
	// when Redis is not configured).
	rc.emit = func(ctx context.Context, eventType, documentID, threatModelID, ownerID string) {
		if GlobalEventEmitter == nil {
			return
		}
		if err := GlobalEventEmitter.EmitEvent(ctx, EventPayload{
			EventType:     eventType,
			ThreatModelID: threatModelID,
			ObjectID:      documentID,
			ObjectType:    "document",
			OwnerID:       ownerID,
			Timestamp:     time.Now().UTC(),
		}); err != nil {
			slogging.Get().Warn("result-consumer: event emission failed for %s/%s: %v", eventType, documentID, err)
		}
	}

	// lookupDocument: resolve job → document_ref → threat_model_id + owner.
	rc.lookupDocument = func(ctx context.Context, jobID string) (string, string, string, bool) {
		logger := slogging.Get()

		// Step 1: get document_ref from extraction_jobs.
		docRef, err := jobStore.GetDocumentRef(ctx, jobID)
		if err != nil {
			logger.Warn("result-consumer: lookup document_ref for job %s: %v", jobID, err)
			return "", "", "", false
		}
		if docRef == "" || docRef == unknownDocumentRef {
			// Either the row does not exist (document deleted before the result
			// arrived) or it was created by MarkTerminal's bare-upsert-insert
			// path with the unknownDocumentRef sentinel (a terminal result with
			// no prior queued row). In both cases the real document is unknown,
			// so there is nothing to update and no webhook to emit — drop.
			return "", "", "", false
		}

		// Step 2: get ThreatModelID from the document (lightweight, no cache).
		threatModelID, err := docs.GetThreatModelID(ctx, docRef)
		if err != nil {
			// Document may have been deleted; degrade to docRef-only.
			logger.Warn("result-consumer: GetThreatModelID for doc %s: %v — proceeding without threat_model_id", docRef, err)
			return docRef, "", "", true
		}

		// Step 3: get owner from documents+threat_models join via GetPickerDispatch.
		// We only need the ownerInternalUUID; the picker metadata is discarded.
		_, ownerID, err := docs.GetPickerDispatch(ctx, docRef)
		if err != nil {
			logger.Warn("result-consumer: GetPickerDispatch for doc %s: %v — proceeding without owner_id", docRef, err)
			return docRef, threatModelID, "", true
		}

		return docRef, threatModelID, ownerID, true
	}

	return rc
}
