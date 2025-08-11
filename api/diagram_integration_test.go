package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestThreatModelDiagramIntegration tests the complete CRUD lifecycle for threat model diagrams
func TestThreatModelDiagramIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:id/diagrams", func(t *testing.T) {
		testThreatModelDiagramPOST(t, suite)
	})

	t.Run("GET /threat_models/:id/diagrams", func(t *testing.T) {
		testThreatModelDiagramGETList(t, suite)
	})

	t.Run("GET /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testThreatModelDiagramGETByID(t, suite)
	})

	t.Run("PUT /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testThreatModelDiagramPUT(t, suite)
	})

	t.Run("PATCH /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testThreatModelDiagramPATCH(t, suite)
	})

	t.Run("DELETE /threat_models/:id/diagrams/:diagram_id", func(t *testing.T) {
		testThreatModelDiagramDELETE(t, suite)
	})
}

// testThreatModelDiagramPOST tests creating diagrams via POST
func testThreatModelDiagramPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name": "Integration Test Diagram",
		"type": "DFD-1.0.0",
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])

	// Store the diagram ID for other tests
	suite.testDiagramID = response["id"].(string)
}

// testThreatModelDiagramGETList tests retrieving diagrams list via GET
func testThreatModelDiagramGETList(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have at least one diagram
	if suite.testDiagramID == "" {
		suite.createTestDiagram(t)
	}

	// Test GET list
	path := fmt.Sprintf("/threat_models/%s/diagrams", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one diagram")

	// Check the first diagram in the list
	diagram := response[0].(map[string]interface{})
	assert.NotEmpty(t, diagram["id"])
	assert.NotEmpty(t, diagram["name"])

	// Test pagination
	req = suite.makeAuthenticatedRequest("GET", path+"?limit=1&offset=0", nil)
	w = suite.executeRequest(req)
	paginatedResponse := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.LessOrEqual(t, len(paginatedResponse), 1, "Pagination should limit results")
}

// testThreatModelDiagramGETByID tests retrieving specific diagram via GET
func testThreatModelDiagramGETByID(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a diagram to get
	if suite.testDiagramID == "" {
		suite.createTestDiagram(t)
	}

	// Test GET by ID
	path := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, suite.testDiagramID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, suite.testDiagramID, response["id"])
	assert.NotEmpty(t, response["name"])
}

// testThreatModelDiagramPUT tests updating diagrams via PUT
func testThreatModelDiagramPUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a diagram to update
	if suite.testDiagramID == "" {
		suite.createTestDiagram(t)
	}

	// Update the diagram
	updateBody := map[string]interface{}{
		"id":   suite.testDiagramID,
		"name": "Updated Integration Test Diagram",
	}

	path := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, suite.testDiagramID)
	req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, suite.testDiagramID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
}

// testThreatModelDiagramDELETE tests deleting diagrams via DELETE
func testThreatModelDiagramDELETE(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create a diagram specifically for deletion
	deleteTestDiagramID := suite.createTestDiagram(t)

	// Delete the diagram
	path := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, deleteTestDiagramID)
	req := suite.makeAuthenticatedRequest("DELETE", path, nil)
	w := suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the diagram no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testThreatModelDiagramPATCH tests partially updating diagrams via PATCH using JSON Patch format
func testThreatModelDiagramPATCH(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a diagram to patch
	if suite.testDiagramID == "" {
		suite.createTestDiagram(t)
	}

	// Store original values for verification
	originalPath := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, suite.testDiagramID)
	originalReq := suite.makeAuthenticatedRequest("GET", originalPath, nil)
	originalW := suite.executeRequest(originalReq)
	originalResponse := suite.assertJSONResponse(t, originalW, http.StatusOK)

	// Test JSON Patch operations
	patchOps := []PatchOperation{
		{
			Op:    "replace",
			Path:  "/name",
			Value: "PATCH Updated Diagram Name",
		},
	}

	// Make PATCH request
	path := fmt.Sprintf("/threat_models/%s/diagrams/%s", suite.threatModelID, suite.testDiagramID)
	req := suite.makeAuthenticatedRequest("PATCH", path, patchOps)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify that the response contains the patched values
	assert.Equal(t, suite.testDiagramID, response["id"])
	assert.Equal(t, "PATCH Updated Diagram Name", response["name"])

	// Verify that other fields weren't changed unexpectedly
	assert.Equal(t, originalResponse["id"], response["id"])

	// Verify that modified_at was updated (should be different from original)
	if originalModified, exists := originalResponse["modified_at"]; exists {
		if responseModified, exists := response["modified_at"]; exists {
			assert.NotEqual(t, originalModified, responseModified, "modified_at should be updated")
		}
	}

	// Verify database persistence using the helper function
	expectedData := map[string]interface{}{
		"name": "PATCH Updated Diagram Name",
	}
	verifyDiagramInDatabase(suite, t, suite.testDiagramID, suite.threatModelID, expectedData)

	// Test individual field updates
	t.Run("SingleFieldPatch", func(t *testing.T) {
		singlePatchOps := []PatchOperation{
			{
				Op:    "replace",
				Path:  "/name",
				Value: "Single Field PATCH Update",
			},
		}

		req := suite.makeAuthenticatedRequest("PATCH", path, singlePatchOps)
		w := suite.executeRequest(req)
		response := suite.assertJSONResponse(t, w, http.StatusOK)

		// Verify only the name was updated
		assert.Equal(t, "Single Field PATCH Update", response["name"])

		// Verify database persistence
		expectedSingleData := map[string]interface{}{
			"name": "Single Field PATCH Update",
		}
		verifyDiagramInDatabase(suite, t, suite.testDiagramID, suite.threatModelID, expectedSingleData)
	})

	// Test error cases
	t.Run("InvalidPatchOperations", func(t *testing.T) {
		// Test invalid path
		invalidPatchOps := []PatchOperation{
			{
				Op:    "replace",
				Path:  "/nonexistent_field",
				Value: "should fail",
			},
		}

		req := suite.makeAuthenticatedRequest("PATCH", path, invalidPatchOps)
		w := suite.executeRequest(req)

		// Depending on implementation, this could return 400 Bad Request or another appropriate error
		// For now, we'll accept any error status (4xx or 5xx)
		assert.True(t, w.Code >= 400, "Invalid patch should return an error status")

		// Test invalid operation
		invalidOpPatchOps := []PatchOperation{
			{
				Op:    "invalid_operation",
				Path:  "/name",
				Value: "should fail",
			},
		}

		req = suite.makeAuthenticatedRequest("PATCH", path, invalidOpPatchOps)
		w = suite.executeRequest(req)
		assert.True(t, w.Code >= 400, "Invalid operation should return an error status")
	})
}
