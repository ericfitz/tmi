package api

import (
	"encoding/json"
	"fmt"
	"testing"
	
	jsonpatch "github.com/evanphx/json-patch"
)

func TestDiagramStoreAuth(t *testing.T) {
	// Initialize test fixtures
	InitTestFixtures()

	d := Diagram{
		Id:          NewUUID(),
		Name:        "Debug Diagram",
		Owner:       "test@example.com",
		Authorization: []Authorization{
			{
				Subject: "test@example.com",
				Role:    RoleOwner,
			},
			{
				Subject: "newowner@example.com",
				Role:    RoleOwner,
			},
		},
	}

	// Test role resolution
	role := GetUserRoleForDiagram("test@example.com", d)
	fmt.Printf("Role for original owner: %s\n", role)

	newRole := GetUserRoleForDiagram("newowner@example.com", d)
	fmt.Printf("Role for new owner: %s\n", newRole)
}

func TestPatchOperation(t *testing.T) {
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
		Owner:       "test@example.com",
		Authorization: []Authorization{
			{
				Subject: "test@example.com",
				Role:    RoleOwner,
			},
		},
	}
	
	// Store directly
	DiagramStore.mutex.Lock()
	DiagramStore.data[diagramID] = diagram
	DiagramStore.mutex.Unlock()
	
	// Create a patch operation
	patchOp := []PatchOperation{
		{
			Op:   "add",
			Path: "/authorization/-",
			Value: map[string]string{
				"subject": "newowner@example.com",
				"role":    "owner",
			},
		},
	}
	
	// Convert it to JSON patch format
	patchBytes, err := convertOperationsToJSONPatch(patchOp)
	if err != nil {
		t.Fatalf("Failed to convert patch operations: %v", err)
	}
	
	// Create json for original diagram
	originalBytes, err := json.Marshal(diagram)
	if err != nil {
		t.Fatalf("Failed to marshal diagram: %v", err)
	}
	
	// Create patch object
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Fatalf("Failed to decode patch: %v", err)
	}
	
	// Apply patch
	modifiedBytes, err := patch.Apply(originalBytes)
	if err != nil {
		t.Fatalf("Failed to apply patch: %v", err)
	}
	
	// Unmarshal result
	var modifiedDiagram Diagram
	err = json.Unmarshal(modifiedBytes, &modifiedDiagram)
	if err != nil {
		t.Fatalf("Failed to unmarshal modified diagram: %v", err)
	}
	
	// Check that the patch applied correctly
	fmt.Printf("Modified diagram: %+v\n", modifiedDiagram)
	
	// Check authorization entries
	fmt.Printf("Authorization entries after patch: %+v\n", modifiedDiagram.Authorization)
	
	// Update store
	DiagramStore.Update(diagramID, modifiedDiagram)
	
	// Check role for new user
	newUserRole := GetUserRoleForDiagram("newowner@example.com", modifiedDiagram)
	fmt.Printf("Role for new user: %s\n", newUserRole)
	
	// Check that we can retrieve the modified diagram
	retrieved, err := DiagramStore.Get(diagramID)
	if err != nil {
		t.Fatalf("Failed to retrieve diagram: %v", err)
	}
	
	fmt.Printf("Retrieved diagram: %+v\n", retrieved)
	
	// Test role resolution for the new user
	roleAfterRetrieval := GetUserRoleForDiagram("newowner@example.com", retrieved)
	fmt.Printf("Role after retrieval: %s\n", roleAfterRetrieval)
}
