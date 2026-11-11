package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/google/uuid"
)

// extractionBus is the subset of the worker NATS connection the publisher
// needs. Narrowed to an interface so the publisher is unit-testable.
type extractionBus interface {
	PutPayload(ctx context.Context, name string, data []byte) (string, error)
	PublishJob(ctx context.Context, subject string, job jobenvelope.Job) error
}

// queuedInserter is the subset of ExtractionJobStore the publisher needs.
type queuedInserter interface {
	InsertQueued(ctx context.Context, jobID, documentRef string) error
}

// ExtractionRequest is one document's extraction submission.
type ExtractionRequest struct {
	DocumentID  string
	ContentType string
	Bytes       []byte
}

// ExtractionPublisher submits extraction jobs to the worker pipeline.
type ExtractionPublisher struct {
	bus   extractionBus
	store queuedInserter
}

// NewExtractionPublisher wraps a worker.Conn and the job store.
func NewExtractionPublisher(conn *worker.Conn, store *ExtractionJobStore) *ExtractionPublisher {
	return &ExtractionPublisher{bus: &connBusAdapter{conn: conn}, store: store}
}

// Publish writes the document bytes to the Object Store, publishes an extract
// job, and records a queued row. Returns the job_id for caller correlation.
func (p *ExtractionPublisher) Publish(ctx context.Context, req ExtractionRequest) (string, error) {
	jobID := uuid.New().String()

	ref, err := p.bus.PutPayload(ctx, "job-"+jobID+"-source", req.Bytes)
	if err != nil {
		return "", fmt.Errorf("extraction publisher: put payload: %w", err)
	}

	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: req.ContentType,
		Input:       jobenvelope.Input{ObjectRef: ref, ByteSize: int64(len(req.Bytes))},
	}
	subject := worker.SubjectExtractPrefix + extractSubjectSuffix(req.ContentType)
	if err := p.bus.PublishJob(ctx, subject, job); err != nil {
		return "", fmt.Errorf("extraction publisher: publish: %w", err)
	}

	if err := p.store.InsertQueued(ctx, jobID, req.DocumentID); err != nil {
		return "", fmt.Errorf("extraction publisher: queue row: %w", err)
	}

	return jobID, nil
}

// extractSubjectSuffix maps a content type to the extract subject kind
// (jobs.extract.<kind>; see cmd/extractor/handler.go subjectTypeToken).
// The extractor filters jobs.extract.> and routes by ContentType, so the
// suffix is a stream-filter hint. Kinds: plaintext / ooxml / pdf / html.
// Matching logic mirrors cmd/extractor/handler.go subjectTypeToken exactly.
func extractSubjectSuffix(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "application/pdf"):
		return "pdf"
	case strings.Contains(ct, "text/html"):
		return "html"
	case strings.Contains(ct, "openxmlformats-officedocument"):
		return "ooxml"
	case strings.HasPrefix(ct, "text/plain"), strings.HasPrefix(ct, "text/csv"):
		return "plaintext"
	default:
		return "plaintext"
	}
}

// connBusAdapter adapts *worker.Conn to extractionBus, marshaling the Job
// as plain JSON — the wire format the extractor worker expects (see
// cmd/extractor/json.go jsonMarshal, which is a thin json.Marshal wrapper).
type connBusAdapter struct{ conn *worker.Conn }

func (a *connBusAdapter) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	return a.conn.PutPayload(ctx, name, data)
}

func (a *connBusAdapter) PublishJob(ctx context.Context, subject string, job jobenvelope.Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("extraction publisher: marshal job: %w", err)
	}
	return a.conn.Publish(ctx, subject, data)
}
