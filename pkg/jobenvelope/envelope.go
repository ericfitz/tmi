package jobenvelope

import "time"

// Status is the terminal state reported on a result envelope.
type Status string

const (
	// StatusCompleted means the pipeline produced output successfully.
	StatusCompleted Status = "completed"
	// StatusFailed means a stage produced a typed error.
	StatusFailed Status = "failed"
)

// Limits are the per-job processing bounds carried in the envelope. They
// mirror the in-code extractor caps so a worker can enforce the same wall
// even when the controller's cgroup cap differs.
type Limits struct {
	// MaxBytes caps the source payload size the worker will read.
	MaxBytes int64 `json:"max_bytes"`
	// WallClock is the per-job wall-clock budget for the parse/embed stage.
	WallClock time.Duration `json:"wall_clock"`
}

// Input carries job input. Exactly one mode is populated:
//   - content-ref: ObjectRef + ByteSize are set; the bytes are already in
//     the JetStream Object Store.
//   - source-locator: SourceURL (+ optional SourceSecretRef, FetchLimits)
//     is set. RESERVED — not exercised by issue #347.
type Input struct {
	// ObjectRef is the Object Store locator for content-ref mode.
	ObjectRef string `json:"object_ref,omitempty"`
	// ByteSize is the source payload size in bytes (content-ref mode).
	ByteSize int64 `json:"byte_size,omitempty"`
	// SourceURL is the fetch target for source-locator mode. RESERVED.
	SourceURL string `json:"source_url,omitempty"`
	// SourceSecretRef names a secret for authenticated fetch. RESERVED.
	SourceSecretRef string `json:"source_secret_ref,omitempty"`
	// FetchLimits bounds a source-locator fetch. RESERVED.
	FetchLimits *Limits `json:"fetch_limits,omitempty"`
}

// Output carries job output by reference.
type Output struct {
	// ResultRef is the Object Store locator for the stage output blob.
	ResultRef string `json:"result_ref,omitempty"`
}

// Job is the forward-path envelope a stage consumes.
type Job struct {
	// JobID is the idempotency key — stable across the whole pipeline.
	JobID string `json:"job_id"`
	// ContentType is the source MIME type, used to route to an extractor.
	ContentType string `json:"content_type"`
	// Limits are the per-job processing bounds.
	Limits Limits `json:"limits"`
	// Deadline is the absolute wall-clock deadline for the whole job.
	Deadline time.Time `json:"deadline"`
	// Input carries the stage input (by reference).
	Input Input `json:"input"`
	// Metadata carries small key/value pairs forwarded between stages
	// (e.g. document title). Never large payloads.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Result is the return-path envelope published to jobs.result.<job_id>.
type Result struct {
	// JobID matches the originating Job.
	JobID string `json:"job_id"`
	// Status is the terminal state.
	Status Status `json:"status"`
	// ReasonCode is the typed reason on failure (a pkg/extract Reason*
	// constant) or empty on success.
	ReasonCode string `json:"reason_code,omitempty"`
	// ReasonDetail is optional human-readable context (e.g. "slide #42").
	ReasonDetail string `json:"reason_detail,omitempty"`
	// Output carries the result blob reference on success.
	Output Output `json:"output,omitempty"`
}
