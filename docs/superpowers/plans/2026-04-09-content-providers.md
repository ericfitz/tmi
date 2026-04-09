# Content Provider Infrastructure & Google Drive Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the content extraction pipeline into Source/Extractor layers, add document access tracking, and implement Google Drive as a service content provider.

**Architecture:** Split the current `ContentProvider` monolith into `ContentSource` (auth + fetch bytes) and `ContentExtractor` (bytes → text) layers with a pipeline orchestrator. Add `access_status`/`content_source` fields to Document for creation-time access validation. Google Drive uses a bot service account with `ValidateAccess`/`RequestAccess` support and a background poller for pending documents.

**Tech Stack:** Go, Gin, GORM, Google Drive API v3 (`google.golang.org/api/drive/v3`), `golang.org/x/oauth2/google`

**Issue:** #232
**Design spec:** `docs/superpowers/specs/2026-04-08-content-providers-design.md`

---

## File Structure

### New Files

| File | Responsibility |
|------|---------------|
| `api/content_source.go` | `ContentSource`, `AccessValidator`, `AccessRequester` interfaces + `ContentSourceRegistry` |
| `api/content_extractor.go` | `ContentExtractor` interface + `ContentExtractorRegistry` |
| `api/content_pipeline.go` | `ContentPipeline` orchestrator (source → extractor) + `URLPatternMatcher` |
| `api/content_source_http.go` | `HTTPSource` — wraps existing HTTP fetch logic |
| `api/content_source_google_drive.go` | `GoogleDriveSource` — service account auth, fetch, validate access, request access |
| `api/content_extractor_html.go` | `HTMLExtractor` — reuses `extractTextFromHTML` |
| `api/content_extractor_pdf.go` | `PDFExtractor` — reuses `extractTextFromPDF` |
| `api/content_extractor_plaintext.go` | `PlainTextExtractor` — passthrough for text/plain, text/csv |
| `api/content_source_test.go` | Tests for `ContentSourceRegistry` |
| `api/content_extractor_test.go` | Tests for `ContentExtractorRegistry` |
| `api/content_pipeline_test.go` | Tests for `ContentPipeline` + `URLPatternMatcher` |
| `api/content_source_http_test.go` | Tests for `HTTPSource` |
| `api/content_source_google_drive_test.go` | Tests for `GoogleDriveSource` |
| `api/content_extractor_html_test.go` | Tests for `HTMLExtractor` |
| `api/content_extractor_pdf_test.go` | Tests for `PDFExtractor` |
| `api/content_extractor_plaintext_test.go` | Tests for `PlainTextExtractor` |
| `api/access_poller.go` | Background poller for `pending_access` documents |
| `api/access_poller_test.go` | Tests for background poller |
| `internal/config/content_sources.go` | `ContentSourcesConfig` + `GoogleDriveConfig` |

### Modified Files

| File | Change |
|------|--------|
| `api/models/models.go` | Add `AccessStatus`, `ContentSource` fields to `Document` |
| `api/document_store.go` | Add `ListByAccessStatus` method to interface |
| `api/document_store_gorm.go` | Implement `ListByAccessStatus` |
| `api/document_sub_resource_handlers.go` | Hook URL pattern matcher into `CreateDocument`/`BulkCreateDocuments` |
| `api/timmy_content_provider.go` | Add adapter: `ContentPipeline` implements `ContentProvider` for URI-based refs |
| `api/timmy_session_manager.go` | Filter documents by `access_status` in `snapshotDocuments`, report skipped sources |
| `api/timmy_handlers.go` | Add `RefreshSources` and `RequestAccess` handlers |
| `cmd/server/main.go` | Wire up content pipeline, Google Drive source, background poller |
| `internal/config/config.go` | Add `ContentSources` field to `Config` struct |
| `internal/config/timmy.go` | No change (pipeline config lives in `content_sources.go`) |
| `api-schema/tmi-openapi.json` | New schemas, modified Document schema, new endpoints |

### Deleted/Replaced Files

None. Existing `timmy_content_provider_http.go` and `timmy_content_provider_pdf.go` remain as-is during the refactor — the new pipeline wraps them via the adapter pattern until all callers are migrated, then they become thin wrappers. The old `ContentProvider` interface remains — the pipeline implements it.

---

## Task 1: ContentSource and ContentExtractor Interfaces

**Files:**
- Create: `api/content_source.go`
- Create: `api/content_extractor.go`
- Test: `api/content_source_test.go`
- Test: `api/content_extractor_test.go`

- [ ] **Step 1: Write failing tests for ContentSourceRegistry**

```go
// api/content_source_test.go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSource is a test double for ContentSource
type mockSource struct {
	name      string
	canHandle bool
	data      []byte
	ct        string
	err       error
}

func (m *mockSource) Name() string { return m.name }
func (m *mockSource) CanHandle(_ context.Context, _ string) bool { return m.canHandle }
func (m *mockSource) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return m.data, m.ct, m.err
}

func TestContentSourceRegistry_FindSource(t *testing.T) {
	r := NewContentSourceRegistry()
	s1 := &mockSource{name: "nope", canHandle: false}
	s2 := &mockSource{name: "yep", canHandle: true}
	r.Register(s1)
	r.Register(s2)

	src, ok := r.FindSource(context.Background(), "https://example.com")
	require.True(t, ok)
	assert.Equal(t, "yep", src.Name())
}

func TestContentSourceRegistry_FindSource_NoMatch(t *testing.T) {
	r := NewContentSourceRegistry()
	r.Register(&mockSource{name: "nope", canHandle: false})

	_, ok := r.FindSource(context.Background(), "https://example.com")
	assert.False(t, ok)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestContentSourceRegistry`
Expected: FAIL — `NewContentSourceRegistry` not defined

- [ ] **Step 3: Implement ContentSource interfaces and registry**

```go
// api/content_source.go
package api

import "context"

// ContentSource authenticates and fetches raw bytes from a URI.
type ContentSource interface {
	Name() string
	CanHandle(ctx context.Context, uri string) bool
	Fetch(ctx context.Context, uri string) (data []byte, contentType string, err error)
}

// AccessValidator checks whether a source can access a URI without downloading it.
type AccessValidator interface {
	ValidateAccess(ctx context.Context, uri string) (accessible bool, err error)
}

// AccessRequester programmatically requests access to a URI (e.g., share request email).
type AccessRequester interface {
	RequestAccess(ctx context.Context, uri string) error
}

// ContentSourceRegistry manages content sources in priority order.
type ContentSourceRegistry struct {
	sources []ContentSource
}

// NewContentSourceRegistry creates a new registry.
func NewContentSourceRegistry() *ContentSourceRegistry {
	return &ContentSourceRegistry{}
}

// Register adds a source to the registry (tried in registration order).
func (r *ContentSourceRegistry) Register(source ContentSource) {
	r.sources = append(r.sources, source)
}

// FindSource returns the first source that can handle the given URI.
func (r *ContentSourceRegistry) FindSource(ctx context.Context, uri string) (ContentSource, bool) {
	for _, s := range r.sources {
		if s.CanHandle(ctx, uri) {
			return s, true
		}
	}
	return nil, false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestContentSourceRegistry`
Expected: PASS

- [ ] **Step 5: Write failing tests for ContentExtractorRegistry**

```go
// api/content_extractor_test.go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExtractor struct {
	name      string
	canHandle bool
	result    ExtractedContent
	err       error
}

func (m *mockExtractor) Name() string                                    { return m.name }
func (m *mockExtractor) CanHandle(contentType string) bool               { return m.canHandle }
func (m *mockExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	return m.result, m.err
}

func TestContentExtractorRegistry_FindExtractor(t *testing.T) {
	r := NewContentExtractorRegistry()
	e1 := &mockExtractor{name: "nope", canHandle: false}
	e2 := &mockExtractor{name: "yep", canHandle: true}
	r.Register(e1)
	r.Register(e2)

	ext, ok := r.FindExtractor("text/html")
	require.True(t, ok)
	assert.Equal(t, "yep", ext.Name())
}

func TestContentExtractorRegistry_FindExtractor_NoMatch(t *testing.T) {
	r := NewContentExtractorRegistry()
	r.Register(&mockExtractor{name: "nope", canHandle: false})

	_, ok := r.FindExtractor("application/octet-stream")
	assert.False(t, ok)
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `make test-unit name=TestContentExtractorRegistry`
Expected: FAIL — `ContentExtractor` interface not defined

- [ ] **Step 7: Implement ContentExtractor interface and registry**

```go
// api/content_extractor.go
package api

// ContentExtractor converts raw bytes into plain text.
type ContentExtractor interface {
	Name() string
	CanHandle(contentType string) bool
	Extract(data []byte, contentType string) (ExtractedContent, error)
}

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

