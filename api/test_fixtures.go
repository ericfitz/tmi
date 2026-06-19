package api

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// Test fixture email constants
const (
	testEmailDefault = "test@example.com"
)

// Fixtures provides test data for unit tests
// CustomDiagram extends Diagram with authorization fields for testing
// SEM@71c76e4f3ee8185c2e04a7476bacd2537c75d2e4: test-only diagram type extending DfdDiagram with owner and authorization fields (pure)
type CustomDiagram struct {
	DfdDiagram
	Owner         string
	Authorization []Authorization
}

var TestFixtures struct {
	// Test users for authorization
	OwnerUser  string
	WriterUser string
	ReaderUser string

	// Owner field values
	Owner string

	// Test threat models
	ThreatModel   ThreatModel
	ThreatModelID string

	// Test diagrams
	Diagram     DfdDiagram
	DiagramID   string
	DiagramAuth []Authorization // Store authorization separately since it's not in the Diagram struct

	// Test flags
	Initialized bool
}

// InitTestFixtures initializes test data in stores
// SEM@d48970168f241f7cb359d0cfdb00f3e26abb59da: initialize in-memory stores with canonical test threat model and diagram fixtures (mutates shared state)
func InitTestFixtures() {
	// Database stores are initialized by the main application

	// Set up test users for authorization entries
	TestFixtures.OwnerUser = testEmailDefault
	TestFixtures.WriterUser = testWriterEmail
	TestFixtures.ReaderUser = testReaderEmail

	// Set up owner field value
	TestFixtures.Owner = testEmailDefault

	// Create timestamps
	now := time.Now().UTC()

	// Create a test threat model
	metadata := []Metadata{
		{Key: "priority", Value: "high"},
		{Key: "status", Value: "active"},
	}

	threats := []Threat{
		{
			Id:            new(NewUUID()),
			Name:          "SQL Injection",
			Description:   new("Database attack via malicious SQL"),
			CreatedAt:     &now,
			ModifiedAt:    &now,
			ThreatModelId: new(NewUUID()),
			Severity:      new("High"),
			Priority:      new("High"),
			Status:        new("Open"),
			ThreatType:    []string{"Injection"},
			Mitigated:     new(false),
			Metadata:      &metadata,
		},
	}

	// diagrams := []TypesUUID{NewUUID()} // Not used currently

	// Create threat model with new UUID
	uuid1 := NewUUID()
	ownerUser := User{
		PrincipalType: UserPrincipalTypeUser,
		Provider:      "test",
		ProviderId:    TestFixtures.Owner,
		DisplayName:   TestFixtures.Owner,
		Email:         openapi_types.Email(TestFixtures.Owner),
	}
	threatModel := ThreatModel{
		Id:          new(uuid1),
		Name:        "Test Threat Model",
		Description: new("This is a test threat model"),
		CreatedAt:   &now,
		ModifiedAt:  &now,
		Owner:       ownerUser,
		CreatedBy:   &ownerUser,
		Authorization: &[]Authorization{
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    TestFixtures.OwnerUser,
				Role:          RoleOwner,
			},
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    TestFixtures.WriterUser,
				Role:          RoleWriter,
			},
			{
				PrincipalType: AuthorizationPrincipalTypeUser,
				Provider:      "test",
				ProviderId:    TestFixtures.ReaderUser,
				Role:          RoleReader,
			},
		},
		Metadata: &metadata,
		Threats:  &threats,
		// Diagrams will be set after creating the diagram
	}

	// Create a test diagram with cells using new union types
	cells := []DfdDiagram_Cells_Item{}

	// Create test nodes using helper functions
	if node1, err := CreateNode(NewUUID().String(), NodeShapeProcess, 100, 200, 80, 40); err == nil {
		cells = append(cells, node1)
	}

	if node2, err := CreateNode(NewUUID().String(), NodeShapeStore, 300, 200, 80, 40); err == nil {
		cells = append(cells, node2)
	}

	// Create a test edge connecting the nodes
	if len(cells) >= 2 {
		// Extract IDs from the nodes to create an edge
		if node1Data, err := cells[0].AsNode(); err == nil {
			if node2Data, err := cells[1].AsNode(); err == nil {
				if edge, err := CreateEdge(NewUUID().String(), EdgeShapeFlow, node1Data.Id.String(), node2Data.Id.String()); err == nil {
					cells = append(cells, edge)
				}
			}
		}
	}

	// Create diagram with new UUID
	uuid2 := NewUUID()
	diagram := DfdDiagram{
		Id:         new(uuid2),
		Name:       "Test Diagram",
		CreatedAt:  &now,
		ModifiedAt: &now,
		Cells:      cells,
		Metadata:   &metadata,
		Type:       DfdDiagramTypeDFD100,
	}

	// Store authorization data separately for tests
	diagramAuth := []Authorization{
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "test",
			ProviderId:    TestFixtures.OwnerUser,
			Role:          RoleOwner,
		},
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "test",
			ProviderId:    TestFixtures.WriterUser,
			Role:          RoleWriter,
		},
		{
			PrincipalType: AuthorizationPrincipalTypeUser,
			Provider:      "test",
			ProviderId:    TestFixtures.ReaderUser,
			Role:          RoleReader,
		},
	}

	// Store the fixtures with their UUIDs
	tmID := uuid1.String()
	dID := uuid2.String()

	TestFixtures.ThreatModel = threatModel
	TestFixtures.ThreatModelID = tmID

	TestFixtures.Diagram = diagram
	TestFixtures.DiagramID = dID
	TestFixtures.DiagramAuth = diagramAuth

	// Associate the diagram with the threat model by adding it to the Diagrams array
	var diagramUnion Diagram
	if err := diagramUnion.FromDfdDiagram(diagram); err == nil {
		diagrams := []Diagram{diagramUnion}
		threatModel.Diagrams = &diagrams
		TestFixtures.ThreatModel = threatModel
	}

	// Initialize mock stores for unit tests, always resetting to ensure clean state.
	// This prevents test contamination when other tests replace global stores.
	InitializeMockStores()

	// Always populate the stores with test data
	// Use the updated threat model that has the diagram association
	updatedThreatModel := TestFixtures.ThreatModel
	_, _ = ThreatModelStore.Create(updatedThreatModel, func(tm ThreatModel, _ string) ThreatModel {
		parsedId, _ := ParseUUID(tmID)
		tm.Id = &parsedId
		return tm
	})

	// Store the diagram
	_, _ = DiagramStore.Create(diagram, func(d DfdDiagram, _ string) DfdDiagram {
		parsedId, _ := ParseUUID(dID)
		d.Id = &parsedId
		return d
	})

	TestFixtures.Initialized = true
}

