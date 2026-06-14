package api

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingJobStore struct {
	terminalJobID, terminalStatus, terminalReason string
	markCalls                                     int
	// notFirst makes MarkTerminal report a non-first-transition (already
	// terminal). The zero value reports the first transition (true), so existing
	// tests that expect the webhook to fire keep working without changes.
	notFirst bool
}

func (r *recordingJobStore) MarkTerminal(_ context.Context, jobID, status, reasonCode string) (bool, error) {
	r.terminalJobID, r.terminalStatus, r.terminalReason = jobID, status, reasonCode
	r.markCalls++
	return !r.notFirst, nil
}

type recordingDocUpdater struct {
	id, status, reason, detail string
	called                     bool
}

func (r *recordingDocUpdater) UpdateAccessStatusWithDiagnostics(_ context.Context, id, accessStatus, _ /*contentSource*/, reasonCode, reasonDetail string) error {
	r.id, r.status, r.reason, r.detail, r.called = id, accessStatus, reasonCode, reasonDetail, true
	return nil
}

type recordingEmitter struct {
	eventType, objectID string
	calls               int
}

func (r *recordingEmitter) emit(_ context.Context, eventType, documentID, _, _ string) {
	r.eventType, r.objectID = eventType, documentID
	r.calls++
}

type recordingBlobDeleter struct{ deleted []string }

func (r *recordingBlobDeleter) DeletePayload(_ context.Context, ref string) error {
	r.deleted = append(r.deleted, ref)
	return nil
}

func newTestResultConsumer(jobs *recordingJobStore, docs *recordingDocUpdater, em *recordingEmitter, blobs *recordingBlobDeleter) *ResultConsumer {
	return &ResultConsumer{
		jobs:  jobs,
		docs:  docs,
		emit:  em.emit,
		blobs: blobs,
		lookupDocument: func(_ context.Context, _ string) (string, string, string, bool) {
			return "doc-7", "tm-1", "owner-1", true
		},
	}
}

func TestResultConsumer_Completed(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-7",
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-7/result"},
	})
	require.NoError(t, err)
	assert.Equal(t, models.ExtractionStatusCompleted, jobs.terminalStatus)
	assert.True(t, docs.called)
	assert.Equal(t, AccessStatusAccessible, docs.status)
	assert.Equal(t, EventDocumentExtractionCompleted, em.eventType)
	assert.NotEmpty(t, blobs.deleted)
}

func TestResultConsumer_Failed_ClassifiesReason(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:      "job-8",
		Status:     jobenvelope.StatusFailed,
		ReasonCode: "extraction_limit:timeout",
	})
	require.NoError(t, err)
	assert.Equal(t, models.ExtractionStatusFailed, jobs.terminalStatus)
	assert.Equal(t, AccessStatusExtractionFailed, docs.status)
	assert.Equal(t, "extraction_limit:timeout", docs.reason)
	assert.Equal(t, EventDocumentExtractionFailed, em.eventType)
}

func TestResultConsumer_DeletedDocument_DropsGracefully(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)
	rc.lookupDocument = func(_ context.Context, _ string) (string, string, string, bool) {
		return "", "", "", false
	}

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-9",
		Status: jobenvelope.StatusCompleted,
	})
	require.NoError(t, err)
	assert.False(t, docs.called)
}

// TestResultConsumer_EmitOnce_OnRedelivery asserts the #438 contract: when
// MarkTerminal reports the row is already terminal (a JetStream redelivery of a
// result that was fully processed before), the consumer still performs the
// idempotent access-status update but does NOT re-emit the webhook.
func TestResultConsumer_EmitOnce_OnRedelivery(t *testing.T) {
	jobs := &recordingJobStore{notFirst: true} // already terminal → not the first transition
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-10",
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-10/result"},
	})
	require.NoError(t, err)

	// Terminal state was still recorded and access status still updated
	// (idempotent), but the webhook was suppressed on the redelivery.
	assert.Equal(t, 1, jobs.markCalls)
	assert.True(t, docs.called, "access-status update must still run on redelivery")
	assert.Equal(t, 0, em.calls, "webhook must not be re-emitted on redelivery")
	// Blob cleanup remains best-effort and idempotent — still attempted.
	assert.NotEmpty(t, blobs.deleted)
}

// TestResultConsumer_FirstTransition_Emits is the positive counterpart: the
// first terminal transition emits exactly one webhook.
func TestResultConsumer_FirstTransition_Emits(t *testing.T) {
	jobs := &recordingJobStore{} // notFirst=false → first transition
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-11",
		Status: jobenvelope.StatusCompleted,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, em.calls)
	assert.Equal(t, EventDocumentExtractionCompleted, em.eventType)
}

// fakeJSMsg is a minimal jetstream.Msg implementation for exercising the
// consumer callbacks (subject binding, ack/nak/term decisions).
type fakeJSMsg struct {
	subject string
	data    []byte
	acked   bool
	naked   bool
	termed  bool
}

func (m *fakeJSMsg) Metadata() (*jetstream.MsgMetadata, error) { return &jetstream.MsgMetadata{}, nil }
func (m *fakeJSMsg) Data() []byte                              { return m.data }
func (m *fakeJSMsg) Headers() nats.Header                      { return nil }
func (m *fakeJSMsg) Subject() string                           { return m.subject }
func (m *fakeJSMsg) Reply() string                             { return "" }
func (m *fakeJSMsg) Ack() error                                { m.acked = true; return nil }
func (m *fakeJSMsg) DoubleAck(_ context.Context) error         { m.acked = true; return nil }
func (m *fakeJSMsg) Nak() error                                { m.naked = true; return nil }
func (m *fakeJSMsg) NakWithDelay(_ time.Duration) error        { m.naked = true; return nil }
func (m *fakeJSMsg) InProgress() error                         { return nil }
func (m *fakeJSMsg) Term() error                               { m.termed = true; return nil }
func (m *fakeJSMsg) TermWithReason(_ string) error             { m.termed = true; return nil }

