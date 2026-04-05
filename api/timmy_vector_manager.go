package api

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// LoadedIndex represents an in-memory vector index for a threat model
type LoadedIndex struct {
	ThreatModelID  string
	Index          *VectorIndex
	LastAccessed   time.Time
	ActiveSessions int
	MemoryBytes    int64
}

// VectorIndexManager manages in-memory vector indexes per threat model
type VectorIndexManager struct {
	mu                sync.Mutex
	indexes           map[string]*LoadedIndex
	embeddingStore    TimmyEmbeddingStore
	maxMemoryBytes    int64
	inactivityTimeout time.Duration

	// Metrics
	totalEvictions    int64
	pressureEvictions int64
	rejectedSessions  int64
}

// NewVectorIndexManager creates a new manager with the given memory budget
func NewVectorIndexManager(embeddingStore TimmyEmbeddingStore, maxMemoryMB int, inactivityTimeoutSeconds int) *VectorIndexManager {
	mgr := &VectorIndexManager{
		indexes:           make(map[string]*LoadedIndex),
		embeddingStore:    embeddingStore,
		maxMemoryBytes:    int64(maxMemoryMB) * 1024 * 1024,
		inactivityTimeout: time.Duration(inactivityTimeoutSeconds) * time.Second,
	}
	go mgr.evictionLoop()
	return mgr
}

// GetOrLoadIndex returns the index for a threat model, loading from DB if needed
func (m *VectorIndexManager) GetOrLoadIndex(ctx context.Context, threatModelID string, dimension int) (*VectorIndex, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if loaded, ok := m.indexes[threatModelID]; ok {
		loaded.LastAccessed = time.Now()
		loaded.ActiveSessions++
		return loaded.Index, nil
	}

	if !m.canAllocate() {
		m.evictLRU()
		if !m.canAllocate() {
			m.rejectedSessions++
			return nil, fmt.Errorf("insufficient memory to load vector index")
		}
	}

	embeddings, err := m.embeddingStore.ListByThreatModel(ctx, threatModelID)
	if err != nil {
		return nil, fmt.Errorf("failed to load embeddings: %w", err)
	}

	idx := NewVectorIndex(dimension)
	for _, emb := range embeddings {
		vector := bytesToFloat32(emb.VectorData)
		idx.Add(emb.ID, vector, string(emb.ChunkText))
	}

	loaded := &LoadedIndex{
		ThreatModelID:  threatModelID,
		Index:          idx,
		LastAccessed:   time.Now(),
		ActiveSessions: 1,
		MemoryBytes:    idx.MemorySize(),
	}
	m.indexes[threatModelID] = loaded

	slogging.Get().Debug("Loaded vector index for threat model %s: %d vectors, %d bytes",
		threatModelID, idx.Count(), loaded.MemoryBytes)
	return idx, nil
}

// ReleaseIndex decrements the active session count
func (m *VectorIndexManager) ReleaseIndex(threatModelID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if loaded, ok := m.indexes[threatModelID]; ok {
		loaded.ActiveSessions--
		if loaded.ActiveSessions < 0 {
			loaded.ActiveSessions = 0
		}
	}
}

// GetStatus returns current memory and index status for the admin endpoint
func (m *VectorIndexManager) GetStatus() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()

	var totalMemory int64
	var largestIndex int64
	indexDetails := make([]map[string]any, 0, len(m.indexes))

	for _, loaded := range m.indexes {
		totalMemory += loaded.MemoryBytes
		if loaded.MemoryBytes > largestIndex {
			largestIndex = loaded.MemoryBytes
		}
		indexDetails = append(indexDetails, map[string]any{
			"threat_model_id": loaded.ThreatModelID,
			"vectors":         loaded.Index.Count(),
			"memory_bytes":    loaded.MemoryBytes,
			"active_sessions": loaded.ActiveSessions,
			"last_accessed":   loaded.LastAccessed,
		})
	}

	avgSize := int64(0)
	if len(m.indexes) > 0 {
		avgSize = totalMemory / int64(len(m.indexes))
	}

	utilPct := float64(0)
	if m.maxMemoryBytes > 0 {
		utilPct = float64(totalMemory) / float64(m.maxMemoryBytes) * 100
	}

	return map[string]any{
		"memory_used_bytes":      totalMemory,
		"memory_budget_bytes":    m.maxMemoryBytes,
		"memory_utilization_pct": utilPct,
		"indexes_loaded":         len(m.indexes),
		"avg_index_size_bytes":   avgSize,
		"largest_index_bytes":    largestIndex,
		"evictions_total":        m.totalEvictions,
		"evictions_pressure":     m.pressureEvictions,
		"sessions_rejected":      m.rejectedSessions,
		"indexes":                indexDetails,
	}
}

func (m *VectorIndexManager) canAllocate() bool {
	var total int64
	for _, loaded := range m.indexes {
		total += loaded.MemoryBytes
	}
	return total < int64(float64(m.maxMemoryBytes)*0.9)
}

func (m *VectorIndexManager) evictLRU() {
	var oldest *LoadedIndex
	var oldestID string
	for id, loaded := range m.indexes {
		if loaded.ActiveSessions > 0 {
			continue
		}
		if oldest == nil || loaded.LastAccessed.Before(oldest.LastAccessed) {
			oldest = loaded
			oldestID = id
		}
	}
	if oldest != nil {
		delete(m.indexes, oldestID)
		m.totalEvictions++
		m.pressureEvictions++
		slogging.Get().Debug("Pressure-evicted vector index for threat model %s", oldestID)
	}
}

func (m *VectorIndexManager) evictionLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for id, loaded := range m.indexes {
			if loaded.ActiveSessions == 0 && now.Sub(loaded.LastAccessed) > m.inactivityTimeout {
				delete(m.indexes, id)
				m.totalEvictions++
				slogging.Get().Debug("Inactivity-evicted vector index for threat model %s", id)
			}
		}
		m.mu.Unlock()
	}
}

// bytesToFloat32 converts a byte slice to a float32 slice (little-endian)
func bytesToFloat32(data []byte) []float32 {
	if len(data) == 0 {
		return nil
	}
	n := len(data) / 4
	result := make([]float32, n)
	for i := 0; i < n; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		result[i] = math.Float32frombits(bits)
	}
	return result
}

// float32ToBytes converts a float32 slice to a byte slice (little-endian)
func float32ToBytes(data []float32) []byte {
	result := make([]byte, len(data)*4)
	for i, v := range data {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(result[i*4:(i+1)*4], bits)
	}
	return result
}