// Simple mock stores for unit tests
// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: in-memory threat model store implementation for unit tests (pure)
type MockThreatModelStore struct {
	data map[string]ThreatModel
}

// SEM@e4005658033b63171bdc1130fb523d996fbff9a7: fetch a threat model by ID from the in-memory store, loading diagrams dynamically (pure)
func (m *MockThreatModelStore) Get(id string) (ThreatModel, error) {
	if item, exists := m.data[id]; exists {
		// Filter out soft-deleted entities
		if item.DeletedAt != nil {
			return ThreatModel{}, fmt.Errorf("threat model not found")
		}
		// Dynamically load diagrams from DiagramStore
		var diagrams []Diagram
		if mockDiagStore, ok := DiagramStore.(*MockDiagramStore); ok {
			for diagramID, threatModelID := range mockDiagStore.threatModelMapping {
				if threatModelID == id {
					if diagram, err := DiagramStore.Get(diagramID); err == nil {
						// Convert DfdDiagram to Diagram union type
						var diagUnion Diagram
						if err := diagUnion.FromDfdDiagram(diagram); err == nil {
							diagrams = append(diagrams, diagUnion)
						}
					}
				}
			}
		}
		// Update the threat model with loaded diagrams
		if len(diagrams) > 0 {
			item.Diagrams = &diagrams
		}
		return item, nil
	}
	return ThreatModel{}, fmt.Errorf("threat model not found")
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: list threat models from in-memory store with optional predicate filter (pure)
func (m *MockThreatModelStore) List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel {
	var result []ThreatModel
	for _, item := range m.data {
		if filter == nil || filter(item) {
			result = append(result, item)
		}
	}
	return result
}

// SEM@ce7f0b599ec1a118c3d01ee48ad1e397e9f0c19d: list threat models as list items with query-param filters, pagination, and total count (pure)
func (m *MockThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]TMListItem, int) {
	var result []TMListItem
	for _, item := range m.data {
		// Apply authorization filter
		if filter != nil && !filter(item) {
			continue
		}

		// Apply query parameter filters if provided
		if filters != nil && !matchesThreatModelFilters(item, filters) {
			continue
		}

		var createdBy User
		if item.CreatedBy != nil {
			createdBy = *item.CreatedBy
		}
		var createdAt time.Time
		if item.CreatedAt != nil {
			createdAt = *item.CreatedAt
		}
		var modifiedAt time.Time
		if item.ModifiedAt != nil {
			modifiedAt = *item.ModifiedAt
		}

		result = append(result, TMListItem{
			Id:                   item.Id,
			Name:                 item.Name,
			Description:          item.Description,
			Owner:                item.Owner,
			CreatedBy:            createdBy,
			SecurityReviewer:     item.SecurityReviewer,
			ThreatModelFramework: item.ThreatModelFramework,
			IssueUri:             item.IssueUri,
			Status:               item.Status,
			StatusUpdated:        item.StatusUpdated,
			CreatedAt:            createdAt,
			ModifiedAt:           modifiedAt,
			DeletedAt:            item.DeletedAt,
		})
	}

	// Store total count before pagination
	total := len(result)

	// Apply pagination
	if offset >= total {
		return []TMListItem{}, total
	}

	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}

	return result[offset:end], total
}

