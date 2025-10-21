package api

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// stringPointer returns a pointer to the string value
func stringPointer(s string) *string {
	return &s
}

func uuidPointer(u TypesUUID) *TypesUUID {
	return &u
}

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
	TestFixtures.OwnerUser = "test@example.com"
	TestFixtures.WriterUser = "writer@example.com"
	TestFixtures.ReaderUser = "reader@example.com"

	// Set up owner field value
	TestFixtures.Owner = "test@example.com"

	// Create timestamps
	now := time.Now().UTC()

	// Create a test threat model
	metadata := []Metadata{
		{Key: "priority", Value: "high"},
		{Key: "status", Value: "active"},
	}

	threats := []Threat{
		{
			Id:            uuidPointer(NewUUID()),
			Name:          "SQL Injection",
			Description:   stringPointer("Database attack via malicious SQL"),
			CreatedAt:     &now,
			ModifiedAt:    &now,
			ThreatModelId: uuidPointer(NewUUID()),
			Severity:      ThreatSeverityHigh,
			Priority:      "High",
			Status:        "Open",
			ThreatType:    "Injection",
			Mitigated:     false,
			Metadata:      &metadata,
		},
	}

	// diagrams := []TypesUUID{NewUUID()} // Not used currently

	// Create threat model with new UUID
	uuid1 := NewUUID()
	threatModel := ThreatModel{
		Id:          uuidPointer(uuid1),
		Name:        "Test Threat Model",
		Description: stringPointer("This is a test threat model"),
		CreatedAt:   &now,
		ModifiedAt:  &now,
		Owner:       TestFixtures.Owner,
		Authorization: []Authorization{
			{
				Subject: TestFixtures.OwnerUser,
				Role:    RoleOwner,
			},
			{
				Subject: TestFixtures.WriterUser,
				Role:    RoleWriter,
			},
			{
				Subject: TestFixtures.ReaderUser,
				Role:    RoleReader,
			},
		},
		Metadata: &metadata,
		Threats:  &threats,
		// Diagrams will be set after creating the diagram
	}

	// Create a test diagram with cells using new union types
	cells := []DfdDiagram_Cells_Item{}

	// Create test nodes using helper functions
	if node1, err := CreateNode(NewUUID().String(), Process, 100, 200, 80, 40); err == nil {
		cells = append(cells, node1)
	}

	if node2, err := CreateNode(NewUUID().String(), Store, 300, 200, 80, 40); err == nil {
		cells = append(cells, node2)
	}

	// Create a test edge connecting the nodes
	if len(cells) >= 2 {
		// Extract IDs from the nodes to create an edge
		if node1Data, err := cells[0].AsNode(); err == nil {
			if node2Data, err := cells[1].AsNode(); err == nil {
				if edge, err := CreateEdge(NewUUID().String(), EdgeShapeEdge, node1Data.Id.String(), node2Data.Id.String()); err == nil {
					cells = append(cells, edge)
				}
			}
		}
	}

	// Create diagram with new UUID
	uuid2 := NewUUID()
	diagram := DfdDiagram{
		Id:         uuidPointer(uuid2),
		Name:       "Test Diagram",
		CreatedAt:  now,
		ModifiedAt: now,
		Cells:      cells,
		Metadata:   &metadata,
		Type:       DfdDiagramTypeDFD100,
	}

	// Store authorization data separately for tests
	diagramAuth := []Authorization{
		{
			Subject: TestFixtures.OwnerUser,
			Role:    RoleOwner,
		},
		{
			Subject: TestFixtures.WriterUser,
			Role:    RoleWriter,
		},
		{
			Subject: TestFixtures.ReaderUser,
			Role:    RoleReader,
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

	// Initialize stores appropriately for test environment
	if ThreatModelStore == nil || DiagramStore == nil {
		// Unit tests - initialize mock stores
		InitializeMockStores()
	}

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

func (m *MockThreatModelStore) ListWithCounts(offset, limit int, filter func(ThreatModel) bool) []ThreatModelWithCounts {
	var result []ThreatModelWithCounts
	for _, item := range m.data {
		if filter == nil || filter(item) {
			result = append(result, ThreatModelWithCounts{ThreatModel: item})
		}
	}
	return result
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
	data map[string]DfdDiagram
}

func (m *MockDiagramStore) Get(id string) (DfdDiagram, error) {
	if item, exists := m.data[id]; exists {
		return item, nil
	}
	return DfdDiagram{}, fmt.Errorf("diagram not found")
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
	return m.Create(item, idSetter)
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
	DiagramStore = &MockDiagramStore{data: make(map[string]DfdDiagram)}
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
		Position: struct {
			X float32 `json:"x"`
			Y float32 `json:"y"`
		}{
			X: x,
			Y: y,
		},
		Size: struct {
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
