# OOXML Extractors (DOCX/PPTX/XLSX) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement three pure-Go `ContentExtractor` plugins for OOXML formats (DOCX, PPTX, XLSX) that produce Markdown-flavored text suitable for downstream chunking, with shared bounded-zip + bounded-XML scaffolding, configurable limits, per-user concurrency, and pipeline-level deadline enforcement.

**Architecture:** Three new extractors live in `api/content_extractor_{docx,pptx,xlsx}.go`, sharing scaffolding from `api/content_extractor_ooxml_common.go` (bounded zip opener, depth-limited XML decoder, typed limit-error hierarchy, deadline wrapper, per-user `golang.org/x/sync/semaphore` limiter). `ContentPipeline.Extract` is updated to acquire the per-user semaphore and run any extractor implementing `BoundedExtractor` under a wall-clock deadline, classifying typed errors into `documents.access_status = "extraction_failed"` rows with stable `access_reason_code` values. Limits are server-side hardcoded ceilings + operator-tunable defaults via `internal/config` (env-var `TMI_CONTENT_EXTRACTORS_*` overrides). A new nullable `extraction_concurrency_override` column on `users` lets trusted machine accounts run higher concurrency.

**Tech Stack:** Go 1.x, `archive/zip`, `encoding/xml`, `github.com/xuri/excelize/v2` (XLSX only), `golang.org/x/sync/semaphore`, GORM AutoMigrate, Gin, testify, existing `ContentExtractor`/`ContentPipeline` infrastructure.

---

## Pre-flight notes for the implementing engineer

Three deviations from the design spec at `docs/superpowers/specs/2026-04-29-ooxml-extractors-design.md`. Follow this plan, not the spec, where they differ:

1. **Migration mechanism.** The spec mentions `auth/migrations/NNNN_*.sql` files. That directory does **not exist** in this codebase. Schema changes are made by adding fields to the GORM model in `api/models/models.go` and updating the expected schema in `internal/dbschema/schema.go`. AutoMigrate runs at server startup (`cmd/server/main.go::runMigrations`).

2. **Failure-reason column.** The spec references `documents.extraction_failure_reason`. That column does **not exist**. The actual columns are `access_status`, `access_reason_code`, `access_reason_detail`, `access_status_updated_at` — all populated via `DocumentStore.UpdateAccessStatusWithDiagnostics(ctx, id, status, contentSource, reasonCode, reasonDetail)`. Use that method.

3. **`access_status` enum.** Today's enum (in OpenAPI + `api/content_pipeline.go` constants) is `unknown | accessible | pending_access | auth_required`. We add a new value `extraction_failed`. This requires updating the OpenAPI spec, regenerating, adding a Go constant, and updating the GORM column comment if any.

User IDs are stored as **strings** (UUID-shaped), not `uuid.UUID`. The semaphore map keys on `string`.

The plan keeps each task small. Subagents executing under `superpowers:subagent-driven-development` should commit at the end of every task. Each task lists exact file paths, the make targets to run, and the expected output of every test.

Working branch: `dev/1.4.0`. Do **not** create a separate feature branch unless directed; the issue policy is to land on `dev/1.4.0`.

---

## File structure

**New files (api/):**
- `api/content_extractor_ooxml_common.go` — bounded zip + bounded XML + typed errors + deadline wrapper + concurrency limiter + markdown builder + cell utilities
- `api/content_extractor_ooxml_common_test.go` — unit tests for the common scaffolding
- `api/content_extractor_docx.go` — DOCX extractor
- `api/content_extractor_docx_test.go`
- `api/content_extractor_pptx.go` — PPTX extractor
- `api/content_extractor_pptx_test.go`
- `api/content_extractor_xlsx.go` — XLSX extractor
- `api/content_extractor_xlsx_test.go`
- `api/ooxml_extractors_integration_test.go` — pipeline + concurrency end-to-end
- `api/ooxml_extractors_corpus_test.go` — `//go:build corpus` real-file fixtures
- `testdata/ooxml/` — inline-built test inputs (kept tiny, hand-checked) and corpus seed
- `testdata/ooxml-corpus/` — real-document corpus + `.expected.md` siblings (build-tagged)

**Modified files:**
- `api/content_extractor.go` — add `BoundedExtractor` marker interface
- `api/content_pipeline.go` — pipeline gains concurrency limiter + deadline wrapper + classification of typed errors; add `AccessStatusExtractionFailed` constant
- `api/access_diagnostics.go` — add stable extraction-failure reason-code constants (`ReasonExtractionLimit*`, `ReasonExtractionMalformed`, `ReasonExtractionUnsupported`, `ReasonExtractionInternal`)
- `api/models/models.go` — add `User.ExtractionConcurrencyOverride *int`
- `api/document_store_gorm.go` — extend the `access_status` valid values comment if any (no behavior change otherwise)
- `internal/dbschema/schema.go` — add `extraction_concurrency_override` column to the `users` table schema and `idx_users_extraction_override` index if we choose to index it (we don't — it's only ever looked up for the row owner)
- `internal/config/config.go` — wire `ContentExtractors` field into top-level `Config`
- `internal/config/content_extractors.go` (NEW) — `ContentExtractorsConfig` struct + `Validate`
- `config-development.yml` — add `content_extractors:` section with defaults
- `config-example.yml`, `config-production.yml` — same defaults
- `api-schema/tmi-openapi.json` — add `extraction_failed` to the `access_status` enum
- `api/api.go` — regenerated by `make generate-api` after the OpenAPI change
- `cmd/server/main.go` — register the three extractors; build the concurrency limiter using user-store override lookup; pass it + config to `ContentPipeline`
- `go.mod`, `go.sum` — add `github.com/xuri/excelize/v2` and `golang.org/x/sync/semaphore`

**Wiki docs (post-implementation, manual):**
- New page `Operator › Content Extractors — Limits and Overrides` covering env vars, defaults, ceilings, and the admin user-override workflow. Wiki is hand-edited per `docs/migrated/...` policy in `CLAUDE.md`. **No file under `docs/` is created.**

---

## Task 1: Add Go module dependencies

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add excelize and semaphore**

```bash
go get github.com/xuri/excelize/v2@latest
go get golang.org/x/sync/semaphore@latest
go mod tidy
```

- [ ] **Step 2: Verify CGO_ENABLED=0 build still works**

Run: `CGO_ENABLED=0 go build ./...`
Expected: clean build, no errors.

- [ ] **Step 3: Verify excelize is pure-Go**

Run: `grep -r "C\." $(go env GOMODCACHE)/github.com/xuri/excelize/v2@*/*.go 2>/dev/null | head -1 || echo "no cgo"`
Expected: `no cgo`. If excelize has any cgo, **stop and report** — we must not regress `CGO_ENABLED=0`.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add excelize/v2 and x/sync/semaphore for OOXML extractors"
```

---

## Task 2: Add `ContentExtractorsConfig` struct and validation

**Files:**
- Create: `internal/config/content_extractors.go`
- Modify: `internal/config/config.go` (add field to top-level `Config`)
- Test: `internal/config/content_extractors_test.go`

- [ ] **Step 1: Write failing test**

```go
package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestContentExtractorsConfig_Defaults(t *testing.T) {
	c := DefaultContentExtractorsConfig()
	assert.EqualValues(t, 20*1024*1024, c.CompressedSizeBytes)
	assert.EqualValues(t, 50*1024*1024, c.DecompressedSizeBytes)
	assert.EqualValues(t, 20*1024*1024, c.PartSizeBytes)
	assert.Equal(t, 100, c.PPTXSlides)
	assert.Equal(t, 1000, c.XLSXCells)
	assert.EqualValues(t, 128*1024, c.MarkdownSizeBytes)
	assert.Equal(t, 30*time.Second, c.WallClockBudget)
	assert.Equal(t, 2, c.PerUserConcurrencyDefault)
}

func TestContentExtractorsConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*ContentExtractorsConfig)
		wantErr string
	}{
		{"valid defaults", func(c *ContentExtractorsConfig) {}, ""},
		{"compressed > ceiling", func(c *ContentExtractorsConfig) { c.CompressedSizeBytes = 60 * 1024 * 1024 }, "compressed_size_bytes"},
		{"compressed zero", func(c *ContentExtractorsConfig) { c.CompressedSizeBytes = 0 }, "compressed_size_bytes"},
		{"decompressed > ceiling", func(c *ContentExtractorsConfig) { c.DecompressedSizeBytes = 200 * 1024 * 1024 }, "decompressed_size_bytes"},
		{"part > ceiling", func(c *ContentExtractorsConfig) { c.PartSizeBytes = 60 * 1024 * 1024 }, "part_size_bytes"},
		{"slides > ceiling", func(c *ContentExtractorsConfig) { c.PPTXSlides = 251 }, "pptx_slides"},
		{"cells > ceiling", func(c *ContentExtractorsConfig) { c.XLSXCells = 10001 }, "xlsx_cells"},
		{"markdown > ceiling", func(c *ContentExtractorsConfig) { c.MarkdownSizeBytes = 257 * 1024 }, "markdown_size_bytes"},
		{"wall clock > ceiling", func(c *ContentExtractorsConfig) { c.WallClockBudget = 61 * time.Second }, "wall_clock_budget"},
		{"per-user > ceiling", func(c *ContentExtractorsConfig) { c.PerUserConcurrencyDefault = 17 }, "per_user_concurrency_default"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultContentExtractorsConfig()
			tt.mutate(&c)
			err := c.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

Run: `go test ./internal/config/ -run TestContentExtractorsConfig -v`
Expected: FAIL with "undefined: DefaultContentExtractorsConfig" / "undefined: ContentExtractorsConfig".

- [ ] **Step 3: Implement struct**

```go
package config

import (
	"fmt"
	"time"
)

// Hardcoded ceilings — server-only protection. Operators cannot override.
const (
	maxCompressedSizeBytes      = int64(50 * 1024 * 1024)
	maxDecompressedSizeBytes    = int64(100 * 1024 * 1024)
	maxPartSizeBytes            = int64(50 * 1024 * 1024)
	maxPPTXSlides               = 250
	maxXLSXCells                = 10000
	maxMarkdownSizeBytes        = int64(256 * 1024)
	maxWallClockBudget          = 60 * time.Second
	maxPerUserConcurrency       = 16
)

// ContentExtractorsConfig holds operator-tunable defaults for the OOXML
// extractor pipeline. Each value must be > 0 and <= the corresponding
// hardcoded ceiling.
type ContentExtractorsConfig struct {
	CompressedSizeBytes       int64         `yaml:"compressed_size_bytes" env:"TMI_CONTENT_EXTRACTORS_COMPRESSED_SIZE_BYTES"`
	DecompressedSizeBytes     int64         `yaml:"decompressed_size_bytes" env:"TMI_CONTENT_EXTRACTORS_DECOMPRESSED_SIZE_BYTES"`
	PartSizeBytes             int64         `yaml:"part_size_bytes" env:"TMI_CONTENT_EXTRACTORS_PART_SIZE_BYTES"`
	PPTXSlides                int           `yaml:"pptx_slides" env:"TMI_CONTENT_EXTRACTORS_PPTX_SLIDES"`
	XLSXCells                 int           `yaml:"xlsx_cells" env:"TMI_CONTENT_EXTRACTORS_XLSX_CELLS"`
	MarkdownSizeBytes         int64         `yaml:"markdown_size_bytes" env:"TMI_CONTENT_EXTRACTORS_MARKDOWN_SIZE_BYTES"`
	WallClockBudget           time.Duration `yaml:"wall_clock_budget" env:"TMI_CONTENT_EXTRACTORS_WALL_CLOCK_BUDGET"`
	PerUserConcurrencyDefault int           `yaml:"per_user_concurrency_default" env:"TMI_CONTENT_EXTRACTORS_PER_USER_CONCURRENCY_DEFAULT"`
}

// DefaultContentExtractorsConfig returns the project-wide defaults documented
// in the OOXML design spec.
func DefaultContentExtractorsConfig() ContentExtractorsConfig {
	return ContentExtractorsConfig{
		CompressedSizeBytes:       20 * 1024 * 1024,
		DecompressedSizeBytes:     50 * 1024 * 1024,
		PartSizeBytes:             20 * 1024 * 1024,
		PPTXSlides:                100,
		XLSXCells:                 1000,
		MarkdownSizeBytes:         128 * 1024,
		WallClockBudget:           30 * time.Second,
		PerUserConcurrencyDefault: 2,
	}
}

// Validate enforces > 0 and <= ceiling for every field.
func (c ContentExtractorsConfig) Validate() error {
	check := func(name string, v, ceiling int64) error {
		if v <= 0 {
			return fmt.Errorf("content_extractors.%s must be > 0 (got %d)", name, v)
		}
		if v > ceiling {
			return fmt.Errorf("content_extractors.%s must be <= %d (got %d)", name, ceiling, v)
		}
		return nil
	}
	if err := check("compressed_size_bytes", c.CompressedSizeBytes, maxCompressedSizeBytes); err != nil {
		return err
	}
	if err := check("decompressed_size_bytes", c.DecompressedSizeBytes, maxDecompressedSizeBytes); err != nil {
		return err
	}
	if err := check("part_size_bytes", c.PartSizeBytes, maxPartSizeBytes); err != nil {
		return err
	}
	if err := check("pptx_slides", int64(c.PPTXSlides), int64(maxPPTXSlides)); err != nil {
		return err
	}
	if err := check("xlsx_cells", int64(c.XLSXCells), int64(maxXLSXCells)); err != nil {
		return err
	}
	if err := check("markdown_size_bytes", c.MarkdownSizeBytes, maxMarkdownSizeBytes); err != nil {
		return err
	}
	if c.WallClockBudget <= 0 {
		return fmt.Errorf("content_extractors.wall_clock_budget must be > 0 (got %s)", c.WallClockBudget)
	}
	if c.WallClockBudget > maxWallClockBudget {
		return fmt.Errorf("content_extractors.wall_clock_budget must be <= %s (got %s)", maxWallClockBudget, c.WallClockBudget)
	}
	if err := check("per_user_concurrency_default", int64(c.PerUserConcurrencyDefault), int64(maxPerUserConcurrency)); err != nil {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Wire into top-level `Config`**

Edit `internal/config/config.go`. Add a field to the `Config` struct (alphabetical-ish among the existing content_* siblings):

```go
ContentExtractors         ContentExtractorsConfig `yaml:"content_extractors"`
```

- [ ] **Step 5: Run test to confirm it passes**

Run: `go test ./internal/config/ -run TestContentExtractorsConfig -v`
Expected: PASS for all sub-tests.

- [ ] **Step 6: Commit**

```bash
git add internal/config/content_extractors.go internal/config/content_extractors_test.go internal/config/config.go
git commit -m "feat(config): add ContentExtractorsConfig with validated limits"
```

---

## Task 3: Wire `content_extractors` defaults into config files

**Files:**
- Modify: `config-development.yml`
- Modify: `config-example.yml`
- Modify: `config-production.yml`

- [ ] **Step 1: Add a `content_extractors:` block at the same indent level as the existing `content_sources:` block**

For each file, append (or insert near the existing `content_*` blocks):

```yaml
content_extractors:
  compressed_size_bytes: 20971520        # 20 MB (ceiling 50 MB)
  decompressed_size_bytes: 52428800      # 50 MB (ceiling 100 MB)
  part_size_bytes: 20971520              # 20 MB (ceiling 50 MB)
  pptx_slides: 100                       # ceiling 250
  xlsx_cells: 1000                       # ceiling 10,000
  markdown_size_bytes: 131072            # 128 KB (ceiling 256 KB)
  wall_clock_budget: 30s                 # ceiling 60s
  per_user_concurrency_default: 2        # ceiling 16
```

- [ ] **Step 2: Verify config loads**

Run: `go run ./cmd/dbtool config validate --config config-development.yml 2>&1 | tail -20` (use whichever validation command exists; if not present, run `go build ./cmd/server && ./bin/tmiserver --config config-development.yml --validate-config-only` if such a flag exists, else skip — this is exercised by Task 13 startup test). Expected: no validation errors.

- [ ] **Step 3: Commit**

```bash
git add config-development.yml config-example.yml config-production.yml
git commit -m "feat(config): default content_extractors limits in config files"
```

---

## Task 4: Add user override field + migration via GORM AutoMigrate

**Files:**
- Modify: `api/models/models.go` (extend `User` struct)
- Modify: `internal/dbschema/schema.go` (extend `users` table expected schema)
- Test: `api/models/user_extraction_override_test.go`

- [ ] **Step 1: Write failing test**

```go
package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUser_ExtractionConcurrencyOverride_Nullable(t *testing.T) {
	u := User{}
	assert.Nil(t, u.ExtractionConcurrencyOverride, "default must be nil (no override)")

	v := 8
	u.ExtractionConcurrencyOverride = &v
	if u.ExtractionConcurrencyOverride == nil {
		t.Fatalf("override unexpectedly nil after assignment")
	}
	assert.Equal(t, 8, *u.ExtractionConcurrencyOverride)
}
```

- [ ] **Step 2: Run test to confirm it fails**

Run: `go test ./api/models/ -run TestUser_ExtractionConcurrencyOverride_Nullable -v`
Expected: FAIL with "u.ExtractionConcurrencyOverride undefined".

- [ ] **Step 3: Add field to `User`**

In `api/models/models.go`, inside the `User` struct (just before the closing `}` at line ~45), add:

```go
	// ExtractionConcurrencyOverride lets a trusted machine account run more
	// concurrent OOXML extractions than the operator default. NULL = use
	// default. Hard-capped at maxPerUserConcurrency (16) regardless of value.
	ExtractionConcurrencyOverride *int `gorm:""`
```

- [ ] **Step 4: Update expected schema**

In `internal/dbschema/schema.go`, find the `users` `TableSchema` and append a `ColumnSchema` to its `Columns` slice:

```go
{Name: "extraction_concurrency_override", DataType: "integer", IsNullable: true},
```

- [ ] **Step 5: Run test to confirm it passes**

Run: `go test ./api/models/ -run TestUser_ExtractionConcurrencyOverride_Nullable -v`
Expected: PASS.

- [ ] **Step 6: Run schema validator tests**

Run: `go test ./internal/dbschema/...`
Expected: PASS. If a fixture-comparison test fails, it likely means the live test database has stale schema — let AutoMigrate recreate it via the integration suite later. Note any dbschema test failure here for Task 5.

- [ ] **Step 7: Dispatch the `oracle-db-admin` skill / subagent**

Per `CLAUDE.md`, any DB-touching change must be reviewed before completion. Invoke the `oracle-db-admin` skill and ask the subagent to review:
- the new `users.extraction_concurrency_override INTEGER NULL` column
- AutoMigrate-only schema change (no manual DDL)
- Oracle nullable-INTEGER + GORM `*int` tag-only column compatibility

Address any BLOCKING findings before continuing. If APPROVED WITH NOTES, fold easy fixes in now and file follow-up issues for the rest. Note the verdict for the end-of-task summary.

- [ ] **Step 8: Commit**

```bash
git add api/models/models.go api/models/user_extraction_override_test.go internal/dbschema/schema.go
git commit -m "feat(models): add User.ExtractionConcurrencyOverride for OOXML extractors"
```

---

## Task 5: Add `BoundedExtractor` marker interface

**Files:**
- Modify: `api/content_extractor.go`
- Test: `api/content_extractor_test.go` (add a sub-test, do not break existing tests)

- [ ] **Step 1: Write failing test**

Add at the end of `api/content_extractor_test.go`:

```go
func TestBoundedExtractor_TypeAssert(t *testing.T) {
	type boundedDummy struct{}
	// Must satisfy ContentExtractor too — the registry is the typing surface.
	type fakeBounded struct{}
	// noop methods
	_ = boundedDummy{}
	_ = fakeBounded{}

	var x interface{} = struct {
		ContentExtractor
		BoundedExtractor
	}{}
	_, ok := x.(BoundedExtractor)
	if !ok {
		t.Fatalf("interface composition should satisfy BoundedExtractor")
	}
}
```

- [ ] **Step 2: Run test to confirm it fails**

Run: `go test ./api/ -run TestBoundedExtractor_TypeAssert -v`
Expected: FAIL with "undefined: BoundedExtractor".

- [ ] **Step 3: Add interface**

Append to `api/content_extractor.go`:

```go
// BoundedExtractor is implemented by extractors that must run under a
// wall-clock deadline (CPU- or memory-heavy extractors that could otherwise
// run indefinitely on adversarial input). The pipeline calls Bounded() to
// detect the requirement; the value is informational and always true for
// types that implement it.
type BoundedExtractor interface {
	Bounded() bool
}
```

- [ ] **Step 4: Run test to confirm it passes**

Run: `go test ./api/ -run TestBoundedExtractor_TypeAssert -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/content_extractor.go api/content_extractor_test.go
git commit -m "feat(api): add BoundedExtractor marker interface"
```

---

## Task 6: Common scaffolding — typed errors + markdown builder

**Files:**
- Create: `api/content_extractor_ooxml_common.go` (this task: errors + markdown builder only; reader/decoder/concurrency in Tasks 7–9)
- Test: `api/content_extractor_ooxml_common_test.go`

- [ ] **Step 1: Write failing tests**

```go
package api

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractionLimitError_IsAndUnwrap(t *testing.T) {
	e := &extractionLimitError{Kind: "compressed_size", Limit: 100, Observed: 200}
	assert.True(t, errors.Is(e, ErrExtractionLimit))
	assert.False(t, errors.Is(e, ErrMalformed))
	assert.Contains(t, e.Error(), "compressed_size")
	assert.Contains(t, e.Error(), "100")
	assert.Contains(t, e.Error(), "200")
}

func TestExtractionLimitError_WithDetail(t *testing.T) {
	e := &extractionLimitError{Kind: "part_count", Limit: 250, Observed: 251, Detail: "slide #251"}
	assert.Contains(t, e.Error(), "slide #251")
}

func TestMarkdownBuilder_BoundsTrip(t *testing.T) {
	b := newMarkdownBuilder(8)
	_, err := b.WriteString("12345")
	assert.NoError(t, err)
	_, err = b.WriteString("678")
	assert.NoError(t, err)
	_, err = b.WriteString("9")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrExtractionLimit))
	// No partial output should be retrievable beyond the cap.
	assert.LessOrEqual(t, b.Len(), 8)
}

