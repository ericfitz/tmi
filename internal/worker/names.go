// Package worker naming constants and helpers for NATS JetStream subjects,
// streams, and the Object Store bucket used by TMI Component Platform workers.
//
// These names mirror what the component-controller renders in
// internal/platform/controller/render_jetstream.go, which is the source of
// truth for per-component stream and consumer naming. Any change to the
// controller's sanitizeName, streamNameFor, or consumerNameFor functions MUST
// be reflected here, and vice versa.
package worker

import "strings"

// JobsStreamPrefix is the prefix the controller's streamNameFor prepends to
// the sanitized component name to produce the per-component JetStream stream
// name. Plan 1's render_jetstream.go is the source of truth for this value.
const JobsStreamPrefix = "TMI_"

// PayloadBucket is the JetStream Object Store bucket name used for
// payload-by-reference: large job payloads are stored here and the job
// envelope carries only the object key. Plan 2 creates this bucket at worker
// startup; the Plan 1 controller does not render it.
const PayloadBucket = "TMI_PAYLOADS"

// ResultStream is the dedicated JetStream stream that Plan 2 workers create
// for jobs.result.* subjects. It is not owned by any Plan 1 per-component
// stream; the monolith result-consumer (Plan 3) and the workers both bind it.
const ResultStream = "TMI_RESULTS"

// SubjectExtractPrefix is the NATS subject prefix for extraction job messages
// (e.g., "jobs.extract.<jobID>").
const SubjectExtractPrefix = "jobs.extract."

// SubjectChunkEmbedPrefix is the NATS subject prefix for chunk-and-embed job
// messages (e.g., "jobs.chunkembed.<jobID>").
const SubjectChunkEmbedPrefix = "jobs.chunkembed."

// SubjectResultPrefix is the NATS subject prefix for job result messages
// (e.g., "jobs.result.<jobID>").
const SubjectResultPrefix = "jobs.result."

// SubjectDLQ is the NATS subject for dead-letter-queue messages: jobs that
// have exhausted all delivery retries.
const SubjectDLQ = "jobs.dlq"

// SubjectHeartbeatPrefix is the NATS subject prefix for component heartbeat
// messages (e.g., "components.heartbeat.<componentName>").
const SubjectHeartbeatPrefix = "components.heartbeat."

// ResultSubject returns the NATS subject for the result of a specific job.
func ResultSubject(jobID string) string {
	return SubjectResultPrefix + jobID
}

// ChunkEmbedSubject returns the NATS subject for a chunk-and-embed job with
// the given job ID.
func ChunkEmbedSubject(jobID string) string {
	return SubjectChunkEmbedPrefix + jobID
}

// HeartbeatSubject returns the NATS subject for a component's heartbeat
// messages.
func HeartbeatSubject(component string) string {
	return SubjectHeartbeatPrefix + component
}

// SanitizeName upcases s and replaces JetStream-illegal characters (".", "-",
// " ") with "_". It mirrors the unexported sanitizeName function in
// internal/platform/controller/render_jetstream.go; the two implementations
// MUST stay in sync so that a worker can derive the same stream and consumer
// names as the controller.
func SanitizeName(s string) string {
	up := strings.ToUpper(s)
	return strings.NewReplacer(".", "_", "-", "_", " ", "_").Replace(up)
}

// StreamNameFor returns the JetStream stream name for a component, mirroring
// the controller's streamNameFor function in render_jetstream.go.
// Example: "tmi-extractor" → "TMI_TMI_EXTRACTOR".
func StreamNameFor(componentName string) string {
	return JobsStreamPrefix + SanitizeName(componentName)
}

// ConsumerNameFor returns the durable JetStream consumer name for a component,
// mirroring the controller's consumerNameFor function in render_jetstream.go.
// Example: "tmi-chunk-embed" → "TMI_CHUNK_EMBED_CONSUMER".
func ConsumerNameFor(componentName string) string {
	return SanitizeName(componentName) + "_CONSUMER"
}
