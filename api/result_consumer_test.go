package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingJobStore struct{ terminalJobID, terminalStatus, terminalReason string }

func (r *recordingJobStore) MarkTerminal(_ context.Context, jobID, status, reasonCode string) error {
	r.terminalJobID, r.terminalStatus, r.terminalReason = jobID, status, reasonCode
	return nil
}

type recordingDocUpdater struct {
	id, status, reason string
	called             bool
}

func (r *recordingDocUpdater) UpdateAccessStatusWithDiagnostics(_ context.Context, id, accessStatus, _ /*contentSource*/, reasonCode, _ /*reasonDetail*/ string) error {
	r.id, r.status, r.reason, r.called = id, accessStatus, reasonCode, true
	return nil
}

type recordingEmitter struct{ eventType, objectID string }

func (r *recordingEmitter) emit(_ context.Context, eventType, documentID, _, _ string) {
	r.eventType, r.objectID = eventType, documentID
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
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-7-result"},
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
