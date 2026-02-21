package api

import (
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
		Authorization: []Authorization{
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
type MockThreatModelStore struct {
	data map[string]ThreatModel
}

func (m *MockThreatModelStore) Get(id string) (ThreatModel, error) {
	if item, exists := m.data[id]; exists {
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

func (m *MockThreatModelStore) List(offset, limit int, filter func(ThreatModel) bool) []ThreatModel {
	var result []ThreatModel
	for _, item := range m.data {
		if filter == nil || filter(item) {
			result = append(result, item)
		}
	}
	return result
}

func (m *MockThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool, filters *ThreatModelFilters) ([]ThreatModelWithCounts, int) {
	var result []ThreatModelWithCounts
	for _, item := range m.data {
		// Apply authorization filter
		if filter != nil && !filter(item) {
			continue
		}

		// Apply query parameter filters if provided
		if filters != nil && !matchesThreatModelFilters(item, filters) {
			continue
		}

		result = append(result, ThreatModelWithCounts{ThreatModel: item})
	}

	// Store total count before pagination
	total := len(result)

	// Apply pagination
	if offset >= total {
		return []ThreatModelWithCounts{}, total
	}

	end := offset + limit
	if end > total || limit <= 0 {
		end = total
	}

	return result[offset:end], total
}

// matchesThreatModelFilters checks if a threat model matches the provided filters
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
	return true
}

// matchesStringFilter checks if a string field matches a filter (case-insensitive partial match)
func matchesStringFilter(value string, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return value != "" && containsIgnoreCase(value, *filter)
}

// matchesStringPtrFilter checks if a string pointer field matches a filter
func matchesStringPtrFilter(value *string, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return value != nil && containsIgnoreCase(*value, *filter)
}

// matchesStatusFilter checks if the status matches the filter (case-insensitive exact match)
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
func matchesOwnerFilter(owner User, filter *string) bool {
	if filter == nil || *filter == "" {
		return true
	}
	return containsIgnoreCase(owner.ProviderId, *filter) ||
		containsIgnoreCase(owner.DisplayName, *filter) ||
		containsIgnoreCase(string(owner.Email), *filter)
}

// matchesDateAfterFilter checks if a date is after the filter date
func matchesDateAfterFilter(date *time.Time, filter *time.Time) bool {
	if filter == nil {
		return true
	}
	return date != nil && !date.Before(*filter)
}

// matchesDateBeforeFilter checks if a date is before the filter date
func matchesDateBeforeFilter(date *time.Time, filter *time.Time) bool {
	if filter == nil {
		return true
	}
	return date != nil && !date.After(*filter)
}

// containsIgnoreCase checks if haystack contains needle (case-insensitive)
func containsIgnoreCase(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

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
	m.data[id] = item
	return item, nil
}

func (m *MockThreatModelStore) Update(id string, item ThreatModel) error {
	m.data[id] = item
	return nil
}

func (m *MockThreatModelStore) Delete(id string) error {
	delete(m.data, id)
	return nil
}

func (m *MockThreatModelStore) Count() int {
	return len(m.data)
}

type MockDiagramStore struct {
	data               map[string]DfdDiagram
	threatModelMapping map[string]string // diagram_id -> threat_model_id
}

func (m *MockDiagramStore) Get(id string) (DfdDiagram, error) {
	if item, exists := m.data[id]; exists {
		return item, nil
	}
	return DfdDiagram{}, fmt.Errorf("diagram not found")
}

func (m *MockDiagramStore) GetThreatModelID(diagramID string) (string, error) {
	if tmID, exists := m.threatModelMapping[diagramID]; exists {
		return tmID, nil
	}
	return "", fmt.Errorf("diagram not found")
}

func (m *MockDiagramStore) List(offset, limit int, filter func(DfdDiagram) bool) []DfdDiagram {
	var result []DfdDiagram
	for _, item := range m.data {
		if filter == nil || filter(item) {
			result = append(result, item)
		}
	}
	return result
}

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

func (m *MockDiagramStore) CreateWithThreatModel(item DfdDiagram, threatModelID string, idSetter func(DfdDiagram, string) DfdDiagram) (DfdDiagram, error) {
	diagram, err := m.Create(item, idSetter)
	if err == nil && diagram.Id != nil {
		m.threatModelMapping[diagram.Id.String()] = threatModelID
	}
	return diagram, err
}

func (m *MockDiagramStore) Update(id string, item DfdDiagram) error {
	m.data[id] = item
	return nil
}

func (m *MockDiagramStore) Delete(id string) error {
	delete(m.data, id)
	return nil
}

func (m *MockDiagramStore) Count() int {
	return len(m.data)
}

// InitializeMockStores creates simple mock stores for unit tests
func InitializeMockStores() {
	ThreatModelStore = &MockThreatModelStore{data: make(map[string]ThreatModel)}
	DiagramStore = &MockDiagramStore{
		data:               make(map[string]DfdDiagram),
		threatModelMapping: make(map[string]string),
	}
}

// CreateNode creates a Node union item from basic parameters (test helper)
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

	if err := item.FromNode(node); err != nil {
		return item, fmt.Errorf("failed to create node: %w", err)
	}

	return item, nil
}

// CreateEdge creates an Edge union item from basic parameters (test helper)
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

	if err := item.FromEdge(edge); err != nil {
		return item, fmt.Errorf("failed to create edge: %w", err)
	}

	return item, nil
}
