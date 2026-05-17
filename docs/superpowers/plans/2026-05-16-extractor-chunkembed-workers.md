# TMI Component Platform — Extractor & Chunk-Embed Workers (Plan 2 of 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the two `TMIComponent` worker binaries — `tmi-extractor` (sandboxed parse, `egress: none`) and `tmi-chunk-embed` (chunk + embed, `egress: allowlist`) — by relocating the existing extractor logic into a framework-free shared package, defining the canonical job-envelope contract, and wiring each worker as a NATS JetStream consumer with heartbeat and typed-error production.

**Architecture:** Two new framework-free Go packages — `pkg/extract` (the relocated extractor registry, OOXML/PDF/HTML/plaintext extractors, bounded-extractor and wall-clock-budget logic) and `pkg/jobenvelope` (the single job-envelope schema shared by Plan 2 workers and the Plan 3 monolith). Two worker binaries under `cmd/extractor/` and `cmd/chunkembed/`, each a long-lived process: a NATS JetStream durable-consumer loop, a heartbeat publisher, a JetStream Object Store reader/writer for payload-by-reference, and typed-error → result-envelope mapping. The monolith's `api` package re-imports `pkg/extract` so the existing in-process extraction path is unchanged (relocation, not rewrite). No monolith request-path change, no `extraction_jobs` table, no `202` response — those are Plan 3.

**Tech Stack:** Go 1.26.2, `github.com/nats-io/nats.go v1.36.0` (JetStream + Object Store), `github.com/tmc/langchaingo v0.1.14` (OpenAI-compatible embeddings), Chainguard distroless images, `CGO_ENABLED=0`. NATS runs as a GitHub Actions `services:` container for process-mode tests and as the Plan 1 `deployments/k8s/platform/nats.yml` StatefulSet for the worker-level kind e2e.

---

## Scope

**This plan (Plan 2) delivers:**
- `pkg/extract/` — the relocated extractor package: `ContentExtractor` interface, `ContentExtractorRegistry`, the OOXML common machinery (opener, bounded reader, wall-clock budget, markdown builder, `extractionLimitError`), the DOCX/PPTX/XLSX/PDF/HTML/plaintext extractors, `ExtractedContent`, the sentinel errors, the extraction reason-code constants, and `ClassifyExtractionError`. Framework-free: no Gin, no GORM, no `internal/config`.
- `pkg/jobenvelope/` — the single canonical job-envelope Go types (`Job`, `Input`, `Output`, `Result`), JSON (de)serialization, and the envelope-validation function. Shared by both workers now and the Plan 3 monolith later.
- `cmd/extractor/main.go` and supporting files — the `tmi-extractor` worker: NATS JetStream durable consumer on `jobs.extract.*`, Object Store payload read, parse via `pkg/extract`, Object Store extracted-text write, publish `jobs.chunkembed.<job_id>` on success or `jobs.result.<job_id>` on failure, heartbeat publisher.
- `cmd/chunkembed/main.go` and supporting files — the `tmi-chunk-embed` worker: NATS JetStream durable consumer on `jobs.chunkembed.*`, Object Store read, chunk (relocated `TextChunker`) + embed (langchaingo OpenAI-compatible embedder), Object Store result write, publish `jobs.result.<job_id>`, heartbeat publisher.
- `internal/worker/` — shared worker runtime helpers: NATS connection + JetStream context bootstrap from env vars, the durable-consumer loop with at-least-once + idempotency handling, the heartbeat publisher, graceful shutdown.
- The monolith re-wiring: `api/content_extractor*.go` and `api/content_pipeline.go` updated to consume `pkg/extract` instead of in-package types, so the existing in-process path is byte-for-byte equivalent.
- Two `TMIComponent` CRs: `deployments/k8s/platform/components/tmi-extractor.yml` and `tmi-chunk-embed.yml`.
- Two Chainguard distroless Dockerfiles + Makefile targets per worker (build / test / container).
- Process-mode unit + integration tests (workers against a NATS service container) and a worker-level kind e2e (publish a job by hand against the Plan 1 cluster, assert the result).

