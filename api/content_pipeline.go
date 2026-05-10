package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Provider name constants
const (
	ProviderConfluence      = "confluence"
	ProviderGoogleDrive     = "google_drive"
	ProviderGoogleWorkspace = "google_workspace"
	ProviderHTTP            = "http"
	ProviderMicrosoft       = "microsoft"
	// ProviderOneDrive is the legacy name; retained as an alias until all wiring switches to ProviderMicrosoft.
	ProviderOneDrive = ProviderMicrosoft
)

// Google host constants shared by URL matching and CanHandle implementations.
const (
	googleHostDocs  = "docs.google.com"
	googleHostDrive = "drive.google.com"
)

// Microsoft host constants shared by URL matching, CanHandle, and picker
// validation. Hosts cover both audiences:
//   - *.sharepoint.com (suffix match)        — Entra-managed work/school
//   - microsoftHostOneDriveLive (exact)      — consumer OneDrive root
//   - .microsoftHostOneDriveLive (suffix)    — consumer OneDrive subdomains
//   - microsoftHostOneDriveShort (exact)     — consumer short link
const (
	microsoftHostSharePointSuffix = ".sharepoint.com"
	microsoftHostOneDriveLive     = "onedrive.live.com"
	microsoftHostOneDriveShort    = "1drv.ms"
)

