package api

import (
	"errors"
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
