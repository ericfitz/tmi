package api

import (
	"math"
	"sort"
	"sync"
)

// VectorSearchResult represents a search result with similarity score
type VectorSearchResult struct {
	ID         string
	ChunkText  string
	Similarity float32
}

// vectorEntry stores a single vector in the index
type vectorEntry struct {
	id        string
	vector    []float32
	chunkText string
}

// VectorIndex is an in-memory vector index using brute-force cosine similarity.
// This is adequate for threat-model scale (hundreds of vectors).
type VectorIndex struct {
	mu        sync.RWMutex
	entries   []vectorEntry
	dimension int
}

// NewVectorIndex creates a new vector index for vectors of the given dimension
func NewVectorIndex(dimension int) *VectorIndex {
	return &VectorIndex{
		dimension: dimension,
	}
}

// Add inserts a vector into the index
func (vi *VectorIndex) Add(id string, vector []float32, chunkText string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	vi.entries = append(vi.entries, vectorEntry{
		id:        id,
		vector:    vector,
		chunkText: chunkText,
	})
}

// Delete removes a vector by ID
func (vi *VectorIndex) Delete(id string) {
	vi.mu.Lock()
	defer vi.mu.Unlock()
	for i, e := range vi.entries {
		if e.id == id {
			vi.entries = append(vi.entries[:i], vi.entries[i+1:]...)
			return
		}
	}
}

// Search returns the top-K most similar vectors to the query
func (vi *VectorIndex) Search(query []float32, topK int) []VectorSearchResult {
	vi.mu.RLock()
	defer vi.mu.RUnlock()

	if len(vi.entries) == 0 {
		return nil
	}

	type scored struct {
		entry      vectorEntry
		similarity float32
	}
	results := make([]scored, 0, len(vi.entries))
	for _, e := range vi.entries {
		sim := cosineSimilarity(query, e.vector)
		results = append(results, scored{entry: e, similarity: sim})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].similarity > results[j].similarity
	})

	k := topK
	if k > len(results) {
		k = len(results)
	}

	out := make([]VectorSearchResult, k)
	for i := 0; i < k; i++ {
		out[i] = VectorSearchResult{
			ID:         results[i].entry.id,
			ChunkText:  results[i].entry.chunkText,
			Similarity: results[i].similarity,
		}
	}
	return out
}

// Count returns the number of vectors in the index
func (vi *VectorIndex) Count() int {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	return len(vi.entries)
}

// MemorySize estimates the memory used by this index in bytes
func (vi *VectorIndex) MemorySize() int64 {
	vi.mu.RLock()
	defer vi.mu.RUnlock()
	var size int64
	for _, e := range vi.entries {
		size += int64(len(e.vector)) * 4 // float32 vector
		size += int64(len(e.chunkText))  // chunk text
		size += int64(len(e.id))         // ID string
		size += 64                       // struct overhead estimate
	}
	return size
}

// cosineSimilarity computes cosine similarity between two vectors
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) {
		return 0
	}
	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dotProduct / (math.Sqrt(normA) * math.Sqrt(normB)))
}
