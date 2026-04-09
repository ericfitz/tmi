package api

import "strings"

// PlainTextExtractor extracts content from plain text and CSV content types.
type PlainTextExtractor struct{}

// NewPlainTextExtractor creates a new PlainTextExtractor.
func NewPlainTextExtractor() *PlainTextExtractor { return &PlainTextExtractor{} }

// Name returns the extractor name.
func (e *PlainTextExtractor) Name() string { return "plaintext" }

// CanHandle returns true for text/plain and text/csv content types.
func (e *PlainTextExtractor) CanHandle(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/plain") || strings.HasPrefix(ct, "text/csv")
}

// Extract returns the raw bytes as a string with no transformation.
func (e *PlainTextExtractor) Extract(data []byte, contentType string) (ExtractedContent, error) {
	return ExtractedContent{
		Text:        string(data),
		ContentType: contentType,
	}, nil
}
