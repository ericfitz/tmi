package api

import (
	"strings"
	"unicode"
)

// TextChunker splits text into chunks suitable for embedding
type TextChunker struct {
	maxChunkSize int // Target max characters per chunk
	overlap      int // Characters of overlap between chunks
}

// NewTextChunker creates a chunker with the given size and overlap (in characters)
func NewTextChunker(maxChunkSize, overlap int) *TextChunker {
	return &TextChunker{
		maxChunkSize: maxChunkSize,
		overlap:      overlap,
	}
}

// Chunk splits text into chunks at sentence boundaries
func (tc *TextChunker) Chunk(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	if len(text) <= tc.maxChunkSize {
		return []string{text}
	}

	sentences := splitSentences(text)
	var chunks []string
	var current strings.Builder
	var overlapSentences []string

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		// If adding this sentence would exceed the limit, finalize current chunk
		if current.Len() > 0 && current.Len()+1+len(sentence) > tc.maxChunkSize {
			chunks = append(chunks, strings.TrimSpace(current.String()))

			// Start new chunk with overlap from previous sentences
			current.Reset()
			if tc.overlap > 0 {
				for _, os := range overlapSentences {
					current.WriteString(os)
					current.WriteString(" ")
				}
			}
			overlapSentences = nil
		}

		if current.Len() > 0 {
			current.WriteString(" ")
		}
		current.WriteString(sentence)

		// Track recent sentences for overlap
		if tc.overlap > 0 {
			overlapSentences = append(overlapSentences, sentence)
			// Keep only enough sentences to fit in overlap budget
			totalLen := 0
			for i := len(overlapSentences) - 1; i >= 0; i-- {
				totalLen += len(overlapSentences[i]) + 1
				if totalLen > tc.overlap {
					overlapSentences = overlapSentences[i+1:]
					break
				}
			}
		}
	}

	if current.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(current.String()))
	}

	return chunks
}

// splitSentences splits text into sentences at period, question mark, or exclamation boundaries
func splitSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	runes := []rune(text)
	for i, r := range runes {
		current.WriteRune(r)

		// Check for sentence-ending punctuation followed by space or end of text
		if (r == '.' || r == '?' || r == '!') &&
			(i == len(runes)-1 || unicode.IsSpace(runes[i+1])) {
			sentences = append(sentences, current.String())
			current.Reset()
		}
	}

	// Remaining text (no final punctuation)
	if current.Len() > 0 {
		sentences = append(sentences, current.String())
	}

	return sentences
}
