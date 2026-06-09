package worker

// Naming constants and helpers for NATS JetStream subjects, streams, the
// payload Object Store bucket, and durable consumers. These mirror what the
// component-controller renders — internal/platform/controller/render_jetstream.go
// is the source of truth; SanitizeName/StreamNameFor/ConsumerNameFor here and
// sanitizeName/streamNameFor/consumerNameFor there MUST stay in sync.

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

// DLQStream is the dead-letter JetStream stream bound to SubjectDLQ
// ("jobs.dlq"). The monolith creates it, publishes dead-lettered Job
// envelopes to it, and consumes from it (see api/dlq_producer.go and the
// ResultConsumer DLQ subscription). It is not owned by any per-component
// stream.
const DLQStream = "TMI_DLQ"

// DLQAdvisoryStream is the JetStream stream that durably captures
// MAX_DELIVERIES consumer advisories so the monolith's DLQ producer survives
// restarts (a plain core-NATS subscription would miss advisories fired while
// the monolith is down).
const DLQAdvisoryStream = "TMI_DLQ_ADVISORY"

const (
	// SubjectExtractPrefix is the NATS subject prefix for extraction job messages
	// (e.g., "jobs.extract.<jobID>").
	SubjectExtractPrefix = "jobs.extract."

	// SubjectChunkEmbedPrefix is the NATS subject prefix for chunk-and-embed job
	// messages (e.g., "jobs.chunkembed.<jobID>").
	SubjectChunkEmbedPrefix = "jobs.chunkembed."

	// SubjectResultPrefix is the NATS subject prefix for job result messages
	// (e.g., "jobs.result.<jobID>").
	SubjectResultPrefix = "jobs.result."

	// SubjectDLQ is the NATS subject for dead-letter-queue messages: jobs that
	// have exhausted all delivery retries.
	SubjectDLQ = "jobs.dlq"

	// SubjectMaxDeliverAdvisory is the wildcard subject on which JetStream
	// publishes a MAX_DELIVERIES advisory when a message exhausts a consumer's
	// MaxDeliver. The concrete subject is
	// "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.<stream>.<consumer>".
	SubjectMaxDeliverAdvisory = "$JS.EVENT.ADVISORY.CONSUMER.MAX_DELIVERIES.>"

	// SubjectHeartbeatPrefix is the NATS subject prefix for component heartbeat
	// messages (e.g., "components.heartbeat.<componentName>").
	SubjectHeartbeatPrefix = "components.heartbeat."
)

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
// messages. The component argument is passed through UNSANITIZED: NATS
// subjects use "." as a hierarchy delimiter (not an illegal character), so
// sanitizing would corrupt the subject hierarchy.
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
