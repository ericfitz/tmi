package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
	"github.com/ericfitz/tmi/pkg/extract"
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
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: maps URL patterns to content provider identifiers (pure)
type URLPatternMatcher struct {
	knownProviders map[string]bool
}

// NewURLPatternMatcher creates a matcher with all known provider patterns.
// SEM@8b53645247cdf13cbfc2a73ad553fde880a9c3bd: build a URLPatternMatcher with all supported content providers registered (pure)
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
// SEM@a2db6d159e7859f682bdd332f9a3bfb0b222b7af: map a URL to its canonical content provider name (pure)
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
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: validate that a provider name is registered in the matcher (pure)
func (m *URLPatternMatcher) IsKnownProvider(name string) bool {
	return m.knownProviders[name]
}

// extractHost extracts the hostname from a lowercased URL string.
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: parse the hostname from a lowercase URL string, stripping port and path (pure)
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
// SEM@d3cdf19de7c031cfdc661e3c7549c0c38a5ed523: configures wall-clock budget for content extraction (pure)
type PipelineLimits struct {
	WallClockBudget time.Duration
}

// DefaultPipelineLimits returns the design-spec default budget; used by tests.
// SEM@d3cdf19de7c031cfdc661e3c7549c0c38a5ed523: build PipelineLimits with a 30-second wall-clock budget (pure)
func DefaultPipelineLimits() PipelineLimits {
	return PipelineLimits{WallClockBudget: 30 * time.Second}
}

// ContentPipeline orchestrates Source -> Extractor for URI-based content.
// SEM@117032a3c5523a04e970f76a285e342169d5150c: orchestrates fetching and text extraction from external content sources
type ContentPipeline struct {
	sources    *ContentSourceRegistry
	extractors *ContentExtractorRegistry
	matcher    *URLPatternMatcher
	limiter    *ConcurrencyLimiter
	limits     PipelineLimits
	dumper     *extractedTextNoteDumper // optional; nil disables the dev-mode hook
}

// NewContentPipeline creates a new pipeline.
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: build a ContentPipeline without concurrency limiting (pure)
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
// SEM@9d853db42745838f38cf03567f0d9e14a212c576: build a ContentPipeline with concurrency limiting and wall-clock budget (pure)
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
// SEM@117032a3c5523a04e970f76a285e342169d5150c: register a dev-mode hook to persist extracted text as notes (mutates shared state)
func (p *ContentPipeline) SetExtractedTextNoteDumper(d *extractedTextNoteDumper) {
	p.dumper = d
}

// RebuildPipelineWithSources creates a new ContentPipeline that reuses all
// settings from base (extractor registry, URL pattern matcher, concurrency
// limiter, pipeline limits, and the extracted-text dumper) but replaces the
// content source registry. This is used by ContentSourceHolder to build a
// fresh pipeline whenever the source registry is rebuilt at runtime, without
// reconstructing the extractor stack.
// SEM@8429fbdd74c6f347eff47e11551b900e16a1dc06: build a new ContentPipeline reusing base settings but with a different source registry (pure)
func RebuildPipelineWithSources(base *ContentPipeline, sources *ContentSourceRegistry) *ContentPipeline {
	p := &ContentPipeline{
		sources:    sources,
		extractors: base.extractors,
		matcher:    base.matcher,
		limiter:    base.limiter,
		limits:     base.limits,
		dumper:     base.dumper,
	}
	return p
}

// Extract fetches bytes from the appropriate source and extracts text.
// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: fetch and extract text from a URI using the matching source and extractor
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
			return extract.ExtractWithDeadline(ctx, p.limits.WallClockBudget, func(dctx context.Context) (ExtractedContent, error) {
				return ce.ExtractCtx(dctx, data, contentType)
			})
		}
		// Legacy path — extractor isn't ctx-aware. The deadline still fires
		// at the goroutine boundary, but in-flight I/O continues until it
		// finishes naturally; the pipeline returns DeadlineExceeded promptly
		// while the goroutine drains in the background.
		return extract.ExtractWithDeadline(ctx, p.limits.WallClockBudget, func(_ context.Context) (ExtractedContent, error) {
			return ext.Extract(data, contentType)
		})
	}
	return ext.Extract(data, contentType)
}