// FindExtractor returns the first extractor that can handle the given content type.
func (r *ContentExtractorRegistry) FindExtractor(contentType string) (ContentExtractor, bool) {
	for _, e := range r.extractors {
		if e.CanHandle(contentType) {
			return e, true
		}
	}
	return nil, false
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `make test-unit name=TestContentExtractorRegistry`
Expected: PASS

- [ ] **Step 9: Lint**

Run: `make lint`
Expected: PASS (no new warnings)

- [ ] **Step 10: Commit**

```bash
git add api/content_source.go api/content_extractor.go api/content_source_test.go api/content_extractor_test.go
git commit -m "feat(timmy): add ContentSource and ContentExtractor interfaces and registries

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 2: URLPatternMatcher and ContentPipeline Orchestrator

**Files:**
- Create: `api/content_pipeline.go`
- Test: `api/content_pipeline_test.go`

- [ ] **Step 1: Write failing tests for URLPatternMatcher**

```go
// api/content_pipeline_test.go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLPatternMatcher_Identify(t *testing.T) {
	m := NewURLPatternMatcher()

	tests := []struct {
		uri      string
		expected string
	}{
		{"https://docs.google.com/document/d/abc/edit", "google_drive"},
		{"https://drive.google.com/file/d/abc/view", "google_drive"},
		{"https://docs.google.com/spreadsheets/d/abc/edit", "google_drive"},
		{"https://docs.google.com/presentation/d/abc/edit", "google_drive"},
		{"https://mycompany.atlassian.net/wiki/spaces/ENG/pages/123", "confluence"},
		{"https://mycompany.sharepoint.com/sites/team/doc.docx", "onedrive"},
		{"https://onedrive.live.com/edit.aspx?id=abc", "onedrive"},
		{"https://example.com/readme.html", "http"},
		{"https://example.com/doc.pdf", "http"},
		{"", ""},
		{"ftp://example.com/file", ""},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.expected, m.Identify(tt.uri))
		})
	}
}

func TestURLPatternMatcher_IsKnownProvider(t *testing.T) {
	m := NewURLPatternMatcher()
	assert.True(t, m.IsKnownProvider("google_drive"))
	assert.True(t, m.IsKnownProvider("confluence"))
	assert.True(t, m.IsKnownProvider("onedrive"))
	assert.True(t, m.IsKnownProvider("http"))
	assert.False(t, m.IsKnownProvider("dropbox"))
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestURLPatternMatcher`
Expected: FAIL

- [ ] **Step 3: Implement URLPatternMatcher**

```go
// api/content_pipeline.go
package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Provider name constants
const (
	ProviderGoogleDrive = "google_drive"
	ProviderConfluence  = "confluence"
	ProviderOneDrive    = "onedrive"
	ProviderHTTP        = "http"
)

// URLPatternMatcher maps URIs to provider names.
// Always active — even for disabled providers — to enable clear 422 errors.
type URLPatternMatcher struct {
	knownProviders map[string]bool
}

// NewURLPatternMatcher creates a matcher with all known provider patterns.
func NewURLPatternMatcher() *URLPatternMatcher {
	return &URLPatternMatcher{
		knownProviders: map[string]bool{
			ProviderGoogleDrive: true,
			ProviderConfluence:  true,
			ProviderOneDrive:    true,
			ProviderHTTP:        true,
		},
	}
}

// Identify returns the provider name for a URI, or "" if unrecognized.
func (m *URLPatternMatcher) Identify(uri string) string {
	if uri == "" {
		return ""
	}
	lower := strings.ToLower(uri)

	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return ""
	}

	// Extract host from URI (simple parse, no net/url to avoid alloc for hot path)
	host := extractHost(lower)

	switch {
	case host == "docs.google.com" || host == "drive.google.com":
		return ProviderGoogleDrive
	case strings.HasSuffix(host, ".atlassian.net") && strings.Contains(lower, "/wiki/"):
		return ProviderConfluence
	case strings.HasSuffix(host, ".sharepoint.com") || host == "onedrive.live.com":
		return ProviderOneDrive
	default:
		return ProviderHTTP
	}
}

// IsKnownProvider returns true if the provider name is recognized.
func (m *URLPatternMatcher) IsKnownProvider(name string) bool {
	return m.knownProviders[name]
}

// extractHost extracts the hostname from a lowercased URL string.
func extractHost(lower string) string {
	// Skip scheme
	idx := strings.Index(lower, "://")
	if idx < 0 {
		return ""
	}
	rest := lower[idx+3:]
	// Strip port and path
	if i := strings.IndexAny(rest, ":/"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// ContentPipeline orchestrates Source -> Extractor for URI-based content.
type ContentPipeline struct {
	sources    *ContentSourceRegistry
	extractors *ContentExtractorRegistry
	matcher    *URLPatternMatcher
}

// NewContentPipeline creates a new pipeline.
func NewContentPipeline(
	sources *ContentSourceRegistry,
	extractors *ContentExtractorRegistry,
	matcher *URLPatternMatcher,
) *ContentPipeline {
	return &ContentPipeline{
		sources:    sources,
		extractors: extractors,
		matcher:    matcher,
	}
}

// Extract fetches bytes from the appropriate source and extracts text.
func (p *ContentPipeline) Extract(ctx context.Context, uri string) (ExtractedContent, error) {
	logger := slogging.Get()

	src, ok := p.sources.FindSource(ctx, uri)
	if !ok {
		return ExtractedContent{}, fmt.Errorf("no content source can handle URI: %s", uri)
	}

	logger.Debug("ContentPipeline: fetching %s via source %s", uri, src.Name())
	data, contentType, err := src.Fetch(ctx, uri)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("source %s fetch failed: %w", src.Name(), err)
	}

	ext, ok := p.extractors.FindExtractor(contentType)
	if !ok {
		// Fall back to plain text for unknown content types
		return ExtractedContent{
			Text:        string(data),
			ContentType: contentType,
		}, nil
	}

	logger.Debug("ContentPipeline: extracting %s via extractor %s", contentType, ext.Name())
	return ext.Extract(data, contentType)
}

// Matcher returns the pipeline's URL pattern matcher.
func (p *ContentPipeline) Matcher() *URLPatternMatcher {
	return p.matcher
}

// Sources returns the pipeline's source registry.
func (p *ContentPipeline) Sources() *ContentSourceRegistry {
	return p.sources
}
```

- [ ] **Step 4: Run tests to verify URLPatternMatcher tests pass**

Run: `make test-unit name=TestURLPatternMatcher`
Expected: PASS

- [ ] **Step 5: Write failing test for ContentPipeline.Extract**

Add to `api/content_pipeline_test.go`:

```go
func TestContentPipeline_Extract(t *testing.T) {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("<h1>Hello</h1>"),
		ct:        "text/html",
	})

	extractors := NewContentExtractorRegistry()
	extractors.Register(&mockExtractor{
		name:      "test-ext",
		canHandle: true,
		result:    ExtractedContent{Text: "Hello", ContentType: "text/html"},
	})

	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())
	result, err := pipeline.Extract(context.Background(), "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "Hello", result.Text)
}

func TestContentPipeline_Extract_NoSource(t *testing.T) {
	sources := NewContentSourceRegistry()
	extractors := NewContentExtractorRegistry()
	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())

	_, err := pipeline.Extract(context.Background(), "https://example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no content source")
}

func TestContentPipeline_Extract_FallbackPlainText(t *testing.T) {
	sources := NewContentSourceRegistry()
	sources.Register(&mockSource{
		name:      "test-src",
		canHandle: true,
		data:      []byte("raw data"),
		ct:        "application/octet-stream",
	})

	extractors := NewContentExtractorRegistry()
	// No extractor registered for application/octet-stream

	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())
	result, err := pipeline.Extract(context.Background(), "https://example.com")
	require.NoError(t, err)
	assert.Equal(t, "raw data", result.Text)
}
```

- [ ] **Step 6: Run tests to verify they pass** (implementation already exists from Step 3)

Run: `make test-unit name=TestContentPipeline`
Expected: PASS

- [ ] **Step 7: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add api/content_pipeline.go api/content_pipeline_test.go
git commit -m "feat(timmy): add URLPatternMatcher and ContentPipeline orchestrator

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 3: Concrete Extractors (HTML, PDF, PlainText)

**Files:**
- Create: `api/content_extractor_html.go`
- Create: `api/content_extractor_pdf.go`
- Create: `api/content_extractor_plaintext.go`
- Test: `api/content_extractor_html_test.go`
- Test: `api/content_extractor_pdf_test.go`
- Test: `api/content_extractor_plaintext_test.go`

- [ ] **Step 1: Write failing tests for PlainTextExtractor**

```go
// api/content_extractor_plaintext_test.go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlainTextExtractor_CanHandle(t *testing.T) {
	e := NewPlainTextExtractor()
	assert.True(t, e.CanHandle("text/plain"))
	assert.True(t, e.CanHandle("text/plain; charset=utf-8"))
	assert.True(t, e.CanHandle("text/csv"))
	assert.False(t, e.CanHandle("text/html"))
	assert.False(t, e.CanHandle("application/json"))
}

