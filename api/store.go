package api

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DataStore provides a generic thread-safe in-memory store
type DataStore[T any] struct {
	data  map[string]T
	mutex sync.RWMutex
}

// NewDataStore creates a new store for a specific type
func NewDataStore[T any]() *DataStore[T] {
	return &DataStore[T]{
		data: make(map[string]T),
	}
}

// Get retrieves an item by ID
func (s *DataStore[T]) Get(id string) (T, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	item, exists := s.data[id]
	if !exists {
		var empty T
		return empty, fmt.Errorf("item with ID %s not found", id)
	}

	return item, nil
}

// List returns all items, optionally filtered and paginated
func (s *DataStore[T]) List(offset, limit int, filter func(T) bool) []T {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var results []T

	// Apply filtering if provided
	for _, item := range s.data {
		if filter == nil || filter(item) {
			results = append(results, item)
		}
	}

	// Apply pagination
	if offset >= len(results) {
		return []T{}
	}

	end := offset + limit
	if end > len(results) || limit <= 0 {
		end = len(results)
	}

	return results[offset:end]
}

// Create adds a new item with a generated UUID
func (s *DataStore[T]) Create(item T, idSetter func(T, string) T) (T, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	id := uuid.New().String()

	// Set the ID on the item
	if idSetter != nil {
		item = idSetter(item, id)
	}

	s.data[id] = item
	return item, nil
}

// Update replaces an existing item
func (s *DataStore[T]) Update(id string, item T) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.data[id]; !exists {
		return fmt.Errorf("item with ID %s not found", id)
	}

	s.data[id] = item
	return nil
}

// Delete removes an item by ID
func (s *DataStore[T]) Delete(id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if _, exists := s.data[id]; !exists {
		return fmt.Errorf("item with ID %s not found", id)
	}

	delete(s.data, id)
	return nil
}

// Count returns the total number of items
func (s *DataStore[T]) Count() int {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return len(s.data)
}

// WithTimestamps is a mixin interface for entities with timestamps
type WithTimestamps interface {
	SetCreatedAt(time.Time)
	SetModifiedAt(time.Time)
}

// UpdateTimestamps updates the timestamps on an entity
func UpdateTimestamps[T WithTimestamps](entity T, isNew bool) T {
	now := time.Now().UTC()
	if isNew {
		entity.SetCreatedAt(now)
	}
	entity.SetModifiedAt(now)
	return entity
}

// DiagramStore stores diagrams
var DiagramStore = NewDataStore[DfdDiagram]()

// ThreatModelStore stores threat models
var ThreatModelStore = NewDataStore[ThreatModel]()
