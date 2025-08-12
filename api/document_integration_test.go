package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDocumentIntegration tests the complete CRUD lifecycle for documents
func TestDocumentIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:threat_model_id/documents", func(t *testing.T) {
		testDocumentPOST(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/documents", func(t *testing.T) {
		testDocumentGETList(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/documents/:document_id", func(t *testing.T) {
		testDocumentGETByID(t, suite)
	})

	t.Run("PUT /threat_models/:threat_model_id/documents/:document_id", func(t *testing.T) {
		testDocumentPUT(t, suite)
	})

	t.Run("DELETE /threat_models/:threat_model_id/documents/:document_id", func(t *testing.T) {
		testDocumentDELETE(t, suite)
	})

	t.Run("POST /threat_models/:threat_model_id/documents/bulk", func(t *testing.T) {
		testDocumentBulkCreate(t, suite)
	})
}

// testDocumentPOST tests creating documents via POST
func testDocumentPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":        "Integration Test Document",
		"url":         "https://example.com/integration-test-doc.pdf",
		"description": "A document created during integration testing",
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/documents", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["url"], response["url"])
	assert.Equal(t, requestBody["description"], response["description"])

	// Store the document ID for other tests
	suite.testDocumentID = response["id"].(string)
}

// testDocumentGETList tests retrieving documents list via GET
func testDocumentGETList(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have at least one document
	if suite.testDocumentID == "" {
		suite.createTestDocument(t)
	}

	// Test GET list
	path := fmt.Sprintf("/threat_models/%s/documents", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one document")

	// Check the first document in the list
	document := response[0].(map[string]interface{})
	assert.NotEmpty(t, document["id"])
	assert.NotEmpty(t, document["name"])
	assert.NotEmpty(t, document["url"])

	// Test pagination
	req = suite.makeAuthenticatedRequest("GET", path+"?limit=1&offset=0", nil)
	w = suite.executeRequest(req)
	paginatedResponse := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.LessOrEqual(t, len(paginatedResponse), 1, "Pagination should limit results")
}

// testDocumentGETByID tests retrieving specific document via GET
func testDocumentGETByID(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a document to get
	if suite.testDocumentID == "" {
		suite.createTestDocument(t)
	}

	// Test GET by ID
	path := fmt.Sprintf("/threat_models/%s/documents/%s", suite.threatModelID, suite.testDocumentID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, suite.testDocumentID, response["id"])
	assert.NotEmpty(t, response["name"])
	assert.NotEmpty(t, response["url"])
}

// testDocumentPUT tests updating documents via PUT
func testDocumentPUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a document to update
	if suite.testDocumentID == "" {
		suite.createTestDocument(t)
	}

	// Update the document
	updateBody := map[string]interface{}{
		"name":        "Updated Integration Test Document",
		"url":         "https://example.com/updated-integration-test-doc.pdf",
		"description": "Updated description for integration testing",
	}

	path := fmt.Sprintf("/threat_models/%s/documents/%s", suite.threatModelID, suite.testDocumentID)
	req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, suite.testDocumentID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["url"], response["url"])
	assert.Equal(t, updateBody["description"], response["description"])
}

// testDocumentDELETE tests deleting documents via DELETE
func testDocumentDELETE(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create a document specifically for deletion
	deleteTestDocumentID := suite.createTestDocument(t)

	// Delete the document
	path := fmt.Sprintf("/threat_models/%s/documents/%s", suite.threatModelID, deleteTestDocumentID)
	req := suite.makeAuthenticatedRequest("DELETE", path, nil)
	w := suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the document no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testDocumentBulkCreate tests bulk creating documents
func testDocumentBulkCreate(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk create (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"name":        "Bulk Test Document 1",
			"url":         "https://example.com/bulk-doc-1.pdf",
			"description": "First document in bulk create test",
		},
		{
			"name":        "Bulk Test Document 2",
			"url":         "https://example.com/bulk-doc-2.pdf",
			"description": "Second document in bulk create test",
		},
		{
			"name":        "Bulk Test Document 3",
			"url":         "https://example.com/bulk-doc-3.pdf",
			"description": "Third document in bulk create test",
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/documents/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	createdDocuments := suite.assertJSONArrayResponse(t, w, http.StatusCreated)
	assert.Len(t, createdDocuments, 3, "Should create 3 documents")

	// Verify each created document
	for i, documentInterface := range createdDocuments {
		document := documentInterface.(map[string]interface{})
		originalDocument := requestBody[i]

		assert.NotEmpty(t, document["id"], "Each document should have an ID")
		assert.Equal(t, originalDocument["name"], document["name"])
		assert.Equal(t, originalDocument["url"], document["url"])
		assert.Equal(t, originalDocument["description"], document["description"])
	}
}
