package api_test

import (
	"fmt"
	"testing"
	
	. "github.com/ericfitz/tmi/api"
)

func TestDiagramStoreAndAuth(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	// Create a test diagram
	diagramID := "debug-diagram-id"
	uuid, _ := ParseUUID(diagramID)
	now := CurrentTime()
	
	diagram := Diagram{
		Id:          uuid,
		Name:        "Debug Diagram",
		Description: stringPointer("For debugging"),
		CreatedAt:   now,
		ModifiedAt:  now,
		Owner:       "owner@example.com",
		Authorization: []Authorization{
			{
				Subject: "owner@example.com",
				Role:    RoleOwner,
			},
		},
	}

	// Store directly
	DiagramStore.mutex.Lock()
	DiagramStore.data[diagramID] = diagram
	DiagramStore.mutex.Unlock()

	// Now retrieve and verify
	retrieved, err := DiagramStore.Get(diagramID)
	if err \!= nil {
		t.Fatalf("Failed to retrieve diagram: %v", err)
	}

	fmt.Printf("Retrieved diagram: %+v\n", retrieved)
	fmt.Printf("Auth entries: %+v\n", retrieved.Authorization)

	// Test role resolution
	role := GetUserRoleForDiagram("owner@example.com", retrieved)
	fmt.Printf("Role for owner: %s\n", role)

	// Now add a new authorization
	updatedDiagram := retrieved
	updatedDiagram.Authorization = append(updatedDiagram.Authorization, Authorization{
		Subject: "newuser@example.com",
		Role:    RoleOwner,
	})

	err = DiagramStore.Update(diagramID, updatedDiagram)
	if err \!= nil {
		t.Fatalf("Failed to update diagram: %v", err)
	}

	// Retrieve again
	updated, err := DiagramStore.Get(diagramID)
	if err \!= nil {
		t.Fatalf("Failed to retrieve updated diagram: %v", err)
	}

	fmt.Printf("Updated diagram: %+v\n", updated)
	fmt.Printf("Updated auth entries: %+v\n", updated.Authorization)

	// Test role resolution for new user
	newUserRole := GetUserRoleForDiagram("newuser@example.com", updated)
	fmt.Printf("Role for new user: %s\n", newUserRole)
}
