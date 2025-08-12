package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestThreatIntegration tests the complete CRUD lifecycle for threats
func TestThreatIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	suite := SetupSubEntityIntegrationTest(t)
	defer suite.TeardownSubEntityIntegrationTest(t)

	t.Run("POST /threat_models/:threat_model_id/threats", func(t *testing.T) {
		testThreatPOST(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/threats", func(t *testing.T) {
		testThreatGETList(t, suite)
	})

	t.Run("GET /threat_models/:threat_model_id/threats/:threat_id", func(t *testing.T) {
		testThreatGETByID(t, suite)
	})

	t.Run("PUT /threat_models/:threat_model_id/threats/:threat_id", func(t *testing.T) {
		testThreatPUT(t, suite)
	})

	t.Run("PATCH /threat_models/:threat_model_id/threats/:threat_id", func(t *testing.T) {
		testThreatPATCH(t, suite)
	})

	t.Run("DELETE /threat_models/:threat_model_id/threats/:threat_id", func(t *testing.T) {
		testThreatDELETE(t, suite)
	})

	t.Run("POST /threat_models/:threat_model_id/threats/bulk", func(t *testing.T) {
		testThreatBulkCreate(t, suite)
	})

	t.Run("PUT /threat_models/:threat_model_id/threats/bulk", func(t *testing.T) {
		testThreatBulkUpdate(t, suite)
	})
}

