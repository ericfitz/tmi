# #347 Plan 3 — Monolith Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire the already-merged extractor/chunk-embed workers into the monolith behind a runtime flag, so document extraction can run asynchronously over NATS while the existing inline path remains the default until cutover.

**Architecture:** A new `extraction_jobs` GORM table is the monolith's internal job-state authority. The `Server` owns a NATS/JetStream connection (reusing `internal/worker`). A `extraction.async_enabled` DB-backed setting routes both extraction callers (`access_poller` background loop and the `document_sub_resource_handlers` request path) between today's inline `ContentPipeline.Extract` and a publish-and-return path; the request path returns `202 Accepted`. A long-lived result-consumer goroutine subscribes to `jobs.result.*` (+ DLQ), classifies typed errors, upserts `extraction_jobs`, updates document `access_status`, emits webhook events, and deletes Object Store blobs. Failures never surface as HTTP 500.

**Tech Stack:** Go, GORM (PostgreSQL + Oracle ADB), NATS JetStream (`nats.go` v1.36 + `jetstream` API), Gin, the project's `make` targets.

**Spec:** [`docs/superpowers/specs/2026-06-03-347-plan3-monolith-integration-design.md`](../specs/2026-06-03-347-plan3-monolith-integration-design.md)

---

## Conventions for every task

- **Always use make targets**, never raw `go`/`docker`: `make build-server`, `make test-unit`, `make test-unit name=TestX`, `make test-integration`, `make lint`, `make validate-openapi`, `make generate-api`.
- **Run a single unit test** with `make test-unit name=TestName`.
- **Integration tests** use the `_Integration` suffix and run via `make test-integration` (they require the NATS service container — see Task 0).
- **Logging:** use `github.com/ericfitz/tmi/internal/slogging` (`slogging.Get()`), never `log` or `fmt.Println`.
- **GORM models** live in `api/models/models.go`, use the custom DB types (`DBVarchar`, `DBText`, `NullableDBVarchar`, `NullableDBText`, etc.), and register in `AllModels()`.
- **Commit after every green step** with a conventional-commit message referencing `#347`.
- **Branch:** all work lands on `dev/1.4.0` (no PR/main merge). Do not push (SSH key is touch-gated; the user pushes).

---

## File Structure

**New files:**
- `api/models/extraction_job.go` — the `ExtractionJob` GORM model (kept in its own file for focus; `AllModels()` still lives in `models.go`).
- `api/extraction_job_store.go` — repository: insert-queued (idempotent) + terminal upsert, both PG/Oracle-portable.
- `api/extraction_job_store_test.go` — repository unit tests (sqlmock or in-memory per existing store-test pattern).
- `api/result_consumer.go` — the result-consumer goroutine: subscribe `jobs.result.*` + DLQ, classify, upsert, update document, emit webhook, delete blobs.
- `api/result_consumer_test.go` — result-consumer handler unit tests with synthetic `Result` envelopes.
- `api/extraction_publisher.go` — the publish-side seam: marshal a `Job`, put bytes in the Object Store, publish `jobs.extract.<type>`, insert the queued row. Used by both callers.
- `api/extraction_publisher_test.go` — publisher unit tests (fake NATS conn).
- `test/integration/workflows/extraction_async_integration_test.go` — full round-trip integration test.

**Modified files:**
- `internal/worker/nats.go` — add `Conn.DeletePayload`.
- `internal/worker/nats_test.go` — test for `DeletePayload`.
- `internal/config/migratable_settings.go` — register `extraction.async_enabled`.
- `api/events.go` — add `EventDocumentExtractionCompleted` / `EventDocumentExtractionFailed`.
- `api/content_pipeline.go` — flag-routing helper(s); expose a result-classification entry usable by the consumer.
- `api/access_poller.go` — flag-gated publish path.
- `api/document_sub_resource_handlers.go` — flag-gated `202 Accepted`.
- `api/server.go` — new `Server` fields (NATS conn, publisher, job store, result-consumer) + setters.
- `cmd/server/main.go` — open NATS conn, wire the publisher/job-store/result-consumer, start the goroutine, close on shutdown.
- `api-schema/tmi-openapi.json` — `202` response + webhook event enum entries.
- `scripts/manage-server.py` (or the `make start-dev` orchestrator) + dev container setup — bring up NATS + both workers.
- `cmd/dbtool/` — picked up automatically via `AllModels()`; verify only.

---

## Task 0: Prerequisite — NATS available to the test/dev environment

**Files:**
- Modify: the `make start-dev` orchestrator (`scripts/manage-server.py` and/or the dev compose/container setup)
- Modify: `Makefile` (if a NATS container target is needed)

This task makes NATS reachable so later integration tests and local async runs work. It does NOT migrate to kind (that is Plan 4).

- [ ] **Step 1: Discover the current `make start-dev` orchestration**

Run:
```bash
rg -n "start-dev|start-database|start-redis|nats|NATS" Makefile scripts/manage-server.py
```
Expected: find how Postgres/Redis containers are started and where to add a NATS container.

- [ ] **Step 2: Add a NATS JetStream container to the dev environment**

Add a NATS container (image `nats:2.10-alpine` with `-js` for JetStream) to the dev startup, exposing `4222`, following the exact pattern used for the Redis/Postgres containers. Set `TMI_NATS_URL=nats://localhost:4222` in the dev env that the server process reads.

- [ ] **Step 3: Add both worker containers to the dev environment**

Start `tmi-extractor` and `tmi-chunk-embed` (Plan 2 already defines their Chainguard images + Makefile build targets — verify with `rg -n "build-extractor|build-chunkembed|tmi-extractor|tmi-chunk-embed" Makefile`). Each worker needs `TMI_NATS_URL=nats://nats:4222` (container network) and `TMI_COMPONENT_NAME` set. The workers self-create their streams (see `ensureStream` in `internal/worker/consumer.go`), so no controller is needed in dev.

- [ ] **Step 4: Verify the dev environment comes up**

Run: `make start-dev`
Expected: server, Postgres, Redis, NATS, and both worker containers start; `curl http://localhost:8080/` returns the version. Check worker logs show "worker consumer: ..." startup lines.

- [ ] **Step 5: Document the NATS service container for CI integration tests**

Ensure `make test-integration` can reach a NATS container (the existing `internal/worker/pipeline_integration_test.go` already depends on one — `rg -n "NATS|nats" scripts/test-framework.mk Makefile` to find the pattern). If a NATS service is already wired for that test, reuse it; otherwise add it the same way Postgres/Redis are provided to integration tests.

- [ ] **Step 6: Commit**

```bash
git add Makefile scripts/manage-server.py
git commit -m "build(platform): bring up NATS + extractor/chunk-embed workers in make start-dev

Refs #347."
```

---

## Task 1: Add `Conn.DeletePayload` to the worker NATS package

**Files:**
- Modify: `internal/worker/nats.go`
- Test: `internal/worker/nats_test.go`

The result-consumer deletes a job's Object Store blobs after persisting the result. `Conn` has `PutPayload`/`GetPayload` but no delete.

- [ ] **Step 1: Write the failing test**

Add to `internal/worker/nats_test.go` (this is an integration-style test that needs NATS; name it with the `_Integration` suffix if the file's other tests use it, otherwise follow the file's existing convention):