// Document access status constants
const (
	AccessStatusUnknown          = "unknown"
	AccessStatusAccessible       = "accessible"
	AccessStatusPendingAccess    = "pending_access"
	AccessStatusExtractionFailed = "extraction_failed"
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
			ProviderMicrosoft:   true,
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

	host := extractHost(lower)

	switch {
	case host == googleHostDocs || host == googleHostDrive:
		return ProviderGoogleDrive
	case strings.HasSuffix(host, ".atlassian.net") && strings.Contains(lower, "/wiki/"):
		return ProviderConfluence
	// Microsoft is multi-audience (#286 work/school + #297 consumer) under a
	// single delegated provider. The hosts below cover both audiences:
	//   - *.sharepoint.com           — Entra-managed (OneDrive-for-Business + SharePoint)
	//   - onedrive.live.com (or *.)  — consumer OneDrive
	//   - 1drv.ms                    — consumer OneDrive short link
	case strings.HasSuffix(host, microsoftHostSharePointSuffix),
		host == microsoftHostOneDriveLive,
		strings.HasSuffix(host, "."+microsoftHostOneDriveLive),
		host == microsoftHostOneDriveShort:
		return ProviderMicrosoft
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
	idx := strings.Index(lower, "://")
	if idx < 0 {
		return ""
	}
	rest := lower[idx+3:]
	if i := strings.IndexAny(rest, ":/"); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

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

// ContentPipeline orchestrates Source -> Extractor for URI-based content.
type ContentPipeline struct {
	sources    *ContentSourceRegistry
	extractors *ContentExtractorRegistry
	matcher    *URLPatternMatcher
	limiter    *ConcurrencyLimiter
	limits     PipelineLimits
	dumper     *extractedTextNoteDumper // optional; nil disables the dev-mode hook
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

// NewContentPipelineWithLimiter wires a per-user concurrency limiter and a
// pipeline-level wall-clock budget into the existing pipeline. The legacy
// NewContentPipeline constructor remains for callers that don't need either.
func NewContentPipelineWithLimiter(
	sources *ContentSourceRegistry,
	extractors *ContentExtractorRegistry,
	matcher *URLPatternMatcher,
	limiter *ConcurrencyLimiter,
	limits PipelineLimits,
) *ContentPipeline {
	p := NewContentPipeline(sources, extractors, matcher)
	p.limiter = limiter
	p.limits = limits
	return p
}

// SetExtractedTextNoteDumper enables the dev/test-only hook that persists each
// successful extraction's markdown as a Note on the parent threat model. The
// caller is responsible for verifying that the build mode permits the hook;
// passing a non-nil dumper in production builds is a programming error and
// should be prevented at config-validation time. Pass nil to disable.
func (p *ContentPipeline) SetExtractedTextNoteDumper(d *extractedTextNoteDumper) {
	p.dumper = d
}

// Extract fetches bytes from the appropriate source and extracts text.
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
		return ExtractedContent{
			Text:        string(data),
			ContentType: contentType,
		}, nil
	}

	logger.Debug("ContentPipeline: extracting %s via extractor %s", contentType, ext.Name())

	if be, isBounded := ext.(BoundedExtractor); isBounded && be.Bounded() && p.limits.WallClockBudget > 0 {
		// Prefer the context-aware path when the extractor implements it:
		// the deadline-bearing context is wired into the OOXML archive's
		// boundedReader so wall-clock cancellation aborts in-flight reads.
		if ce, isCtxAware := ext.(ContextAwareExtractor); isCtxAware {
			return extractWithDeadline(ctx, p.limits.WallClockBudget, func(dctx context.Context) (ExtractedContent, error) {
				return ce.ExtractCtx(dctx, data, contentType)
			})
		}
		// Legacy path — extractor isn't ctx-aware. The deadline still fires
		// at the goroutine boundary, but in-flight I/O continues until it
		// finishes naturally; the pipeline returns DeadlineExceeded promptly
		// while the goroutine drains in the background.
		return extractWithDeadline(ctx, p.limits.WallClockBudget, func(_ context.Context) (ExtractedContent, error) {
			return ext.Extract(data, contentType)
		})
	}
	return ext.Extract(data, contentType)
}

// ExtractionClassification describes how a typed extractor error maps to
// access_status + access_reason_code, plus an optional human-readable
// Detail used to enrich the persisted diagnostic. ReasonDetail is set
// only for limit-errors that carry a Detail string (e.g. "slide #42",
// "sheet 'Sales'", "word/document.xml"); other classifications leave it
// empty.
type ExtractionClassification struct {
	Status       string
	ReasonCode   string
	ReasonDetail string
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
			return ExtractionClassification{
				Status:       AccessStatusExtractionFailed,
				ReasonCode:   code,
				ReasonDetail: le.Detail,
			}
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

// ExtractForDocument is a document-aware variant of Extract. It runs the
// usual fetch + extract pipeline, and on success — if a dev/test-only
// dumper is configured — also persists the extracted markdown as a Note on
// the document's parent threat model. Note creation failures are logged but
// do not affect the returned ExtractedContent or error: the dump hook is
// strictly an inspection aid and must not change the production behavior of
// the pipeline.
func (p *ContentPipeline) ExtractForDocument(ctx context.Context, doc Document) (ExtractedContent, error) {
	out, err := p.Extract(ctx, doc.Uri)
	if err != nil {
		return out, err
	}
	if p.dumper != nil && doc.Id != nil {
		p.dumper.dump(ctx, doc, out)
	}
	return out, nil
}

// extractedTextNoteDumper persists the markdown produced by a successful
// extraction as a Note on the parent threat model. Strictly a dev/test
// inspection aid — see TimmyConfig.DumpExtractedTextToNote.
type extractedTextNoteDumper struct {
	notes     NoteRepository
	documents DocumentRepository
}

// NewExtractedTextNoteDumper builds a dumper. notes/documents must be non-nil.
func NewExtractedTextNoteDumper(notes NoteRepository, documents DocumentRepository) *extractedTextNoteDumper {
	return &extractedTextNoteDumper{notes: notes, documents: documents}
}

func (d *extractedTextNoteDumper) dump(ctx context.Context, doc Document, out ExtractedContent) {
	if d == nil || d.notes == nil || d.documents == nil {
		return
	}
	logger := slogging.Get()

	if doc.Id == nil {
		return
	}
	tmID, err := d.documents.GetThreatModelID(ctx, doc.Id.String())
	if err != nil {
		logger.Warn("dump-extracted-text: GetThreatModelID failed for doc %s: %v", doc.Id, err)
		return
	}
	if tmID == "" {
		// Document has no parent threat model — defensive skip.
		return
	}

	note := &Note{
		Name:    fmt.Sprintf("[extracted] %s @ %s", doc.Name, time.Now().UTC().Format(time.RFC3339)),
		Content: out.Text,
	}
	if err := d.notes.Create(ctx, note, tmID); err != nil {
		logger.Warn("dump-extracted-text: failed to create Note for doc %s (tm=%s): %v", doc.Id, tmID, err)
		return
	}
	logger.Debug("dump-extracted-text: wrote Note %v for doc %s (tm=%s, %d bytes)", note.Id, doc.Id, tmID, len(out.Text))
}

// Matcher returns the pipeline's URL pattern matcher.
func (p *ContentPipeline) Matcher() *URLPatternMatcher {
	return p.matcher
}

// Sources returns the pipeline's source registry.
func (p *ContentPipeline) Sources() *ContentSourceRegistry {
	return p.sources
}
