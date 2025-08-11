package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSourceIntegration tests the complete CRUD lifecycle for sources
func TestSourceIntegration(t *testing.T) {
	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:id/sources", func(t *testing.T) {
		testSourcePOST(t, suite)
	})

	t.Run("GET /threat_models/:id/sources", func(t *testing.T) {
		testSourceGETList(t, suite)
	})

	t.Run("GET /threat_models/:id/sources/:source_id", func(t *testing.T) {
		testSourceGETByID(t, suite)
	})

	t.Run("PUT /threat_models/:id/sources/:source_id", func(t *testing.T) {
		testSourcePUT(t, suite)
	})

	t.Run("DELETE /threat_models/:id/sources/:source_id", func(t *testing.T) {
		testSourceDELETE(t, suite)
	})

	t.Run("POST /threat_models/:id/sources/bulk", func(t *testing.T) {
		testSourceBulkCreate(t, suite)
	})
}

// testSourcePOST tests creating sources via POST
func testSourcePOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":        "Integration Test Source",
		"url":         "https://github.com/example/integration-test-repo",
		"description": "A source created during integration testing",
		"type":        "git",
		"parameters": map[string]interface{}{
			"refType":  "branch",
			"refValue": "main",
			"subPath":  "/src/integration",
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/sources", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["url"], response["url"])
	assert.Equal(t, requestBody["description"], response["description"])
	assert.Equal(t, requestBody["type"], response["type"])

	// Verify parameters
	responseParams, ok := response["parameters"].(map[string]interface{})
	assert.True(t, ok, "Parameters should be an object")
	expectedParams := requestBody["parameters"].(map[string]interface{})
	assert.Equal(t, expectedParams["refType"], responseParams["refType"])
	assert.Equal(t, expectedParams["refValue"], responseParams["refValue"])
	assert.Equal(t, expectedParams["subPath"], responseParams["subPath"])

	// Store the source ID for other tests
	suite.testSourceID = response["id"].(string)
}

// testSourceGETList tests retrieving sources list via GET
func testSourceGETList(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have at least one source
	if suite.testSourceID == "" {
		suite.createTestSource(t)
	}

	// Test GET list
	path := fmt.Sprintf("/threat_models/%s/sources", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one source")

	// Check the first source in the list
	source := response[0].(map[string]interface{})
	assert.NotEmpty(t, source["id"])
	assert.NotEmpty(t, source["name"])
	assert.NotEmpty(t, source["url"])
	assert.NotEmpty(t, source["type"])

	// Test pagination
	req = suite.makeAuthenticatedRequest("GET", path+"?limit=1&offset=0", nil)
	w = suite.executeRequest(req)
	paginatedResponse := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.LessOrEqual(t, len(paginatedResponse), 1, "Pagination should limit results")
}

// testSourceGETByID tests retrieving specific source via GET
func testSourceGETByID(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a source to get
	if suite.testSourceID == "" {
		suite.createTestSource(t)
	}

	// Test GET by ID
	path := fmt.Sprintf("/threat_models/%s/sources/%s", suite.threatModelID, suite.testSourceID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, suite.testSourceID, response["id"])
	assert.NotEmpty(t, response["name"])
	assert.NotEmpty(t, response["url"])
	assert.NotEmpty(t, response["type"])

	// Verify parameters exist
	if response["parameters"] != nil {
		params, ok := response["parameters"].(map[string]interface{})
		assert.True(t, ok, "Parameters should be an object")
		assert.NotEmpty(t, params["refType"])
		assert.NotEmpty(t, params["refValue"])
	}
}

// testSourcePUT tests updating sources via PUT
func testSourcePUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a source to update
	if suite.testSourceID == "" {
		suite.createTestSource(t)
	}

	// Update the source
	updateBody := map[string]interface{}{
		"name":        "Updated Integration Test Source",
		"url":         "https://github.com/example/updated-integration-test-repo",
		"description": "Updated description for integration testing",
		"type":        "git",
		"parameters": map[string]interface{}{
			"refType":  "tag",
			"refValue": "v1.0.0",
			"subPath":  "/src/updated",
		},
	}

	path := fmt.Sprintf("/threat_models/%s/sources/%s", suite.threatModelID, suite.testSourceID)
	req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, suite.testSourceID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["url"], response["url"])
	assert.Equal(t, updateBody["description"], response["description"])
	assert.Equal(t, updateBody["type"], response["type"])

	// Verify updated parameters
	responseParams, ok := response["parameters"].(map[string]interface{})
	assert.True(t, ok, "Parameters should be an object")
	expectedParams := updateBody["parameters"].(map[string]interface{})
	assert.Equal(t, expectedParams["refType"], responseParams["refType"])
	assert.Equal(t, expectedParams["refValue"], responseParams["refValue"])
	assert.Equal(t, expectedParams["subPath"], responseParams["subPath"])
}

// testSourceDELETE tests deleting sources via DELETE
func testSourceDELETE(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create a source specifically for deletion
	deleteTestSourceID := suite.createTestSource(t)

	// Delete the source
	path := fmt.Sprintf("/threat_models/%s/sources/%s", suite.threatModelID, deleteTestSourceID)
	req := suite.makeAuthenticatedRequest("DELETE", path, nil)
	w := suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the source no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testSourceBulkCreate tests bulk creating sources
func testSourceBulkCreate(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk create (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"name":        "Bulk Test Source 1",
			"url":         "https://github.com/example/bulk-repo-1",
			"description": "First source in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "branch",
				"refValue": "main",
			},
		},
		{
			"name":        "Bulk Test Source 2",
			"url":         "https://gitlab.com/example/bulk-repo-2",
			"description": "Second source in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "tag",
				"refValue": "v2.0.0",
			},
		},
		{
			"name":        "Bulk Test Source 3",
			"url":         "https://bitbucket.org/example/bulk-repo-3",
			"description": "Third source in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "commit",
				"refValue": "abc123def456",
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/sources/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	createdSources := suite.assertJSONArrayResponse(t, w, http.StatusCreated)
	assert.Len(t, createdSources, 3, "Should create 3 sources")

	// Verify each created source
	for i, sourceInterface := range createdSources {
		source := sourceInterface.(map[string]interface{})
		originalSource := requestBody[i]

		assert.NotEmpty(t, source["id"], "Each source should have an ID")
		assert.Equal(t, originalSource["name"], source["name"])
		assert.Equal(t, originalSource["url"], source["url"])
		assert.Equal(t, originalSource["description"], source["description"])
		assert.Equal(t, originalSource["type"], source["type"])

		// Verify parameters
		responseParams, ok := source["parameters"].(map[string]interface{})
		assert.True(t, ok, "Parameters should be an object")
		expectedParams := originalSource["parameters"].(map[string]interface{})
		assert.Equal(t, expectedParams["refType"], responseParams["refType"])
		assert.Equal(t, expectedParams["refValue"], responseParams["refValue"])
	}
}