**This plan does NOT deliver (Plan 3):**
- The `extraction_jobs` DB table, `cmd/dbtool/` updates, the monolith result-consumer goroutine.
- The monolith request-path change (`api/document_sub_resource_handlers.go` returning `202 Accepted`, `api/access_poller.go` becoming a pure submitter).
- OpenAPI spec changes (`api-schema/tmi-openapi.json`), `make validate-openapi`, `make generate-api`.
- The full five-criteria acceptance-criteria e2e (crash isolation, egress denial vs. a real CNI, cgroup OOM, dead-letter). Plan 2's e2e exercises the workers in isolation only; the end-to-end acceptance criteria need the Plan 3 monolith path.
- The flag-gated cutover from the in-process extractor and deletion of the in-process dispatch.
- The shared-config / embedding-profile mechanism (the config-system issue, #415). Plan 2's `tmi-chunk-embed` reads its embedding config from plain env vars injected by the CR `spec.config` + `secretRefs`; Plan 3 / #415 replaces that with the projected shared-config object.

**Decisions made here (not in the spec):**
- **Relocation target is `pkg/extract`** (a public-path package, framework-free) rather than `internal/extract` or leaving the code in `api`. Both the `api` package and `cmd/extractor` import it. Chosen so the worker binary does not drag in Gin/GORM/DB. (User decision, 2026-05-16.)
- **The job-envelope Go types live in their own package `pkg/jobenvelope` and are defined in Plan 2.** Plan 3 reuses them. Chosen to avoid Plan 3 redefining a contract Plan 2 already depends on. (User decision, 2026-05-16.)
- **Plan 2's e2e tier is worker-level only** — publish a job by hand, assert a result envelope. The full monolith→worker acceptance-criteria e2e is Plan 3. (User decision, 2026-05-16.)
- **`pkg/extract` keeps the extractors' currently-unexported helper identifiers package-private.** Only the genuine public surface is exported: `ContentExtractor`, `ContextAwareExtractor`, `BoundedExtractor`, `ContentExtractorRegistry`, `ExtractedContent`, `EntityReference`, the sentinel errors (`ErrExtractionLimit`, `ErrMalformed`, `ErrUnsupported`), the `Reason*` extraction constants, `Classification`, `ClassifyError`, `Limits`, `DefaultLimits`, the per-extractor constructors, and `ExtractWithDeadline`. Everything else (`ooxmlOpener`, `boundedReader`, `markdownBuilder`, `extractionLimitError`, `ctxReader`) stays lowercase inside the package — they all move together so no cross-package access is needed.
- **NATS subjects and naming follow Plan 1's `internal/platform/controller/render_jetstream.go` conventions.** Plan 2 reuses those subject strings; if Plan 1 named the stream/consumer differently, Task 1 reconciles the constants.

---

## File Structure

| Path | Responsibility |
|---|---|
| `pkg/extract/doc.go` | Package doc — what `pkg/extract` is and the framework-free invariant |
| `pkg/extract/types.go` | `ExtractedContent`, `EntityReference`, the `ContentExtractor` / `ContextAwareExtractor` / `BoundedExtractor` interfaces |
| `pkg/extract/registry.go` | `ContentExtractorRegistry` (relocated from `api/content_extractor.go`) |
| `pkg/extract/errors.go` | Sentinel errors, `extractionLimitError`, `Reason*` constants, `Classification`, `ClassifyError` |
| `pkg/extract/limits.go` | `Limits`, `DefaultLimits` (relocated `ooxmlLimits` made exported as `Limits`) |
| `pkg/extract/ooxml_common.go` | OOXML opener, bounded reader, markdown builder, `ExtractWithDeadline`, `ctxReader` (relocated from `api/content_extractor_ooxml_common.go`) |
| `pkg/extract/docx.go` | DOCX extractor (relocated from `api/content_extractor_docx.go`) |
| `pkg/extract/pptx.go` | PPTX extractor (relocated from `api/content_extractor_pptx.go`) |
| `pkg/extract/xlsx.go` | XLSX extractor (relocated from `api/content_extractor_xlsx.go`) |
| `pkg/extract/pdf.go` | PDF extractor (relocated from `api/content_extractor_pdf.go`) |
| `pkg/extract/html.go` | HTML extractor + `extractTextFromHTML` (relocated; the monolith keeps its own copy used by `timmy_content_provider_http.go`) |
| `pkg/extract/plaintext.go` | Plain-text extractor (relocated from `api/content_extractor_plaintext.go`) |
| `pkg/extract/chunker.go` | `TextChunker` (relocated from `api/timmy_chunker.go`) |
| `pkg/extract/*_test.go` | The relocated extractor + chunker unit tests, package renamed to `extract` |
| `pkg/jobenvelope/doc.go` | Package doc — the single-envelope contract |
| `pkg/jobenvelope/envelope.go` | `Job`, `Input`, `Output`, `Result`, `Status`, JSON tags |
| `pkg/jobenvelope/validate.go` | `Validate(job) error` — envelope-against-contract checks |
| `pkg/jobenvelope/envelope_test.go` | Round-trip JSON + validation tests |
| `internal/worker/doc.go` | Package doc — shared worker runtime |
| `internal/worker/nats.go` | NATS connect + JetStream context + Object Store handle from env vars |
| `internal/worker/consumer.go` | Durable-consumer loop, at-least-once handling, idempotency-by-`job_id` |
| `internal/worker/heartbeat.go` | Periodic heartbeat publisher on `components.heartbeat.<name>` |
| `internal/worker/env.go` | Typed env-var reader (`MustEnv`, `EnvOr`, `EnvDuration`) |
| `internal/worker/*_test.go` | Worker-runtime unit + integration tests |
| `cmd/extractor/main.go` | `tmi-extractor` entrypoint — wires `internal/worker` + `pkg/extract` |
| `cmd/extractor/handler.go` | `tmi-extractor` per-job handler |
| `cmd/extractor/handler_test.go` | `tmi-extractor` handler tests |
| `cmd/chunkembed/main.go` | `tmi-chunk-embed` entrypoint |
| `cmd/chunkembed/handler.go` | `tmi-chunk-embed` per-job handler — chunk + embed |
| `cmd/chunkembed/embedder.go` | langchaingo OpenAI-compatible embedder construction from env |
| `cmd/chunkembed/handler_test.go` | `tmi-chunk-embed` handler tests |
| `deployments/docker/extractor.Dockerfile` | Chainguard distroless image for `tmi-extractor` |
| `deployments/docker/chunkembed.Dockerfile` | Chainguard distroless image for `tmi-chunk-embed` |
| `deployments/k8s/platform/components/tmi-extractor.yml` | `TMIComponent` CR for the extractor |
| `deployments/k8s/platform/components/tmi-chunk-embed.yml` | `TMIComponent` CR for chunk-embed |
| `test/e2e/platform/workers_e2e_test.go` | Worker-level kind e2e |
| `Makefile` | New targets (see Task 17) |

---

## Task 1: Reconcile NATS subject and Object Store naming with Plan 1

**Files:**
- Read: `internal/platform/controller/render_jetstream.go`
- Create: `internal/worker/doc.go`
- Create: `internal/worker/names.go`

- [ ] **Step 1: Read Plan 1's JetStream naming**

Run: `cat internal/platform/controller/render_jetstream.go`
Note the exact stream name, consumer-name pattern, and subject strings the controller renders. The plan below assumes:
- Stream `TMI_JOBS`, subjects `jobs.>`
- Extract subjects `jobs.extract.<type>` where `<type>` ∈ `ooxml | pdf | html | plaintext`
- Chunk-embed subject `jobs.chunkembed.<job_id>`
- Result subject `jobs.result.<job_id>`
- Object Store bucket `TMI_PAYLOADS`
- Heartbeat subject `components.heartbeat.<component-name>`
- Dead-letter subject `jobs.dlq`

If `render_jetstream.go` uses different names, use **its** names everywhere below and adjust the constants in this task to match.

- [ ] **Step 2: Write the package doc**

Create `internal/worker/doc.go`:

```go
// Package worker is the shared runtime for TMI Component Platform worker
// binaries (tmi-extractor, tmi-chunk-embed). It owns the NATS JetStream
// connection bootstrap, the durable-consumer loop with at-least-once and
// idempotency handling, and the heartbeat publisher. It is framework-free:
// no Gin, no GORM, no internal/config — a worker binary that imports this
// package and pkg/extract / pkg/jobenvelope pulls in nothing else from the
// monolith.
package worker
```

- [ ] **Step 3: Write the naming constants**

Create `internal/worker/names.go` (adjust values to match Step 1 if Plan 1 differs):

```go
package worker

// JetStream stream and Object Store bucket names. These MUST match the
// names the component-controller renders in
// internal/platform/controller/render_jetstream.go — that file is the
// source of truth; this is a consumer-side mirror.
const (
	// JobsStream is the JetStream stream carrying every job subject.
	JobsStream = "TMI_JOBS"
	// PayloadBucket is the JetStream Object Store bucket for payload-by-reference.
	PayloadBucket = "TMI_PAYLOADS"
)

// Subject prefixes and patterns.
const (
	// SubjectExtractPrefix is prepended to the content-type token, e.g.
	// "jobs.extract.ooxml".
	SubjectExtractPrefix = "jobs.extract."
	// SubjectChunkEmbedPrefix is prepended to the job_id.
	SubjectChunkEmbedPrefix = "jobs.chunkembed."
	// SubjectResultPrefix is prepended to the job_id.
	SubjectResultPrefix = "jobs.result."
	// SubjectDLQ is the dead-letter subject the monolith treats as
	// extraction_failed.
	SubjectDLQ = "jobs.dlq"
	// SubjectHeartbeatPrefix is prepended to the component name.
	SubjectHeartbeatPrefix = "components.heartbeat."
)

// ResultSubject returns the result subject for a job_id.
func ResultSubject(jobID string) string { return SubjectResultPrefix + jobID }

// ChunkEmbedSubject returns the chunkembed subject for a job_id.
func ChunkEmbedSubject(jobID string) string { return SubjectChunkEmbedPrefix + jobID }

// HeartbeatSubject returns the heartbeat subject for a component name.
func HeartbeatSubject(component string) string { return SubjectHeartbeatPrefix + component }
```

- [ ] **Step 4: Build the package**

Run: `go build ./internal/worker/`
Expected: builds clean (no test yet — pure constants).

- [ ] **Step 5: Commit**

```bash
git add internal/worker/doc.go internal/worker/names.go
git commit -m "feat(platform): add worker-runtime naming constants for NATS subjects"
```

---

## Task 2: Job-envelope schema package

**Files:**
- Create: `pkg/jobenvelope/doc.go`
- Create: `pkg/jobenvelope/envelope.go`
- Test: `pkg/jobenvelope/envelope_test.go`

- [ ] **Step 1: Write the package doc**

Create `pkg/jobenvelope/doc.go`:

```go
// Package jobenvelope defines the single job-envelope schema used by every
// stage of the TMI Component Platform extraction pipeline. One shape — no
// discriminator — flows from the monolith through tmi-extractor and
// tmi-chunk-embed back to the monolith's result-consumer. Input mode is
// declared per-component (content-ref vs source-locator); the monolith
// populates the matching Input fields. source-locator fields are RESERVED
// for the future code extractor and are not exercised by issue #347.
package jobenvelope
```

- [ ] **Step 2: Write the failing round-trip test**

Create `pkg/jobenvelope/envelope_test.go`:

```go
package jobenvelope

import (
	"encoding/json"
	"testing"
	"time"
)

func TestJobRoundTrip(t *testing.T) {
	in := Job{
		JobID:       "job-abc-123",
		ContentType: "application/pdf",
		Limits:      Limits{MaxBytes: 50 << 20, WallClock: 60 * time.Second},
		Deadline:    time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
		Input:       Input{ObjectRef: "TMI_PAYLOADS/job-abc-123/source", ByteSize: 1234},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Job
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.JobID != in.JobID || out.ContentType != in.ContentType {
		t.Fatalf("round-trip mismatch: got %+v want %+v", out, in)
	}
	if out.Input.ObjectRef != in.Input.ObjectRef || out.Limits.MaxBytes != in.Limits.MaxBytes {
		t.Fatalf("nested round-trip mismatch: got %+v", out)
	}
}

func TestResultRoundTrip(t *testing.T) {
	in := Result{
		JobID:      "job-abc-123",
		Status:     StatusFailed,
		ReasonCode: "extraction_malformed",
		Output:     Output{ResultRef: ""},
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Status != StatusFailed || out.ReasonCode != "extraction_malformed" {
		t.Fatalf("result round-trip mismatch: got %+v", out)
	}
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./pkg/jobenvelope/`
Expected: FAIL — `undefined: Job`, `undefined: Result`.

- [ ] **Step 4: Write the envelope types**

Create `pkg/jobenvelope/envelope.go`:

```go
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
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./pkg/jobenvelope/`
Expected: PASS — both round-trip tests.

- [ ] **Step 6: Commit**

```bash
git add pkg/jobenvelope/doc.go pkg/jobenvelope/envelope.go pkg/jobenvelope/envelope_test.go
git commit -m "feat(platform): add canonical job-envelope schema package"
```

---

## Task 3: Job-envelope validation

**Files:**
- Create: `pkg/jobenvelope/validate.go`
- Modify: `pkg/jobenvelope/envelope_test.go`

- [ ] **Step 1: Add the failing validation tests**

Append to `pkg/jobenvelope/envelope_test.go`:

```go
func TestValidateContentRefOK(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "application/pdf",
		Input: Input{ObjectRef: "b/k", ByteSize: 10}}
	if err := Validate(j); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateRejectsMissingJobID(t *testing.T) {
	j := Job{ContentType: "text/plain", Input: Input{ObjectRef: "b/k"}}
	if err := Validate(j); err == nil {
		t.Fatal("expected error for missing job_id")
	}
}

func TestValidateRejectsNoInput(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "text/plain"}
	if err := Validate(j); err == nil {
		t.Fatal("expected error for empty input")
	}
}

func TestValidateRejectsBothInputModes(t *testing.T) {
	j := Job{JobID: "j1", ContentType: "text/plain",
		Input: Input{ObjectRef: "b/k", SourceURL: "https://x"}}
	if err := Validate(j); err == nil {
		t.Fatal("expected error: content-ref and source-locator both set")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./pkg/jobenvelope/ -run TestValidate`
Expected: FAIL — `undefined: Validate`.

- [ ] **Step 3: Write the validator**

Create `pkg/jobenvelope/validate.go`:

```go
package jobenvelope

import "fmt"

// Validate checks a Job against the envelope contract. It rejects a missing
// job_id, a missing content_type, an empty Input, and an Input that sets
// both the content-ref and source-locator modes at once. It does NOT
// reject source-locator alone — that mode is RESERVED but schema-valid.
func Validate(j Job) error {
	if j.JobID == "" {
		return fmt.Errorf("jobenvelope: job_id is required")
	}
	if j.ContentType == "" {
		return fmt.Errorf("jobenvelope: content_type is required")
	}
	hasContentRef := j.Input.ObjectRef != ""
	hasSourceLocator := j.Input.SourceURL != ""
	switch {
	case hasContentRef && hasSourceLocator:
		return fmt.Errorf("jobenvelope: input sets both content-ref and source-locator")
	case !hasContentRef && !hasSourceLocator:
		return fmt.Errorf("jobenvelope: input has neither object_ref nor source_url")
	}
	return nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./pkg/jobenvelope/`
Expected: PASS — all envelope + validate tests.

- [ ] **Step 5: Commit**

```bash
git add pkg/jobenvelope/validate.go pkg/jobenvelope/envelope_test.go
git commit -m "feat(platform): add job-envelope contract validation"
```

---

## Task 4: Relocate the extractor package skeleton — types, errors, limits

**Files:**
- Create: `pkg/extract/doc.go`
- Create: `pkg/extract/types.go`
- Create: `pkg/extract/errors.go`
- Create: `pkg/extract/limits.go`
- Read: `api/timmy_content_provider.go:23-29`, `api/content_extractor.go`, `api/access_diagnostics.go:24-39`, `api/content_pipeline.go:239-292`, `internal/config/content_extractors.go`

This task moves the *non-extractor* foundation into `pkg/extract`. The extractor files (DOCX/PPTX/XLSX/PDF/HTML/plaintext, OOXML common) move in Tasks 5–6.

- [ ] **Step 1: Write the package doc**

Create `pkg/extract/doc.go`:

```go
// Package extract is the framework-free document/content extraction
// library: the extractor registry, the OOXML/PDF/HTML/plaintext extractors,
// the bounded-extractor and wall-clock-budget machinery, and the typed
// error classification. It imports nothing from the monolith — no Gin, no
// GORM, no internal/config — so it can be linked into the sandboxed
// tmi-extractor worker and into the monolith's api package alike.
//
// This package was relocated from package api during TMI Component Platform
// Plan 2 (issue #347). The move is a relocation, not a rewrite: the
// extraction logic is byte-for-byte the same; only the package boundary,
// the import paths, and the exported-identifier surface changed.
package extract
```

- [ ] **Step 2: Write the public types**

Create `pkg/extract/types.go`. `ExtractedContent` and `EntityReference` are relocated verbatim from `api/timmy_content_provider.go`; the three interfaces from `api/content_extractor.go`:

```go
package extract

import "context"

// ExtractedContent holds the text extracted from a source entity.
type ExtractedContent struct {
	Text        string            // Extracted plain text
	Title       string            // Document title if available
	ContentType string            // Original content type (e.g. "application/pdf")
	Metadata    map[string]string // Provider-specific metadata
}

// EntityReference identifies a source entity for content extraction.
type EntityReference struct {
	EntityType string // "asset", "threat", "document", "note", "diagram", "repository"
	EntityID   string // UUID of the source entity
	URI        string // External URL (empty for DB-resident content)
	Name       string // Display name for progress reporting
}

// ContentExtractor converts raw bytes into plain text.
type ContentExtractor interface {
	Name() string
	CanHandle(contentType string) bool
	Extract(data []byte, contentType string) (ExtractedContent, error)
}

// ContextAwareExtractor is implemented by extractors that accept a
// deadline-bearing context for cooperative cancellation. See the original
// doc comment in api/content_extractor.go for the full semantics.
type ContextAwareExtractor interface {
	ExtractCtx(ctx context.Context, data []byte, contentType string) (ExtractedContent, error)
}

// BoundedExtractor is implemented by extractors that must run under a
// wall-clock deadline. Bounded() is informational and always true for
// implementers.
type BoundedExtractor interface {
	Bounded() bool
}
```

- [ ] **Step 3: Write the errors + reason codes + classification**

Create `pkg/extract/errors.go`. The sentinel errors and `extractionLimitError` move from `api/content_extractor_ooxml_common.go:33-66`; the `Reason*` constants from `api/access_diagnostics.go:24-39`; `ClassifyExtractionError` from `api/content_pipeline.go:239-292`, renamed `ClassifyError`, returning the relocated `Classification`:

```go
package extract

import (
	"context"
	"errors"
	"fmt"
)

// Sentinel errors returned by extractors. Callers use errors.Is to classify.
var (
	ErrExtractionLimit = errors.New("extraction limit exceeded")
	ErrMalformed       = errors.New("malformed document")
	ErrUnsupported     = errors.New("unsupported document subformat")
)

// Extraction access-reason-code constants. Relocated verbatim from
// api/access_diagnostics.go so the monolith and the worker agree on the
// strings written to access_reason_code / the result envelope.
const (
	ReasonExtractionLimitCompressedSize   = "extraction_limit:compressed_size"
	ReasonExtractionLimitDecompressedSize = "extraction_limit:decompressed_size"
	ReasonExtractionLimitPartSize         = "extraction_limit:part_size"
	ReasonExtractionLimitPartCount        = "extraction_limit:part_count"
	ReasonExtractionLimitMarkdownSize     = "extraction_limit:markdown_size"
	ReasonExtractionLimitTimeout          = "extraction_limit:timeout"
	ReasonExtractionLimitXMLDepth         = "extraction_limit:xml_depth"
	ReasonExtractionLimitZipNested        = "extraction_limit:zip_nested"
	ReasonExtractionLimitZipPath          = "extraction_limit:zip_path"
	ReasonExtractionLimitCompressionRatio = "extraction_limit:compression_ratio"
	ReasonExtractionMalformed             = "extraction_malformed"
	ReasonExtractionUnsupported           = "extraction_unsupported"
	ReasonExtractionInternal              = "extraction_internal"
)

// extractionLimitError describes which limit tripped during extraction.
// Kept package-private — callers use errors.Is(err, ErrExtractionLimit)
// and ClassifyError to consume it.
type extractionLimitError struct {
	Kind     string
	Limit    int64
	Observed int64  // -1 if not measurable (e.g. timeout)
	Detail   string // optional context: "slide #42", "sheet 'Sales'"
}

func (e *extractionLimitError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("extraction limit exceeded: kind=%s limit=%d observed=%d detail=%q",
			e.Kind, e.Limit, e.Observed, e.Detail)
	}
	return fmt.Sprintf("extraction limit exceeded: kind=%s limit=%d observed=%d",
		e.Kind, e.Limit, e.Observed)
}

func (e *extractionLimitError) Is(target error) bool { return target == ErrExtractionLimit }
func (e *extractionLimitError) Unwrap() error        { return ErrExtractionLimit }

// Classification describes how a typed extractor error maps to a reason
// code, plus an optional human-readable Detail. Relocated from
// api.ExtractionClassification; the Status field is dropped because
// access_status is a monolith concept — the worker reports only reason
// codes, and the monolith's result-consumer (Plan 3) derives access_status.
type Classification struct {
	ReasonCode   string
	ReasonDetail string
}

// ClassifyError walks the error chain and returns the matching reason code.
// Default is ReasonExtractionInternal. A nil error returns the zero value.
func ClassifyError(err error) Classification {
	if err == nil {
		return Classification{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Classification{ReasonCode: ReasonExtractionLimitTimeout}
	}
	var le *extractionLimitError
	if errors.As(err, &le) {
		var code string
		switch le.Kind {
		case "compressed_size":
			code = ReasonExtractionLimitCompressedSize
		case "decompressed_size":
			code = ReasonExtractionLimitDecompressedSize
		case "part_size":
			code = ReasonExtractionLimitPartSize
		case "part_count":
			code = ReasonExtractionLimitPartCount
		case "markdown_size":
			code = ReasonExtractionLimitMarkdownSize
		case "xml_depth":
			code = ReasonExtractionLimitXMLDepth
		case "zip_nested":
			code = ReasonExtractionLimitZipNested
		case "zip_path":
			code = ReasonExtractionLimitZipPath
		case "compression_ratio":
			code = ReasonExtractionLimitCompressionRatio
		}
		if code != "" {
			return Classification{ReasonCode: code, ReasonDetail: le.Detail}
		}
	}
	if errors.Is(err, ErrMalformed) {
		return Classification{ReasonCode: ReasonExtractionMalformed}
	}
	if errors.Is(err, ErrUnsupported) {
		return Classification{ReasonCode: ReasonExtractionUnsupported}
	}
	return Classification{ReasonCode: ReasonExtractionInternal}
}
```

- [ ] **Step 4: Write the limits**

Create `pkg/extract/limits.go`. This is the relocated `ooxmlLimits` from `api/content_extractor_ooxml_common.go:106-135`, exported as `Limits`, plus `DefaultLimits`:

```go
package extract

import "time"

// Limits is the set of extraction caps the OOXML opener, the XML decoder,
// and the wall-clock budget enforce. Relocated and exported from the
// package-private ooxmlLimits.
type Limits struct {
	CompressedSizeBytes   int64
	DecompressedSizeBytes int64
	PartSizeBytes         int64
	MarkdownSizeBytes     int64
	MaxXMLElementDepth    int
	MaxCompressionRatio   int64
	PPTXSlides            int
	XLSXCells             int
	// WallClockBudget is the per-extraction deadline (0 disables).
	WallClockBudget time.Duration
}

// DefaultLimits returns the design-spec default values.
func DefaultLimits() Limits {
	return Limits{
		CompressedSizeBytes:   20 * 1024 * 1024,
		DecompressedSizeBytes: 50 * 1024 * 1024,
		PartSizeBytes:         20 * 1024 * 1024,
		MarkdownSizeBytes:     128 * 1024,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
		PPTXSlides:            100,
		XLSXCells:             1000,
		WallClockBudget:       30 * time.Second,
	}
}
```

- [ ] **Step 5: Build the package**

Run: `go build ./pkg/extract/`
Expected: builds clean. (`registry.go` and the extractors come in Tasks 5–6; nothing here references them.)

- [ ] **Step 6: Commit**

```bash
git add pkg/extract/doc.go pkg/extract/types.go pkg/extract/errors.go pkg/extract/limits.go
git commit -m "feat(platform): add pkg/extract foundation (types, errors, limits)"
```

---

## Task 5: Relocate the OOXML common machinery and the registry

**Files:**
- Create: `pkg/extract/registry.go` (from `api/content_extractor.go`)
- Create: `pkg/extract/ooxml_common.go` (from `api/content_extractor_ooxml_common.go`)
- Create: `pkg/extract/ooxml_common_test.go` (from `api/content_extractor_ooxml_common_test.go`)

The OOXML common file is the hardest move — it defines `ooxmlOpener`, `ooxmlArchive`, `boundedReader`, `markdownBuilder`, `ctxReader`, `extractWithDeadline`, and the `ooxmlLimits` type that Task 4 already relocated as `Limits`.

- [ ] **Step 1: Copy the registry**

Copy `api/content_extractor.go` to `pkg/extract/registry.go`. Change the package clause to `package extract`. Remove the `ContentExtractor` / `ContextAwareExtractor` / `BoundedExtractor` interface definitions (now in `types.go`) — keep only `ContentExtractorRegistry`, `NewContentExtractorRegistry`, `Register`, `FindExtractor`. The file becomes:

```go
package extract

// ContentExtractorRegistry manages content extractors in priority order.
type ContentExtractorRegistry struct {
	extractors []ContentExtractor
}

// NewContentExtractorRegistry creates a new registry.
func NewContentExtractorRegistry() *ContentExtractorRegistry {
	return &ContentExtractorRegistry{}
}

// Register adds an extractor to the registry.
func (r *ContentExtractorRegistry) Register(extractor ContentExtractor) {
	r.extractors = append(r.extractors, extractor)
}

// FindExtractor returns the first extractor that can handle the content type.
func (r *ContentExtractorRegistry) FindExtractor(contentType string) (ContentExtractor, bool) {
	for _, e := range r.extractors {
		if e.CanHandle(contentType) {
			return e, true
		}
	}
	return nil, false
}
```

- [ ] **Step 2: Copy the OOXML common file**

Run:
```bash
cp api/content_extractor_ooxml_common.go pkg/extract/ooxml_common.go
```
Then edit `pkg/extract/ooxml_common.go`:
1. Change `package api` → `package extract`.
2. Delete the `extractionLimitError` type + its methods (lines ~44–66 in the original) — now in `errors.go`.
3. Delete the `ErrExtractionLimit` / `ErrMalformed` / `ErrUnsupported` `var` block — now in `errors.go`.
4. Delete the `ooxmlLimits` type and `defaultOOXMLLimits()` — replaced by `Limits` / `DefaultLimits` in `limits.go`.
5. Replace every remaining `ooxmlLimits` identifier with `Limits`.
6. Replace every `defaultOOXMLLimits()` call with `DefaultLimits()`.
7. Remove the `import "github.com/ericfitz/tmi/internal/config"` line if present and any `config.` references — `ooxml_common.go` should not import `internal/config` (the limits now come from `Limits`).
8. Rename `extractWithDeadline` → `ExtractWithDeadline` (exported — the worker calls it directly).

- [ ] **Step 3: Copy the OOXML common test**

Run:
```bash
cp api/content_extractor_ooxml_common_test.go pkg/extract/ooxml_common_test.go
```
Edit `pkg/extract/ooxml_common_test.go`: change `package api` → `package extract`, replace `ooxmlLimits` → `Limits`, `defaultOOXMLLimits` → `DefaultLimits`, `extractWithDeadline` → `ExtractWithDeadline`.

- [ ] **Step 4: Build and run the OOXML common test in its new home**

Run: `go test ./pkg/extract/ -run OOXML -v`
Expected: PASS for the OOXML-common tests. If the build fails on a missing identifier, the failure names the symbol — it is either still in another `api/` file not yet moved (defer that reference until Task 6) or a typo from the rename. Do NOT add a placeholder; resolve the actual symbol.

> Note: if a test references a DOCX/PPTX/XLSX extractor not yet moved, temporarily skip that single test with `t.Skip("moved in Task 6")` and remove the skip in Task 6 Step 4. Track each skip in the commit message.

- [ ] **Step 5: Commit**

```bash
git add pkg/extract/registry.go pkg/extract/ooxml_common.go pkg/extract/ooxml_common_test.go
git commit -m "feat(platform): relocate OOXML common machinery and registry to pkg/extract"
```

---

## Task 6: Relocate the six extractors and the chunker

**Files:**
- Create: `pkg/extract/docx.go`, `pkg/extract/pptx.go`, `pkg/extract/xlsx.go`, `pkg/extract/pdf.go`, `pkg/extract/html.go`, `pkg/extract/plaintext.go`, `pkg/extract/chunker.go`
- Create: the matching `*_test.go` for each
- From: `api/content_extractor_{docx,pptx,xlsx,pdf,html,plaintext}.go` + `_test.go`, `api/timmy_chunker.go` + `_test.go`

- [ ] **Step 1: Copy each extractor file**

Run:
```bash
cp api/content_extractor_docx.go       pkg/extract/docx.go
cp api/content_extractor_docx_test.go  pkg/extract/docx_test.go
cp api/content_extractor_pptx.go       pkg/extract/pptx.go
cp api/content_extractor_pptx_test.go  pkg/extract/pptx_test.go
cp api/content_extractor_xlsx.go       pkg/extract/xlsx.go
cp api/content_extractor_xlsx_test.go  pkg/extract/xlsx_test.go
cp api/content_extractor_pdf.go        pkg/extract/pdf.go
cp api/content_extractor_pdf_test.go   pkg/extract/pdf_test.go
cp api/content_extractor_html.go       pkg/extract/html.go
cp api/content_extractor_html_test.go  pkg/extract/html_test.go
cp api/content_extractor_plaintext.go  pkg/extract/plaintext.go
cp api/content_extractor_plaintext_test.go pkg/extract/plaintext_test.go
cp api/timmy_chunker.go                pkg/extract/chunker.go
cp api/timmy_chunker_test.go           pkg/extract/chunker_test.go
```

- [ ] **Step 2: Fix package clauses and identifier renames in every copied file**

In all 14 new files, change `package api` → `package extract`. Then apply these renames consistently:
- `ooxmlLimits` → `Limits`
- `defaultOOXMLLimits()` → `DefaultLimits()`
- `extractWithDeadline` → `ExtractWithDeadline`

- [ ] **Step 3: Resolve the HTML extractor's `extractTextFromHTML` dependency**

`pkg/extract/html.go` calls `extractTextFromHTML`, which currently lives in `api/timmy_content_provider_http.go`. The monolith's `timmy_content_provider_http.go` still needs its own copy. Add a self-contained copy of `extractTextFromHTML` to `pkg/extract/html.go` (relocate the function body verbatim from `api/timmy_content_provider_http.go:78-104`, keeping it lowercase/package-private):

```go
// extractTextFromHTML parses an HTML document and returns the concatenated
// visible text, skipping <script> and <style> content. Self-contained copy
// for pkg/extract; the monolith keeps its own copy in
// api/timmy_content_provider_http.go for the HTTP provider's use.
func extractTextFromHTML(htmlContent string) string {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}
	var sb strings.Builder
	var extractText func(*html.Node)
	extractText = func(n *html.Node) {
		if n.Type == html.TextNode {
			text := strings.TrimSpace(n.Data)
			if text != "" {
				sb.WriteString(text)
				sb.WriteString(" ")
			}
		}
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style") {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extractText(c)
		}
	}
	extractText(doc)
	return strings.TrimSpace(sb.String())
}
```

Add the imports `"strings"` and `"golang.org/x/net/html"` to `pkg/extract/html.go`.

- [ ] **Step 4: Remove the temporary skips from Task 5**

If Task 5 Step 4 added `t.Skip("moved in Task 6")` to any OOXML-common test, remove those skips now — the referenced extractors exist in `pkg/extract`.

- [ ] **Step 5: Build and test the whole package**

Run: `go build ./pkg/extract/`
Expected: builds clean.

Run: `go test ./pkg/extract/`
Expected: PASS — every relocated extractor + chunker test. Total test count should match the sum from the original `api/content_extractor_*_test.go` + `timmy_chunker_test.go` files (no test lost in the move).

> If a test fails on an identifier defined elsewhere in `api/` (a shared helper), copy that helper into `pkg/extract` as a package-private function. Do not import `api` from `pkg/extract` — that would re-create the dependency the relocation exists to break.

- [ ] **Step 6: Commit**

```bash
git add pkg/extract/
git commit -m "feat(platform): relocate document extractors and text chunker to pkg/extract"
```

---

## Task 7: Re-wire the monolith onto pkg/extract

**Files:**
- Delete: `api/content_extractor.go`, `api/content_extractor_docx.go`, `api/content_extractor_pptx.go`, `api/content_extractor_xlsx.go`, `api/content_extractor_pdf.go`, `api/content_extractor_html.go`, `api/content_extractor_plaintext.go`, `api/content_extractor_ooxml_common.go`, `api/timmy_chunker.go`, and all their `_test.go` siblings, `api/content_extractor_test.go`
- Modify: `api/content_pipeline.go`, `api/timmy_content_provider.go`, and every `api/` file that referenced a moved type
- Keep: `api/ooxml_extractors_corpus_test.go`, `api/ooxml_extractors_integration_test.go` — re-point these at `pkg/extract`

The monolith must keep working with byte-for-byte-equivalent extraction. The clean approach: a thin alias layer so the `api` package's many callers do not all need editing.

- [ ] **Step 1: Inventory the monolith's references to moved symbols**

Run:
```bash
rg -n 'ExtractedContent|ContentExtractor|NewContentExtractorRegistry|NewDOCXExtractor|NewPPTXExtractor|NewXLSXExtractor|NewPDFExtractor|NewHTMLExtractor|NewPlainTextExtractor|NewTextChunker|ClassifyExtractionError|ExtractionClassification|ErrMalformed|ErrUnsupported|ErrExtractionLimit|BoundedExtractor|ContextAwareExtractor|ooxmlLimits|defaultOOXMLLimits' api/ --type go -l | sort -u
```
This is the edit set for the rest of this task. Expect `content_pipeline.go`, `timmy_content_provider.go`, the embedding-source files, the corpus/integration tests, and the server-wiring file that builds the registry.

- [ ] **Step 2: Delete the moved files**

Run:
```bash
git rm api/content_extractor.go api/content_extractor_docx.go api/content_extractor_pptx.go \
  api/content_extractor_xlsx.go api/content_extractor_pdf.go api/content_extractor_html.go \
  api/content_extractor_plaintext.go api/content_extractor_ooxml_common.go api/timmy_chunker.go \
  api/content_extractor_docx_test.go api/content_extractor_pptx_test.go api/content_extractor_xlsx_test.go \
  api/content_extractor_pdf_test.go api/content_extractor_html_test.go api/content_extractor_plaintext_test.go \
  api/content_extractor_ooxml_common_test.go api/content_extractor_test.go api/timmy_chunker_test.go
```

- [ ] **Step 3: Add the alias layer**

Create `api/content_extract_aliases.go` so `api`-package call sites keep compiling without a global rename:

```go
package api

import "github.com/ericfitz/tmi/pkg/extract"

// Type aliases re-exporting pkg/extract into the api package. The extractor
// logic was relocated to pkg/extract during Component Platform Plan 2 (#347)
// so the sandboxed worker can link it without pulling in Gin/GORM. These
// aliases keep the monolith's many call sites unchanged.
type (
	ExtractedContent         = extract.ExtractedContent
	ContentExtractor         = extract.ContentExtractor
	ContextAwareExtractor    = extract.ContextAwareExtractor
	BoundedExtractor         = extract.BoundedExtractor
	ContentExtractorRegistry = extract.ContentExtractorRegistry
	ExtractionClassification = extract.Classification
)

// Sentinel-error re-exports.
var (
	ErrExtractionLimit = extract.ErrExtractionLimit
	ErrMalformed       = extract.ErrMalformed
	ErrUnsupported     = extract.ErrUnsupported
)

// Constructor and helper re-exports.
var (
	NewContentExtractorRegistry = extract.NewContentExtractorRegistry
	NewDOCXExtractor            = extract.NewDOCXExtractor
	NewPPTXExtractor            = extract.NewPPTXExtractor
	NewXLSXExtractor            = extract.NewXLSXExtractor
	NewPDFExtractor             = extract.NewPDFExtractor
	NewHTMLExtractor            = extract.NewHTMLExtractor
	NewPlainTextExtractor       = extract.NewPlainTextExtractor
	NewTextChunker              = extract.NewTextChunker
)
```

> `ExtractedContent` and `EntityReference` were defined in `api/timmy_content_provider.go`. Delete those two struct definitions there (lines 23–29 and 11–20) and let the alias provide `ExtractedContent`; add `type EntityReference = extract.EntityReference` to the alias file. Verify no `api/` code constructs `extract.EntityReference` with field names the alias does not preserve — the alias is identical, so it does.

- [ ] **Step 4: Fix the `ClassifyExtractionError` caller**

`ClassifyExtractionError` returned `ExtractionClassification{Status, ReasonCode, ReasonDetail}`; `extract.ClassifyError` returns `Classification{ReasonCode, ReasonDetail}` (no `Status`). Find the monolith caller:

```bash
rg -n 'ClassifyExtractionError' api/ --type go
```

Add a monolith-side wrapper to `api/content_pipeline.go` that re-adds `Status` (which the monolith owns):

```go
// ClassifyExtractionError wraps extract.ClassifyError and re-attaches the
// monolith-owned access_status. The worker (pkg/extract) reports only the
// reason code; access_status is derived here.
func ClassifyExtractionError(err error) struct {
	Status       string
	ReasonCode   string
	ReasonDetail string
} {
	c := extract.ClassifyError(err)
	out := struct {
		Status       string
		ReasonCode   string
		ReasonDetail string
	}{ReasonCode: c.ReasonCode, ReasonDetail: c.ReasonDetail}
	if c.ReasonCode != "" {
		out.Status = AccessStatusExtractionFailed
	}
	return out
}
```

> If existing callers used the named type `ExtractionClassification` as a variable type, keep `ExtractionClassification` as `extract.Classification` (the alias from Step 3) and instead have `ClassifyExtractionError` return that plus track `Status` separately — pick whichever keeps the *fewest* call sites edited. Inspect the actual caller from the `rg` output and choose; do not guess.

- [ ] **Step 5: Re-point the corpus and integration tests**

`api/ooxml_extractors_corpus_test.go` and `api/ooxml_extractors_integration_test.go` exercise the extractors. Update their references to use the `api`-package aliases (no import change needed — the aliases are in `package api`). If they reference `ooxmlLimits` / `defaultOOXMLLimits` directly, change to `extract.Limits` / `extract.DefaultLimits` and add the `pkg/extract` import.

- [ ] **Step 6: Build the monolith**

Run: `make build-server`
Expected: builds clean. Fix every reference the compiler flags — the `rg` output from Step 1 is the checklist.

- [ ] **Step 7: Run the monolith unit tests**

Run: `make test-unit`
Expected: PASS. The extraction behavior is unchanged; any failure is a wiring regression from the move, not a logic change. Investigate and fix the root cause — do not skip tests.

- [ ] **Step 8: Commit**

```bash
git add api/
git commit -m "refactor(api): consume pkg/extract via alias layer after extractor relocation"
```

---

## Task 8: Worker env-var reader

**Files:**
- Create: `internal/worker/env.go`
- Test: `internal/worker/env_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/worker/env_test.go`:

```go
package worker

import (
	"testing"
	"time"
)

func TestEnvOr(t *testing.T) {
	t.Setenv("TMI_TEST_KEY", "hello")
	if got := EnvOr("TMI_TEST_KEY", "fallback"); got != "hello" {
		t.Fatalf("EnvOr: got %q", got)
	}
	if got := EnvOr("TMI_TEST_MISSING", "fallback"); got != "fallback" {
		t.Fatalf("EnvOr fallback: got %q", got)
	}
}

func TestMustEnv(t *testing.T) {
	t.Setenv("TMI_TEST_REQUIRED", "v")
	if got, err := MustEnv("TMI_TEST_REQUIRED"); err != nil || got != "v" {
		t.Fatalf("MustEnv: got %q err %v", got, err)
	}
	if _, err := MustEnv("TMI_TEST_ABSENT"); err == nil {
		t.Fatal("MustEnv: expected error for absent key")
	}
}

func TestEnvDuration(t *testing.T) {
	t.Setenv("TMI_TEST_DUR", "45s")
	if got := EnvDuration("TMI_TEST_DUR", time.Minute); got != 45*time.Second {
		t.Fatalf("EnvDuration: got %v", got)
	}
	if got := EnvDuration("TMI_TEST_DUR_MISSING", time.Minute); got != time.Minute {
		t.Fatalf("EnvDuration fallback: got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/worker/ -run TestEnv -v`
Expected: FAIL — `undefined: EnvOr`.

- [ ] **Step 3: Write the env reader**

Create `internal/worker/env.go`:

```go
package worker

import (
	"fmt"
	"os"
	"time"
)

// MustEnv returns the value of key or an error if it is unset/empty.
func MustEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("worker: required env var %s is not set", key)
	}
	return v, nil
}

// EnvOr returns the value of key, or fallback if it is unset/empty.
func EnvOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// EnvDuration parses key as a Go duration, returning fallback if it is
// unset/empty or fails to parse.
func EnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/worker/ -run TestEnv`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/env.go internal/worker/env_test.go
git commit -m "feat(platform): add worker env-var reader"
```

---

## Task 9: NATS connection + JetStream + Object Store bootstrap

**Files:**
- Create: `internal/worker/nats.go`
- Test: `internal/worker/nats_test.go`

The worker connects to NATS using `nats.go`'s JetStream `jetstream` package. The Object Store is created if absent so process-mode tests do not need cluster setup.

- [ ] **Step 1: Write the integration test (NATS service container required)**

Create `internal/worker/nats_test.go`:

```go
package worker

import (
	"context"
	"os"
	"testing"
	"time"
)

// natsURL is the NATS endpoint for integration tests. CI sets TMI_TEST_NATS_URL
// to the service-container address; locally it defaults to localhost.
func natsURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TMI_TEST_NATS_URL"); v != "" {
		return v
	}
	return "nats://127.0.0.1:4222"
}

