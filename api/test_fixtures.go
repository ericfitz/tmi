package api

import (
	"time"
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

// ResetStores clears all data from the stores
func ResetStores() {
	// Create new empty stores
	ThreatModelStore = NewDataStore[ThreatModel]()
	DiagramStore = NewDataStore[DfdDiagram]()
}

// InitTestFixtures initializes test data in stores
func InitTestFixtures() {
	// Clear any existing test data first
	ResetStores()

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
			CreatedAt:     now,
			ModifiedAt:    now,
			ThreatModelId: NewUUID(),
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
		CreatedAt:   now,
		ModifiedAt:  now,
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
		// Diagrams: &diagrams, // TODO: Fix after schema changes
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
		Id:          uuidPointer(uuid2),
		Name:        "Test Diagram",
		Description: stringPointer("This is a test diagram"),
		CreatedAt:   now,
		ModifiedAt:  now,
		Cells:       cells,
		Metadata:    &metadata,
		Type:        DfdDiagramTypeDFD100,
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

	// Add directly to the underlying map
	ThreatModelStore.mutex.Lock()
	ThreatModelStore.data[tmID] = threatModel
	ThreatModelStore.mutex.Unlock()

	DiagramStore.mutex.Lock()
	DiagramStore.data[dID] = diagram
	DiagramStore.mutex.Unlock()

	TestFixtures.Initialized = true
}
