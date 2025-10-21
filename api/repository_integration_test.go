package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRepositoryIntegration tests the complete CRUD lifecycle for repositorys
func TestRepositoryIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:threat_model_id/repositorys", func(t *testing.T) {
		testRepositoryPOST(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/repositorys", func(t *testing.T) {
		testRepositoryGETList(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/repositorys/:repository_id", func(t *testing.T) {
		testRepositoryGETByID(t, suite)
	})

	t.Run("PUT /threat_models/:threat_model_id/repositorys/:repository_id", func(t *testing.T) {
		testRepositoryPUT(t, suite)
	})

	t.Run("DELETE /threat_models/:threat_model_id/repositorys/:repository_id", func(t *testing.T) {
		testRepositoryDELETE(t, suite)
	})

	t.Run("POST /threat_models/:threat_model_id/repositorys/bulk", func(t *testing.T) {
		testRepositoryBulkCreate(t, suite)
	})
}

// testRepositoryPOST tests creating repositorys via POST
func testRepositoryPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":        "Integration Test Repository",
		"url":         "https://github.com/example/integration-test-repo",
		"description": "A repository created during integration testing",
		"type":        "git",
		"parameters": map[string]interface{}{
			"refType":  "branch",
			"refValue": "main",
			"subPath":  "/src/integration",
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/repositorys", suite.threatModelID)
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

	// Store the repository ID for other tests
	suite.testRepositoryID = response["id"].(string)
}

// testRepositoryGETList tests retrieving repositorys list via GET
func testRepositoryGETList(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have at least one repository
	if suite.testRepositoryID == "" {
		suite.createTestRepository(t)
	}

	// Test GET list
	path := fmt.Sprintf("/threat_models/%s/repositorys", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one repository")

	// Check the first repository in the list
	repository := response[0].(map[string]interface{})
	assert.NotEmpty(t, repository["id"])
	assert.NotEmpty(t, repository["name"])
	assert.NotEmpty(t, repository["url"])
	assert.NotEmpty(t, repository["type"])

	// Test pagination
	req = suite.makeAuthenticatedRequest("GET", path+"?limit=1&offset=0", nil)
	w = suite.executeRequest(req)
	paginatedResponse := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.LessOrEqual(t, len(paginatedResponse), 1, "Pagination should limit results")
}

// testRepositoryGETByID tests retrieving specific repository via GET
func testRepositoryGETByID(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a repository to get
	if suite.testRepositoryID == "" {
		suite.createTestRepository(t)
	}

	// Test GET by ID
	path := fmt.Sprintf("/threat_models/%s/repositorys/%s", suite.threatModelID, suite.testRepositoryID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, suite.testRepositoryID, response["id"])
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

// testRepositoryPUT tests updating repositorys via PUT
func testRepositoryPUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a repository to update
	if suite.testRepositoryID == "" {
		suite.createTestRepository(t)
	}

	// Update the repository
	updateBody := map[string]interface{}{
		"name":        "Updated Integration Test Repository",
		"url":         "https://github.com/example/updated-integration-test-repo",
		"description": "Updated description for integration testing",
		"type":        "git",
		"parameters": map[string]interface{}{
			"refType":  "tag",
			"refValue": "v1.0.0",
			"subPath":  "/src/updated",
		},
	}

	path := fmt.Sprintf("/threat_models/%s/repositorys/%s", suite.threatModelID, suite.testRepositoryID)
	req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, suite.testRepositoryID, response["id"])
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

// testRepositoryDELETE tests deleting repositorys via DELETE
func testRepositoryDELETE(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create a repository specifically for deletion
	deleteTestRepositoryID := suite.createTestRepository(t)

	// Delete the repository
	path := fmt.Sprintf("/threat_models/%s/repositorys/%s", suite.threatModelID, deleteTestRepositoryID)
	req := suite.makeAuthenticatedRequest("DELETE", path, nil)
	w := suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the repository no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testRepositoryBulkCreate tests bulk creating repositorys
func testRepositoryBulkCreate(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk create (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"name":        "Bulk Test Repository 1",
			"url":         "https://github.com/example/bulk-repo-1",
			"description": "First repository in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "branch",
				"refValue": "main",
			},
		},
		{
			"name":        "Bulk Test Repository 2",
			"url":         "https://gitlab.com/example/bulk-repo-2",
			"description": "Second repository in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "tag",
				"refValue": "v2.0.0",
			},
		},
		{
			"name":        "Bulk Test Repository 3",
			"url":         "https://bitbucket.org/example/bulk-repo-3",
			"description": "Third repository in bulk create test",
			"type":        "git",
			"parameters": map[string]interface{}{
				"refType":  "commit",
				"refValue": "abc123def456",
			},
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/repositorys/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	createdRepositorys := suite.assertJSONArrayResponse(t, w, http.StatusCreated)
	assert.Len(t, createdRepositorys, 3, "Should create 3 repositorys")

	// Verify each created repository
	for i, repositoryInterface := range createdRepositorys {
		repository := repositoryInterface.(map[string]interface{})
		originalRepository := requestBody[i]

		assert.NotEmpty(t, repository["id"], "Each repository should have an ID")
		assert.Equal(t, originalRepository["name"], repository["name"])
		assert.Equal(t, originalRepository["url"], repository["url"])
		assert.Equal(t, originalRepository["description"], repository["description"])
		assert.Equal(t, originalRepository["type"], repository["type"])

		// Verify parameters
		responseParams, ok := repository["parameters"].(map[string]interface{})
		assert.True(t, ok, "Parameters should be an object")
		expectedParams := originalRepository["parameters"].(map[string]interface{})
		assert.Equal(t, expectedParams["refType"], responseParams["refType"])
		assert.Equal(t, expectedParams["refValue"], responseParams["refValue"])
	}
}
