package api

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestChunker_ShortTextSingleChunk(t *testing.T) {
	c := NewTextChunker(512, 50)
	chunks := c.Chunk("This is a short text.")
	assert.Len(t, chunks, 1)
	assert.Equal(t, "This is a short text.", chunks[0])
}

func TestChunker_LongTextMultipleChunks(t *testing.T) {
	c := NewTextChunker(100, 20)
	// Generate text with multiple sentences
	sentences := make([]string, 20)
	for i := range sentences {
		sentences[i] = "This is sentence number " + strings.Repeat("x", 5) + "."
	}
	text := strings.Join(sentences, " ")
	chunks := c.Chunk(text)
	assert.Greater(t, len(chunks), 1, "long text should be split into multiple chunks")

	// Verify all content is represented
	combined := strings.Join(chunks, " ")
	for _, s := range sentences {
		assert.Contains(t, combined, s[:10], "chunk content should contain all sentences")
	}
}

func TestChunker_EmptyText(t *testing.T) {
	c := NewTextChunker(512, 50)
	chunks := c.Chunk("")
	assert.Len(t, chunks, 0)
}

func TestChunker_SentenceBoundaryRespected(t *testing.T) {
	c := NewTextChunker(50, 0)
	text := "First sentence here. Second sentence here. Third sentence here."
	chunks := c.Chunk(text)
	// Each chunk should end at a sentence boundary (with a period)
	for _, chunk := range chunks {
		trimmed := strings.TrimSpace(chunk)
		if len(trimmed) > 0 {
			assert.True(t, strings.HasSuffix(trimmed, "."), "chunk should end at sentence boundary: %q", trimmed)
		}
	}
}
