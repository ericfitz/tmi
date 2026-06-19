package main

import (
	"context"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// subjectTypeToken maps a MIME content type to the extract-subject token
// (ooxml | pdf | html | plaintext). Unknown types fall back to plaintext.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: map a MIME content type to the extractor subject token (pure)
func subjectTypeToken(contentType string) string {
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

// buildExtractorRegistry constructs the registry with every extractor the
// worker hosts, in priority order. Identical set to the monolith's
// in-process registry — the extractors are the same pkg/extract code.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: build the content extractor registry with all supported formats in priority order (pure)
func buildExtractorRegistry(limits extract.Limits) *extract.ContentExtractorRegistry {
	reg := extract.NewContentExtractorRegistry()
	reg.Register(extract.NewDOCXExtractor(limits))
	reg.Register(extract.NewPPTXExtractor(limits))
	reg.Register(extract.NewXLSXExtractor(limits))
	reg.Register(extract.NewPDFExtractor())
	reg.Register(extract.NewHTMLExtractor())
	reg.Register(extract.NewPlainTextExtractor())
	return reg
}

// extractHandler is the JobHandler for tmi-extractor.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: worker job handler that extracts text from source blobs and publishes to the chunkembed stage
type extractHandler struct {
	conn   *worker.Conn
	reg    *extract.ContentExtractorRegistry
	limits extract.Limits
}

// newExtractHandler builds the handler. limits carries the per-extraction
// caps and the wall-clock budget.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: build an extractHandler with a worker connection and extraction limits (pure)
func newExtractHandler(conn *worker.Conn, limits extract.Limits) *extractHandler {
	return &extractHandler{conn: conn, reg: buildExtractorRegistry(limits), limits: limits}
}

// Handle reads the source blob, extracts text, and publishes the next-stage
// job or a failure result. A terminal extraction failure is published as a
// result envelope here and a terminal *worker.JobError is returned so the
// consumer terminates the message.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: fetch blob, extract text via registry, and publish extracted content or failure result
func (h *extractHandler) Handle(ctx context.Context, job jobenvelope.Job) error {
	logger := slogging.Get()

	data, err := h.conn.GetPayload(ctx, job.Input.ObjectRef)
	if err != nil {
		// A missing blob is transient if the put has not propagated; after
		// AckWait redeliveries it dead-letters. Treat as transient (return
		// the raw error -> consumer Naks).
		return err
	}

	ext, ok := h.reg.FindExtractor(job.ContentType)
	var out extract.ExtractedContent
	if !ok {
		// No extractor: pass the bytes through as text (mirrors the
		// monolith pipeline's no-extractor branch).
		out = extract.ExtractedContent{Text: string(data), ContentType: job.ContentType}
	} else {
		out, err = h.extract(ctx, ext, data, job)
		if err != nil {
			return h.publishFailure(ctx, job, err)
		}
	}

	textRef, err := h.conn.PutPayload(ctx, job.JobID+"/extracted", []byte(out.Text))
	if err != nil {
		return err // transient -> redeliver
	}

	next := jobenvelope.Job{
		JobID:       job.JobID,
		ContentType: job.ContentType,
		Limits:      job.Limits,
		Deadline:    job.Deadline,
		Input:       jobenvelope.Input{ObjectRef: textRef, ByteSize: int64(len(out.Text))},
		Metadata:    mergeMetadata(job.Metadata, out),
	}
	b, err := jsonMarshal(next, "job")
	if err != nil {
		return err
	}
	if err := h.conn.Publish(ctx, worker.ChunkEmbedSubject(job.JobID), b); err != nil {
		return err
	}
	logger.Debug("tmi-extractor: job %s extracted, %d bytes -> chunkembed", job.JobID, len(out.Text))
	return nil
}

// extract runs the chosen extractor under the wall-clock budget, using the
// context-aware path when the extractor supports it.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: run a content extractor under a wall-clock deadline budget (pure)
func (h *extractHandler) extract(ctx context.Context, ext extract.ContentExtractor,
	data []byte, job jobenvelope.Job) (extract.ExtractedContent, error) {
	budget := h.limits.WallClockBudget
	if job.Limits.WallClock.Std() > 0 {
		budget = job.Limits.WallClock.Std()
	}
	if be, isBounded := ext.(extract.BoundedExtractor); isBounded && be.Bounded() && budget > 0 {
		if ce, isCtx := ext.(extract.ContextAwareExtractor); isCtx {
			return extract.ExtractWithDeadline(ctx, budget, func(dctx context.Context) (extract.ExtractedContent, error) {
				return ce.ExtractCtx(dctx, data, job.ContentType)
			})
		}
		return extract.ExtractWithDeadline(ctx, budget, func(_ context.Context) (extract.ExtractedContent, error) {
			return ext.Extract(data, job.ContentType)
		})
	}
	return ext.Extract(data, job.ContentType)
}

// publishFailure classifies the error, publishes a failed Result envelope,
// and returns a terminal *worker.JobError so the message is terminated.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: publish a terminal extraction failure result envelope and return a terminal job error
func (h *extractHandler) publishFailure(ctx context.Context, job jobenvelope.Job, extractErr error) error {
	c := extract.ClassifyError(extractErr)
	res := jobenvelope.Result{
		JobID:        job.JobID,
		Status:       jobenvelope.StatusFailed,
		ReasonCode:   c.ReasonCode,
		ReasonDetail: c.ReasonDetail,
	}
	b, err := jsonMarshal(res, "result")
	if err != nil {
		return err
	}
	if err := h.conn.Publish(ctx, worker.ResultSubject(job.JobID), b); err != nil {
		return err // transient publish failure -> redeliver and retry
	}
	return &worker.JobError{ReasonCode: c.ReasonCode, Detail: c.ReasonDetail, Terminal: true}
}

// mergeMetadata folds the extractor's title into the forwarded metadata.
// SEM@36720db6f1f6739799ded7c10487674e25b41268: merge extractor output title into forwarded job metadata map (pure)
func mergeMetadata(in map[string]string, out extract.ExtractedContent) map[string]string {
	m := map[string]string{}
	for k, v := range in {
		m[k] = v
	}
	if out.Title != "" {
		m["title"] = out.Title
	}
	return m
}
