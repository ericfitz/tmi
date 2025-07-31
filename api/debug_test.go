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

	// Create a DfdDiagram with the new structure (without Owner and Authorization fields)
	dUuid := NewUUID()
	d := DfdDiagram{
		Id:    &dUuid,
		Name:  "Debug Diagram",
		Cells: []DfdDiagram_Cells_Item{},
	}

	// Set up the parent threat model with owner and authorization
	tmUuid := NewUUID()
	TestFixtures.ThreatModel = ThreatModel{
		Id:    &tmUuid,
		Name:  "Parent Threat Model",
		Owner: "test@example.com",
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

	// Create a DfdDiagram with the new structure (without Owner and Authorization fields)
	diagram := DfdDiagram{
		Id:          &uuid,
		Name:        "Debug Diagram",
		Description: stringPointer("For debugging"),
		CreatedAt:   now,
		ModifiedAt:  now,
		Cells:       []DfdDiagram_Cells_Item{},
	}

	// Set up the parent threat model with owner and authorization
	tmUuid := NewUUID()
	TestFixtures.ThreatModel = ThreatModel{
		Id:    &tmUuid,
		Name:  "Parent Threat Model",
		Owner: "test@example.com",
		Authorization: []Authorization{
			{
				Subject: "test@example.com",
				Role:    RoleOwner,
			},
		},
	}

	// Store using test helper
	InsertDiagramForTest(diagramID, diagram)

	// Create a patch operation that doesn't involve authorization
	// since diagrams no longer have owner or authorization fields
	patchOp := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "Updated Diagram Name",
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

	// Unmarshal result into union type first
	var modifiedDiagramUnion Diagram
	err = json.Unmarshal(modifiedBytes, &modifiedDiagramUnion)
	if err != nil {
		t.Fatalf("Failed to unmarshal modified diagram: %v", err)
	}

	// Convert to DfdDiagram for store operations
	modifiedDiagram, err := modifiedDiagramUnion.AsDfdDiagram()
	if err != nil {
		t.Fatalf("Failed to convert modified diagram: %v", err)
	}

	// Check that the patch applied correctly
	fmt.Printf("Modified diagram: %+v\n", modifiedDiagram)

	// Check authorization entries
	fmt.Printf("Authorization entries after patch: %+v\n", TestFixtures.ThreatModel.Authorization)

	// Update store
	err = DiagramStore.Update(diagramID, modifiedDiagram)
	if err != nil {
		t.Fatalf("Failed to update diagram: %v", err)
	}

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