```go
func TestConn_DeletePayload_Integration(t *testing.T) {
	ctx := context.Background()
	conn := testConn(t) // existing helper in this package's tests; if absent, dial via Connect with the test NATS URL
	defer conn.Close()

	ref, err := conn.PutPayload(ctx, "del-me", []byte("hello"))
	if err != nil {
		t.Fatalf("PutPayload: %v", err)
	}
	if _, err := conn.GetPayload(ctx, ref); err != nil {
		t.Fatalf("GetPayload before delete: %v", err)
	}
	if err := conn.DeletePayload(ctx, ref); err != nil {
		t.Fatalf("DeletePayload: %v", err)
	}
	if _, err := conn.GetPayload(ctx, ref); err == nil {
		t.Fatal("expected GetPayload to fail after delete, got nil error")
	}
}
```

> If there is no `testConn` helper, inspect `internal/worker/nats_test.go` / `pipeline_integration_test.go` for how they obtain a `*Conn` (env `TMI_NATS_URL`) and mirror it.

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-integration name=TestConn_DeletePayload_Integration`
Expected: FAIL — `conn.DeletePayload undefined`.

- [ ] **Step 3: Implement `DeletePayload`**

Add to `internal/worker/nats.go` after `GetPayload`:

```go
// DeletePayload removes a blob by the object_ref produced by PutPayload.
// It is idempotent from the caller's perspective: deleting an absent blob
// is treated as success so result-consumer cleanup never blocks on a
// double-delivery.
func (c *Conn) DeletePayload(ctx context.Context, ref string) error {
	name, ok := payloadName(ref)
	if !ok {
		return fmt.Errorf("worker: malformed object_ref %q", ref)
	}
	if err := c.objs.Delete(ctx, name); err != nil {
		if errors.Is(err, jetstream.ErrObjectNotFound) {
			return nil
		}
		return fmt.Errorf("worker: delete payload %s: %w", name, err)
	}
	return nil
}
```

Add `"errors"` to the imports if not present (the `jetstream` import already exists).

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test-integration name=TestConn_DeletePayload_Integration`
Expected: PASS.

- [ ] **Step 5: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 6: Commit**

```bash
git add internal/worker/nats.go internal/worker/nats_test.go
git commit -m "feat(platform): add Conn.DeletePayload for result-consumer blob cleanup

Refs #347."
```

---

## Task 2: The `extraction_jobs` GORM model

**Files:**
- Create: `api/models/extraction_job.go`
- Modify: `api/models/models.go` (add to `AllModels()`)
- Test: `api/models/extraction_job_test.go`

Internal job-state table. `document_ref` is indexed with **no DB-level FK**. Follow the custom-DB-type convention from `Document`.

- [ ] **Step 1: Write the failing test**

Create `api/models/extraction_job_test.go`:

```go
package models

import "testing"

func TestExtractionJob_TableName(t *testing.T) {
	got := ExtractionJob{}.TableName()
	if got != tableName("extraction_jobs") {
		t.Fatalf("TableName() = %q, want %q", got, tableName("extraction_jobs"))
	}
}

func TestExtractionJob_InAllModels(t *testing.T) {
	found := false
	for _, m := range AllModels() {
		if _, ok := m.(*ExtractionJob); ok {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ExtractionJob not registered in AllModels()")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestExtractionJob_TableName`
Expected: FAIL — `undefined: ExtractionJob`.

- [ ] **Step 3: Create the model**

Create `api/models/extraction_job.go`:

```go
package models

import (
	"time"

	"gorm.io/gorm"
)

// Extraction job status values. The monolith actively writes only
// StatusQueued (at publish time) and the terminal StatusCompleted /
// StatusFailed (when a result lands). The intermediate values exist for
// forward-compatibility and are not written in Plan 3.
const (
	ExtractionStatusQueued        = "queued"
	ExtractionStatusExtracting    = "extracting"
	ExtractionStatusChunkEmbedding = "chunk_embedding"
	ExtractionStatusCompleted     = "completed"
	ExtractionStatusFailed        = "failed"
)

// ExtractionJob is the monolith's internal record of one async extraction
// job. It is the job-state authority for the worker pipeline. The
// result-consumer is the sole writer of terminal states; the publish-side
// callers only insert the initial queued row (idempotently). Components
// (workers) never touch this table. document_ref is indexed but has no
// database-level foreign key, so a document deleted mid-job does not cause
// a constraint violation; the result-consumer tolerates the missing row.
type ExtractionJob struct {
	JobID       DBVarchar         `gorm:"column:job_id;primaryKey;not null;size:36"`
	DocumentRef DBVarchar         `gorm:"column:document_ref;size:36;not null;index:idx_extraction_jobs_doc"`
	Status      DBVarchar         `gorm:"column:status;size:32;not null;default:queued"`
	ReasonCode  NullableDBVarchar `gorm:"column:reason_code;size:64"`
	Stage       NullableDBVarchar `gorm:"column:stage;size:32"`
	Attempts    int32             `gorm:"column:attempts;not null;default:0"`
	CreatedAt   time.Time         `gorm:"column:created_at;not null;autoCreateTime"`
	UpdatedAt   time.Time         `gorm:"column:updated_at;not null;autoUpdateTime"`
	CompletedAt *time.Time        `gorm:"column:completed_at"`
}

// TableName returns the prefixed table name.
func (ExtractionJob) TableName() string {
	return tableName("extraction_jobs")
}

// BeforeCreate is a no-op placeholder kept for symmetry with other models;
// JobID is always supplied by the caller (it is the envelope job_id), so it
// is never generated here.
func (j *ExtractionJob) BeforeCreate(tx *gorm.DB) error {
	return nil
}
```

> Verify the exact names of the custom types by reading `api/models/models.go` (e.g. confirm `NullableDBVarchar` exists; if the project uses a different nullable-varchar type, use that). Do not invent types.

- [ ] **Step 4: Register in `AllModels()`**

In `api/models/models.go`, add `&ExtractionJob{},` to the slice returned by `AllModels()` (near `&Document{}`).

- [ ] **Step 5: Run the tests to verify they pass**

Run: `make test-unit name=TestExtractionJob_TableName` then `make test-unit name=TestExtractionJob_InAllModels`
Expected: PASS for both.

- [ ] **Step 6: Verify dbtool picks up the table**

Run: `rg -n "AllModels|GetAllModels" cmd/dbtool/` and confirm `cmd/dbtool` enumerates via `AllModels()` (per the reference, `cmd/dbtool/schema.go` uses `api.GetAllModels()`). No code change needed if so; if dbtool hard-codes a table list, add `extraction_jobs` to it.

- [ ] **Step 7: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 8: Commit**

```bash
git add api/models/extraction_job.go api/models/models.go api/models/extraction_job_test.go
git commit -m "feat(api): add extraction_jobs GORM model (internal job-state authority)

No DB-level FK on document_ref. Refs #347."
```

---

## Task 3: The `ExtractionJobStore` repository (idempotent insert + terminal upsert)

**Files:**
- Create: `api/extraction_job_store.go`
- Test: `api/extraction_job_store_test.go`

Two operations: `InsertQueued` (OnConflict DoNothing) and `MarkTerminal` (OnConflict DoUpdates). Both use the dialect-portable `clause.OnConflict` pattern from `api/user_api_quota_store_gorm.go`.

- [ ] **Step 1: Inspect the existing dialect/upsert helpers**

