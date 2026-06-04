package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeExtractionBus struct {
	putName    string
	putData    []byte
	putRef     string
	pubSubject string
	pubJob     jobenvelope.Job
}

func (f *fakeExtractionBus) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	f.putName, f.putData = name, data
	f.putRef = "TMI_PAYLOADS/" + name
	return f.putRef, nil
}
func (f *fakeExtractionBus) PublishJob(ctx context.Context, subject string, job jobenvelope.Job) error {
	f.pubSubject, f.pubJob = subject, job
	return nil
}

type fakeQueuedInserter struct{ jobID, docRef string }

func (f *fakeQueuedInserter) InsertQueued(ctx context.Context, jobID, documentRef string) error {
	f.jobID, f.docRef = jobID, documentRef
	return nil
}

func TestPublishExtractionJob_PublishesAndQueues(t *testing.T) {
	bus := &fakeExtractionBus{}
	store := &fakeQueuedInserter{}
	p := &ExtractionPublisher{bus: bus, store: store}

	jobID, err := p.Publish(context.Background(), ExtractionRequest{
		DocumentID:  "doc-9",
		ContentType: "application/pdf",
		Bytes:       []byte("%PDF-1.7"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
	assert.Equal(t, jobID, store.jobID)
	assert.Equal(t, "doc-9", store.docRef)
	assert.Equal(t, bus.putRef, bus.pubJob.Input.ObjectRef)
	assert.Equal(t, "jobs.extract.pdf", bus.pubSubject) // pdf kind for application/pdf
	assert.Equal(t, "application/pdf", bus.pubJob.ContentType)
	assert.Equal(t, int64(len("%PDF-1.7")), bus.pubJob.Input.ByteSize)
}