func TestConnect_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS server available")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := Connect(ctx, Config{NATSURL: natsURL(t), ComponentName: "test-worker"})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer conn.Close()

	// The payload bucket must be usable: put + get a blob.
	ref, err := conn.PutPayload(ctx, "test-job-1", []byte("hello"))
	if err != nil {
		t.Fatalf("PutPayload: %v", err)
	}
	got, err := conn.GetPayload(ctx, ref)
	if err != nil {
		t.Fatalf("GetPayload: %v", err)
	}
	if string(got) != "hello" {
		t.Fatalf("payload round-trip: got %q", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/worker/ -run TestConnect`
Expected: FAIL to compile — `undefined: Connect`, `undefined: Config`.

- [ ] **Step 3: Write the NATS bootstrap**

Create `internal/worker/nats.go`:

```go
package worker

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config is the worker's NATS bootstrap configuration, read from env vars.
type Config struct {
	// NATSURL is the NATS server URL (env TMI_NATS_URL).
	NATSURL string
	// ComponentName is this worker's TMIComponent name (env TMI_COMPONENT_NAME).
	ComponentName string
}

// ConfigFromEnv builds a Config from the standard worker env vars.
func ConfigFromEnv() (Config, error) {
	url, err := MustEnv("TMI_NATS_URL")
	if err != nil {
		return Config{}, err
	}
	name, err := MustEnv("TMI_COMPONENT_NAME")
	if err != nil {
		return Config{}, err
	}
	return Config{NATSURL: url, ComponentName: name}, nil
}

// Conn bundles a NATS connection, a JetStream context, and the payload
// Object Store handle. It is the worker's single handle to the bus.
type Conn struct {
	nc      *nats.Conn
	js      jetstream.JetStream
	objs    jetstream.ObjectStore
	cfg     Config
}

// Connect dials NATS, opens a JetStream context, and ensures the payload
// Object Store bucket exists.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	nc, err := nats.Connect(cfg.NATSURL,
		nats.Name("tmi-"+cfg.ComponentName),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		return nil, fmt.Errorf("worker: nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("worker: jetstream context: %w", err)
	}
	objs, err := js.CreateOrUpdateObjectStore(ctx, jetstream.ObjectStoreConfig{
		Bucket: PayloadBucket,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("worker: object store: %w", err)
	}
	return &Conn{nc: nc, js: js, objs: objs, cfg: cfg}, nil
}

// JetStream returns the JetStream context for consumer/publish wiring.
func (c *Conn) JetStream() jetstream.JetStream { return c.js }

// Config returns the connection's config.
func (c *Conn) Config() Config { return c.cfg }

// Close drains and closes the NATS connection.
func (c *Conn) Close() { c.nc.Close() }

// PutPayload writes bytes to the Object Store under a deterministic name
// derived from jobID, returning the object_ref to carry in an envelope.
func (c *Conn) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	if _, err := c.objs.PutBytes(ctx, name, data); err != nil {
		return "", fmt.Errorf("worker: put payload %s: %w", name, err)
	}
	return PayloadBucket + "/" + name, nil
}

// GetPayload reads a blob by the object_ref produced by PutPayload.
func (c *Conn) GetPayload(ctx context.Context, ref string) ([]byte, error) {
	name, ok := payloadName(ref)
	if !ok {
		return nil, fmt.Errorf("worker: malformed object_ref %q", ref)
	}
	data, err := c.objs.GetBytes(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("worker: get payload %s: %w", name, err)
	}
	return data, nil
}

// payloadName strips the "<bucket>/" prefix from an object_ref.
func payloadName(ref string) (string, bool) {
	prefix := PayloadBucket + "/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		return "", false
	}
	return ref[len(prefix):], true
}