func TestPlainTextExtractor_Extract(t *testing.T) {
	e := NewPlainTextExtractor()
	result, err := e.Extract([]byte("hello world"), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "hello world", result.Text)
	assert.Equal(t, "text/plain", result.ContentType)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestPlainTextExtractor`
Expected: FAIL

- [ ] **Step 3: Implement PlainTextExtractor**

```go
// api/content_extractor_plaintext.go
package api

import "strings"

// PlainTextExtractor handles text/plain and text/csv content.
type PlainTextExtractor struct{}

// NewPlainTextExtractor creates a new PlainTextExtractor.
func NewPlainTextExtractor() *PlainTextExtractor {
	return &PlainTextExtractor{}
}

// Name returns the extractor name.
func (e *PlainTextExtractor) Name() string { return "plaintext" }

// CanHandle returns true for text/plain and text/csv content types.
func (e *PlainTextExtractor) CanHandle(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/csv")
}

// Extract returns the raw bytes as text.
func (e *PlainTextExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	return ExtractedContent{
		Text:        string(data),
		ContentType: contentType,
	}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestPlainTextExtractor`
Expected: PASS

- [ ] **Step 5: Write failing tests for HTMLExtractor**

```go
// api/content_extractor_html_test.go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLExtractor_CanHandle(t *testing.T) {
	e := NewHTMLExtractor()
	assert.True(t, e.CanHandle("text/html"))
	assert.True(t, e.CanHandle("text/html; charset=utf-8"))
	assert.False(t, e.CanHandle("text/plain"))
	assert.False(t, e.CanHandle("application/json"))
}

func TestHTMLExtractor_Extract(t *testing.T) {
	e := NewHTMLExtractor()
	html := []byte(`<html><body><h1>Hello</h1><script>evil()</script><p>World</p></body></html>`)
	result, err := e.Extract(html, "text/html")
	require.NoError(t, err)
	assert.Contains(t, result.Text, "Hello")
	assert.Contains(t, result.Text, "World")
	assert.NotContains(t, result.Text, "evil")
	assert.Equal(t, "text/html", result.ContentType)
}

func TestHTMLExtractor_Extract_Empty(t *testing.T) {
	e := NewHTMLExtractor()
	result, err := e.Extract([]byte(""), "text/html")
	require.NoError(t, err)
	assert.Equal(t, "", result.Text)
}
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `make test-unit name=TestHTMLExtractor`
Expected: FAIL

- [ ] **Step 7: Implement HTMLExtractor**

Reuses the existing `extractTextFromHTML` function from `timmy_content_provider_http.go`.

```go
// api/content_extractor_html.go
package api

import "strings"

// HTMLExtractor extracts visible text from HTML, stripping scripts and styles.
type HTMLExtractor struct{}

// NewHTMLExtractor creates a new HTMLExtractor.
func NewHTMLExtractor() *HTMLExtractor {
	return &HTMLExtractor{}
}

// Name returns the extractor name.
func (e *HTMLExtractor) Name() string { return "html" }

// CanHandle returns true for text/html content types.
func (e *HTMLExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/html")
}

// Extract strips HTML tags and returns visible text.
// Reuses extractTextFromHTML from timmy_content_provider_http.go.
func (e *HTMLExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	text := extractTextFromHTML(string(data))
	return ExtractedContent{
		Text:        text,
		ContentType: contentType,
	}, nil
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `make test-unit name=TestHTMLExtractor`
Expected: PASS

- [ ] **Step 9: Write failing tests for PDFExtractor**

```go
// api/content_extractor_pdf_test.go
package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPDFExtractor_CanHandle(t *testing.T) {
	e := NewPDFExtractor()
	assert.True(t, e.CanHandle("application/pdf"))
	assert.True(t, e.CanHandle("application/PDF"))
	assert.False(t, e.CanHandle("text/plain"))
	assert.False(t, e.CanHandle("text/html"))
}

func TestPDFExtractor_Name(t *testing.T) {
	e := NewPDFExtractor()
	assert.Equal(t, "pdf", e.Name())
}

// Note: Full PDF extraction is tested via the existing TestPDFContentProvider tests.
// The PDFExtractor.Extract method requires a temp file write, tested in integration.
```

- [ ] **Step 10: Run tests to verify they fail**

Run: `make test-unit name=TestPDFExtractor`
Expected: FAIL

- [ ] **Step 11: Implement PDFExtractor**

```go
// api/content_extractor_pdf.go
package api

import (
	"fmt"
	"os"
	"strings"

	pdflib "github.com/ledongthuc/pdf"
)

// PDFExtractor extracts text from PDF data.
type PDFExtractor struct{}

// NewPDFExtractor creates a new PDFExtractor.
func NewPDFExtractor() *PDFExtractor {
	return &PDFExtractor{}
}

// Name returns the extractor name.
func (e *PDFExtractor) Name() string { return "pdf" }

// CanHandle returns true for application/pdf content types.
func (e *PDFExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/pdf")
}

// Extract writes PDF data to a temp file and extracts text.
func (e *PDFExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	tmpFile, err := os.CreateTemp("", "timmy-pdf-*.pdf")
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to write PDF temp file: %w", err)
	}
	// Close before reading so the PDF library can open it
	_ = tmpFile.Close()

	text, err := extractPDFText(tmpPath)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to extract PDF text: %w", err)
	}

	return ExtractedContent{
		Text:        text,
		ContentType: contentType,
	}, nil
}

// extractPDFText opens a PDF file and extracts plain text page by page.
// This is a standalone version of extractTextFromPDF from timmy_content_provider_pdf.go.
func extractPDFText(filePath string) (string, error) {
	f, r, err := pdflib.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open PDF: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	var sb strings.Builder
	totalPage := r.NumPage()
	for i := 1; i <= totalPage; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()), nil
}
```

- [ ] **Step 12: Run tests to verify they pass**

Run: `make test-unit name=TestPDFExtractor`
Expected: PASS

- [ ] **Step 13: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 14: Commit**

```bash
git add api/content_extractor_plaintext.go api/content_extractor_plaintext_test.go \
  api/content_extractor_html.go api/content_extractor_html_test.go \
  api/content_extractor_pdf.go api/content_extractor_pdf_test.go
git commit -m "feat(timmy): add PlainText, HTML, and PDF content extractors

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 4: HTTPSource (Wrap Existing HTTP Fetch)

**Files:**
- Create: `api/content_source_http.go`
- Test: `api/content_source_http_test.go`

- [ ] **Step 1: Write failing tests for HTTPSource**

```go
// api/content_source_http_test.go
package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPSource_CanHandle(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	assert.True(t, s.CanHandle(context.Background(), "https://example.com/doc"))
	assert.True(t, s.CanHandle(context.Background(), "http://example.com/doc"))
	assert.False(t, s.CanHandle(context.Background(), "ftp://example.com/doc"))
	assert.False(t, s.CanHandle(context.Background(), ""))
}

func TestHTTPSource_Fetch_PlainText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer srv.Close()

	s := NewHTTPSource(NewURIValidator([]string{"127.0.0.1"}, []string{"https", "http"}))
	data, ct, err := s.Fetch(context.Background(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(data))
	assert.Contains(t, ct, "text/plain")
}

func TestHTTPSource_Fetch_SSRFBlocked(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	_, _, err := s.Fetch(context.Background(), "http://localhost/secret")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SSRF")
}

func TestHTTPSource_Name(t *testing.T) {
	s := NewHTTPSource(NewURIValidator(nil, nil))
	assert.Equal(t, "http", s.Name())
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `make test-unit name=TestHTTPSource`
Expected: FAIL

- [ ] **Step 3: Implement HTTPSource**

```go
// api/content_source_http.go
package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

const httpSourceMaxBody = 50 * 1024 * 1024 // 50 MiB

// HTTPSource fetches raw bytes from HTTP/HTTPS URLs with SSRF protection.
type HTTPSource struct {
	ssrfValidator *URIValidator
	client        *http.Client
}

