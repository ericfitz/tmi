package extract

import "context"

// ExtractedContent holds the text extracted from a source entity.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: holds plain text and metadata produced by a content extractor (pure)
type ExtractedContent struct {
	Text        string            // Extracted plain text
	Title       string            // Document title if available
	ContentType string            // Original content type (e.g., "application/pdf")
	Metadata    map[string]string // Provider-specific metadata
}

// EntityReference identifies a source entity for content extraction.
// For DB-resident content (notes, assets), URI is empty and the provider
// reads directly from the database using EntityType + EntityID.
// For external content (documents with URLs), URI is the fetch target.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: identifies a source entity for content extraction by type, ID, and optional URI (pure)
type EntityReference struct {
	EntityType string // "asset", "threat", "document", "note", "diagram", "repository"
	EntityID   string // UUID of the source entity
	URI        string // External URL (empty for DB-resident content)
	Name       string // Display name for progress reporting
}

// ContentExtractor converts raw bytes into plain text.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: interface for converting raw bytes of a known content type into extracted text (pure)
type ContentExtractor interface {
	Name() string
	CanHandle(contentType string) bool
	Extract(data []byte, contentType string) (ExtractedContent, error)
}

// ContextAwareExtractor is implemented by extractors that can receive a
// deadline-bearing context for cooperative cancellation. When an extractor
// implements both BoundedExtractor and ContextAwareExtractor, the pipeline's
// ExtractWithDeadline wrapper calls ExtractCtx with the timeout-bounded
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
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: interface for extractors that accept a context for cooperative cancellation (pure)
type ContextAwareExtractor interface {
	ExtractCtx(ctx context.Context, data []byte, contentType string) (ExtractedContent, error)
}

// BoundedExtractor is implemented by extractors that must run under a
// wall-clock deadline (CPU- or memory-heavy extractors that could otherwise
// run indefinitely on adversarial input). The pipeline calls Bounded() to
// detect the requirement; the value is informational and always true for
// types that implement it.
// SEM@f7dfe970572e2574027691de97c695d5ae39d5b7: interface marking extractors that require a wall-clock deadline for safety (pure)
type BoundedExtractor interface {
	Bounded() bool
}
