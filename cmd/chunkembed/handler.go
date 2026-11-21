package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// EmbeddingResult is the chunk-embed stage output blob, written to the
// Object Store and referenced from the result envelope. The monolith's result
// consumer does not read it yet: it deletes the blob after recording the
// result (ingestion is tracked in #1186).
type EmbeddingResult struct {
	// Chunks are the text chunks in order.
	Chunks []string `json:"chunks"`
	// Vectors holds one embedding per chunk, index-aligned with Chunks.
	Vectors [][]float32 `json:"vectors"`
}

// validate checks the result is internally consistent.
func (r EmbeddingResult) validate() error {
	if len(r.Chunks) != len(r.Vectors) {
		return fmt.Errorf("chunk/vector count mismatch: %d chunks, %d vectors",
			len(r.Chunks), len(r.Vectors))
	}
	return nil
}

// jobConn is the subset of *worker.Conn the handler uses; narrowed so the
// failure paths are unit-testable.
type jobConn interface {
	GetPayload(ctx context.Context, ref string) ([]byte, error)
	PutPayload(ctx context.Context, name string, data []byte) (string, error)
	Publish(ctx context.Context, subject string, data []byte) error
	PublishFailureResult(ctx context.Context, jobID, reasonCode, detail string) error
}

// chunkEmbedHandler is the JobHandler for tmi-chunk-embed.
type chunkEmbedHandler struct {
	conn      jobConn
	chunker   *extract.TextChunker
	embedders *embedderPool
}

// chunk sizing — characters per chunk and overlap. These mirror the
// monolith's Timmy chunker defaults (internal/config/timmy.go: ChunkSize
// 512, ChunkOverlap 50) so ingest-time chunking here agrees with the
// monolith's query-time chunking. Plan 3 / #415 replaces these hardcoded
// values with the projected shared-config object.
const (
	chunkMaxChars = 512
	chunkOverlap  = 50
)

// newChunkEmbedHandler builds the handler.
func newChunkEmbedHandler(conn jobConn, embedders *embedderPool) *chunkEmbedHandler {
	return &chunkEmbedHandler{
		conn:      conn,
		chunker:   extract.NewTextChunker(chunkMaxChars, chunkOverlap),
		embedders: embedders,
	}
}

// Handle reads the extracted-text blob, chunks + embeds it, writes the
// result blob, and publishes the final result envelope.
func (h *chunkEmbedHandler) Handle(ctx context.Context, job jobenvelope.Job) error {
	logger := slogging.Get()

	// Resolve the embedder first: a stamped endpoint that is not allowlisted
	// fails fast, before any payload is read. The error carries the host but
	// no credentials.
	embedder, wantDim, err := h.embedders.forJob(job)
	if err != nil {
		if errors.Is(err, errEndpointNotAllowed) {
			logger.Error("tmi-chunk-embed: job %s rejected: %v", job.JobID, err)
			return h.publishFailure(ctx, job, reasonEndpointNotAllowed, err.Error())
		}
		return err // embedder construction failure: transient, redeliver
	}

	text, err := h.conn.GetPayload(ctx, job.Input.ObjectRef)
	if err != nil {
		return err // transient -> redeliver
	}

	chunks := h.chunker.Chunk(string(text))
	vectors, err := embedChunks(ctx, embedder, chunks)
	if err != nil {
		// An embedding-API failure may be transient (rate limit, 5xx).
		// Returning the raw error naks for redelivery; JetStream MaxDeliver
		// bounds the retries.
		return err
	}

	checkDimension(job.JobID, wantDim, vectors)

	result := EmbeddingResult{Chunks: chunks, Vectors: vectors}
	if err := result.validate(); err != nil {
		// A count mismatch is a worker bug, not bad input — terminal.
		return h.publishFailure(ctx, job, extract.ReasonExtractionInternal, err.Error())
	}

	blob, err := jsonMarshal(result, "embedding-result")
	if err != nil {
		return h.publishFailure(ctx, job, extract.ReasonExtractionInternal, err.Error())
	}
	resultRef, err := h.conn.PutPayload(ctx, job.JobID+"/result", blob)
	if err != nil {
		return err // transient
	}

	res := jobenvelope.Result{
		JobID:  job.JobID,
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: resultRef},
	}
	b, err := jsonMarshal(res, "result")
	if err != nil {
		return err
	}
	if err := h.conn.Publish(ctx, worker.ResultSubject(job.JobID), b); err != nil {
		return err
	}
	logger.Debug("tmi-chunk-embed: job %s done, %d chunks embedded", job.JobID, len(chunks))
	return nil
}

// publishFailure publishes a failed result and returns a terminal JobError.
func (h *chunkEmbedHandler) publishFailure(ctx context.Context, job jobenvelope.Job,
	reasonCode, detail string) error {
	return h.conn.PublishFailureResult(ctx, job.JobID, reasonCode, detail)
}
