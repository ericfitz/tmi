package api

import "github.com/ericfitz/tmi/pkg/extract"

// Type aliases re-exporting pkg/extract into the api package. The extractor
// logic was relocated to pkg/extract during Component Platform Plan 2 (#347)
// so the sandboxed worker can link it without pulling in Gin/GORM. These
// aliases keep the monolith's many call sites unchanged.
type (
	ExtractedContent         = extract.ExtractedContent
	EntityReference          = extract.EntityReference
	ContentExtractor         = extract.ContentExtractor
	ContextAwareExtractor    = extract.ContextAwareExtractor
	BoundedExtractor         = extract.BoundedExtractor
	ContentExtractorRegistry = extract.ContentExtractorRegistry
	TextChunker              = extract.TextChunker
)

// Sentinel-error re-exports.
var (
	ErrExtractionLimit = extract.ErrExtractionLimit
	ErrMalformed       = extract.ErrMalformed
	ErrUnsupported     = extract.ErrUnsupported
)

// Constructor re-exports.
var (
	NewContentExtractorRegistry = extract.NewContentExtractorRegistry
	NewDOCXExtractor            = extract.NewDOCXExtractor
	NewPPTXExtractor            = extract.NewPPTXExtractor
	NewXLSXExtractor            = extract.NewXLSXExtractor
	NewPDFExtractor             = extract.NewPDFExtractor
	NewHTMLExtractor            = extract.NewHTMLExtractor
	NewPlainTextExtractor       = extract.NewPlainTextExtractor
	NewTextChunker              = extract.NewTextChunker
)
