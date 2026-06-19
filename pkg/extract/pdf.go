package extract

import (
	"fmt"
	"os"
	"strings"

	pdflib "github.com/ledongthuc/pdf"
)

// PDFExtractor extracts plain text from PDF binary content.
// Because the PDF library requires a seekable reader, the bytes are written
// to a temporary file which is removed after extraction.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: extractor that converts PDF binary content to plain text via a temporary file (pure)
type PDFExtractor struct{}

// NewPDFExtractor creates a new PDFExtractor.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: build a PDFExtractor instance (pure)
func NewPDFExtractor() *PDFExtractor { return &PDFExtractor{} }

// Name returns the extractor name.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: return the canonical name of the PDF extractor (pure)
func (e *PDFExtractor) Name() string { return "pdf" }

// CanHandle returns true for application/pdf content types.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: validate that a content type is application/pdf (pure)
func (e *PDFExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/pdf")
}

// Extract writes the PDF bytes to a temp file and extracts plain text page by page.
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: convert PDF bytes to plain text via a temporary file, returning extracted content
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
	// Close before opening with pdflib so the file is fully flushed.
	if err := tmpFile.Close(); err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to close PDF temp file: %w", err)
	}

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
// It is intentionally named differently from extractTextFromPDF in
// api/timmy_content_provider_pdf.go (the monolith keeps its own copy under
// that name; the names differ to avoid confusion when reading both files).
// SEM@d1c9c93fe4dd63680a390679e8df436b39c27a8b: parse a PDF file path and extract concatenated plain text across all pages (pure)
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
			// Skip pages that cannot be read rather than aborting.
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()), nil
}
