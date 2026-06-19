package extract

import "strings"

// PlainTextExtractor extracts content from plain text and CSV content types.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: extractor for plain text and CSV content types (pure)
type PlainTextExtractor struct{}

// NewPlainTextExtractor creates a new PlainTextExtractor.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: build a PlainTextExtractor (pure)
func NewPlainTextExtractor() *PlainTextExtractor { return &PlainTextExtractor{} }

// Name returns the extractor name.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: return the extractor's canonical name (pure)
func (e *PlainTextExtractor) Name() string { return "plaintext" }

// CanHandle returns true for text/plain and text/csv content types.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: report whether the extractor handles text/plain or text/csv content types (pure)
func (e *PlainTextExtractor) CanHandle(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/csv")
}

// Extract returns the raw bytes as a string with no transformation.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert raw bytes to extracted content with no transformation (pure)
func (e *PlainTextExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	return ExtractedContent{
		Text:        string(data),
		ContentType: contentType,
	}, nil
}
