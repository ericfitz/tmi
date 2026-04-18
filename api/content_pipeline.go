package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Provider name constants
const (
	ProviderConfluence      = "confluence"
	ProviderGoogleDrive     = "google_drive"
	ProviderGoogleWorkspace = "google_workspace"
	ProviderHTTP            = "http"
	ProviderOneDrive        = "onedrive"
)

// Document access status constants
const (
	AccessStatusUnknown       = "unknown"
	AccessStatusAccessible    = "accessible"
	AccessStatusPendingAccess = "pending_access"
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