// Publish marshals and publishes a message to a JetStream subject.
func (c *Conn) Publish(ctx context.Context, subject string, data []byte) error {
	if _, err := c.js.Publish(ctx, subject, data); err != nil {
		return fmt.Errorf("worker: publish %s: %w", subject, err)
	}
	return nil
}
```

- [ ] **Step 4: Build the package**

Run: `go build ./internal/worker/`
Expected: builds clean.

- [ ] **Step 5: Run the integration test against a local NATS**

Start a local NATS with JetStream (process-mode; not a make target — this is a one-off dev check):
```bash
docker run -d --rm --name tmi-nats-test -p 4222:4222 nats:2.10-alpine -js
TMI_RUN_NATS_TESTS=1 go test ./internal/worker/ -run TestConnect_Integration -v
docker stop tmi-nats-test
```
Expected: PASS — payload round-trips through the Object Store.

> The Makefile target that runs this in CI with a `services:` NATS container is added in Task 17.

- [ ] **Step 6: Commit**

```bash
git add internal/worker/nats.go internal/worker/nats_test.go
git commit -m "feat(platform): add worker NATS/JetStream/Object-Store bootstrap"
```

---

## Task 10: Heartbeat publisher

**Files:**
- Create: `internal/worker/heartbeat.go`
- Test: `internal/worker/heartbeat_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/worker/heartbeat_test.go`:

```go
package worker

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHeartbeatPayload(t *testing.T) {
	hb := Heartbeat{Component: "tmi-extractor", InstanceID: "pod-xyz"}
	b, err := json.Marshal(hb)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Heartbeat
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Component != "tmi-extractor" || out.InstanceID != "pod-xyz" {
		t.Fatalf("heartbeat round-trip mismatch: %+v", out)
	}
}