// matchesThreatModelFilters checks if a threat model matches the provided filters
// SEM@d03a452bc6d7e6be064088c1273b70c59a65a3ff: filter a threat model against all query parameter filters (pure)
func matchesThreatModelFilters(item ThreatModel, filters *ThreatModelFilters) bool {
	if !matchesStringFilter(item.Name, filters.Name) {
		return false
	}
	if !matchesStringPtrFilter(item.Description, filters.Description) {
		return false
	}
	if !matchesStringPtrFilter(item.IssueUri, filters.IssueUri) {
		return false
	}
	if !matchesOwnerFilter(item.Owner, filters.Owner) {
		return false
	}
	if !matchesDateAfterFilter(item.CreatedAt, filters.CreatedAfter) {
		return false
	}
	if !matchesDateBeforeFilter(item.CreatedAt, filters.CreatedBefore) {
		return false
	}
	if !matchesDateAfterFilter(item.ModifiedAt, filters.ModifiedAfter) {
		return false
	}
	if !matchesDateBeforeFilter(item.ModifiedAt, filters.ModifiedBefore) {
		return false
	}
	if !matchesStatusFilter(item.Status, filters.Status) {
		return false
	}
	if !matchesDateAfterFilter(item.StatusUpdated, filters.StatusUpdatedAfter) {
		return false
	}
	if !matchesDateBeforeFilter(item.StatusUpdated, filters.StatusUpdatedBefore) {
		return false
	}
	if !matchesSecurityReviewerFilter(item.SecurityReviewer, filters.SecurityReviewer) {
		return false
	}
	return true
}

// matchesSecurityReviewerFilter checks if a security reviewer matches the filter.
// Supports is:null (reviewer must be nil) and is:notnull (reviewer must be non-nil).
// For plain value filters, performs a case-insensitive partial match against the reviewer's identifiers.
// SEM@8a0f012a79adaf71c250f15a0a0a7e5857e7ca3e: filter a threat model by security reviewer field with null, not-null, and partial-match operators (pure)
func matchesSecurityReviewerFilter(reviewer *User, filter *ParsedFilter) bool {
	if filter == nil {
		return true
	}
	switch filter.Operator {
	case FilterOpIsNull:
		return reviewer == nil
	case FilterOpIsNotNull:
		return reviewer != nil
	default:
		// Plain value: partial match against reviewer email/name (matches GORM query semantics)
		if reviewer == nil {
			return false
		}
		v := filter.Value
		return containsIgnoreCase(string(reviewer.Email), v) ||
			containsIgnoreCase(reviewer.DisplayName, v)
	}
}