Run:
```bash
rg -n "func Col\b|func ColumnName\b|clause.OnConflict|dialect" api/user_api_quota_store_gorm.go api/*dialect*.go
```
Confirm the names of the `Col(dialect, ...)` / `ColumnName(dialect, ...)` helpers and how the `dialect` value is obtained from a `*gorm.DB` (mirror exactly).

- [ ] **Step 2: Write the failing test**

Create `api/extraction_job_store_test.go`. Use the same DB-test harness existing store tests use (find one with `rg -ln "func.*Store.*Test|sqlmock|setupTestDB" api/*_store*_test.go` and mirror its setup). Example shape:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractionJobStore_InsertQueued_Idempotent(t *testing.T) {
	db := setupExtractionJobTestDB(t) // helper: opens in-memory/sqlite or testcontainer DB + AutoMigrate(&models.ExtractionJob{})
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	require.NoError(t, store.InsertQueued(ctx, "job-1", "doc-1"))
	// second insert of the same job_id must NOT error (OnConflict DoNothing)
	require.NoError(t, store.InsertQueued(ctx, "job-1", "doc-1"))

	var count int64
	require.NoError(t, db.Model(&models.ExtractionJob{}).Where("job_id = ?", "job-1").Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestExtractionJobStore_MarkTerminal_Upsert(t *testing.T) {
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	require.NoError(t, store.InsertQueued(ctx, "job-2", "doc-2"))
	require.NoError(t, store.MarkTerminal(ctx, "job-2", models.ExtractionStatusFailed, "extraction_limit:timeout"))

	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-2").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusFailed, string(job.Status))
	assert.Equal(t, "extraction_limit:timeout", string(job.ReasonCode))
	require.NotNil(t, job.CompletedAt)
}

func TestExtractionJobStore_MarkTerminal_WithoutQueuedRow_Inserts(t *testing.T) {
	// A result can arrive for a job whose queued-insert lost a race; upsert must create the row.
	db := setupExtractionJobTestDB(t)
	store := NewExtractionJobStore(db)
	ctx := context.Background()

	require.NoError(t, store.MarkTerminal(ctx, "job-3", models.ExtractionStatusCompleted, ""))
	var job models.ExtractionJob
	require.NoError(t, db.Where("job_id = ?", "job-3").First(&job).Error)
	assert.Equal(t, models.ExtractionStatusCompleted, string(job.Status))
}
```

Write `setupExtractionJobTestDB` mirroring the project's existing GORM unit-test DB setup (do not invent a new harness — reuse the established one).

- [ ] **Step 3: Run the tests to verify they fail**

Run: `make test-unit name=TestExtractionJobStore_InsertQueued_Idempotent`
Expected: FAIL — `undefined: NewExtractionJobStore`.

- [ ] **Step 4: Implement the store**

Create `api/extraction_job_store.go`:

```go
package api

import (
	"context"
	"fmt"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExtractionJobStore persists ExtractionJob rows. The result-consumer is the
// sole writer of terminal states; the publish-side callers only InsertQueued.
type ExtractionJobStore struct {
	db *gorm.DB
}

// NewExtractionJobStore constructs the store.
func NewExtractionJobStore(db *gorm.DB) *ExtractionJobStore {
	return &ExtractionJobStore{db: db}
}

// InsertQueued inserts a queued row for job_id. It is idempotent: a row that
// already exists (e.g. from a redelivery or a prior submit) is left
// unchanged (OnConflict DoNothing). Portable across PostgreSQL and Oracle.
func (s *ExtractionJobStore) InsertQueued(ctx context.Context, jobID, documentRef string) error {
	row := models.ExtractionJob{
		JobID:       models.DBVarchar(jobID),
		DocumentRef: models.DBVarchar(documentRef),
		Status:      models.DBVarchar(models.ExtractionStatusQueued),
	}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "job_id"}}, DoNothing: true}).
		Create(&row).Error
	if err != nil {
		return fmt.Errorf("extraction job store: insert queued %s: %w", jobID, err)
	}
	return nil
}

// MarkTerminal upserts the terminal state for job_id. If the queued row is
// missing (race), it is created. OnConflict on job_id updates status,
// reason_code, completed_at, updated_at. Portable across PostgreSQL and
// Oracle via clause.OnConflict.
func (s *ExtractionJobStore) MarkTerminal(ctx context.Context, jobID, status, reasonCode string) error {
	now := time.Now().UTC()
	row := models.ExtractionJob{
		JobID:       models.DBVarchar(jobID),
		DocumentRef: models.DBVarchar(""), // unknown on the bare-upsert path; left as-is if row exists
		Status:      models.DBVarchar(status),
		ReasonCode:  models.NullableDBVarchar(reasonCode),
		CompletedAt: &now,
	}
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "job_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "reason_code", "completed_at", "updated_at",
			}),
		}).
		Create(&row).Error
	if err != nil {
		return fmt.Errorf("extraction job store: mark terminal %s: %w", jobID, err)
	}
	return nil
}
```

> **Oracle note (for Task 9 review):** `clause.AssignmentColumns` lists raw column names. Confirm against `user_api_quota_store_gorm.go` whether this project wraps update columns with a dialect helper (`ColumnName(dialect, ...)`); if it does, use that helper here too for Oracle identifier-casing correctness. Do not deviate from the established pattern.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `make test-unit name=TestExtractionJobStore_InsertQueued_Idempotent` (then the other two by name).
Expected: PASS.

- [ ] **Step 6: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add api/extraction_job_store.go api/extraction_job_store_test.go
git commit -m "feat(api): add ExtractionJobStore with idempotent insert + terminal upsert

OnConflict-based, portable across PostgreSQL and Oracle. Refs #347."
```

---

## Task 4: The `extraction.async_enabled` operational setting

**Files:**
- Modify: `internal/config/migratable_settings.go`
- Test: `internal/config/migratable_settings_test.go`

Register the boolean setting (default `false`) so it appears in the settings service and the generated config reference.

- [ ] **Step 1: Find where boolean settings are registered**

Run: `rg -n "Type: \"bool\"|GetMigratableSettings|extraction" internal/config/migratable_settings.go`
Identify the struct/section that returns the `[]MigratableSetting` slice and the config field backing the value (the existing settings read from a config struct, e.g. `t.Enabled`).

- [ ] **Step 2: Write the failing test**

Add to `internal/config/migratable_settings_test.go`:

```go
func TestMigratableSettings_IncludesExtractionAsyncEnabled(t *testing.T) {
	cfg := getDefaultConfig() // existing test helper used by reference_gen_test.go
	var found *MigratableSetting
	for i, ms := range cfg.GetMigratableSettings() {
		if ms.Key == "extraction.async_enabled" {
			found = &cfg.GetMigratableSettings()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("extraction.async_enabled not registered")
	}
	if found.Type != "bool" {
		t.Errorf("Type = %q, want bool", found.Type)
	}
	if found.Value != "false" {
		t.Errorf("default Value = %q, want false", found.Value)
	}
	if found.EnvVar != "TMI_EXTRACTION_ASYNC_ENABLED" {
		t.Errorf("EnvVar = %q, want TMI_EXTRACTION_ASYNC_ENABLED", found.EnvVar)
	}
}
```

