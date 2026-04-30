package api

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractionLimitError_IsAndUnwrap(t *testing.T) {
	e := &extractionLimitError{Kind: "compressed_size", Limit: 100, Observed: 200}
	assert.True(t, errors.Is(e, ErrExtractionLimit))
	assert.False(t, errors.Is(e, ErrMalformed))
	assert.Contains(t, e.Error(), "compressed_size")
	assert.Contains(t, e.Error(), "100")
	assert.Contains(t, e.Error(), "200")
	assert.NotContains(t, e.Error(), "detail=")
}

func TestExtractionLimitError_WithDetail(t *testing.T) {
	e := &extractionLimitError{Kind: "part_count", Limit: 250, Observed: 251, Detail: "slide #251"}
	assert.Contains(t, e.Error(), "slide #251")
	assert.Contains(t, e.Error(), `detail=`)
}

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

func TestMarkdownBuilder_BelowBound(t *testing.T) {
	b := newMarkdownBuilder(64)
	_, err := b.WriteString("hello")
	assert.NoError(t, err)
	assert.Equal(t, "hello", b.String())
}

// buildZip is a tiny helper that builds an in-memory OOXML-shaped archive
// from a name -> bytes map. Used by all OOXML extractor tests.
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

func TestOOXMLOpener_RejectsCompressedTooLarge(t *testing.T) {
	o := newOOXMLOpener(ooxmlLimits{
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

func TestOOXMLOpener_RejectsPathTraversal(t *testing.T) {
	o := newOOXMLOpener(defaultOOXMLLimits())
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

func TestOOXMLOpener_RejectsAbsoluteAndBackslashPath(t *testing.T) {
	o := newOOXMLOpener(defaultOOXMLLimits())
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

func TestOOXMLOpener_RejectsNestedZip(t *testing.T) {
	inner := buildZip(t, map[string][]byte{"a.xml": []byte("<x/>")})
	outer := buildZip(t, map[string][]byte{"nested.zip": inner})
	o := newOOXMLOpener(defaultOOXMLLimits())
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

func TestOOXMLOpener_RejectsCompressionRatioBomb(t *testing.T) {
	// Build a single member with extreme compression ratio. zlib compresses
	// long runs of zeros down to a tiny payload.
	big := bytes.Repeat([]byte{0}, 200_000) // 200 KB
	data := buildZip(t, map[string][]byte{"document.xml": big})
	limits := defaultOOXMLLimits()
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

func TestOOXMLOpener_TripsPartSize(t *testing.T) {
	// member exceeds part size cap; the limit may fire at openMember (via the
	// UncompressedSize64 header check) or during io.ReadAll (via boundedReader
	// for archives where the stored size is zero/wrong). Either path must
	// return an extractionLimitError with Kind=="part_size".
	data := buildZip(t, map[string][]byte{"big.xml": bytes.Repeat([]byte("a"), 5_000)})
	limits := defaultOOXMLLimits()
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

func TestOOXMLOpener_TripsCumulativeDecompressed(t *testing.T) {
	a := bytes.Repeat([]byte("a"), 800)
	b := bytes.Repeat([]byte("b"), 800)
	data := buildZip(t, map[string][]byte{"a.xml": a, "b.xml": b})
	limits := defaultOOXMLLimits()
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

func TestBoundedXMLDecoder_HappyPath(t *testing.T) {
	src := bytes.NewReader([]byte(`<root><a>x</a><b>y</b></root>`))
	d := newBoundedXMLDecoder(src, 10)
	for {
		_, err := d.Token()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
	}
}

func TestBoundedXMLDecoder_TripsDepth(t *testing.T) {
	// 6 nested elements with maxDepth=4 should trip on the 5th open.
	xml := `<a><b><c><d><e><f>x</f></e></d></c></b></a>`
	src := bytes.NewReader([]byte(xml))
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
		if err == io.EOF {
			break
		}
		t.Fatalf("unexpected error: %v", err)
	}
	assert.True(t, tripped, "depth limit must trip")
}
