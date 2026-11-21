package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/internal/config"
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

type fakeStampedProvider struct {
	sc  config.StampedConfig
	err error
}

func (f fakeStampedProvider) Get(context.Context) (config.StampedConfig, error) { return f.sc, f.err }

func TestPublishExtractionJob_StampsEffectiveEmbeddingProfile(t *testing.T) {
	// Row-wins values flow through the real provider over a settings reader.
	reader := fakeSettingsReader{vals: map[string]string{
		"timmy.text_embedding_model":    "row-model",
		"timmy.text_embedding_base_url": "https://row.example.com/v1",
		"timmy.embedding_dimension":     "1024",
	}}
	bus := &fakeExtractionBus{}
	p := &ExtractionPublisher{bus: bus, store: &fakeQueuedInserter{}, stamped: NewStampedConfigProvider(reader)}

	_, err := p.Publish(context.Background(), ExtractionRequest{DocumentID: "d", ContentType: "text/plain", Bytes: []byte("x")})
	require.NoError(t, err)
	require.NotNil(t, bus.pubJob.Config)
	require.NotNil(t, bus.pubJob.Config.Embedding)
	assert.Equal(t, jobenvelope.EmbeddingProfile{Model: "row-model", Endpoint: "https://row.example.com/v1", Dimension: 1024}, *bus.pubJob.Config.Embedding)
}

func TestPublishExtractionJob_OmitsConfigWhenNotStampable(t *testing.T) {
	cases := map[string]fakeStampedProvider{
		"provider error": {err: errors.New("db down")},
		"empty model":    {sc: config.StampedConfig{Embedding: config.EmbeddingProfile{Endpoint: "https://h/v1"}}},
		"empty endpoint": {sc: config.StampedConfig{Embedding: config.EmbeddingProfile{Model: "m"}}},
	}
	for name, prov := range cases {
		t.Run(name, func(t *testing.T) {
			bus := &fakeExtractionBus{}
			p := &ExtractionPublisher{bus: bus, store: &fakeQueuedInserter{}, stamped: prov}
			_, err := p.Publish(context.Background(), ExtractionRequest{DocumentID: "d", ContentType: "text/plain", Bytes: []byte("x")})
			require.NoError(t, err, "a stamping problem must not fail the publish")
			assert.Nil(t, bus.pubJob.Config)
		})
	}
}

func TestPublishExtractionJob_NoProviderStampsNothing(t *testing.T) {
	bus := &fakeExtractionBus{}
	p := &ExtractionPublisher{bus: bus, store: &fakeQueuedInserter{}}
	_, err := p.Publish(context.Background(), ExtractionRequest{DocumentID: "d", ContentType: "text/plain", Bytes: []byte("x")})
	require.NoError(t, err)
	assert.Nil(t, bus.pubJob.Config)
}
