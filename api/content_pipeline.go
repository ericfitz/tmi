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
	// onedrive.live.com (consumer Microsoft accounts) is intentionally NOT
	// matched here. Tracked in #297; for now consumer URLs fall through to
	// ProviderHTTP rather than being misidentified as Entra-managed Microsoft.
	case strings.HasSuffix(host, ".sharepoint.com"):
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

	if be, ok := ext.(BoundedExtractor); ok && be.Bounded() && p.limits.WallClockBudget > 0 {
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

// Matcher returns the pipeline's URL pattern matcher.
func (p *ContentPipeline) Matcher() *URLPatternMatcher {
	return p.matcher
}

// Sources returns the pipeline's source registry.
func (p *ContentPipeline) Sources() *ContentSourceRegistry {
	return p.sources
}
