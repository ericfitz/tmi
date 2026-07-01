package jobenvelope

import (
	"encoding/json"
	"fmt"
	"time"
)

// Status is the terminal state reported on a result envelope.
// SEM@fd1c8398b240fc2d88814019a767c2cebb498d27: string type representing the terminal outcome of a job pipeline result envelope (pure)
type Status string

const (
	// StatusCompleted means the pipeline produced output successfully.
	StatusCompleted Status = "completed"
	// StatusFailed means a stage produced a typed error.
	StatusFailed Status = "failed"
)

// Duration is a time.Duration that marshals to and from a JSON string in
// Go duration syntax (e.g. "60s", "1m30s") rather than a raw nanosecond
// integer, so the job envelope stays human- and cross-language-readable.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: convert time.Duration to/from a human-readable JSON duration string (pure)
type Duration time.Duration

// MarshalJSON renders the duration as a quoted Go duration string.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: serialize a Duration as a quoted Go duration string (pure)
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// UnmarshalJSON parses a quoted Go duration string. It also accepts a bare
// JSON number, interpreted as nanoseconds, for forward compatibility.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: parse a Duration from a quoted Go duration string or raw nanosecond integer (pure)
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		parsed, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("jobenvelope: invalid duration %q: %w", s, err)
		}
		*d = Duration(parsed)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("jobenvelope: duration must be a string or number: %w", err)
	}
	*d = Duration(n)
	return nil
}

// Std returns the wrapped value as a standard time.Duration.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: convert the custom Duration to a standard time.Duration (pure)
func (d Duration) Std() time.Duration { return time.Duration(d) }

// Limits are the per-job processing bounds carried in the envelope. They
// mirror the in-code extractor caps so a worker can enforce the same wall
// even when the controller's cgroup cap differs.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: per-job processing bounds for payload size and wall-clock budget (pure)
type Limits struct {
	// MaxBytes caps the source payload size the worker will read.
	MaxBytes int64 `json:"max_bytes"`
	// WallClock is the per-job wall-clock budget for the parse/embed stage.
	// Serialized as a Go duration string (e.g. "60s").
	WallClock Duration `json:"wall_clock"`
}

// Input carries job input. Exactly one mode is populated:
//   - content-ref: ObjectRef + ByteSize are set; the bytes are already in
//     the JetStream Object Store.
//   - source-locator: SourceURL (+ optional SourceSecretRef, FetchLimits)
//     is set. RESERVED — not exercised by issue #347.
//
// SEM@fd1c8398b240fc2d88814019a767c2cebb498d27: job input descriptor referencing a payload by object-store ref or source URL (pure)
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
// SEM@fd1c8398b240fc2d88814019a767c2cebb498d27: job output descriptor referencing the result blob by object-store ref (pure)
type Output struct {
	// ResultRef is the Object Store locator for the stage output blob.
	ResultRef string `json:"result_ref,omitempty"`
}

// Job is the forward-path envelope a stage consumes.
// SEM@49694f3eccb0f470f5f0308ef283dce1a077a92f: forward-path pipeline envelope carrying job identity, content type, limits, and input ref (pure)
type Job struct {
	// JobID is the idempotency key — stable across the whole pipeline.
	JobID string `json:"job_id"`
	// ContentType is the source MIME type, used to route to an extractor.
	ContentType string `json:"content_type"`
	// Limits are the per-job processing bounds.
	Limits Limits `json:"limits"`
	// Deadline is the absolute wall-clock deadline for the whole job.
	// Nil means no deadline. Producers that enforce a deadline set a
	// non-nil, future timestamp.
	Deadline *time.Time `json:"deadline,omitempty"`
	// Input carries the stage input (by reference).
	Input Input `json:"input"`
	// Metadata carries small key/value pairs forwarded between stages
	// (e.g. document title). Never large payloads.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Result is the return-path envelope published to jobs.result.<job_id>.
// SEM@fd1c8398b240fc2d88814019a767c2cebb498d27: return-path pipeline envelope reporting terminal status, reason, and output ref (pure)
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