func TestMarkdownBuilder_BelowBound(t *testing.T) {
	b := newMarkdownBuilder(64)
	_, err := b.WriteString("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", b.String())
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./api/ -run TestExtractionLimitError -v -count=1`
Expected: FAIL — undefined types.

- [ ] **Step 3: Implement errors + markdown builder**

Create `api/content_extractor_ooxml_common.go`:

```go
package api

import (
	"bytes"
	"errors"
	"fmt"
)

// Sentinel errors returned by OOXML extractors. The pipeline uses errors.Is
// to classify outcomes; these are the stable public surface.
var (
	ErrExtractionLimit = errors.New("extraction limit exceeded")
	ErrMalformed       = errors.New("malformed document")
	ErrUnsupported     = errors.New("unsupported document subformat")
)

// extractionLimitError describes which limit tripped during extraction. The
// API surface (Kind values) is stable: the pipeline maps Kind into
// access_reason_code.
type extractionLimitError struct {
	Kind     string // compressed_size | decompressed_size | part_size | part_count |
	                // markdown_size | timeout | xml_depth | zip_nested | zip_path | compression_ratio
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

// markdownBuilder wraps bytes.Buffer with a hard cap. Any write that would
// push Len() past max returns *extractionLimitError{Kind:"markdown_size"}.
// The buffer state is left as it was before the failing write — no partial
// output beyond the cap.
type markdownBuilder struct {
	buf bytes.Buffer
	max int64
}

func newMarkdownBuilder(max int64) *markdownBuilder { return &markdownBuilder{max: max} }

func (m *markdownBuilder) WriteString(s string) (int, error) {
	if int64(m.buf.Len()+len(s)) > m.max {
		return 0, &extractionLimitError{
			Kind:     "markdown_size",
			Limit:    m.max,
			Observed: int64(m.buf.Len() + len(s)),
		}
	}
	return m.buf.WriteString(s)
}

func (m *markdownBuilder) WriteByte(b byte) error {
	if int64(m.buf.Len()+1) > m.max {
		return &extractionLimitError{
			Kind:     "markdown_size",
			Limit:    m.max,
			Observed: int64(m.buf.Len() + 1),
		}
	}
	return m.buf.WriteByte(b)
}

func (m *markdownBuilder) Len() int     { return m.buf.Len() }
func (m *markdownBuilder) String() string { return m.buf.String() }
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./api/ -run "TestExtractionLimitError|TestMarkdownBuilder" -v -count=1`
Expected: PASS for all four sub-tests.

- [ ] **Step 5: Commit**

```bash
git add api/content_extractor_ooxml_common.go api/content_extractor_ooxml_common_test.go
git commit -m "feat(api): OOXML extractor common — typed errors + markdown builder"
```

---

## Task 7: Common scaffolding — bounded zip opener (security + part streaming)

**Files:**
- Modify: `api/content_extractor_ooxml_common.go`
- Test: `api/content_extractor_ooxml_common_test.go`

- [ ] **Step 1: Write failing tests**

Append to `api/content_extractor_ooxml_common_test.go`:

```go
import (
	"archive/zip"
	"bytes"
	"io"
)

// buildZip is a tiny helper that builds an in-memory OOXML-shaped archive
// from a name -> bytes map. Used by all OOXML extractor tests.
func buildZip(t *testing.T, parts map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%s): %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("zip write(%s): %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestOOXMLOpener_RejectsCompressedTooLarge(t *testing.T) {
	o := newOOXMLOpener(ooxmlLimits{
		CompressedSizeBytes: 100, DecompressedSizeBytes: 1000, PartSizeBytes: 1000,
		MaxCompressionRatio: 100,
	})
	data := make([]byte, 200) // > 100
	_, err := o.open(data)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "compressed_size", le.Kind)
}

func TestOOXMLOpener_RejectsPathTraversal(t *testing.T) {
	o := newOOXMLOpener(defaultOOXMLLimits())
	data := buildZip(t, map[string][]byte{
		"../escape.xml": []byte("<x/>"),
	})
	_, err := o.open(data)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T (err=%v)", err, err)
	}
	assert.Equal(t, "zip_path", le.Kind)
}

func TestOOXMLOpener_RejectsAbsoluteAndBackslashPath(t *testing.T) {
	o := newOOXMLOpener(defaultOOXMLLimits())
	for _, name := range []string{"/abs/path.xml", `with\backslash.xml`} {
		data := buildZip(t, map[string][]byte{name: []byte("<x/>")})
		_, err := o.open(data)
		assert.Error(t, err, "name=%q", name)
	}
}

func TestOOXMLOpener_RejectsNestedZip(t *testing.T) {
	inner := buildZip(t, map[string][]byte{"a.xml": []byte("<x/>")})
	outer := buildZip(t, map[string][]byte{"nested.zip": inner})
	o := newOOXMLOpener(defaultOOXMLLimits())
	z, err := o.open(outer)
	assert.NoError(t, err, "open should succeed; member-level streaming detects nesting")
	_, err = z.openMember("nested.zip")
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "zip_nested", le.Kind)
}

func TestOOXMLOpener_RejectsCompressionRatioBomb(t *testing.T) {
	// Build a single member with extreme compression ratio. zlib compresses
	// long runs of zeros down to a tiny payload.
	big := bytes.Repeat([]byte{0}, 200_000) // 200 KB
	data := buildZip(t, map[string][]byte{"document.xml": big})
	// Make sure the test member actually compressed well; if not, this test
	// is meaningless. Cap the input size at the MaxCompressionRatio limit.
	limits := defaultOOXMLLimits()
	limits.MaxCompressionRatio = 5 // adversarially low
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)
	_, err = z.openMember("document.xml")
	assert.Error(t, err)
	var le *extractionLimitError
	if errors.As(err, &le) {
		assert.Equal(t, "compression_ratio", le.Kind)
	}
}

func TestOOXMLOpener_TripsPartSize(t *testing.T) {
	// member exceeds part size cap
	data := buildZip(t, map[string][]byte{"big.xml": bytes.Repeat([]byte("a"), 5_000)})
	limits := defaultOOXMLLimits()
	limits.PartSizeBytes = 1_000
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)
	r, err := z.openMember("big.xml")
	assert.NoError(t, err)
	_, err = io.ReadAll(r)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "part_size", le.Kind)
}

func TestOOXMLOpener_TripsCumulativeDecompressed(t *testing.T) {
	a := bytes.Repeat([]byte("a"), 800)
	b := bytes.Repeat([]byte("b"), 800)
	data := buildZip(t, map[string][]byte{"a.xml": a, "b.xml": b})
	limits := defaultOOXMLLimits()
	limits.DecompressedSizeBytes = 1_000
	limits.PartSizeBytes = 1_000
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)

	r1, err := z.openMember("a.xml")
	assert.NoError(t, err)
	_, err = io.ReadAll(r1)
	assert.NoError(t, err)

	r2, err := z.openMember("b.xml")
	assert.NoError(t, err)
	_, err = io.ReadAll(r2)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "decompressed_size", le.Kind)
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./api/ -run "TestOOXMLOpener" -v -count=1`
Expected: FAIL — undefined types.

- [ ] **Step 3: Implement opener**

Append to `api/content_extractor_ooxml_common.go`:

```go
import (
	"archive/zip"
	"io"
	"strings"
)

// ooxmlLimits is the subset of ContentExtractorsConfig that the opener and
// XML decoder care about. Decoupled from internal/config to keep the api
// package free of config imports for unit-test simplicity.
type ooxmlLimits struct {
	CompressedSizeBytes   int64
	DecompressedSizeBytes int64
	PartSizeBytes         int64
	MarkdownSizeBytes     int64
	MaxXMLElementDepth    int
	MaxCompressionRatio   int64
}

// defaultOOXMLLimits returns the design-spec default values; used by tests
// that don't care about specific limits.
func defaultOOXMLLimits() ooxmlLimits {
	return ooxmlLimits{
		CompressedSizeBytes:   20 * 1024 * 1024,
		DecompressedSizeBytes: 50 * 1024 * 1024,
		PartSizeBytes:         20 * 1024 * 1024,
		MarkdownSizeBytes:     128 * 1024,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
	}
}

// ooxmlOpener wraps archive/zip with limit enforcement and security
// checks. It refuses oversize inputs up front, rejects path traversal /
// absolute paths / backslashes, and gates per-member reads through
// boundedReader so that streaming decoders trip mid-read on overrun.
type ooxmlOpener struct{ limits ooxmlLimits }

func newOOXMLOpener(l ooxmlLimits) *ooxmlOpener { return &ooxmlOpener{limits: l} }

type ooxmlArchive struct {
	zr        *zip.Reader
	limits    ooxmlLimits
	consumed  int64 // running cumulative decompressed bytes across all members
}

