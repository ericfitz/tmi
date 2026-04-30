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

	cl := NewConcurrencyLimiter(2, nil)
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

	cl := NewConcurrencyLimiter(2, nil)
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
	cl := NewConcurrencyLimiter(2, nil)
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
		name       string
		err        error
		want       string
		wantDetail string
	}{
		{"compressed_size", &extractionLimitError{Kind: "compressed_size"}, ReasonExtractionLimitCompressedSize, ""},
		{"decompressed_size", &extractionLimitError{Kind: "decompressed_size"}, ReasonExtractionLimitDecompressedSize, ""},
		{"part_size", &extractionLimitError{Kind: "part_size", Detail: "word/document.xml"}, ReasonExtractionLimitPartSize, "word/document.xml"},
		{"part_count_with_detail", &extractionLimitError{Kind: "part_count", Detail: "slide #42"}, ReasonExtractionLimitPartCount, "slide #42"},
		{"markdown_size", &extractionLimitError{Kind: "markdown_size"}, ReasonExtractionLimitMarkdownSize, ""},
		{"xml_depth", &extractionLimitError{Kind: "xml_depth"}, ReasonExtractionLimitXMLDepth, ""},
		{"zip_nested", &extractionLimitError{Kind: "zip_nested", Detail: "nested.zip"}, ReasonExtractionLimitZipNested, "nested.zip"},
		{"zip_path", &extractionLimitError{Kind: "zip_path"}, ReasonExtractionLimitZipPath, ""},
		{"compression_ratio", &extractionLimitError{Kind: "compression_ratio"}, ReasonExtractionLimitCompressionRatio, ""},
		{"malformed_wrapped", fmt.Errorf("wrap: %w", ErrMalformed), ReasonExtractionMalformed, ""},
		{"unsupported_wrapped", fmt.Errorf("wrap: %w", ErrUnsupported), ReasonExtractionUnsupported, ""},
		{"context.DeadlineExceeded", context.DeadlineExceeded, ReasonExtractionLimitTimeout, ""},
		{"internal", errors.New("random failure"), ReasonExtractionInternal, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := ClassifyExtractionError(c.err)
			assert.Equal(t, AccessStatusExtractionFailed, r.Status)
			assert.Equal(t, c.want, r.ReasonCode)
			assert.Equal(t, c.wantDetail, r.ReasonDetail)
		})
	}
}

func TestClassifyExtractionError_NilReturnsZero(t *testing.T) {
	r := ClassifyExtractionError(nil)
	assert.Equal(t, "", r.Status)
	assert.Equal(t, "", r.ReasonCode)
	assert.Equal(t, "", r.ReasonDetail)
}

// slowReadingExtractor is a ContextAwareExtractor whose ExtractCtx opens a
// real OOXML archive (so it traverses the same boundedReader code path as
// the production extractors) and then deliberately slow-reads the document
// part one byte at a time, sleeping between reads. It is the harness for
// TestContentPipeline_TimeoutAbortsExtractionMidStream: with the archive's
// extractionCtx wired in by WithContext, the slow read loop must abort
// promptly when the wall-clock deadline fires rather than running to
// completion.
type slowReadingExtractor struct {
	limits    ooxmlLimits
	stepDelay time.Duration
}

func (e *slowReadingExtractor) Name() string             { return "slow-docx" }
func (e *slowReadingExtractor) CanHandle(ct string) bool { return ct == docxContentType }
func (e *slowReadingExtractor) Bounded() bool            { return true }
func (e *slowReadingExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	return e.ExtractCtx(context.Background(), data, ct)
}

func (e *slowReadingExtractor) ExtractCtx(ctx context.Context, data []byte, _ string) (ExtractedContent, error) {
	opener := newOOXMLOpener(e.limits)
	arch, err := opener.open(data)
	if err != nil {
		return ExtractedContent{}, err
	}
	arch.WithContext(ctx)
	rdr, err := arch.openMember("word/document.xml")
	if err != nil {
		return ExtractedContent{}, err
	}
	defer func() { _ = rdr.Close() }()
	buf := make([]byte, 1)
	for {
		// Sleep before each read so the wall-clock deadline has the chance
		// to fire while the goroutine is parked.
		time.Sleep(e.stepDelay)
		_, rerr := rdr.Read(buf)
		if rerr != nil {
			return ExtractedContent{}, rerr
		}
	}
}

// TestContentPipeline_TimeoutAbortsExtractionMidStream verifies that a
// wall-clock deadline does not just unblock the pipeline goroutine — it
// also reaches into the extractor's in-flight I/O via the
// ContextAwareExtractor + boundedReader.extractionCtx path, so a
// slow-reading extractor returns context.DeadlineExceeded shortly after
// the budget is exhausted instead of running to completion.
func TestContentPipeline_TimeoutAbortsExtractionMidStream(t *testing.T) {
	// A small, valid DOCX archive — the extractor reads its document.xml
	// one byte at a time, sleeping between reads, so the document content
	// is irrelevant beyond being a well-formed OOXML archive.
	docx := buildZip(t, map[string][]byte{
		"word/document.xml": []byte(minimalDocxBody),
	})

	srcs := NewContentSourceRegistry()
	srcs.Register(&ooxmlStubSource{data: docx, ct: docxContentType})
	exts := NewContentExtractorRegistry()
	// 5ms per-byte sleep over hundreds of bytes of document.xml ensures the
	// extractor's natural runtime is on the order of seconds; without
	// cooperative cancellation the call would block well past the budget.
	exts.Register(&slowReadingExtractor{limits: defaultOOXMLLimits(), stepDelay: 5 * time.Millisecond})

	cl := NewConcurrencyLimiter(2, nil)
	cfg := DefaultPipelineLimits()
	cfg.WallClockBudget = 50 * time.Millisecond
	p := NewContentPipelineWithLimiter(srcs, exts, NewURLPatternMatcher(), cl, cfg)
	ctx := WithUserID(context.Background(), "alice")

	start := time.Now()
	_, err := p.Extract(ctx, "https://example.com/slow.docx")
	elapsed := time.Since(start)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded,
		"deadline must propagate as context.DeadlineExceeded")
	// With ctx-aware cancellation the goroutine must return shortly after
	// the 50ms budget. Without it, the goroutine would continue until the
	// extractor naturally finished (seconds).
	assert.Less(t, elapsed, 500*time.Millisecond,
		"deadline must abort in-flight I/O via ctxReader; elapsed=%s", elapsed)
}