// matchesStringFilter checks if a string field matches a filter (case-insensitive partial match)
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: filter a string field by substring match, passing nil filter (pure)
func matchesStringFilter(value string, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return value != "" && containsIgnoreCase(value, *filter)
}

// matchesStringPtrFilter checks if a string pointer field matches a filter
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: filter an optional string field by substring match, passing nil pointer or filter (pure)
func matchesStringPtrFilter(value *string, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return value != nil && containsIgnoreCase(*value, *filter)
}

// matchesStatusFilter checks if the status matches the filter (case-insensitive exact match)
// SEM@eec953ef2825657d2acb1096511bf77db3ca5bea: filter a status field against an allowed list with case-insensitive exact match (pure)
func matchesStatusFilter(value *string, filter []string) bool {
	if len(filter) == 0 {
		return true
	}
	if value == nil {
		return false
	}
	for _, f := range filter {
		if strings.EqualFold(*value, f) {
			return true
		}
	}
	return false
}

// matchesOwnerFilter checks if the owner matches the filter
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: filter a resource owner by substring match across provider ID, display name, and email (pure)
func matchesOwnerFilter(owner User, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return containsIgnoreCase(owner.ProviderId, *filter) ||
		containsIgnoreCase(owner.DisplayName, *filter) ||
		containsIgnoreCase(string(owner.Email), *filter)
}

// matchesDateAfterFilter checks if a date is after the filter date
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: filter a timestamp to those on or after a cutoff date (pure)
func matchesDateAfterFilter(date *time.Time, filter *time.Time) bool {
	if filter == nil {
		return true
	}
	return date != nil && !date.Before(*filter)
}

// matchesDateBeforeFilter checks if a date is before the filter date
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: filter a timestamp to those on or before a cutoff date (pure)
func matchesDateBeforeFilter(date *time.Time, filter *time.Time) bool {
	if filter == nil {
		return true
	}
	return date != nil && !date.After(*filter)
}

