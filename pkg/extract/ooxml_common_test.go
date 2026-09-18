package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify extraction limit error matches its sentinel and reports kind and sizes
func TestExtractionLimitError_IsAndUnwrap(t *testing.T) {
	e := &extractionLimitError{Kind: "compressed_size", Limit: 100, Observed: 200}
	assert.True(t, errors.Is(e, ErrExtractionLimit))
	assert.False(t, errors.Is(e, ErrMalformed))
	assert.Contains(t, e.Error(), "compressed_size")
	assert.Contains(t, e.Error(), "100")
	assert.Contains(t, e.Error(), "200")
	assert.NotContains(t, e.Error(), "detail=")
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify extraction limit error message includes optional detail
func TestExtractionLimitError_WithDetail(t *testing.T) {
	e := &extractionLimitError{Kind: "part_count", Limit: 250, Observed: 251, Detail: "slide #251"}
	assert.Contains(t, e.Error(), "slide #251")
	assert.Contains(t, e.Error(), `detail=`)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify markdown builder fails once output exceeds its byte bound
func TestMarkdownBuilder_BoundsTrip(t *testing.T) {
	b := newMarkdownBuilder(8)
	_, err := b.WriteString("12345")
	assert.NoError(t, err)
	_, err = b.WriteString("678")
	assert.NoError(t, err)
	_, err = b.WriteString("9")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrExtractionLimit))
	// No partial output should be retrievable beyond the cap.
	assert.LessOrEqual(t, b.Len(), 8)
	// Prior writes must be intact after the cap trip.
	assert.Equal(t, "12345678", b.String())
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify markdown builder enforces its bound on single-byte writes
func TestMarkdownBuilder_WriteByte(t *testing.T) {
	b := newMarkdownBuilder(3)
	assert.NoError(t, b.WriteByte('a'))
	assert.NoError(t, b.WriteByte('b'))
	assert.NoError(t, b.WriteByte('c'))
	err := b.WriteByte('d')
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrExtractionLimit))
	assert.Equal(t, "abc", b.String(), "successful writes must be preserved on cap trip")
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify markdown builder accepts output under its byte bound
func TestMarkdownBuilder_BelowBound(t *testing.T) {
	b := newMarkdownBuilder(64)
	_, err := b.WriteString("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", b.String())
}

