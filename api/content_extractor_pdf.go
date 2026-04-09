package api

import (
	"fmt"
	"os"
	"strings"

	pdflib "github.com/ledongthuc/pdf"
)

// PDFExtractor extracts plain text from PDF binary content.
// Because the PDF library requires a seekable reader, the bytes are written
// to a temporary file which is removed after extraction.
type PDFExtractor struct{}

// NewPDFExtractor creates a new PDFExtractor.
func NewPDFExtractor() *PDFExtractor { return &PDFExtractor{} }

// Name returns the extractor name.
func (e *PDFExtractor) Name() string { return "pdf" }

// CanHandle returns true for application/pdf content types.
func (e *PDFExtractor) CanHandle(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "application/pdf")
}

// Extract writes the PDF bytes to a temp file and extracts plain text page by page.
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
// timmy_content_provider_pdf.go to avoid a package-level name collision.
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
