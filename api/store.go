package api

import (
	"context"
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
	CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error)
	Update(id string, item DfdDiagram) error
	Delete(id string) error
	Count() int
}

// Global store instances (will be initialized in main.go)
var ThreatModelStore ThreatModelStoreInterface
var DiagramStore DiagramStoreInterface
var GlobalDocumentStore DocumentStore
var GlobalSourceStore SourceStore
var GlobalThreatStore ThreatStore

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

// DiagramInMemoryStore wraps DataStore to implement DiagramStoreInterface
type DiagramInMemoryStore struct {
	*DataStore[DfdDiagram]
}

// NewDiagramInMemoryStore creates a new in-memory diagram store
func NewDiagramInMemoryStore() *DiagramInMemoryStore {
	return &DiagramInMemoryStore{
		DataStore: NewDataStore[DfdDiagram](),
	}
}

// CreateWithThreatModel creates a diagram with a threat model ID (for in-memory, this is the same as Create)
func (s *DiagramInMemoryStore) CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	// For in-memory store, we don't enforce threat model foreign key constraints
	return s.Create(item, idSetter)
}

// InitializeInMemoryStores initializes stores with in-memory implementations
func InitializeInMemoryStores() {
	ThreatModelStore = NewThreatModelInMemoryStore()
	DiagramStore = NewDiagramInMemoryStore()
	GlobalDocumentStore = NewInMemoryDocumentStore()
	GlobalSourceStore = NewInMemorySourceStore()
	GlobalThreatStore = NewInMemoryThreatStore()
}

// InitializeDatabaseStores initializes stores with database implementations
func InitializeDatabaseStores(db *sql.DB) {
	ThreatModelStore = NewThreatModelDatabaseStore(db)
	DiagramStore = NewDiagramDatabaseStore(db)
	GlobalDocumentStore = NewDatabaseDocumentStore(db, nil, nil)
	GlobalSourceStore = NewDatabaseSourceStore(db, nil, nil)
	GlobalThreatStore = NewDatabaseThreatStore(db, nil, nil)
}

// InMemoryDocumentStore provides a simple in-memory implementation for testing
type InMemoryDocumentStore struct {
	*DataStore[Document]
}

// NewInMemoryDocumentStore creates a simple in-memory document store for testing
func NewInMemoryDocumentStore() *InMemoryDocumentStore {
	return &InMemoryDocumentStore{
		DataStore: NewDataStore[Document](),
	}
}

// List implements DocumentStore interface for in-memory testing
func (s *InMemoryDocumentStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Document, error) {
	filter := func(doc Document) bool {
		// Simple filtering - in real implementation this would check threat_model_id relationship
		return true
	}
	return s.DataStore.List(offset, limit, filter), nil
}

// ParseUUIDOrNil parses a UUID string, returning a nil UUID on error
func ParseUUIDOrNil(s string) uuid.UUID {
	if u, err := uuid.Parse(s); err == nil {
		return u
	}
	return uuid.Nil
}

// Create implements DocumentStore interface for in-memory testing
func (s *InMemoryDocumentStore) Create(ctx context.Context, document *Document, threatModelID string) error {
	_, err := s.DataStore.Create(*document, func(doc Document, id string) Document {
		if doc.Id == nil {
			uuid := ParseUUIDOrNil(id)
			doc.Id = &uuid
		}
		return doc
	})
	return err
}

// Implement other required methods with simple implementations
func (s *InMemoryDocumentStore) Get(ctx context.Context, id string) (*Document, error) {
	doc, err := s.DataStore.Get(id)
	return &doc, err
}

func (s *InMemoryDocumentStore) Update(ctx context.Context, document *Document, threatModelID string) error {
	return s.DataStore.Update(document.Id.String(), *document)
}

func (s *InMemoryDocumentStore) Delete(ctx context.Context, id string) error {
	return s.DataStore.Delete(id)
}