// testThreatPOST tests creating threats via POST
func testThreatPOST(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data
	requestBody := map[string]interface{}{
		"name":        "Integration Test Threat",
		"description": "A threat created during integration testing",
		"severity":    "high",
		"status":      "identified",
		"threat_type": "spoofing",
		"priority":    "high",
		"mitigated":   false,
		"mitigation":  "Implement strong authentication",
		"score":       8.5,
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/threats", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	// Verify response
	response := suite.assertJSONResponse(t, w, http.StatusCreated)

	// Verify response contains expected fields
	assert.NotEmpty(t, response["id"], "Response should contain ID")
	assert.Equal(t, requestBody["name"], response["name"])
	assert.Equal(t, requestBody["description"], response["description"])
	assert.Equal(t, strings.ToLower("high"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")
	assert.Equal(t, requestBody["status"], response["status"])
	assert.Equal(t, requestBody["threat_type"], response["threat_type"])
	assert.Equal(t, requestBody["priority"], response["priority"])
	assert.Equal(t, requestBody["mitigated"], response["mitigated"])
	assert.Equal(t, requestBody["mitigation"], response["mitigation"])
	assert.Equal(t, requestBody["score"], response["score"])
	assert.Equal(t, suite.threatModelID, response["threat_model_id"])
	assert.NotEmpty(t, response["created_at"], "Response should contain created_at")
	assert.NotEmpty(t, response["modified_at"], "Response should contain modified_at")

	// Store the threat ID for other tests
	suite.testThreatID = response["id"].(string)
}

// testThreatGETList tests retrieving threats list via GET
func testThreatGETList(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have at least one threat
	if suite.testThreatID == "" {
		suite.createTestThreat(t)
	}

	// Test GET list
	path := fmt.Sprintf("/threat_models/%s/threats", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONArrayResponse(t, w, http.StatusOK)

	// Verify response
	assert.GreaterOrEqual(t, len(response), 1, "Should return at least one threat")

	// Check the first threat in the list
	threat := response[0].(map[string]interface{})
	assert.NotEmpty(t, threat["id"])
	assert.NotEmpty(t, threat["name"])
	assert.Equal(t, suite.threatModelID, threat["threat_model_id"])

	// Test pagination
	req = suite.makeAuthenticatedRequest("GET", path+"?limit=1&offset=0", nil)
	w = suite.executeRequest(req)
	paginatedResponse := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.LessOrEqual(t, len(paginatedResponse), 1, "Pagination should limit results")
}

// testThreatGETByID tests retrieving specific threat via GET
func testThreatGETByID(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a threat to get
	if suite.testThreatID == "" {
		suite.createTestThreat(t)
	}

	// Test GET by ID
	path := fmt.Sprintf("/threat_models/%s/threats/%s", suite.threatModelID, suite.testThreatID)
	req := suite.makeAuthenticatedRequest("GET", path, nil)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify response
	assert.Equal(t, suite.testThreatID, response["id"])
	assert.Equal(t, suite.threatModelID, response["threat_model_id"])
	assert.NotEmpty(t, response["name"])
	assert.NotEmpty(t, response["severity"])
	assert.NotEmpty(t, response["status"])
	assert.NotEmpty(t, response["threat_type"])
	assert.NotEmpty(t, response["priority"])
	assert.NotNil(t, response["mitigated"])
}

// testThreatPUT tests updating threats via PUT
func testThreatPUT(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a threat to update
	if suite.testThreatID == "" {
		suite.createTestThreat(t)
	}

	// Update the threat
	updateBody := map[string]interface{}{
		"name":        "Updated Integration Test Threat",
		"description": "Updated description for integration testing",
		"severity":    "critical",
		"status":      "mitigated",
		"threat_type": "tampering",
		"priority":    "critical",
		"mitigated":   true,
		"mitigation":  "Updated mitigation strategy",
		"score":       9.2,
	}

	path := fmt.Sprintf("/threat_models/%s/threats/%s", suite.threatModelID, suite.testThreatID)
	req := suite.makeAuthenticatedRequest("PUT", path, updateBody)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify updates
	assert.Equal(t, suite.testThreatID, response["id"])
	assert.Equal(t, updateBody["name"], response["name"])
	assert.Equal(t, updateBody["description"], response["description"])
	assert.Equal(t, strings.ToLower("critical"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")
	assert.Equal(t, updateBody["status"], response["status"])
	assert.Equal(t, updateBody["threat_type"], response["threat_type"])
	assert.Equal(t, updateBody["priority"], response["priority"])
	assert.Equal(t, updateBody["mitigated"], response["mitigated"])
	assert.Equal(t, updateBody["mitigation"], response["mitigation"])
	assert.Equal(t, updateBody["score"], response["score"])
}

// testThreatPATCH tests patching threats via PATCH (JSON Patch)
func testThreatPATCH(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Ensure we have a threat to patch
	if suite.testThreatID == "" {
		suite.createTestThreat(t)
	}

	// Create JSON Patch operations
	patchOperations := []map[string]interface{}{
		{
			"op":    "replace",
			"path":  "/name",
			"value": "Patched Integration Test Threat",
		},
		{
			"op":    "replace",
			"path":  "/severity",
			"value": "medium",
		},
		{
			"op":    "replace",
			"path":  "/score",
			"value": 6.8,
		},
	}

	path := fmt.Sprintf("/threat_models/%s/threats/%s", suite.threatModelID, suite.testThreatID)
	req := suite.makeAuthenticatedRequest("PATCH", path, patchOperations)
	w := suite.executeRequest(req)

	response := suite.assertJSONResponse(t, w, http.StatusOK)

	// Verify patches were applied
	assert.Equal(t, suite.testThreatID, response["id"])
	assert.Equal(t, "Patched Integration Test Threat", response["name"])
	assert.Equal(t, strings.ToLower("medium"), strings.ToLower(response["severity"].(string)), "Severity comparison should be case-insensitive")
	assert.Equal(t, 6.8, response["score"])
}

// testThreatDELETE tests deleting threats via DELETE
func testThreatDELETE(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Create a threat specifically for deletion
	deleteTestThreatID := suite.createTestThreat(t)

	// Delete the threat
	path := fmt.Sprintf("/threat_models/%s/threats/%s", suite.threatModelID, deleteTestThreatID)
	req := suite.makeAuthenticatedRequest("DELETE", path, nil)
	w := suite.executeRequest(req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Verify the threat no longer exists
	req = suite.makeAuthenticatedRequest("GET", path, nil)
	w = suite.executeRequest(req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// testThreatBulkCreate tests bulk creating threats
func testThreatBulkCreate(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// Test data for bulk create (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"name":        "Bulk Test Threat 1",
			"description": "First threat in bulk create test",
			"severity":    "high",
			"status":      "identified",
			"threat_type": "spoofing",
			"priority":    "high",
			"mitigated":   false,
		},
		{
			"name":        "Bulk Test Threat 2",
			"description": "Second threat in bulk create test",
			"severity":    "medium",
			"status":      "identified",
			"threat_type": "tampering",
			"priority":    "medium",
			"mitigated":   false,
		},
		{
			"name":        "Bulk Test Threat 3",
			"description": "Third threat in bulk create test",
			"severity":    "low",
			"status":      "identified",
			"threat_type": "repudiation",
			"priority":    "low",
			"mitigated":   false,
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/threats/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("POST", path, requestBody)
	w := suite.executeRequest(req)

	createdThreats := suite.assertJSONArrayResponse(t, w, http.StatusCreated)
	assert.Len(t, createdThreats, 3, "Should create 3 threats")

	// Verify each created threat
	for i, threatInterface := range createdThreats {
		threat := threatInterface.(map[string]interface{})
		originalThreat := requestBody[i]

		assert.NotEmpty(t, threat["id"], "Each threat should have an ID")
		assert.Equal(t, originalThreat["name"], threat["name"])
		assert.Equal(t, originalThreat["description"], threat["description"])
		assert.Equal(t, strings.ToLower(originalThreat["severity"].(string)), strings.ToLower(threat["severity"].(string)), "Severity comparison should be case-insensitive")
		assert.Equal(t, suite.threatModelID, threat["threat_model_id"])
	}
}

// testThreatBulkUpdate tests bulk updating threats
func testThreatBulkUpdate(t *testing.T, suite *SubEntityIntegrationTestSuite) {
	// First create some threats to update
	threat1ID := suite.createTestThreat(t)
	threat2ID := suite.createTestThreat(t)

	// Test data for bulk update (direct array, no wrapper)
	requestBody := []map[string]interface{}{
		{
			"id":          threat1ID,
			"name":        "Bulk Updated Threat 1",
			"description": "First threat updated in bulk",
			"severity":    "critical",
			"status":      "mitigated",
			"threat_type": "spoofing",
			"priority":    "critical",
			"mitigated":   true,
			"mitigation":  "Updated mitigation for threat 1",
		},
		{
			"id":          threat2ID,
			"name":        "Bulk Updated Threat 2",
			"description": "Second threat updated in bulk",
			"severity":    "high",
			"status":      "in_progress",
			"threat_type": "tampering",
			"priority":    "high",
			"mitigated":   false,
			"mitigation":  "Updated mitigation for threat 2",
		},
	}

	// Make request
	path := fmt.Sprintf("/threat_models/%s/threats/bulk", suite.threatModelID)
	req := suite.makeAuthenticatedRequest("PUT", path, requestBody)
	w := suite.executeRequest(req)

	updatedThreats := suite.assertJSONArrayResponse(t, w, http.StatusOK)
	assert.Len(t, updatedThreats, 2, "Should update 2 threats")

	// Verify each updated threat
	for i, threatInterface := range updatedThreats {
		threat := threatInterface.(map[string]interface{})
		originalThreat := requestBody[i]

		assert.Equal(t, originalThreat["id"], threat["id"])
		assert.Equal(t, originalThreat["name"], threat["name"])
		assert.Equal(t, originalThreat["description"], threat["description"])
		assert.Equal(t, strings.ToLower(originalThreat["severity"].(string)), strings.ToLower(threat["severity"].(string)), "Severity comparison should be case-insensitive")
		assert.Equal(t, originalThreat["status"], threat["status"])
		assert.Equal(t, originalThreat["mitigated"], threat["mitigated"])
		assert.Equal(t, originalThreat["mitigation"], threat["mitigation"])
	}

	// Verify the threats were actually updated in the database
	for _, threatInterface := range updatedThreats {
		threat := threatInterface.(map[string]interface{})
		threatID := threat["id"].(string)

		path := fmt.Sprintf("/threat_models/%s/threats/%s", suite.threatModelID, threatID)
		req := suite.makeAuthenticatedRequest("GET", path, nil)
		w := suite.executeRequest(req)

		retrievedThreat := suite.assertJSONResponse(t, w, http.StatusOK)
		assert.Equal(t, threat["name"], retrievedThreat["name"], "Updated threat should persist in database")
		assert.Equal(t, strings.ToLower(threat["severity"].(string)), strings.ToLower(retrievedThreat["severity"].(string)), "Updated severity should persist in database (case-insensitive)")
	}
}