> Confirm `getDefaultConfig()` exists (it is used by `internal/config/reference_gen_test.go`). If the helper differs, use the one those tests use.

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test-unit name=TestMigratableSettings_IncludesExtractionAsyncEnabled`
Expected: FAIL — setting not registered.

- [ ] **Step 4: Register the setting**

Add a config field for the default (find the config struct that holds operational defaults; add an `ExtractionAsyncEnabled bool` field defaulting to `false`), then add to the `GetMigratableSettings()` slice (mirroring the `timmy.enabled` line):

```go
{Key: "extraction.async_enabled", Value: strconv.FormatBool(c.ExtractionAsyncEnabled), Type: "bool", Description: "Route document extraction through the async worker pipeline instead of inline (default false; requires NATS)", Source: settingSource("TMI_EXTRACTION_ASYNC_ENABLED"), EnvVar: "TMI_EXTRACTION_ASYNC_ENABLED"},
```

Match the receiver/variable name (`c`, `t`, etc.) and the `Class` field to the neighbouring operational settings exactly.

- [ ] **Step 5: Run the test + regenerate the config reference**

Run: `make test-unit name=TestMigratableSettings_IncludesExtractionAsyncEnabled`
Expected: PASS.

Run: `make generate-config-docs`
Expected: `config-reference.md` regenerates including the new key. (Stage the regenerated file; the staleness test `TestConfigReferenceFile_MatchesRegistry` requires it to be current.)

- [ ] **Step 6: Build + lint + full config tests**

Run: `make build-server && make lint && make test-unit name=TestConfigReferenceFile_MatchesRegistry`
Expected: all pass.

- [ ] **Step 7: Commit**

```bash
git add internal/config/migratable_settings.go internal/config/migratable_settings_test.go config-reference.md
git commit -m "feat(config): add extraction.async_enabled operational setting (default false)

Refs #347."
```

---

## Task 5: Server NATS connection + dependency wiring

**Files:**
- Modify: `api/server.go` (fields + setters)
- Modify: `cmd/server/main.go` (open conn, inject, close on shutdown)
- Test: `api/server_test.go` (setter test) — or the existing server-test file

This task adds the plumbing only; the publisher and consumer that use it come in Tasks 6–8. The flag's fail-safe rule lives here: if no NATS conn is injected, the async path is unavailable regardless of the flag.

- [ ] **Step 1: Write the failing test**

Add to the appropriate server test file:

```go
func TestServer_SetExtractionDeps_NATSAvailability(t *testing.T) {
	s := &Server{}
	if s.AsyncExtractionAvailable() {
		t.Fatal("expected async unavailable with no NATS conn")
	}
	s.SetExtractionNATS(&worker.Conn{}) // non-nil sentinel
	if !s.AsyncExtractionAvailable() {
		t.Fatal("expected async available once NATS conn is set")
	}
}
```

Add the import `"github.com/ericfitz/tmi/internal/worker"`.

- [ ] **Step 2: Run the test to verify it fails**

Run: `make test-unit name=TestServer_SetExtractionDeps_NATSAvailability`
Expected: FAIL — undefined methods.

- [ ] **Step 3: Add fields + setters to `Server`**

In `api/server.go`, add fields to the `Server` struct (near `settingsService`):

```go
	// Async extraction pipeline (Plan 3 of #347). nil natsConn means the
	// async path is unavailable and extraction falls back to inline.
	extractionNATS  *worker.Conn
	extractionJobs  *ExtractionJobStore
	resultConsumer  *ResultConsumer // started in cmd/server; nil until wired
```

Add the import `"github.com/ericfitz/tmi/internal/worker"`.

Add setters + the availability check:

```go
// SetExtractionNATS injects the monolith's NATS connection used to publish
// extraction jobs and run the result-consumer. nil disables the async path.
func (s *Server) SetExtractionNATS(conn *worker.Conn) { s.extractionNATS = conn }

// SetExtractionJobStore injects the extraction_jobs repository.
func (s *Server) SetExtractionJobStore(store *ExtractionJobStore) { s.extractionJobs = store }

// AsyncExtractionAvailable reports whether the async worker path can be used.
// It is false when no NATS connection is wired, which forces the inline path
// regardless of the extraction.async_enabled setting (fail-safe).
func (s *Server) AsyncExtractionAvailable() bool { return s.extractionNATS != nil }
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `make test-unit name=TestServer_SetExtractionDeps_NATSAvailability`
Expected: PASS.

- [ ] **Step 5: Open + inject + close the NATS conn in `cmd/server/main.go`**

Near where other dependencies are built (before `apiServer` starts), open the connection only when a NATS URL is configured, and inject it. Follow the existing config-read + error-log pattern.

```go
// Async extraction (Plan 3 of #347): open the monolith's NATS connection
// when configured. Absence is non-fatal — extraction falls back to inline.
var extractionConn *worker.Conn
if natsURL := os.Getenv("TMI_NATS_URL"); natsURL != "" {
	wcfg := worker.Config{NATSURL: natsURL, ComponentName: "monolith"}
	conn, err := worker.Connect(ctx, wcfg)
	if err != nil {
		logger.Warn("async extraction disabled: NATS connect failed: %v", err)
	} else {
		extractionConn = conn
		apiServer.SetExtractionNATS(conn)
		jobStore := api.NewExtractionJobStore(db) // db = the *gorm.DB used elsewhere in main
		apiServer.SetExtractionJobStore(jobStore)
		logger.Info("async extraction pipeline connected to NATS")
	}
}
```

In the shutdown path (alongside `stopBackgroundWorkers`), close it:

```go
if extractionConn != nil {
	extractionConn.Close()
}
```

Add imports `"os"` (likely already present) and `"github.com/ericfitz/tmi/internal/worker"`.

> Confirm the exact `*gorm.DB` variable name in `main.go` (`rg -n "gorm.DB|\.AutoMigrate|db :=|database" cmd/server/main.go`) and use it for `NewExtractionJobStore`.

- [ ] **Step 6: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add api/server.go cmd/server/main.go
git commit -m "feat(api): wire monolith NATS connection + extraction_jobs store into Server

Async path is unavailable (fail-safe to inline) when NATS is not configured. Refs #347."
```

---

## Task 6: The extraction publisher (publish-side seam)

**Files:**
- Create: `api/extraction_publisher.go`
- Test: `api/extraction_publisher_test.go`

One function both callers use: put bytes in the Object Store, build + publish a `Job` to `jobs.extract.<type>`, and insert the queued row. The content-type → subject suffix mapping mirrors what the extractor worker consumes.

- [ ] **Step 1: Confirm the extract subject convention (already established)**

The extractor consumes the wildcard `jobs.extract.>` (`cmd/extractor/main.go`: `FilterSubject: worker.SubjectExtractPrefix + ">"`) and routes **by `job.ContentType`** internally (`cmd/extractor/handler.go`: `reg.FindExtractor(job.ContentType)`). You cannot publish to a wildcard, so the publisher publishes to a concrete subject under the prefix. The established convention (see `internal/worker/pipeline_integration_test.go` and `test/e2e/platform/workers_e2e_test.go`) is `worker.SubjectExtractPrefix + "<kind>"` where `<kind>` is `plaintext` / `ooxml` / `pdf` / `html`. The suffix is only a routing hint for the stream filter; correctness comes from `ContentType` in the envelope. Mirror the existing `plaintext`/`ooxml`/`pdf`/`html` kinds.

- [ ] **Step 2: Write the failing test**

Create `api/extraction_publisher_test.go` with a fake conn + fake store capturing calls:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeExtractionBus struct {
	putName    string
	putData    []byte
	putRef     string
	pubSubject string
	pubJob     jobenvelope.Job
}

func (f *fakeExtractionBus) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	f.putName, f.putData = name, data
	f.putRef = "TMI_PAYLOADS/" + name
	return f.putRef, nil
}
func (f *fakeExtractionBus) PublishJob(ctx context.Context, subject string, job jobenvelope.Job) error {
	f.pubSubject, f.pubJob = subject, job
	return nil
}

type fakeQueuedInserter struct{ jobID, docRef string }

func (f *fakeQueuedInserter) InsertQueued(ctx context.Context, jobID, documentRef string) error {
	f.jobID, f.docRef = jobID, documentRef
	return nil
}

func TestPublishExtractionJob_PublishesAndQueues(t *testing.T) {
	bus := &fakeExtractionBus{}
	store := &fakeQueuedInserter{}
	p := &ExtractionPublisher{bus: bus, store: store}

	jobID, err := p.Publish(context.Background(), ExtractionRequest{
		DocumentID:  "doc-9",
		ContentType: "application/pdf",
		Bytes:       []byte("%PDF-1.7"),
	})
	require.NoError(t, err)
	assert.NotEmpty(t, jobID)
	assert.Equal(t, jobID, store.jobID)
	assert.Equal(t, "doc-9", store.docRef)
	assert.Equal(t, bus.putRef, bus.pubJob.Input.ObjectRef)
	assert.Contains(t, bus.pubSubject, "jobs.extract.") // exact suffix asserted per Step 1 mapping
	assert.Equal(t, "application/pdf", bus.pubJob.ContentType)
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test-unit name=TestPublishExtractionJob_PublishesAndQueues`
Expected: FAIL — undefined types.