// buildZip is a tiny helper that builds an in-memory OOXML-shaped archive
// from a name -> bytes map. Used by all OOXML extractor tests.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: build an in-memory zip archive from named parts for tests (pure)
func buildZip(t *testing.T, parts map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, data := range parts {
		f, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%s): %v", name, err)
		}
		if _, err := f.Write(data); err != nil {
			t.Fatalf("zip write(%s): %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener rejects an archive over the compressed size limit
func TestOOXMLOpener_RejectsCompressedTooLarge(t *testing.T) {
	o := newOOXMLOpener(Limits{
		CompressedSizeBytes: 100, DecompressedSizeBytes: 1000, PartSizeBytes: 1000,
		MaxCompressionRatio: 100,
	})
	data := make([]byte, 200) // > 100
	_, err := o.open(data)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "compressed_size", le.Kind)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener rejects archive entries with path traversal
func TestOOXMLOpener_RejectsPathTraversal(t *testing.T) {
	o := newOOXMLOpener(DefaultLimits())
	data := buildZip(t, map[string][]byte{
		"../escape.xml": []byte("<x/>"),
	})
	_, err := o.open(data)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T (err=%v)", err, err)
	}
	assert.Equal(t, "zip_path", le.Kind)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener rejects absolute and backslash entry paths
func TestOOXMLOpener_RejectsAbsoluteAndBackslashPath(t *testing.T) {
	o := newOOXMLOpener(DefaultLimits())
	for _, name := range []string{"/abs/path.xml", `with\backslash.xml`} {
		data := buildZip(t, map[string][]byte{name: []byte("<x/>")})
		_, err := o.open(data)
		assert.Error(t, err, "name=%q", name)
		var le *extractionLimitError
		if !errors.As(err, &le) {
			t.Fatalf("name=%q: expected extractionLimitError, got %T", name, err)
		}
		assert.Equal(t, "zip_path", le.Kind, "name=%q", name)
	}
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener rejects an archive nested inside the archive
func TestOOXMLOpener_RejectsNestedZip(t *testing.T) {
	inner := buildZip(t, map[string][]byte{"a.xml": []byte("<x/>")})
	outer := buildZip(t, map[string][]byte{"nested.zip": inner})
	o := newOOXMLOpener(DefaultLimits())
	z, err := o.open(outer)
	assert.NoError(t, err, "open should succeed; member-level streaming detects nesting")
	_, err = z.openMember("nested.zip")
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "zip_nested", le.Kind)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener rejects an entry with an excessive compression ratio
func TestOOXMLOpener_RejectsCompressionRatioBomb(t *testing.T) {
	// Build a single member with extreme compression ratio. zlib compresses
	// long runs of zeros down to a tiny payload.
	big := bytes.Repeat([]byte{0}, 200_000) // 200 KB
	data := buildZip(t, map[string][]byte{"document.xml": big})
	limits := DefaultLimits()
	limits.MaxCompressionRatio = 5 // adversarially low
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)
	_, err = z.openMember("document.xml")
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "compression_ratio", le.Kind)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener fails when one part exceeds the decompressed size limit
func TestOOXMLOpener_TripsPartSize(t *testing.T) {
	// member exceeds part size cap; the limit may fire at openMember (via the
	// UncompressedSize64 header check) or during io.ReadAll (via boundedReader
	// for archives where the stored size is zero/wrong). Either path must
	// return an extractionLimitError with Kind=="part_size".
	data := buildZip(t, map[string][]byte{"big.xml": bytes.Repeat([]byte("a"), 5_000)})
	limits := DefaultLimits()
	limits.PartSizeBytes = 1_000
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)
	r, openErr := z.openMember("big.xml")
	if openErr != nil {
		// Limit fired at openMember — acceptable.
		var le *extractionLimitError
		if !errors.As(openErr, &le) {
			t.Fatalf("expected extractionLimitError from openMember, got %T: %v", openErr, openErr)
		}
		assert.Equal(t, "part_size", le.Kind)
		return
	}
	// Limit should fire during streaming.
	_, err = io.ReadAll(r)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError from ReadAll, got %T", err)
	}
	assert.Equal(t, "part_size", le.Kind)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify OOXML opener fails when total decompressed bytes exceed the limit
func TestOOXMLOpener_TripsCumulativeDecompressed(t *testing.T) {
	a := bytes.Repeat([]byte("a"), 800)
	b := bytes.Repeat([]byte("b"), 800)
	data := buildZip(t, map[string][]byte{"a.xml": a, "b.xml": b})
	limits := DefaultLimits()
	limits.DecompressedSizeBytes = 1_000
	limits.PartSizeBytes = 1_000
	o := newOOXMLOpener(limits)
	z, err := o.open(data)
	assert.NoError(t, err)

	r1, err := z.openMember("a.xml")
	assert.NoError(t, err)
	_, err = io.ReadAll(r1)
	assert.NoError(t, err)

	r2, err := z.openMember("b.xml")
	assert.NoError(t, err)
	_, err = io.ReadAll(r2)
	assert.Error(t, err)
	var le *extractionLimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected extractionLimitError, got %T", err)
	}
	assert.Equal(t, "decompressed_size", le.Kind)
}