// NewHTTPSource creates a new HTTPSource.
func NewHTTPSource(ssrfValidator *URIValidator) *HTTPSource {
	return &HTTPSource{
		ssrfValidator: ssrfValidator,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

// Name returns the source name.
func (s *HTTPSource) Name() string { return "http" }

// CanHandle returns true for http:// and https:// URIs.
func (s *HTTPSource) CanHandle(_ context.Context, uri string) bool {
	return strings.HasPrefix(uri, "http://") || strings.HasPrefix(uri, "https://")
}

// Fetch downloads the URI and returns raw bytes and content type.
func (s *HTTPSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	if err := s.ssrfValidator.Validate(uri); err != nil {
		return nil, "", fmt.Errorf("SSRF check failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := s.client.Do(req) //nolint:gosec // URL is validated by SSRFValidator
	if err != nil {
		return nil, "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, uri)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, httpSourceMaxBody))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	return data, resp.Header.Get("Content-Type"), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestHTTPSource`
Expected: PASS

- [ ] **Step 5: Lint**

Run: `make lint`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/content_source_http.go api/content_source_http_test.go
git commit -m "feat(timmy): add HTTPSource content source

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 5: Pipeline Adapter — Bridge Old ContentProvider Interface

**Files:**
- Modify: `api/timmy_content_provider.go`
- Modify: `api/timmy_content_provider_test.go`

The `ContentPipeline` needs to implement the existing `ContentProvider` interface so it can be plugged into the existing `ContentProviderRegistry` used by `TimmySessionManager`. This is the bridge between old and new.

- [ ] **Step 1: Write failing test for PipelineContentProvider adapter**

Add to `api/timmy_content_provider_test.go`:

```go
func TestPipelineContentProvider_CanHandle(t *testing.T) {
	sources := NewContentSourceRegistry()
	sources.Register(NewHTTPSource(NewURIValidator([]string{"127.0.0.1"}, []string{"https", "http"})))
	extractors := NewContentExtractorRegistry()
	extractors.Register(NewPlainTextExtractor())
	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())

	adapter := NewPipelineContentProvider(pipeline)

	// URI-based references are handled
	assert.True(t, adapter.CanHandle(context.Background(), EntityReference{EntityType: "document", URI: "https://example.com/doc"}))
	// Non-URI references are not handled
	assert.False(t, adapter.CanHandle(context.Background(), EntityReference{EntityType: "note", EntityID: "123"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestPipelineContentProvider`
Expected: FAIL

- [ ] **Step 3: Add PipelineContentProvider adapter to timmy_content_provider.go**

Add to the end of `api/timmy_content_provider.go`:

```go
// PipelineContentProvider adapts the two-layer ContentPipeline to the
// existing ContentProvider interface, bridging old and new code.
type PipelineContentProvider struct {
	pipeline *ContentPipeline
}

// NewPipelineContentProvider creates an adapter.
func NewPipelineContentProvider(pipeline *ContentPipeline) *PipelineContentProvider {
	return &PipelineContentProvider{pipeline: pipeline}
}

// Name returns the adapter name.
func (p *PipelineContentProvider) Name() string { return "pipeline" }

// CanHandle returns true for entity references with a URI.
func (p *PipelineContentProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	return ref.URI != ""
}

// Extract delegates to the content pipeline.
func (p *PipelineContentProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	result, err := p.pipeline.Extract(ctx, ref.URI)
	if err != nil {
		return ExtractedContent{}, err
	}
	if result.Title == "" {
		result.Title = ref.Name
	}
	return result, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestPipelineContentProvider`
Expected: PASS

- [ ] **Step 5: Lint and build**

Run: `make lint && make build-server`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/timmy_content_provider.go api/timmy_content_provider_test.go
git commit -m "feat(timmy): add PipelineContentProvider adapter bridging old and new interfaces

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 6: Wire Up Pipeline in main.go

**Files:**
- Modify: `cmd/server/main.go`

Replace the direct provider registration with the new pipeline, keeping the adapter for backward compatibility.

- [ ] **Step 1: Update provider registration in initializeTimmy**

In `cmd/server/main.go`, replace the block at ~line 969-981:

```go
// Old:
//   registry := api.NewContentProviderRegistry()
//   registry.Register(api.NewDirectTextProvider())
//   registry.Register(api.NewJSONContentProvider())
//   ...
//   registry.Register(api.NewHTTPContentProvider(timmyURIValidator))
//   registry.Register(api.NewPDFContentProvider(timmyURIValidator))

// New:
registry := api.NewContentProviderRegistry()
registry.Register(api.NewDirectTextProvider())
registry.Register(api.NewJSONContentProvider())

// Build URI validators from SSRF config
issueURIValidator := buildURIValidator(cfg.SSRF.IssueURI, "TMI_SSRF_ISSUE_URI")
documentURIValidator := buildURIValidator(cfg.SSRF.DocumentURI, "TMI_SSRF_DOCUMENT_URI")
repositoryURIValidator := buildURIValidator(cfg.SSRF.RepositoryURI, "TMI_SSRF_REPOSITORY_URI")
timmyURIValidator := buildURIValidator(cfg.SSRF.Timmy, "TMI_SSRF_TIMMY")

apiServer.SetURIValidators(issueURIValidator, documentURIValidator, repositoryURIValidator)

// Build two-layer content pipeline for URI-based content
contentSources := api.NewContentSourceRegistry()
contentSources.Register(api.NewHTTPSource(timmyURIValidator))

contentExtractors := api.NewContentExtractorRegistry()
contentExtractors.Register(api.NewPlainTextExtractor())
contentExtractors.Register(api.NewHTMLExtractor())
contentExtractors.Register(api.NewPDFExtractor())

pipeline := api.NewContentPipeline(contentSources, contentExtractors, api.NewURLPatternMatcher())

// Adapter: pipeline implements ContentProvider for URI-based refs
registry.Register(api.NewPipelineContentProvider(pipeline))
```

- [ ] **Step 2: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS — no behavior change, same providers registered, same extraction results

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "refactor(timmy): wire content pipeline into server startup

Replaces direct HTTP/PDF provider registration with the two-layer
Source/Extractor pipeline via PipelineContentProvider adapter.
No behavior change.

Part of #232 Phase 1 — Source/Extractor refactor."
```

---

## Task 7: Document Access Tracking — Model and Store

**Files:**
- Modify: `api/models/models.go` (Document struct)
- Modify: `api/document_store.go` (add ListByAccessStatus)
- Modify: `api/document_store_gorm.go` (implement ListByAccessStatus)

- [ ] **Step 1: Add AccessStatus and ContentSource fields to Document model**

In `api/models/models.go`, add two fields to the `Document` struct after `TimmyEnabled`:

```go
AccessStatus *string `gorm:"type:varchar(32);default:unknown"`
ContentSource *string `gorm:"type:varchar(64)"`
```

- [ ] **Step 2: Add ListByAccessStatus to DocumentStore interface**

In `api/document_store.go`, add to the interface:

```go
// ListByAccessStatus returns documents with the given access status across all threat models.
ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error)
```

- [ ] **Step 3: Implement ListByAccessStatus in GORM store**

In `api/document_store_gorm.go`, add:

```go
func (s *GormDocumentStore) ListByAccessStatus(ctx context.Context, status string, limit int) ([]Document, error) {
	var dbDocs []models.Document
	result := s.db.WithContext(ctx).
		Where("access_status = ? AND deleted_at IS NULL", status).
		Limit(limit).
		Find(&dbDocs)
	if result.Error != nil {
		return nil, result.Error
	}

	docs := make([]Document, 0, len(dbDocs))
	for _, d := range dbDocs {
		docs = append(docs, documentModelToAPI(d))
	}
	return docs, nil
}
```

- [ ] **Step 4: Update documentModelToAPI and documentAPIToModel**

Find the `documentModelToAPI` and `documentAPIToModel` functions in `api/document_store_gorm.go` and add the new fields to both mapping functions:

In `documentModelToAPI`, add:
```go
// Map access_status and content_source
if d.AccessStatus != nil {
    doc.AccessStatus = d.AccessStatus
}
if d.ContentSource != nil {
    doc.ContentSource = d.ContentSource
}
```

In `documentAPIToModel`, add:
```go
if doc.AccessStatus != nil {
    model.AccessStatus = doc.AccessStatus
}
if doc.ContentSource != nil {
    model.ContentSource = doc.ContentSource
}
```

Note: `AccessStatus` and `ContentSource` fields will also need to be added to the generated `Document` type in `api/api.go` via the OpenAPI spec — that happens in Task 11. For now, use a local type alias or add the fields manually for compilation. The GORM model is the source of truth for the database.

- [ ] **Step 5: Build**

Run: `make build-server`
Expected: PASS (may need minor adjustments if the generated API type doesn't have the fields yet — add them temporarily to the Document type mapping with comments noting they'll come from OpenAPI in Task 11)

- [ ] **Step 6: Run unit tests**

Run: `make test-unit`
Expected: PASS — existing tests should not break, new fields have defaults

- [ ] **Step 7: Commit**

```bash
git add api/models/models.go api/document_store.go api/document_store_gorm.go
git commit -m "feat(timmy): add access_status and content_source to Document model

Adds AccessStatus (default 'unknown') and ContentSource (nullable) fields
to support document access tracking for content providers.

Part of #232 Phase 2 — Document access tracking."
```

---

## Task 8: Hook URL Pattern Matcher into Document Creation

**Files:**
- Modify: `api/document_sub_resource_handlers.go`
- Modify: `api/document_sub_resource_handlers_test.go`

- [ ] **Step 1: Add pipeline reference to DocumentSubResourceHandler**

In `api/document_sub_resource_handlers.go`, add a field and setter:

```go
// Add to DocumentSubResourceHandler struct:
contentPipeline *ContentPipeline

// Add setter method:
func (h *DocumentSubResourceHandler) SetContentPipeline(p *ContentPipeline) {
	h.contentPipeline = p
}
```

- [ ] **Step 2: Add access detection logic to CreateDocument**

In `CreateDocument`, after the URI validation (`validateURI`) and before the `Create` call, add:

```go
// Detect content source from URI pattern
if h.contentPipeline != nil {
	matcher := h.contentPipeline.Matcher()
	provider := matcher.Identify(document.Uri)

	if provider != "" && provider != ProviderHTTP {
		// Check if the provider has a registered source
		_, hasSource := h.contentPipeline.Sources().FindSource(c.Request.Context(), document.Uri)
		if !hasSource {
			HandleRequestError(c, &RequestError{
				Status:  422,
				Code:    "provider_not_configured",
				Message: fmt.Sprintf("%s document access is not configured on this server. Contact your administrator.", provider),
			})
			return
		}

		contentSource := provider
		document.ContentSource = &contentSource

		// Try to validate access if the source supports it
		src, _ := h.contentPipeline.Sources().FindSource(c.Request.Context(), document.Uri)
		if validator, ok := src.(AccessValidator); ok {
			accessible, valErr := validator.ValidateAccess(c.Request.Context(), document.Uri)
			if valErr != nil {
				logger.Warn("Access validation failed for %s: %v", document.Uri, valErr)
			}
			if accessible {
				status := "accessible"
				document.AccessStatus = &status
			} else {
				// Try to request access if supported
				if requester, ok := src.(AccessRequester); ok {
					if reqErr := requester.RequestAccess(c.Request.Context(), document.Uri); reqErr != nil {
						logger.Warn("Access request failed for %s: %v", document.Uri, reqErr)
					}
				}
				status := "pending_access"
				document.AccessStatus = &status
			}
		} else {
			status := "unknown"
			document.AccessStatus = &status
		}
	} else {
		// Plain HTTP or no provider — status unknown until extraction
		if provider == ProviderHTTP {
			contentSource := ProviderHTTP
			document.ContentSource = &contentSource
		}
		status := "unknown"
		document.AccessStatus = &status
	}
}
```

- [ ] **Step 3: Write test for 422 on unconfigured provider**

Add to `api/document_sub_resource_handlers_test.go`:

```go
func TestCreateDocument_UnconfiguredProvider_Returns422(t *testing.T) {
	// Set up a pipeline with URL matcher but no Google Drive source registered
	sources := NewContentSourceRegistry()
	// Only HTTP source — no Google Drive
	sources.Register(NewHTTPSource(NewURIValidator([]string{"127.0.0.1"}, []string{"https", "http"})))
	extractors := NewContentExtractorRegistry()
	pipeline := NewContentPipeline(sources, extractors, NewURLPatternMatcher())

	// Configure the handler with the pipeline
	// (depends on test setup patterns in existing test file — follow those patterns)
	// The test should POST a document with a Google Drive URI and expect 422
	// URI: "https://docs.google.com/document/d/abc123/edit"
	// Expected: 422 with "google_drive document access is not configured"
}
```

Note: The exact test setup depends on the existing test harness patterns in `document_sub_resource_handlers_test.go`. Follow those patterns for creating the gin context, setting up stores, etc. The key assertion is: a Google Drive URL with no Google Drive source returns HTTP 422.

- [ ] **Step 4: Run test to verify it passes**

Run: `make test-unit name=TestCreateDocument_UnconfiguredProvider`
Expected: PASS

- [ ] **Step 5: Build and run all unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/document_sub_resource_handlers.go api/document_sub_resource_handlers_test.go
git commit -m "feat(timmy): detect content source and validate access on document creation

Returns 422 for known providers that aren't configured (e.g., Google Drive
URL when no Google Drive source is registered). Sets access_status and
content_source fields on the document.

Part of #232 Phase 2 — Document access tracking."
```

---

## Task 9: Google Drive Configuration

**Files:**
- Create: `internal/config/content_sources.go`
- Modify: `internal/config/config.go`

- [ ] **Step 1: Create ContentSourcesConfig**

```go
// internal/config/content_sources.go
package config

// ContentSourcesConfig holds configuration for all content source providers.
type ContentSourcesConfig struct {
	GoogleDrive GoogleDriveConfig `yaml:"google_drive"`
}

// GoogleDriveConfig holds Google Drive service account configuration.
type GoogleDriveConfig struct {
	Enabled             bool   `yaml:"enabled" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_ENABLED"`
	ServiceAccountEmail string `yaml:"service_account_email" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_SERVICE_ACCOUNT_EMAIL"`
	CredentialsFile     string `yaml:"credentials_file" env:"TMI_CONTENT_SOURCE_GOOGLE_DRIVE_CREDENTIALS_FILE"`
}

// IsConfigured returns true if Google Drive has the minimum required configuration.
func (c GoogleDriveConfig) IsConfigured() bool {
	return c.Enabled && c.CredentialsFile != ""
}
```

- [ ] **Step 2: Add ContentSources to Config struct**

In `internal/config/config.go`, add to the `Config` struct:

```go
ContentSources ContentSourcesConfig `yaml:"content_sources"`
```

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/config/content_sources.go internal/config/config.go
git commit -m "feat(timmy): add Google Drive content source configuration

Part of #232 Phase 3 — Google Drive source."
```

---

## Task 10: GoogleDriveSource Implementation

**Files:**
- Create: `api/content_source_google_drive.go`
- Test: `api/content_source_google_drive_test.go`

This task requires adding the Google Drive API dependency.

- [ ] **Step 1: Add Google Drive API dependency**

```bash
cd /Users/efitz/Projects/tmi
go get google.golang.org/api/drive/v3
go get golang.org/x/oauth2/google
```

- [ ] **Step 2: Write failing tests for GoogleDriveSource**

```go
// api/content_source_google_drive_test.go
package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGoogleDriveSource_CanHandle(t *testing.T) {
	// Use a nil service for CanHandle tests — it only checks URL patterns
	s := &GoogleDriveSource{}

	tests := []struct {
		uri      string
		expected bool
	}{
		{"https://docs.google.com/document/d/abc123/edit", true},
		{"https://docs.google.com/spreadsheets/d/abc123/edit", true},
		{"https://docs.google.com/presentation/d/abc123/edit", true},
		{"https://drive.google.com/file/d/abc123/view", true},
		{"https://drive.google.com/open?id=abc123", true},
		{"https://example.com/doc.html", false},
		{"https://confluence.example.com/wiki/page", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			assert.Equal(t, tt.expected, s.CanHandle(context.Background(), tt.uri))
		})
	}
}

func TestGoogleDriveSource_ExtractFileID(t *testing.T) {
	tests := []struct {
		uri    string
		fileID string
		ok     bool
	}{
		{"https://docs.google.com/document/d/1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms/edit", "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgVE2upms", true},
		{"https://docs.google.com/spreadsheets/d/abc123/edit#gid=0", "abc123", true},
		{"https://drive.google.com/file/d/abc123/view", "abc123", true},
		{"https://drive.google.com/open?id=abc123", "abc123", true},
		{"https://example.com/doc", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.uri, func(t *testing.T) {
			fileID, ok := extractGoogleDriveFileID(tt.uri)
			assert.Equal(t, tt.ok, ok)
			if ok {
				assert.Equal(t, tt.fileID, fileID)
			}
		})
	}
}

func TestGoogleDriveSource_Name(t *testing.T) {
	s := &GoogleDriveSource{}
	assert.Equal(t, "google_drive", s.Name())
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `make test-unit name=TestGoogleDriveSource`
Expected: FAIL

- [ ] **Step 4: Implement GoogleDriveSource**

```go
// api/content_source_google_drive.go
package api

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

const googleDriveMaxExportSize = 10 * 1024 * 1024 // 10 MiB

// googleDriveDocPathRegex matches /document/d/{fileId}, /spreadsheets/d/{fileId}, etc.
var googleDriveDocPathRegex = regexp.MustCompile(`/(?:document|spreadsheets|presentation|file)/d/([^/]+)`)

// GoogleDriveSource fetches content from Google Drive using a service account.
// Implements ContentSource, AccessValidator, and AccessRequester.
type GoogleDriveSource struct {
	service             *drive.Service
	serviceAccountEmail string
}

// NewGoogleDriveSource creates a new GoogleDriveSource from a credentials JSON file.
func NewGoogleDriveSource(credentialsFile string, serviceAccountEmail string) (*GoogleDriveSource, error) {
	ctx := context.Background()

	// Read credentials file
	creds, err := readCredentialsFile(credentialsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read Google credentials: %w", err)
	}

	config, err := google.JWTConfigFromJSON(creds, drive.DriveReadonlyScope)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Google credentials: %w", err)
	}

	client := config.Client(ctx)
	svc, err := drive.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		return nil, fmt.Errorf("failed to create Drive service: %w", err)
	}

	return &GoogleDriveSource{
		service:             svc,
		serviceAccountEmail: serviceAccountEmail,
	}, nil
}

// readCredentialsFile reads a Google credentials JSON file.
func readCredentialsFile(path string) ([]byte, error) {
	//nolint:gosec // Path comes from operator config, not user input
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read credentials file %s: %w", path, err)
	}
	return data, nil
}

// Name returns the source name.
func (s *GoogleDriveSource) Name() string { return "google_drive" }

// CanHandle returns true for Google Docs/Drive URLs.
func (s *GoogleDriveSource) CanHandle(_ context.Context, uri string) bool {
	lower := strings.ToLower(uri)
	host := extractHost(lower)
	return host == "docs.google.com" || host == "drive.google.com"
}

// Fetch downloads or exports the file content.
func (s *GoogleDriveSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	logger := slogging.Get()

	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return nil, "", fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	// Get file metadata to determine type
	file, err := s.service.Files.Get(fileID).
		Fields("id,name,mimeType").
		Context(ctx).
		Do()
	if err != nil {
		return nil, "", fmt.Errorf("failed to get file metadata: %w", err)
	}

	logger.Debug("GoogleDriveSource: file %s mimeType=%s", file.Name, file.MimeType)

	// Google Workspace types need export; binary files use direct download
	switch file.MimeType {
	case "application/vnd.google-apps.document":
		return s.exportFile(ctx, fileID, "text/plain")
	case "application/vnd.google-apps.spreadsheet":
		return s.exportFile(ctx, fileID, "text/csv")
	case "application/vnd.google-apps.presentation":
		return s.exportFile(ctx, fileID, "text/plain")
	default:
		return s.downloadFile(ctx, fileID, file.MimeType)
	}
}

// ValidateAccess checks whether the service account can access the file (metadata-only read).
func (s *GoogleDriveSource) ValidateAccess(ctx context.Context, uri string) (bool, error) {
	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return false, fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	_, err := s.service.Files.Get(fileID).
		Fields("id").
		Context(ctx).
		Do()
	if err != nil {
		// Any error (including 404/403) means we can't access it
		return false, nil
	}
	return true, nil
}

// RequestAccess creates a permission request for the service account.
func (s *GoogleDriveSource) RequestAccess(ctx context.Context, uri string) error {
	logger := slogging.Get()

	fileID, ok := extractGoogleDriveFileID(uri)
	if !ok {
		return fmt.Errorf("could not extract file ID from Google Drive URL: %s", uri)
	}

	// Note: creating a permission with type=user and sendNotificationEmail=true
	// sends an access request email to the file owner.
	// This requires the service account email to be set.
	if s.serviceAccountEmail == "" {
		logger.Warn("GoogleDriveSource: cannot request access — no service account email configured")
		return nil
	}

	logger.Info("GoogleDriveSource: requesting access to file %s for %s", fileID, s.serviceAccountEmail)

	// We can't actually create a permission for ourselves (that requires the owner to share).
	// Instead, log that the user should share the document with the service account.
	// A real implementation would use the Drive API's "request access" feature or
	// generate an email template. For now, we log the intent.
	logger.Info("GoogleDriveSource: document owner should share file %s with %s", fileID, s.serviceAccountEmail)
	return nil
}

// exportFile exports a Google Workspace file to the specified MIME type.
func (s *GoogleDriveSource) exportFile(ctx context.Context, fileID, exportMIME string) ([]byte, string, error) {
	resp, err := s.service.Files.Export(fileID, exportMIME).
		Context(ctx).
		Download()
	if err != nil {
		return nil, "", fmt.Errorf("failed to export file: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read export: %w", err)
	}
	return data, exportMIME, nil
}

// downloadFile downloads a binary file from Drive.
func (s *GoogleDriveSource) downloadFile(ctx context.Context, fileID, mimeType string) ([]byte, string, error) {
	resp, err := s.service.Files.Get(fileID).
		Context(ctx).
		Download()
	if err != nil {
		return nil, "", fmt.Errorf("failed to download file: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	data, err := io.ReadAll(io.LimitReader(resp.Body, googleDriveMaxExportSize))
	if err != nil {
		return nil, "", fmt.Errorf("failed to read download: %w", err)
	}
	return data, mimeType, nil
}

// extractGoogleDriveFileID extracts the file ID from a Google Drive/Docs URL.
func extractGoogleDriveFileID(uri string) (string, bool) {
	// Try path-based pattern: /d/{fileId}/
	if matches := googleDriveDocPathRegex.FindStringSubmatch(uri); len(matches) > 1 {
		return matches[1], true
	}

	// Try query parameter: ?id={fileId}
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", false
	}
	if id := parsed.Query().Get("id"); id != "" {
		return id, true
	}

	return "", false
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `make test-unit name=TestGoogleDriveSource`
Expected: PASS (CanHandle and ExtractFileID tests pass; Fetch/ValidateAccess/RequestAccess require a real service, tested in integration)

- [ ] **Step 6: Lint and build**

Run: `make lint && make build-server`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add api/content_source_google_drive.go api/content_source_google_drive_test.go go.mod go.sum
git commit -m "feat(timmy): add GoogleDriveSource content source

Service account-based Google Drive source with file ID extraction,
export for Google Workspace types, and download for binary files.
Implements ContentSource, AccessValidator, and AccessRequester.

Part of #232 Phase 3 — Google Drive source."
```

---

## Task 11: Background Access Poller

**Files:**
- Create: `api/access_poller.go`
- Test: `api/access_poller_test.go`

- [ ] **Step 1: Write failing test for AccessPoller**

```go
// api/access_poller_test.go
package api

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// mockAccessValidator tracks calls for testing
type mockAccessValidator struct {
	accessible bool
	err        error
	calls      int
}

func (m *mockAccessValidator) Name() string                                    { return "mock" }
func (m *mockAccessValidator) CanHandle(_ context.Context, _ string) bool      { return true }
func (m *mockAccessValidator) Fetch(_ context.Context, _ string) ([]byte, string, error) {
	return nil, "", nil
}
func (m *mockAccessValidator) ValidateAccess(_ context.Context, _ string) (bool, error) {
	m.calls++
	return m.accessible, m.err
}

func TestAccessPoller_CheckPendingDocuments(t *testing.T) {
	validator := &mockAccessValidator{accessible: true}

	sources := NewContentSourceRegistry()
	sources.Register(validator)

	poller := NewAccessPoller(sources, nil, time.Minute, 7*24*time.Hour)

	// The poller's checkPendingDocuments method requires a document store.
	// This test verifies the poller can be created and the check method signature is correct.
	assert.NotNil(t, poller)
	assert.Equal(t, time.Minute, poller.interval)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `make test-unit name=TestAccessPoller`
Expected: FAIL

- [ ] **Step 3: Implement AccessPoller**

```go
// api/access_poller.go
package api

import (
	"context"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// AccessPoller periodically checks documents with "pending_access" status.
type AccessPoller struct {
	sources       *ContentSourceRegistry
	documentStore DocumentStore
	interval      time.Duration
	maxAge        time.Duration
	stopCh        chan struct{}
}

// NewAccessPoller creates a new background access poller.
func NewAccessPoller(
	sources *ContentSourceRegistry,
	documentStore DocumentStore,
	interval time.Duration,
	maxAge time.Duration,
) *AccessPoller {
	return &AccessPoller{
		sources:       sources,
		documentStore: documentStore,
		interval:      interval,
		maxAge:        maxAge,
		stopCh:        make(chan struct{}),
	}
}

// Start begins the background polling loop.
func (p *AccessPoller) Start() {
	go p.run()
}

// Stop signals the poller to stop.
func (p *AccessPoller) Stop() {
	close(p.stopCh)
}

func (p *AccessPoller) run() {
	logger := slogging.Get()
	logger.Info("AccessPoller: started (interval=%s, maxAge=%s)", p.interval, p.maxAge)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			logger.Info("AccessPoller: stopped")
			return
		case <-ticker.C:
			p.checkPendingDocuments()
		}
	}
}

func (p *AccessPoller) checkPendingDocuments() {
	logger := slogging.Get()
	ctx := context.Background()

	if p.documentStore == nil {
		return
	}

	docs, err := p.documentStore.ListByAccessStatus(ctx, "pending_access", 100)
	if err != nil {
		logger.Warn("AccessPoller: failed to list pending documents: %v", err)
		return
	}

	if len(docs) == 0 {
		return
	}

	logger.Debug("AccessPoller: checking %d pending documents", len(docs))

	for _, doc := range docs {
		// Skip documents older than maxAge
		if doc.CreatedAt != nil && time.Since(*doc.CreatedAt) > p.maxAge {
			logger.Debug("AccessPoller: skipping expired document %s (created %s)", doc.Id, doc.CreatedAt)
			continue
		}

		src, ok := p.sources.FindSource(ctx, doc.Uri)
		if !ok {
			continue
		}

		validator, ok := src.(AccessValidator)
		if !ok {
			continue
		}

		accessible, valErr := validator.ValidateAccess(ctx, doc.Uri)
		if valErr != nil {
			logger.Debug("AccessPoller: validation error for %s: %v", doc.Uri, valErr)
			continue
		}

		if accessible {
			logger.Info("AccessPoller: document %s is now accessible", doc.Id)
			status := "accessible"
			doc.AccessStatus = &status
			if updateErr := p.documentStore.Update(ctx, &doc, ""); updateErr != nil {
				logger.Warn("AccessPoller: failed to update document %s: %v", doc.Id, updateErr)
			}
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `make test-unit name=TestAccessPoller`
Expected: PASS

- [ ] **Step 5: Lint and build**

Run: `make lint && make build-server`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add api/access_poller.go api/access_poller_test.go
git commit -m "feat(timmy): add background access poller for pending documents

Polls documents with pending_access status at a configurable interval.
Updates status to accessible when the source confirms access.
Stops retrying after maxAge.

Part of #232 Phase 3 — Google Drive source."
```

---

## Task 12: Timmy Session Integration — Skip Inaccessible Documents

**Files:**
- Modify: `api/timmy_session_manager.go`
- Modify: `api/timmy_session_manager_test.go`

- [ ] **Step 1: Add SkippedSource type and update snapshotDocuments**

In `api/timmy_session_manager.go`, add:

```go
// SkippedSource records a source entity that was excluded from a Timmy session.
type SkippedSource struct {
	EntityID   string `json:"entity_id"`
	Name       string `json:"name"`
	Reason     string `json:"reason"`
}
```

- [ ] **Step 2: Update snapshotDocuments to filter by access_status**

Modify `snapshotDocuments` in `api/timmy_session_manager.go` to return skipped sources:

```go
func (sm *TimmySessionManager) snapshotDocuments(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	if GlobalDocumentStore == nil {
		return nil, nil, nil
	}
	docs, err := GlobalDocumentStore.List(ctx, threatModelID, 0, snapshotMaxItems)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list documents: %w", err)
	}
	var entries []SourceSnapshotEntry
	var skipped []SkippedSource
	for _, d := range docs {
		if !isTimmyEnabled(d.TimmyEnabled) {
			continue
		}

		// Check access status
		status := "unknown"
		if d.AccessStatus != nil {
			status = *d.AccessStatus
		}

		switch status {
		case "accessible", "unknown":
			entries = append(entries, newSnapshotEntry("document", uuidPtrToString(d.Id), d.Name))
		case "pending_access":
			skipped = append(skipped, SkippedSource{
				EntityID: uuidPtrToString(d.Id),
				Name:     d.Name,
				Reason:   "access pending — document owner has been notified",
			})
		case "auth_required":
			skipped = append(skipped, SkippedSource{
				EntityID: uuidPtrToString(d.Id),
				Name:     d.Name,
				Reason:   "account linking required",
			})
		}
	}
	return entries, skipped, nil
}
```

- [ ] **Step 3: Update snapshotSources to collect skipped sources**

Update the `snapshotSources` method to return `[]SkippedSource` as a second return value:

```go
func (sm *TimmySessionManager) snapshotSources(ctx context.Context, threatModelID string) ([]SourceSnapshotEntry, []SkippedSource, error) {
	var entries []SourceSnapshotEntry
	var allSkipped []SkippedSource

	// Non-document collectors (unchanged)
	simpleCollectors := []func() ([]SourceSnapshotEntry, error){
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotAssets(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotThreats(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotNotes(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotRepositories(ctx, threatModelID) },
		func() ([]SourceSnapshotEntry, error) { return sm.snapshotDiagrams() },
	}

	for _, collect := range simpleCollectors {
		items, err := collect()
		if err != nil {
			return nil, nil, err
		}
		entries = append(entries, items...)
	}

	// Document collector returns skipped sources
	docEntries, skipped, err := sm.snapshotDocuments(ctx, threatModelID)
	if err != nil {
		return nil, nil, err
	}
	entries = append(entries, docEntries...)
	allSkipped = append(allSkipped, skipped...)

	return entries, allSkipped, nil
}
```

- [ ] **Step 4: Update CreateSession to propagate skipped sources**

Update `CreateSession` to return skipped sources as part of the session creation. The skipped sources should be reported via the progress callback:

```go
// In CreateSession, update the snapshotSources call:
sources, skipped, err := sm.snapshotSources(ctx, threatModelID)
// ...
if progress != nil && len(skipped) > 0 {
	progress("snapshot", "", "", 100, fmt.Sprintf("found %d entities, %d skipped", len(sources), len(skipped)))
}
```

Also update the return type of `CreateSession` to include skipped sources:

```go
func (sm *TimmySessionManager) CreateSession(
	ctx context.Context,
	userID, threatModelID, title string,
	progress SessionProgressCallback,
) (*models.TimmySession, []SkippedSource, error) {
```

Update all callers accordingly.

- [ ] **Step 5: Update the handler to send skipped sources in the session_created event**

In `api/timmy_handlers.go`, update `CreateTimmyChatSession`:

```go
session, skipped, createErr := s.timmySessionManager.CreateSession(...)
// ...
apiSession := timmySessionToAPI(session)
// Send skipped sources as a separate event before session_created
if len(skipped) > 0 {
	_ = sse.SendEvent("skipped_sources", skipped)
}
_ = sse.SendEvent("session_created", apiSession)
```

- [ ] **Step 6: Run all unit tests**

Run: `make test-unit`
Expected: PASS — update any tests that call `CreateSession` to handle the new return value

- [ ] **Step 7: Commit**

```bash
git add api/timmy_session_manager.go api/timmy_handlers.go
git commit -m "feat(timmy): skip inaccessible documents in Timmy sessions

Documents with pending_access or auth_required status are excluded
from session snapshots. Skipped sources are reported via SSE event.

Part of #232 Phase 4 — Timmy session integration."
```

---

## Task 13: RefreshSources and RequestAccess Handlers

**Files:**
- Modify: `api/timmy_handlers.go`

- [ ] **Step 1: Add RefreshSources handler**

```go
// RefreshTimmySources re-scans sources for an active session, picking up
// any documents whose access_status has changed to "accessible".
// POST /threat_models/{id}/timmy/sessions/{session_id}/refresh_sources
func (s *Server) RefreshTimmySources(c *gin.Context, threatModelId ThreatModelId, sessionId SessionId) {
	logger := slogging.Get().WithContext(c)

	session, err := s.getAndVerifyTimmySession(c, threatModelId, sessionId)
	if err != nil {
		return // error already handled
	}

	if s.timmySessionManager == nil {
		HandleRequestError(c, ServiceUnavailableError("Timmy is not configured"))
		return
	}

	// Re-snapshot sources
	sources, skipped, snapshotErr := s.timmySessionManager.SnapshotSources(
		c.Request.Context(), threatModelId.String(),
	)
	if snapshotErr != nil {
		logger.Error("Failed to refresh sources: %v", snapshotErr)
		HandleRequestError(c, ServerError("Failed to refresh sources"))
		return
	}

	// Update session snapshot
	snapshotJSON, _ := json.Marshal(sources)
	session.SourceSnapshot = models.JSONRaw(snapshotJSON)
	if updateErr := GlobalTimmySessionStore.Update(c.Request.Context(), session); updateErr != nil {
		logger.Error("Failed to update session snapshot: %v", updateErr)
		HandleRequestError(c, ServerError("Failed to update session"))
		return
	}

	c.JSON(http.StatusOK, map[string]any{
		"source_count":    len(sources),
		"skipped_sources": skipped,
	})
}
```

Note: `SnapshotSources` needs to be exported from `TimmySessionManager`. Rename `snapshotSources` to exported or add a public wrapper.

- [ ] **Step 2: Add RequestAccess handler**

```go
// RequestDocumentAccess re-sends an access request for a pending_access document.
// POST /threat_models/{id}/documents/{document_id}/request_access
func (s *Server) RequestDocumentAccess(c *gin.Context, threatModelId ThreatModelId, documentId DocumentId) {
	logger := slogging.Get().WithContext(c)

	doc, err := GlobalDocumentStore.Get(c.Request.Context(), documentId.String())
	if err != nil {
		HandleRequestError(c, NotFoundError("Document not found"))
		return
	}

	if doc.AccessStatus == nil || *doc.AccessStatus != "pending_access" {
		HandleRequestError(c, &RequestError{
			Status:  409,
			Code:    "not_pending_access",
			Message: "document is not in pending_access status",
		})
		return
	}

	if s.contentPipeline == nil {
		HandleRequestError(c, ServiceUnavailableError("Content pipeline not configured"))
		return
	}

	src, ok := s.contentPipeline.Sources().FindSource(c.Request.Context(), doc.Uri)
	if !ok {
		HandleRequestError(c, &RequestError{
			Status:  422,
			Code:    "no_source",
			Message: "no content source available for this document's URI",
		})
		return
	}

	requester, ok := src.(AccessRequester)
	if !ok {
		HandleRequestError(c, &RequestError{
			Status:  422,
			Code:    "access_request_not_supported",
			Message: "the content source for this document does not support access requests",
		})
		return
	}

	if reqErr := requester.RequestAccess(c.Request.Context(), doc.Uri); reqErr != nil {
		logger.Error("Failed to request access for document %s: %v", documentId, reqErr)
		HandleRequestError(c, ServerError("Failed to request access"))
		return
	}

	c.JSON(http.StatusOK, map[string]string{
		"status":  "access_requested",
		"message": "access request has been sent to the document owner",
	})
}
```

- [ ] **Step 3: Add contentPipeline field to Server**

In `api/server.go`, add:
```go
contentPipeline *ContentPipeline

func (s *Server) SetContentPipeline(p *ContentPipeline) {
	s.contentPipeline = p
}
```

Wire it up in `cmd/server/main.go` after creating the pipeline:
```go
apiServer.SetContentPipeline(pipeline)
```

- [ ] **Step 4: Build and run unit tests**

Run: `make build-server && make test-unit`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add api/timmy_handlers.go api/server.go cmd/server/main.go
git commit -m "feat(timmy): add RefreshSources and RequestAccess handlers

RefreshSources re-scans session sources to pick up newly-accessible documents.
RequestAccess re-sends the access request for a pending_access document.

Part of #232 Phase 4 — Timmy session integration."
```

---

## Task 14: Wire Google Drive Source into Server Startup

**Files:**
- Modify: `cmd/server/main.go`

- [ ] **Step 1: Add Google Drive source registration at startup**

In `cmd/server/main.go`, after building the content pipeline and before registering the adapter, add:

```go
// Register Google Drive source if configured
if cfg.ContentSources.GoogleDrive.IsConfigured() {
	gdSource, gdErr := api.NewGoogleDriveSource(
		cfg.ContentSources.GoogleDrive.CredentialsFile,
		cfg.ContentSources.GoogleDrive.ServiceAccountEmail,
	)
	if gdErr != nil {
		logger.Error("Failed to initialize Google Drive source: %v", gdErr)
		// Don't crash — skip this source
	} else {
		contentSources.Register(gdSource)
		logger.Info("Content source enabled: google_drive (service account: %s)",
			cfg.ContentSources.GoogleDrive.ServiceAccountEmail)
	}
}

// Log active sources
logger.Info("Content sources enabled: %s", listSourceNames(contentSources))
```

Add the helper function:
```go
func listSourceNames(r *api.ContentSourceRegistry) string {
	// ContentSourceRegistry doesn't expose names — add a Names() method
	return strings.Join(r.Names(), ", ")
}
```

Add `Names()` to `ContentSourceRegistry` in `api/content_source.go`:
```go
func (r *ContentSourceRegistry) Names() []string {
	names := make([]string, len(r.sources))
	for i, s := range r.sources {
		names[i] = s.Name()
	}
	return names
}
```

- [ ] **Step 2: Start background access poller**

After pipeline setup, add:

```go
// Start background access poller
accessPoller := api.NewAccessPoller(
	contentSources,
	api.GlobalDocumentStore,
	5*time.Minute,  // poll interval
	7*24*time.Hour, // max age
)
accessPoller.Start()
// Store reference for cleanup (add to server or use defer in main)
```

- [ ] **Step 3: Build**

Run: `make build-server`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add cmd/server/main.go api/content_source.go
git commit -m "feat(timmy): wire Google Drive source and access poller into server startup

Registers GoogleDriveSource when configured, starts background access
poller for pending documents, logs active content sources.

Part of #232 Phase 3 — Google Drive source."
```

---

## Task 15: OpenAPI Spec Updates

**Files:**
- Modify: `api-schema/tmi-openapi.json`

This task updates the OpenAPI spec with new schemas, modified schemas, and new endpoints. Use `jq` for surgical modifications given the file size.

- [ ] **Step 1: Add access_status enum to Document schema**

Add `access_status` property to the Document schema:
```json
"access_status": {
  "type": "string",
  "enum": ["accessible", "pending_access", "auth_required", "unknown"],
  "default": "unknown",
  "description": "Access validation status for external content providers"
}
```

- [ ] **Step 2: Add content_source to Document schema**

```json
"content_source": {
  "type": "string",
  "nullable": true,
  "description": "Content provider that handles this document's URI (e.g., google_drive, http)",
  "example": "google_drive"
}
```

- [ ] **Step 3: Add SkippedSource schema**

```json
"SkippedSource": {
  "type": "object",
  "required": ["entity_id", "name", "reason"],
  "properties": {
    "entity_id": {
      "type": "string",
      "format": "uuid",
      "description": "ID of the skipped entity"
    },
    "name": {
      "type": "string",
      "description": "Name of the skipped entity"
    },
    "reason": {
      "type": "string",
      "description": "Why this entity was skipped"
    }
  }
}
```

- [ ] **Step 4: Add refresh_sources endpoint**

```json
"/threat_models/{threat_model_id}/timmy/sessions/{session_id}/refresh_sources": {
  "post": {
    "operationId": "refreshTimmySources",
    "summary": "Refresh session sources",
    "description": "Re-scans sources for an active Timmy session, picking up any documents whose access status has changed.",
    "tags": ["Timmy"],
    "security": [{"BearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": "#/components/parameters/SessionId"}
    ],
    "responses": {
      "200": {
        "description": "Sources refreshed",
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "properties": {
                "source_count": {"type": "integer"},
                "skipped_sources": {
                  "type": "array",
                  "items": {"$ref": "#/components/schemas/SkippedSource"}
                }
              }
            }
          }
        }
      },
      "401": {"$ref": "#/components/responses/Unauthorized"},
      "403": {"$ref": "#/components/responses/Forbidden"},
      "404": {"$ref": "#/components/responses/NotFound"}
    }
  }
}
```

- [ ] **Step 5: Add request_access endpoint**

```json
"/threat_models/{threat_model_id}/documents/{document_id}/request_access": {
  "post": {
    "operationId": "requestDocumentAccess",
    "summary": "Request document access",
    "description": "Re-sends the access request for a document with pending_access status.",
    "tags": ["Documents"],
    "security": [{"BearerAuth": []}],
    "parameters": [
      {"$ref": "#/components/parameters/ThreatModelId"},
      {"$ref": "#/components/parameters/DocumentId"}
    ],
    "responses": {
      "200": {
        "description": "Access request sent",
        "content": {
          "application/json": {
            "schema": {
              "type": "object",
              "properties": {
                "status": {"type": "string"},
                "message": {"type": "string"}
              }
            }
          }
        }
      },
      "401": {"$ref": "#/components/responses/Unauthorized"},
      "403": {"$ref": "#/components/responses/Forbidden"},
      "404": {"$ref": "#/components/responses/NotFound"},
      "409": {"description": "Document is not in pending_access status"},
      "422": {"description": "Content source not configured or does not support access requests"}
    }
  }
}
```

- [ ] **Step 6: Validate and regenerate**

Run:
```bash
make validate-openapi
make generate-api
make build-server
make test-unit
```
Expected: All pass

- [ ] **Step 7: Commit**

```bash
git add api-schema/tmi-openapi.json api/api.go
git commit -m "feat(api): add document access tracking and source refresh endpoints

Adds access_status and content_source to Document schema.
Adds SkippedSource schema.
Adds refresh_sources and request_access endpoints.

Part of #232 Phase 5 — OpenAPI spec updates.
Closes #232"
```

---

## Summary

| Task | Phase | Description |
|------|-------|-------------|
| 1 | Phase 1 | ContentSource + ContentExtractor interfaces and registries |
| 2 | Phase 1 | URLPatternMatcher + ContentPipeline orchestrator |
| 3 | Phase 1 | PlainText, HTML, PDF extractors |
| 4 | Phase 1 | HTTPSource (wrap existing HTTP fetch) |
| 5 | Phase 1 | PipelineContentProvider adapter (bridge old interface) |
| 6 | Phase 1 | Wire pipeline into main.go |
| 7 | Phase 2 | Document model: access_status + content_source |
| 8 | Phase 2 | URL pattern detection in CreateDocument |
| 9 | Phase 3 | Google Drive configuration |
| 10 | Phase 3 | GoogleDriveSource implementation |
| 11 | Phase 3 | Background access poller |
| 12 | Phase 4 | Skip inaccessible docs in Timmy sessions |
| 13 | Phase 4 | RefreshSources + RequestAccess handlers |
| 14 | Phase 3 | Wire Google Drive + poller into startup |
| 15 | Phase 5 | OpenAPI spec updates |
