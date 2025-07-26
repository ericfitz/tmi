package api_test

import (
	"fmt"
	"testing"

	. "github.com/ericfitz/tmi/api"
)

// Helper function to create string pointers
func stringPointer(s string) *string {
	return &s
}

func TestDiagramStoreAndAuth(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Create a test diagram
	diagramID := "debug-diagram-id"
	uuid, _ := ParseUUID(diagramID)
	now := CurrentTime()

	// Create a DfdDiagram with the new structure (without Owner and Authorization fields)
	dfdDiagram := DfdDiagram{
		Id:          &uuid,
		Name:        "Debug Diagram",
		Description: stringPointer("For debugging"),
		CreatedAt:   now,
		ModifiedAt:  now,
		Cells:       []DfdDiagram_Cells_Item{},
	}

	// Convert to union type for API compatibility
	var diagram Diagram
	err := diagram.FromDfdDiagram(dfdDiagram)
	if err != nil {
		panic(err) // In test code, panic is acceptable for setup errors
	}

	// Set up the parent threat model with owner and authorization
	tmUuid := NewUUID()
	TestFixtures.ThreatModel = ThreatModel{
		Id:    &tmUuid,
		Name:  "Parent Threat Model",
		Owner: "owner@example.com",
		Authorization: []Authorization{
			{
				Subject: "owner@example.com",
				Role:    RoleOwner,
			},
		},
	}

	// Insert the diagram directly into the store using our helper function
	InsertDiagramForTest(diagramID, dfdDiagram)

	// Now update using the API
	err = DiagramStore.Update(diagramID, dfdDiagram)
	if err != nil {
		t.Fatalf("Failed to store diagram: %v", err)
	}

	// Now retrieve and verify
	retrieved, err := DiagramStore.Get(diagramID)
	if err != nil {
		t.Fatalf("Failed to retrieve diagram: %v", err)
	}

	fmt.Printf("Retrieved diagram: %+v\n", retrieved)
	fmt.Printf("Auth entries: %+v\n", TestFixtures.ThreatModel.Authorization)

	// Test role resolution
	role := GetUserRoleForDiagram("owner@example.com", retrieved)
	fmt.Printf("Role for owner: %s\n", role)

	// Now add a new authorization
	// Update the diagram
	updatedDiagram := retrieved

	// Update authorization in the parent threat model
	TestFixtures.ThreatModel.Authorization = append(TestFixtures.ThreatModel.Authorization, Authorization{
		Subject: "newuser@example.com",
		Role:    RoleOwner,
	})

	err = DiagramStore.Update(diagramID, updatedDiagram)
	if err != nil {
		t.Fatalf("Failed to update diagram: %v", err)
	}

	// Retrieve again
	updated, err := DiagramStore.Get(diagramID)
	if err != nil {
		t.Fatalf("Failed to retrieve updated diagram: %v", err)
	}

	fmt.Printf("Updated diagram: %+v\n", updated)
	fmt.Printf("Updated auth entries: %+v\n", TestFixtures.ThreatModel.Authorization)

	// Test role resolution for new user
	newUserRole := GetUserRoleForDiagram("newuser@example.com", updated)
	fmt.Printf("Role for new user: %s\n", newUserRole)
}
