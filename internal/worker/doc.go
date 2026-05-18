// Package worker is the shared runtime for TMI Component Platform worker
// binaries (tmi-extractor, tmi-chunk-embed). It owns the NATS JetStream
// connection bootstrap, the durable-consumer loop with at-least-once and
// idempotency handling, and the heartbeat publisher. It is framework-free:
// no Gin, no GORM, no internal/config — a worker binary that imports this
// package and pkg/extract / pkg/jobenvelope pulls in nothing else from the
// monolith.
package worker
