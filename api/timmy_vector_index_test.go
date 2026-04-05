package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVectorIndex_AddAndSearch(t *testing.T) {
	idx := NewVectorIndex(3) // 3-dimensional vectors for testing

	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "authentication")
	idx.Add("chunk-2", []float32{0.0, 1.0, 0.0}, "database")
	idx.Add("chunk-3", []float32{0.9, 0.1, 0.0}, "auth tokens")

	results := idx.Search([]float32{1.0, 0.0, 0.0}, 2)
	require.Len(t, results, 2)
	assert.Equal(t, "chunk-1", results[0].ID, "closest match should be exact vector")
	assert.Equal(t, "chunk-3", results[1].ID, "second closest should be similar vector")
}

func TestVectorIndex_SearchEmpty(t *testing.T) {
	idx := NewVectorIndex(3)
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 5)
	assert.Len(t, results, 0)
}

func TestVectorIndex_SearchTopKExceedsCount(t *testing.T) {
	idx := NewVectorIndex(3)
	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "text")
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 10)
	assert.Len(t, results, 1, "should return at most the number of stored vectors")
}

func TestVectorIndex_Delete(t *testing.T) {
	idx := NewVectorIndex(3)
	idx.Add("chunk-1", []float32{1.0, 0.0, 0.0}, "text")
	idx.Add("chunk-2", []float32{0.0, 1.0, 0.0}, "text")
	idx.Delete("chunk-1")
	results := idx.Search([]float32{1.0, 0.0, 0.0}, 5)
	assert.Len(t, results, 1)
	assert.Equal(t, "chunk-2", results[0].ID)
}

func TestVectorIndex_MemorySize(t *testing.T) {
	idx := NewVectorIndex(768)
	idx.Add("chunk-1", make([]float32, 768), "text")
	idx.Add("chunk-2", make([]float32, 768), "text")
	// 2 vectors * 768 dims * 4 bytes = 6144 bytes minimum
	assert.GreaterOrEqual(t, idx.MemorySize(), int64(6144))
}