func TestHeartbeatInterval(t *testing.T) {
	// A zero interval falls back to the default.
	if got := heartbeatInterval(0); got != defaultHeartbeatInterval {
		t.Fatalf("interval fallback: got %v", got)
	}
	if got := heartbeatInterval(5 * time.Second); got != 5*time.Second {
		t.Fatalf("interval passthrough: got %v", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/worker/ -run TestHeartbeat`
Expected: FAIL — `undefined: Heartbeat`.

- [ ] **Step 3: Write the heartbeat publisher**

Create `internal/worker/heartbeat.go`:

```go
package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// defaultHeartbeatInterval is the publish cadence when none is configured.
const defaultHeartbeatInterval = 10 * time.Second

// Heartbeat is the liveness message a worker publishes on
// components.heartbeat.<component>. The monolith uses it to distinguish
// "type declared, no healthy instance" from "instances present".
type Heartbeat struct {
	// Component is the TMIComponent name.
	Component string `json:"component"`
	// InstanceID identifies the publishing pod/process.
	InstanceID string `json:"instance_id"`
	// SentAt is the publish timestamp.
	SentAt time.Time `json:"sent_at"`
}

func heartbeatInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return defaultHeartbeatInterval
	}
	return d
}

// RunHeartbeat publishes a Heartbeat on the component's heartbeat subject
// every interval until ctx is cancelled. It is meant to run in its own
// goroutine; a publish failure is logged and retried on the next tick.
func RunHeartbeat(ctx context.Context, conn *Conn, instanceID string, interval time.Duration) {
	logger := slogging.Get()
	subject := HeartbeatSubject(conn.Config().ComponentName)
	tick := time.NewTicker(heartbeatInterval(interval))
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			hb := Heartbeat{
				Component:  conn.Config().ComponentName,
				InstanceID: instanceID,
				SentAt:     time.Now().UTC(),
			}
			b, err := json.Marshal(hb)
			if err != nil {
				logger.Error("worker heartbeat: marshal failed: %v", err)
				continue
			}
			if err := conn.Publish(ctx, subject, b); err != nil {
				logger.Warn("worker heartbeat: publish failed: %v", err)
			}
		}
	}
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/worker/ -run TestHeartbeat`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/heartbeat.go internal/worker/heartbeat_test.go
git commit -m "feat(platform): add worker heartbeat publisher"
```

---

## Task 11: Durable-consumer loop with idempotency

**Files:**
- Create: `internal/worker/consumer.go`
- Test: `internal/worker/consumer_test.go`

The consumer loop is the shared engine: subscribe a durable JetStream consumer to a subject filter, dispatch each message to a `JobHandler`, ack/nak by the handler's outcome, and skip already-processed `job_id`s (at-least-once → exactly-once-effect).

- [ ] **Step 1: Write the failing unit test (handler dispatch, no NATS)**

Create `internal/worker/consumer_test.go`:

```go
package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

func TestHandlerOutcome(t *testing.T) {
	// A handler returning nil yields OutcomeAck.
	if got := outcomeFor(nil); got != OutcomeAck {
		t.Fatalf("nil error: got %v", got)
	}
	// A handler returning a JobError (typed, terminal) yields OutcomeTerm:
	// the job will never succeed on redelivery, so terminate it.
	je := &JobError{ReasonCode: jobenvelope.StatusFailed, Terminal: true}
	if got := outcomeFor(je); got != OutcomeTerm {
		t.Fatalf("terminal JobError: got %v", got)
	}
	// A non-terminal error (transient: NATS hiccup) yields OutcomeNak for
	// redelivery.
	if got := outcomeFor(errors.New("transient")); got != OutcomeNak {
		t.Fatalf("transient error: got %v", got)
	}
}

func TestJobErrorString(t *testing.T) {
	je := &JobError{ReasonCode: "extraction_malformed", Detail: "bad zip", Terminal: true}
	if je.Error() == "" {
		t.Fatal("JobError.Error() empty")
	}
}

var _ = context.Background // keep context imported for later edits
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/worker/ -run 'TestHandlerOutcome|TestJobError'`
Expected: FAIL — `undefined: outcomeFor`, `undefined: JobError`.

- [ ] **Step 3: Write the consumer engine**

Create `internal/worker/consumer.go`:

```go
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// JobError is a typed, terminal failure a handler returns when a job can
// never succeed (malformed input, unsupported format, timeout). The
// consumer terminates such a message rather than redelivering it.
type JobError struct {
	// ReasonCode is a pkg/extract Reason* constant (or a status string).
	ReasonCode string
	// Detail is optional human-readable context.
	Detail string
	// Terminal is true when redelivery cannot help.
	Terminal bool
}

func (e *JobError) Error() string {
	return fmt.Sprintf("job error: reason=%s detail=%q terminal=%v", e.ReasonCode, e.Detail, e.Terminal)
}

// outcome is the consumer's per-message decision.
type outcome int

const (
	// OutcomeAck marks the message processed successfully.
	OutcomeAck outcome = iota
	// OutcomeNak requests redelivery (transient failure).
	OutcomeNak
	// OutcomeTerm permanently drops the message (terminal failure).
	OutcomeTerm
)

func outcomeFor(err error) outcome {
	if err == nil {
		return OutcomeAck
	}
	var je *JobError
	if errors.As(err, &je) && je.Terminal {
		return OutcomeTerm
	}
	return OutcomeNak
}

// JobHandler processes one decoded job. Returning nil acks the message;
// returning a terminal *JobError terminates it; any other error naks it for
// redelivery. The handler is responsible for publishing the result envelope
// for terminal failures BEFORE returning the *JobError — the consumer only
// decides ack/nak/term, it does not publish results.
type JobHandler func(ctx context.Context, job jobenvelope.Job) error

// ConsumerConfig configures the durable consumer.
type ConsumerConfig struct {
	// Durable is the durable consumer name (stable across restarts).
	Durable string
	// FilterSubject is the subject filter (e.g. "jobs.extract.>").
	FilterSubject string
	// AckWait is the redelivery timeout — also the JetStream-side backstop
	// for a worker that dies mid-job.
	AckWait time.Duration
	// MaxDeliver caps redeliveries before JetStream dead-letters.
	MaxDeliver int
}

// idempotency tracks job_ids already completed this process lifetime so a
// redelivered message is acked without reprocessing. A worker restart loses
// the set; the result-blob-exists check in the handler is the durable guard.
type idempotency struct {
	seen map[string]struct{}
}

func newIdempotency() *idempotency { return &idempotency{seen: map[string]struct{}{}} }
func (i *idempotency) done(id string) bool {
	_, ok := i.seen[id]
	return ok
}
func (i *idempotency) mark(id string) { i.seen[id] = struct{}{} }

// RunConsumer creates the durable consumer and dispatches messages to the
// handler until ctx is cancelled. It blocks; run it on the main goroutine.
func RunConsumer(ctx context.Context, conn *Conn, cfg ConsumerConfig, handle JobHandler) error {
	logger := slogging.Get()
	cons, err := conn.JetStream().CreateOrUpdateConsumer(ctx, JobsStream, jetstream.ConsumerConfig{
		Durable:       cfg.Durable,
		FilterSubject: cfg.FilterSubject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       cfg.AckWait,
		MaxDeliver:    cfg.MaxDeliver,
	})
	if err != nil {
		return fmt.Errorf("worker: create consumer: %w", err)
	}
	idem := newIdempotency()

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		var job jobenvelope.Job
		if err := json.Unmarshal(msg.Data(), &job); err != nil {
			logger.Error("worker consumer: undecodable message on %s: %v", msg.Subject(), err)
			_ = msg.Term() // a message we cannot decode can never succeed
			return
		}
		if err := jobenvelope.Validate(job); err != nil {
			logger.Error("worker consumer: invalid envelope job=%s: %v", job.JobID, err)
			_ = msg.Term()
			return
		}
		if idem.done(job.JobID) {
			logger.Debug("worker consumer: job %s already processed, acking redelivery", job.JobID)
			_ = msg.Ack()
			return
		}
		err := handle(ctx, job)
		switch outcomeFor(err) {
		case OutcomeAck:
			idem.mark(job.JobID)
			_ = msg.Ack()
		case OutcomeTerm:
			idem.mark(job.JobID)
			logger.Warn("worker consumer: terminal failure job=%s: %v", job.JobID, err)
			_ = msg.Term()
		default:
			logger.Warn("worker consumer: transient failure job=%s, will redeliver: %v", job.JobID, err)
			_ = msg.Nak()
		}
	})
	if err != nil {
		return fmt.Errorf("worker: consume: %w", err)
	}
	defer cc.Stop()

	<-ctx.Done()
	logger.Info("worker consumer: shutting down")
	return nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/worker/ -run 'TestHandlerOutcome|TestJobError'`
Expected: PASS.

- [ ] **Step 5: Build the whole worker package**

Run: `go build ./internal/worker/`
Expected: builds clean.

- [ ] **Step 6: Commit**

```bash
git add internal/worker/consumer.go internal/worker/consumer_test.go
git commit -m "feat(platform): add worker durable-consumer loop with idempotency"
```

---

## Task 12: tmi-extractor handler

**Files:**
- Create: `cmd/extractor/handler.go`
- Test: `cmd/extractor/handler_test.go`

The handler ties `pkg/extract` to the envelope: read the source blob, route by content type, parse under the wall-clock budget, write the extracted-text blob, publish either `jobs.chunkembed.<job_id>` (success) or `jobs.result.<job_id>` (failure).

- [ ] **Step 1: Write the failing handler test**

Create `cmd/extractor/handler_test.go`:

```go
package main

import (
	"strings"
	"testing"

	"github.com/ericfitz/tmi/pkg/extract"
)

func TestSubjectTypeToken(t *testing.T) {
	cases := map[string]string{
		"application/pdf": "pdf",
		"text/html":       "html",
		"text/plain":      "plaintext",
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "ooxml",
		"application/octet-stream": "plaintext", // unknown defaults to plaintext
	}
	for ct, want := range cases {
		if got := subjectTypeToken(ct); got != want {
			t.Fatalf("subjectTypeToken(%q): got %q want %q", ct, got, want)
		}
	}
}

func TestExtractDispatch(t *testing.T) {
	reg := buildExtractorRegistry(extract.DefaultLimits())
	// Plain text passes through unchanged.
	ext, ok := reg.FindExtractor("text/plain")
	if !ok {
		t.Fatal("no extractor for text/plain")
	}
	out, err := ext.Extract([]byte("hello world"), "text/plain")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(out.Text, "hello world") {
		t.Fatalf("plaintext extract: got %q", out.Text)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/extractor/`
Expected: FAIL — `undefined: subjectTypeToken`, `undefined: buildExtractorRegistry`.

- [ ] **Step 3: Write the handler**

Create `cmd/extractor/handler.go`:

```go
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
type extractHandler struct {
	conn   *worker.Conn
	reg    *extract.ContentExtractorRegistry
	limits extract.Limits
}

// newExtractHandler builds the handler. limits carries the per-extraction
// caps and the wall-clock budget.
func newExtractHandler(conn *worker.Conn, limits extract.Limits) *extractHandler {
	return &extractHandler{conn: conn, reg: buildExtractorRegistry(limits), limits: limits}
}

// Handle reads the source blob, extracts text, and publishes the next-stage
// job or a failure result. A terminal extraction failure is published as a
// result envelope here and a terminal *worker.JobError is returned so the
// consumer terminates the message.
func (h *extractHandler) Handle(ctx context.Context, job jobenvelope.Job) error {
	logger := slogging.Get()

	data, err := h.conn.GetPayload(ctx, job.Input.ObjectRef)
	if err != nil {
		// A missing blob is transient if the put has not propagated, but
		// after AckWait redeliveries it dead-letters — treat as transient.
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
		return err // transient — redeliver
	}

	next := jobenvelope.Job{
		JobID:       job.JobID,
		ContentType: job.ContentType,
		Limits:      job.Limits,
		Deadline:    job.Deadline,
		Input:       jobenvelope.Input{ObjectRef: textRef, ByteSize: int64(len(out.Text))},
		Metadata:    mergeMetadata(job.Metadata, out),
	}
	b, err := marshalJob(next)
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
func (h *extractHandler) extract(ctx context.Context, ext extract.ContentExtractor,
	data []byte, job jobenvelope.Job) (extract.ExtractedContent, error) {
	budget := h.limits.WallClockBudget
	if job.Limits.WallClock > 0 {
		budget = job.Limits.WallClock
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
func (h *extractHandler) publishFailure(ctx context.Context, job jobenvelope.Job, extractErr error) error {
	c := extract.ClassifyError(extractErr)
	res := jobenvelope.Result{
		JobID:        job.JobID,
		Status:       jobenvelope.StatusFailed,
		ReasonCode:   c.ReasonCode,
		ReasonDetail: c.ReasonDetail,
	}
	b, err := marshalResult(res)
	if err != nil {
		return err
	}
	if err := h.conn.Publish(ctx, worker.ResultSubject(job.JobID), b); err != nil {
		return err // transient publish failure — redeliver and retry
	}
	return &worker.JobError{ReasonCode: c.ReasonCode, Detail: c.ReasonDetail, Terminal: true}
}

// mergeMetadata folds the extractor's title into the forwarded metadata.
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
```