// TestBoundedXMLDecoder_DecodeElementNoDrift verifies that mixing Token() and
// DecodeElement() does not accumulate depth-counter drift. Before the fix,
// each DecodeElement call leaked +1 into b.depth because the matching
// EndElement was consumed internally by the underlying decoder without going
// through our Token() wrapper. After N siblings the phantom depth would exceed
// maxDepth, causing false xml_depth trips.
// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify bounded XML decoder depth stays accurate across element decodes
func TestBoundedXMLDecoder_DecodeElementNoDrift(t *testing.T) {
	// Three siblings processed via DecodeElement: depth must return to 0 at EOF.
	src := bytes.NewReader([]byte(`<root><item>a</item><item>b</item><item>c</item></root>`))
	d := newBoundedXMLDecoder(src, 10)
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.NoError(t, err, "unexpected error from Token()")
		start, ok := tok.(xml.StartElement)
		if ok && start.Name.Local == "item" {
			var s string
			err = d.DecodeElement(&s, &start)
			assert.NoError(t, err, "DecodeElement must not error")
		}
	}
	// After processing root + 3 items, depth counter must be 0 (fully balanced).
	assert.Equal(t, 0, d.depth, "depth must return to 0 after all elements consumed")
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify bounded XML decoder parses a shallow document without error
func TestBoundedXMLDecoder_HappyPath(t *testing.T) {
	src := bytes.NewReader([]byte(`<root><a>x</a><b>y</b></root>`))
	d := newBoundedXMLDecoder(src, 10)
	for {
		_, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		assert.NoError(t, err)
	}
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify bounded XML decoder fails when nesting exceeds the depth limit
func TestBoundedXMLDecoder_TripsDepth(t *testing.T) {
	// 6 nested elements with maxDepth=4 should trip on the 5th open.
	xmlStr := `<a><b><c><d><e><f>x</f></e></d></c></b></a>`
	src := bytes.NewReader([]byte(xmlStr))
	d := newBoundedXMLDecoder(src, 4)
	tripped := false
	for {
		_, err := d.Token()
		if err == nil {
			continue
		}
		if errors.Is(err, ErrExtractionLimit) {
			var le *extractionLimitError
			errors.As(err, &le)
			assert.Equal(t, "xml_depth", le.Kind)
			tripped = true
			break
		}
		if errors.Is(err, io.EOF) {
			break
		}
		t.Fatalf("unexpected error: %v", err)
	}
	assert.True(t, tripped, "depth limit must trip")
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify deadline-bounded extraction returns the extractor result
func TestExtractWithDeadline_Happy(t *testing.T) {
	ctx := context.Background()
	out, err := ExtractWithDeadline(ctx, 200*time.Millisecond, func(ctx context.Context) (ExtractedContent, error) {
		return ExtractedContent{Text: "ok"}, nil
	})
	assert.NoError(t, err)
	assert.Equal(t, "ok", out.Text)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify deadline-bounded extraction fails when the extractor overruns
func TestExtractWithDeadline_Timeout(t *testing.T) {
	ctx := context.Background()
	_, err := ExtractWithDeadline(ctx, 50*time.Millisecond, func(ctx context.Context) (ExtractedContent, error) {
		select {
		case <-ctx.Done():
			return ExtractedContent{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
			return ExtractedContent{}, nil
		}
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify deadline-bounded extraction stops on parent context cancel
func TestExtractWithDeadline_ParentCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := ExtractWithDeadline(ctx, 5*time.Second, func(ctx context.Context) (ExtractedContent, error) {
		<-ctx.Done()
		return ExtractedContent{}, ctx.Err()
	})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify context-aware reader fails reads after cancellation
func TestCtxReader_CancelsOnContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := newCtxReader(ctx, strings.NewReader("data"))
	_, err := r.Read(make([]byte, 4))
	assert.ErrorIs(t, err, context.Canceled)
}

// SEM@b4a403da2147ccb51a674e10d71891d4fccfe06a: verify context-aware reader passes data through before cancellation
func TestCtxReader_PassesThroughBeforeCancel(t *testing.T) {
	ctx := context.Background()
	r := newCtxReader(ctx, strings.NewReader("hello"))
	buf := make([]byte, 5)
	n, err := r.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))
}

// A panicking extractor (adversarial input tripping a parser bug, e.g.
// GO-2026-6452) must surface as ErrMalformed, not crash the process.
// SEM@4359a42e5ac4f24a1d76d8c8c8455c6cc91541c5: verify ExtractWithDeadline converts an extractor panic into ErrMalformed (pure)
func TestExtractWithDeadlineContainsPanic(t *testing.T) {
	_, err := ExtractWithDeadline(context.Background(), time.Second, func(context.Context) (ExtractedContent, error) {
		panic("runtime error: index out of range [-1]")
	})
	if !errors.Is(err, ErrMalformed) {
		t.Fatalf("want ErrMalformed, got %v", err)
	}
}