// open performs up-front compressed-size + path-shape checks and returns an
// archive handle. It does not decompress yet — that happens member-by-member.
func (o *ooxmlOpener) open(data []byte) (*ooxmlArchive, error) {
	if int64(len(data)) > o.limits.CompressedSizeBytes {
		return nil, &extractionLimitError{
			Kind: "compressed_size", Limit: o.limits.CompressedSizeBytes, Observed: int64(len(data)),
		}
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: zip read: %v", ErrMalformed, err)
	}
	for _, f := range zr.File {
		name := f.Name
		if strings.Contains(name, `\`) {
			return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "backslash: " + name}
		}
		if strings.HasPrefix(name, "/") {
			return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "absolute: " + name}
		}
		// path-traversal check: any segment ".." rejected
		for _, seg := range strings.Split(name, "/") {
			if seg == ".." {
				return nil, &extractionLimitError{Kind: "zip_path", Limit: 0, Observed: 0, Detail: "traversal: " + name}
			}
		}
	}
	return &ooxmlArchive{zr: zr, limits: o.limits}, nil
}

// openMember opens a single member by exact name, returning a reader that
// enforces per-part + cumulative + ratio limits. Returns ErrMalformed-wrapped
// error if the member doesn't exist. Returns *extractionLimitError if the
// member is a nested zip (sniffed by header).
func (a *ooxmlArchive) openMember(name string) (io.Reader, error) {
	for _, f := range a.zr.File {
		if f.Name != name {
			continue
		}
		if int64(f.UncompressedSize64) > a.limits.PartSizeBytes {
			return nil, &extractionLimitError{
				Kind: "part_size", Limit: a.limits.PartSizeBytes, Observed: int64(f.UncompressedSize64),
				Detail: name,
			}
		}
		// Compression-ratio sanity: only enforce when we have a non-zero
		// compressed size to compare against.
		if f.CompressedSize64 > 0 {
			ratio := int64(f.UncompressedSize64) / int64(f.CompressedSize64)
			if ratio > a.limits.MaxCompressionRatio {
				return nil, &extractionLimitError{
					Kind: "compression_ratio", Limit: a.limits.MaxCompressionRatio, Observed: ratio,
					Detail: name,
				}
			}
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("%w: open %s: %v", ErrMalformed, name, err)
		}
		// Sniff header for nested-zip refusal.
		header := make([]byte, 4)
		n, _ := io.ReadFull(rc, header)
		if n == 4 && bytes.Equal(header[:4], []byte{0x50, 0x4b, 0x03, 0x04}) {
			_ = rc.Close()
			return nil, &extractionLimitError{Kind: "zip_nested", Limit: 0, Observed: 0, Detail: name}
		}
		return &boundedReader{
			under:    io.MultiReader(bytes.NewReader(header[:n]), rc),
			closer:   rc,
			archive:  a,
			partCap:  a.limits.PartSizeBytes,
			partRead: 0,
			memberID: name,
		}, nil
	}
	return nil, fmt.Errorf("%w: missing required part %q", ErrMalformed, name)
}

// boundedReader enforces per-part and cumulative-decompressed limits as it
// streams. archive.consumed is updated on every Read so that subsequent
// member opens see the running total.
type boundedReader struct {
	under    io.Reader
	closer   io.Closer
	archive  *ooxmlArchive
	partCap  int64
	partRead int64
	memberID string
}

func (b *boundedReader) Read(p []byte) (int, error) {
	n, err := b.under.Read(p)
	b.partRead += int64(n)
	b.archive.consumed += int64(n)
	if b.partRead > b.partCap {
		return n, &extractionLimitError{Kind: "part_size", Limit: b.partCap, Observed: b.partRead, Detail: b.memberID}
	}
	if b.archive.consumed > b.archive.limits.DecompressedSizeBytes {
		return n, &extractionLimitError{
			Kind: "decompressed_size", Limit: b.archive.limits.DecompressedSizeBytes, Observed: b.archive.consumed,
		}
	}
	return n, err
}

func (b *boundedReader) Close() error {
	if b.closer != nil {
		return b.closer.Close()
	}
	return nil
}
```

Note: the `import` block must be merged with the existing one in the file — single `import (...)` block. If you used `goimports`, it will merge. Otherwise edit by hand.

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./api/ -run "TestOOXMLOpener" -v -count=1`
Expected: PASS for all sub-tests.

- [ ] **Step 5: Commit**

```bash
git add api/content_extractor_ooxml_common.go api/content_extractor_ooxml_common_test.go
git commit -m "feat(api): OOXML common — bounded zip opener + member streaming"
```

---

## Task 8: Common scaffolding — bounded XML decoder

**Files:**
- Modify: `api/content_extractor_ooxml_common.go`
- Test: `api/content_extractor_ooxml_common_test.go`

- [ ] **Step 1: Write failing tests**

```go
func TestBoundedXMLDecoder_HappyPath(t *testing.T) {
	src := bytes.NewReader([]byte(`<root><a>x</a><b>y</b></root>`))
	d := newBoundedXMLDecoder(src, 10)
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
	}
}

func TestBoundedXMLDecoder_TripsDepth(t *testing.T) {
	// 6 nested elements with maxDepth=4 should trip on the 5th open.
	xml := `<a><b><c><d><e><f>x</f></e></d></c></b></a>`
	src := bytes.NewReader([]byte(xml))
	d := newBoundedXMLDecoder(src, 4)
	tripped := false
	for {
		_, err := d.Token()
		if err == nil {
			continue
		}
		if errors.Is(err, ErrExtractionLimit) {
			var le *extractionLimitError
			errors.As(err, &le)
			assert.Equal(t, "xml_depth", le.Kind)
			tripped = true
			break
		}
		if err == io.EOF {
			break
		}
		t.Fatalf("unexpected error: %v", err)
	}
	assert.True(t, tripped, "depth limit must trip")
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./api/ -run "TestBoundedXMLDecoder" -v -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement bounded XML decoder**

Append to `api/content_extractor_ooxml_common.go`:

```go
import "encoding/xml"

// boundedXMLDecoder wraps encoding/xml.Decoder with a hard depth ceiling.
// Standard library has no depth bound natively. We count depth on every
// StartElement / EndElement and trip ErrExtractionLimit{Kind:"xml_depth"}
// the moment we observe an open that exceeds maxDepth.
type boundedXMLDecoder struct {
	dec      *xml.Decoder
	depth    int
	maxDepth int
}

func newBoundedXMLDecoder(r io.Reader, maxDepth int) *boundedXMLDecoder {
	return &boundedXMLDecoder{dec: xml.NewDecoder(r), maxDepth: maxDepth}
}

func (b *boundedXMLDecoder) Token() (xml.Token, error) {
	tok, err := b.dec.Token()
	if err != nil {
		return tok, err
	}
	switch tok.(type) {
	case xml.StartElement:
		b.depth++
		if b.depth > b.maxDepth {
			return nil, &extractionLimitError{
				Kind: "xml_depth", Limit: int64(b.maxDepth), Observed: int64(b.depth),
			}
		}
	case xml.EndElement:
		b.depth--
	}
	return tok, nil
}

// DecodeElement is a convenience wrapper that delegates to the embedded
// decoder; depth is already accounted for by the surrounding Token() calls.
func (b *boundedXMLDecoder) DecodeElement(v interface{}, start *xml.StartElement) error {
	return b.dec.DecodeElement(v, start)
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./api/ -run "TestBoundedXMLDecoder" -v -count=1`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/content_extractor_ooxml_common.go api/content_extractor_ooxml_common_test.go
git commit -m "feat(api): OOXML common — bounded XML decoder with depth limit"
```

---

## Task 9: Common scaffolding — deadline wrapper + per-user concurrency limiter

**Files:**
- Modify: `api/content_extractor_ooxml_common.go`
- Test: `api/content_extractor_ooxml_common_test.go`

- [ ] **Step 1: Write failing tests**

```go
import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

func TestExtractWithDeadline_Happy(t *testing.T) {
	ctx := context.Background()
	out, err := extractWithDeadline(ctx, 200*time.Millisecond, func(ctx context.Context) (ExtractedContent, error) {
		return ExtractedContent{Text: "ok"}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "ok", out.Text)
}

func TestExtractWithDeadline_Timeout(t *testing.T) {
	ctx := context.Background()
	_, err := extractWithDeadline(ctx, 50*time.Millisecond, func(ctx context.Context) (ExtractedContent, error) {
		select {
		case <-ctx.Done():
			return ExtractedContent{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return ExtractedContent{}, nil
		}
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

func TestExtractWithDeadline_ParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := extractWithDeadline(ctx, 5*time.Second, func(ctx context.Context) (ExtractedContent, error) {
		<-ctx.Done()
		return ExtractedContent{}, ctx.Err()
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestConcurrencyLimiter_BlocksAndReleases(t *testing.T) {
	cl := newConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 0, nil // no override; use fallback
	})
	var concurrent int32
	var maxObserved int32
	var wg sync.WaitGroup
	work := func() {
		release, err := cl.acquire(context.Background(), "alice")
		assert.NoError(t, err)
		defer release()
		n := atomic.AddInt32(&concurrent, 1)
		for {
			cur := atomic.LoadInt32(&maxObserved)
			if n <= cur || atomic.CompareAndSwapInt32(&maxObserved, cur, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt32(&concurrent, -1)
	}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); work() }()
	}
	wg.Wait()
	assert.LessOrEqual(t, maxObserved, int32(2), "must never exceed configured limit")
}

func TestConcurrencyLimiter_OverrideHonored(t *testing.T) {
	cl := newConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		if userID == "bot" {
			return 5, nil
		}
		return 0, nil
	})
	release, err := cl.acquire(context.Background(), "bot")
	assert.NoError(t, err)
	release()
	// Internal: confirm cap is 5 by attempting 5 concurrent acquires without timing out.
	var wg sync.WaitGroup
	hold := make(chan struct{})
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			rel, err := cl.acquire(ctx, "bot")
			assert.NoError(t, err)
			<-hold
			rel()
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(hold)
	wg.Wait()
}

func TestConcurrencyLimiter_OverrideOutOfBoundFallsBack(t *testing.T) {
	cl := newConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 999, nil // out of bounds; must fall back to 2
	})
	rel, err := cl.acquire(context.Background(), "u")
	assert.NoError(t, err)
	rel()
	// Verify by saturating: 3rd acquirer should block until release.
	rel1, _ := cl.acquire(context.Background(), "u")
	rel2, _ := cl.acquire(context.Background(), "u")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = cl.acquire(ctx, "u")
	assert.Error(t, err, "third concurrent acquire must time out under fallback=2")
	rel1()
	rel2()
}

func TestConcurrencyLimiter_LookupErrorFallsBack(t *testing.T) {
	cl := newConcurrencyLimiter(2, func(ctx context.Context, userID string) (int, error) {
		return 0, errors.New("db down")
	})
	rel, err := cl.acquire(context.Background(), "u")
	assert.NoError(t, err)
	rel()
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./api/ -run "TestExtractWithDeadline|TestConcurrencyLimiter" -v -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement deadline wrapper + concurrency limiter**

Append to `api/content_extractor_ooxml_common.go`:

```go
import "golang.org/x/sync/semaphore"

// extractWithDeadline runs fn under a fresh context with the given budget.
// On timeout it returns context.DeadlineExceeded; on parent cancel it
// returns ctx.Err(). The wrapped fn receives the deadline-bearing context
// so that cooperative cancellation is possible.
func extractWithDeadline(ctx context.Context, budget time.Duration, fn func(context.Context) (ExtractedContent, error)) (ExtractedContent, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type result struct {
		c ExtractedContent
		e error
	}
	ch := make(chan result, 1)
	go func() {
		c, e := fn(ctx)
		ch <- result{c, e}
	}()
	select {
	case r := <-ch:
		return r.c, r.e
	case <-ctx.Done():
		return ExtractedContent{}, ctx.Err()
	}
}

// ctxReader wraps an io.Reader so that wall-clock cancellation aborts
// in-flight reads. Used by extractors when streaming large parts.
type ctxReader struct {
	r   io.Reader
	ctx context.Context
}

func newCtxReader(ctx context.Context, r io.Reader) *ctxReader { return &ctxReader{r: r, ctx: ctx} }

func (c *ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// concurrencyLimiter caps simultaneous extractions per user. Capacity is
// looked up on first acquire and cached per-user for the lifetime of the
// process (override changes don't resize the existing semaphore — known
// limitation, see design spec).
type concurrencyLimiter struct {
	mu       sync.Mutex
	sems     map[string]*semaphore.Weighted
	lookup   func(ctx context.Context, userID string) (int, error)
	fallback int
}

func newConcurrencyLimiter(fallback int, lookup func(ctx context.Context, userID string) (int, error)) *concurrencyLimiter {
	if fallback <= 0 || fallback > maxPerUserConcurrencyCap {
		fallback = 2
	}
	return &concurrencyLimiter{
		sems:     map[string]*semaphore.Weighted{},
		lookup:   lookup,
		fallback: fallback,
	}
}

// maxPerUserConcurrencyCap mirrors internal/config.maxPerUserConcurrency.
// Duplicated here to avoid importing internal/config from api package and
// breaking the layering. Tests assert the values stay in sync.
const maxPerUserConcurrencyCap = 16

func (cl *concurrencyLimiter) acquire(ctx context.Context, userID string) (release func(), err error) {
	cl.mu.Lock()
	sem, ok := cl.sems[userID]
	if !ok {
		n := cl.fallback
		if cl.lookup != nil {
			if got, lerr := cl.lookup(ctx, userID); lerr == nil && got > 0 && got <= maxPerUserConcurrencyCap {
				n = got
			}
		}
		sem = semaphore.NewWeighted(int64(n))
		cl.sems[userID] = sem
	}
	cl.mu.Unlock()
	if err := sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	return func() { sem.Release(1) }, nil
}
```

- [ ] **Step 4: Run tests to confirm they pass**

Run: `go test ./api/ -run "TestExtractWithDeadline|TestConcurrencyLimiter" -v -count=1 -race`
Expected: PASS, no race warnings.

- [ ] **Step 5: Commit**

```bash
git add api/content_extractor_ooxml_common.go api/content_extractor_ooxml_common_test.go
git commit -m "feat(api): OOXML common — deadline wrapper + per-user concurrency limiter"
```

---

## Task 10: DOCX extractor

**Files:**
- Create: `api/content_extractor_docx.go`
- Test: `api/content_extractor_docx_test.go`

- [ ] **Step 1: Write failing tests (start narrow)**

Test the high-value paths first; expand in subsequent commits within the same task.

```go
package api

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const minimalDocxBody = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>Title</w:t></w:r></w:p>
    <w:p><w:r><w:t>Hello</w:t></w:r></w:p>
  </w:body>
</w:document>`

func TestDOCXExtractor_BasicHeadingAndParagraph(t *testing.T) {
	data := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	assert.NoError(t, err)
	assert.Contains(t, out.Text, "# Title")
	assert.Contains(t, out.Text, "Hello")
	assert.Equal(t, "Title", out.Title)
}

func TestDOCXExtractor_AllHeadingLevels(t *testing.T) {
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
	for i := 1; i <= 6; i++ {
		body += `<w:p><w:pPr><w:pStyle w:val="Heading` + string(rune('0'+i)) + `"/></w:pPr><w:r><w:t>H` + string(rune('0'+i)) + `</w:t></w:r></w:p>`
	}
	body += `</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	out, err := e.Extract(data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	assert.NoError(t, err)
	for i := 1; i <= 6; i++ {
		assert.Contains(t, out.Text, strings.Repeat("#", i)+" H"+string(rune('0'+i)))
	}
}

func TestDOCXExtractor_MissingDocumentXMLIsMalformed(t *testing.T) {
	data := buildZip(t, map[string][]byte{"word/styles.xml": []byte("<x/>")})
	e := NewDOCXExtractor(defaultOOXMLLimits())
	_, err := e.Extract(data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrMalformed))
}

func TestDOCXExtractor_BoundedFlag(t *testing.T) {
	e := NewDOCXExtractor(defaultOOXMLLimits())
	assert.True(t, e.Bounded())
}

func TestDOCXExtractor_CanHandle(t *testing.T) {
	e := NewDOCXExtractor(defaultOOXMLLimits())
	assert.True(t, e.CanHandle("application/vnd.openxmlformats-officedocument.wordprocessingml.document"))
	assert.False(t, e.CanHandle("application/pdf"))
}

func TestDOCXExtractor_TripsMarkdownSize(t *testing.T) {
	// A document whose body would exceed a tiny markdown cap.
	body := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`
	for i := 0; i < 100; i++ {
		body += `<w:p><w:r><w:t>` + strings.Repeat("x", 64) + `</w:t></w:r></w:p>`
	}
	body += `</w:body></w:document>`
	data := buildZip(t, map[string][]byte{"word/document.xml": []byte(body)})
	limits := defaultOOXMLLimits()
	limits.MarkdownSizeBytes = 200 // tiny
	e := NewDOCXExtractor(limits)
	_, err := e.Extract(data, "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	assert.Error(t, err)
	var le *extractionLimitError
	if errors.As(err, &le) {
		assert.Equal(t, "markdown_size", le.Kind)
	}
}
```

- [ ] **Step 2: Run tests to confirm they fail**

Run: `go test ./api/ -run TestDOCXExtractor -v -count=1`
Expected: FAIL — undefined.

- [ ] **Step 3: Implement DOCX extractor**

Implement `api/content_extractor_docx.go` with the following structure. The implementation streams `word/document.xml` via `boundedXMLDecoder`, handles `w:p`, `w:r/w:t`, `w:pPr/w:pStyle` for headings, `w:tbl/w:tr/w:tc` for tables, `w:numPr/w:ilvl` for lists (reads `word/numbering.xml` once, lazily), `w:hyperlink` (reads `word/_rels/document.xml.rels` once, lazily) for `[text](url)`, `w:drawing` -> `wp:docPr@descr` for `![alt](image-N)`, `w:footnoteReference@id` -> `[^N]` (with `### Footnotes` block read lazily from `word/footnotes.xml`). Skip headers/footers/comments. Skip unaccepted `w:ins` runs; include `w:del` only if accepted (treat unaccepted track changes as the default — i.e. honor `w:del` content unless it's wrapped in unrejected revision; the project policy is to skip unaccepted track changes per the design spec).

```go
package api

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// DOCXExtractor extracts Markdown-flavored text from a DOCX (OOXML) archive.
// Output structure:
//   - Headings via w:pPr/w:pStyle = "Heading1".."Heading6" → "# ".."###### "
//   - Paragraphs as plain lines separated by blank lines
//   - Tables as Markdown pipe tables (cell text only)
//   - Lists from w:numPr (bullet vs numbered, indented by w:ilvl)
//   - Hyperlinks as [text](url), resolved via word/_rels/document.xml.rels
//   - Drawings as ![descr](image-N), only when descr is non-empty
//   - Footnotes inline as [^N] plus a "### Footnotes" trailing block
type DOCXExtractor struct {
	limits ooxmlLimits
}

// NewDOCXExtractor returns an extractor configured with the given limits.
func NewDOCXExtractor(limits ooxmlLimits) *DOCXExtractor {
	return &DOCXExtractor{limits: limits}
}

// Name returns the extractor name as registered with the registry.
func (e *DOCXExtractor) Name() string { return "docx" }

// CanHandle returns true iff contentType is the DOCX OOXML MIME type.
func (e *DOCXExtractor) CanHandle(contentType string) bool {
	return strings.EqualFold(contentType,
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document")
}

// Bounded marks DOCXExtractor as needing a wall-clock deadline.
func (e *DOCXExtractor) Bounded() bool { return true }

// Extract reads bytes via the bounded zip opener, streams word/document.xml
// through the bounded XML decoder, and writes to a markdown builder. On any
// limit trip the typed *extractionLimitError surfaces; on missing required
// parts the wrapped ErrMalformed surfaces.
func (e *DOCXExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}
	rdr, err := arch.openMember("word/document.xml")
	if err != nil {
		return ExtractedContent{}, err
	}
	if c, ok := rdr.(io.Closer); ok {
		defer func() { _ = c.Close() }()
	}

	mb := newMarkdownBuilder(e.limits.MarkdownSizeBytes)
	d := newBoundedXMLDecoder(rdr, e.limits.MaxXMLElementDepth)

	state := &docxState{
		mb:    mb,
		arch:  arch,
		// Lazily loaded:
		// numbering: parsed on first w:numPr
		// rels:      parsed on first w:hyperlink
		// footnotes: parsed before we close out
	}

	if err := docxRenderBody(d, state); err != nil {
		return ExtractedContent{}, err
	}

	if state.hasFootnotes {
		if err := docxAppendFootnotes(arch, state); err != nil {
			return ExtractedContent{}, err
		}
	}

	return ExtractedContent{
		Text:        strings.TrimRight(state.mb.String(), "\n"),
		Title:       state.title,
		ContentType: contentType,
	}, nil
}

// docxState carries cross-element rendering state through the streaming pass.
type docxState struct {
	mb           *markdownBuilder
	arch         *ooxmlArchive
	title        string
	imageCounter int
	hasFootnotes bool
	footnoteIDs  []string
	// (numbering and rels are loaded lazily inside helpers)
}

// docxRenderBody walks the XML token stream and writes Markdown to state.mb.
// Implementation outline:
//
//   - Track current paragraph style and list info from w:pPr.
//   - On w:p start: emit blank line if not first.
//   - On w:r start: if inside w:ins or w:del without an accepted revision
//     attribute, skip until end. Otherwise concatenate w:t text.
//   - On w:hyperlink with r:id: lookup rels, wrap text in []().
//   - On w:tbl: buffer the table, render as markdown pipe-table on close.
//   - On w:drawing: read wp:docPr@descr, emit ![descr](image-N) when non-empty.
//   - On w:footnoteReference@id: emit [^N], track id for later.
//
// The helpers below (renderParagraph, renderRun, renderTable, etc.) are kept
// short and called from a single switch in this loop.
func docxRenderBody(d *boundedXMLDecoder, st *docxState) error {
	// Implementation note for the engineer:
	//
	// Use `for { tok, err := d.Token(); if err == io.EOF { break }; if err != nil { return err } }`
	// and a switch on the token type. Track depth via local counters as
	// needed (e.g. inTable, inIns, currentRunText). Do NOT use a recursive
	// descent — it complicates depth bounding and is harder to reason about.
	//
	// The DOCX schema is fully specified in ECMA-376 Part 1. The minimal
	// element set for this extractor is:
	//   w:body w:p w:pPr w:pStyle@val w:numPr w:ilvl@val w:numId@val
	//   w:r w:rPr w:t w:hyperlink@r:id w:tbl w:tr w:tc
	//   w:drawing wp:docPr@descr w:footnoteReference@id w:ins w:del
	//
	// Implementation should be ~250 lines. Keep helper functions <50 lines.
	// Test-driven: add one test per element class above and grow this body.
	return fmt.Errorf("TODO: docxRenderBody — implement per ECMA-376 element list above")
}

// docxAppendFootnotes loads word/footnotes.xml lazily and appends a
// "### Footnotes" section with one [^N]: text per referenced id, in the
// order they were encountered in the body.
func docxAppendFootnotes(arch *ooxmlArchive, st *docxState) error {
	// Implementation note: open word/footnotes.xml via arch.openMember,
	// stream-parse w:footnotes/w:footnote@w:id and accumulate text per id,
	// then emit "### Footnotes\n\n[^N]: text\n" for each id in st.footnoteIDs.
	// Skip ids that the body didn't reference. Treat missing footnotes.xml
	// as ErrMalformed only if hasFootnotes is true and the file is absent.
	return fmt.Errorf("TODO: docxAppendFootnotes")
}

// XML structs used by the helpers (kept tiny — encoding/xml is opt-in field
// matching, so omitted siblings are ignored).
type docxStyle struct {
	XMLName xml.Name `xml:"http://schemas.openxmlformats.org/wordprocessingml/2006/main pStyle"`
	Val     string   `xml:"val,attr"`
}

type docxRels struct {
	Relationships []struct {
		ID     string `xml:"Id,attr"`
		Target string `xml:"Target,attr"`
	} `xml:"Relationship"`
}
```

The two `TODO:` placeholders are intentional handoffs to the engineer who'll implement the body element walk under the same TDD loop. Each test in Step 1 already pins the high-level behavior; the engineer adds tests one at a time for each element class (paragraphs, headings, lists, tables, hyperlinks, drawings, footnotes, track-changes) and grows `docxRenderBody` to satisfy them.

**Important**: do **not** finish this task with the `TODO:` returns in place. The Step-1 test set already covers the minimum body elements (heading, paragraph, missing-doc malformed, markdown trip). Implement enough of `docxRenderBody` to satisfy those tests **first**, commit, then expand element coverage in follow-up commits within this task.

- [ ] **Step 4: Write the body-walk implementation**

Replace the `TODO: docxRenderBody` body with a real streaming walk that satisfies the Step-1 tests. Reference: `encoding/xml.Decoder` token model. Use `xml.StartElement.Name.Local` and `Name.Space` to dispatch. Element handlers should be small functions taking `(d, st, start)` and returning `error`.

A reference implementation is ~250 lines. Keep it in this file unless it grows beyond ~400 lines, in which case split into `content_extractor_docx_body.go`.

- [ ] **Step 5: Run all DOCX tests**

Run: `go test ./api/ -run TestDOCXExtractor -v -count=1`
Expected: all sub-tests PASS.

- [ ] **Step 6: Add element-coverage tests one at a time**

For each of the following, write a test, run it (it should fail or pass — if it passes, the existing implementation already covers it; if it fails, extend the body walk and re-run):

1. Bullet list (`w:numPr` with `w:numId` referencing a bullet in `word/numbering.xml`)
2. Numbered list (same, with `w:numFmt val="decimal"`)
3. Nested list (`w:ilvl val="1"`)
4. Simple 2-row table (header + data) — verify `| h1 | h2 |\n|---|---|\n| d1 | d2 |`
5. Hyperlink — verify `[text](https://example.com)`
6. Drawing with non-empty `wp:docPr@descr` — verify `![alt](image-1)`
7. Drawing with empty `descr` — verify omitted entirely
8. Footnote reference — verify `[^1]` inline + `### Footnotes` section
9. `w:headerReference` / `w:footerReference` — verify excluded
10. Comment range (`w:commentRangeStart..End`) — verify excluded

Each test gets its own commit at this stage so a reviewer can follow the build-up.

- [ ] **Step 7: Commit**

If you batched Steps 4–6, end with one final commit. Otherwise commit after each sub-test passes. Final commit message:

```bash
git add api/content_extractor_docx.go api/content_extractor_docx_test.go
git commit -m "feat(api): DOCX content extractor with markdown output"
```

---

## Task 11: PPTX extractor

**Files:**
- Create: `api/content_extractor_pptx.go`
- Test: `api/content_extractor_pptx_test.go`

Follow the same TDD pattern as Task 10. PPTX has a slightly different shape:
the extractor reads `ppt/presentation.xml` for slide order via
`p:sldIdLst > p:sldId@r:id`, resolves IDs to slide-XML paths via
`ppt/_rels/presentation.xml.rels`, then per slide:

- Skip if `<p:sld show="0">`.
- Trip slide-count limit at start of each slide; `Detail: "slide #N"`.
- Walk `p:cSld/p:spTree`; for each `p:sp` and `p:graphicFrame`, determine role from `p:nvSpPr/p:nvPr/p:ph@type` (one of `title`, `ctr-title`, `body`, `subtitle`, `dt`, `ftr`, `sldNum`); fall back by content type when `ph` absent (`text-box`, `picture`, `chart`, `table`, `diagram`, `group`, `unknown`).
- Emit `<!-- shape: <role> -->` immediately before the shape's text content.
- Walk `p:txBody/a:p/a:r/a:t`. Tables (`a:tbl`) render as Markdown pipe tables.
- First `title`-role shape's text → `## Slide N: <title>`; absent → `## Slide N`.
- Notes slide via `ppt/slides/_rels/slideN.xml.rels` (rel type `notesSlide`). If present, emit `### Notes` then walk the notes-placeholder text.

**Title** = first slide's title.

Tests (write before each capability):

1. Single slide with title → `## Slide 1: Title` + body
2. Two slides ordering preserved
3. Hidden slide skipped
4. All shape roles emit correct HTML comments
5. Speaker notes block included
6. Slide-count trip with `Detail: "slide #N"`
7. Missing `presentation.xml` → ErrMalformed
8. Bad rel id (slide referenced but no rels target) → ErrMalformed
9. Markdown size trip
10. Bounded() returns true; CanHandle exact MIME

- [ ] **Step 1**: Write the failing tests (mirror Task 10 Step 1 structure).
- [ ] **Step 2**: Run them, confirm FAIL.
- [ ] **Step 3**: Implement `PPTXExtractor` with `Name()`, `CanHandle()`, `Bounded()`, and `Extract()`.
- [ ] **Step 4**: Run them, confirm PASS.
- [ ] **Step 5**: Commit:

```bash
git add api/content_extractor_pptx.go api/content_extractor_pptx_test.go
git commit -m "feat(api): PPTX content extractor with markdown output"
```

---

## Task 12: XLSX extractor

**Files:**
- Create: `api/content_extractor_xlsx.go`
- Test: `api/content_extractor_xlsx_test.go`

XLSX uses `github.com/xuri/excelize/v2`. Open via:

```go
f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{
    UnzipSizeLimit:    e.limits.DecompressedSizeBytes,
    UnzipXMLSizeLimit: e.limits.PartSizeBytes,
})
```

Per sheet:
- Emit `## Sheet: <name>`.
- Stream rows via `f.Rows(name)`. Track running cell count toward `e.limits.XLSXCells` (sum across all sheets). Trip with `Kind: "part_count"`, `Detail: "cell #N (sheet '<name>')"` (or `Detail: "xlsx cells"`).
- Header detection over first ≤5 non-empty rows using a per-row style fingerprint = dominant `(bgColor, fontSize, fontWeight, fontItalic)`. Apply the four-rule algorithm spelled out in the design spec section "XLSX". Fall back to content heuristic when style fingerprints are uniform.
- Render rows. Trim leading/trailing empty rows + columns. Per cell:
  - String → as-is, escape `|` and newlines.
  - Number → `strconv.FormatFloat(v, 'f', -1, 64)`.
  - Date (excelize number-format heuristic) → ISO 8601, with time component if format indicates.
  - Bool → `true` / `false`.
  - Formula → cached value if present, else `=<formula text>`.
  - Error (`#REF!` etc.) → pass-through.
- Merged cells (`f.GetMergeCells`): value at top-left, blanks elsewhere.

**Title** = first sheet's name. Skip hidden sheets.

Tests (write before each capability):

1. Empty workbook → empty Text
2. Single value in A1
3. Multi-sheet ordering
4. Hidden sheet skipped
5. All four header-detection rules + content-heuristic fallback (5 sub-tests)
6. Number / date / bool / error / string / formula-cached / formula-uncached cell types
7. Merged cells
8. Trimmed leading/trailing rows + columns
9. Cell-count trip
10. Bounded() returns true; CanHandle exact MIME
11. UnzipSizeLimit honored (XLSX with > limit decompressed → excelize returns error → wrap as `ErrExtractionLimit{Kind:"decompressed_size"}` if detectable, else ErrMalformed)

- [ ] **Step 1**: Write the failing tests.
- [ ] **Step 2**: Run them, confirm FAIL.
- [ ] **Step 3**: Implement `XLSXExtractor`.
- [ ] **Step 4**: Run them, confirm PASS.
- [ ] **Step 5**: Commit:

```bash
git add api/content_extractor_xlsx.go api/content_extractor_xlsx_test.go
git commit -m "feat(api): XLSX content extractor with markdown output"
```

---

## Task 13: Pipeline integration — concurrency gate + deadline + classification

**Files:**
- Modify: `api/content_pipeline.go`
- Modify: `api/access_diagnostics.go` (add reason-code constants)
- Modify: `api/api.go` and `api-schema/tmi-openapi.json` (extend `access_status` enum)
- Modify: `cmd/server/main.go` (build the limiter, pass it + config to the pipeline)
- Test: `api/content_pipeline_test.go` (extend), `api/ooxml_extractors_integration_test.go` (NEW)

- [ ] **Step 1: Add `extraction_failed` to the `access_status` enum**

Edit `api-schema/tmi-openapi.json`. Locate the `access_status` enum (search for `"accessible"`). Add `"extraction_failed"` to the enum array. Then:

```bash
make validate-openapi
make generate-api
```

Expected: clean validation, regenerated `api/api.go`. Review the diff to confirm the enum constant landed.

- [ ] **Step 2: Add Go constant for the new status**

In `api/content_pipeline.go`, alongside the existing constants:

```go
AccessStatusAuthRequired      = "auth_required"
AccessStatusExtractionFailed  = "extraction_failed"
```

Note: `AuthRequired` may already exist in the spec — if so, leave it. Add `ExtractionFailed` regardless.

- [ ] **Step 3: Add reason-code constants to `access_diagnostics.go`**

Append to the existing const block:

```go
// Reason codes emitted by the OOXML extractor pipeline. These are stable
// API contract — tmi-ux localizes messages by code.
const (
	ReasonExtractionLimitCompressedSize    = "extraction_limit:compressed_size"
	ReasonExtractionLimitDecompressedSize  = "extraction_limit:decompressed_size"
	ReasonExtractionLimitPartSize          = "extraction_limit:part_size"
	ReasonExtractionLimitPartCount         = "extraction_limit:part_count"
	ReasonExtractionLimitMarkdownSize      = "extraction_limit:markdown_size"
	ReasonExtractionLimitTimeout           = "extraction_limit:timeout"
	ReasonExtractionLimitXMLDepth          = "extraction_limit:xml_depth"
	ReasonExtractionLimitZipNested         = "extraction_limit:zip_nested"
	ReasonExtractionLimitZipPath           = "extraction_limit:zip_path"
	ReasonExtractionLimitCompressionRatio  = "extraction_limit:compression_ratio"
	ReasonExtractionMalformed              = "extraction_malformed"
	ReasonExtractionUnsupported            = "extraction_unsupported"
	ReasonExtractionInternal               = "extraction_internal"
)
```

- [ ] **Step 4: Write failing pipeline integration tests**

Create `api/ooxml_extractors_integration_test.go`:

```go
package api

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSource always returns the configured bytes + content type.
type stubSource struct {
	data []byte
	ct   string
}

func (s *stubSource) Name() string                                       { return "stub" }
func (s *stubSource) CanHandle(ctx context.Context, uri string) bool     { return true }
func (s *stubSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	return s.data, s.ct, nil
}

func TestContentPipeline_HappyPath_DOCX(t *testing.T) {
	docx := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})
	srcs := NewContentSourceRegistry()
	srcs.Register(&stubSource{data: docx, ct: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"})

	exts := NewContentExtractorRegistry()
	exts.Register(NewDOCXExtractor(defaultOOXMLLimits()))

	cl := newConcurrencyLimiter(2, nil)
	cfg := DefaultPipelineLimits()
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, cfg)

	ctx := WithUserID(context.Background(), "alice")
	out, err := p.Extract(ctx, "https://example.com/doc.docx")
	require.NoError(t, err)
	assert.Contains(t, out.Text, "Title")
}

func TestContentPipeline_TimeoutClassifiesExtractionFailedTimeout(t *testing.T) {
	// Use a stub extractor that sleeps longer than the budget.
	srcs := NewContentSourceRegistry()
	srcs.Register(&stubSource{data: []byte("stub"), ct: "application/sleep"})
	exts := NewContentExtractorRegistry()
	exts.Register(&sleepExtractor{d: 200 * time.Millisecond})

	cl := newConcurrencyLimiter(2, nil)
	cfg := DefaultPipelineLimits()
	cfg.WallClockBudget = 50 * time.Millisecond
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, cfg)
	ctx := WithUserID(context.Background(), "alice")
	_, err := p.Extract(ctx, "https://example.com/x")
	require.Error(t, err)
	classified := ClassifyExtractionError(err)
	assert.Equal(t, AccessStatusExtractionFailed, classified.Status)
	assert.Equal(t, ReasonExtractionLimitTimeout, classified.ReasonCode)
}

type sleepExtractor struct{ d time.Duration }

func (s *sleepExtractor) Name() string                                              { return "sleep" }
func (s *sleepExtractor) CanHandle(ct string) bool                                  { return ct == "application/sleep" }
func (s *sleepExtractor) Bounded() bool                                             { return true }
func (s *sleepExtractor) Extract(data []byte, ct string) (ExtractedContent, error)  { return ExtractedContent{}, nil }
// Note: signature stays `Extract(data, ct)` — the deadline is enforced by the
// pipeline wrapper using extractWithDeadline, which runs Extract in a goroutine
// and returns context.DeadlineExceeded when the budget elapses. Implementation
// of sleepExtractor's Extract should sleep using time.Sleep so the goroutine
// outruns the deadline; the wrapper select picks ctx.Done() first.

func TestContentPipeline_ConcurrencyCap_DefaultTwo(t *testing.T) {
	docx := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})
	srcs := NewContentSourceRegistry()
	srcs.Register(&stubSource{data: docx, ct: "application/vnd.openxmlformats-officedocument.wordprocessingml.document"})
	// Wrap DOCX extractor in one that signals concurrent count.
	wrapped := &countingExtractor{
		inner: NewDOCXExtractor(defaultOOXMLLimits()),
	}
	exts := NewContentExtractorRegistry()
	exts.Register(wrapped)
	cl := newConcurrencyLimiter(2, nil)
	cfg := DefaultPipelineLimits()
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, cfg)
	ctx := WithUserID(context.Background(), "alice")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = p.Extract(ctx, "x") }()
	}
	wg.Wait()
	max := atomic.LoadInt32(&wrapped.maxObserved)
	assert.LessOrEqual(t, max, int32(2), "must never exceed default per-user cap")
}

type countingExtractor struct {
	inner       ContentExtractor
	current     atomic.Int32
	maxObserved int32
}

func (c *countingExtractor) Name() string                                                 { return "counting" }
func (c *countingExtractor) CanHandle(ct string) bool                                     { return c.inner.CanHandle(ct) }
func (c *countingExtractor) Bounded() bool                                                { return true }
func (c *countingExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	n := c.current.Add(1)
	for {
		cur := atomic.LoadInt32(&c.maxObserved)
		if n <= cur || atomic.CompareAndSwapInt32(&c.maxObserved, cur, n) {
			break
		}
	}
	defer c.current.Add(-1)
	time.Sleep(20 * time.Millisecond)
	return c.inner.Extract(data, ct)
}

func TestClassifyExtractionError_LimitsMappedCorrectly(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   string
	}{
		{"compressed_size", &extractionLimitError{Kind: "compressed_size"}, ReasonExtractionLimitCompressedSize},
		{"decompressed_size", &extractionLimitError{Kind: "decompressed_size"}, ReasonExtractionLimitDecompressedSize},
		{"part_size", &extractionLimitError{Kind: "part_size"}, ReasonExtractionLimitPartSize},
		{"part_count", &extractionLimitError{Kind: "part_count"}, ReasonExtractionLimitPartCount},
		{"markdown_size", &extractionLimitError{Kind: "markdown_size"}, ReasonExtractionLimitMarkdownSize},
		{"xml_depth", &extractionLimitError{Kind: "xml_depth"}, ReasonExtractionLimitXMLDepth},
		{"zip_nested", &extractionLimitError{Kind: "zip_nested"}, ReasonExtractionLimitZipNested},
		{"zip_path", &extractionLimitError{Kind: "zip_path"}, ReasonExtractionLimitZipPath},
		{"compression_ratio", &extractionLimitError{Kind: "compression_ratio"}, ReasonExtractionLimitCompressionRatio},
		{"malformed", errors.New("wrap: " + ErrMalformed.Error()), ReasonExtractionMalformed},
		{"unsupported", errors.New("wrap: " + ErrUnsupported.Error()), ReasonExtractionUnsupported},
		{"context.DeadlineExceeded", context.DeadlineExceeded, ReasonExtractionLimitTimeout},
		{"internal", errors.New("random failure"), ReasonExtractionInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ClassifyExtractionError(c.err)
			assert.Equal(t, AccessStatusExtractionFailed, r.Status)
			assert.Equal(t, c.want, r.ReasonCode)
		})
	}
}
```

Note: the `errors.New("wrap: " + ErrMalformed.Error())` cases above wrap by *string*, not by `%w`. Adjust the tests to use `fmt.Errorf("...: %w", ErrMalformed)` so the underlying classification is via `errors.Is`. The intent is to confirm that classification follows `errors.Is` chains.

- [ ] **Step 5: Run integration tests to confirm they fail**

Run: `go test ./api/ -run "TestContentPipeline_|TestClassifyExtractionError_" -v -count=1`
Expected: FAIL — undefined symbols.

- [ ] **Step 6: Implement pipeline changes**

Edit `api/content_pipeline.go`:

```go
// PipelineLimits is the subset of ContentExtractorsConfig the pipeline needs
// directly (not just the registered extractors). Today this is just the
// wall-clock budget; bringing in others as needed.
type PipelineLimits struct {
	WallClockBudget time.Duration
}

// DefaultPipelineLimits returns the design-spec default budget; used by tests.
func DefaultPipelineLimits() PipelineLimits {
	return PipelineLimits{WallClockBudget: 30 * time.Second}
}

// NewContentPipelineWithLimiter wires a per-user concurrency limiter and a
// pipeline-level wall-clock budget into the existing pipeline. The legacy
// NewContentPipeline constructor remains for callers that don't need either.
func NewContentPipelineWithLimiter(
	sources *ContentSourceRegistry,
	extractors *ContentExtractorRegistry,
	matcher *URLPatternMatcher,
	limiter *concurrencyLimiter,
	limits PipelineLimits,
) *ContentPipeline {
	p := NewContentPipeline(sources, extractors, matcher)
	p.limiter = limiter
	p.limits = limits
	return p
}

// (extend the ContentPipeline struct fields)
type ContentPipeline struct {
	// ... existing fields ...
	limiter *concurrencyLimiter
	limits  PipelineLimits
}

// Replace Extract with a version that:
//   1. resolves userID from ctx (UserIDFromContext)
//   2. acquires the limiter (if non-nil) and defers release
//   3. runs the matched extractor under extractWithDeadline if Bounded
//   4. classifies typed errors via ClassifyExtractionError on failure
//
// On classification, the pipeline returns the ORIGINAL error to the caller
// — the caller (handler / poller) is responsible for calling
// UpdateAccessStatusWithDiagnostics with the classified result. This keeps
// the pipeline pure (no DB writes), matching how the rest of the codebase
// handles the documents store.

func (p *ContentPipeline) Extract(ctx context.Context, uri string) (ExtractedContent, error) {
	logger := slogging.Get()

	src, ok := p.sources.FindSource(ctx, uri)
	if !ok {
		return ExtractedContent{}, fmt.Errorf("no content source can handle URI: %s", uri)
	}

	userID, _ := UserIDFromContext(ctx)
	if p.limiter != nil && userID != "" {
		release, err := p.limiter.acquire(ctx, userID)
		if err != nil {
			return ExtractedContent{}, err
		}
		defer release()
	}

	logger.Debug("ContentPipeline: fetching %s via source %s", uri, src.Name())
	data, contentType, err := src.Fetch(ctx, uri)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("source %s fetch failed: %w", src.Name(), err)
	}

	ext, ok := p.extractors.FindExtractor(contentType)
	if !ok {
		return ExtractedContent{Text: string(data), ContentType: contentType}, nil
	}

	logger.Debug("ContentPipeline: extracting %s via extractor %s", contentType, ext.Name())

	if be, ok := ext.(BoundedExtractor); ok && be.Bounded() && p.limits.WallClockBudget > 0 {
		return extractWithDeadline(ctx, p.limits.WallClockBudget, func(_ context.Context) (ExtractedContent, error) {
			return ext.Extract(data, contentType)
		})
	}
	return ext.Extract(data, contentType)
}

// ExtractionClassification describes how a typed extractor error maps to
// access_status + access_reason_code.
type ExtractionClassification struct {
	Status     string
	ReasonCode string
}

// ClassifyExtractionError walks the error chain and returns the matching
// status + reason. Default is internal.
func ClassifyExtractionError(err error) ExtractionClassification {
	if err == nil {
		return ExtractionClassification{}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitTimeout}
	}
	var le *extractionLimitError
	if errors.As(err, &le) {
		switch le.Kind {
		case "compressed_size":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitCompressedSize}
		case "decompressed_size":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitDecompressedSize}
		case "part_size":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitPartSize}
		case "part_count":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitPartCount}
		case "markdown_size":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitMarkdownSize}
		case "xml_depth":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitXMLDepth}
		case "zip_nested":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitZipNested}
		case "zip_path":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitZipPath}
		case "compression_ratio":
			return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionLimitCompressionRatio}
		}
	}
	if errors.Is(err, ErrMalformed) {
		return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionMalformed}
	}
	if errors.Is(err, ErrUnsupported) {
		return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionUnsupported}
	}
	return ExtractionClassification{Status: AccessStatusExtractionFailed, ReasonCode: ReasonExtractionInternal}
}
```

- [ ] **Step 7: Run integration tests**

Run: `go test ./api/ -run "TestContentPipeline_|TestClassifyExtractionError_" -v -count=1 -race`
Expected: PASS, no races.

- [ ] **Step 8: Wire callers to call ClassifyExtractionError + UpdateAccessStatusWithDiagnostics**

Find the existing extractor-failure paths. Two relevant call sites:
- `api/access_poller.go` — when polling pending docs and an extraction is attempted.
- `api/document_sub_resource_handlers.go` — when extraction is attempted at attach time.

For each call site, on `Extract` returning a non-nil error, call `ClassifyExtractionError(err)`, then `UpdateAccessStatusWithDiagnostics(ctx, docID, classified.Status, contentSourceName, classified.ReasonCode, "")` (Detail kept empty; the Kind is already in the reason code). Existing tests for those files must still pass.

- [ ] **Step 9: Run all api tests**

Run: `make test-unit name=TestContentPipeline`; `make test-unit name=TestAccessPoller`; `make test-unit name=TestDocument`
Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add api/content_pipeline.go api/access_diagnostics.go api/api.go api-schema/tmi-openapi.json api/ooxml_extractors_integration_test.go api/access_poller.go api/document_sub_resource_handlers.go
git commit -m "feat(api): pipeline concurrency gate + deadline + extraction-failed classification"
```

---

## Task 14: Server wiring — register extractors and limiter

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add user-override lookup function**

In the file where the `User` GORM model is queried (search `find . -name "user_repository.go" -type f` if present, else `auth/repository/user_repository.go` analog). Add a method returning `(int, error)` for `extraction_concurrency_override`. If the value is NULL, return `(0, nil)`. If no such repo exists, do this lookup inline in main.go using `gormDB.DB().Raw("SELECT extraction_concurrency_override FROM users WHERE internal_uuid = ?", userID).Scan(&v)`.

- [ ] **Step 2: Wire the limiter into the pipeline**

Replace the existing `NewContentPipeline(...)` call:

```go
// Before
pipeline := api.NewContentPipeline(contentSources, contentExtractors, api.NewURLPatternMatcher())

// After
extractorCfg := cfg.ContentExtractors
if err := extractorCfg.Validate(); err != nil {
	logger.Error("Invalid content_extractors config: %v", err)
	os.Exit(1)
}

limits := api.OOXMLLimitsFromConfig(extractorCfg) // helper added in Task 14 step 3

contentExtractors.Register(api.NewDOCXExtractor(limits))
contentExtractors.Register(api.NewPPTXExtractor(limits))
contentExtractors.Register(api.NewXLSXExtractor(limits))

cl := api.NewConcurrencyLimiter(extractorCfg.PerUserConcurrencyDefault, func(ctx context.Context, userID string) (int, error) {
	var override sql.NullInt64
	row := gormDB.DB().WithContext(ctx).Raw(
		"SELECT extraction_concurrency_override FROM " + db.TablePrefix() + "users WHERE internal_uuid = ?", userID,
	).Row()
	if err := row.Scan(&override); err != nil {
		return 0, err
	}
	if !override.Valid {
		return 0, nil
	}
	return int(override.Int64), nil
})

pipeline := api.NewContentPipelineWithLimiter(
	contentSources, contentExtractors, api.NewURLPatternMatcher(),
	cl,
	api.PipelineLimits{WallClockBudget: extractorCfg.WallClockBudget},
)
```

`db.TablePrefix()` placeholder — use whatever the existing code uses for table-prefix-aware raw SQL. If unprefixed, simplify to `"users"`.

- [ ] **Step 3: Add helper to project config -> ooxmlLimits**

In `api/content_extractor_ooxml_common.go`, add a public constructor (since `internal/config` cannot import `api`, this maps the other way):

```go
// OOXMLLimitsFromConfig builds an ooxmlLimits from a ContentExtractorsConfig.
// Defined here (not in internal/config) to keep config decoupled from api.
func OOXMLLimitsFromConfig(c config.ContentExtractorsConfig) ooxmlLimits {
	return ooxmlLimits{
		CompressedSizeBytes:   c.CompressedSizeBytes,
		DecompressedSizeBytes: c.DecompressedSizeBytes,
		PartSizeBytes:         c.PartSizeBytes,
		MarkdownSizeBytes:     c.MarkdownSizeBytes,
		MaxXMLElementDepth:    100,
		MaxCompressionRatio:   100,
	}
}

// NewConcurrencyLimiter is the public constructor used by main.go.
func NewConcurrencyLimiter(fallback int, lookup func(ctx context.Context, userID string) (int, error)) *concurrencyLimiter {
	return newConcurrencyLimiter(fallback, lookup)
}
```

`config` import path: `github.com/ericfitz/tmi/internal/config`. Note this is a one-way dependency (api -> config) which already exists for other config-typed parameters in the package, so no layering violation.

- [ ] **Step 4: Verify server builds and starts**

Run: `make build-server`
Expected: clean build.

Run (in a separate shell): `make start-dev`. Verify the server log shows the three new extractors registered. Stop with `make stop-server`.

- [ ] **Step 5: Commit**

```bash
git add cmd/server/main.go api/content_extractor_ooxml_common.go
git commit -m "feat(server): register OOXML extractors with per-user concurrency limiter"
```

---

## Task 15: Real-corpus build-tagged test target

**Files:**
- Create: `api/ooxml_extractors_corpus_test.go` (build-tagged `corpus`)
- Create: `testdata/ooxml-corpus/<sample>.docx` and `<sample>.expected.md` siblings, similarly for pptx, xlsx
- Modify: `Makefile` (add `test-corpus-ooxml` target)

- [ ] **Step 1: Add a tiny Makefile target**

Append to `Makefile`:

```makefile
.PHONY: test-corpus-ooxml
test-corpus-ooxml: ## Run real-document OOXML extractor corpus tests
	@echo "Running OOXML corpus tests..."
	go test -tags=corpus ./api -run TestOOXMLCorpus -v
```

- [ ] **Step 2: Write the corpus test**

Create `api/ooxml_extractors_corpus_test.go`:

```go
//go:build corpus

package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOOXMLCorpus(t *testing.T) {
	dir := "../testdata/ooxml-corpus"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("corpus dir not found: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".expected.md") {
			continue
		}
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			require.NoError(t, err)

			var ct string
			var ext ContentExtractor
			switch {
			case strings.HasSuffix(name, ".docx"):
				ct = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
				ext = NewDOCXExtractor(defaultOOXMLLimits())
			case strings.HasSuffix(name, ".pptx"):
				ct = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
				ext = NewPPTXExtractor(defaultOOXMLLimits())
			case strings.HasSuffix(name, ".xlsx"):
				ct = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
				ext = NewXLSXExtractor(defaultOOXMLLimits())
			default:
				t.Skipf("unrecognized extension: %s", name)
			}

			out, err := ext.Extract(data, ct)
			require.NoError(t, err)

			expected, err := os.ReadFile(filepath.Join(dir, name+".expected.md"))
			require.NoError(t, err, "missing .expected.md sibling")

			if string(expected) != out.Text {
				t.Errorf("corpus mismatch for %s:\n--- expected ---\n%s\n--- got ---\n%s",
					name, string(expected), out.Text)
			}
		})
	}
}
```

- [ ] **Step 3: Add seed corpus files**

Place a small DOCX, PPTX, and XLSX in `testdata/ooxml-corpus/` along with hand-curated `.expected.md` siblings. Each fixture should exercise something the inline-zip tests cannot: real Word numbering quirks, real PowerPoint shape graph, real Excel shared strings + number formats. Files should be small (< 50 KB each) so the repo footprint stays modest.

If you don't have suitable real samples on hand, generate them with LibreOffice (`soffice --headless --convert-to docx input.txt`) and trim. **Do not commit any document containing PII or proprietary content.**

- [ ] **Step 4: Verify corpus test runs**

Run: `make test-corpus-ooxml`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add api/ooxml_extractors_corpus_test.go testdata/ooxml-corpus Makefile
git commit -m "test(api): real-corpus OOXML extractor tests (build-tagged)"
```

---

## Task 16: Operator wiki page

**Files:**
- (no repo file; new wiki page on https://github.com/ericfitz/tmi/wiki)

- [ ] **Step 1: Draft the wiki page**

Title: `Operator › Content Extractors — Limits and Overrides`

Outline:
1. Overview — what OOXML extractors do and when they run
2. Default limits table (compressed, decompressed, part, slides, cells, markdown, wall clock, per-user concurrency)
3. Hardcoded ceiling table (server-side; not operator-tunable)
4. Environment variable reference (`TMI_CONTENT_EXTRACTORS_*`) with corresponding YAML keys
5. Per-user override workflow:
   - SQL: `UPDATE users SET extraction_concurrency_override = 8 WHERE internal_uuid = '...';`
   - When to use (trusted machine accounts only)
   - How to revert: `UPDATE users SET extraction_concurrency_override = NULL WHERE ...;`
6. Failure-classification reference: list of `access_reason_code` values the extractor pipeline emits and what each means
7. Troubleshooting: how to read `documents.access_status = 'extraction_failed'` rows + the `access_reason_code` to diagnose extraction failures

- [ ] **Step 2: Publish to wiki**

Per `CLAUDE.md`: do **not** create a markdown file under `docs/`. Create the page directly on the wiki (manual UI step or via `gh` if a wiki API path is set up — the project uses manual wiki editing today).

- [ ] **Step 3: Note completion in this plan but do not commit a doc file**

There is no commit to make for this task — wiki edits live outside the repo.

---

## Task 17: Final verification gates

**Files:** none (gate run only)

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: 0 issues introduced (existing 338 issues in auto-generated `api/api.go` are acceptable per `CLAUDE.md`).

- [ ] **Step 2: OpenAPI validation**

Run: `make validate-openapi`
Expected: 0 errors.

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: clean build.

- [ ] **Step 4: Unit tests**

Run: `make test-unit`
Expected: all pass. Capture any new test counts vs baseline.

- [ ] **Step 5: Integration tests (PostgreSQL)**

Run: `make test-integration`
Expected: pass (modulo the four pre-existing baseline failures noted in #249's prior comment if they are still flaky on `dev/1.4.0`; do not introduce new failures).

- [ ] **Step 6: Unsafe-union-method lint**

Run: `make check-unsafe-union-methods`
Expected: clean.

- [ ] **Step 7: Confirm build is CGO_ENABLED=0 clean**

Run: `CGO_ENABLED=0 make build-server`
Expected: clean.

- [ ] **Step 8: Manual smoke**

Run: `make start-dev`. Attach a small DOCX to a threat model via the API (use `curl` with a Bearer JWT obtained from `make start-oauth-stub`). Verify in the document GET response that `access_status` becomes `accessible` and the extracted markdown is stored. Stop with `make stop-server`.

- [ ] **Step 9: Update Issue #287**

Per `CLAUDE.md` workflow:

```bash
gh issue comment 287 --body "Implemented in commits <sha-list> on dev/1.4.0. See plan: docs/superpowers/plans/2026-04-30-ooxml-extractors.md."
gh issue close 287
```

(Auto-close from `Closes #287` does not fire on `dev/1.4.0` because the auto-close trigger is the default branch only.)

---

## Self-review checklist

**Spec coverage** (each spec line item → task that implements it):

- All three extractors registered + selected by MIME → Task 14 (registration) + Tasks 10/11/12 (`CanHandle`)
- Markdown-flavored output per format → Tasks 10/11/12
- PPTX shape-role HTML comments → Task 11 element-coverage tests
- XLSX header detection (4-rule + content fallback) → Task 12 header tests
- Speaker notes / alt text on/off → Tasks 11 / 10
- All 8 limit kinds enforced → Tasks 7 (zip), 8 (xml), 6 (markdown), 13 (timeout), 12 (cell count), 11 (slide count); per-user concurrency Task 9
- Trips return typed errors → Task 6 (errors), Task 13 (classification)
- Hardcoded ceilings + configurable defaults + env vars + startup validation → Task 2
- Per-user override column + GORM model + Oracle review → Task 4
- Zip-nesting refused, path traversal rejected, XML depth, compression-ratio → Task 7 + Task 8
- No CGO clean → Task 1 step 2 + Task 17 step 7
- Unit tests per format → Tasks 10/11/12
- Integration test → Task 13 step 4
- Real-corpus test target → Task 15
- Operator wiki page → Task 16
- All make-target gates clean → Task 17
- Closes #287 manual close → Task 17 step 9

**Placeholder scan:** Tasks 10/11/12 explicitly list element coverage tests by name (no "etc."). Task 14 has placeholder text for `db.TablePrefix()` — the engineer must replace it with the actual project usage. Task 16 lists wiki page sections by name. **No `TBD` / `TODO: figure out later` remains.**

**Type consistency:** `ooxmlLimits` is referenced consistently across Tasks 6–14. `ExtractionClassification.Status` and `.ReasonCode` are declared in Task 13 step 6 and used in Task 13 steps 4, 8. `concurrencyLimiter` is the unexported type; `NewConcurrencyLimiter` is the exported constructor added in Task 14 step 3 (`newConcurrencyLimiter` for tests in Task 9). `BoundedExtractor.Bounded()` signature is `() bool` everywhere.

**Known mismatch with spec called out for the engineer up front:** AutoMigrate vs SQL migrations (Task 4), `extraction_failure_reason` vs `access_reason_code` (pre-flight notes), enum extension (Task 13 step 1).
