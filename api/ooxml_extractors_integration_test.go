package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ericfitz/tmi/pkg/extract"
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
	exts.Register(NewDOCXExtractor(extract.DefaultLimits()))

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
	wrapped := &countingExtractor{inner: NewDOCXExtractor(extract.DefaultLimits())}
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

// TestClassifyExtractionError_LimitsMappedCorrectly verifies the
// monolith-owned overlay: ClassifyExtractionError delegates reason-code
// classification to extract.ClassifyError (covered exhaustively per-Kind by
// pkg/extract's own TestClassifyError_LimitsMappedCorrectly) and attaches
// access_status. A representative slice of limit, sentinel, timeout, and
// internal errors confirms the reason code + detail flow through and that
// Status is set whenever a reason code is produced.
func TestClassifyExtractionError_LimitsMappedCorrectly(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		want       string
		wantDetail string
	}{
		{"compressed_size", extract.NewLimitError("compressed_size", ""), ReasonExtractionLimitCompressedSize, ""},
		{"part_size", extract.NewLimitError("part_size", "word/document.xml"), ReasonExtractionLimitPartSize, "word/document.xml"},
		{"part_count_with_detail", extract.NewLimitError("part_count", "slide #42"), ReasonExtractionLimitPartCount, "slide #42"},
		{"zip_nested", extract.NewLimitError("zip_nested", "nested.zip"), ReasonExtractionLimitZipNested, "nested.zip"},
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

// ctxCheckingReader wraps an io.Reader and returns the context's error
// before every Read once the context is done, so a slow read loop unblocks
// promptly on wall-clock cancellation. It mirrors the cooperative
// cancellation that pkg/extract's boundedReader applies to OOXML parts.
type ctxCheckingReader struct {
	r   io.Reader
	ctx context.Context
}

func (c *ctxCheckingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// slowReadingExtractor is a ContextAwareExtractor whose ExtractCtx
// deliberately slow-reads its input one byte at a time, sleeping between
// reads, through a context-checking reader. It is the harness for
// TestContentPipeline_TimeoutAbortsExtractionMidStream: with the
// deadline-bearing context wired in by the pipeline through ExtractCtx,
// the slow read loop must abort promptly when the wall-clock deadline
// fires rather than running to completion.
//
// The extractor-internal boundedReader cancellation path is covered by
// pkg/extract's own tests; this harness verifies only that the pipeline
// hands the deadline-bearing context to a ContextAwareExtractor.
type slowReadingExtractor struct {
	ct        string
	stepDelay time.Duration
}

func (e *slowReadingExtractor) Name() string             { return "slow" }
func (e *slowReadingExtractor) CanHandle(ct string) bool { return ct == e.ct }
func (e *slowReadingExtractor) Bounded() bool            { return true }
func (e *slowReadingExtractor) Extract(data []byte, ct string) (ExtractedContent, error) {
	return e.ExtractCtx(context.Background(), data, ct)
}

func (e *slowReadingExtractor) ExtractCtx(ctx context.Context, data []byte, _ string) (ExtractedContent, error) {
	rdr := &ctxCheckingReader{r: bytes.NewReader(data), ctx: ctx}
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
// ContextAwareExtractor path, so a slow-reading extractor returns
// context.DeadlineExceeded shortly after the budget is exhausted instead
// of running to completion.
func TestContentPipeline_TimeoutAbortsExtractionMidStream(t *testing.T) {
	// A few hundred bytes of payload — the extractor reads it one byte at a
	// time, sleeping between reads, so the content itself is irrelevant.
	payload := make([]byte, 400)

	srcs := NewContentSourceRegistry()
	srcs.Register(&ooxmlStubSource{data: payload, ct: "application/slow"})
	exts := NewContentExtractorRegistry()
	// 5ms per-byte sleep over hundreds of bytes ensures the extractor's
	// natural runtime is on the order of seconds; without cooperative
	// cancellation the call would block well past the budget.
	exts.Register(&slowReadingExtractor{ct: "application/slow", stepDelay: 5 * time.Millisecond})

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