// containsIgnoreCase checks if haystack contains needle (case-insensitive)
// SEM@93f28e44afc91d0a7917b5dc1aaed9a52b00529a: check whether a string contains a substring case-insensitively (pure)
func containsIgnoreCase(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// SEM@5981ac53dd2229e2bb211a96f0b495fe72df5f32: store a threat model in the mock store, assigning a UUID and default status if absent
func (m *MockThreatModelStore) Create(item ThreatModel, idSetter func(ThreatModel, string) ThreatModel) (ThreatModel, error) {
	var id string
	if item.Id != nil {
		id = item.Id.String()
	} else {
		// Generate a new ID if not provided
		newID := uuid.New()
		item.Id = &newID
		id = newID.String()
	}
	if idSetter != nil {
		item = idSetter(item, id)
	}
	// Mirror the GORM store's create-time status defaulting so in-memory tests
	// exercise the same semantics as production (Issue #282).
	if item.Status == nil || *item.Status == "" {
		status := DefaultThreatModelStatus
		item.Status = &status
	}
	if item.StatusUpdated == nil {
		now := time.Now().UTC()
		item.StatusUpdated = &now
	}
	m.data[id] = item
	return item, nil
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: replace a threat model entry in the mock store by ID
func (m *MockThreatModelStore) Update(_ context.Context, id string, item ThreatModel) error {
	m.data[id] = item
	return nil
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: remove a threat model from the mock store by ID
func (m *MockThreatModelStore) Delete(id string) error {
	delete(m.data, id)
	return nil
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: return the number of threat models in the mock store (pure)
func (m *MockThreatModelStore) Count() int {
	return len(m.data)
}

// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: fetch a threat model from the mock store regardless of soft-delete status
func (m *MockThreatModelStore) GetIncludingDeleted(id string) (ThreatModel, error) {
	if item, exists := m.data[id]; exists {
		return item, nil
	}
	return ThreatModel{}, fmt.Errorf("threat model not found")
}

// SEM@d48970168f241f7cb359d0cfdb00f3e26abb59da: fetch the authorization list and owner for a threat model from the mock store
func (m *MockThreatModelStore) GetAuthorization(id string) ([]Authorization, User, error) {
	item, err := m.Get(id)
	if err != nil {
		return nil, User{}, err
	}
	return derefAuthSlice(item.Authorization), item.Owner, nil
}

// SEM@d48970168f241f7cb359d0cfdb00f3e26abb59da: fetch authorization and owner for a threat model including soft-deleted records
func (m *MockThreatModelStore) GetAuthorizationIncludingDeleted(id string) ([]Authorization, User, error) {
	item, err := m.GetIncludingDeleted(id)
	if err != nil {
		return nil, User{}, err
	}
	return derefAuthSlice(item.Authorization), item.Owner, nil
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: mark a threat model as deleted by setting its deletion timestamp in the mock store
func (m *MockThreatModelStore) SoftDelete(_ context.Context, id string) error {
	if item, exists := m.data[id]; exists {
		now := time.Now().UTC()
		item.DeletedAt = &now
		m.data[id] = item
		return nil
	}
	return fmt.Errorf("threat model not found: %s", id)
}

// SEM@e4005658033b63171bdc1130fb523d996fbff9a7: clear the deletion timestamp on a soft-deleted threat model in the mock store
func (m *MockThreatModelStore) Restore(id string) error {
	if item, exists := m.data[id]; exists {
		if item.DeletedAt == nil {
			return fmt.Errorf("threat model with ID %s not found or not deleted", id)
		}
		item.DeletedAt = nil
		m.data[id] = item
		return nil
	}
	return fmt.Errorf("threat model with ID %s not found or not deleted", id)
}

// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: permanently remove a threat model from the mock store by ID
func (m *MockThreatModelStore) HardDelete(id string) error {
	return m.Delete(id)
}

// SEM@8fc3d262100f4b47c12ee2155e83e344babb1bd3: in-memory diagram store with threat-model mapping for unit tests
type MockDiagramStore struct {
	data               map[string]DfdDiagram
	threatModelMapping map[string]string // diagram_id -> threat_model_id
}

// SEM@e4005658033b63171bdc1130fb523d996fbff9a7: fetch a diagram from the mock store by ID
func (m *MockDiagramStore) Get(id string) (DfdDiagram, error) {
	if item, exists := m.data[id]; exists {
		return item, nil
	}
	return DfdDiagram{}, fmt.Errorf("diagram not found")
}

// SEM@383547f0bee568a092d84a1f830f227b74f6d723: fetch the parent threat model ID for a diagram from the mock store
func (m *MockDiagramStore) GetThreatModelID(diagramID string) (string, error) {
	if tmID, exists := m.threatModelMapping[diagramID]; exists {
		return tmID, nil
	}
	return "", fmt.Errorf("diagram not found")
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: list diagrams from the mock store, optionally filtered by a predicate (pure)
func (m *MockDiagramStore) List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram {
	var result []DfdDiagram
	for _, item := range m.data {
		if filter == nil || filter(item) {
			result = append(result, item)
		}
	}
	return result
}

// SEM@5981ac53dd2229e2bb211a96f0b495fe72df5f32: store a diagram in the mock store, assigning a UUID if absent
func (m *MockDiagramStore) Create(item DfdDiagram, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	var id string
	if item.Id != nil {
		id = item.Id.String()
	} else {
		// Generate a new ID if not provided
		newID := uuid.New()
		item.Id = &newID
		id = newID.String()
	}
	if idSetter != nil {
		item = idSetter(item, id)
	}
	m.data[id] = item
	return item, nil
}

// SEM@8fc3d262100f4b47c12ee2155e83e344babb1bd3: store a diagram and record its parent threat model association in the mock store
func (m *MockDiagramStore) CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	diagram, err := m.Create(item, idSetter)
	if err == nil && diagram.Id != nil {
		m.threatModelMapping[diagram.Id.String()] = threatModelID
	}
	return diagram, err
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: replace a diagram entry in the mock store by ID
func (m *MockDiagramStore) Update(_ context.Context, id string, item DfdDiagram) error {
	m.data[id] = item
	return nil
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: remove a diagram from the mock store by ID
func (m *MockDiagramStore) Delete(id string) error {
	delete(m.data, id)
	return nil
}

// SEM@9936be5037906d553ff6e5c579ca9f27d222d149: return the number of diagrams in the mock store (pure)
func (m *MockDiagramStore) Count() int {
	return len(m.data)
}

// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: fetch a diagram from the mock store regardless of soft-delete status
func (m *MockDiagramStore) GetIncludingDeleted(id string) (DfdDiagram, error) {
	if item, exists := m.data[id]; exists {
		return item, nil
	}
	return DfdDiagram{}, fmt.Errorf("diagram not found")
}

// SEM@03d4456b06cc7df1a6c638286c7947c816ebd0e8: fetch multiple diagrams by ID list from the mock store, skipping missing entries
func (m *MockDiagramStore) GetBatch(ids []string) ([]DfdDiagram, error) {
	var result []DfdDiagram
	for _, id := range ids {
		d, err := m.Get(id)
		if err != nil {
			continue
		}
		result = append(result, d)
	}
	return result, nil
}

// SEM@c79f3cd129aecd7cd6562b875b7f02232594d3d1: remove a diagram from the mock store (no-op soft-delete for tests)
func (m *MockDiagramStore) SoftDelete(_ context.Context, id string) error {
	return m.Delete(id)
}

// SEM@e4005658033b63171bdc1130fb523d996fbff9a7: no-op restore for mock diagram store in unit tests (pure)
func (m *MockDiagramStore) Restore(id string) error {
	return nil
}

// SEM@3e2f91117dc821148cc037a1ea89214f2215cf5e: permanently remove a diagram from the mock store by ID
func (m *MockDiagramStore) HardDelete(id string) error {
	return m.Delete(id)
}

// InitializeMockStores creates simple mock stores for unit tests
// SEM@8fc3d262100f4b47c12ee2155e83e344babb1bd3: initialize global threat model and diagram stores with empty in-memory mocks (mutates shared state)
func InitializeMockStores() {
	ThreatModelStore = &MockThreatModelStore{data: make(map[string]ThreatModel)}
	DiagramStore = &MockDiagramStore{
		data:               make(map[string]DfdDiagram),
		threatModelMapping: make(map[string]string),
	}
}

// CreateNode creates a Node union item from basic parameters (test helper)
// SEM@e0319b46956724d532b5b4f64b9f66b006e3a0a9: build a diagram cell union item representing a positioned node with given shape and dimensions (pure)
func CreateNode(id string, shape NodeShape, x, y, width, height float32) (DfdDiagram_Cells_Item, error) {
	var item DfdDiagram_Cells_Item

	uuid, err := ParseUUID(id)
	if err != nil {
		return item, fmt.Errorf("invalid UUID: %w", err)
	}

	node := Node{
		Id:    uuid,
		Shape: shape,
		Position: &struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{
			X: x,
			Y: y,
		},
		Size: &struct {
			Height float32 `json:"height"`
			Width  float32 `json:"width"`
		}{
			Height: height,
			Width:  width,
		},
	}

	if err := SafeFromNode(&item, node); err != nil {
		return item, fmt.Errorf("failed to create node: %w", err)
	}

	return item, nil
}

// CreateEdge creates an Edge union item from basic parameters (test helper)
// SEM@e0319b46956724d532b5b4f64b9f66b006e3a0a9: build a diagram cell union item representing a directed edge between two node cells (pure)
func CreateEdge(id string, shape EdgeShape, sourceId, targetId string) (DfdDiagram_Cells_Item, error) {
	var item DfdDiagram_Cells_Item

	uuid, err := ParseUUID(id)
	if err != nil {
		return item, fmt.Errorf("invalid UUID: %w", err)
	}

	sourceUUID, err := ParseUUID(sourceId)
	if err != nil {
		return item, fmt.Errorf("invalid source UUID: %w", err)
	}

	targetUUID, err := ParseUUID(targetId)
	if err != nil {
		return item, fmt.Errorf("invalid target UUID: %w", err)
	}

	edge := Edge{
		Id:     uuid,
		Shape:  shape,
		Source: EdgeTerminal{Cell: sourceUUID},
		Target: EdgeTerminal{Cell: targetUUID},
	}

	if err := SafeFromEdge(&item, edge); err != nil {
		return item, fmt.Errorf("failed to create edge: %w", err)
	}

	return item, nil
}
