package api

import "context"

// ContentExtractor converts raw bytes into plain text.
type ContentExtractor interface {
	Name() string
	CanHandle(contentType string) bool
	Extract(data []byte, contentType string) (ExtractedContent, error)
}

// ContextAwareExtractor is implemented by extractors that can receive a
// deadline-bearing context for cooperative cancellation. When an extractor
// implements both BoundedExtractor and ContextAwareExtractor, the pipeline's
// extractWithDeadline wrapper calls ExtractCtx with the timeout-bounded
// context so any wall-clock cancellation aborts in-flight reads through
// the archive's boundedReader (which checks ctx.Err() per Read call).
//
// Extractors that only implement BoundedExtractor (without
// ContextAwareExtractor) still get a wall-clock deadline at the goroutine
// boundary, but in-flight I/O continues until it finishes naturally —
// the pipeline returns DeadlineExceeded promptly while the extractor
// goroutine drains in the background.
//
// Extract should remain implemented as the legacy entry point and
// typically delegates to ExtractCtx with a context.Background().
type ContextAwareExtractor interface {
	ExtractCtx(ctx context.Context, data []byte, contentType string) (ExtractedContent, error)
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

// BoundedExtractor is implemented by extractors that must run under a
// wall-clock deadline (CPU- or memory-heavy extractors that could otherwise
// run indefinitely on adversarial input). The pipeline calls Bounded() to
// detect the requirement; the value is informational and always true for
// types that implement it.
type BoundedExtractor interface {
	Bounded() bool
}
