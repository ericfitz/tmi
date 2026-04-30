# OOXML Content Extractors — DOCX, PPTX, XLSX

**Issue:** [#287](https://github.com/ericfitz/tmi/issues/287)
**Date:** 2026-04-29
**Status:** Design

## Overview

Implement three `ContentExtractor` plugins for OOXML document formats — Word
(`.docx`), PowerPoint (`.pptx`), and Excel (`.xlsx`) — so that documents
fetched from any `ContentSource` can be turned into structured Markdown text
suitable for downstream chunking and embedding generation.

Output is **Markdown-flavored text** rather than plain text, so document
structure (headings, slides, sheets, tables, lists) is preserved without
extending the existing `ExtractedContent` type. The downstream chunker
interprets Markdown sectioning to attach heading-path / slide / sheet
metadata to each chunk.

## Decomposition

This issue is the parent for three closely related extractors that share
infrastructure (zip-bounded reader, XML depth-limited decoder, limit error
hierarchy, concurrency limiter). The three extractors land together because
they share that infrastructure and because the downstream consumer (#283
upgrades Google Drive exports to OOXML) needs all three formats to ship
together.

| Sub-component | Lives in | Owns |
|---|---|---|
| Shared OOXML scaffolding | `api/content_extractor_ooxml_common.go` | Bounded zip reader, bounded XML decoder, typed error hierarchy, deadline wrapper, concurrency limiter |
| DOCX extractor | `api/content_extractor_docx.go` | Word document body + footnotes + numbering |
| PPTX extractor | `api/content_extractor_pptx.go` | Slides, speaker notes, shape roles |
| XLSX extractor | `api/content_extractor_xlsx.go` | Sheets, header-row detection, cell-type rendering (uses `excelize/v2`) |

Follow-up issues already filed:
- **#337** — dev-mode flag to dump extracted markdown into a Note for
  developer inspection. Depends on this issue.

Downstream consumer:
- **#283** — Google Drive sources export Docs/Slides/Sheets as OOXML.
  Blocked on this issue landing.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Output format | Markdown-flavored text in existing `ExtractedContent.Text` | No interface change. Markdown vocabulary (`#`, `-`, `\|`) covers every structure cue these formats produce. Embedding tools natively understand markdown chunking. |
| Library strategy | Hand-rolled DOCX/PPTX (`archive/zip` + `encoding/xml`); `github.com/xuri/excelize/v2` for XLSX | DOCX/PPTX text extraction is a few hundred lines each. XLSX is the format that genuinely needs a library — shared strings, number formats, date serial numbers, formulas with cached values. excelize is pure-Go MIT, same dependency category as the existing `ledongthuc/pdf`. |
| Format scope | DOCX + PPTX + XLSX in this issue | All three are OOXML, share infrastructure, and #283 depends on all three. Splitting would serialize two Must-Have issues. |
| Limit configuration | Hardcoded ceiling `const`s + configurable defaults via config file/env | Ceilings protect the server (server-only concern, not operator-tunable). Defaults are operator-tunable up to the ceiling. |
| Per-user concurrency | Per-user `golang.org/x/sync/semaphore` with optional DB-stored override | Default 2 keeps single-user load bounded. Override column on `users` allows trusted machine accounts to run higher. |
| PPTX shape metadata | HTML comments (`<!-- shape: title -->`) before each shape's content | Markdown-renderer-invisible, downstream-parseable. Roles drawn from `<p:nvSpPr><p:nvPr ph="…"/>`. |
| XLSX header detection | Style fingerprint over first ≤5 rows; content heuristic only as fallback | Real-world spreadsheets reliably style header rows differently. Style signal beats content heuristics. |
| Temp files | None for OOXML (`bytes.NewReader` works directly with `archive/zip.NewReader`) | "Delete temp files" is satisfied by-construction. |

## Architecture

### Components

```
api/
  content_extractor.go               (existing — interface + registry)
  content_pipeline.go                (existing — gains concurrency gate + deadline wrapper)
  content_extractor_ooxml_common.go  (NEW — shared scaffolding)
  content_extractor_docx.go          (NEW)
  content_extractor_pptx.go          (NEW)
  content_extractor_xlsx.go          (NEW)
  content_extractor_*_test.go        (NEW)
  ooxml_extractors_integration_test.go (NEW)
  ooxml_extractors_corpus_test.go    (NEW, build-tagged 'corpus')

config/
  (existing — gains ContentExtractorsConfig section)

cmd/server/main.go
  (existing — registers the three extractors and the concurrency limiter)

auth/migrations/
  NNNN_user_extraction_concurrency.up.sql   (NEW)
  NNNN_user_extraction_concurrency.down.sql (NEW)
```

### Hardcoded ceilings (server-only)

```go
const (
    maxCompressedSizeBytes   = 50 * 1024 * 1024   // 50 MB
    maxDecompressedSizeBytes = 100 * 1024 * 1024  // 100 MB
    maxPartSizeBytes         = 50 * 1024 * 1024   // 50 MB
    maxPPTXSlides            = 250
    maxXLSXCells             = 10000
    maxMarkdownSizeBytes     = 256 * 1024         // 256 KB
    maxWallClockBudget       = 60 * time.Second
    maxPerUserConcurrency    = 16
    maxXMLElementDepth       = 100
    maxCompressionRatio      = 100  // single-member ratio limit
)
```

### Configurable defaults

`config-development.yml`:

```yaml
content_extractors:
  compressed_size_bytes: 20971520     # 20 MB     (≤ 50 MB)
  decompressed_size_bytes: 52428800   # 50 MB     (≤ 100 MB)
  part_size_bytes: 20971520           # 20 MB     (≤ 50 MB)
  pptx_slides: 100                    #           (≤ 250)
  xlsx_cells: 1000                    #           (≤ 10,000)
  markdown_size_bytes: 131072         # 128 KB    (≤ 256 KB)
  wall_clock_budget: 30s              #           (≤ 60s)
  per_user_concurrency_default: 2     #           (≤ 16)
```

Each value also has a `TMI_CONTENT_EXTRACTORS_*` env-var override. Startup
validation: every value must be `> 0` and `≤` the corresponding ceiling, else
fatal.

## Per-format extraction logic

### DOCX

- **MIME**: `application/vnd.openxmlformats-officedocument.wordprocessingml.document`
- Open archive, locate `word/document.xml` (required; absence → `ErrMalformed`).
- Stream via `boundedXMLDecoder`; only act on `w:body` and descendants.
- Element handling, in document order:
  - `w:p`: blank lines around. `w:pPr/w:pStyle` of `Heading1..Heading6` →
    Markdown `#..######`. Else inspect `w:numPr` for list membership: parse
    `word/numbering.xml` once, cache; bullet vs numbered; indent by `w:ilvl`.
  - `w:r/w:t`: concatenate runs. Skip runs in unaccepted `w:ins`; include
    `w:del` only if accepted.
  - `w:tbl/w:tr/w:tc`: render as Markdown table after `</w:tbl>`. Cell text
    only.
  - `w:hyperlink` with `r:id` → resolve via `word/_rels/document.xml.rels`;
    emit `[text](url)`.
  - `w:drawing`: read `wp:docPr` `descr` attribute. Non-empty → emit
    `![alt-text](image-N)` with monotonic `N`. Empty → omit. **No image bytes
    are processed.**
  - `w:headerReference`, `w:footerReference`: skipped.
  - Comments / unaccepted track changes: skipped.
  - `w:footnoteReference id="N"` → `[^N]`. After body, parse
    `word/footnotes.xml` once and emit `### Footnotes` with `[^N]: text`.
- **Title**: first `Heading1` text if present, else `dc:title` from
  `docProps/core.xml`.

### PPTX

- **MIME**: `application/vnd.openxmlformats-officedocument.presentationml.presentation`
- Read `ppt/presentation.xml` for slide order via `p:sldIdLst > r:id` →
  resolve to slide-XML paths via `ppt/_rels/presentation.xml.rels`.
- Per slide:
  - Skip if `<p:sld show="0">`.
  - Slide-count limit checked here (config default; ceiling 250). Trip →
    `ErrPartCount` with `Detail: "slide #N"`.
  - Walk `p:cSld/p:spTree`. For each `p:sp` and `p:graphicFrame`:
    - Determine role from `p:nvSpPr/p:nvPr/p:ph type=…`:
      `title`, `ctr-title`, `body`, `subtitle`, `dt`, `ftr`, `sldNum`.
      No `ph` → fallback by content type: `text-box`, `picture`, `chart`,
      `table`, `diagram`, `group`, `unknown`.
    - Emit `<!-- shape: <role> -->` immediately before content.
    - Walk `p:txBody/a:p/a:r/a:t` for text in document order.
    - Tables (`a:tbl`) render as Markdown tables.
  - First `title`-role shape's text → `## Slide N: <title>`; absent →
    `## Slide N`.
  - Notes slide via `ppt/slides/_rels/slideN.xml.rels` (rel type
    `notesSlide`). If present, emit `### Notes` then walk the
    notes-placeholder shape's text.
- **Title**: first slide's title if present.

### XLSX

- **MIME**: `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`
- Use `excelize.OpenReader(bytes.NewReader(data), excelize.Options{
  UnzipSizeLimit, UnzipXMLSizeLimit})` populated from config.
- Iterate `f.GetSheetList()`. Skip hidden sheets.
- Per sheet:
  - Emit `## Sheet: <name>`.
  - Stream rows via `f.Rows(name)`. Track running cell count toward the
    limit (sum across all sheets).
  - Header detection over first ≤5 non-empty rows:
    - Per-row fingerprint = dominant `(bgColor, fontSize, fontWeight, fontItalic)`.
    - **0–1 row available**: no header (nothing to compare against).
    - **2 rows available**: insufficient signal to apply style rules — fall
      back to the content heuristic only.
    - **3 rows available**: apply only the "header + uniform" rule
      (`row1≠row2`, `row2==row3` → header). Otherwise no header.
    - **4 rows available**: apply "header + uniform" (`row1≠row2`,
      `row2==row3==row4` → header) and "alternating, no header"
      (`row1==row3`, `row2==row4`, `row1≠row2` → no header). Otherwise no header.
    - **5+ rows available**, full ruleset:
      - **Header + uniform**: `row1≠row2`, `row2==row3==row4` → header.
      - **Header + alternating**: `row1≠row2`, `row1≠row3`, `row2==row4`,
        `row3==row5` → header, alternating data.
      - **Alternating, no header**: `row1==row3==row5`, `row2==row4`,
        `row1≠row2` → no header.
    - **Uniform fingerprints across all examined rows** (no styling signal):
      fall back to content heuristic — header iff all row-1 cells
      string-typed AND no duplicates AND row 2 has at least one non-string
      cell.
    - **Anything else** → no header.
  - Render rows. Trim leading/trailing empty rows + columns. Per cell:
    - String → as-is, escape `|` and newlines.
    - Number → `strconv.FormatFloat(v, 'f', -1, 64)`.
    - Date (detected via excelize number-format heuristic) → ISO 8601, with
      time component if format indicates.
    - Bool → `true` / `false`.
    - Formula → cached value if present (`f.GetCellValue` default), else
      `=<formula text>`.
    - Error (`#REF!` etc.) → pass-through.
  - Merged cells (`f.GetMergeCells`): value at top-left, blanks elsewhere.
- **Title**: first sheet's name.

## Output assembly

All three extractors write into a `markdownBuilder` wrapping `bytes.Buffer`
that trips `ErrExtractionLimit{kind: "markdown_size"}` the moment a write
would exceed the configured cap. **No partial output** is returned on trip.

## Error hierarchy

```go
var ErrExtractionLimit = errors.New("extraction limit exceeded")
var ErrMalformed       = errors.New("malformed document")
var ErrUnsupported     = errors.New("unsupported document subformat")

type extractionLimitError struct {
    Kind     string  // compressed_size | decompressed_size | part_size |
                    // part_count | markdown_size | timeout | xml_depth |
                    // zip_nested | zip_path | compression_ratio
    Limit    int64
    Observed int64   // -1 if not measurable (e.g. timeout)
    Detail   string  // optional context: "slide #42", "sheet 'Sales'"
}

func (e *extractionLimitError) Error() string { ... }
func (e *extractionLimitError) Is(target error) bool { return target == ErrExtractionLimit }
func (e *extractionLimitError) Unwrap() error { return ErrExtractionLimit }
```

## Pipeline integration

`ContentPipeline.Extract` (in `api/content_pipeline.go`) gains:

1. **Per-user concurrency gate** before the matched extractor runs:
   `concurrencyLimiter.acquire(ctx, userID)`; defer release.
2. **Per-extraction deadline** for any extractor implementing
   `BoundedExtractor` (`interface{ Bounded() bool }`):
   `extractWithDeadline(ctx, cfg.WallClockBudget, fn)`.
3. **Result classification**:

```
extractor returns err
  ├── errors.Is(err, ErrExtractionLimit)        → status=extraction_failed, reason="limit:<kind>"
  ├── errors.Is(err, ErrMalformed)              → status=extraction_failed, reason="malformed"
  ├── errors.Is(err, ErrUnsupported)            → status=extraction_failed, reason="unsupported"
  ├── errors.Is(err, context.DeadlineExceeded)  → status=extraction_failed, reason="limit:timeout"
  └── default                                   → status=extraction_failed, reason="internal"
```

Existing `documents.extraction_failure_reason` column (added in sub-project 4
of #249) is reused; no new column for it.

## Concurrency limiter

```go
type concurrencyLimiter struct {
    mu       sync.Mutex
    sems     map[uuid.UUID]*semaphore.Weighted
    lookup   func(ctx context.Context, userID uuid.UUID) (int, error)
    fallback int
}

func (cl *concurrencyLimiter) acquire(ctx context.Context, userID uuid.UUID) (release func(), err error) {
    cl.mu.Lock()
    sem, ok := cl.sems[userID]
    if !ok {
        n, _ := cl.lookup(ctx, userID)
        if n <= 0 || n > maxPerUserConcurrency { n = cl.fallback }
        sem = semaphore.NewWeighted(int64(n))
        cl.sems[userID] = sem
    }
    cl.mu.Unlock()
    if err := sem.Acquire(ctx, 1); err != nil { return nil, err }
    return func() { sem.Release(1) }, nil
}
```

Override lookup is cached for 60s in front of the DB to avoid per-extraction
queries.

Known limitations:
- Capacity is captured at first-acquire; an override change doesn't resize an
  existing semaphore until the user's entry ages out / server restarts.
- No global cap. Default 2 + wall-clock budget bound aggregate load at
  current scale.

## Database migration

`auth/migrations/NNNN_user_extraction_concurrency.up.sql`:

```sql
ALTER TABLE users ADD COLUMN extraction_concurrency_override INTEGER NULL;
```

`auth/migrations/NNNN_user_extraction_concurrency.down.sql`:

```sql
ALTER TABLE users DROP COLUMN extraction_concurrency_override;
```

GORM model: `auth.User.ExtractionConcurrencyOverride *int`.

**Oracle review**: required before completion. PG and Oracle both handle
nullable INTEGER cleanly, but the migration goes through the
`oracle-db-admin` subagent per project policy.

## Security hardening

1. **Refuse zip-within-zip** — any member sniffing as a zip header → reject.
2. **Path traversal** — reject any zip member whose name contains `..`, an
   absolute path, or a backslash.
3. **Per-member streaming with running totals** — trip mid-stream rather
   than after `io.Copy`.
4. **Compression-ratio sanity** — single-member decompressed:compressed
   ratio > `maxCompressionRatio` (100:1) → reject.
5. **XML element depth ≤ `maxXMLElementDepth`** (100) — `xml.Decoder`
   doesn't bound nesting natively; `boundedXMLDecoder` counts depth and
   aborts.
6. **Standard library `encoding/xml` only** — Go's stdlib is XXE-safe by
   default (no DTDs, no entity expansion).
7. **No external network** from any extractor — bytes in, text out.
8. **Temp-file discipline** — OOXML never writes temp files (`bytes.NewReader`
   works directly with `archive/zip.NewReader`). PDF discipline is unchanged.
9. **Wall-clock enforcement** via `context.WithTimeout` + a `ctxReader`
   wrapping every member-stream read.
10. **No CGO** — `CGO_ENABLED=0` build clean.

## Data flow

### Happy path

```
caller (poller / handler)
        │
        │ ContentPipeline.Extract(ctx, doc)
        ▼
ContentPipeline
        │  1. resolve userID from ctx
        │  2. concurrencyLimiter.acquire(ctx, userID)   ◄── may block
        │     (defer release)
        │  3. ContentSource.Fetch(ctx, doc.URI)
        │  4. registry.FindExtractor(contentType)
        │  5. if BoundedExtractor → extractWithDeadline(...) else direct call
        │
        ▼
*OOXMLExtractor (DOCX|PPTX|XLSX)
        │  a. ooxmlOpener.open(bytes)                    ◄── compressed-size + path checks
        │  b. per-member openBounded(maxPartSize)        ◄── per-part + ratio + running total
        │  c. boundedXMLDecoder over the part            ◄── depth ≤ 100
        │  d. write into markdownBuilder                 ◄── trips at maxMarkdownSize
        │  e. return ExtractedContent
        ▼
ContentPipeline (resumes)
        │  6. on success: caller persists ExtractedContent.Text
        │  7. on error: classify + set access-status reason
        │  8. release semaphore (deferred)
        ▼
caller — receives result or typed error
```

### Deadline wrapper

```go
func extractWithDeadline(ctx context.Context, budget time.Duration, fn func(context.Context) (ExtractedContent, error)) (ExtractedContent, error) {
    ctx, cancel := context.WithTimeout(ctx, budget)
    defer cancel()

    type result struct{ c ExtractedContent; e error }
    ch := make(chan result, 1)
    go func() { c, e := fn(ctx); ch <- result{c, e} }()

    select {
    case r := <-ch:           return r.c, r.e
    case <-ctx.Done():        return ExtractedContent{}, ctx.Err()
    }
}

type ctxReader struct{ r io.Reader; ctx context.Context }
func (c *ctxReader) Read(p []byte) (int, error) {
    if err := c.ctx.Err(); err != nil { return 0, err }
    return c.r.Read(p)
}
```

## Testing

### Unit tests (table-driven, inline-bytes, testify)

Each extractor's `*_test.go` exercises:

- **DOCX**: empty, single paragraph, all heading levels, lists (bullet,
  numbered, nested), tables (with/without header), accepted/unaccepted
  revisions, footnotes, alt-text on/off, hyperlink resolution,
  header/footer/comment exclusion, malformed inputs (missing parts, bad
  XML), every limit trip, every security gate.
- **PPTX**: single slide, multi-slide order preservation, every shape
  role, text-box fallback, speaker notes, hidden-slide skip, slide tables,
  alt-text counter across slides, slide-count trip with `Detail`,
  malformed (missing `presentation.xml`, bad rel ref), every security gate.
- **XLSX**: empty workbook, single value, multi-sheet ordering, hidden
  sheet skip, all four header-detection rules + content-heuristic fallback,
  every cell type (number, date, bool, error, string), formula cached vs
  uncached, merged cells, trimming, cell-count trip, every security gate.
- **Common scaffolding**: `boundedXMLDecoder` depth trip,
  `extractWithDeadline` happy + timeout + parent-cancel paths,
  `concurrencyLimiter` blocking + release behavior + lookup-error fallback.

Inline OOXML built via small `buildOOXML(t, parts map[string][]byte) []byte`
helper on top of `archive/zip.NewWriter`. Real specimens live in `testdata/`
only when inline construction is impractical (e.g. footnote rendering across
real Word's numbering quirks).

### Integration tests (`api/ooxml_extractors_integration_test.go`)

End-to-end through `ContentPipeline.Extract` with a stub `ContentSource`
returning curated bytes. Verifies extractor selection, markdown propagation,
and limit-error mapping to `documents.extraction_failure_reason`. Per-user
concurrency: 4 simultaneous extracts on a default-2 user → exactly 2
observed concurrent invocations.

### Real-corpus tests (`api/ooxml_extractors_corpus_test.go`)

Build-tagged `corpus`, opt-in via `make test-corpus-ooxml`. Seed corpus in
`testdata/ooxml-corpus/` with a sibling `.expected.md` per file; tests do
byte-for-byte comparison and fail with a unified diff. One file per format
covering: normal document, with tables, with images, large-but-legitimate
adversarial input.

### CATS

OOXML formats aren't directly fuzzed by CATS (OpenAPI fuzzer, not binary).
The `POST /threat_models/{id}/documents` endpoint that triggers extraction
is already CATS-fuzzed; we verify those response classifications stay
clean. No new CATS work in scope.

## Definition of done

- [ ] All three extractors registered and selected by MIME via the
      existing registry.
- [ ] DOCX, PPTX, XLSX produce Markdown-flavored text per the rules above.
- [ ] PPTX shape-role HTML comments emitted; XLSX header detection follows
      the four-rule fingerprint algorithm; speaker notes included; alt-text
      emitted only when present.
- [ ] All eight limits + per-user concurrency enforced; trips return typed
      errors mapped to access-status `extraction_failed` rows with the
      correct `reason`.
- [ ] Hardcoded `const` ceilings; configurable defaults via
      `config-development.yml` + `TMI_CONTENT_EXTRACTORS_*`; startup
      validation refuses out-of-bound config.
- [ ] Per-user concurrency override column added via migration;
      `oracle-db-admin` subagent reviews + signs off.
- [ ] Zip-nesting refused; path traversal rejected; XML depth bounded;
      compression-ratio sanity check.
- [ ] No CGO; `CGO_ENABLED=0` build clean.
- [ ] Unit tests as listed.
- [ ] Integration test as listed.
- [ ] Real-corpus test target with seed corpus.
- [ ] Operator wiki page on the GitHub Wiki (limits, env vars, user-override
      admin workflow).
- [ ] `make lint`, `make build-server`, `make test-unit`,
      `make test-integration`, `make validate-openapi` all clean.
- [ ] Final commit references `Closes #287` and is merged into `dev/1.4.0`.
      Issue closed manually via `gh issue close 287` (auto-close fires only
      from `main`).

## Out of scope

- Editing or generation of OOXML files.
- Legacy Office formats (`.doc`, `.ppt`, `.xls`).
- Format conversion (e.g. DOCX → PDF).
- Image bytes / OCR — only `descr` alt-text captured.
- Charts / SmartArt / equations beyond the shape's text body.
- Comments and unaccepted track-changes (review metadata, not document
  content).
- DOCX headers / footers / watermarks.
- PPTX slide layouts and slide masters.
- XLSX hidden sheets, frozen panes, conditional formatting, charts.
- Dev-mode "save markdown as Note" inspection feature → tracked as **#337**.
- Global concurrency cap → known follow-up.
- Hot-resize of per-user semaphore on override change → known follow-up.

## Known follow-ups

| Item | Trigger to file |
|---|---|
| Global extraction concurrency cap | If a deployment shape has many users extracting concurrently |
| Hot-resize of per-user semaphore on override change | If overrides start changing frequently for the same user |
| OCR over embedded images | If/when scanned-document searchability is needed |
| `.xls` / `.doc` / `.ppt` legacy support | If/when a customer has only legacy files |
| Chart / SmartArt extraction | If embedding quality demands it |

## References

- [Issue #287](https://github.com/ericfitz/tmi/issues/287)
- [Issue #249](https://github.com/ericfitz/tmi/issues/249) (parent)
- [Issue #283](https://github.com/ericfitz/tmi/issues/283) (downstream consumer)
- [Issue #337](https://github.com/ericfitz/tmi/issues/337) (dev-mode Note dump)
- [Issue #232](https://github.com/ericfitz/tmi/issues/232) (extractor pipeline)
- [Confluence delegated provider design (2026-04-25)](2026-04-25-confluence-delegated-provider-design.md)
- [`github.com/xuri/excelize/v2`](https://github.com/qax-os/excelize)
- ECMA-376 Office Open XML File Formats (5th ed.) — DOCX, PPTX, XLSX schemas