// TestResultConsumer_SubjectMismatch_Terminated asserts the forged-result
// guard: a payload whose job_id does not match the jobs.result.<id> subject
// it arrived on is terminated without flipping terminal state, emitting a
// webhook, or deleting blobs.
func TestResultConsumer_SubjectMismatch_Terminated(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	payload, err := json.Marshal(jobenvelope.Result{
		JobID:  "job-victim",
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-victim/result"},
	})
	require.NoError(t, err)
	msg := &fakeJSMsg{subject: worker.ResultSubject("job-attacker"), data: payload}

	rc.makeCallback(context.Background())(msg)

	assert.True(t, msg.termed, "mismatched subject/job_id must be terminated")
	assert.False(t, msg.acked)
	assert.False(t, msg.naked)
	assert.Equal(t, 0, jobs.markCalls, "forged result must not flip terminal state")
	assert.Equal(t, 0, em.calls, "forged result must not emit a webhook")
	assert.Empty(t, blobs.deleted, "forged result must not delete blobs")
}

// TestResultConsumer_ForeignResultRef_NotDeleted asserts the ref-to-job
// binding: a result whose result_ref names another job's blob still records
// terminal state, but the foreign blob is never deleted.
func TestResultConsumer_ForeignResultRef_NotDeleted(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-12",
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-13/result"},
	})
	require.NoError(t, err)
	assert.Equal(t, models.ExtractionStatusCompleted, jobs.terminalStatus)
	assert.Empty(t, blobs.deleted, "foreign result_ref must not be deleted")
}

// TestResultConsumer_DLQ_ForeignInputRef_NotDeleted covers the dead-letter
// variant: the synthesized failure is processed and acked, but an input
// object_ref not bound to the job's ID is never deleted.
func TestResultConsumer_DLQ_ForeignInputRef_NotDeleted(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	payload, err := json.Marshal(jobenvelope.Job{
		JobID:       "job-14",
		ContentType: "text/plain",
		Input:       jobenvelope.Input{ObjectRef: "TMI_PAYLOADS/job-other-source"},
	})
	require.NoError(t, err)
	msg := &fakeJSMsg{subject: worker.SubjectDLQ, data: payload}

	rc.makeDLQCallback(context.Background())(msg)

	assert.True(t, msg.acked)
	assert.Equal(t, models.ExtractionStatusFailed, jobs.terminalStatus)
	assert.Empty(t, blobs.deleted, "foreign input object_ref must not be deleted")
}

// TestResultConsumer_InvalidEnvelope_Terminated covers the makeCallback
// validation gate: a result on the correct subject but with an envelope that
// fails ValidateResult (here, an unknown status) is terminated — never
// persisted, emitted, or blob-deleted.
func TestResultConsumer_InvalidEnvelope_Terminated(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	payload, err := json.Marshal(jobenvelope.Result{
		JobID:  "job-bad",
		Status: jobenvelope.Status("definitely-not-terminal"),
	})
	require.NoError(t, err)
	msg := &fakeJSMsg{subject: worker.ResultSubject("job-bad"), data: payload}

	rc.makeCallback(context.Background())(msg)

	assert.True(t, msg.termed, "invalid envelope must be terminated")
	assert.False(t, msg.acked)
	assert.False(t, msg.naked)
	assert.Equal(t, 0, jobs.markCalls, "invalid envelope must not flip terminal state")
	assert.Equal(t, 0, em.calls, "invalid envelope must not emit a webhook")
	assert.False(t, docs.called, "invalid envelope must not update document state")
}

// TestResultConsumer_SanitizesReasonDetail covers the makeCallback sanitize
// step: control characters in a worker-supplied reason_detail are stripped
// before the value is persisted (and later served to clients).
func TestResultConsumer_SanitizesReasonDetail(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	payload, err := json.Marshal(jobenvelope.Result{
		JobID:        "job-san",
		Status:       jobenvelope.StatusFailed,
		ReasonCode:   "extraction_malformed",
		ReasonDetail: "slide \x1b[2Jx\x00 done",
	})
	require.NoError(t, err)
	msg := &fakeJSMsg{subject: worker.ResultSubject("job-san"), data: payload}

	rc.makeCallback(context.Background())(msg)

	assert.True(t, msg.acked)
	assert.True(t, docs.called)
	assert.Equal(t, "slide [2Jx done", docs.detail,
		"control characters must be stripped from reason_detail before persistence")
}

// TestResultConsumer_DLQ_InvalidEnvelope_Terminated covers the makeDLQCallback
// validation gate: a dead-letter payload that fails Validate (here, missing
// content_type) is terminated rather than processed or acked.
func TestResultConsumer_DLQ_InvalidEnvelope_Terminated(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	payload, err := json.Marshal(jobenvelope.Job{
		JobID: "job-dlq-bad", // no content_type → fails Validate
		Input: jobenvelope.Input{ObjectRef: "TMI_PAYLOADS/job-dlq-bad-source"},
	})
	require.NoError(t, err)
	msg := &fakeJSMsg{subject: worker.SubjectDLQ, data: payload}

	rc.makeDLQCallback(context.Background())(msg)

	assert.True(t, msg.termed, "invalid DLQ envelope must be terminated")
	assert.False(t, msg.acked)
	assert.Equal(t, 0, jobs.markCalls, "invalid DLQ envelope must not flip terminal state")
	assert.Empty(t, blobs.deleted)
}