// ExtractionClassification describes how a typed extractor error maps to
// access_status + access_reason_code. The reason code comes from
// extract.ClassifyError; access_status is the monolith-owned overlay.
// SEM@3de5c96824ab56d75179c1213960ce962da87ec7: structured result describing the outcome or failure reason of a content extraction (pure)
type ExtractionClassification struct {
	Status       string
	ReasonCode   string
	ReasonDetail string
}

// ClassifyExtractionError classifies a typed extractor error and attaches
// the monolith-owned access_status. The reason-code classification is
// delegated to extract.ClassifyError (the relocated library logic); a
// non-empty reason code maps to AccessStatusExtractionFailed.
// SEM@d1fd850907490887fd11a6ccd4a691326ede6e4e: convert an extraction error into a structured ExtractionClassification (pure)
func ClassifyExtractionError(err error) ExtractionClassification {
	c := extract.ClassifyError(err)
	out := ExtractionClassification{ReasonCode: c.ReasonCode, ReasonDetail: c.ReasonDetail}
	if c.ReasonCode != "" {
		out.Status = AccessStatusExtractionFailed
	}
	return out
}

// ExtractForDocument is a document-aware variant of Extract. It runs the
// usual fetch + extract pipeline, and on success — if a dev/test-only
// dumper is configured — also persists the extracted markdown as a Note on
// the document's parent threat model. Note creation failures are logged but
// do not affect the returned ExtractedContent or error: the dump hook is
// strictly an inspection aid and must not change the production behavior of
// the pipeline.
// SEM@117032a3c5523a04e970f76a285e342169d5150c: extract content for a document and optionally dump the result as a note
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
// SEM@117032a3c5523a04e970f76a285e342169d5150c: dev-mode hook that persists extracted text as threat-model notes (reads DB)
type extractedTextNoteDumper struct {
	notes     NoteRepository
	documents DocumentRepository
}

// NewExtractedTextNoteDumper builds a dumper. notes/documents must be non-nil.
// SEM@117032a3c5523a04e970f76a285e342169d5150c: build an extractedTextNoteDumper backed by note and document repositories (pure)
func NewExtractedTextNoteDumper(notes NoteRepository, documents DocumentRepository) *extractedTextNoteDumper {
	return &extractedTextNoteDumper{notes: notes, documents: documents}
}

// SEM@117032a3c5523a04e970f76a285e342169d5150c: store extracted document text as a new note under the parent threat model (reads DB)
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

// FetchForPublish performs only the fetch step (FindSource + Fetch) of the
// pipeline and returns the raw bytes and content-type. It does NOT run any
// extractor. This is the seam used by the async extraction path to obtain
// bytes for publishing to the worker pipeline; the worker performs the actual
// extraction. The same per-user concurrency limiter that guards Extract is
// also applied here so that concurrent fetch-for-publish calls are subject to
// the same cap.
// SEM@d994c2f113f9e0997f83a0815018638cc94111f7: fetch raw content bytes and content type for a URI without text extraction
func (p *ContentPipeline) FetchForPublish(ctx context.Context, uri string) ([]byte, string, error) {
	logger := slogging.Get()

	src, ok := p.sources.FindSource(ctx, uri)
	if !ok {
		return nil, "", fmt.Errorf("no content source can handle URI: %s", uri)
	}

	userID, _ := UserIDFromContext(ctx)
	if p.limiter != nil && userID != "" {
		release, err := p.limiter.acquire(ctx, userID)
		if err != nil {
			return nil, "", err
		}
		defer release()
	}

	logger.Debug("ContentPipeline.FetchForPublish: fetching %s via source %s", uri, src.Name())
	data, contentType, err := src.Fetch(ctx, uri)
	if err != nil {
		return nil, "", fmt.Errorf("source %s fetch failed: %w", src.Name(), err)
	}
	return data, contentType, nil
}

// Matcher returns the pipeline's URL pattern matcher.
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: fetch the pipeline's URL pattern matcher (pure)
func (p *ContentPipeline) Matcher() *URLPatternMatcher {
	return p.matcher
}

// Sources returns the pipeline's source registry.
// SEM@d98d9d1c0d122ade355ea522b49b132e523ebf52: fetch the pipeline's content source registry (pure)
func (p *ContentPipeline) Sources() *ContentSourceRegistry {
	return p.sources
}
