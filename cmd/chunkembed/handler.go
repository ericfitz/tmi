package main

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/tmc/langchaingo/embeddings"
)

// EmbeddingResult is the chunk-embed stage output blob, written to the
// Object Store and referenced from the result envelope.
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

// chunkEmbedHandler is the JobHandler for tmi-chunk-embed.
type chunkEmbedHandler struct {
	conn     *worker.Conn
	chunker  *extract.TextChunker
	embedder embeddings.Embedder
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
func newChunkEmbedHandler(conn *worker.Conn, emb embeddings.Embedder) *chunkEmbedHandler {
	return &chunkEmbedHandler{
		conn:     conn,
		chunker:  extract.NewTextChunker(chunkMaxChars, chunkOverlap),
		embedder: emb,
	}
}

// Handle reads the extracted-text blob, chunks + embeds it, writes the
// result blob, and publishes the final result envelope.
func (h *chunkEmbedHandler) Handle(ctx context.Context, job jobenvelope.Job) error {
	logger := slogging.Get()

	text, err := h.conn.GetPayload(ctx, job.Input.ObjectRef)
	if err != nil {
		return err // transient -> redeliver
	}

	chunks := h.chunker.Chunk(string(text))
	vectors, err := embedChunks(ctx, h.embedder, chunks)
	if err != nil {
		// An embedding-API failure may be transient (rate limit, 5xx).
		// Returning the raw error naks for redelivery; JetStream MaxDeliver
		// bounds the retries.
		return err
	}

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
	res := jobenvelope.Result{
		JobID:        job.JobID,
		Status:       jobenvelope.StatusFailed,
		ReasonCode:   reasonCode,
		ReasonDetail: detail,
	}
	b, err := jsonMarshal(res, "result")
	if err != nil {
		return err
	}
	if err := h.conn.Publish(ctx, worker.ResultSubject(job.JobID), b); err != nil {
		return err
	}
	return &worker.JobError{ReasonCode: reasonCode, Detail: detail, Terminal: true}
}
