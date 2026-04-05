package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	pdflib "github.com/ledongthuc/pdf"

	"github.com/ericfitz/tmi/internal/slogging"
)

// PDFContentProvider fetches PDF documents and extracts their text content.
type PDFContentProvider struct {
	ssrfValidator *SSRFValidator
	client        *http.Client
}

// NewPDFContentProvider creates a new PDFContentProvider with the given SSRF validator.
func NewPDFContentProvider(ssrfValidator *SSRFValidator) *PDFContentProvider {
	return &PDFContentProvider{
		ssrfValidator: ssrfValidator,
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// Name returns the provider name for logging.
func (p *PDFContentProvider) Name() string { return "pdf" }

// CanHandle returns true for entity references whose URI ends with ".pdf" (case-insensitive).
func (p *PDFContentProvider) CanHandle(_ context.Context, ref EntityReference) bool {
	if ref.URI == "" {
		return false
	}
	return strings.HasSuffix(strings.ToLower(ref.URI), ".pdf")
}

// Extract fetches a PDF from the given URI, writes it to a temp file, and extracts plain text.
// The download is limited to 50 MiB. SSRF protection is enforced before the request is made.
func (p *PDFContentProvider) Extract(ctx context.Context, ref EntityReference) (ExtractedContent, error) {
	logger := slogging.Get()

	if err := p.ssrfValidator.Validate(ref.URI); err != nil {
		return ExtractedContent{}, fmt.Errorf("SSRF check failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ref.URI, nil)
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := p.client.Do(req) //nolint:gosec // URL is validated by SSRFValidator before reaching this point
	if err != nil {
		return ExtractedContent{}, fmt.Errorf("failed to fetch PDF: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

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

	const maxPDFSize = 50 * 1024 * 1024 // 50 MiB
	if _, err := io.Copy(tmpFile, io.LimitReader(resp.Body, maxPDFSize)); err != nil {
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
