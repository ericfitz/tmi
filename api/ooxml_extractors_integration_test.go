package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ooxmlStubSource always returns the configured bytes + content type.
type ooxmlStubSource struct {
	data []byte
	ct   string
}

func (s *ooxmlStubSource) Name() string                                   { return "stub" }
func (s *ooxmlStubSource) CanHandle(ctx context.Context, uri string) bool { return true }
func (s *ooxmlStubSource) Fetch(ctx context.Context, uri string) ([]byte, string, error) {
	return s.data, s.ct, nil
}

func TestContentPipeline_HappyPath_DOCX(t *testing.T) {
	docx := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})
	srcs := NewContentSourceRegistry()
	srcs.Register(&ooxmlStubSource{data: docx, ct: docxContentType})

	exts := NewContentExtractorRegistry()
	exts.Register(NewDOCXExtractor(defaultOOXMLLimits()))

	cl := newConcurrencyLimiter(2, nil)
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, DefaultPipelineLimits())

	ctx := WithUserID(context.Background(), "alice")
	out, err := p.Extract(ctx, "https://example.com/doc.docx")
	require.NoError(t, err)
	assert.Contains(t, out.Text, "Title")
}

// sleepExtractor lets the deadline wrapper time out during extraction.
type sleepExtractor struct{ d time.Duration }

func (s *sleepExtractor) Name() string             { return "sleep" }
func (s *sleepExtractor) CanHandle(ct string) bool { return ct == "application/sleep" }
func (s *sleepExtractor) Bounded() bool            { return true }
func (s *sleepExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	time.Sleep(s.d)
	return ExtractedContent{Text: "should not arrive"}, nil
}

func TestContentPipeline_TimeoutClassifiesExtractionFailedTimeout(t *testing.T) {
	srcs := NewContentSourceRegistry()
	srcs.Register(&ooxmlStubSource{data: []byte("stub"), ct: "application/sleep"})
	exts := NewContentExtractorRegistry()
	exts.Register(&sleepExtractor{d: 200 * time.Millisecond})

	cl := newConcurrencyLimiter(2, nil)
	cfg := DefaultPipelineLimits()
	cfg.WallClockBudget = 50 * time.Millisecond
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, cfg)
	ctx := WithUserID(context.Background(), "alice")
	_, err := p.Extract(ctx, "https://example.com/x")
	require.Error(t, err)
	classified := ClassifyExtractionError(err)
	assert.Equal(t, AccessStatusExtractionFailed, classified.Status)
	assert.Equal(t, ReasonExtractionLimitTimeout, classified.ReasonCode)
}

// countingExtractor wraps an inner extractor and tracks max concurrency.
type countingExtractor struct {
	inner       ContentExtractor
	current     atomic.Int32
	maxObserved atomic.Int32
}

func (c *countingExtractor) Name() string             { return "counting" }
func (c *countingExtractor) CanHandle(ct string) bool { return c.inner.CanHandle(ct) }
func (c *countingExtractor) Bounded() bool            { return true }
func (c *countingExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	n := c.current.Add(1)
	for {
		cur := c.maxObserved.Load()
		if n <= cur || c.maxObserved.CompareAndSwap(cur, n) {
			break
		}
	}
	defer c.current.Add(-1)
	time.Sleep(20 * time.Millisecond)
	return c.inner.Extract(data, ct)
}

func TestContentPipeline_ConcurrencyCap_DefaultTwo(t *testing.T) {
	docx := buildZip(t, map[string][]byte{"word/document.xml": []byte(minimalDocxBody)})
	srcs := NewContentSourceRegistry()
	srcs.Register(&ooxmlStubSource{data: docx, ct: docxContentType})
	wrapped := &countingExtractor{inner: NewDOCXExtractor(defaultOOXMLLimits())}
	exts := NewContentExtractorRegistry()
	exts.Register(wrapped)
	cl := newConcurrencyLimiter(2, nil)
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, DefaultPipelineLimits())
	ctx := WithUserID(context.Background(), "alice")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = p.Extract(ctx, "x") }()
	}
	wg.Wait()
	observed := wrapped.maxObserved.Load()
	assert.LessOrEqual(t, observed, int32(2), "must never exceed default per-user cap")
}

func TestClassifyExtractionError_LimitsMappedCorrectly(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"compressed_size", &extractionLimitError{Kind: "compressed_size"}, ReasonExtractionLimitCompressedSize},
		{"decompressed_size", &extractionLimitError{Kind: "decompressed_size"}, ReasonExtractionLimitDecompressedSize},
		{"part_size", &extractionLimitError{Kind: "part_size"}, ReasonExtractionLimitPartSize},
		{"part_count", &extractionLimitError{Kind: "part_count"}, ReasonExtractionLimitPartCount},
		{"markdown_size", &extractionLimitError{Kind: "markdown_size"}, ReasonExtractionLimitMarkdownSize},
		{"xml_depth", &extractionLimitError{Kind: "xml_depth"}, ReasonExtractionLimitXMLDepth},
		{"zip_nested", &extractionLimitError{Kind: "zip_nested"}, ReasonExtractionLimitZipNested},
		{"zip_path", &extractionLimitError{Kind: "zip_path"}, ReasonExtractionLimitZipPath},
		{"compression_ratio", &extractionLimitError{Kind: "compression_ratio"}, ReasonExtractionLimitCompressionRatio},
		{"malformed_wrapped", fmt.Errorf("wrap: %w", ErrMalformed), ReasonExtractionMalformed},
		{"unsupported_wrapped", fmt.Errorf("wrap: %w", ErrUnsupported), ReasonExtractionUnsupported},
		{"context.DeadlineExceeded", context.DeadlineExceeded, ReasonExtractionLimitTimeout},
		{"internal", errors.New("random failure"), ReasonExtractionInternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ClassifyExtractionError(c.err)
			assert.Equal(t, AccessStatusExtractionFailed, r.Status)
			assert.Equal(t, c.want, r.ReasonCode)
		})
	}
}

func TestClassifyExtractionError_NilReturnsZero(t *testing.T) {
	r := ClassifyExtractionError(nil)
	assert.Equal(t, "", r.Status)
	assert.Equal(t, "", r.ReasonCode)
}
