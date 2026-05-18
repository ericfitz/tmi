package main

import (
	"testing"

	"github.com/ericfitz/tmi/pkg/extract"
)

func TestChunkText(t *testing.T) {
	chunker := extract.NewTextChunker(50, 10)
	long := "Sentence one. Sentence two. Sentence three. Sentence four. Sentence five."
	chunks := chunker.Chunk(long)
	if len(chunks) < 2 {
		t.Fatalf("expected the long text to split into >=2 chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if c == "" {
			t.Fatalf("chunk %d is empty", i)
		}
	}
}

func TestEmbeddingResultShape(t *testing.T) {
	r := EmbeddingResult{
		Chunks:  []string{"a", "b"},
		Vectors: [][]float32{{0.1}, {0.2}},
	}
	if err := r.validate(); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
	bad := EmbeddingResult{Chunks: []string{"a", "b"}, Vectors: [][]float32{{0.1}}}
	if err := bad.validate(); err == nil {
		t.Fatal("expected error: chunk/vector count mismatch")
	}
}
