package api

import (
	"time"
)

// stringPointer returns a pointer to the string value
func stringPointer(s string) *string {
	return &s
}

// Fixtures provides test data for unit tests
var TestFixtures struct {
	// Test users for authorization
	OwnerUser  string
	WriterUser string
	ReaderUser string
	
	// Owner field values
	Owner      string
	
	// Test threat models
	ThreatModel     ThreatModel
	ThreatModelID   string
	
	// Test diagrams
	Diagram         Diagram
	DiagramID       string
	
	// Test flags
	Initialized     bool
}

// ResetStores clears all data from the stores
func ResetStores() {
	// Create new empty stores
	ThreatModelStore = NewStore[ThreatModel]()
	DiagramStore = NewStore[Diagram]()
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
			Id:           NewUUID(),
			Name:         "SQL Injection",
			Description:  stringPointer("Database attack via malicious SQL"),
			CreatedAt:    now,
			ModifiedAt:   now,
			ThreatModelId: NewUUID(),
			Metadata:     &metadata,
		},
	}
	
	diagrams := []TypesUUID{NewUUID()}
	
	// Create threat model with new UUID
	uuid1 := NewUUID()
	threatModel := ThreatModel{
		Id:          uuid1,
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
		Metadata:  &metadata,
		Threats:   &threats,
		Diagrams:  &diagrams,
	}
	
	// Create a test diagram
	components := []DiagramComponent{
		{
			Id:   NewUUID(),
			Type: "node",
			Data: map[string]interface{}{
				"x":     100,
				"y":     200,
				"label": "Web Server",
			},
			Metadata: &metadata,
		},
		{
			Id:   NewUUID(),
			Type: "node",
			Data: map[string]interface{}{
				"x":     300,
				"y":     200,
				"label": "Database",
			},
			Metadata: &metadata,
		},
	}
	
	// Create diagram with new UUID
	uuid2 := NewUUID()
	diagram := Diagram{
		Id:          uuid2,
		Name:        "Test Diagram",
		Description: stringPointer("This is a test diagram"),
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
		Components: &components,
		Metadata:   &metadata,
	}
	
	// Store the fixtures with their UUIDs
	tmID := uuid1.String()
	dID := uuid2.String()
	
	TestFixtures.ThreatModel = threatModel
	TestFixtures.ThreatModelID = tmID
	
	TestFixtures.Diagram = diagram
	TestFixtures.DiagramID = dID
	
	// Add directly to the underlying map
	ThreatModelStore.mutex.Lock()
	ThreatModelStore.data[tmID] = threatModel
	ThreatModelStore.mutex.Unlock()
	
	DiagramStore.mutex.Lock()
	DiagramStore.data[dID] = diagram
	DiagramStore.mutex.Unlock()
	
	TestFixtures.Initialized = true
}