- [ ] **Step 4: Implement the publisher**

Create `api/extraction_publisher.go`:

```go
package api

import (
	"context"
	"fmt"

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

// NewExtractionPublisher wraps a worker.Conn and the job store. The conn is
// adapted to extractionBus via connBusAdapter.
func NewExtractionPublisher(conn *worker.Conn, store *ExtractionJobStore) *ExtractionPublisher {
	return &ExtractionPublisher{bus: &connBusAdapter{conn: conn}, store: store}
}

// Publish writes the document bytes to the Object Store, publishes an
// extract job, and records a queued row. Returns the job_id for caller
// correlation (e.g. the 202 response body).
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

// extractSubjectSuffix maps a content type to the extract subject kind used
// by the established convention (jobs.extract.<kind>, see
// internal/worker/pipeline_integration_test.go). The extractor worker filters
// jobs.extract.> and routes by the envelope ContentType, so the suffix is a
// stream-filter hint, not the routing key. Kinds: plaintext / ooxml / pdf / html.
func extractSubjectSuffix(contentType string) string {
	switch contentType {
	case "application/pdf":
		return "pdf"
	case "text/html":
		return "html"
	case "text/plain":
		return "plaintext"
	default:
		// OOXML family (docx/pptx/xlsx) and anything else the registry handles.
		return "ooxml"
	}
}

// connBusAdapter adapts *worker.Conn to extractionBus, marshaling the Job.
type connBusAdapter struct{ conn *worker.Conn }

func (a *connBusAdapter) PutPayload(ctx context.Context, name string, data []byte) (string, error) {
	return a.conn.PutPayload(ctx, name, data)
}
func (a *connBusAdapter) PublishJob(ctx context.Context, subject string, job jobenvelope.Job) error {
	data, err := jobMarshal(job)
	if err != nil {
		return err
	}
	return a.conn.Publish(ctx, subject, data)
}
```

Add a tiny `jobMarshal` helper (or inline `json.Marshal`) — match the worker's serialization. If the worker uses plain `json.Marshal(job)`, use that here for symmetry.

> **Replace `extractSubjectSuffix`'s body** with the exact suffix scheme found in Step 1. The placeholder mapping above must be corrected to the real one before the test's `Contains` assertion is tightened to the exact suffix.

- [ ] **Step 5: Run the test to verify it passes**

Run: `make test-unit name=TestPublishExtractionJob_PublishesAndQueues`
Expected: PASS.

- [ ] **Step 6: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 7: Commit**

```bash
git add api/extraction_publisher.go api/extraction_publisher_test.go
git commit -m "feat(api): add ExtractionPublisher (Object Store + publish + queued row)

Refs #347."
```

---

## Task 7: The result-consumer goroutine

**Files:**
- Create: `api/result_consumer.go`
- Test: `api/result_consumer_test.go`
- Modify: `api/server.go` / `cmd/server/main.go` (start + stop)

Subscribes to `jobs.result.*` (the `TMI_RESULTS` stream) and the DLQ subject. Per message: classify, upsert `extraction_jobs`, update document `access_status`, emit a webhook event, delete blobs. Must never crash the monolith.

`RunConsumer`/`JobHandler` in `internal/worker` are typed to `jobenvelope.Job` (forward path), so the result-consumer uses its own `jetstream.Consume` loop over `jobenvelope.Result`.

- [ ] **Step 1: Write the failing test for the handler logic**

Create `api/result_consumer_test.go`. Test the pure per-result handler (no NATS) with fakes:

```go
package api

import (
	"context"
	"testing"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingJobStore struct {
	terminalJobID, terminalStatus, terminalReason string
}

func (r *recordingJobStore) MarkTerminal(ctx context.Context, jobID, status, reasonCode string) error {
	r.terminalJobID, r.terminalStatus, r.terminalReason = jobID, status, reasonCode
	return nil
}

type recordingDocUpdater struct {
	id, status, reason string
	called             bool
}

func (r *recordingDocUpdater) UpdateAccessStatusWithDiagnostics(ctx context.Context, id, accessStatus, contentSource, reasonCode, reasonDetail string) error {
	r.id, r.status, r.reason, r.called = id, accessStatus, reasonCode, true
	return nil
}

type recordingEmitter struct{ eventType, objectID string }

func (r *recordingEmitter) emit(ctx context.Context, eventType, documentID, threatModelID, ownerID string) {
	r.eventType, r.objectID = eventType, documentID
}

type recordingBlobDeleter struct{ deleted []string }

func (r *recordingBlobDeleter) DeletePayload(ctx context.Context, ref string) error {
	r.deleted = append(r.deleted, ref)
	return nil
}

func newTestResultConsumer(jobs *recordingJobStore, docs *recordingDocUpdater, em *recordingEmitter, blobs *recordingBlobDeleter) *ResultConsumer {
	return &ResultConsumer{
		jobs:    jobs,
		docs:    docs,
		emit:    em.emit,
		blobs:   blobs,
		lookupDocument: func(ctx context.Context, jobID string) (docRef, tmID, ownerID string, ok bool) {
			return "doc-7", "tm-1", "owner-1", true
		},
	}
}

func TestResultConsumer_Completed(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:  "job-7",
		Status: jobenvelope.StatusCompleted,
		Output: jobenvelope.Output{ResultRef: "TMI_PAYLOADS/job-7-result"},
	})
	require.NoError(t, err)
	assert.Equal(t, models.ExtractionStatusCompleted, jobs.terminalStatus)
	assert.True(t, docs.called)
	assert.Equal(t, AccessStatusAccessible, docs.status)
	assert.Equal(t, EventDocumentExtractionCompleted, em.eventType)
	assert.NotEmpty(t, blobs.deleted)
}

func TestResultConsumer_Failed_ClassifiesReason(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)

	err := rc.handleResult(context.Background(), jobenvelope.Result{
		JobID:      "job-8",
		Status:     jobenvelope.StatusFailed,
		ReasonCode: "extraction_limit:timeout",
	})
	require.NoError(t, err)
	assert.Equal(t, models.ExtractionStatusFailed, jobs.terminalStatus)
	assert.Equal(t, AccessStatusExtractionFailed, docs.status)
	assert.Equal(t, "extraction_limit:timeout", docs.reason)
	assert.Equal(t, EventDocumentExtractionFailed, em.eventType)
}

func TestResultConsumer_DeletedDocument_DropsGracefully(t *testing.T) {
	jobs := &recordingJobStore{}
	docs := &recordingDocUpdater{}
	em := &recordingEmitter{}
	blobs := &recordingBlobDeleter{}
	rc := newTestResultConsumer(jobs, docs, em, blobs)
	rc.lookupDocument = func(ctx context.Context, jobID string) (string, string, string, bool) {
		return "", "", "", false // document gone
	}
	err := rc.handleResult(context.Background(), jobenvelope.Result{JobID: "job-9", Status: jobenvelope.StatusCompleted})
	require.NoError(t, err) // dropped gracefully, no doc update, no panic
	assert.False(t, docs.called)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test-unit name=TestResultConsumer_Completed`
