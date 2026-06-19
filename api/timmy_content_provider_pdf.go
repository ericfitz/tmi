package api

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	pdflib "github.com/ledongthuc/pdf"

	"github.com/ericfitz/tmi/internal/slogging"
)

// PDFEmbeddingSource fetches PDF documents and extracts their text content.
// It uses SafeHTTPClient (DNS-pinned, SSRF-checked) and caps downloads at 50 MiB.
// SEM@b554bb5371f70e0115912131e032671de29e8c09: embedding source that fetches and extracts text from remote PDF documents (pure)
type PDFEmbeddingSource struct {
	client *SafeHTTPClient
}

// NewPDFEmbeddingSource creates a new PDFEmbeddingSource with the given SSRF validator.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: build a PDF embedding source with SSRF-safe HTTP client and 50 MiB download cap (pure)
func NewPDFEmbeddingSource(ssrfValidator *URIValidator) *PDFEmbeddingSource {
	return &PDFEmbeddingSource{
		client: NewSafeHTTPClient(
			ssrfValidator,
			WithDefaultTimeouts(60*time.Second, 15*time.Second, 50*1024*1024),
		),
	}
}

// Name returns the provider name for logging.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: return the provider name for the PDF embedding source (pure)
func (p *PDFEmbeddingSource) Name() string { return "pdf" }

// CanHandle returns true for entity references whose URI ends with ".pdf" (case-insensitive).
// SEM@80346558ce851de593c85a2d5660f92a649b1686: report whether an entity reference URI ends with .pdf (pure)
func (p *PDFEmbeddingSource) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(ref.URI), ".pdf")
}

// Extract fetches a PDF from the given URI via the egress helper (DNS-pinned,
// SSRF-checked), writes it to a temp file, and extracts plain text. The
// download is limited to 50 MiB.
// SEM@80346558ce851de593c85a2d5660f92a649b1686: fetch a PDF from a URI via SSRF-checked client and extract its plain text
func (p *PDFEmbeddingSource) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	logger := slogging.Get()

	resp, err := p.client.FetchStreaming(ctx, ref.URI, SafeFetchOptions{
		MaxBodyBytes: 50 * 1024 * 1024,
	})
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("SSRF check failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// PDF libraries require a seekable reader, so write to a temp file first.
	tmpFile, err := os.CreateTemp("", "timmy-pdf-*.pdf")
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to download PDF: %w", err)
	}

	text, err := extractTextFromPDF(tmpPath)
	if err != nil {
		logger.Error("Failed to extract text from PDF %s: %v", ref.URI, err)
		return ExtractedContent{}, fmt.Errorf("failed to extract PDF text: %w", err)
	}

	return ExtractedContent{
		Text:        text,
		Title:       ref.Name,
		ContentType: "application/pdf",
	}, nil
}

// extractTextFromPDF opens a PDF file and extracts plain text page by page.
// SEM@f481f64fd69f2ddaaf71768f305f9fcdec86ec2d: parse a PDF file and concatenate plain text from all readable pages (pure)
func extractTextFromPDF(filePath string) (string, error) {
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
			// Skip pages that cannot be read rather than aborting
			continue
		}
		sb.WriteString(text)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String()), nil
}