func (s *InMemoryDocumentStore) BulkCreate(ctx context.Context, documents []Document, threatModelID string) error {
	for _, doc := range documents {
		if err := s.Create(ctx, &doc, threatModelID); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemoryDocumentStore) InvalidateCache(ctx context.Context, id string) error {
	return nil // No-op for in-memory
}

func (s *InMemoryDocumentStore) WarmCache(ctx context.Context, threatModelID string) error {
	return nil // No-op for in-memory
}

// InMemorySourceStore provides a simple in-memory implementation for testing
type InMemorySourceStore struct {
	*DataStore[Source]
}

// NewInMemorySourceStore creates a simple in-memory source store for testing
func NewInMemorySourceStore() *InMemorySourceStore {
	return &InMemorySourceStore{
		DataStore: NewDataStore[Source](),
	}
}

// List implements SourceStore interface for in-memory testing
func (s *InMemorySourceStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Source, error) {
	filter := func(src Source) bool {
		// Simple filtering - in real implementation this would check threat_model_id relationship
		return true
	}
	return s.DataStore.List(offset, limit, filter), nil
}

// Create implements SourceStore interface for in-memory testing
func (s *InMemorySourceStore) Create(ctx context.Context, source *Source, threatModelID string) error {
	_, err := s.DataStore.Create(*source, func(src Source, id string) Source {
		if src.Id == nil {
			uuid := ParseUUIDOrNil(id)
			src.Id = &uuid
		}
		return src
	})
	return err
}

// Implement other required methods with simple implementations
func (s *InMemorySourceStore) Get(ctx context.Context, id string) (*Source, error) {
	src, err := s.DataStore.Get(id)
	return &src, err
}

func (s *InMemorySourceStore) Update(ctx context.Context, source *Source, threatModelID string) error {
	return s.DataStore.Update(source.Id.String(), *source)
}

func (s *InMemorySourceStore) Delete(ctx context.Context, id string) error {
	return s.DataStore.Delete(id)
}

func (s *InMemorySourceStore) BulkCreate(ctx context.Context, sources []Source, threatModelID string) error {
	for _, src := range sources {
		if err := s.Create(ctx, &src, threatModelID); err != nil {
			return err
		}
	}
	return nil
}

func (s *InMemorySourceStore) InvalidateCache(ctx context.Context, id string) error {
	return nil // No-op for in-memory
}

func (s *InMemorySourceStore) WarmCache(ctx context.Context, threatModelID string) error {
	return nil // No-op for in-memory
}

// InMemoryThreatStore provides a simple in-memory implementation for testing
type InMemoryThreatStore struct {
	*DataStore[Threat]
}

// NewInMemoryThreatStore creates a simple in-memory threat store for testing
func NewInMemoryThreatStore() *InMemoryThreatStore {
	return &InMemoryThreatStore{
		DataStore: NewDataStore[Threat](),
	}
}

// List implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) List(ctx context.Context, threatModelID string, offset, limit int) ([]Threat, error) {
	filter := func(threat Threat) bool {
		// Simple filtering - in real implementation this would check threat_model_id relationship
		return true
	}
	return s.DataStore.List(offset, limit, filter), nil
}

// Create implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) Create(ctx context.Context, threat *Threat) error {
	_, err := s.DataStore.Create(*threat, func(t Threat, id string) Threat {
		if t.Id == nil {
			uuid := ParseUUIDOrNil(id)
			t.Id = &uuid
		}
		return t
	})
	return err
}

// Get implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) Get(ctx context.Context, id string) (*Threat, error) {
	threat, err := s.DataStore.Get(id)
	return &threat, err
}

// Update implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) Update(ctx context.Context, threat *Threat) error {
	return s.DataStore.Update(threat.Id.String(), *threat)
}

// Delete implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) Delete(ctx context.Context, id string) error {
	return s.DataStore.Delete(id)
}

// Patch implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) Patch(ctx context.Context, id string, operations []PatchOperation) (*Threat, error) {
	// Simple implementation - just return the current threat
	threat, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	// In a real implementation, apply the patch operations here
	return threat, nil
}

// BulkCreate implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) BulkCreate(ctx context.Context, threats []Threat) error {
	for _, threat := range threats {
		if err := s.Create(ctx, &threat); err != nil {
			return err
		}
	}
	return nil
}

// BulkUpdate implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) BulkUpdate(ctx context.Context, threats []Threat) error {
	for _, threat := range threats {
		if err := s.Update(ctx, &threat); err != nil {
			return err
		}
	}
	return nil
}

// InvalidateCache implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) InvalidateCache(ctx context.Context, id string) error {
	return nil // No-op for in-memory
}

// WarmCache implements ThreatStore interface for in-memory testing
func (s *InMemoryThreatStore) WarmCache(ctx context.Context, threatModelID string) error {
	return nil // No-op for in-memory
}