Expected: FAIL — undefined `ResultConsumer`, `EventDocumentExtractionCompleted`, etc.

- [ ] **Step 3: Add the new event constants**

In `api/events.go`, alongside the Document events:

```go
	// Document extraction outcome events (async pipeline, #347)
	EventDocumentExtractionCompleted = "document.extraction_completed"
	EventDocumentExtractionFailed    = "document.extraction_failed"
```

- [ ] **Step 4: Implement the result-consumer**

Create `api/result_consumer.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"time"

	"github.com/ericfitz/tmi/api/models"
	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/internal/worker"
	"github.com/ericfitz/tmi/pkg/jobenvelope"
	"github.com/nats-io/nats.go/jetstream"
)

// terminalMarker is the subset of ExtractionJobStore the consumer needs.
type terminalMarker interface {
	MarkTerminal(ctx context.Context, jobID, status, reasonCode string) error
}

// docAccessUpdater is the subset of DocumentRepository the consumer needs.
type docAccessUpdater interface {
	UpdateAccessStatusWithDiagnostics(ctx context.Context, id, accessStatus, contentSource, reasonCode, reasonDetail string) error
}

// blobDeleter is the subset of worker.Conn used for cleanup.
type blobDeleter interface {
	DeletePayload(ctx context.Context, ref string) error
}

// emitFunc emits a document extraction webhook event.
type emitFunc func(ctx context.Context, eventType, documentID, threatModelID, ownerID string)

// ResultConsumer subscribes to jobs.result.* (+ DLQ) and persists outcomes.
type ResultConsumer struct {
	conn  *worker.Conn
	jobs  terminalMarker
	docs  docAccessUpdater
	blobs blobDeleter
	emit  emitFunc

	// lookupDocument resolves a job_id to its document and ownership context
	// (reads the extraction_jobs row's document_ref, then the document).
	// Returns ok=false when the document no longer exists.
	lookupDocument func(ctx context.Context, jobID string) (docRef, threatModelID, ownerID string, ok bool)

	cancel context.CancelFunc
}

// handleResult applies one result envelope. It never returns an error for a
// business outcome (failed extraction is a normal result); it returns an
// error only for an infrastructure failure that warrants redelivery.
func (rc *ResultConsumer) handleResult(ctx context.Context, res jobenvelope.Result) error {
	logger := slogging.Get()

	status := models.ExtractionStatusCompleted
	accessStatus := AccessStatusAccessible
	eventType := EventDocumentExtractionCompleted
	reasonCode, reasonDetail := "", ""

	if res.Status == jobenvelope.StatusFailed {
		status = models.ExtractionStatusFailed
		accessStatus = AccessStatusExtractionFailed
		eventType = EventDocumentExtractionFailed
		reasonCode, reasonDetail = res.ReasonCode, res.ReasonDetail
	}

	if err := rc.jobs.MarkTerminal(ctx, res.JobID, status, reasonCode); err != nil {
		return err // transient → redeliver
	}

	docRef, tmID, ownerID, ok := rc.lookupDocument(ctx, res.JobID)
	if !ok {
		logger.Warn("result-consumer: document for job %s no longer exists; dropping", res.JobID)
	} else {
		if err := rc.docs.UpdateAccessStatusWithDiagnostics(ctx, docRef, accessStatus, "", reasonCode, reasonDetail); err != nil {
			return err // transient → redeliver
		}
		if rc.emit != nil {
			rc.emit(ctx, eventType, docRef, tmID, ownerID)
		}
	}

	// Best-effort blob cleanup — never block the ack on it.
	if rc.blobs != nil && res.Output.ResultRef != "" {
		if err := rc.blobs.DeletePayload(ctx, res.Output.ResultRef); err != nil {
			logger.Warn("result-consumer: blob cleanup for job %s failed: %v", res.JobID, err)
		}
	}
	return nil
}

// Start launches the consume loop in a goroutine over the TMI_RESULTS stream
// filtered to jobs.result.* and the DLQ. It recovers from per-message panics.
func (rc *ResultConsumer) Start(ctx context.Context) error {
	ctx, rc.cancel = context.WithCancel(ctx)
	js := rc.conn.JetStream()

	stream, err := js.Stream(ctx, worker.ResultStream)
	if err != nil {
		// The result stream is created by the controller / first worker; if it
		// is absent the async path is not live yet — log and return.
		slogging.Get().Warn("result-consumer: result stream %s not found: %v", worker.ResultStream, err)
		return nil
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:        "monolith-result-consumer",
		FilterSubjects: []string{worker.SubjectResultPrefix + ">", worker.SubjectDLQ},
		AckPolicy:      jetstream.AckExplicitPolicy,
		AckWait:        30 * time.Second,
		MaxDeliver:     5,
	})
	if err != nil {
		return err
	}

	cc, err := cons.Consume(func(msg jetstream.Msg) {
		defer func() {
			if r := recover(); r != nil {
				slogging.Get().Error("result-consumer: panic on %s: %v", msg.Subject(), r)
				_ = msg.Term() // a message that panics us cannot be retried safely
			}
		}()
		var res jobenvelope.Result
		if err := json.Unmarshal(msg.Data(), &res); err != nil {
			slogging.Get().Error("result-consumer: undecodable result on %s: %v", msg.Subject(), err)
			_ = msg.Term()
			return
		}
		if err := rc.handleResult(context.Background(), res); err != nil {
			slogging.Get().Warn("result-consumer: transient failure job=%s, redelivering: %v", res.JobID, err)
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	})
	if err != nil {
		return err
	}

	go func() {
		<-ctx.Done()
		cc.Stop()
		slogging.Get().Info("result-consumer: stopped")
	}()
	return nil
}

// Stop cancels the consume loop.
func (rc *ResultConsumer) Stop() {
	if rc.cancel != nil {
		rc.cancel()
	}
}
```

> **DLQ note:** a dead-lettered message arriving on `worker.SubjectDLQ` is the original *Job* envelope, not a *Result*. If `json.Unmarshal` into `Result` yields an empty `Status`, treat it as a failed extraction with `ReasonCode = "extraction_failed"`. Adjust `handleResult`/the decode path to detect the DLQ subject (`msg.Subject() == worker.SubjectDLQ`) and synthesize a failed `Result{JobID: <from Job>, Status: StatusFailed, ReasonCode: "extraction_failed"}`. Add a test `TestResultConsumer_DLQ_TreatedAsFailed` mirroring the failed-case test but constructing the message on the DLQ subject. Implement this in the same task.

