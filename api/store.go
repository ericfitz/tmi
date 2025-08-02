package api

import (
	"database/sql"
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

// Store interfaces to allow switching between in-memory and database implementations
// ThreatModelWithCounts extends ThreatModel with count information
type ThreatModelWithCounts struct {
	ThreatModel
	DocumentCount int
	SourceCount   int
	DiagramCount  int
	ThreatCount   int
}

type ThreatModelStoreInterface interface {
	Get(id string) (ThreatModel, error)
	List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel
	ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts
	Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error)
	Update(id string, item ThreatModel) error
	Delete(id string) error
	Count() int
	UpdateCounts(threatModelID string) error
	CountSubEntitiesFromPayload(tm ThreatModel) (documentCount, sourceCount, diagramCount, threatCount int)
	UpdateCountsWithValues(threatModelID string, documentCount, sourceCount, diagramCount, threatCount int) error
}

type DiagramStoreInterface interface {
	Get(id string) (DfdDiagram, error)
	List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram
	Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	Update(id string, item DfdDiagram) error
	Delete(id string) error
	Count() int
}

// Global store instances (will be initialized in main.go)
var ThreatModelStore ThreatModelStoreInterface
var DiagramStore DiagramStoreInterface

// ThreatModelInMemoryStore wraps DataStore to implement count management methods
type ThreatModelInMemoryStore struct {
	*DataStore[ThreatModel]
}

// NewThreatModelInMemoryStore creates a new in-memory threat model store
func NewThreatModelInMemoryStore() *ThreatModelInMemoryStore {
	return &ThreatModelInMemoryStore{
		DataStore: NewDataStore[ThreatModel](),
	}
}

// UpdateCounts is a no-op for in-memory store (counts are calculated on-the-fly)
func (s *ThreatModelInMemoryStore) UpdateCounts(threatModelID string) error {
	// In-memory store doesn't need to update counts as they're calculated dynamically
	return nil
}

// UpdateCountsWithValues is a no-op for in-memory store
func (s *ThreatModelInMemoryStore) UpdateCountsWithValues(threatModelID string, documentCount, sourceCount, diagramCount, threatCount int) error {
	// In-memory store doesn't need to update counts as they're calculated dynamically
	return nil
}

// ListWithCounts returns threat models with dynamically calculated counts
func (s *ThreatModelInMemoryStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts {
	models := s.List(offset, limit, filter)
	result := make([]ThreatModelWithCounts, 0, len(models))

	for _, tm := range models {
		docCount, srcCount, diagCount, threatCount := s.CountSubEntitiesFromPayload(tm)
		result = append(result, ThreatModelWithCounts{
			ThreatModel:   tm,
			DocumentCount: docCount,
			SourceCount:   srcCount,
			DiagramCount:  diagCount,
			ThreatCount:   threatCount,
		})
	}

	return result
}

// CountSubEntitiesFromPayload counts entities from a threat model payload
func (s *ThreatModelInMemoryStore) CountSubEntitiesFromPayload(tm ThreatModel) (documentCount, sourceCount, diagramCount, threatCount int) {
	if tm.Documents != nil {
		documentCount = len(*tm.Documents)
	}
	if tm.SourceCode != nil {
		sourceCount = len(*tm.SourceCode)
	}
	if tm.Diagrams != nil {
		diagramCount = len(*tm.Diagrams)
	}
	if tm.Threats != nil {
		threatCount = len(*tm.Threats)
	}
	return
}

// InitializeInMemoryStores initializes stores with in-memory implementations
func InitializeInMemoryStores() {
	ThreatModelStore = NewThreatModelInMemoryStore()
	DiagramStore = NewDataStore[DfdDiagram]()
}

// InitializeDatabaseStores initializes stores with database implementations
func InitializeDatabaseStores(db *sql.DB) {
	ThreatModelStore = NewThreatModelDatabaseStore(db)
	DiagramStore = NewDiagramDatabaseStore(db)
}