- [ ] **Step 4: Add the marshal helpers**

These are small and shared by `cmd/extractor` and `cmd/chunkembed`; put one copy in each `main` package (they are not worth a shared package). Append to `cmd/extractor/handler.go`:

```go
// marshalJob / marshalResult are thin json.Marshal wrappers kept here so
// the handler reads cleanly; errors are wrapped for log context.
func marshalJob(j jobenvelope.Job) ([]byte, error) {
	return jsonMarshal(j, "job")
}
func marshalResult(r jobenvelope.Result) ([]byte, error) {
	return jsonMarshal(r, "result")
}
```

Create `cmd/extractor/json.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
)

// jsonMarshal wraps json.Marshal with a labelled error.
func jsonMarshal(v any, label string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return b, nil
}
```

- [ ] **Step 5: Run the handler test**

Run: `go test ./cmd/extractor/`
Expected: PASS — `TestSubjectTypeToken`, `TestExtractDispatch`.

- [ ] **Step 6: Commit**

```bash
git add cmd/extractor/handler.go cmd/extractor/json.go cmd/extractor/handler_test.go
git commit -m "feat(extractor): add tmi-extractor job handler"
```

---

## Task 13: tmi-extractor entrypoint

**Files:**
- Create: `cmd/extractor/main.go`

- [ ] **Step 1: Write the entrypoint**

Create `cmd/extractor/main.go`:

```go
// Command tmi-extractor is the sandboxed document-parse worker of the TMI
// Component Platform (issue #347). It consumes jobs.extract.* from NATS
// JetStream, parses each payload with pkg/extract, and publishes the next
// pipeline stage or a typed failure result. It runs egress: none — its only
// network peer is NATS.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/extract"
)

func main() {
	logger := slogging.Get()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := worker.ConfigFromEnv()
	if err != nil {
		logger.Error("tmi-extractor: config error: %v", err)
		os.Exit(1)
	}

	conn, err := worker.Connect(ctx, cfg)
	if err != nil {
		logger.Error("tmi-extractor: NATS connect failed: %v", err)
		os.Exit(1)
	}
	defer conn.Close()

	limits := limitsFromEnv()
	handler := newExtractHandler(conn, limits)
	instanceID := worker.EnvOr("HOSTNAME", "tmi-extractor-local")

	go worker.RunHeartbeat(ctx, conn, instanceID,
		worker.EnvDuration("TMI_HEARTBEAT_INTERVAL", 0))

	logger.Info("tmi-extractor: starting consumer, component=%s", cfg.ComponentName)
	err = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
		Durable:       "tmi-extractor",
		FilterSubject: worker.SubjectExtractPrefix + ">",
		AckWait:       worker.EnvDuration("TMI_JOB_ACK_WAIT", 90*time.Second),
		MaxDeliver:    3,
	}, handler.Handle)
	if err != nil {
		logger.Error("tmi-extractor: consumer error: %v", err)
		os.Exit(1)
	}
	logger.Info("tmi-extractor: stopped cleanly")
}

// limitsFromEnv builds extraction limits, overriding the defaults with the
// TMI_CONTENT_EXTRACTORS_* env vars the CR's spec.config supplies. Unset
// vars keep the design-spec default.
func limitsFromEnv() extract.Limits {
	l := extract.DefaultLimits()
	if v := worker.EnvDuration("TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET", 0); v > 0 {
		l.WallClockBudget = v
	}
	return l
}
```

> The full set of `TMI_CONTENT_EXTRACTORS_*` overrides (compressed-size, part-count, etc.) is intentionally minimal here — the wall-clock budget is the one the CR commonly tunes. Adding more is a one-line `EnvOr`/parse each; do it when a CR needs it. The cgroup caps (CPU/RAM) come from the CR `resources` field, not env vars.

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/tmi-extractor ./cmd/extractor/`
Expected: builds clean.

- [ ] **Step 3: Verify it fails fast without config**

Run: `./bin/tmi-extractor`
Expected: exits non-zero with `config error: worker: required env var TMI_NATS_URL is not set`.

- [ ] **Step 4: Commit**

```bash
git add cmd/extractor/main.go
git commit -m "feat(extractor): add tmi-extractor worker entrypoint"
```

---

## Task 14: tmi-chunk-embed embedder and handler

**Files:**
- Create: `cmd/chunkembed/embedder.go`
- Create: `cmd/chunkembed/handler.go`
- Create: `cmd/chunkembed/json.go`
- Test: `cmd/chunkembed/handler_test.go`

`tmi-chunk-embed` chunks the extracted text (relocated `extract.TextChunker`) and embeds each chunk via an OpenAI-compatible langchaingo embedder. Its embedding config comes from env vars (CR `spec.config` + `secretRefs`); Plan 3 / #415 replaces this with the shared-config object.

- [ ] **Step 1: Write the failing handler test**

Create `cmd/chunkembed/handler_test.go`:

```go
package main

import (
	"testing"

	"github.com/ericfitz/tmi/pkg/extract"
)

func TestChunkText(t *testing.T) {
	chunker := extract.NewTextChunker(50, 10)
	long := "Sentence one. Sentence two. Sentence three. Sentence four. Sentence five."
	chunks := chunker.Chunk(long)
	if len(chunks) < 2 {
		t.Fatalf("expected the long text to split into >=2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c == "" {
			t.Fatalf("chunk %d is empty", i)
		}
	}
}