- [ ] **Step 5: Run the handler tests to verify they pass**

Run: `make test-unit name=TestResultConsumer_Completed` (then the others by name).
Expected: PASS.

- [ ] **Step 6: Wire Start/Stop into the server lifecycle**

In `cmd/server/main.go`, after the NATS conn is set (Task 5) and the job store + document repository are available, construct and start the consumer:

```go
if extractionConn != nil {
	rc := api.NewResultConsumer(extractionConn, jobStore, api.GlobalDocumentRepository)
	if err := rc.Start(ctx); err != nil {
		logger.Warn("result-consumer failed to start: %v", err)
	} else {
		apiServer.SetResultConsumer(rc)
	}
}
```

Add a `NewResultConsumer(conn, jobStore, docs)` constructor that wires `emit` to `GlobalEventEmitter.EmitEvent` (building the `EventPayload`) and `lookupDocument` to a real query (read the `extraction_jobs.document_ref` for the job, then the document's threat-model + owner). Add `Server.SetResultConsumer` and call `rc.Stop()` in the shutdown path (alongside `extractionConn.Close()`, before closing the conn).

- [ ] **Step 7: Build + lint**

Run: `make build-server && make lint`
Expected: both succeed.

- [ ] **Step 8: Commit**

```bash
git add api/result_consumer.go api/result_consumer_test.go api/events.go api/server.go cmd/server/main.go
git commit -m "feat(api): add result-consumer goroutine for async extraction outcomes

Subscribes jobs.result.* + DLQ; classifies, upserts extraction_jobs, updates
document access_status, emits document.extraction_* webhooks, deletes blobs.
Refs #347."
```

---

## Task 8: Flag-gated routing in the two callers

**Files:**
- Modify: `api/access_poller.go`
- Modify: `api/document_sub_resource_handlers.go`
- Modify: `api/server.go` (a helper to read the flag + availability)
- Test: `api/access_poller_test.go`, `api/document_sub_resource_handlers_test.go`

Both callers consult `extraction.async_enabled` AND `AsyncExtractionAvailable()`. When both true → publish; else inline (today's behavior).

- [ ] **Step 1: Add a flag-resolution helper on Server**

In `api/server.go`:

```go
// useAsyncExtraction reports whether extraction should route through the
// worker pipeline: the setting is on AND a NATS connection is available.
// When the setting is on but NATS is absent, it logs once and returns false
// (fail-safe to inline) so extractions are never silently dropped.
func (s *Server) useAsyncExtraction(ctx context.Context) bool {
	if s.settingsService == nil || !s.AsyncExtractionAvailable() {
		return false
	}
	on, err := s.settingsService.GetBool(ctx, "extraction.async_enabled")
	if err != nil {
		slogging.Get().Warn("extraction.async_enabled read failed, using inline: %v", err)
		return false
	}
	if on && !s.AsyncExtractionAvailable() {
		slogging.Get().Warn("extraction.async_enabled is on but NATS is unavailable; using inline")
		return false
	}
	return on
}
```

> The access_poller is a standalone struct, not the Server. Give the poller its own `asyncDecider func(ctx) bool` field set to `server.useAsyncExtraction` at wiring time, plus a `publisher *ExtractionPublisher`. The request handler is on `DocumentSubResourceHandler`; give it the same two collaborators.

- [ ] **Step 2: Write the failing test — request path returns 202 when async**

Add to `api/document_sub_resource_handlers_test.go` a test that, with `asyncDecider` returning true and a fake publisher returning `job-x`, asserts the handler responds `202` with a body containing the `job_id`, and (with the decider false) still returns the existing `201` inline path. Mirror the file's existing handler-test harness (gin test context, fake stores).

```go
func TestCreateDocument_AsyncReturns202(t *testing.T) {
	// Arrange a DocumentSubResourceHandler with asyncDecider -> true and a
	// fake ExtractionPublisher returning "job-x". Build a gin context with a
	// valid create-document body whose URI is an extractable source.
	// Assert: w.Code == http.StatusAccepted and the JSON body has job_id == "job-x".
	t.Skip("fill in using the existing CreateDocument test harness in this file")
}
```

> Replace the `t.Skip` with a real test built on this file's existing `CreateDocument` test scaffolding (find it with `rg -n "func Test.*CreateDocument" api/document_sub_resource_handlers_test.go`). The plan requires a concrete assertion on `http.StatusAccepted` and the `job_id` body field.

- [ ] **Step 3: Run the test to verify it fails**

Run: `make test-unit name=TestCreateDocument_AsyncReturns202`
Expected: FAIL.

- [ ] **Step 4: Implement the request-path branch**

In `api/document_sub_resource_handlers.go` `CreateDocument`, where it currently determines access/extraction (around the `contentPipeline`/`AccessValidator` block, lines ~459–527) and returns `http.StatusCreated`, add — guarded by the async decision — a branch that:

1. fetches the source bytes via the existing fetch path (the same bytes that would be extracted inline),
2. calls `h.publisher.Publish(ctx, ExtractionRequest{DocumentID: document.Id, ContentType: <detected>, Bytes: <fetched>})`,
3. sets the document `access_status` to `pending_access`,
4. returns `c.JSON(http.StatusAccepted, gin.H{"document": document, "job_id": jobID})`.

When the decider is false, leave the existing `http.StatusCreated` path untouched.

> Determine from the existing handler exactly where the source bytes are available (or whether fetching happens in the pipeline). If the request path does not currently fetch bytes itself (it may defer to the access poller), the async request-path branch may instead just publish a "fetch+extract" intent — BUT per the spec the fetch stays in the monolith. Reconcile by fetching in the handler before publishing. Confirm against `content_pipeline.go` `Extract` to see where the fetch occurs and reuse that fetch step.

- [ ] **Step 5: Run the test to verify it passes**

Run: `make test-unit name=TestCreateDocument_AsyncReturns202`
Expected: PASS.

- [ ] **Step 6: Write + satisfy the access_poller async test**

Add `TestAccessPoller_AsyncPublishesInsteadOfInline` to `api/access_poller_test.go`: with `asyncDecider -> true` and a fake publisher, assert `pollOnce` publishes (publisher called) and does NOT call the inline pipeline, and leaves `access_status` as `pending_access` (the result-consumer finishes it). Then implement the branch in `access_poller.go` at the current inline seam (`pollOnce`, around line 153–176): if `p.asyncDecider(ctx)` and `p.publisher != nil`, fetch bytes + `p.publisher.Publish(...)` + continue; else today's inline `ExtractForDocument` path.

- [ ] **Step 7: Run all touched tests + build + lint**

Run: `make test-unit name=TestAccessPoller_AsyncPublishesInsteadOfInline` then `make build-server && make lint`
Expected: all pass.

- [ ] **Step 8: Wire the collaborators in `cmd/server/main.go`**

Where the publisher/job-store/consumer were wired (Tasks 5–7), also: build `publisher := api.NewExtractionPublisher(extractionConn, jobStore)`; call `apiServer.SetExtractionPublisher(publisher)` (add setter) which propagates it + the `useAsyncExtraction` decider into both the `DocumentSubResourceHandler` and the `AccessPoller`. Add the small setters on `Server` and the handler/poller. Guard everything on `extractionConn != nil`.

- [ ] **Step 9: Commit**

```bash
git add api/access_poller.go api/document_sub_resource_handlers.go api/server.go api/access_poller_test.go api/document_sub_resource_handlers_test.go cmd/server/main.go
git commit -m "feat(api): flag-gated async extraction routing in request path + access poller

Returns 202 Accepted on the request path when extraction.async_enabled and
NATS are available; falls back to inline otherwise. Refs #347."
```

---

## Task 9: OpenAPI — 202 response + webhook event enum

**Files:**
- Modify: `api-schema/tmi-openapi.json`
- Regenerate: `api/api.go` (via `make generate-api`)

- [ ] **Step 1: Back up the spec (large JSON)**

Run:
```bash
cp api-schema/tmi-openapi.json api-schema/tmi-openapi.json.bak
stat -f%z api-schema/tmi-openapi.json
```

- [ ] **Step 2: Add the 202 response to the create-document operation**

Find the operation for the document-create endpoint (the one `CreateDocument` serves). Add a `"202"` response describing an accepted async extraction with a body schema `{ document, job_id }`. Keep the existing `"201"`. Preserve the operation's `x-tmi-authz` annotation. Use `jq` for the edit (file is large):

```bash
# Identify the path+method, then add the 202 response object via jq, then:
jq empty api-schema/tmi-openapi.json && echo Valid
```

- [ ] **Step 3: Add the two webhook event types to the event enum**

Find the webhook event-type enum (the list containing `document.created`, `document.updated`, ...) and add `document.extraction_completed` and `document.extraction_failed`. Validate with `jq empty`.

- [ ] **Step 4: Validate the spec**

Run: `make validate-openapi`
Expected: passes (fix any OWASP/schema findings).

- [ ] **Step 5: Regenerate API code**

Run: `make generate-api`
Expected: `api/api.go` regenerates. Then `make build-server`.

- [ ] **Step 6: Delete the backup, run unit tests**

Run:
```bash
rm api-schema/tmi-openapi.json.bak
make test-unit
```
Expected: tests pass.

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): OpenAPI 202 response + document.extraction_* webhook events

Refs #347."
```

---

## Task 10: Full integration test (process-mode round-trip)

**Files:**
- Create: `test/integration/workflows/extraction_async_integration_test.go`

Requires NATS + both workers (Task 0). Names use the `_Integration` suffix so `make test-integration` picks them up.

- [ ] **Step 1: Write the integration test**

```go
//go:build integration

package workflows

// TestExtractionAsyncRoundTrip_Integration submits a small extractable
// document through the async path and asserts the monolith persists the
// terminal outcome.
func TestExtractionAsyncRoundTrip_Integration(t *testing.T) {
	// 1. Connect to NATS (TMI_NATS_URL) and the test DB (AutoMigrate ExtractionJob).
	// 2. Build an ExtractionPublisher + ExtractionJobStore against them.
	// 3. Start a ResultConsumer against the same conn + a real DocumentStore.
	// 4. Insert a document row; publish an extraction job for a tiny text/plain payload.
	// 5. Poll the extraction_jobs row (or the document access_status) until status is terminal or a timeout (e.g. 30s).
	// 6. Assert status == completed and the document access_status == accessible.
}
```

Fill in using the harness `internal/worker/pipeline_integration_test.go` already establishes for NATS + the project's integration DB setup. Also add `TestExtractionAsyncIdempotent_Integration` (publish the same job_id twice → exactly one terminal row) and `TestExtractionAsyncDLQ_Integration` (publish a job no worker will complete / force a worker kill → DLQ → `failed`), if the worker harness supports inducing those conditions; otherwise note the gap explicitly in the test file and defer DLQ to Plan 4's cluster tests.

- [ ] **Step 2: Run the integration test**

Run: `make test-integration name=TestExtractionAsyncRoundTrip_Integration`
Expected: PASS (workers must be running per Task 0).

- [ ] **Step 3: Commit**

```bash
git add test/integration/workflows/extraction_async_integration_test.go
git commit -m "test(platform): async extraction round-trip integration test

Refs #347."
```

---

## Task 11: Oracle review + final verification

**Files:** none (review + gate)

- [ ] **Step 1: Dispatch the oracle-db-admin subagent**

Invoke the `oracle-db-admin` skill against the DB-touching changes: `api/models/extraction_job.go`, `api/extraction_job_store.go` (the `clause.OnConflict` insert + upsert), and the `AllModels()` registration. Specifically ask it to:
- Confirm `oracle-samples/gorm-oracle` emits a correct `MERGE` for `clause.OnConflict` with `DoNothing` and with `DoUpdates`. **If it does not, replace `MarkTerminal`/`InsertQueued` with an explicit `SELECT ... FOR UPDATE` + INSERT/UPDATE transaction** (the documented fallback in the spec) and re-run Task 3's tests.
- Review nullable `completed_at` / `reason_code` handling on Oracle (empty-string vs NULL CLOB pitfalls — the project has prior issues here, e.g. #425).
- Confirm the no-FK indexed `document_ref` and the `idx_extraction_jobs_doc` index are acceptable.

Address every BLOCKING finding before proceeding; fold APPROVED-WITH-NOTES items in or file follow-ups.

- [ ] **Step 2: Run the security-regression skill**

The change touches a handler returning a new response and the result-consumer path. Run the `security-regression` skill to confirm no previously-fixed vuln is reintroduced (verbose-error 500s, etc.).

- [ ] **Step 3: Full gate**

Run:
```bash
make lint
make build-server
make test-unit
make test-integration
```
Expected: all pass.

- [ ] **Step 4: Update the issue + spec status**

Comment on #347 summarizing Plan 3 completion and that the cluster acceptance suite + kind migration remain as Plan 4. Do NOT close #347 (Plan 4 remains). Update the spec's status line if desired.

- [ ] **Step 5: Final commit (docs/status only, if any)**

```bash
git add -A
git commit -m "docs(platform): mark #347 Plan 3 complete; Plan 4 (cluster acceptance) pending

Refs #347."
```

---

## Notes for the executing engineer

- **Do not push.** The SSH key is touch-gated; the user pushes. Land all commits on `dev/1.4.0`.
- **`extractSubjectSuffix` is already correct** (mirrors `jobs.extract.<kind>` from `internal/worker/pipeline_integration_test.go`); tighten the Task 6 test's `Contains` to the exact subject once you confirm the kind for your test's content type.
- **Test bodies you MUST flesh out** (they are scaffolding, not final): the `t.Skip`/comment-only bodies in Task 8 (`TestCreateDocument_AsyncReturns202`) and Task 10 — build them on the existing harnesses named in those steps (`rg -n "func Test.*CreateDocument" api/document_sub_resource_handlers_test.go` and `internal/worker/pipeline_integration_test.go`).
- **`internal/worker.RunConsumer` cannot be reused for results** — it is typed to `jobenvelope.Job`. The result-consumer's own `jetstream.Consume` loop over `jobenvelope.Result` (Task 7) is intentional.
- **Verify custom DB type names** (`DBVarchar`, `NullableDBVarchar`) against `api/models/models.go` before writing the model — do not invent types.
- **Confirm the dialect/column helper** (`Col`/`ColumnName`) usage from `api/user_api_quota_store_gorm.go` and apply it consistently in `extraction_job_store.go` for Oracle identifier correctness.
```