func TestEmbeddingResultShape(t *testing.T) {
	// An EmbeddingResult holds one vector per chunk in chunk order.
	r := EmbeddingResult{
		Chunks:  []string{"a", "b"},
		Vectors: [][]float32{{0.1}, {0.2}},
	}
	if err := r.validate(); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	bad := EmbeddingResult{Chunks: []string{"a", "b"}, Vectors: [][]float32{{0.1}}}
	if err := bad.validate(); err == nil {
		t.Fatal("expected error: chunk/vector count mismatch")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/chunkembed/`
Expected: FAIL — `undefined: EmbeddingResult`.

- [ ] **Step 3: Write the embedder construction**

Create `cmd/chunkembed/embedder.go`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/tmc/langchaingo/embeddings"
	"github.com/tmc/langchaingo/llms/openai"
)

// embedConfig is tmi-chunk-embed's embedding configuration, read from env.
// In Plan 3 / #415 this is replaced by the projected shared-config object so
// the worker and the monolith's Timmy query path cannot diverge.
type embedConfig struct {
	Model   string
	BaseURL string
	APIKey  string
}

// embedConfigFromEnv reads the embedding config. Model and BaseURL come from
// the CR spec.config; APIKey comes from a secretRef-injected env var.
func embedConfigFromEnv() (embedConfig, error) {
	model, err := worker.MustEnv("TMI_EMBEDDING_MODEL")
	if err != nil {
		return embedConfig{}, err
	}
	baseURL, err := worker.MustEnv("TMI_EMBEDDING_BASE_URL")
	if err != nil {
		return embedConfig{}, err
	}
	apiKey, err := worker.MustEnv("TMI_EMBEDDING_API_KEY")
	if err != nil {
		return embedConfig{}, err
	}
	return embedConfig{Model: model, BaseURL: baseURL, APIKey: apiKey}, nil
}

// newEmbedder builds an OpenAI-compatible langchaingo embedder.
func newEmbedder(cfg embedConfig) (embeddings.Embedder, error) {
	llm, err := openai.New(
		openai.WithEmbeddingModel(cfg.Model),
		openai.WithBaseURL(cfg.BaseURL),
		openai.WithToken(cfg.APIKey),
	)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: build embedding LLM: %w", err)
	}
	emb, err := embeddings.NewEmbedder(llm)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: build embedder: %w", err)
	}
	return emb, nil
}

// embedChunks embeds every chunk, returning one vector per chunk in order.
func embedChunks(ctx context.Context, emb embeddings.Embedder, chunks []string) ([][]float32, error) {
	if len(chunks) == 0 {
		return nil, nil
	}
	vectors, err := emb.EmbedDocuments(ctx, chunks)
	if err != nil {
		return nil, fmt.Errorf("chunkembed: embed documents: %w", err)
	}
	return vectors, nil
}
```

- [ ] **Step 4: Write the handler**

Create `cmd/chunkembed/handler.go`:

```go
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

// chunk sizing — characters per chunk and overlap. Matches the monolith's
// Timmy chunker defaults so ingest-time and query-time chunking agree.
const (
	chunkMaxChars = 1000
	chunkOverlap  = 100
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
		return err // transient — redeliver
	}

	chunks := h.chunker.Chunk(string(text))
	vectors, err := embedChunks(ctx, h.embedder, chunks)
	if err != nil {
		// An embedding-API failure may be transient (rate limit, 5xx).
		// Nak for redelivery; JetStream MaxDeliver bounds the retries.
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
```

- [ ] **Step 5: Write the json helper**

Create `cmd/chunkembed/json.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
)

// jsonMarshal wraps json.Marshal with a labelled error.
func jsonMarshal(v any, label string) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return b, nil
}
```

- [ ] **Step 6: Run the handler test**

Run: `go test ./cmd/chunkembed/`
Expected: PASS — `TestChunkText`, `TestEmbeddingResultShape`.

- [ ] **Step 7: Commit**

```bash
git add cmd/chunkembed/embedder.go cmd/chunkembed/handler.go cmd/chunkembed/json.go cmd/chunkembed/handler_test.go
git commit -m "feat(chunkembed): add tmi-chunk-embed embedder and job handler"
```

---

## Task 15: tmi-chunk-embed entrypoint

**Files:**
- Create: `cmd/chunkembed/main.go`

- [ ] **Step 1: Write the entrypoint**

Create `cmd/chunkembed/main.go`:

```go
// Command tmi-chunk-embed is the chunk-and-embed worker of the TMI Component
// Platform (issue #347). It consumes jobs.chunkembed.* from NATS JetStream,
// splits the extracted text into chunks, embeds each chunk via an
// OpenAI-compatible embedding API, and publishes the final result. It runs
// egress: allowlist — it may reach NATS and the embedding API host only.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
)

func main() {
	logger := slogging.Get()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := worker.ConfigFromEnv()
	if err != nil {
		logger.Error("tmi-chunk-embed: config error: %v", err)
		os.Exit(1)
	}

	embCfg, err := embedConfigFromEnv()
	if err != nil {
		logger.Error("tmi-chunk-embed: embedding config error: %v", err)
		os.Exit(1)
	}
	embedder, err := newEmbedder(embCfg)
	if err != nil {
		logger.Error("tmi-chunk-embed: embedder build failed: %v", err)
		os.Exit(1)
	}

	conn, err := worker.Connect(ctx, cfg)
	if err != nil {
		logger.Error("tmi-chunk-embed: NATS connect failed: %v", err)
		os.Exit(1)
	}
	defer conn.Close()

	handler := newChunkEmbedHandler(conn, embedder)
	instanceID := worker.EnvOr("HOSTNAME", "tmi-chunk-embed-local")

	go worker.RunHeartbeat(ctx, conn, instanceID,
		worker.EnvDuration("TMI_HEARTBEAT_INTERVAL", 0))

	logger.Info("tmi-chunk-embed: starting consumer, component=%s", cfg.ComponentName)
	err = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
		Durable:       "tmi-chunk-embed",
		FilterSubject: worker.SubjectChunkEmbedPrefix + ">",
		AckWait:       worker.EnvDuration("TMI_JOB_ACK_WAIT", 120*time.Second),
		MaxDeliver:    3,
	}, handler.Handle)
	if err != nil {
		logger.Error("tmi-chunk-embed: consumer error: %v", err)
		os.Exit(1)
	}
	logger.Info("tmi-chunk-embed: stopped cleanly")
}
```

- [ ] **Step 2: Build the binary**

Run: `go build -o bin/tmi-chunk-embed ./cmd/chunkembed/`
Expected: builds clean.

- [ ] **Step 3: Verify it fails fast without config**

Run: `./bin/tmi-chunk-embed`
Expected: exits non-zero with a `config error` naming the first missing env var.

- [ ] **Step 4: Commit**

```bash
git add cmd/chunkembed/main.go
git commit -m "feat(chunkembed): add tmi-chunk-embed worker entrypoint"
```

---

## Task 16: Worker-pipeline integration test

**Files:**
- Create: `internal/worker/pipeline_integration_test.go`

This test runs both handlers in-process against a real NATS+JetStream (service container) and asserts a job flows extract → chunkembed → result. It is the process-mode tier — it does NOT need kind.

- [ ] **Step 1: Write the integration test**

Create `internal/worker/pipeline_integration_test.go`:

```go
package worker_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// This test exercises the worker runtime end to end against a real NATS
// JetStream server. It publishes an extract job and asserts a result
// envelope lands on jobs.result.<job_id>. The extract+chunkembed handlers
// are driven by RunConsumer in background goroutines.
//
// Requires: TMI_RUN_NATS_TESTS=1 and a NATS server with JetStream. CI wires
// this via a services: nats container (see the Makefile target test-worker).
func TestPipeline_Integration(t *testing.T) {
	if os.Getenv("TMI_RUN_NATS_TESTS") == "" {
		t.Skip("set TMI_RUN_NATS_TESTS=1 with a NATS JetStream server available")
	}
	natsURL := os.Getenv("TMI_TEST_NATS_URL")
	if natsURL == "" {
		natsURL = "nats://127.0.0.1:4222"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := worker.Connect(ctx, worker.Config{NATSURL: natsURL, ComponentName: "itest"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	// Put a plaintext source payload.
	jobID := "itest-job-1"
	srcRef, err := conn.PutPayload(ctx, jobID+"/source", []byte("hello integration world"))
	if err != nil {
		t.Fatalf("put source: %v", err)
	}

	// Subscribe to the result subject before publishing the job.
	results := subscribeResults(ctx, t, conn, jobID)

	// Publish the extract job.
	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: "text/plain",
		Limits:      jobenvelope.Limits{WallClock: 10 * time.Second},
		Deadline:    time.Now().Add(20 * time.Second),
		Input:       jobenvelope.Input{ObjectRef: srcRef, ByteSize: 23},
	}
	jb, _ := json.Marshal(job)
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"plaintext", jb); err != nil {
		t.Fatalf("publish job: %v", err)
	}

	select {
	case res := <-results:
		if res.Status != jobenvelope.StatusCompleted {
			t.Fatalf("expected completed, got %s (%s)", res.Status, res.ReasonCode)
		}
		if res.Output.ResultRef == "" {
			t.Fatal("completed result missing result_ref")
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for result envelope")
	}
}
```

- [ ] **Step 2: Add the test wiring helper**

Append to `internal/worker/pipeline_integration_test.go`. This helper starts both handlers (imported from the worker binaries' logic — but the handlers live in `package main`, which a test cannot import). Resolve this by exercising the runtime with **inline handlers** that call the same `pkg/extract` / `pkg/jobenvelope` code, so the integration test validates the *runtime* (consumer loop, NATS, Object Store, publish), and the handler-specific logic stays covered by Task 12 / Task 14 unit tests:

```go
// subscribeResults wires an inline extract handler and an inline chunkembed
// handler onto the bus, then returns a channel that receives the final
// result envelope. The inline handlers mirror cmd/extractor/handler.go and
// cmd/chunkembed/handler.go but are defined here because package main is not
// importable by a test.
func subscribeResults(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) <-chan jobenvelope.Result {
	t.Helper()
	out := make(chan jobenvelope.Result, 1)

	// Inline extract handler: read source, publish chunkembed job.
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			Durable:       "itest-extract",
			FilterSubject: worker.SubjectExtractPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, func(c context.Context, job jobenvelope.Job) error {
			data, err := conn.GetPayload(c, job.Input.ObjectRef)
			if err != nil {
				return err
			}
			ref, err := conn.PutPayload(c, job.JobID+"/extracted", data)
			if err != nil {
				return err
			}
			next := jobenvelope.Job{
				JobID: job.JobID, ContentType: job.ContentType,
				Limits: job.Limits, Deadline: job.Deadline,
				Input: jobenvelope.Input{ObjectRef: ref, ByteSize: int64(len(data))},
			}
			b, _ := json.Marshal(next)
			return conn.Publish(c, worker.ChunkEmbedSubject(job.JobID), b)
		})
	}()

	// Inline chunkembed handler: read text, publish a completed result.
	go func() {
		_ = worker.RunConsumer(ctx, conn, worker.ConsumerConfig{
			Durable:       "itest-chunkembed",
			FilterSubject: worker.SubjectChunkEmbedPrefix + ">",
			AckWait:       15 * time.Second,
			MaxDeliver:    2,
		}, func(c context.Context, job jobenvelope.Job) error {
			text, err := conn.GetPayload(c, job.Input.ObjectRef)
			if err != nil {
				return err
			}
			ref, err := conn.PutPayload(c, job.JobID+"/result", text)
			if err != nil {
				return err
			}
			res := jobenvelope.Result{
				JobID: job.JobID, Status: jobenvelope.StatusCompleted,
				Output: jobenvelope.Output{ResultRef: ref},
			}
			b, _ := json.Marshal(res)
			return conn.Publish(c, worker.ResultSubject(job.JobID), b)
		})
	}()

	// Subscribe to the result subject via a plain core NATS-style consumer.
	go func() {
		cons, err := conn.JetStream().CreateOrUpdateConsumer(ctx, worker.JobsStream,
			jetstreamConsumerForResult(jobID))
		if err != nil {
			t.Errorf("result consumer: %v", err)
			return
		}
		cc, err := cons.Consume(func(msg jetstreamMsg) {
			var r jobenvelope.Result
			if json.Unmarshal(msg.Data(), &r) == nil && r.JobID == jobID {
				_ = msg.Ack()
				out <- r
			} else {
				_ = msg.Ack()
			}
		})
		if err != nil {
			t.Errorf("result consume: %v", err)
			return
		}
		<-ctx.Done()
		cc.Stop()
	}()
	return out
}
```

> **Step 2 cleanup note:** the snippet above references `jetstreamConsumerForResult` and the `jetstreamMsg` type as shorthand. Replace them with the real `jetstream.ConsumerConfig{Durable: "itest-result", FilterSubject: worker.ResultSubject(jobID), AckPolicy: jetstream.AckExplicitPolicy}` and `jetstream.Msg`, importing `"github.com/nats-io/nats.go/jetstream"`. They are written as placeholders here ONLY to keep the result-subscription readable; the executor must inline the real types — do not ship the placeholder names.

- [ ] **Step 3: Run the integration test against local NATS**

```bash
docker run -d --rm --name tmi-nats-test -p 4222:4222 nats:2.10-alpine -js
TMI_RUN_NATS_TESTS=1 go test ./internal/worker/ -run TestPipeline_Integration -v
docker stop tmi-nats-test
```
Expected: PASS — a completed result envelope arrives within the timeout.

- [ ] **Step 4: Commit**

```bash
git add internal/worker/pipeline_integration_test.go
git commit -m "test(platform): add worker-pipeline integration test against NATS JetStream"
```

---

## Task 17: Dockerfiles and Makefile targets

**Files:**
- Create: `deployments/docker/extractor.Dockerfile`
- Create: `deployments/docker/chunkembed.Dockerfile`
- Modify: `Makefile`

- [ ] **Step 1: Write the extractor Dockerfile**

Create `deployments/docker/extractor.Dockerfile`:

```dockerfile
# tmi-extractor — sandboxed document-parse worker.
# Chainguard distroless static base, matching the main TMI server hardening.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/tmi-extractor ./cmd/extractor/

FROM cgr.dev/chainguard/static:latest
COPY --from=build /out/tmi-extractor /tmi-extractor
USER 65532:65532
ENTRYPOINT ["/tmi-extractor"]
```

- [ ] **Step 2: Write the chunkembed Dockerfile**

Create `deployments/docker/chunkembed.Dockerfile`:

```dockerfile
# tmi-chunk-embed — chunk-and-embed worker.
# Chainguard distroless static base, matching the main TMI server hardening.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" \
    -o /out/tmi-chunk-embed ./cmd/chunkembed/

FROM cgr.dev/chainguard/static:latest
COPY --from=build /out/tmi-chunk-embed /tmi-chunk-embed
USER 65532:65532
ENTRYPOINT ["/tmi-chunk-embed"]
```

- [ ] **Step 3: Add the Makefile targets**

Append to the `Makefile`, after the existing platform targets block (after the `test-e2e-platform` target):

```makefile
.PHONY: build-extractor build-chunkembed build-workers test-workers \
        build-extractor-container build-chunkembed-container

build-extractor:  ## Build the tmi-extractor worker binary
	go build -o bin/tmi-extractor ./cmd/extractor/

build-chunkembed:  ## Build the tmi-chunk-embed worker binary
	go build -o bin/tmi-chunk-embed ./cmd/chunkembed/

build-workers: build-extractor build-chunkembed  ## Build both worker binaries

test-workers:  ## Run worker + extract + envelope tests (starts a NATS JetStream container)
	@docker run -d --rm --name tmi-nats-test -p 4222:4222 nats:2.10-alpine -js >/dev/null
	@sleep 2
	@TMI_RUN_NATS_TESTS=1 TMI_TEST_NATS_URL=nats://127.0.0.1:4222 \
		go test ./internal/worker/... ./pkg/extract/... ./pkg/jobenvelope/... \
		./cmd/extractor/... ./cmd/chunkembed/...; \
		status=$$?; docker stop tmi-nats-test >/dev/null; exit $$status

build-extractor-container:  ## Build the tmi-extractor container image
	docker build -f deployments/docker/extractor.Dockerfile -t tmi-extractor:dev .

build-chunkembed-container:  ## Build the tmi-chunk-embed container image
	docker build -f deployments/docker/chunkembed.Dockerfile -t tmi-chunk-embed:dev .
```

> `test-workers` manages its own NATS container because, unlike the monolith's PG/Redis, NATS is not part of `make start-dev` yet — `make start-dev` becomes K8s-shaped in Plan 3. Keeping the container lifecycle inside the target follows the project rule "always use make targets" while staying self-contained.

- [ ] **Step 4: Verify the build targets**

Run: `make build-workers`
Expected: `bin/tmi-extractor` and `bin/tmi-chunk-embed` produced.

- [ ] **Step 5: Verify the test target**

Run: `make test-workers`
Expected: PASS — all worker, extract, envelope, and handler tests; the NATS container is started and stopped automatically.

- [ ] **Step 6: Verify the container builds**

Run: `make build-extractor-container && make build-chunkembed-container`
Expected: both images build. Confirm they are small (static distroless):
```bash
docker images tmi-extractor:dev tmi-chunk-embed:dev
```

- [ ] **Step 7: Commit**

```bash
git add deployments/docker/extractor.Dockerfile deployments/docker/chunkembed.Dockerfile Makefile
git commit -m "build(platform): add worker Dockerfiles and Makefile targets"
```

---

## Task 18: TMIComponent CRs for the two workers

**Files:**
- Create: `deployments/k8s/platform/components/tmi-extractor.yml`
- Create: `deployments/k8s/platform/components/tmi-chunk-embed.yml`
- Read: `config/crd/bases/tmi.dev_tmicomponents.yaml`, `api/platform/v1alpha1/tmicomponent_types.go`

- [ ] **Step 1: Confirm the CRD field names**

Run: `cat api/platform/v1alpha1/tmicomponent_types.go`
Confirm the spec fields: `image`, `jobSubjects`, `inputMode`, `egress`, `allowlist.hosts`, `config`, `secretRefs[].{name,secretName,secretKey}`, `resources`, `scratchVolume`, `scaling.{minReplicas,maxReplicas,queueDepthTarget}`. The CRs below assume these exact names — adjust if the type differs.

- [ ] **Step 2: Write the tmi-extractor CR**

Create `deployments/k8s/platform/components/tmi-extractor.yml`:

```yaml
apiVersion: tmi.dev/v1alpha1
kind: TMIComponent
metadata:
  name: tmi-extractor
  namespace: tmi-platform
spec:
  image: tmi-extractor:dev
  jobSubjects:
    - jobs.extract.ooxml
    - jobs.extract.pdf
    - jobs.extract.html
    - jobs.extract.plaintext
  inputMode: content-ref
  # egress: none — the parse sandbox reaches NATS only. No DNS, no
  # 169.254.169.254, no DB, no Redis, no internet.
  egress: none
  config:
    TMI_COMPONENT_NAME: tmi-extractor
    TMI_NATS_URL: nats://nats.tmi-platform.svc:4222
    TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET: 30s
    TMI_JOB_ACK_WAIT: 90s
  resources:
    requests:
      cpu: 250m
      memory: 128Mi
    limits:
      cpu: 500m
      memory: 256Mi
  scaling:
    minReplicas: 0
    maxReplicas: 10
    queueDepthTarget: 5
```

- [ ] **Step 3: Write the tmi-chunk-embed CR**

Create `deployments/k8s/platform/components/tmi-chunk-embed.yml`:

```yaml
apiVersion: tmi.dev/v1alpha1
kind: TMIComponent
metadata:
  name: tmi-chunk-embed
  namespace: tmi-platform
spec:
  image: tmi-chunk-embed:dev
  jobSubjects:
    - jobs.chunkembed.>
  inputMode: content-ref
  # egress: allowlist — the worker reaches NATS and the embedding API host
  # only. Replace the host below with the real embedding endpoint host.
  egress: allowlist
  allowlist:
    hosts:
      - api.openai.com
  config:
    TMI_COMPONENT_NAME: tmi-chunk-embed
    TMI_NATS_URL: nats://nats.tmi-platform.svc:4222
    TMI_EMBEDDING_MODEL: text-embedding-3-small
    TMI_EMBEDDING_BASE_URL: https://api.openai.com/v1
    TMI_JOB_ACK_WAIT: 120s
  secretRefs:
    # The embedding API key is mounted from a K8s Secret as the env var
    # TMI_EMBEDDING_API_KEY. The Secret object is created out of band:
    #   kubectl -n tmi-platform create secret generic tmi-embedding \
    #     --from-literal=api-key=sk-...
    - name: TMI_EMBEDDING_API_KEY
      secretName: tmi-embedding
      secretKey: api-key
  resources:
    requests:
      cpu: 250m
      memory: 256Mi
    limits:
      cpu: 1000m
      memory: 512Mi
  scaling:
    minReplicas: 0
    maxReplicas: 10
    queueDepthTarget: 5
```

- [ ] **Step 4: Validate the CRs against the CRD schema**

With the Plan 1 cluster up (`make e2e-platform-up`), dry-run apply:
```bash
kubectl --context kind-tmi-platform apply --dry-run=server \
  -f deployments/k8s/platform/components/tmi-extractor.yml \
  -f deployments/k8s/platform/components/tmi-chunk-embed.yml
```
Expected: both CRs validate against the CRD OpenAPI schema (`tmicomponent.tmi.dev/tmi-extractor created (server dry run)`). If a field is rejected, fix the CR to match the actual CRD — the CRD is the source of truth.

- [ ] **Step 5: Commit**

```bash
git add deployments/k8s/platform/components/
git commit -m "feat(platform): add TMIComponent CRs for tmi-extractor and tmi-chunk-embed"
```

---

## Task 19: Worker-level kind e2e test

**Files:**
- Create: `test/e2e/platform/workers_e2e_test.go`
- Modify: `Makefile`

This e2e is the worker-level tier: with the Plan 1 cluster + controller + both `TMIComponent` CRs applied, publish a job by hand and assert a result lands. It does NOT assert the full #347 acceptance criteria (crash isolation, egress denial, OOM, dead-letter) — those need the Plan 3 monolith path and are Plan 3's e2e.

- [ ] **Step 1: Write the e2e test**

Create `test/e2e/platform/workers_e2e_test.go`:

```go
//go:build e2e

package platform_test

import (
	"context"
	"encoding/json"
	"os/exec"
	"testing"
	"time"

	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
)

// TestWorkersE2E_PlaintextJob applies nothing itself — it assumes
// `make e2e-platform-up`, the component-controller, and both TMIComponent
// CRs are already deployed (the Makefile target test-e2e-workers wires
// that). It connects to the in-cluster NATS through a port-forward, puts a
// plaintext payload, publishes an extract job, and asserts a completed
// result envelope lands.
func TestWorkersE2E_PlaintextJob(t *testing.T) {
	// The Makefile target sets up a kubectl port-forward to NATS on 4222.
	natsURL := "nats://127.0.0.1:4222"

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	conn, err := worker.Connect(ctx, worker.Config{NATSURL: natsURL, ComponentName: "e2e"})
	if err != nil {
		t.Fatalf("connect to in-cluster NATS: %v", err)
	}
	defer conn.Close()

	jobID := "e2e-job-1"
	srcRef, err := conn.PutPayload(ctx, jobID+"/source", []byte("end to end plaintext"))
	if err != nil {
		t.Fatalf("put source: %v", err)
	}

	job := jobenvelope.Job{
		JobID:       jobID,
		ContentType: "text/plain",
		Limits:      jobenvelope.Limits{WallClock: 10 * time.Second},
		Deadline:    time.Now().Add(60 * time.Second),
		Input:       jobenvelope.Input{ObjectRef: srcRef, ByteSize: 20},
	}
	jb, _ := json.Marshal(job)
	if err := conn.Publish(ctx, worker.SubjectExtractPrefix+"plaintext", jb); err != nil {
		t.Fatalf("publish job: %v", err)
	}

	// Poll the result subject. KEDA scales tmi-extractor and tmi-chunk-embed
	// from zero on queue depth, so allow generous time for cold start.
	res := waitForResult(ctx, t, conn, jobID)
	if res.Status != jobenvelope.StatusCompleted {
		t.Fatalf("expected completed, got %s reason=%s", res.Status, res.ReasonCode)
	}

	// Sanity: confirm the worker pods actually scaled up (KEDA worked).
	out, err := exec.CommandContext(ctx, "kubectl", "--context", "kind-tmi-platform",
		"-n", "tmi-platform", "get", "pods", "-l", "app=tmi-extractor",
		"--no-headers").Output()
	if err != nil {
		t.Logf("pod check skipped: %v", err)
	} else if len(out) == 0 {
		t.Log("warning: no tmi-extractor pods observed (may have scaled back to zero)")
	}
}
```

- [ ] **Step 2: Add the result-wait helper**

Append to `test/e2e/platform/workers_e2e_test.go`:

```go
// waitForResult subscribes a durable JetStream consumer to the job's result
// subject and blocks until a Result arrives or ctx expires.
func waitForResult(ctx context.Context, t *testing.T, conn *worker.Conn, jobID string) jobenvelope.Result {
	t.Helper()
	cons, err := conn.JetStream().CreateOrUpdateConsumer(ctx, worker.JobsStream,
		resultConsumerConfig(jobID))
	if err != nil {
		t.Fatalf("result consumer: %v", err)
	}
	resCh := make(chan jobenvelope.Result, 1)
	cc, err := cons.Consume(func(msg natsMsg) {
		var r jobenvelope.Result
		if json.Unmarshal(msg.Data(), &r) == nil && r.JobID == jobID {
			_ = msg.Ack()
			resCh <- r
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		t.Fatalf("consume results: %v", err)
	}
	defer cc.Stop()
	select {
	case r := <-resCh:
		return r
	case <-ctx.Done():
		t.Fatal("timed out waiting for the worker pipeline result")
		return jobenvelope.Result{}
	}
}
```

> Replace `resultConsumerConfig(jobID)` with an inline `jetstream.ConsumerConfig{Durable: "e2e-result", FilterSubject: worker.ResultSubject(jobID), AckPolicy: jetstream.AckExplicitPolicy}` and `natsMsg` with `jetstream.Msg`, importing `"github.com/nats-io/nats.go/jetstream"`. Named here for readability only — inline the real types; do not ship the placeholder names.

- [ ] **Step 3: Add the Makefile e2e target**

Append to the `Makefile` after the Task 17 worker targets:

```makefile
.PHONY: test-e2e-workers

test-e2e-workers:  ## Build worker images, load into kind, deploy CRs, run the worker e2e
	@echo ">> assumes 'make e2e-platform-up' has run and the controller is deployed"
	$(MAKE) build-extractor-container build-chunkembed-container
	kind load docker-image tmi-extractor:dev --name tmi-platform
	kind load docker-image tmi-chunk-embed:dev --name tmi-platform
	kubectl --context kind-tmi-platform -n tmi-platform create secret generic tmi-embedding \
		--from-literal=api-key=$${TMI_EMBEDDING_API_KEY:-sk-e2e-placeholder} \
		--dry-run=client -o yaml | kubectl --context kind-tmi-platform apply -f -
	kubectl --context kind-tmi-platform apply -f deployments/k8s/platform/components/
	@echo ">> port-forwarding NATS to localhost:4222 for the test"
	kubectl --context kind-tmi-platform -n tmi-platform port-forward svc/nats 4222:4222 & \
		PF_PID=$$!; sleep 3; \
		go test -tags e2e ./test/e2e/platform/ -run TestWorkersE2E -v; \
		status=$$?; kill $$PF_PID 2>/dev/null; exit $$status
```

> The chunk-embed step needs a reachable embedding API. For a hermetic e2e the executor may point `TMI_EMBEDDING_BASE_URL` at a stub; with the placeholder key the plaintext job still exercises extract → chunkembed → result wiring and the chunk-embed handler will surface an embedding-API failure as a `failed` result — which still proves the pipeline plumbing. The test asserts `completed` only when a real embedding endpoint is supplied; if running hermetic, change the assertion to accept `failed` with an embedding reason. Decide based on whether a real key is available and note the choice in the commit.

- [ ] **Step 4: Run the worker e2e**

```bash
make e2e-platform-up
make build-component-controller
# deploy the controller per Plan 1's instructions, then:
make test-e2e-workers
make e2e-platform-down
```
Expected: the e2e publishes a job and observes a result envelope; worker pods scale from zero via KEDA.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/platform/workers_e2e_test.go Makefile
git commit -m "test(platform): add worker-level kind e2e for the extraction pipeline"
```

---

## Task 20: Lint, full build, and final verification

**Files:** none — verification only.

- [ ] **Step 1: Run gofmt**

Run: `gofmt -l pkg/ internal/worker/ cmd/extractor/ cmd/chunkembed/`
Expected: no output (all files formatted). If any file is listed, run `gofmt -w` on it.

- [ ] **Step 2: Run the linter**

Run: `make lint`
Expected: 0 issues. Fix every finding — do not suppress.

- [ ] **Step 3: Full monolith build**

Run: `make build-server`
Expected: builds clean — confirms the Task 7 re-wiring did not regress the monolith.

- [ ] **Step 4: Build the workers and the controller**

Run: `make build-workers && make build-component-controller`
Expected: all three binaries build.

- [ ] **Step 5: Run the monolith unit tests**

Run: `make test-unit`
Expected: PASS — the relocation kept extraction behavior identical.

- [ ] **Step 6: Run the worker tests**

Run: `make test-workers`
Expected: PASS — worker runtime, `pkg/extract`, `pkg/jobenvelope`, both handlers.

- [ ] **Step 7: Run the platform controller tests**

Run: `make test-platform`
Expected: PASS — Plan 1's controller tests still pass (Plan 2 did not touch the controller).

- [ ] **Step 8: Security regression scan**

Run the `security-regression` skill against the branch changes (new outbound HTTP in `cmd/chunkembed` to the embedding API; new NATS connections). Expected: PASS. The chunk-embed worker's egress is allowlisted at the CRD/NetworkPolicy layer; confirm no SSRF-reachable user-controlled URL was introduced — the embedding base URL comes from CR config, not request input.

- [ ] **Step 9: Oracle DB review**

Plan 2 introduces **no** DB-touching code (`extraction_jobs` is Plan 3). Confirm no `*_repository.go`, GORM model, or raw SQL changed:
```bash
git diff --name-only dev/1.4.0 | rg 'repository|store_gorm|models/|dberrors' || echo "no DB-touching files — oracle-db-admin review not required for Plan 2"
```
Expected: the `echo` branch fires. If any DB file shows up, dispatch the `oracle-db-admin` subagent.

- [ ] **Step 10: Final commit if any fixes were made**

```bash
git add -A
git commit -m "chore(platform): lint and verification fixes for Plan 2 workers"
```

---

## Notes for the executor

- **Relocation, not rewrite.** Tasks 4–7 move code; they must not change extraction *behavior*. If a relocated test fails, the cause is a wiring/rename mistake, not a needed logic change. The monolith's `make test-unit` passing after Task 7 is the proof.
- **`pkg/extract` must stay framework-free.** It may import `golang.org/x/net/html`, `archive/zip`, `encoding/xml`, the PDF/excelize libs — but never `internal/config`, `internal/slogging`, Gin, or GORM. If the executor finds a relocated file importing one of those, the dependency must be broken (the limits already moved to the `Limits` struct; logging in extractors should be dropped or replaced with returned errors).
- **NATS naming is mirrored, not authoritative.** `internal/worker/names.go` mirrors what Plan 1's `render_jetstream.go` renders. If they disagree at integration time, the controller wins — fix `names.go`.
- **Plan 3 consumes Plan 2's contracts unchanged:** `pkg/jobenvelope` (the envelope), `internal/worker` (the runtime), and the result subject convention. Plan 3 adds the monolith result-consumer, the `extraction_jobs` table, and the `202` request path on top — it must not redefine the envelope.
- **The chunk-embed embedding config is interim.** Plan 2 reads it from env vars; #415 / Plan 3 replaces it with the projected shared-config object so the worker and the monolith's Timmy query path cannot disagree on the embedding model. Do not over-build the env-var path.

## Self-Review

- **Spec coverage:** worker binaries (T12–15), NATS consumer loop (T11), heartbeat (T10), typed-error envelopes (T2–3, T12, T14), Object Store payload-by-reference (T9), relocation of extractor logic (T4–7), the `tmi-extractor`/`tmi-chunk-embed` `TMIComponent` CRs (T18), Chainguard images (T17), Makefile targets (T17, T19), process-mode + worker-level e2e (T16, T19). Deferred-to-Plan-3 items (extraction_jobs table, result-consumer, 202 path, OpenAPI, full acceptance-criteria e2e, flag-gated cutover) are explicitly out of scope per the user's decisions and the spec's plan split.
- **Placeholder scan:** two readability placeholders are explicitly flagged for the executor to inline (`jetstreamConsumerForResult`/`jetstreamMsg` in T16, `resultConsumerConfig`/`natsMsg` in T19) — each has an inline-the-real-type instruction. No silent TBDs.
- **Type consistency:** `jobenvelope.Job`/`Result`/`Input`/`Output`/`Limits`/`Status` are defined in T2 and used unchanged in T11–16, T19. `extract.Limits`/`DefaultLimits`/`ClassifyError`/`Classification` defined in T4 and used in T6, T12. `worker.Conn`/`Config`/`ConsumerConfig`/`JobError`/`Heartbeat` defined in T8–11 and used in T12–16. Subject helpers (`ResultSubject`, `ChunkEmbedSubject`, `HeartbeatSubject`, `SubjectExtractPrefix`) defined in T1 and used consistently.
